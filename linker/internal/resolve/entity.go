// Package resolve provides entity resolution for WebAssembly Component Model.
//
// It defines the core types for representing WebAssembly entities (functions,
// memory, tables, globals) and their sources (module exports, host functions).
package resolve

import (
	"github.com/tetratelabs/wazero/api"
)

// EntityKind identifies the type of a WebAssembly entity.
type EntityKind uint8

const (
	EntityFunc EntityKind = iota
	EntityMemory
	EntityTable
	EntityGlobal
)

// Entity represents an exported WebAssembly entity.
type Entity struct {
	Source EntitySource
	Kind   EntityKind
}

// EntitySource is the interface for entity value providers.
// EntitySource implementations describe where an entity's value comes from.
type EntitySource interface {
	isEntitySource()
}

// ModuleExport references an export from a real wazero module.
type ModuleExport struct {
	Module     api.Module
	ExportName string
}

func (ModuleExport) isEntitySource() {}

// DirectMemory references a memory directly with source tracking.
type DirectMemory struct {
	Memory       api.Memory
	SourceModule api.Module // module that owns this memory (for bridge creation)
	SourceExport string     // export name in source module
}

func (DirectMemory) isEntitySource() {}

// DirectGlobal references a global directly.
type DirectGlobal struct {
	Global       api.Global
	SourceModule api.Module // module that owns this global
	SourceExport string     // export name in source module
}

func (DirectGlobal) isEntitySource() {}

// TableSource references a table from a source module.
type TableSource struct {
	Module     api.Module
	ExportName string
}

func (TableSource) isEntitySource() {}

// TrapFunc represents an unresolved function that will trap if called.
type TrapFunc struct {
	Name   string // The function name for error messages
	Reason string // Why the function is unresolved
}

func (TrapFunc) isEntitySource() {}

// HostFuncDef describes a host function's signature.
// This interface allows the resolve package to reference host functions
// without depending on the linker package's FuncDef type.
type HostFuncDef interface {
	GetHandler() api.GoModuleFunc
	GetParamTypes() []api.ValueType
	GetResultTypes() []api.ValueType
}

// HostFunc references a host function definition.
type HostFunc struct {
	Def HostFuncDef
}

func (HostFunc) isEntitySource() {}

// BoundHostFunc is a host function bound to specific memory and allocator.
// BoundHostFunc is for canon-lowered functions that need memory from a specific module.
type BoundHostFunc struct {
	Def       HostFuncDef
	Memory    api.Memory
	Allocator api.Function
}

func (BoundHostFunc) isEntitySource() {}
