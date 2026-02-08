package wasm

import (
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// Minimal WASM module structure for testing:
// magic (4) + version (4) + sections

var testMagicVersion = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func TestParseGlobalExports_TooShort(t *testing.T) {
	tests := [][]byte{
		nil,
		{},
		{0x00, 0x61, 0x73},
		{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00},
	}

	for i, input := range tests {
		result := ParseGlobalExports(input)
		if result != nil {
			t.Errorf("test %d: expected nil for short input, got %v", i, result)
		}
	}
}

func TestParseGlobalImports_TooShort(t *testing.T) {
	result := ParseGlobalImports([]byte{0x00, 0x61, 0x73, 0x6d})
	if result != nil {
		t.Error("expected nil for short input")
	}
}

func TestParseTableExports_TooShort(t *testing.T) {
	result := ParseTableExports([]byte{0x00, 0x61})
	if result != nil {
		t.Error("expected nil for short input")
	}
}

func TestParseGlobalExports_NoSections(t *testing.T) {
	result := ParseGlobalExports(testMagicVersion)
	if len(result) != 0 {
		t.Errorf("expected no exports, got %d", len(result))
	}
}

func TestParseGlobalImports_NoImportSection(t *testing.T) {
	// Module with only type section (0x01)
	wasm := append([]byte{}, testMagicVersion...)
	wasm = append(wasm, 0x01, 0x04, 0x01, 0x60, 0x00, 0x00) // type section

	result := ParseGlobalImports(wasm)
	if result != nil {
		t.Error("expected nil for module without import section")
	}
}

func TestParseTableExports_NoExportSection(t *testing.T) {
	wasm := append([]byte{}, testMagicVersion...)
	wasm = append(wasm, 0x01, 0x04, 0x01, 0x60, 0x00, 0x00) // type section

	result := ParseTableExports(wasm)
	if result != nil {
		t.Error("expected nil for module without export section")
	}
}

// buildTestModuleWithGlobalExport creates a minimal WASM module with a global export
func buildTestModuleWithGlobalExport(exportName string, valType api.ValueType, mutable bool) []byte {
	wasm := append([]byte{}, testMagicVersion...)

	// Global section (0x06)
	globalSection := []byte{
		0x01,                   // 1 global
		ValTypeToWasm(valType), // type
	}
	if mutable {
		globalSection = append(globalSection, 0x01)
	} else {
		globalSection = append(globalSection, 0x00)
	}
	// Init expression: i32.const 0, end
	globalSection = append(globalSection, 0x41, 0x00, 0x0b)

	wasm = append(wasm, 0x06)
	wasm = append(wasm, EncodeULEB128(uint32(len(globalSection)))...)
	wasm = append(wasm, globalSection...)

	// Export section (0x07)
	exportSection := []byte{0x01} // 1 export
	exportSection = append(exportSection, EncodeULEB128(uint32(len(exportName)))...)
	exportSection = append(exportSection, []byte(exportName)...)
	exportSection = append(exportSection, 0x03) // global export
	exportSection = append(exportSection, 0x00) // global index 0

	wasm = append(wasm, 0x07)
	wasm = append(wasm, EncodeULEB128(uint32(len(exportSection)))...)
	wasm = append(wasm, exportSection...)

	return wasm
}

func TestParseGlobalExports_SingleGlobal(t *testing.T) {
	wasm := buildTestModuleWithGlobalExport("test_global", api.ValueTypeI32, false)

	result := ParseGlobalExports(wasm)
	if len(result) != 1 {
		t.Fatalf("expected 1 export, got %d", len(result))
	}

	if result[0].Name != "test_global" {
		t.Errorf("expected name 'test_global', got '%s'", result[0].Name)
	}
	if result[0].ValType != api.ValueTypeI32 {
		t.Errorf("expected i32, got %v", result[0].ValType)
	}
	if result[0].Mutable {
		t.Error("expected immutable")
	}
}

func TestParseGlobalExports_MutableGlobal(t *testing.T) {
	wasm := buildTestModuleWithGlobalExport("mut_global", api.ValueTypeI64, true)

	result := ParseGlobalExports(wasm)
	if len(result) != 1 {
		t.Fatalf("expected 1 export, got %d", len(result))
	}

	if result[0].ValType != api.ValueTypeI64 {
		t.Errorf("expected i64, got %v", result[0].ValType)
	}
	if !result[0].Mutable {
		t.Error("expected mutable")
	}
}

// buildTestModuleWithGlobalImport creates a WASM module that imports a global
func buildTestModuleWithGlobalImport(modName, impName string, valType api.ValueType, mutable bool) []byte {
	wasm := append([]byte{}, testMagicVersion...)

	// Import section (0x02)
	importSection := []byte{0x01} // 1 import
	importSection = append(importSection, EncodeULEB128(uint32(len(modName)))...)
	importSection = append(importSection, []byte(modName)...)
	importSection = append(importSection, EncodeULEB128(uint32(len(impName)))...)
	importSection = append(importSection, []byte(impName)...)
	importSection = append(importSection, 0x03) // global import
	importSection = append(importSection, ValTypeToWasm(valType))
	if mutable {
		importSection = append(importSection, 0x01)
	} else {
		importSection = append(importSection, 0x00)
	}

	wasm = append(wasm, 0x02)
	wasm = append(wasm, EncodeULEB128(uint32(len(importSection)))...)
	wasm = append(wasm, importSection...)

	return wasm
}

func TestParseGlobalImports_SingleImport(t *testing.T) {
	wasm := buildTestModuleWithGlobalImport("env", "my_global", api.ValueTypeI32, false)

	result := ParseGlobalImports(wasm)
	if len(result) != 1 {
		t.Fatalf("expected 1 import, got %d", len(result))
	}

	if result[0].ModuleName != "env" {
		t.Errorf("expected module 'env', got '%s'", result[0].ModuleName)
	}
	if result[0].ImportName != "my_global" {
		t.Errorf("expected import 'my_global', got '%s'", result[0].ImportName)
	}
	if result[0].ValType != api.ValueTypeI32 {
		t.Error("expected i32")
	}
	if result[0].Mutable {
		t.Error("expected immutable")
	}
}

// buildTestModuleWithTableExport creates a WASM module with a table export
func buildTestModuleWithTableExport(exportName string) []byte {
	wasm := append([]byte{}, testMagicVersion...)

	// Table section (0x04)
	tableSection := []byte{
		0x01, // 1 table
		0x70, // funcref
		0x00, // no max
		0x01, // min = 1
	}

	wasm = append(wasm, 0x04)
	wasm = append(wasm, EncodeULEB128(uint32(len(tableSection)))...)
	wasm = append(wasm, tableSection...)

	// Export section (0x07)
	exportSection := []byte{0x01} // 1 export
	exportSection = append(exportSection, EncodeULEB128(uint32(len(exportName)))...)
	exportSection = append(exportSection, []byte(exportName)...)
	exportSection = append(exportSection, 0x01) // table export
	exportSection = append(exportSection, 0x00) // table index 0

	wasm = append(wasm, 0x07)
	wasm = append(wasm, EncodeULEB128(uint32(len(exportSection)))...)
	wasm = append(wasm, exportSection...)

	return wasm
}

func TestParseTableExports_SingleTable(t *testing.T) {
	wasm := buildTestModuleWithTableExport("$imports")

	result := ParseTableExports(wasm)
	if len(result) != 1 {
		t.Fatalf("expected 1 table export, got %d", len(result))
	}

	if result[0] != "$imports" {
		t.Errorf("expected '$imports', got '%s'", result[0])
	}
}

func TestParseGlobalExports_ImportedGlobal(t *testing.T) {
	wasm := append([]byte{}, testMagicVersion...)

	// Import section with global import
	importSection := []byte{0x01}
	importSection = append(importSection, 0x03, 'e', 'n', 'v')                // "env"
	importSection = append(importSection, 0x06, 'm', 'y', '_', 'v', 'a', 'r') // "my_var"
	importSection = append(importSection, 0x03)                               // global
	importSection = append(importSection, 0x7f)                               // i32
	importSection = append(importSection, 0x00)                               // immutable

	wasm = append(wasm, 0x02)
	wasm = append(wasm, EncodeULEB128(uint32(len(importSection)))...)
	wasm = append(wasm, importSection...)

	// Export section re-exporting the imported global
	exportSection := []byte{0x01}
	exportSection = append(exportSection, 0x08, 'r', 'e', '_', 'g', 'l', 'o', 'b', 'l')
	exportSection = append(exportSection, 0x03) // global export
	exportSection = append(exportSection, 0x00) // index 0

	wasm = append(wasm, 0x07)
	wasm = append(wasm, EncodeULEB128(uint32(len(exportSection)))...)
	wasm = append(wasm, exportSection...)

	result := ParseGlobalExports(wasm)
	if len(result) != 1 {
		t.Fatalf("expected 1 export, got %d", len(result))
	}

	if !result[0].IsImport {
		t.Error("expected IsImport to be true")
	}
	if result[0].ImportModule != "env" {
		t.Errorf("expected import module 'env', got '%s'", result[0].ImportModule)
	}
}
