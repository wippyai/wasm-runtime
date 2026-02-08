package linker

import (
	"github.com/tetratelabs/wazero/api"
	internalwasm "github.com/wippyai/wasm-runtime/linker/internal/wasm"
)

// synthModuleBuilder wraps internal/wasm.SynthModuleBuilder for package use.
type synthModuleBuilder struct {
	*internalwasm.SynthModuleBuilder
}

func newSynthModuleBuilder(hostModuleName string) *synthModuleBuilder {
	return &synthModuleBuilder{
		SynthModuleBuilder: internalwasm.NewSynthModuleBuilder(hostModuleName),
	}
}

func (b *synthModuleBuilder) addFunc(name string, params, results []api.ValueType) {
	b.AddFunc(name, params, results)
}

func (b *synthModuleBuilder) setTableSize(size uint32) {
	b.SetTableSize(size)
}

func (b *synthModuleBuilder) setTableImport(moduleName, importName, exportName string) {
	b.SetTableImport(moduleName, importName, exportName)
}

func (b *synthModuleBuilder) setMemoryImport(moduleName, importName, exportName string) {
	b.SetMemoryImport(moduleName, importName, exportName)
}

func (b *synthModuleBuilder) addGlobalImport(moduleName, importName, exportName string, valType api.ValueType, mutable bool) {
	b.AddGlobalImport(moduleName, importName, exportName, valType, mutable)
}

func (b *synthModuleBuilder) addLocalGlobal(exportName string, valType api.ValueType, mutable bool, initValue int64) {
	b.AddLocalGlobal(exportName, valType, mutable, initValue)
}

func (b *synthModuleBuilder) build() []byte {
	return b.Build()
}

// GlobalExport is a re-export of the internal wasm package type.
type GlobalExport = internalwasm.GlobalExport

// GlobalImport is a re-export of the internal wasm package type.
type GlobalImport = internalwasm.GlobalImport

// parseGlobalExports delegates to internal/wasm.
func parseGlobalExports(wasmBytes []byte) []GlobalExport {
	return internalwasm.ParseGlobalExports(wasmBytes)
}

// parseGlobalImports delegates to internal/wasm.
func parseGlobalImports(wasmBytes []byte) []GlobalImport {
	return internalwasm.ParseGlobalImports(wasmBytes)
}

// parseTableExports delegates to internal/wasm.
func parseTableExports(wasmBytes []byte) []string {
	return internalwasm.ParseTableExports(wasmBytes)
}

// valTypeToWasm delegates to internal/wasm.
func valTypeToWasm(t api.ValueType) byte {
	return internalwasm.ValTypeToWasm(t)
}
