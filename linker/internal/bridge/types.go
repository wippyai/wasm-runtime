// Package bridge handles creation of bridge modules for WebAssembly Component Model.
//
// Bridge modules act as intermediaries that:
// - Re-export functions from other modules with correct signatures
// - Import/export memory, tables, and globals that host modules can't handle
// - Provide synthetic WASM modules when wazero host modules are insufficient
package bridge

import (
	"github.com/tetratelabs/wazero/api"
)

// Export describes a function to export in a bridge module
type Export struct {
	Name        string
	Fn          api.GoModuleFunc
	ParamTypes  []api.ValueType
	ResultTypes []api.ValueType
}

// ImportSig describes the expected signature for an import
type ImportSig struct {
	Params  []api.ValueType
	Results []api.ValueType
}

// TableLimit describes expected table limits for an import
type TableLimit struct {
	Min uint32
	Max uint32 // 0 means unbounded
}

// GlobalInfo describes a global variable for import/export
type GlobalInfo struct {
	SourceModule api.Module
	Name         string
	SourceExport string
	ValType      api.ValueType
	Mutable      bool
}

// MemoryInfo describes memory for import/export
type MemoryInfo struct {
	SourceModule api.Module
	SourceExport string // export name in source module
	ExportAs     string // name to export as
}

// TableInfo describes a table for import/export
type TableInfo struct {
	SourceModule api.Module
	SourceExport string // export name in source module
	ExportAs     string // name to export as
}

// HostBinding describes a resolved host function binding
type HostBinding struct {
	ImportName  string
	Handler     api.GoModuleFunc
	ParamTypes  []api.ValueType
	ResultTypes []api.ValueType
	IsTrap      bool
}
