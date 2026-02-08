package resolve

import (
	"github.com/tetratelabs/wazero/api"
)

// VirtualInstance represents a component instance created from exports
// without generating synthetic WASM. It holds direct references to entities.
//
// Thread-safety: reads are safe after creation; concurrent writes require
// external synchronization. In practice, VirtualInstances are populated
// during instantiation (single-threaded) then read-only thereafter.
type VirtualInstance struct {
	entities     map[string]Entity
	name         string
	orderedNames []string
}

// NewVirtualInstance creates an empty virtual instance.
func NewVirtualInstance(name string) *VirtualInstance {
	return &VirtualInstance{
		name:     name,
		entities: make(map[string]Entity),
	}
}

// Name returns the instance name.
func (v *VirtualInstance) Name() string {
	return v.name
}

// Define adds an entity with the given name.
func (v *VirtualInstance) Define(name string, entity Entity) {
	if _, exists := v.entities[name]; !exists {
		v.orderedNames = append(v.orderedNames, name)
	}
	v.entities[name] = entity
}

// DefineFunc adds a function entity from a host function definition.
func (v *VirtualInstance) DefineFunc(name string, def HostFuncDef) {
	v.Define(name, Entity{
		Kind:   EntityFunc,
		Source: HostFunc{Def: def},
	})
}

// DefineModuleExport adds an entity from a real module export.
func (v *VirtualInstance) DefineModuleExport(name string, kind EntityKind, mod api.Module, exportName string) {
	v.Define(name, Entity{
		Kind: kind,
		Source: ModuleExport{
			Module:     mod,
			ExportName: exportName,
		},
	})
}

// DefineMemory adds a memory entity.
func (v *VirtualInstance) DefineMemory(name string, mem api.Memory) {
	v.Define(name, Entity{
		Kind:   EntityMemory,
		Source: DirectMemory{Memory: mem},
	})
}

// DefineMemoryWithSource adds a memory entity with source tracking.
func (v *VirtualInstance) DefineMemoryWithSource(name string, mem api.Memory, srcMod api.Module, srcExport string) {
	v.Define(name, Entity{
		Kind: EntityMemory,
		Source: DirectMemory{
			Memory:       mem,
			SourceModule: srcMod,
			SourceExport: srcExport,
		},
	})
}

// DefineGlobal adds a global entity.
func (v *VirtualInstance) DefineGlobal(name string, global api.Global) {
	v.Define(name, Entity{
		Kind:   EntityGlobal,
		Source: DirectGlobal{Global: global},
	})
}

// DefineGlobalWithSource adds a global entity with source tracking.
func (v *VirtualInstance) DefineGlobalWithSource(name string, global api.Global, srcMod api.Module, srcExport string) {
	v.Define(name, Entity{
		Kind: EntityGlobal,
		Source: DirectGlobal{
			Global:       global,
			SourceModule: srcMod,
			SourceExport: srcExport,
		},
	})
}

// DefineTableRef adds a table reference from a module export.
// Note: Tables are tracked for linking purposes but not directly accessible.
func (v *VirtualInstance) DefineTableRef(name string, mod api.Module, exportName string) {
	v.Define(name, Entity{
		Kind: EntityTable,
		Source: ModuleExport{
			Module:     mod,
			ExportName: exportName,
		},
	})
}

// DefineTableSource adds a table entity with a TableSource.
func (v *VirtualInstance) DefineTableSource(name string, mod api.Module, exportName string) {
	v.Define(name, Entity{
		Kind: EntityTable,
		Source: TableSource{
			Module:     mod,
			ExportName: exportName,
		},
	})
}

// Get returns an entity by name for type inspection.
// Get returns nil if not found. Use typed accessors (GetFunc, GetMemory) for values.
func (v *VirtualInstance) Get(name string) *Entity {
	if e, ok := v.entities[name]; ok {
		return &e
	}
	return nil
}

// GetFunc returns the WASM function at the given name.
// GetFunc returns nil for host functions (use HostFunc entity source directly).
func (v *VirtualInstance) GetFunc(name string) api.Function {
	e := v.Get(name)
	if e == nil || e.Kind != EntityFunc {
		return nil
	}

	switch src := e.Source.(type) {
	case ModuleExport:
		if src.Module == nil {
			return nil
		}
		return src.Module.ExportedFunction(src.ExportName)
	case HostFunc:
		return nil // Host funcs need separate handling
	}
	return nil
}

// GetMemory returns the linear memory at the given name.
// GetMemory resolves both direct references and module exports.
func (v *VirtualInstance) GetMemory(name string) api.Memory {
	e := v.Get(name)
	if e == nil || e.Kind != EntityMemory {
		return nil
	}

	switch src := e.Source.(type) {
	case ModuleExport:
		if src.Module == nil {
			return nil
		}
		return src.Module.ExportedMemory(src.ExportName)
	case DirectMemory:
		return src.Memory
	}
	return nil
}

// GetGlobal returns the global variable at the given name.
// GetGlobal resolves both direct references and module exports.
func (v *VirtualInstance) GetGlobal(name string) api.Global {
	e := v.Get(name)
	if e == nil || e.Kind != EntityGlobal {
		return nil
	}

	switch src := e.Source.(type) {
	case ModuleExport:
		if src.Module == nil {
			return nil
		}
		return src.Module.ExportedGlobal(src.ExportName)
	case DirectGlobal:
		return src.Global
	}
	return nil
}

// HasTable checks if a table entity exists at the given name.
// Note: wazero doesn't expose tables directly via api.Module,
// so we can only check existence, not retrieve the table.
func (v *VirtualInstance) HasTable(name string) bool {
	e := v.Get(name)
	return e != nil && e.Kind == EntityTable
}

// OrderedFuncNames returns function entity names in insertion order.
// The order matches the component binary's export order, which determines
// table population indices for call_indirect.
func (v *VirtualInstance) OrderedFuncNames() []string {
	var result []string
	for _, name := range v.orderedNames {
		if e, ok := v.entities[name]; ok && e.Kind == EntityFunc {
			result = append(result, name)
		}
	}
	return result
}

// All returns a copy of all entities for iteration.
// All is safe to modify the returned map.
func (v *VirtualInstance) All() map[string]Entity {
	result := make(map[string]Entity, len(v.entities))
	for k, e := range v.entities {
		result[k] = e
	}
	return result
}

// Entities returns direct access to the entities map.
// Entities returns a map that should not be modified.
func (v *VirtualInstance) Entities() map[string]Entity {
	return v.entities
}

// HasMemory checks if the virtual instance has any memory entity with valid source.
func (v *VirtualInstance) HasMemory() bool {
	for _, entity := range v.entities {
		if entity.Kind == EntityMemory && entity.Source != nil {
			return true
		}
	}
	return false
}

// HasTableEntity checks if the virtual instance has any table entity.
func (v *VirtualInstance) HasTableEntity() bool {
	for _, entity := range v.entities {
		if entity.Kind == EntityTable {
			return true
		}
	}
	return false
}

// HasGlobals checks if the virtual instance has any global entity with valid source.
func (v *VirtualInstance) HasGlobals() bool {
	for _, entity := range v.entities {
		if entity.Kind == EntityGlobal {
			if src, ok := entity.Source.(DirectGlobal); ok && src.SourceModule != nil {
				return true
			}
		}
	}
	return false
}
