// Package wasm provides WASM binary manipulation utilities for the linker.
package wasm

import (
	"github.com/tetratelabs/wazero/api"
)

// SynthModuleBuilder builds synthetic WASM modules for virtual instances
// that export tables, memory, globals, and/or functions.
type SynthModuleBuilder struct {
	hostModuleName   string
	tableImportMod   string
	tableImportName  string
	tableExportName  string
	memoryImportMod  string
	memoryImportName string
	memoryExportName string
	funcs            []synthFunc
	globals          []synthGlobal
	tableSize        uint32
}

type synthFunc struct {
	name        string
	paramTypes  []api.ValueType
	resultTypes []api.ValueType
}

type synthGlobal struct {
	moduleName string
	importName string
	exportName string
	valType    api.ValueType
	mutable    bool
	isLocal    bool
	initValue  int64
}

// NewSynthModuleBuilder creates a new synthetic module builder.
func NewSynthModuleBuilder(hostModuleName string) *SynthModuleBuilder {
	return &SynthModuleBuilder{
		hostModuleName:  hostModuleName,
		tableSize:       2,
		tableExportName: "$imports",
	}
}

// AddFunc adds a function to import and re-export.
func (b *SynthModuleBuilder) AddFunc(name string, params, results []api.ValueType) {
	b.funcs = append(b.funcs, synthFunc{
		name:        name,
		paramTypes:  params,
		resultTypes: results,
	})
}

// SetTableSize sets the minimum table size.
func (b *SynthModuleBuilder) SetTableSize(size uint32) {
	b.tableSize = size
}

// SetTableImport configures table import from another module.
func (b *SynthModuleBuilder) SetTableImport(moduleName, importName, exportName string) {
	b.tableImportMod = moduleName
	b.tableImportName = importName
	b.tableExportName = exportName
}

// HasTableImport returns true if a table import is configured.
func (b *SynthModuleBuilder) HasTableImport() bool {
	return b.tableImportMod != ""
}

// SetMemoryImport configures memory import from another module.
func (b *SynthModuleBuilder) SetMemoryImport(moduleName, importName, exportName string) {
	b.memoryImportMod = moduleName
	b.memoryImportName = importName
	b.memoryExportName = exportName
}

// HasMemoryImport returns true if a memory import is configured.
func (b *SynthModuleBuilder) HasMemoryImport() bool {
	return b.memoryImportMod != ""
}

// AddGlobalImport adds a global to import from another module.
func (b *SynthModuleBuilder) AddGlobalImport(moduleName, importName, exportName string, valType api.ValueType, mutable bool) {
	b.globals = append(b.globals, synthGlobal{
		moduleName: moduleName,
		importName: importName,
		exportName: exportName,
		valType:    valType,
		mutable:    mutable,
	})
}

// AddLocalGlobal adds a locally defined global with an initial value.
func (b *SynthModuleBuilder) AddLocalGlobal(exportName string, valType api.ValueType, mutable bool, initValue int64) {
	b.globals = append(b.globals, synthGlobal{
		exportName: exportName,
		valType:    valType,
		mutable:    mutable,
		isLocal:    true,
		initValue:  initValue,
	})
}

// Build generates the WASM module bytes.
func (b *SynthModuleBuilder) Build() []byte {
	if len(b.funcs) == 0 && !b.HasTableImport() && !b.HasMemoryImport() && len(b.globals) == 0 {
		return nil
	}

	hasFuncs := len(b.funcs) > 0
	var wasm []byte

	// Magic and version
	wasm = append(wasm, 0x00, 0x61, 0x73, 0x6d)
	wasm = append(wasm, 0x01, 0x00, 0x00, 0x00)

	// Type section
	if hasFuncs {
		typeSection := b.buildTypeSection()
		wasm = append(wasm, 0x01)
		wasm = append(wasm, EncodeULEB128(uint32(len(typeSection)))...)
		wasm = append(wasm, typeSection...)
	}

	// Import section
	importSection := b.buildImportSection()
	if len(importSection) > 1 {
		wasm = append(wasm, 0x02)
		wasm = append(wasm, EncodeULEB128(uint32(len(importSection)))...)
		wasm = append(wasm, importSection...)
	}

	// Function section
	if hasFuncs {
		funcSection := b.buildFuncSection()
		wasm = append(wasm, 0x03)
		wasm = append(wasm, EncodeULEB128(uint32(len(funcSection)))...)
		wasm = append(wasm, funcSection...)
	}

	// Table section
	if hasFuncs && !b.HasTableImport() {
		tableSection := b.buildTableSection()
		wasm = append(wasm, 0x04)
		wasm = append(wasm, EncodeULEB128(uint32(len(tableSection)))...)
		wasm = append(wasm, tableSection...)
	}

	// Global section
	if globalSection := b.buildGlobalSection(); globalSection != nil {
		wasm = append(wasm, 0x06)
		wasm = append(wasm, EncodeULEB128(uint32(len(globalSection)))...)
		wasm = append(wasm, globalSection...)
	}

	// Export section
	exportSection := b.buildExportSection()
	wasm = append(wasm, 0x07)
	wasm = append(wasm, EncodeULEB128(uint32(len(exportSection)))...)
	wasm = append(wasm, exportSection...)

	// Element section
	if hasFuncs {
		elemSection := b.buildElemSection()
		wasm = append(wasm, 0x09)
		wasm = append(wasm, EncodeULEB128(uint32(len(elemSection)))...)
		wasm = append(wasm, elemSection...)
	}

	// Code section
	if hasFuncs {
		codeSection := b.buildCodeSection()
		wasm = append(wasm, 0x0a)
		wasm = append(wasm, EncodeULEB128(uint32(len(codeSection)))...)
		wasm = append(wasm, codeSection...)
	}

	return wasm
}

func (b *SynthModuleBuilder) buildTypeSection() []byte {
	var section []byte
	section = append(section, EncodeULEB128(uint32(len(b.funcs)))...)

	for _, f := range b.funcs {
		section = append(section, 0x60)
		section = append(section, EncodeULEB128(uint32(len(f.paramTypes)))...)
		for _, t := range f.paramTypes {
			section = append(section, ValTypeToWasm(t))
		}
		section = append(section, EncodeULEB128(uint32(len(f.resultTypes)))...)
		for _, t := range f.resultTypes {
			section = append(section, ValTypeToWasm(t))
		}
	}

	return section
}

func (b *SynthModuleBuilder) countImportedGlobals() int {
	count := 0
	for _, g := range b.globals {
		if !g.isLocal {
			count++
		}
	}
	return count
}

func (b *SynthModuleBuilder) countLocalGlobals() int {
	count := 0
	for _, g := range b.globals {
		if g.isLocal {
			count++
		}
	}
	return count
}

func (b *SynthModuleBuilder) buildImportSection() []byte {
	var section []byte

	numImports := len(b.funcs) + b.countImportedGlobals()
	if b.HasTableImport() {
		numImports++
	}
	if b.HasMemoryImport() {
		numImports++
	}
	section = append(section, EncodeULEB128(uint32(numImports))...)

	// Function imports
	for i, f := range b.funcs {
		section = append(section, EncodeULEB128(uint32(len(b.hostModuleName)))...)
		section = append(section, []byte(b.hostModuleName)...)
		section = append(section, EncodeULEB128(uint32(len(f.name)))...)
		section = append(section, []byte(f.name)...)
		section = append(section, 0x00)
		section = append(section, EncodeULEB128(uint32(i))...)
	}

	// Table import
	if b.HasTableImport() {
		section = append(section, EncodeULEB128(uint32(len(b.tableImportMod)))...)
		section = append(section, []byte(b.tableImportMod)...)
		section = append(section, EncodeULEB128(uint32(len(b.tableImportName)))...)
		section = append(section, []byte(b.tableImportName)...)
		section = append(section, 0x01)
		section = append(section, 0x70)
		section = append(section, 0x00)
		section = append(section, EncodeULEB128(b.tableSize)...)
	}

	// Memory import
	if b.HasMemoryImport() {
		section = append(section, EncodeULEB128(uint32(len(b.memoryImportMod)))...)
		section = append(section, []byte(b.memoryImportMod)...)
		section = append(section, EncodeULEB128(uint32(len(b.memoryImportName)))...)
		section = append(section, []byte(b.memoryImportName)...)
		section = append(section, 0x02)
		section = append(section, 0x00)
		section = append(section, 0x00)
	}

	// Global imports
	for _, g := range b.globals {
		if g.isLocal {
			continue
		}
		section = append(section, EncodeULEB128(uint32(len(g.moduleName)))...)
		section = append(section, []byte(g.moduleName)...)
		section = append(section, EncodeULEB128(uint32(len(g.importName)))...)
		section = append(section, []byte(g.importName)...)
		section = append(section, 0x03)
		section = append(section, ValTypeToWasm(g.valType))
		if g.mutable {
			section = append(section, 0x01)
		} else {
			section = append(section, 0x00)
		}
	}

	return section
}

func (b *SynthModuleBuilder) buildFuncSection() []byte {
	var section []byte
	section = append(section, EncodeULEB128(uint32(len(b.funcs)))...)
	for i := range b.funcs {
		section = append(section, EncodeULEB128(uint32(i))...)
	}
	return section
}

func (b *SynthModuleBuilder) buildTableSection() []byte {
	var section []byte
	section = append(section, 0x01)
	section = append(section, 0x70)
	section = append(section, 0x01)
	section = append(section, EncodeULEB128(b.tableSize)...)
	section = append(section, EncodeULEB128(b.tableSize)...)
	return section
}

func (b *SynthModuleBuilder) buildGlobalSection() []byte {
	numLocal := b.countLocalGlobals()
	if numLocal == 0 {
		return nil
	}

	var section []byte
	section = append(section, EncodeULEB128(uint32(numLocal))...)

	for _, g := range b.globals {
		if !g.isLocal {
			continue
		}
		section = append(section, ValTypeToWasm(g.valType))
		if g.mutable {
			section = append(section, 0x01)
		} else {
			section = append(section, 0x00)
		}
		switch g.valType {
		case api.ValueTypeI32:
			section = append(section, 0x41)
			section = append(section, EncodeSLEB128(int32(g.initValue))...)
		case api.ValueTypeI64:
			section = append(section, 0x42)
			section = append(section, EncodeSLEB128(g.initValue)...)
		case api.ValueTypeF32:
			section = append(section, 0x43, 0, 0, 0, 0)
		case api.ValueTypeF64:
			section = append(section, 0x44, 0, 0, 0, 0, 0, 0, 0, 0)
		default:
			section = append(section, 0x41, 0x00)
		}
		section = append(section, 0x0B)
	}

	return section
}

func (b *SynthModuleBuilder) buildExportSection() []byte {
	var section []byte

	numExports := len(b.funcs) + len(b.globals)
	hasTable := b.HasTableImport() || len(b.funcs) > 0
	if hasTable {
		numExports++
	}
	if b.HasMemoryImport() {
		numExports++
	}
	section = append(section, EncodeULEB128(uint32(numExports))...)

	// Table export
	if hasTable {
		section = append(section, EncodeULEB128(uint32(len(b.tableExportName)))...)
		section = append(section, []byte(b.tableExportName)...)
		section = append(section, 0x01)
		section = append(section, 0x00)
	}

	// Memory export
	if b.HasMemoryImport() {
		section = append(section, EncodeULEB128(uint32(len(b.memoryExportName)))...)
		section = append(section, []byte(b.memoryExportName)...)
		section = append(section, 0x02)
		section = append(section, 0x00)
	}

	// Global exports
	numImportedGlobals := b.countImportedGlobals()
	importedIdx := 0
	localIdx := 0
	for _, g := range b.globals {
		section = append(section, EncodeULEB128(uint32(len(g.exportName)))...)
		section = append(section, []byte(g.exportName)...)
		section = append(section, 0x03)
		if g.isLocal {
			section = append(section, EncodeULEB128(uint32(numImportedGlobals+localIdx))...)
			localIdx++
		} else {
			section = append(section, EncodeULEB128(uint32(importedIdx))...)
			importedIdx++
		}
	}

	// Function exports
	numImports := len(b.funcs)
	for i, f := range b.funcs {
		section = append(section, EncodeULEB128(uint32(len(f.name)))...)
		section = append(section, []byte(f.name)...)
		section = append(section, 0x00)
		section = append(section, EncodeULEB128(uint32(numImports+i))...)
	}

	return section
}

func (b *SynthModuleBuilder) buildElemSection() []byte {
	var section []byte
	section = append(section, 0x01)
	section = append(section, 0x00)
	section = append(section, 0x41, 0x00)
	section = append(section, 0x0b)

	section = append(section, EncodeULEB128(uint32(len(b.funcs)))...)
	for i := range b.funcs {
		section = append(section, EncodeULEB128(uint32(i))...)
	}

	return section
}

func (b *SynthModuleBuilder) buildCodeSection() []byte {
	var section []byte
	section = append(section, EncodeULEB128(uint32(len(b.funcs)))...)

	for i, f := range b.funcs {
		funcBody := b.buildFuncBody(i, f)
		section = append(section, EncodeULEB128(uint32(len(funcBody)))...)
		section = append(section, funcBody...)
	}

	return section
}

func (b *SynthModuleBuilder) buildFuncBody(importIdx int, f synthFunc) []byte {
	var body []byte
	body = append(body, 0x00)

	for i := range f.paramTypes {
		body = append(body, 0x20)
		body = append(body, EncodeULEB128(uint32(i))...)
	}

	body = append(body, 0x10)
	body = append(body, EncodeULEB128(uint32(importIdx))...)
	body = append(body, 0x0b)

	return body
}
