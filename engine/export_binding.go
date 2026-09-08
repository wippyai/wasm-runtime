package engine

import (
	"fmt"

	"github.com/tetratelabs/wazero/api"
	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/transcoder"
)

// exportBinding caches resolved execution context for an exported function.
// All fields are immutable once resolved, enabling warm-call lookup with
// zero allocations and no mutex copying.
type exportBinding struct {
	fn          api.Function
	postReturn  api.Function
	coreMod     api.Module
	memory      *WazeroMemory
	alloc       *wazeroAllocator
	asyncify    *Asyncify
	scheduler   *Scheduler
	name        string
	paramTypes  []wit.Type
	resultTypes []wit.Type
	coreModIdx  int
	canonical   bool
}

// isCanonical reports whether this binding represents a Canonical ABI export
// (e.g. created via canon lift) rather than a legacy raw core module WIT export.
func (b *exportBinding) isCanonical() bool {
	return b != nil && b.canonical
}

// coreModName returns a human-readable identifier for the core module.
func (b *exportBinding) coreModName() string {
	if b == nil || b.coreMod == nil {
		return "default"
	}
	name := b.coreMod.Name()
	if name != "" {
		return name
	}
	if b.coreModIdx >= 0 {
		return fmt.Sprintf("core#%d", b.coreModIdx)
	}
	return "core"
}

// asyncifyCoreState stores asyncify runtime and scheduler per core module.
type asyncifyCoreState struct {
	asyncify  *Asyncify
	scheduler *Scheduler
}

// getExportBinding returns the resolved binding for an export name, caching it
// for subsequent warm calls.
func (i *WazeroInstance) getExportBinding(name string) (*exportBinding, error) {
	// Warm call: read from the cache under a shared lock
	i.bindingMu.RLock()
	if b, ok := i.exportBindings[name]; ok {
		i.bindingMu.RUnlock()
		return b, nil
	}
	i.bindingMu.RUnlock()

	// Cold call: resolve and populate cache under write lock
	i.bindingMu.Lock()
	defer i.bindingMu.Unlock()

	// Double-check after acquiring write lock
	if b, ok := i.exportBindings[name]; ok {
		return b, nil
	}

	if i.exportBindings == nil {
		i.exportBindings = make(map[string]*exportBinding)
	}

	// 1. Resolve for multi-module component
	if i.linkerInst != nil {
		return i.resolveComponentExportBindingLocked(name)
	}

	// 2. Resolve for single-module instance
	return i.resolveSingleModuleExportBindingLocked(name)
}

func (i *WazeroInstance) resolveComponentExportBindingLocked(name string) (*exportBinding, error) {
	exp, ok := i.linkerInst.GetExport(name)
	var fn api.Function
	if ok {
		fn = exp.CoreFunc
	}
	if fn == nil {
		fn = i.linkerInst.ExportedFunction(name)
	}
	if fn == nil && i.instance != nil {
		fn = i.instance.ExportedFunction(name)
	}
	if fn == nil {
		return nil, fmt.Errorf("export %q not found", name)
	}

	// Resolve the owning core module and instance index
	var coreMod api.Module
	coreModIdx := -1
	if i.module != nil && i.module.canonRegistry != nil && i.module.validated != nil {
		lift := i.module.canonRegistry.FindLift(name)
		if lift != nil && int(lift.CoreFuncIdx) < len(i.module.validated.Raw.CoreFuncIndexSpace) {
			owner := i.module.validated.Raw.CoreFuncIndexSpace[lift.CoreFuncIdx]
			if owner.Kind == component.CoreFuncAliasExport {
				coreModIdx = owner.InstanceIdx
				coreMod = i.linkerInst.GetModule(coreModIdx)
			}
		}
	}
	if coreMod == nil {
		coreMod = i.instance
	}

	// Resolve Canonical ABI options (Memory, Realloc, PostReturn, WIT types)
	var canonMem *WazeroMemory
	var canonAlloc *wazeroAllocator
	var postReturn api.Function
	var paramTypes, resultTypes []wit.Type

	if exp.Canon != nil {
		if exp.Canon.Memory != nil {
			canonMem = i.getOrCreateMemoryLocked(exp.Canon.Memory)
		}
		if exp.Canon.Realloc != nil {
			allocMod := exp.Canon.ReallocMod
			canonAlloc = i.getOrCreateAllocatorLocked(exp.Canon.Realloc, allocMod)
		}
		postReturn = exp.Canon.PostReturn
		paramTypes = exp.Canon.ParamTypes
		resultTypes = exp.Canon.ResultTypes
	}

	// A canonical binding is authoritative, including absent options. Only
	// raw core exports use the instance's compatibility bindings.
	if exp.Canon == nil {
		canonMem = i.memory
		canonAlloc = i.alloc
		if coreMod != nil {
			postReturn = coreMod.ExportedFunction("cabi_post_" + name)
		}
	}
	if len(paramTypes) == 0 && len(resultTypes) == 0 && i.module != nil && i.module.canonRegistry != nil {
		if lift := i.module.canonRegistry.FindLift(name); lift != nil {
			paramTypes = lift.Params
			resultTypes = lift.Results
		}
	}

	// Resolve Asyncify / Scheduler for this core module
	var async *Asyncify
	var sched *Scheduler
	if i.asyncify != nil || i.asyncifyEnabled {
		var err error
		async, sched, err = i.getOrCreateAsyncifyForModuleLocked(coreMod, coreModIdx)
		if err != nil {
			return nil, fmt.Errorf("initialize asyncify for export %q: %w", name, err)
		}
	}

	canonical := exp.Canon != nil || (i.module != nil && i.module.canonRegistry != nil && i.module.canonRegistry.FindLift(name) != nil)

	binding := &exportBinding{
		name:        name,
		fn:          fn,
		postReturn:  postReturn,
		memory:      canonMem,
		alloc:       canonAlloc,
		paramTypes:  paramTypes,
		resultTypes: resultTypes,
		coreMod:     coreMod,
		coreModIdx:  coreModIdx,
		asyncify:    async,
		scheduler:   sched,
		canonical:   canonical,
	}

	i.exportBindings[name] = binding
	return binding, nil
}

func (i *WazeroInstance) resolveSingleModuleExportBindingLocked(name string) (*exportBinding, error) {
	if i.instance == nil {
		return nil, fmt.Errorf("instance is closed or uninitialized")
	}
	fn := i.instance.ExportedFunction(name)
	if fn == nil {
		return nil, fmt.Errorf("export %q not found", name)
	}

	var paramTypes, resultTypes []wit.Type
	canonical := false
	if i.module != nil && i.module.canonRegistry != nil {
		if lift := i.module.canonRegistry.FindLift(name); lift != nil {
			paramTypes = lift.Params
			resultTypes = lift.Results
			canonical = true
		}
	}

	postReturn := i.instance.ExportedFunction("cabi_post_" + name)

	binding := &exportBinding{
		name:        name,
		fn:          fn,
		postReturn:  postReturn,
		memory:      i.memory,
		alloc:       i.alloc,
		paramTypes:  paramTypes,
		resultTypes: resultTypes,
		coreMod:     i.instance,
		coreModIdx:  0,
		asyncify:    i.asyncify,
		scheduler:   i.scheduler,
		canonical:   canonical,
	}

	i.exportBindings[name] = binding
	return binding, nil
}

// typeRequiresMemory reports whether lowering or lifting the WIT type touches linear memory.
func typeRequiresMemory(t wit.Type) bool {
	if t == nil {
		return false
	}
	switch typ := t.(type) {
	case wit.String:
		return true
	case *wit.TypeDef:
		if typ == nil {
			return false
		}
		switch kind := typ.Kind.(type) {
		case *wit.List:
			return true
		case *wit.Record:
			for _, f := range kind.Fields {
				if typeRequiresMemory(f.Type) {
					return true
				}
			}
		case *wit.Tuple:
			for _, elem := range kind.Types {
				if typeRequiresMemory(elem) {
					return true
				}
			}
		case *wit.Variant:
			for _, c := range kind.Cases {
				if c.Type != nil && typeRequiresMemory(c.Type) {
					return true
				}
			}
		case *wit.Option:
			return typeRequiresMemory(kind.Type)
		case *wit.Result:
			if kind.OK != nil && typeRequiresMemory(kind.OK) {
				return true
			}
			if kind.Err != nil && typeRequiresMemory(kind.Err) {
				return true
			}
		case wit.Type:
			return typeRequiresMemory(kind)
		}
	}
	return false
}

// paramsRequireMemory reports whether lowering params requires linear memory.
func paramsRequireMemory(paramTypes []wit.Type) bool {
	count := 0
	for _, pt := range paramTypes {
		if typeRequiresMemory(pt) {
			return true
		}
		count += flatCount(pt)
	}
	return count > transcoder.MaxFlatParams
}

// paramsRequireAlloc reports whether lowering params requires guest memory allocation.
func paramsRequireAlloc(paramTypes []wit.Type) bool {
	return paramsRequireMemory(paramTypes)
}

// resultsRequireMemory reports whether lifting results requires linear memory.
func resultsRequireMemory(resultTypes []wit.Type) bool {
	if usesRetptr(resultTypes) {
		return true
	}
	for _, rt := range resultTypes {
		if typeRequiresMemory(rt) {
			return true
		}
	}
	return false
}

// localFunctionOwner only proves ownership for locally defined exports. A
// re-exported import can execute on another instance and has no local provenance.
func localFunctionOwner(mod api.Module, name string) api.Module {
	if mod == nil {
		return nil
	}
	def := mod.ExportedFunctionDefinitions()[name]
	if def == nil {
		return nil
	}
	if _, _, imported := def.Import(); imported {
		return nil
	}
	return mod
}

func localFreeFunction(mod api.Module) api.Function {
	for _, name := range []string{CabiFree, legacyDealloc, simpleFree} {
		if localFunctionOwner(mod, name) != nil {
			return mod.ExportedFunction(name)
		}
	}
	return nil
}

func (i *WazeroInstance) getOrCreateAllocatorLocked(reallocFn api.Function, allocMod api.Module) *wazeroAllocator {
	if reallocFn == nil {
		return nil
	}

	// allocMod is supplied by canonical index-space provenance, never inferred
	// from function wrappers or shared compiled definitions. Unknown means no free.
	freeFn := localFreeFunction(allocMod)

	if i.alloc != nil && i.alloc.allocFn == reallocFn && i.alloc.freeFn == freeFn {
		return i.alloc
	}
	if a, ok := i.allocatorCache[reallocFn]; ok && a.freeFn == freeFn {
		return a
	}

	paramCount := len(reallocFn.Definition().ParamTypes())
	isSimpleAlloc := paramCount < 4

	alloc := &wazeroAllocator{
		allocFn:       reallocFn,
		freeFn:        freeFn,
		stackBuf:      make([]uint64, 16),
		isSimpleAlloc: isSimpleAlloc,
	}
	if i.allocatorCache == nil {
		i.allocatorCache = make(map[api.Function]*wazeroAllocator)
	}
	i.allocatorCache[reallocFn] = alloc
	return alloc
}

func (i *WazeroInstance) getOrCreateMemoryLocked(mem api.Memory) *WazeroMemory {
	if mem == nil {
		return nil
	}
	if i.memory != nil && i.memory.mem == mem {
		return i.memory
	}
	if wm, ok := i.memoryCache[mem]; ok {
		return wm
	}
	wm := &WazeroMemory{mem: mem}
	if i.memoryCache == nil {
		i.memoryCache = make(map[api.Memory]*WazeroMemory)
	}
	i.memoryCache[mem] = wm
	return wm
}

func (i *WazeroInstance) getOrCreateAsyncifyForModuleLocked(mod api.Module, modIdx int) (*Asyncify, *Scheduler, error) {
	if mod == nil {
		return i.asyncify, i.scheduler, nil
	}
	if i.instance != nil && mod == i.instance && i.asyncify != nil && i.scheduler != nil {
		return i.asyncify, i.scheduler, nil
	}
	if entry, ok := i.asyncifyCache[mod]; ok {
		return entry.asyncify, entry.scheduler, nil
	}
	if mod.ExportedFunction("asyncify_get_state") == nil {
		return nil, nil, nil
	}

	a := NewAsyncify()
	if i.asyncifyConfig.StackSize > 0 {
		a.SetStackSize(i.asyncifyConfig.StackSize)
	}
	if i.asyncifyConfig.DataAddr > 0 {
		a.SetDataAddr(i.asyncifyConfig.DataAddr)
	}
	if i.asyncifyConfig.ownedStackBytes != 0 {
		// Owned configuration pre-initializes every current transformed core.
		// A later unreserved core cannot safely allocate here because export
		// binding has no call context to give an untrusted guest allocator.
		reservation, err := i.ownedAsyncifyReservationLocked(mod, i.asyncifyConfig.ownedStackBytes)
		if err != nil {
			return nil, nil, err
		}
		a.SetStackSize(i.asyncifyConfig.ownedStackBytes)
		a.SetDataAddr(reservation.dataAddr)
	}
	if i.linkerInst != nil {
		a.trusted = i.linkerInst.IsModuleTransformed(mod)
	} else {
		a.trusted = i.transformed
	}

	if err := a.Init(mod); err != nil {
		return nil, nil, err
	}
	a.lifetime = i.lifetime
	if i.asyncify != nil {
		a.generation = i.asyncify.generation
	}

	sched := NewScheduler(a)
	if i.asyncifyCache == nil {
		i.asyncifyCache = make(map[api.Module]*asyncifyCoreState)
	}
	i.asyncifyCache[mod] = &asyncifyCoreState{asyncify: a, scheduler: sched}
	if i.linkerInst != nil {
		i.linkerInst.RegisterAsyncifyController(mod, a)
	}
	return a, sched, nil
}

// clearAsyncifyHostArgs releases parked Canonical ABI stacks across every
// configured core after an aborted session. A nested bridge can park args in a
// child controller even though the session scheduler belongs to the root.
func (i *WazeroInstance) clearAsyncifyHostArgs() {
	i.bindingMu.RLock()
	controllers := make([]*Asyncify, 0, len(i.asyncifyCache)+1)
	if i.asyncify != nil {
		controllers = append(controllers, i.asyncify)
	}
	for _, state := range i.asyncifyCache {
		if state != nil && state.asyncify != nil {
			controllers = append(controllers, state.asyncify)
		}
	}
	i.bindingMu.RUnlock()
	for _, controller := range controllers {
		controller.ClearHostArgs()
	}
}

// poisonAsyncify marks an instance unusable for further guest entries after an
// async execution fails. Clearing parked host arguments prevents retained
// Canonical ABI state, but it cannot restore every core in a nested component
// call to a proven normal Asyncify state. Reuse would therefore be unsound.
// Close is the recovery boundary: it tears down every core and its stack.
func (i *WazeroInstance) poisonAsyncify(cause error) {
	if cause == nil {
		return
	}
	i.clearAsyncifyHostArgs()
	i.bindingMu.Lock()
	if i.asyncifyPoison == nil {
		i.asyncifyPoison = cause
	}
	i.bindingMu.Unlock()
}

func (i *WazeroInstance) setSuspendedSession(cs *CallSession) {
	i.bindingMu.Lock()
	defer i.bindingMu.Unlock()
	i.activeSession = cs
}

func (i *WazeroInstance) clearSuspendedSession(cs *CallSession) {
	i.bindingMu.Lock()
	defer i.bindingMu.Unlock()
	if i.activeSession == cs {
		i.activeSession = nil
	}
}

func (i *WazeroInstance) checkSuspended(target *exportBinding) error {
	i.bindingMu.RLock()
	poison := i.asyncifyPoison
	active := i.activeSession
	i.bindingMu.RUnlock()
	if poison != nil {
		return fmt.Errorf("asyncify execution previously failed: %w; close the instance before reuse", poison)
	}

	if active == nil || active.lifted {
		return nil
	}

	if active.done {
		if active.liftErr != nil {
			return fmt.Errorf("previous call result failed: %w; close the instance before reuse", active.liftErr)
		}
		return fmt.Errorf("previous call completed execution; lift its results and finish post-return before starting another call")
	}

	targetName := ""
	targetCoreName := "unknown"
	if target != nil {
		targetName = target.name
		targetCoreName = target.coreModName()
	}

	activeName := ""
	activeCoreName := "unknown"
	if active.binding != nil {
		activeName = active.binding.name
		activeCoreName = active.binding.coreModName()
	}

	if active.binding != nil && target != nil && active.binding.coreMod != target.coreMod {
		return fmt.Errorf("cannot execute export %q on core module %q: active call on core module %q is suspended; switching core while suspended is not supported",
			targetName, targetCoreName, activeCoreName)
	}

	return fmt.Errorf("cannot execute export %q: instance has an active suspended async call %q; resume or complete active call before starting new call",
		targetName, activeName)
}
