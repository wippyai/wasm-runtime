package linker

import (
	"sync"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/linker/internal/resolve"
)

// Type aliases for backwards compatibility.
// These types are now defined in linker/internal/resolve.
type (
	EntityKind      = resolve.EntityKind
	Entity          = resolve.Entity
	EntitySource    = resolve.EntitySource
	ModuleExport    = resolve.ModuleExport
	DirectMemory    = resolve.DirectMemory
	DirectGlobal    = resolve.DirectGlobal
	TableSource     = resolve.TableSource
	TrapFunc        = resolve.TrapFunc
	HostFunc        = resolve.HostFunc
	BoundHostFunc   = resolve.BoundHostFunc
	VirtualInstance = resolve.VirtualInstance
)

// Entity kind constants (re-exported for backwards compatibility)
const (
	EntityFunc   = resolve.EntityFunc
	EntityMemory = resolve.EntityMemory
	EntityTable  = resolve.EntityTable
	EntityGlobal = resolve.EntityGlobal
)

// NewVirtualInstance creates an empty virtual instance.
func NewVirtualInstance(name string) *VirtualInstance {
	return resolve.NewVirtualInstance(name)
}

// Resolver handles import resolution for component instantiation.
// It maps instance names to either VirtualInstances or wazero Modules.
//
// Resolution priority during instantiation:
//  1. VirtualInstance (if registered for the name)
//  2. Module (if registered for the name)
//  3. Linker namespace bindings (fallback)
//
// Resolver is thread-safe.
type Resolver struct {
	linker    *Linker
	instances map[string]*VirtualInstance
	modules   map[string]api.Module
	mu        sync.RWMutex
}

// NewResolver creates a resolver for the given linker
func NewResolver(l *Linker) *Resolver {
	return &Resolver{
		linker:    l,
		instances: make(map[string]*VirtualInstance),
		modules:   make(map[string]api.Module),
	}
}

// RegisterInstance makes a virtual instance available for import resolution.
// RegisterInstance should be called before instantiating modules that depend on this instance.
func (r *Resolver) RegisterInstance(name string, inst *VirtualInstance) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances[name] = inst
}

// RegisterModule makes a wazero module available for import resolution.
// RegisterModule exports from this module can satisfy imports from other modules.
func (r *Resolver) RegisterModule(name string, mod api.Module) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[name] = mod
}

// GetInstance returns a registered virtual instance by name.
// GetInstance returns nil if not found. Used internally during instantiation.
func (r *Resolver) GetInstance(name string) *VirtualInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.instances[name]
}

// GetModule returns a registered module by name.
// GetModule returns nil if not found. Used internally during instantiation.
func (r *Resolver) GetModule(name string) api.Module {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modules[name]
}

// ResolveFunc finds a function by instance and export name.
// ResolveFunc checks virtual instances, real modules, then linker namespace.
func (r *Resolver) ResolveFunc(instanceName, exportName string) api.Function {
	r.mu.RLock()
	inst := r.instances[instanceName]
	mod := r.modules[instanceName]
	r.mu.RUnlock()

	if inst != nil {
		return inst.GetFunc(exportName)
	}

	if mod != nil {
		return mod.ExportedFunction(exportName)
	}

	// Try linker namespace resolution
	if r.linker != nil {
		path := instanceName + "#" + exportName
		if def := r.linker.Resolve(path); def != nil {
			return nil // Host funcs need separate instantiation
		}
	}

	return nil
}

// ResolveMemory finds a memory by instance and export name.
// ResolveMemory checks virtual instances first, then real modules.
func (r *Resolver) ResolveMemory(instanceName, exportName string) api.Memory {
	r.mu.RLock()
	inst := r.instances[instanceName]
	mod := r.modules[instanceName]
	r.mu.RUnlock()

	if inst != nil {
		return inst.GetMemory(exportName)
	}

	if mod != nil {
		return mod.ExportedMemory(exportName)
	}

	return nil
}

// ResolveGlobal finds a global by instance and export name.
// ResolveGlobal checks virtual instances first, then real modules.
func (r *Resolver) ResolveGlobal(instanceName, exportName string) api.Global {
	r.mu.RLock()
	inst := r.instances[instanceName]
	mod := r.modules[instanceName]
	r.mu.RUnlock()

	if inst != nil {
		return inst.GetGlobal(exportName)
	}

	if mod != nil {
		return mod.ExportedGlobal(exportName)
	}

	return nil
}

// CreateVirtualFromModule wraps a real module as a virtual instance.
// CreateVirtualFromModule is useful for aliasing module exports under a different namespace.
// It returns nil if mod is nil.
func (r *Resolver) CreateVirtualFromModule(name string, mod api.Module, exports map[string]EntityKind) *VirtualInstance {
	if mod == nil {
		return nil
	}
	v := NewVirtualInstance(name)

	for exportName, kind := range exports {
		v.DefineModuleExport(exportName, kind, mod, exportName)
	}

	r.RegisterInstance(name, v)
	return v
}
