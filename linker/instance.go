package linker

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/linker/internal/bridge"
	"github.com/wippyai/wasm-runtime/linker/internal/invoke"
	"github.com/wippyai/wasm-runtime/linker/internal/memory"
	"github.com/wippyai/wasm-runtime/linker/internal/resolve"
	"github.com/wippyai/wasm-runtime/transcoder"
	"github.com/wippyai/wasm-runtime/wasm"
	"go.bytecodealliance.org/wit"
	"go.uber.org/zap"
)

var instanceCounter uint64
var instanceRegistry sync.Map // map[uint64]*Instance

// instanceContextKey is the context key for the active instance.
// Used by host handlers when the caller module lacks an instanceID suffix.
type instanceContextKey struct{}

// InstanceFromContext extracts the instance from context, or nil if not present.
func InstanceFromContext(ctx context.Context) *Instance {
	if inst, ok := ctx.Value(instanceContextKey{}).(*Instance); ok {
		return inst
	}
	return nil
}

// WithInstance returns a context with the instance attached.
func WithInstance(ctx context.Context, inst *Instance) context.Context {
	return context.WithValue(ctx, instanceContextKey{}, inst)
}

// isValidMemory checks if a memory interface is non-nil and not a typed nil.
func isValidMemory(mem api.Memory) bool {
	if mem == nil {
		return false
	}
	// Check for typed nil (interface non-nil but concrete value nil)
	return !reflect.ValueOf(mem).IsNil()
}

// extractInstanceID parses the instance ID suffix from a module name.
func extractInstanceID(moduleName string) (uint64, bool) {
	for i := len(moduleName) - 1; i >= 0; i-- {
		if moduleName[i] == '#' {
			id, err := strconv.ParseUint(moduleName[i+1:], 10, 64)
			return id, err == nil
		}
	}
	return 0, false
}

// lookupInstanceFromCaller finds the Instance owning the caller module.
func lookupInstanceFromCaller(caller api.Module) *Instance {
	id, ok := extractInstanceID(caller.Name())
	if !ok {
		return nil
	}
	if inst, ok := instanceRegistry.Load(id); ok {
		return inst.(*Instance)
	}
	return nil
}

// resolveMemory returns cached memory and allocator, resolving on first call.
// Not thread-safe.
func (inst *Instance) resolveMemory() (api.Memory, api.Function) {
	if inst.memResolved {
		return inst.cachedMemory, inst.cachedAlloc
	}
	for _, mod := range inst.modules {
		if m := mod.Memory(); isValidMemory(m) {
			inst.cachedMemory = m
			inst.cachedAlloc = mod.ExportedFunction("cabi_realloc")
			break
		}
	}
	inst.memResolved = true
	return inst.cachedMemory, inst.cachedAlloc
}

// trapHandler closes the module with exit code 1.
func trapHandler(ctx context.Context, mod api.Module, _ []uint64) {
	if mod != nil {
		_ = mod.CloseWithExitCode(ctx, 1)
	}
}

// Export represents an exported component function.
type Export struct {
	CoreFunc api.Function
	Canon    *CanonExport
	Name     string
}

// CanonExport holds resolved canonical ABI options for lifted functions.
type CanonExport struct {
	Memory      api.Memory   // linear memory for data
	Realloc     api.Function // allocation function
	PostReturn  api.Function // cleanup function (may be nil)
	ParamTypes  []wit.Type   // WIT types for parameters
	ResultTypes []wit.Type   // WIT types for results (usually 0-1)
	Encoding    byte         // string encoding: 0=UTF8, 1=UTF16, 2=CompactUTF16
}

// Instance represents a live component instance.
// NOT thread-safe: each goroutine should use its own Instance.
type Instance struct {
	cachedMemory    api.Memory
	cachedAlloc     api.Function
	bridgeBuilder   *bridge.Builder
	bridgeCollector *bridge.Collector
	resources       *ResourceStore
	coreInstances   map[int]*coreInstance
	bridgeModules   map[string]bool
	virtualBridges  map[string]bool
	pre             *InstancePre
	exports         map[string]Export
	encoder         *transcoder.Encoder
	decoder         *transcoder.Decoder
	layoutCalc      *transcoder.LayoutCalculator
	modules         []api.Module
	valueSpace      []uint64
	instanceID      uint64
	memResolved     bool
}

// coreInstance wraps either a real wazero module or a virtual instance.
type coreInstance struct {
	module  api.Module
	virtual *VirtualInstance
}

// moduleName returns a unique module name for this instance.
func (inst *Instance) moduleName(base string) string {
	return fmt.Sprintf("%s#%d", base, inst.instanceID)
}

// compFuncSource describes what creates a component function.
type compFuncSource struct {
	kind       compFuncKind
	coreFunc   uint32 // for lifts: core func index being lifted
	reExportOf uint32 // for exports: the function index this re-exports
}

type compFuncKind int

const (
	compFuncAlias  compFuncKind = iota // from function alias
	compFuncLift                       // from canon lift
	compFuncExport                     // from func export (re-export)
)

// maxReExportDepth limits re-export chain resolution to detect cycles.
const maxReExportDepth = 100

// boundModuleWrapper wraps a module to provide alternate memory/realloc.
type boundModuleWrapper struct {
	api.Module
	boundMem   api.Memory
	boundAlloc api.Function
	allocName  string
}

func (w *boundModuleWrapper) Memory() api.Memory {
	if w.boundMem == nil {
		// Fallback to caller's memory if bound memory is nil
		return w.Module.Memory()
	}
	return w.boundMem
}

func (w *boundModuleWrapper) ExportedFunction(name string) api.Function {
	if name == w.allocName && w.boundAlloc != nil {
		return w.boundAlloc
	}
	return w.Module.ExportedFunction(name)
}

// createBoundHandlerFromDef creates a handler that binds memory at call time.
func createBoundHandlerFromDef(def resolve.HostFuncDef) api.GoModuleFunc {
	handler := def.GetHandler()
	return func(ctx context.Context, caller api.Module, stack []uint64) {
		// Try to find the instance: first by caller module name, then by context.
		inst := lookupInstanceFromCaller(caller)
		if inst == nil {
			inst = InstanceFromContext(ctx)
		}
		if inst == nil {
			handler(ctx, caller, stack)
			return
		}

		mem, alloc := inst.resolveMemory()
		if mem != nil {
			wrapped := &boundModuleWrapper{
				Module:     caller,
				boundMem:   mem,
				boundAlloc: alloc,
				allocName:  "cabi_realloc",
			}
			handler(ctx, wrapped, stack)
		} else {
			handler(ctx, caller, stack)
		}
	}
}

// createSharedMemoryHandler creates a handler that resolves memory at call time.
func createSharedMemoryHandler(def *FuncDef) api.GoModuleFunc {
	return createSharedMemoryHandlerFromDef(def)
}

// createSharedMemoryHandlerFromDef creates a handler from a HostFuncDef.
func createSharedMemoryHandlerFromDef(def resolve.HostFuncDef) api.GoModuleFunc {
	handler := def.GetHandler()
	return func(ctx context.Context, caller api.Module, stack []uint64) {
		// Try caller module name first, then context (for shim modules without #instanceID)
		inst := lookupInstanceFromCaller(caller)
		if inst == nil {
			inst = InstanceFromContext(ctx)
		}
		if inst == nil {
			handler(ctx, caller, stack)
			return
		}

		mem, alloc := inst.resolveMemory()
		if mem != nil {
			wrapped := &boundModuleWrapper{
				Module:     caller,
				boundMem:   mem,
				boundAlloc: alloc,
				allocName:  "cabi_realloc",
			}
			handler(ctx, wrapped, stack)
		} else {
			handler(ctx, caller, stack)
		}
	}
}

// NewInstance creates a new live instance.
func (pre *InstancePre) NewInstance(ctx context.Context) (*Instance, error) {
	numInst := pre.numInstances
	if numInst == 0 {
		numInst = 1
	}
	numExp := pre.numExports
	if numExp == 0 {
		numExp = 4
	}

	compiler := transcoder.NewCompiler()

	bridgeBuilder, err := bridge.NewBuilder(pre.linker.runtime)
	if err != nil {
		return nil, instError("init", -1, "", "failed to create bridge builder", err)
	}

	inst := &Instance{
		pre:             pre,
		instanceID:      atomic.AddUint64(&instanceCounter, 1),
		modules:         make([]api.Module, 0, numInst),
		exports:         make(map[string]Export, numExp),
		resources:       NewResourceStore(),
		coreInstances:   make(map[int]*coreInstance, numInst),
		bridgeModules:   make(map[string]bool, numInst),
		virtualBridges:  make(map[string]bool),
		bridgeBuilder:   bridgeBuilder,
		bridgeCollector: bridge.NewCollector(),
		encoder:         transcoder.NewEncoderWithCompiler(compiler),
		decoder:         transcoder.NewDecoderWithCompiler(compiler),
		layoutCalc:      transcoder.NewLayoutCalculator(),
	}

	instanceRegistry.Store(inst.instanceID, inst)

	if pre.graph == nil {
		return inst, nil
	}

	order := pre.topoOrder
	if order == nil {
		var err error
		order, err = pre.graph.TopologicalSort()
		if err != nil {
			inst.Close(ctx)
			return nil, instError("compile", -1, "", "topological sort failed", err)
		}
	}

	if err := inst.ensureHostModules(ctx); err != nil {
		inst.Close(ctx)
		return nil, err
	}

	for _, idx := range order {
		parsedInst := pre.graph.Instances[idx]
		if parsedInst == nil {
			continue
		}

		switch parsedInst.Kind {
		case component.CoreInstanceInstantiate:
			mod, err := inst.instantiateModule(ctx, idx, parsedInst)
			if err != nil {
				inst.Close(ctx)
				// Error already wrapped by instantiateModule
				return nil, err
			}
			inst.modules = append(inst.modules, mod)
			inst.coreInstances[idx] = &coreInstance{module: mod}

			if err := inst.createGlobalBridges(ctx, idx, int(parsedInst.ModuleIndex), mod); err != nil {
				inst.Close(ctx)
				return nil, err
			}

		case component.CoreInstanceFromExports:
			virt := inst.createVirtualInstance(idx, parsedInst)
			inst.coreInstances[idx] = &coreInstance{virtual: virt}
		}
	}

	inst.buildExports()

	if err := inst.callStart(ctx); err != nil {
		inst.Close(ctx)
		// Error already wrapped by callStart
		return nil, err
	}

	return inst, nil
}

// instantiateModule creates a core module instance.
func (inst *Instance) instantiateModule(
	ctx context.Context,
	instanceIdx int,
	parsed *component.ParsedCoreInstance,
) (api.Module, error) {
	if int(parsed.ModuleIndex) >= len(inst.pre.compiled) {
		return nil, instError("module_instantiate", instanceIdx, "",
			fmt.Sprintf("module index %d out of range (have %d)", parsed.ModuleIndex, len(inst.pre.compiled)), nil)
	}

	compiled := inst.pre.compiled[parsed.ModuleIndex]
	moduleName := inst.moduleName(fmt.Sprintf("$%d", instanceIdx))
	modConfig := wazero.NewModuleConfig().WithName(moduleName)

	expectedFuncs := make(map[string]map[string]importSig)
	for _, fn := range compiled.ImportedFunctions() {
		modName, funcName, _ := fn.Import()
		if modName == "" {
			modName = EmptyModuleName
		}
		if expectedFuncs[modName] == nil {
			expectedFuncs[modName] = make(map[string]importSig)
		}
		expectedFuncs[modName][funcName] = importSig{
			params:  fn.ParamTypes(),
			results: fn.ResultTypes(),
		}
	}
	// Wazero doesn't expose table imports, so parse raw bytes
	var expectedTables map[string]map[string]tableLimit
	if inst.pre.component != nil && inst.pre.component.Raw != nil {
		comp := inst.pre.component.Raw
		if int(parsed.ModuleIndex) < len(comp.CoreModules) {
			expectedTables = getExpectedTableLimits(comp.CoreModules[parsed.ModuleIndex])
		}
	}

	argsModules := make(map[string]bool)

	for _, arg := range parsed.Args {
		if arg.Kind != component.CoreInstantiateInstance {
			continue
		}

		argName := arg.Name
		if argName == "" {
			argName = EmptyModuleName
		}

		var source *coreInstance
		if resolver := inst.pre.linker.resolver; resolver != nil {
			if virt := resolver.GetInstance(argName); virt != nil {
				source = &coreInstance{virtual: virt}
			} else if mod := resolver.GetModule(argName); mod != nil {
				source = &coreInstance{module: mod}
			}
		}

		if source == nil {
			source = inst.coreInstances[int(arg.InstanceIndex)]
		}
		if source == nil {
			continue
		}

		created, err := inst.createBridgeFrom(ctx, argName, source, nil, expectedFuncs[argName], expectedTables[argName])
		if err != nil {
			return nil, instError("bridge_create", instanceIdx, argName,
				fmt.Sprintf("from instance %d", arg.InstanceIndex), err)
		}
		if created {
			argsModules[argName] = true
		}
	}

	bindingsByModule := make(map[string][]resolvedBinding)
	for _, binding := range inst.pre.bindings {
		if binding.InstanceIndex != instanceIdx {
			continue
		}
		// Skip imports already satisfied by Args
		if argsModules[binding.ModuleName] {
			continue
		}
		bindingsByModule[binding.ModuleName] = append(bindingsByModule[binding.ModuleName], binding)
	}

	if resolver := inst.pre.linker.resolver; resolver != nil {
		for importModName := range bindingsByModule {
			var source *coreInstance
			if virt := resolver.GetInstance(importModName); virt != nil {
				source = &coreInstance{virtual: virt}
			} else if mod := resolver.GetModule(importModName); mod != nil {
				source = &coreInstance{module: mod}
			}
			if source != nil {
				// Pass bindings to merge host functions into the bridge module
				created, err := inst.createBridgeFrom(ctx, importModName, source, bindingsByModule[importModName], expectedFuncs[importModName], expectedTables[importModName])
				if err != nil {
					return nil, instError("bridge_create", instanceIdx, importModName, "from resolver", err)
				}
				if created {
					argsModules[importModName] = true
				}
			}
		}
		for name := range argsModules {
			delete(bindingsByModule, name)
		}
	}

	for importModName, bindings := range bindingsByModule {
		localBindings := bindings // capture for closure
		_, _, err := inst.pre.linker.getOrCreateHostModule(ctx, importModName, func() (api.Module, error) {
			// Check if this would replace a synth module that has globals
			if existing := inst.pre.linker.runtime.Module(importModName); existing != nil {
				if existing.ExportedGlobal("libpython3.12.so:memory_base") != nil {
					return existing, nil
				}
			}

			hostBuilder := inst.pre.linker.runtime.NewHostModuleBuilder(importModName)
			hasExports := false

			for _, binding := range localBindings {
				if binding.FuncDef != nil {
					hasExports = true
					handler := createSharedMemoryHandler(binding.FuncDef)
					// Use binding types (from WASM import) to match the expected signature
					hostBuilder.NewFunctionBuilder().
						WithGoModuleFunction(handler, binding.ParamTypes, binding.ResultTypes).
						Export(binding.ImportName)
				} else if binding.Trap {
					hasExports = true
					hostBuilder.NewFunctionBuilder().
						WithGoModuleFunction(api.GoModuleFunc(trapHandler), binding.ParamTypes, binding.ResultTypes).
						Export(binding.ImportName)
				}
			}

			if !hasExports {
				return nil, nil
			}
			return hostBuilder.Instantiate(ctx)
		})
		if err != nil {
			return nil, instError("import_resolution", instanceIdx, importModName, "host module instantiation failed", err)
		}
	}

	mod, err := inst.pre.linker.runtime.InstantiateModule(ctx, compiled, modConfig)
	if err != nil {
		return nil, instError("module_instantiate", instanceIdx, "", "wazero instantiation failed", err)
	}

	return mod, nil
}

// ensureHostModules creates all host modules upfront with all needed exports.
func (inst *Instance) ensureHostModules(ctx context.Context) error {
	if inst.pre.hostModuleBindings == nil {
		return nil
	}

	for importModName, bindings := range inst.pre.hostModuleBindings {
		localBindings := bindings // capture for closure
		_, _, err := inst.pre.linker.getOrCreateHostModule(ctx, importModName, func() (api.Module, error) {
			hostBuilder := inst.pre.linker.runtime.NewHostModuleBuilder(importModName)
			hasExports := false

			for _, binding := range localBindings {
				if binding.FuncDef != nil {
					hasExports = true
					handler := createSharedMemoryHandler(binding.FuncDef)
					// Use binding types (from WASM import) to match the expected signature
					hostBuilder.NewFunctionBuilder().
						WithGoModuleFunction(handler, binding.ParamTypes, binding.ResultTypes).
						Export(binding.ImportName)
				} else if binding.Trap {
					hasExports = true
					hostBuilder.NewFunctionBuilder().
						WithGoModuleFunction(api.GoModuleFunc(trapHandler), binding.ParamTypes, binding.ResultTypes).
						Export(binding.ImportName)
				}
			}

			if !hasExports {
				return nil, nil
			}
			return hostBuilder.Instantiate(ctx)
		})
		if err != nil {
			return instError("import_resolution", -1, importModName, "host module instantiation failed", err)
		}
	}

	return nil
}

// importSig describes the expected signature for an import.
type importSig struct {
	params  []api.ValueType
	results []api.ValueType
}

// tableLimit describes the expected table limits for an import.
type tableLimit struct {
	min uint32
	max uint32 // 0 means unbounded
}

// getExpectedTableLimits parses raw WASM bytes to extract table import limits.
func getExpectedTableLimits(wasmBytes []byte) map[string]map[string]tableLimit {
	result := make(map[string]map[string]tableLimit)

	mod, err := wasm.ParseModule(wasmBytes)
	if err != nil {
		return result
	}

	for _, imp := range mod.Imports {
		if imp.Desc.Kind != wasm.KindTable || imp.Desc.Table == nil {
			continue
		}
		modName := imp.Module
		if modName == "" {
			modName = EmptyModuleName
		}
		if result[modName] == nil {
			result[modName] = make(map[string]tableLimit)
		}
		var maxVal uint32
		if imp.Desc.Table.Limits.Max != nil {
			maxVal = uint32(*imp.Desc.Table.Limits.Max)
		}
		result[modName][imp.Name] = tableLimit{
			min: uint32(imp.Desc.Table.Limits.Min),
			max: maxVal,
		}
	}

	return result
}

// createBridgeFrom creates a bridge module from a core instance.
func (inst *Instance) createBridgeFrom(ctx context.Context, name string, source *coreInstance, bindings []resolvedBinding, expectedTypes map[string]importSig, expectedTableLimits map[string]tableLimit) (bool, error) {
	if name == "env" {
		if existing := inst.pre.linker.runtime.Module(name); existing != nil {
			if existing.ExportedGlobal("libpython3.12.so:memory_base") != nil {
				return true, nil
			}
		}
	}

	if source.virtual != nil {
		if source.virtual.HasTableEntity() || source.virtual.HasMemory() || source.virtual.HasGlobals() {
			return inst.createSynthBridgeFrom(ctx, name, source.virtual, bindings, expectedTypes, expectedTableLimits)
		}
	}

	if source.module != nil {
		moduleIndex := -1
		for i, ci := range inst.coreInstances {
			if ci != nil && ci.module == source.module {
				// Find the CoreInstance that created this module to get ModuleIndex
				if i < len(inst.pre.component.Raw.CoreInstances) {
					parsed := inst.pre.component.Raw.CoreInstances[i].Parsed
					if parsed != nil && parsed.Kind == component.CoreInstanceInstantiate {
						moduleIndex = int(parsed.ModuleIndex)
					}
				}
				break
			}
		}
		return inst.createSynthBridgeFromModule(ctx, name, source.module, bindings, moduleIndex)
	}

	var exports []bridge.Export
	if source.virtual != nil {
		exports = inst.collectVirtualExports(source.virtual, name)
	}

	// Add required host functions that aren't in the exports yet
	// The graph tells us which functions the host must provide
	exports = inst.addRequiredHostFunctions(exports, name)

	// Also add host functions from bindings (these may not be in the source)
	exports = inst.mergeBindingExports(exports, bindings)

	// Override types with expected types from ALL core modules (pre-computed)
	// This is critical for canon lower functions where the lowered core signature
	// may differ from the host function's registered types
	// Use pre-computed types from InstancePre which aggregates across all consumers
	preExpected := inst.pre.expectedFuncTypes[name]
	if len(preExpected) > 0 {
		for i := range exports {
			if sig, ok := preExpected[exports[i].Name]; ok {
				exports[i].ParamTypes = sig.params
				exports[i].ResultTypes = sig.results
			}
		}
	}

	if len(exports) == 0 {
		return false, nil
	}

	needsReplace := source.module != nil && inst.virtualBridges[name]

	if needsReplace {
		_, _, err := inst.pre.linker.getOrReplaceHostModule(ctx, name,
			func(existing api.Module) bool { return existing != nil && !inst.virtualBridges[name] },
			func() (api.Module, error) {
				builder := inst.pre.linker.runtime.NewHostModuleBuilder(name)
				for _, exp := range exports {
					builder.NewFunctionBuilder().
						WithGoModuleFunction(exp.Fn, exp.ParamTypes, exp.ResultTypes).
						Export(exp.Name)
				}
				return builder.Instantiate(ctx)
			})
		if err != nil {
			return false, err
		}
		delete(inst.virtualBridges, name)
	} else {
		_, _, err := inst.pre.linker.getOrCreateHostModule(ctx, name, func() (api.Module, error) {
			builder := inst.pre.linker.runtime.NewHostModuleBuilder(name)
			for _, exp := range exports {
				builder.NewFunctionBuilder().
					WithGoModuleFunction(exp.Fn, exp.ParamTypes, exp.ResultTypes).
					Export(exp.Name)
			}
			return builder.Instantiate(ctx)
		})
		if err != nil {
			return false, err
		}
	}

	if source.virtual != nil && inst.virtualBridges != nil {
		inst.virtualBridges[name] = true
	}
	return true, nil
}

// addRequiredHostFunctions adds required host functions not already in exports.
func (inst *Instance) addRequiredHostFunctions(exports []bridge.Export, namespace string) []bridge.Export {
	if inst.pre.depGraph == nil {
		return exports
	}

	existing := make(map[string]bool)
	for _, exp := range exports {
		existing[exp.Name] = true
	}

	required := inst.pre.depGraph.RequiredHostFunctions()
	prefix := namespace + "#"
	for key := range required {
		if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		funcName := key[len(prefix):]
		if existing[funcName] {
			continue
		}

		hostDef := inst.pre.linker.Resolve(key)
		if hostDef != nil {
			handler := createSharedMemoryHandler(hostDef)
			exports = append(exports, bridge.Export{
				Name:        funcName,
				Fn:          handler,
				ParamTypes:  hostDef.ParamTypes,
				ResultTypes: hostDef.ResultTypes,
			})
			existing[funcName] = true
		}
	}

	return exports
}

// mergeBindingExports adds host function exports from bindings to existing exports.
func (inst *Instance) mergeBindingExports(exports []bridge.Export, bindings []resolvedBinding) []bridge.Export {
	if len(bindings) == 0 {
		return exports
	}

	hostBindings := make([]bridge.HostBinding, 0, len(bindings))
	for _, b := range bindings {
		hb := bridge.HostBinding{
			ImportName:  b.ImportName,
			ParamTypes:  b.ParamTypes,
			ResultTypes: b.ResultTypes,
			IsTrap:      b.Trap,
		}
		if b.FuncDef != nil {
			hb.Handler = createSharedMemoryHandler(b.FuncDef)
			hb.ParamTypes = b.FuncDef.ParamTypes
			hb.ResultTypes = b.FuncDef.ResultTypes
		}
		hostBindings = append(hostBindings, hb)
	}

	return inst.bridgeCollector.MergeBindings(exports, hostBindings)
}

// createSynthBridgeFrom creates a synthetic WASM module for virtual instances.
func (inst *Instance) createSynthBridgeFrom(ctx context.Context, name string, virt *VirtualInstance, bindings []resolvedBinding, expectedTypes map[string]importSig, expectedTableLimits map[string]tableLimit) (bool, error) {
	hostModName := name + "$host"

	// Collect function exports for the host module (outside lock)
	exports := inst.collectVirtualExports(virt, name)

	// Add required host functions that aren't in the exports yet
	exports = inst.addRequiredHostFunctions(exports, name)

	// Also add host functions from bindings
	exports = inst.mergeBindingExports(exports, bindings)

	// Override types with expected types from ALL core modules (pre-computed)
	preExpected := inst.pre.expectedFuncTypes[name]
	if len(preExpected) > 0 {
		for i := range exports {
			if sig, ok := preExpected[exports[i].Name]; ok {
				exports[i].ParamTypes = sig.params
				exports[i].ResultTypes = sig.results
			}
		}
	}

	// Find table source (outside lock)
	var tableSource *TableSource
	var tableExportName string
	for entityName, entity := range virt.Entities() {
		if entity.Kind == EntityTable {
			if src, ok := entity.Source.(TableSource); ok {
				tableSource = &src
				tableExportName = entityName
			}
			break
		}
	}

	// Find memory source from stored DirectMemory info (outside lock)
	var memorySource api.Module
	var memorySourceExportName string // name in source module
	var memoryExportName string       // name to export as
	for entityName, entity := range virt.Entities() {
		if entity.Kind == EntityMemory {
			if src, ok := entity.Source.(DirectMemory); ok && src.Memory != nil {
				// Use stored source info when available
				if src.SourceModule != nil && src.SourceExport != "" {
					memorySource = src.SourceModule
					memorySourceExportName = src.SourceExport
					memoryExportName = entityName
				} else {
					// Fallback: search coreInstances for the memory source
					for _, ci := range inst.coreInstances {
						if ci == nil || ci.module == nil {
							continue
						}
						if ci.module.Memory() == src.Memory {
							memorySource = ci.module
							memoryExportName = entityName
							for expName := range ci.module.ExportedMemoryDefinitions() {
								memorySourceExportName = expName
								break
							}
							break
						}
					}
				}
			}
			break
		}
	}

	// Collect global sources for re-export
	type globalExport struct {
		exportName   string     // name to export as
		sourceModule api.Module // source module
		sourceName   string     // export name in source module
		valType      api.ValueType
		mutable      bool
	}
	var globals []globalExport
	for entityName, entity := range virt.Entities() {
		if entity.Kind == EntityGlobal {
			if src, ok := entity.Source.(DirectGlobal); ok && src.SourceModule != nil {
				// Get global type from the source
				var valType api.ValueType
				var mutable bool
				if src.Global != nil {
					valType = src.Global.Type()
					// Check if mutable by type assertion
					_, mutable = src.Global.(api.MutableGlobal)
				} else {
					// Default to i32 if we can't determine the type
					valType = api.ValueTypeI32
				}
				globals = append(globals, globalExport{
					exportName:   entityName,
					sourceModule: src.SourceModule,
					sourceName:   src.SourceExport,
					valType:      valType,
					mutable:      mutable,
				})
			}
		}
	}

	// Need at least something to export
	if len(exports) == 0 && tableSource == nil && memorySource == nil && len(globals) == 0 {
		return false, nil
	}

	// Create host module with synchronized access (only if we have functions)
	// Always close and recreate the host module because different virtual instances
	// may need different function exports, and we can't reliably check host module exports
	var hostModCreated bool
	if len(exports) > 0 {
		// Always recreate - close old host module if it exists
		alwaysFalse := func(mod api.Module) bool { return false }
		var err error
		_, hostModCreated, err = inst.pre.linker.getOrReplaceHostModule(ctx, hostModName, alwaysFalse, func() (api.Module, error) {
			hostBuilder := inst.pre.linker.runtime.NewHostModuleBuilder(hostModName)
			for _, exp := range exports {
				hostBuilder.NewFunctionBuilder().
					WithGoModuleFunction(exp.Fn, exp.ParamTypes, exp.ResultTypes).
					Export(exp.Name)
			}
			return hostBuilder.Instantiate(ctx)
		})
		if err != nil {
			return false, err
		}
		if hostModCreated {
			inst.bridgeModules[hostModName] = true
			inst.pre.linker.addBridgeRefs(map[string]bool{hostModName: true})
		}
	}

	// Create validator that checks for required exports.
	// If we need a table, memory, or global that the existing module doesn't have, recreate it.
	validator := func(mod api.Module) bool {
		// Tables: wazero doesn't expose ExportedTableDefinitions, so we use a heuristic:
		// if we need a table export and the module was created without one, the instantiation
		// of the module that needs the table will fail. Since we can't reliably detect table
		// exports, if we need a table, always recreate (conservative approach).
		if tableSource != nil && tableExportName != "" {
			return false
		}
		// Check if module has required memory export
		if memorySource != nil && memoryExportName != "" {
			memDefs := mod.ExportedMemoryDefinitions()
			if _, ok := memDefs[memoryExportName]; !ok {
				return false
			}
		}
		// Check if module has required global exports
		for _, g := range globals {
			if mod.ExportedGlobal(g.exportName) == nil {
				return false
			}
		}
		return true
	}

	// Create synthetic module with synchronized access, replacing if exports don't match
	_, synthCreated, err := inst.pre.linker.getOrReplaceHostModule(ctx, name, validator, func() (api.Module, error) {
		builder := newSynthModuleBuilder(hostModName)
		for _, exp := range exports {
			builder.addFunc(exp.Name, exp.ParamTypes, exp.ResultTypes)
		}

		if tableSource != nil {
			sourceModName := tableSource.Module.Name()
			builder.setTableImport(sourceModName, tableSource.ExportName, tableExportName)
		}

		// Determine table size: use max of resolved exports and expected limits
		tableSize := uint32(len(exports))
		if limit, ok := expectedTableLimits[tableExportName]; ok && limit.min > tableSize {
			tableSize = limit.min
		}
		// Also check $imports as a common table name
		if limit, ok := expectedTableLimits["$imports"]; ok && limit.min > tableSize {
			tableSize = limit.min
		}
		if tableSize > 0 {
			builder.setTableSize(tableSize)
		}

		if memorySource != nil && memorySourceExportName != "" {
			sourceModName := memorySource.Name()
			builder.setMemoryImport(sourceModName, memorySourceExportName, memoryExportName)
		}

		// Add global imports
		for _, g := range globals {
			builder.addGlobalImport(g.sourceModule.Name(), g.sourceName, g.exportName, g.valType, g.mutable)
		}

		synthWasm := builder.build()
		if synthWasm == nil {
			return nil, nil
		}

		compiled, compileErr := inst.pre.linker.runtime.CompileModule(ctx, synthWasm)
		if compileErr != nil {
			return nil, compileErr
		}

		modConfig := wazero.NewModuleConfig().WithName(name)
		mod, instErr := inst.pre.linker.runtime.InstantiateModule(ctx, compiled, modConfig)
		if instErr != nil {
			compiled.Close(ctx)
			return nil, instErr
		}
		return mod, nil
	})
	if err != nil {
		return false, err
	}

	// Track only if we created the module
	if synthCreated {
		inst.bridgeModules[name] = true
		inst.pre.linker.addBridgeRefs(map[string]bool{name: true})
	}
	return true, nil
}

// createSynthBridgeFromModule creates a synthetic WASM bridge for a real module.
func (inst *Instance) createSynthBridgeFromModule(ctx context.Context, name string, sourceMod api.Module, bindings []resolvedBinding, moduleIndex int) (bool, error) {
	sourceModName := sourceMod.Name()
	hostModName := name + "$host"

	// Check if already exists
	if inst.pre.linker.runtime.Module(name) != nil {
		// Check if we need to replace a virtual bridge
		if inst.virtualBridges[name] {
			if existingMod := inst.pre.linker.runtime.Module(name); existingMod != nil {
				if err := existingMod.Close(ctx); err != nil {
					Logger().Warn("failed to close existing virtual bridge module",
						zap.String("module", name),
						zap.Error(err))
				}
			}
			delete(inst.virtualBridges, name)
		} else {
			return true, nil
		}
	}

	// Collect all function exports from the source module
	var exports []bridge.Export
	funcDefs := sourceMod.ExportedFunctionDefinitions()
	for funcName, def := range funcDefs {
		fn := inst.safeGetExportedFunction(sourceMod, funcName)
		if fn == nil {
			continue
		}
		wrapper := bridge.ForwardingWrapper(fn, len(def.ParamTypes()))
		if wrapper == nil {
			continue
		}
		exports = append(exports, bridge.Export{
			Name:        funcName,
			Fn:          wrapper,
			ParamTypes:  def.ParamTypes(),
			ResultTypes: def.ResultTypes(),
		})
	}

	// Add required host functions
	exports = inst.addRequiredHostFunctions(exports, name)
	exports = inst.mergeBindingExports(exports, bindings)

	// Apply expected types from pre-computed set
	preExpected := inst.pre.expectedFuncTypes[name]
	if len(preExpected) > 0 {
		for i := range exports {
			if sig, ok := preExpected[exports[i].Name]; ok {
				exports[i].ParamTypes = sig.params
				exports[i].ResultTypes = sig.results
			}
		}
	}

	// Create host module for functions
	var hostModCreated bool
	if len(exports) > 0 {
		var err error
		_, hostModCreated, err = inst.pre.linker.getOrCreateHostModule(ctx, hostModName, func() (api.Module, error) {
			builder := inst.pre.linker.runtime.NewHostModuleBuilder(hostModName)
			for _, exp := range exports {
				builder.NewFunctionBuilder().
					WithGoModuleFunction(exp.Fn, exp.ParamTypes, exp.ResultTypes).
					Export(exp.Name)
			}
			return builder.Instantiate(ctx)
		})
		if err != nil {
			return false, err
		}
	}

	// Find memory export name
	var memoryExportName string
	memDefs := sourceMod.ExportedMemoryDefinitions()
	for memName := range memDefs {
		memoryExportName = memName
		break
	}

	// Parse global exports from raw module bytes
	var globalExports []GlobalExport
	if moduleIndex >= 0 && inst.pre.component != nil && inst.pre.component.Raw != nil {
		if moduleIndex < len(inst.pre.component.Raw.CoreModules) {
			globalExports = parseGlobalExports(inst.pre.component.Raw.CoreModules[moduleIndex])
		}
	}

	// Build synthetic WASM that imports from host module and source module
	_, synthCreated, err := inst.pre.linker.getOrCreateHostModule(ctx, name, func() (api.Module, error) {
		builder := newSynthModuleBuilder(hostModName)

		// Add functions for import from host module
		for _, exp := range exports {
			builder.addFunc(exp.Name, exp.ParamTypes, exp.ResultTypes)
		}

		// Import memory from source module
		if memoryExportName != "" {
			builder.setMemoryImport(sourceModName, memoryExportName, memoryExportName)
		}

		// Import globals from source module
		for _, g := range globalExports {
			builder.addGlobalImport(sourceModName, g.Name, g.Name, g.ValType, g.Mutable)
		}

		synthWasm := builder.build()
		if synthWasm == nil {
			return nil, nil
		}

		compiled, compileErr := inst.pre.linker.runtime.CompileModule(ctx, synthWasm)
		if compileErr != nil {
			return nil, compileErr
		}

		modConfig := wazero.NewModuleConfig().WithName(name)
		mod, instErr := inst.pre.linker.runtime.InstantiateModule(ctx, compiled, modConfig)
		if instErr != nil {
			compiled.Close(ctx)
			return nil, instErr
		}
		return mod, nil
	})
	if err != nil {
		// Clean up host module if we created it but synth module failed
		if hostModCreated {
			if hostMod := inst.pre.linker.runtime.Module(hostModName); hostMod != nil {
				if closeErr := hostMod.Close(ctx); closeErr != nil {
					Logger().Warn("failed to close host module during cleanup",
						zap.String("module", hostModName),
						zap.Error(closeErr))
				}
			}
		}
		return false, err
	}

	// Track only if we created the synth module
	if synthCreated {
		inst.bridgeModules[name] = true
		inst.pre.linker.addBridgeRefs(map[string]bool{name: true})
	}
	return true, nil
}

// createGlobalBridges creates synthetic bridge modules for global exports.
func (inst *Instance) createGlobalBridges(ctx context.Context, instanceIdx, moduleIdx int, mod api.Module) error {
	if inst.pre.expectedGlobalTypes == nil || inst.pre.component == nil {
		return nil
	}

	// Parse global exports from the module's raw bytes
	if moduleIdx < 0 || moduleIdx >= len(inst.pre.component.Raw.CoreModules) {
		return nil
	}
	moduleExports := parseGlobalExports(inst.pre.component.Raw.CoreModules[moduleIdx])
	if len(moduleExports) == 0 {
		return nil
	}

	// Build a map of exported global names for quick lookup
	exportedGlobals := make(map[string]GlobalExport)
	for _, g := range moduleExports {
		exportedGlobals[g.Name] = g
	}

	// Check each import module that needs globals
	// Only handle "env" module for now - other modules like GOT.mem have different semantics
	for importModName, neededGlobals := range inst.pre.expectedGlobalTypes {
		// Only create global bridges for "env" module (standard WASI dynamic linking)
		// Other modules like GOT.mem, GOT.func, libc.so, libpython3.12.so have
		// different linking semantics that aren't handled here
		if importModName != "env" {
			continue
		}

		// Find matching globals FIRST - before any module operations
		var matchingGlobals []GlobalExport
		for globalName, importInfo := range neededGlobals {
			if _, ok := exportedGlobals[globalName]; ok {
				matchingGlobals = append(matchingGlobals, GlobalExport{
					Name:    globalName,
					ValType: importInfo.ValType,
					Mutable: importInfo.Mutable,
				})
			}
		}

		// Skip if this module doesn't export any matching globals
		if len(matchingGlobals) == 0 {
			continue
		}

		// Check if module already exists with all needed globals
		if existingMod := inst.pre.linker.runtime.Module(importModName); existingMod != nil {
			hasAllFromThisModule := true
			for _, g := range matchingGlobals {
				if existingMod.ExportedGlobal(g.Name) == nil {
					hasAllFromThisModule = false
					break
				}
			}
			if hasAllFromThisModule {
				continue
			}
			// Close existing module to recreate with globals
			if err := existingMod.Close(ctx); err != nil {
				Logger().Warn("failed to close existing module before recreating with globals",
					zap.String("module", importModName),
					zap.Error(err))
			}
		}

		// Create synthetic bridge module that imports globals from this module
		// and exports them to the import module
		sourceModName := mod.Name()
		hostModName := importModName + "$host"

		// Get any existing function bindings for this import module
		var funcBindings []resolvedBinding
		funcNames := make(map[string]bool)
		for _, b := range inst.pre.bindings {
			if b.ModuleName == importModName && (b.FuncDef != nil || b.Trap) {
				funcBindings = append(funcBindings, b)
				funcNames[b.ImportName] = true
			}
		}

		// Remove globals that conflict with function names
		// In WASM, a module can't export both a function and global with the same name
		filteredGlobals := make([]GlobalExport, 0, len(matchingGlobals))
		for _, g := range matchingGlobals {
			if !funcNames[g.Name] {
				filteredGlobals = append(filteredGlobals, g)
			}
		}
		matchingGlobals = filteredGlobals

		if len(matchingGlobals) == 0 && len(funcBindings) == 0 {
			continue
		}

		// Create or get the host module for functions first (if needed)
		if len(funcBindings) > 0 {
			_, _, err := inst.pre.linker.getOrCreateHostModule(ctx, hostModName, func() (api.Module, error) {
				hostBuilder := inst.pre.linker.runtime.NewHostModuleBuilder(hostModName)
				for _, binding := range funcBindings {
					if binding.FuncDef != nil {
						handler := createSharedMemoryHandler(binding.FuncDef)
						hostBuilder.NewFunctionBuilder().
							WithGoModuleFunction(handler, binding.ParamTypes, binding.ResultTypes).
							Export(binding.ImportName)
					} else if binding.Trap {
						hostBuilder.NewFunctionBuilder().
							WithGoModuleFunction(api.GoModuleFunc(trapHandler), binding.ParamTypes, binding.ResultTypes).
							Export(binding.ImportName)
					}
				}
				return hostBuilder.Instantiate(ctx)
			})
			if err != nil {
				return instError("global_bridge", instanceIdx, importModName, "host module creation failed", err)
			}
		}

		// Build synthetic WASM module that imports from source and exports to target
		_, synthCreated, err := inst.pre.linker.getOrCreateHostModule(ctx, importModName, func() (api.Module, error) {
			builder := newSynthModuleBuilder(hostModName)

			// Track exported names to avoid duplicates
			exportedNames := make(map[string]bool)

			// Add functions from host module
			funcCount := 0
			for _, binding := range funcBindings {
				if exportedNames[binding.ImportName] {
					continue // skip duplicate
				}
				exportedNames[binding.ImportName] = true
				builder.addFunc(binding.ImportName, binding.ParamTypes, binding.ResultTypes)
				funcCount++
			}

			// Check if source module has memory and add memory import
			if mod.Memory() != nil {
				for memName := range mod.ExportedMemoryDefinitions() {
					builder.setMemoryImport(sourceModName, memName, "memory")
					break
				}
			}

			// Check if source module has table and add table import
			if mod.ExportedGlobal("__indirect_function_table") == nil {
				if moduleIdx >= 0 && moduleIdx < len(inst.pre.component.Raw.CoreModules) {
					tableExports := parseTableExports(inst.pre.component.Raw.CoreModules[moduleIdx])
					for _, tableName := range tableExports {
						builder.setTableImport(sourceModName, tableName, "__indirect_function_table")
						break
					}
				}
			}

			// Add global imports from source module
			for _, g := range matchingGlobals {
				if exportedNames[g.Name] {
					continue
				}
				exportedNames[g.Name] = true
				builder.addGlobalImport(sourceModName, g.Name, g.Name, g.ValType, g.Mutable)
			}

			// Add special WASI dynamic linking globals that aren't exported by source
			for globalName, importInfo := range neededGlobals {
				if exportedNames[globalName] {
					continue
				}
				if globalName == "__memory_base" || globalName == "__table_base" {
					exportedNames[globalName] = true
					builder.addLocalGlobal(globalName, importInfo.ValType, importInfo.Mutable, 0)
				}
			}

			synthWasm := builder.build()
			if synthWasm == nil {
				return nil, nil
			}

			compiled, compileErr := inst.pre.linker.runtime.CompileModule(ctx, synthWasm)
			if compileErr != nil {
				return nil, compileErr
			}

			modConfig := wazero.NewModuleConfig().WithName(importModName)
			bridgeMod, instErr := inst.pre.linker.runtime.InstantiateModule(ctx, compiled, modConfig)
			if instErr != nil {
				compiled.Close(ctx)
				return nil, instErr
			}
			return bridgeMod, nil
		})
		if err != nil {
			return instError("global_bridge", instanceIdx, importModName, "synthetic bridge creation failed", err)
		}

		// Track only if we created the module
		if synthCreated {
			inst.bridgeModules[importModName] = true
			inst.pre.linker.addBridgeRefs(map[string]bool{importModName: true})
		}
	}

	return nil
}

// safeGetExportedFunction wraps ExportedFunction with panic recovery
// for wazero wazevo engine issues with re-exported imports.
func (inst *Instance) safeGetExportedFunction(mod api.Module, name string) (fn api.Function) {
	defer func() {
		if r := recover(); r != nil {
			// Function not accessible - likely a re-exported import issue in wazevo.
			// This is expected for some modules and not an error condition.
			Logger().Debug("safeGetExportedFunction: recovered panic for function lookup",
				zap.String("name", name),
				zap.Any("panic", r))
			fn = nil
		}
	}()
	return mod.ExportedFunction(name)
}

// findImportSource finds the source of a re-exported import.
func (inst *Instance) findImportSource(moduleInstanceIdx int, exportName string) EntitySource {
	if inst.pre == nil || inst.pre.component == nil || inst.pre.component.Raw == nil {
		return nil
	}
	comp := inst.pre.component.Raw

	// Get the module instance's parsed info to find its args
	if moduleInstanceIdx >= len(comp.CoreInstances) {
		return nil
	}
	ci := comp.CoreInstances[moduleInstanceIdx]
	if ci.Parsed == nil || ci.Parsed.Kind != component.CoreInstanceInstantiate {
		return nil
	}

	// Search through the module's args to find a virtual instance that has this function
	for _, arg := range ci.Parsed.Args {
		if arg.Kind != component.CoreInstantiateInstance {
			continue
		}
		argCI := inst.coreInstances[int(arg.InstanceIndex)]
		if argCI == nil || argCI.virtual == nil {
			continue
		}

		// Check if this virtual instance has the export
		// Export names might include module prefix (like "wasi_snapshot_preview1:fd_write")
		// but virtual instance stores them without prefix (like "fd_write")
		lookupName := exportName
		if idx := strings.Index(exportName, ":"); idx >= 0 {
			// Try without prefix first
			lookupName = exportName[idx+1:]
		}

		if e, ok := argCI.virtual.Entities()[lookupName]; ok && e.Source != nil {
			return e.Source
		}
		// Also try the original name in case it's stored with prefix
		if lookupName != exportName {
			if e, ok := argCI.virtual.Entities()[exportName]; ok && e.Source != nil {
				return e.Source
			}
		}
	}

	return nil
}

// collectVirtualExports extracts function exports from a virtual instance.
func (inst *Instance) collectVirtualExports(virt *VirtualInstance, namespace string) []bridge.Export {
	// Use insertion order from the component binary to preserve table indices.
	// The adapter module uses call_indirect with indices matching this order.
	funcNames := virt.OrderedFuncNames()

	var exports []bridge.Export
	for _, entityName := range funcNames {
		entity := virt.Entities()[entityName]

		if entity.Source == nil {
			continue
		}

		switch src := entity.Source.(type) {
		case ModuleExport:
			if src.Module == nil {
				continue
			}
			defs := src.Module.ExportedFunctionDefinitions()
			if defs == nil {
				continue
			}
			def := defs[src.ExportName]
			if def == nil {
				continue
			}
			// Get function - may panic for re-exported imports with wazevo engine
			fn := inst.safeGetExportedFunction(src.Module, src.ExportName)
			if fn != nil {
				wrapper := bridge.ForwardingWrapper(fn, len(def.ParamTypes()))
				if wrapper != nil {
					exports = append(exports, bridge.Export{
						Name:        entityName,
						Fn:          wrapper,
						ParamTypes:  def.ParamTypes(),
						ResultTypes: def.ResultTypes(),
					})
				}
			} else if inst.pre.depGraph != nil && inst.pre.depGraph.IsRequiredFromHost(namespace, entityName) {
				// Function not accessible (likely re-exported import)
				// Look up host function from linker
				hostDef := inst.pre.linker.Resolve(namespace + "#" + entityName)
				if hostDef != nil {
					handler := createSharedMemoryHandler(hostDef)
					exports = append(exports, bridge.Export{
						Name:        entityName,
						Fn:          handler,
						ParamTypes:  hostDef.ParamTypes,
						ResultTypes: hostDef.ResultTypes,
					})
				}
			}

		case HostFunc:
			if src.Def == nil {
				continue
			}
			// Create wrapper that resolves memory at call time via registry lookup
			handler := createSharedMemoryHandlerFromDef(src.Def)
			exports = append(exports, bridge.Export{
				Name:        entityName,
				Fn:          handler,
				ParamTypes:  src.Def.GetParamTypes(),
				ResultTypes: src.Def.GetResultTypes(),
			})

		case BoundHostFunc:
			if src.Def == nil {
				continue
			}
			// Create handler using registry lookup (same as shared handler)
			boundHandler := createBoundHandlerFromDef(src.Def)
			exports = append(exports, bridge.Export{
				Name:        entityName,
				Fn:          boundHandler,
				ParamTypes:  src.Def.GetParamTypes(),
				ResultTypes: src.Def.GetResultTypes(),
			})

		case TrapFunc:
			reason := src.Reason
			name := entityName
			// Resource-drop stubs are no-ops (adapter handles lifecycle)
			if strings.HasPrefix(entityName, "[resource-drop]") {
				exports = append(exports, bridge.Export{
					Name: entityName,
					Fn: api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
					}),
					ParamTypes:  []api.ValueType{api.ValueTypeI32},
					ResultTypes: nil,
				})
				continue
			}
			// Unresolved host function: panics with descriptive message when called.
			// The import is needed by the adapter but no host registered it.
			exports = append(exports, bridge.Export{
				Name: entityName,
				Fn: api.GoModuleFunc(func(_ context.Context, mod api.Module, _ []uint64) {
					panic(fmt.Sprintf("unresolved host import %q: %s", name, reason))
				}),
				ParamTypes:  nil,
				ResultTypes: nil,
			})
		}
	}
	return exports
}

// createVirtualInstance creates a virtual instance from exports.
func (inst *Instance) createVirtualInstance(
	instanceIdx int,
	parsed *component.ParsedCoreInstance,
) *VirtualInstance {
	virt := NewVirtualInstance(fmt.Sprintf("$%d", instanceIdx))

	comp := inst.pre.component.Raw

	// Pre-compute entity spaces once (avoid repeated calls in loop)
	var memSpace, globalSpace, tableSpace []coreEntityEntry
	if comp != nil {
		memSpace = buildCoreEntitySpace(comp, 0x02)
		globalSpace = buildCoreEntitySpace(comp, 0x03)
		tableSpace = buildCoreEntitySpace(comp, 0x01) // 0x01 = table
	}

	for _, exp := range parsed.Exports {
		kind := coreExportKindToEntity(exp.Kind)
		entity := Entity{Kind: kind}

		switch exp.Kind {
		case component.CoreExportFunc:
			// Use CoreFuncIndexSpace to resolve core function references
			if comp != nil {
				idx := int(exp.Index)
				if idx < len(comp.CoreFuncIndexSpace) {
					coreEntry := comp.CoreFuncIndexSpace[idx]
					switch coreEntry.Kind {
					case component.CoreFuncAliasExport:
						if coreEntry.InstanceIdx >= len(inst.coreInstances) {
							continue
						}
						ci := inst.coreInstances[coreEntry.InstanceIdx]
						if ci != nil {
							// Prefer virtual instance if available (handles re-exported imports correctly)
							if ci.virtual != nil {
								if e, ok := ci.virtual.Entities()[coreEntry.ExportName]; ok && e.Source != nil {
									entity.Source = e.Source
								}
							} else if ci.module != nil {
								// Check if function is safely accessible (not a re-exported import)
								if fn := inst.safeGetExportedFunction(ci.module, coreEntry.ExportName); fn != nil {
									entity.Source = ModuleExport{
										Module:     ci.module,
										ExportName: coreEntry.ExportName,
									}
								} else {
									// Function not accessible - try to find it from module's import sources
									entity.Source = inst.findImportSource(coreEntry.InstanceIdx, coreEntry.ExportName)
								}
							}
						}
					case component.CoreFuncResourceDrop:
						// Resource.drop - find the function in any import namespace
						// The export name is like "[resource-drop]pollable"
						def := inst.resolveResourceDropFunc(exp.Name)
						if def != nil {
							entity.Source = HostFunc{Def: def}
						} else {
							// Resource drops without host registration use a no-op stub.
							// The adapter module handles resource lifecycle internally.
							entity.Source = TrapFunc{
								Name:   exp.Name,
								Reason: "resource-drop function not registered",
							}
						}
					case component.CoreFuncCanonLower:
						// Canon lower - find the host function being lowered
						compFuncIdx := coreEntry.FuncIndex
						if def := inst.resolveCanonLowerFunc(compFuncIdx); def != nil {
							// Get bound memory from MemoryIdx
							var boundMem api.Memory
							memIdx := int(coreEntry.MemoryIdx)
							if memIdx >= 0 && memIdx < len(memSpace) {
								entry := memSpace[memIdx]
								if ci := inst.coreInstances[entry.instanceIdx]; ci != nil && ci.module != nil {
									if mem := ci.module.Memory(); isValidMemory(mem) {
										boundMem = mem
									}
								}
							}
							// Fallback: first module with memory
							if !isValidMemory(boundMem) {
								for _, mod := range inst.modules {
									if mem := mod.Memory(); isValidMemory(mem) {
										boundMem = mem
										break
									}
								}
							}

							// Get bound realloc function from ReallocIdx
							var boundRealloc api.Function
							reallocIdx := int(coreEntry.ReallocIdx)
							if reallocIdx >= 0 && reallocIdx < len(comp.CoreFuncIndexSpace) {
								reallocEntry := comp.CoreFuncIndexSpace[reallocIdx]
								if reallocEntry.Kind == component.CoreFuncAliasExport && reallocEntry.InstanceIdx < len(inst.coreInstances) {
									ci := inst.coreInstances[reallocEntry.InstanceIdx]
									if ci != nil && ci.module != nil {
										boundRealloc = ci.module.ExportedFunction(reallocEntry.ExportName)
									}
								}
							}

							if isValidMemory(boundMem) {
								entity.Source = BoundHostFunc{
									Def:       def,
									Memory:    boundMem,
									Allocator: boundRealloc,
								}
							} else {
								entity.Source = HostFunc{Def: def}
							}
						} else {
							// Build descriptive path for error reporting
							path := exp.Name
							if int(compFuncIdx) < len(comp.FuncIndexSpace) {
								entry := comp.FuncIndexSpace[compFuncIdx]
								if int(entry.InstanceIdx) < len(comp.Imports) {
									imp := comp.Imports[entry.InstanceIdx]
									path = imp.Name + "#" + entry.ExportName
								}
							}
							entity.Source = TrapFunc{
								Name:   exp.Name,
								Reason: path + ": host function not registered",
							}
						}
					}
				}
			}

		case component.CoreExportMemory:
			// Memory exports reference core memory index space
			idx := int(exp.Index)
			if idx < len(memSpace) {
				entry := memSpace[idx]
				ci := inst.coreInstances[entry.instanceIdx]
				if ci != nil && ci.module != nil {
					if mem := ci.module.Memory(); mem != nil {
						entity.Source = DirectMemory{
							Memory:       mem,
							SourceModule: ci.module,
							SourceExport: entry.exportName,
						}
					}
				}
			}
			// Fallback: use first module with memory (search coreInstances for proper source tracking)
			if entity.Source == nil {
				for _, ci := range inst.coreInstances {
					if ci == nil || ci.module == nil {
						continue
					}
					if mem := ci.module.Memory(); mem != nil {
						// Find the export name for this memory
						var exportName string
						for expName := range ci.module.ExportedMemoryDefinitions() {
							exportName = expName
							break
						}
						entity.Source = DirectMemory{
							Memory:       mem,
							SourceModule: ci.module,
							SourceExport: exportName,
						}
						break
					}
				}
			}

		case component.CoreExportGlobal:
			// Global exports reference core global index space
			idx := int(exp.Index)
			var global api.Global
			if idx < len(globalSpace) {
				entry := globalSpace[idx]
				if ci := inst.coreInstances[entry.instanceIdx]; ci != nil && ci.module != nil {
					if g := ci.module.ExportedGlobal(entry.exportName); g != nil {
						global = g
						entity.Source = DirectGlobal{
							Global:       g,
							SourceModule: ci.module,
							SourceExport: entry.exportName,
						}
					}
				}
			}
			// Fallback: search by export name
			if entity.Source == nil {
				for _, mod := range inst.modules {
					if g := mod.ExportedGlobal(exp.Name); g != nil {
						global = g
						entity.Source = DirectGlobal{
							Global:       g,
							SourceModule: mod,
							SourceExport: exp.Name,
						}
						break
					}
				}
			}
			// Add global value to value space for start function args
			if global != nil {
				inst.valueSpace = append(inst.valueSpace, global.Get())
			}

		case component.CoreExportTable:
			// Track table source for bridge module creation
			idx := int(exp.Index)
			if idx < len(tableSpace) {
				entry := tableSpace[idx]
				ci := inst.coreInstances[entry.instanceIdx]
				if ci != nil && ci.module != nil {
					entity.Source = TableSource{
						Module:     ci.module,
						ExportName: entry.exportName,
					}
				}
			}
		}

		virt.Define(exp.Name, entity)
	}

	return virt
}

// coreExportKindToEntity converts byte kind to EntityKind
func coreExportKindToEntity(k byte) EntityKind {
	switch k {
	case component.CoreExportFunc:
		return EntityFunc
	case component.CoreExportMemory:
		return EntityMemory
	case component.CoreExportTable:
		return EntityTable
	case component.CoreExportGlobal:
		return EntityGlobal
	default:
		// Unknown kinds are treated as func for forward compatibility,
		// but with no source the entity won't be usable
		return EntityFunc
	}
}

// coreEntityEntry tracks a core entity (memory/table/global) from aliases
type coreEntityEntry struct {
	exportName  string
	instanceIdx int
}

// buildCoreEntitySpace builds an index space for core entities from aliases
func buildCoreEntitySpace(comp *component.Component, coreSort byte) []coreEntityEntry {
	var entries []coreEntityEntry
	for _, alias := range comp.Aliases {
		if alias.Parsed == nil {
			continue
		}
		// Sort=0x00 is core, and CoreSort matches the entity type
		if alias.Parsed.Sort == 0x00 && alias.Parsed.CoreSort == coreSort {
			entries = append(entries, coreEntityEntry{
				instanceIdx: int(alias.Parsed.Instance),
				exportName:  alias.Parsed.Name,
			})
		}
	}
	return entries
}

// resolveCanonLowerFunc resolves a canon lower function to its host implementation.
// Component function index -> component function (import alias) -> host function
func (inst *Instance) resolveCanonLowerFunc(compFuncIdx uint32) *FuncDef {
	comp := inst.pre.component.Raw
	if comp == nil {
		return nil
	}

	// Component functions can come from:
	// 1. Import aliases (alias export from imported instance)
	// 2. Canon lifts (not applicable here)

	// Look at FuncIndexSpace to find the function source
	if int(compFuncIdx) >= len(comp.FuncIndexSpace) {
		return nil
	}
	entry := comp.FuncIndexSpace[compFuncIdx]

	// For import aliases: InstanceIdx is the import index, ExportName is the func name
	if int(entry.InstanceIdx) < len(comp.Imports) {
		imp := comp.Imports[entry.InstanceIdx]
		// imp.Name is the import path like "test:strings/host@0.1.0"
		// entry.ExportName is the function name like "log"
		path := imp.Name + "#" + entry.ExportName
		return inst.pre.linker.Resolve(path)
	}

	return nil
}

// resolveResourceDropFunc searches all imports for a resource-drop function.
// The funcName is like "[resource-drop]pollable".
func (inst *Instance) resolveResourceDropFunc(funcName string) *FuncDef {
	comp := inst.pre.component.Raw
	if comp == nil {
		return nil
	}

	// Search through all imports to find one that has this function
	for _, imp := range comp.Imports {
		path := imp.Name + "#" + funcName
		if def := inst.pre.linker.Resolve(path); def != nil {
			return def
		}
	}

	return nil
}

// buildExports populates the exports map from component exports.
func (inst *Instance) buildExports() {
	if inst.pre.component == nil || inst.pre.component.Raw == nil {
		return
	}

	comp := inst.pre.component.Raw

	// Use pre-computed function sources from InstancePre
	compFuncSources := inst.pre.compFuncSources
	policy := buildExportNamePolicy(comp)

	// Build exports by resolving through the component func index space
	for _, exp := range comp.Exports {
		export := Export{Name: exp.Name}

		if exp.Sort == 0x01 {
			// Resolve the function and canon info, following re-export chains if needed
			export.CoreFunc, export.Canon = inst.resolveCompFuncWithCanon(comp, compFuncSources, exp.SortIndex)
		}

		inst.exports[exp.Name] = export
	}

	// Add canon-lifted function aliases that correspond to declared component
	// exports. This covers instance exports that are flattened to runtime names
	// such as "<instance-export>#<method>".
	for funcIdx, info := range inst.pre.canonLifts {
		name := compAliasExportName(comp, info.CoreFuncIndex)
		if name == "" || !policy.allows(name) {
			continue
		}
		if _, exists := inst.exports[name]; exists {
			continue
		}

		coreFunc, canon := inst.resolveCompFuncWithCanon(comp, compFuncSources, funcIdx)
		if coreFunc == nil {
			continue
		}
		inst.exports[name] = Export{
			Name:     name,
			CoreFunc: coreFunc,
			Canon:    canon,
		}
	}
}

type exportNamePolicy struct {
	direct           map[string]struct{}
	instancePrefixes []string
}

func buildExportNamePolicy(comp *component.Component) exportNamePolicy {
	policy := exportNamePolicy{
		direct:           make(map[string]struct{}),
		instancePrefixes: make([]string, 0, len(comp.Exports)),
	}

	for _, exp := range comp.Exports {
		switch exp.Sort {
		case component.SortFunc:
			policy.direct[exp.Name] = struct{}{}
		case component.SortInstance:
			if exp.Name == "" {
				continue
			}
			policy.instancePrefixes = append(policy.instancePrefixes, exp.Name+"#")
		}
	}

	return policy
}

func (p exportNamePolicy) allows(name string) bool {
	if _, ok := p.direct[name]; ok {
		return true
	}

	for _, prefix := range p.instancePrefixes {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		// Require method name after prefix.
		if len(name) > len(prefix) {
			return true
		}
	}

	return false
}

func compAliasExportName(comp *component.Component, coreFuncIdx uint32) string {
	if int(coreFuncIdx) >= len(comp.CoreFuncIndexSpace) {
		return ""
	}
	entry := comp.CoreFuncIndexSpace[coreFuncIdx]
	if entry.Kind != component.CoreFuncAliasExport {
		return ""
	}
	return entry.ExportName
}

// resolveCompFuncWithCanon resolves a component function to a core function and canon options.
func (inst *Instance) resolveCompFuncWithCanon(
	comp *component.Component,
	sources map[uint32]compFuncSource,
	funcIdx uint32,
) (api.Function, *CanonExport) {
	// Track the funcIdx for canon resolution after following re-export chains
	var liftFuncIdx uint32

	// Follow re-export chains to find the underlying lift
	var visitedStack [8]uint32
	var visitedMap map[uint32]bool
	visitedCount := 0

	for i := 0; i < maxReExportDepth; i++ {
		// Cycle detection
		if visitedMap != nil {
			if visitedMap[funcIdx] {
				return nil, nil
			}
			visitedMap[funcIdx] = true
		} else {
			for j := 0; j < visitedCount; j++ {
				if visitedStack[j] == funcIdx {
					return nil, nil
				}
			}
			if visitedCount < len(visitedStack) {
				visitedStack[visitedCount] = funcIdx
				visitedCount++
			} else {
				visitedMap = make(map[uint32]bool, len(visitedStack)+1)
				for j := 0; j < len(visitedStack); j++ {
					visitedMap[visitedStack[j]] = true
				}
				visitedMap[funcIdx] = true
			}
		}

		src, ok := sources[funcIdx]
		if !ok {
			return nil, nil
		}

		switch src.kind {
		case compFuncLift:
			liftFuncIdx = funcIdx
			coreFunc := inst.getCoreFunc(comp, int(src.coreFunc))
			canon := inst.resolveCanon(liftFuncIdx)
			return coreFunc, canon

		case compFuncAlias:
			if int(funcIdx) < len(comp.FuncIndexSpace) {
				entry := comp.FuncIndexSpace[funcIdx]
				if ci := inst.coreInstances[int(entry.InstanceIdx)]; ci != nil && ci.module != nil {
					return ci.module.ExportedFunction(entry.ExportName), nil
				}
			}
			return nil, nil

		case compFuncExport:
			funcIdx = src.reExportOf
			continue
		}

		return nil, nil
	}
	return nil, nil
}

// resolveCanon builds CanonExport from pre-parsed canonLiftInfo.
func (inst *Instance) resolveCanon(funcIdx uint32) *CanonExport {
	if inst.pre.canonLifts == nil || inst.pre.typeResolver == nil {
		return nil
	}

	info, ok := inst.pre.canonLifts[funcIdx]
	if !ok {
		return nil
	}

	// Resolve function type to wit.Type
	comp := inst.pre.component.Raw
	if int(info.TypeIndex) >= len(comp.TypeIndexSpace) {
		return nil
	}

	funcType, ok := comp.TypeIndexSpace[info.TypeIndex].(*component.FuncType)
	if !ok {
		return nil
	}

	paramTypes, resultType, err := inst.pre.typeResolver.ResolveFunc(funcType)
	if err != nil {
		return nil
	}

	var resultTypes []wit.Type
	if resultType != nil {
		resultTypes = []wit.Type{resultType}
	}

	canon := &CanonExport{
		ParamTypes:  paramTypes,
		ResultTypes: resultTypes,
		Encoding:    info.Encoding,
	}

	// Resolve memory
	canon.Memory = inst.Memory()

	// Resolve realloc
	if info.ReallocIndex >= 0 {
		canon.Realloc = inst.getCoreFunc(comp, int(info.ReallocIndex))
	} else {
		// Fall back to searching for common allocator names
		canon.Realloc = inst.Allocator()
	}

	// Resolve post-return
	if info.PostReturnIndex >= 0 {
		canon.PostReturn = inst.getCoreFunc(comp, int(info.PostReturnIndex))
	}

	return canon
}

// resolveCompFunc resolves a component function index to a core function.
func (inst *Instance) resolveCompFunc(
	comp *component.Component,
	sources map[uint32]compFuncSource,
	funcIdx uint32,
) api.Function {
	// Use stack-allocated array for cycle detection (most chains are short)
	// Falls back to map if chain exceeds array size
	var visitedStack [8]uint32
	var visitedMap map[uint32]bool // lazily allocated if needed
	visitedCount := 0

	for i := 0; i < maxReExportDepth; i++ {
		// Check for cycle
		if visitedMap != nil {
			if visitedMap[funcIdx] {
				return nil // cycle detected
			}
			visitedMap[funcIdx] = true
		} else {
			// Linear scan for small chains
			for j := 0; j < visitedCount; j++ {
				if visitedStack[j] == funcIdx {
					return nil // cycle detected
				}
			}
			if visitedCount < len(visitedStack) {
				visitedStack[visitedCount] = funcIdx
				visitedCount++
			} else {
				// Overflow: migrate to map
				visitedMap = make(map[uint32]bool, len(visitedStack)+1)
				for j := 0; j < len(visitedStack); j++ {
					visitedMap[visitedStack[j]] = true
				}
				visitedMap[funcIdx] = true
			}
		}

		src, ok := sources[funcIdx]
		if !ok {
			return nil
		}

		switch src.kind {
		case compFuncLift:
			return inst.getCoreFunc(comp, int(src.coreFunc))

		case compFuncAlias:
			if int(funcIdx) < len(comp.FuncIndexSpace) {
				entry := comp.FuncIndexSpace[funcIdx]
				if ci := inst.coreInstances[int(entry.InstanceIdx)]; ci != nil && ci.module != nil {
					return ci.module.ExportedFunction(entry.ExportName)
				}
			}
			return nil

		case compFuncExport:
			// Follow the re-export chain
			funcIdx = src.reExportOf
			continue
		}

		return nil
	}
	return nil
}

// callStart invokes the component's start function if defined.
func (inst *Instance) callStart(ctx context.Context) error {
	if inst.pre.component == nil || inst.pre.component.Raw == nil {
		return nil
	}

	comp := inst.pre.component.Raw
	if comp.Start == nil {
		return nil
	}

	// Resolve the component function at FuncIndex
	funcIdx := comp.Start.FuncIndex
	if int(funcIdx) >= len(comp.FuncIndexSpace) {
		return instError("start", -1, "",
			fmt.Sprintf("function index %d out of range (have %d)", funcIdx, len(comp.FuncIndexSpace)), nil)
	}

	entry := comp.FuncIndexSpace[funcIdx]
	if int(entry.InstanceIdx) >= len(inst.coreInstances) {
		return instError("start", int(entry.InstanceIdx), entry.ExportName,
			"instance index out of range", nil)
	}
	ci := inst.coreInstances[int(entry.InstanceIdx)]
	if ci == nil || ci.module == nil {
		return instError("start", int(entry.InstanceIdx), entry.ExportName,
			"instance not found or not a module", nil)
	}

	fn := ci.module.ExportedFunction(entry.ExportName)
	if fn == nil {
		return instError("start", int(entry.InstanceIdx), entry.ExportName,
			"function not exported", nil)
	}

	// Resolve start args from value space
	var args []uint64
	if len(comp.Start.Args) > 0 {
		args = make([]uint64, len(comp.Start.Args))
		for i, valIdx := range comp.Start.Args {
			if int(valIdx) >= len(inst.valueSpace) {
				return instError("start", -1, "",
					fmt.Sprintf("value index %d out of range (have %d values)", valIdx, len(inst.valueSpace)), nil)
			}
			args[i] = inst.valueSpace[valIdx]
		}
	}

	// Attach instance to context for host handler lookup (needed for shim modules)
	ctx = WithInstance(ctx, inst)

	_, err := fn.Call(ctx, args...)
	if err != nil {
		return instError("start", int(entry.InstanceIdx), entry.ExportName, "execution failed", err)
	}
	return nil
}

// getCoreFunc retrieves a core function by its index.
func (inst *Instance) getCoreFunc(comp *component.Component, idx int) api.Function {
	if idx >= len(comp.CoreFuncIndexSpace) {
		return nil
	}

	entry := comp.CoreFuncIndexSpace[idx]
	switch entry.Kind {
	case component.CoreFuncAliasExport:
		// Core func is an alias from a core instance export
		if entry.InstanceIdx >= len(inst.coreInstances) {
			return nil
		}
		ci := inst.coreInstances[entry.InstanceIdx]
		if ci == nil || ci.module == nil {
			return nil
		}
		return inst.safeGetExportedFunction(ci.module, entry.ExportName)

	case component.CoreFuncCanonLower:
		// Core func is from canon lower - these are provided by host bindings
		// and shouldn't be directly called through exports
		return nil

	default:
		return nil
	}
}

// GetExport returns a component export by name.
func (inst *Instance) GetExport(name string) (Export, bool) {
	exp, ok := inst.exports[name]
	return exp, ok
}

// Call invokes an exported function with Canonical ABI encoding/decoding.
func (inst *Instance) Call(ctx context.Context, name string, args ...any) ([]any, error) {
	exp, ok := inst.exports[name]
	if !ok {
		return nil, fmt.Errorf("linker: export %q not found", name)
	}

	if exp.CoreFunc == nil {
		return nil, fmt.Errorf("linker: export %q has no core function", name)
	}

	// Attach instance to context for host handler lookup (needed for shim modules)
	ctx = WithInstance(ctx, inst)

	// If no canon info, fall back to raw call with type coercion
	if exp.Canon == nil {
		return inst.callRawWithCoercion(ctx, exp.CoreFunc, args)
	}

	// Encode parameters using Canonical ABI
	var flatParams []uint64
	if len(exp.Canon.ParamTypes) > 0 {
		if len(args) != len(exp.Canon.ParamTypes) {
			return nil, fmt.Errorf("linker: parameter count mismatch: expected %d, got %d",
				len(exp.Canon.ParamTypes), len(args))
		}

		mem := inst.wrapMemory(exp.Canon.Memory)
		alloc := inst.wrapAllocator(ctx, exp.Canon.Realloc)

		var err error
		flatParams, err = inst.encoder.EncodeParams(exp.Canon.ParamTypes, args, mem, alloc, nil)
		if err != nil {
			return nil, fmt.Errorf("encode params: %w", err)
		}
	}

	// Check if result uses retptr pattern (more than 1 flat value for result)
	usesRetptr := invoke.UsesRetptr(exp.Canon.ResultTypes)

	// Call the core function
	flatResults, err := exp.CoreFunc.Call(ctx, flatParams...)
	if err != nil {
		return nil, err
	}

	// Decode results using Canonical ABI
	var results []any
	if len(exp.Canon.ResultTypes) > 0 {
		mem := inst.wrapMemory(exp.Canon.Memory)

		if usesRetptr && len(flatResults) == 1 {
			// Result was returned via pointer - load from memory
			basePtr := uint32(flatResults[0])
			offset := uint32(0)
			results = make([]any, len(exp.Canon.ResultTypes))
			for i, rt := range exp.Canon.ResultTypes {
				// Calculate layout for proper alignment
				layout := inst.layoutCalc.Calculate(rt)
				// Align offset to type's alignment requirement
				if layout.Align > 0 {
					offset = (offset + layout.Align - 1) &^ (layout.Align - 1)
				}
				val, err := inst.decoder.LoadValue(rt, basePtr+offset, mem)
				if err != nil {
					return nil, fmt.Errorf("load result[%d]: %w", i, err)
				}
				results[i] = val
				offset += layout.Size
			}
		} else {
			// Results returned as flat values
			results, err = inst.decoder.DecodeResults(exp.Canon.ResultTypes, flatResults, mem)
			if err != nil {
				return nil, fmt.Errorf("decode results: %w", err)
			}
		}
	}

	// Call post-return for cleanup if defined
	if exp.Canon.PostReturn != nil {
		// Post-return errors are informational but don't fail the call
		// Results are already valid at this point
		_, _ = exp.Canon.PostReturn.Call(ctx, flatResults...)
	}

	return results, nil
}

// CallRaw invokes an exported function with raw uint64 values, no ABI encoding.
func (inst *Instance) CallRaw(ctx context.Context, name string, args ...uint64) ([]uint64, error) {
	exp, ok := inst.exports[name]
	if !ok {
		return nil, fmt.Errorf("linker: export %q not found", name)
	}

	if exp.CoreFunc == nil {
		return nil, fmt.Errorf("linker: export %q has no core function", name)
	}

	// Attach instance to context for host handler lookup (needed for shim modules)
	ctx = WithInstance(ctx, inst)

	return exp.CoreFunc.Call(ctx, args...)
}

// callRawWithCoercion calls a function without canon info, coercing args to uint64.
func (inst *Instance) callRawWithCoercion(ctx context.Context, fn api.Function, args []any) ([]any, error) {
	flatArgs, err := invoke.CoerceArgs(args)
	if err != nil {
		return nil, err
	}

	results, err := fn.Call(ctx, flatArgs...)
	if err != nil {
		return nil, err
	}

	return invoke.ResultsToAny(results), nil
}

// ExportedFunction returns an exported function by name, or nil if not found.
func (inst *Instance) ExportedFunction(name string) api.Function {
	if exp, ok := inst.exports[name]; ok {
		return exp.CoreFunc
	}
	return nil
}

// Modules returns the instantiated core modules.
func (inst *Instance) Modules() []api.Module {
	return inst.modules
}

// Resources returns the resource store.
func (inst *Instance) Resources() *ResourceStore {
	return inst.resources
}

// Memory returns the first exported memory, or nil if none.
func (inst *Instance) Memory() api.Memory {
	for _, mod := range inst.modules {
		if mod == nil {
			continue
		}
		if mem := mod.Memory(); isValidMemory(mem) {
			return mem
		}
	}
	return nil
}

var allocatorNames = []string{"cabi_realloc", "canonical_abi_realloc", "alloc", "malloc"}
var freeNames = []string{"cabi_free", "canonical_abi_free", "free", "deallocate"}

// findExportByNames searches all modules for a function matching any given name.
func (inst *Instance) findExportByNames(names []string) api.Function {
	for _, mod := range inst.modules {
		if mod == nil {
			continue
		}
		for _, name := range names {
			if fn := inst.safeGetExportedFunction(mod, name); fn != nil {
				return fn
			}
		}
	}
	return nil
}

// Allocator returns the allocator function from the component.
// Allocator searches for: cabi_realloc, canonical_abi_realloc, alloc, malloc
func (inst *Instance) Allocator() api.Function {
	return inst.findExportByNames(allocatorNames)
}

// Free returns the free function from the component.
// Free searches for: cabi_free, canonical_abi_free, free, deallocate
func (inst *Instance) Free() api.Function {
	return inst.findExportByNames(freeNames)
}

// GetModule returns the module at the given core instance index.
// GetModule returns nil if index is out of range, not instantiated, or is a virtual instance.
func (inst *Instance) GetModule(instanceIndex int) api.Module {
	if ci := inst.coreInstances[instanceIndex]; ci != nil {
		return ci.module
	}
	return nil
}

// Graph returns the instance graph for inspection.
// Graph may be nil for simple components.
func (inst *Instance) Graph() *component.InstanceGraph {
	if inst.pre == nil {
		return nil
	}
	return inst.pre.graph
}

// Close releases all instance resources
func (inst *Instance) Close(ctx context.Context) error {
	// Unregister from instance registry
	instanceRegistry.Delete(inst.instanceID)

	// Close core modules (instance-specific, scoped by instanceID)
	var firstErr error
	for _, mod := range inst.modules {
		if mod != nil {
			if err := mod.Close(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	// Handle bridge cleanup - always use ref counting since refs are added immediately
	if inst.pre != nil && inst.pre.linker != nil && len(inst.bridgeModules) > 0 {
		inst.pre.linker.releaseBridgeRefs(ctx, inst.bridgeModules)
	}

	inst.modules = nil
	inst.exports = nil
	inst.bridgeModules = nil
	inst.coreInstances = nil
	return firstErr
}

// wrapMemory wraps a wazero api.Memory to implement transcoder.Memory
func (inst *Instance) wrapMemory(mem api.Memory) transcoder.Memory {
	return memory.WrapMemory(mem)
}

// wrapAllocator wraps a wazero api.Function to implement transcoder.Allocator
func (inst *Instance) wrapAllocator(ctx context.Context, fn api.Function) transcoder.Allocator {
	return memory.WrapAllocator(ctx, fn)
}

// Type aliases for internal types used in tests
type memoryWrapper = memory.Wrapper
type allocatorWrapper = memory.AllocatorWrapper
