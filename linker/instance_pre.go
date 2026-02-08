package linker

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/linker/internal/graph"
	"go.uber.org/zap"
)

// InstancePre represents a pre-validated, pre-compiled component ready for instantiation.
// InstancePre is created once with Linker.Instantiate, then NewInstance is called multiple times.
//
// It is thread-safe: NewInstance can be called concurrently from multiple goroutines.
// Each call creates an independent Instance with its own module instances.
type InstancePre struct {
	hostModuleBindings  map[string][]resolvedBinding
	component           *component.ValidatedComponent
	expectedGlobalTypes map[string]map[string]GlobalImport
	linker              *Linker
	graph               *component.InstanceGraph
	depGraph            *graph.Graph
	expectedFuncTypes   map[string]map[string]importSig
	compFuncSources     map[uint32]compFuncSource
	canonLifts          map[uint32]*canonLiftInfo
	typeResolver        *component.TypeResolver
	bindings            []resolvedBinding
	topoOrder           []int
	compiled            []wazero.CompiledModule
	numExports          int
	numInstances        int
}

// canonLiftInfo holds pre-parsed canonical lift information
type canonLiftInfo struct {
	CoreFuncIndex   uint32 // core function being lifted
	TypeIndex       uint32 // component type index
	MemoryIndex     uint32 // memory index (0 = default)
	ReallocIndex    int32  // realloc core func index (-1 = not specified)
	PostReturnIndex int32  // post-return core func index (-1 = not specified)
	Encoding        byte   // string encoding
}

// resolvedBinding describes a resolved import binding
type resolvedBinding struct {
	FuncDef       *FuncDef
	ModuleName    string
	ImportName    string
	ParamTypes    []api.ValueType
	ResultTypes   []api.ValueType
	InstanceIndex int
	Trap          bool
}

// Instantiate validates and compiles a component, returning an InstancePre.
// Instantiate is the expensive phase - call once then reuse.
func (l *Linker) Instantiate(ctx context.Context, c *component.ValidatedComponent) (*InstancePre, error) {
	if c == nil || c.Raw == nil {
		return nil, instError("validate", -1, "", "nil component", nil)
	}

	pre := &InstancePre{
		linker:    l,
		component: c,
	}

	// Compile all core modules (CoreModules is [][]byte)
	for i, modBytes := range c.Raw.CoreModules {
		// Rewrite empty module names in imports (wazero doesn't allow them)
		modBytes = rewriteEmptyModuleNames(modBytes)

		// Apply asyncify transform if enabled and module isn't already asyncified
		if l.options.AsyncifyTransform && !asyncify.IsAsyncified(modBytes) {
			transformed, err := asyncify.Transform(modBytes, asyncify.Config{
				AsyncImports: l.options.AsyncifyImports,
			})
			if err != nil {
				for j, cm := range pre.compiled {
					if closeErr := cm.Close(ctx); closeErr != nil {
						Logger().Warn("failed to close compiled module during cleanup",
							zap.Int("module_index", j),
							zap.Error(closeErr))
					}
				}
				return nil, instError("compile", i, "", "asyncify transform failed", err)
			}
			modBytes = transformed
		}

		compiled, err := l.runtime.CompileModule(ctx, modBytes)
		if err != nil {
			// Clean up already-compiled modules before returning error
			for j, cm := range pre.compiled {
				if closeErr := cm.Close(ctx); closeErr != nil {
					Logger().Warn("failed to close compiled module during cleanup",
						zap.Int("module_index", j),
						zap.Error(closeErr))
				}
			}
			return nil, instError("compile", i, "", "module compilation failed", err)
		}
		pre.compiled = append(pre.compiled, compiled)
	}

	// Build instance graph if we have core instances
	// Pass the full component to track FromExports dependencies via core index spaces
	if len(c.Raw.CoreInstances) > 0 {
		pre.graph = component.NewInstanceGraphWithComponent(c.Raw.CoreInstances, c.Raw)

		// Cache topological sort (deterministic, no need to recompute)
		order, err := pre.graph.TopologicalSort()
		if err != nil {
			for j, cm := range pre.compiled {
				if closeErr := cm.Close(ctx); closeErr != nil {
					Logger().Warn("failed to close compiled module during cleanup",
						zap.Int("module_index", j),
						zap.Error(closeErr))
				}
			}
			return nil, instError("compile", -1, "", "dependency cycle in instances", err)
		}
		pre.topoOrder = order
	}

	// Build dependency graph to determine which functions host must provide
	// vs which are provided by adapter modules
	pre.depGraph = graph.Build(c)

	// Pre-resolve import bindings
	if err := pre.resolveBindings(); err != nil {
		for j, cm := range pre.compiled {
			if closeErr := cm.Close(ctx); closeErr != nil {
				Logger().Warn("failed to close compiled module during cleanup",
					zap.Int("module_index", j),
					zap.Error(closeErr))
			}
		}
		return nil, err // already an InstantiationError from resolveBindings
	}

	// Pre-compute component function sources (used in buildExports)
	pre.compFuncSources = pre.buildCompFuncSources()

	// Parse canon lifts for type resolution
	pre.canonLifts = pre.buildCanonLifts()

	// Build type resolver for function signature resolution
	if len(c.Raw.TypeIndexSpace) > 0 {
		pre.typeResolver = component.NewTypeResolverWithInstances(
			c.Raw.TypeIndexSpace,
			c.Raw.InstanceTypes,
		)
	}

	// Store capacity hints
	pre.numInstances = len(c.Raw.CoreInstances)
	pre.numExports = len(c.Raw.Exports)

	// Pre-aggregate bindings by module name (for host module creation)
	pre.hostModuleBindings = pre.aggregateHostModuleBindings()

	// Pre-aggregate expected function signatures from all core modules
	pre.expectedFuncTypes = pre.buildExpectedFuncTypes()

	// Pre-aggregate expected global types from all core modules
	pre.expectedGlobalTypes = pre.buildExpectedGlobalTypes()

	return pre, nil
}

// aggregateHostModuleBindings groups all bindings by module name across all core instances.
// This ensures host modules are created once with ALL needed exports.
// Only includes bindings that have FuncDef or Trap set (resolvable host functions).
// Skips modules that will be provided by Args (referencing other core instances).
func (pre *InstancePre) aggregateHostModuleBindings() map[string][]resolvedBinding {
	if len(pre.bindings) == 0 {
		return nil
	}

	// Collect all module names that are provided by Args (will be bridges, not host modules)
	argsModules := make(map[string]bool)
	if pre.component != nil && pre.component.Raw != nil {
		for _, ci := range pre.component.Raw.CoreInstances {
			if ci.Parsed == nil || ci.Parsed.Kind != component.CoreInstanceInstantiate {
				continue
			}
			for _, arg := range ci.Parsed.Args {
				if arg.Kind == component.CoreInstantiateInstance {
					argName := arg.Name
					if argName == "" {
						argName = EmptyModuleName
					}
					argsModules[argName] = true
				}
			}
		}
	}

	result := make(map[string][]resolvedBinding)
	seen := make(map[string]map[string]bool) // moduleName -> importName -> exists

	for _, binding := range pre.bindings {
		// Only include bindings that can create host functions
		if binding.FuncDef == nil && !binding.Trap {
			continue
		}
		// Skip internal component imports ($#N format)
		if len(binding.ModuleName) > 0 && binding.ModuleName[0] == '$' {
			continue
		}
		// Skip modules that will be provided by Args (these become bridges, not host modules)
		if argsModules[binding.ModuleName] {
			continue
		}
		// Deduplicate: same module+import may appear in multiple core instances
		if seen[binding.ModuleName] == nil {
			seen[binding.ModuleName] = make(map[string]bool)
		}
		if seen[binding.ModuleName][binding.ImportName] {
			continue
		}
		seen[binding.ModuleName][binding.ImportName] = true
		result[binding.ModuleName] = append(result[binding.ModuleName], binding)
	}

	return result
}

// buildExpectedFuncTypes collects expected function signatures from ALL compiled modules.
// This aggregates import types across all core instances so bridge modules can be
// created with signatures that satisfy all consumers.
func (pre *InstancePre) buildExpectedFuncTypes() map[string]map[string]importSig {
	result := make(map[string]map[string]importSig)

	for _, compiled := range pre.compiled {
		imports := compiled.ImportedFunctions()
		for _, fn := range imports {
			modName, funcName, _ := fn.Import()
			if modName == "" {
				modName = EmptyModuleName
			}

			if result[modName] == nil {
				result[modName] = make(map[string]importSig)
			}

			// Store the expected signature from this module's imports
			result[modName][funcName] = importSig{
				params:  fn.ParamTypes(),
				results: fn.ResultTypes(),
			}
		}
	}

	return result
}

// buildExpectedGlobalTypes collects expected global types from ALL core modules.
// Parses raw WASM bytes since wazero doesn't expose ImportedGlobals().
func (pre *InstancePre) buildExpectedGlobalTypes() map[string]map[string]GlobalImport {
	if pre.component == nil || pre.component.Raw == nil {
		return nil
	}

	result := make(map[string]map[string]GlobalImport)

	for _, moduleBytes := range pre.component.Raw.CoreModules {
		globals := parseGlobalImports(moduleBytes)
		for _, g := range globals {
			modName := g.ModuleName
			if modName == "" {
				modName = EmptyModuleName
			}
			if result[modName] == nil {
				result[modName] = make(map[string]GlobalImport)
			}
			result[modName][g.ImportName] = g
		}
	}

	return result
}

// buildCompFuncSources pre-computes the component function index space mapping.
// This is deterministic based on component structure, so we compute once.
func (pre *InstancePre) buildCompFuncSources() map[uint32]compFuncSource {
	if pre.component == nil || pre.component.Raw == nil {
		return nil
	}

	comp := pre.component.Raw
	sources := make(map[uint32]compFuncSource)
	funcIdx := uint32(0)

	for _, marker := range comp.SectionOrder {
		switch marker.Kind {
		case component.SectionAlias:
			for i := marker.StartIndex; i < marker.StartIndex+marker.Count; i++ {
				if i >= len(comp.Aliases) {
					continue
				}
				alias := comp.Aliases[i]
				if alias.Parsed != nil && alias.Parsed.Sort == 0x01 {
					sources[funcIdx] = compFuncSource{kind: compFuncAlias}
					funcIdx++
				}
			}
		case component.SectionCanon:
			for i := marker.StartIndex; i < marker.StartIndex+marker.Count; i++ {
				if i >= len(comp.Canons) {
					continue
				}
				canon := comp.Canons[i]
				if canon.Parsed != nil && canon.Parsed.Kind == component.CanonLift {
					sources[funcIdx] = compFuncSource{
						kind:     compFuncLift,
						coreFunc: canon.Parsed.FuncIndex,
					}
					funcIdx++
				}
			}
		case component.SectionExport:
			for i := marker.StartIndex; i < marker.StartIndex+marker.Count; i++ {
				if i >= len(comp.Exports) {
					continue
				}
				exp := comp.Exports[i]
				if exp.Sort == 0x01 {
					sources[funcIdx] = compFuncSource{
						kind:       compFuncExport,
						reExportOf: exp.SortIndex,
					}
					funcIdx++
				}
			}
		}
	}

	return sources
}

// buildCanonLifts pre-parses canon lift entries to extract canonical options
func (pre *InstancePre) buildCanonLifts() map[uint32]*canonLiftInfo {
	if pre.component == nil || pre.component.Raw == nil {
		return nil
	}

	comp := pre.component.Raw
	lifts := make(map[uint32]*canonLiftInfo)
	funcIdx := uint32(0)

	for _, marker := range comp.SectionOrder {
		switch marker.Kind {
		case component.SectionAlias:
			for i := marker.StartIndex; i < marker.StartIndex+marker.Count; i++ {
				if i >= len(comp.Aliases) {
					continue
				}
				alias := comp.Aliases[i]
				if alias.Parsed != nil && alias.Parsed.Sort == 0x01 {
					funcIdx++
				}
			}
		case component.SectionCanon:
			for i := marker.StartIndex; i < marker.StartIndex+marker.Count; i++ {
				if i >= len(comp.Canons) {
					continue
				}
				canon := comp.Canons[i]
				if canon.Parsed != nil && canon.Parsed.Kind == component.CanonLift {
					info := &canonLiftInfo{
						CoreFuncIndex:   canon.Parsed.FuncIndex,
						TypeIndex:       canon.Parsed.TypeIndex,
						MemoryIndex:     canon.Parsed.GetMemoryIndex(),
						ReallocIndex:    canon.Parsed.GetReallocIndex(),
						PostReturnIndex: -1,
						Encoding:        canon.Parsed.GetStringEncoding(),
					}
					// Check for post-return option
					for _, opt := range canon.Parsed.Options {
						if opt.Kind == component.CanonOptPostReturn {
							info.PostReturnIndex = int32(opt.Index)
							break
						}
					}
					lifts[funcIdx] = info
					funcIdx++
				}
			}
		case component.SectionExport:
			for i := marker.StartIndex; i < marker.StartIndex+marker.Count; i++ {
				if i >= len(comp.Exports) {
					continue
				}
				exp := comp.Exports[i]
				if exp.Sort == 0x01 {
					funcIdx++
				}
			}
		}
	}

	return lifts
}

// resolveBindings pre-resolves all import bindings for the component
func (pre *InstancePre) resolveBindings() error {
	if pre.graph == nil {
		return nil
	}

	moduleInstantiations := pre.graph.ModuleInstantiations()

	for _, mi := range moduleInstantiations {
		if mi.ModuleIndex >= len(pre.compiled) {
			return instError("compile", mi.InstanceIndex, "", "module index out of range", nil)
		}

		// Collect module names provided by Args (other core instances or named modules).
		// These imports are satisfied at instantiation time, not by host functions.
		argsProvided := make(map[string]bool, len(mi.Args))
		for _, arg := range mi.Args {
			name := arg.Name
			if name == "" {
				name = EmptyModuleName
			}
			argsProvided[name] = true
		}

		compiled := pre.compiled[mi.ModuleIndex]
		imports := compiled.ImportedFunctions()

		for _, imp := range imports {
			moduleName, funcName, _ := imp.Import()
			binding := resolvedBinding{
				InstanceIndex: mi.InstanceIndex,
				ModuleName:    moduleName,
				ImportName:    funcName,
				ParamTypes:    imp.ParamTypes(),
				ResultTypes:   imp.ResultTypes(),
			}

			// Try to resolve from linker namespace first
			path := moduleName + "#" + funcName
			def := pre.linker.Resolve(path)
			if def != nil {
				binding.FuncDef = def
			} else if argsProvided[moduleName] {
				// Import satisfied by Args (core instance reference) - will be
				// resolved at instantiation time via bridges.
				binding.Trap = true
			} else {
				return instError("import_resolution", mi.InstanceIndex, path, "unresolved import", nil)
			}

			pre.bindings = append(pre.bindings, binding)
		}
	}

	return nil
}

// CompiledModules returns the compiled modules for inspection
func (pre *InstancePre) CompiledModules() []wazero.CompiledModule {
	return pre.compiled
}

// Component returns the validated component
func (pre *InstancePre) Component() *component.ValidatedComponent {
	return pre.component
}

// Close releases compiled module resources
func (pre *InstancePre) Close(ctx context.Context) error {
	var firstErr error
	for _, cm := range pre.compiled {
		if err := cm.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	pre.compiled = nil
	return firstErr
}

// IsRequiredFromHost checks if a function must be provided by the host.
// IsRequiredFromHost returns true if the function is not provided by an adapter module.
func (pre *InstancePre) IsRequiredFromHost(namespace, funcName string) bool {
	if pre.depGraph == nil {
		return true // No graph - assume all functions come from host
	}
	return pre.depGraph.IsRequiredFromHost(namespace, funcName)
}

// RequiredHostFunctions returns all functions the host must provide.
// RequiredHostFunctions uses key format: "namespace#funcname"
func (pre *InstancePre) RequiredHostFunctions() map[string]bool {
	if pre.depGraph == nil {
		return nil
	}
	return pre.depGraph.RequiredHostFunctions()
}
