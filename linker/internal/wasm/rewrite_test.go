package wasm

import (
	"bytes"
	"testing"
)

func TestRewriteEmptyModuleNames_TooShort(t *testing.T) {
	tests := [][]byte{
		nil,
		{},
		{0x00, 0x61, 0x73, 0x6d},
		{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00},
	}

	for i, input := range tests {
		result := RewriteEmptyModuleNames(input)
		if !bytes.Equal(result, input) {
			t.Errorf("test %d: expected unchanged input for short wasm", i)
		}
	}
}

func TestRewriteEmptyModuleNames_NoImportSection(t *testing.T) {
	// Module with only type section
	wasm := append([]byte{}, testMagicVersion...)
	wasm = append(wasm, 0x01, 0x04, 0x01, 0x60, 0x00, 0x00)

	result := RewriteEmptyModuleNames(wasm)
	if !bytes.Equal(result, wasm) {
		t.Error("expected unchanged wasm when no import section")
	}
}

func TestRewriteEmptyModuleNames_NoEmptyModuleName(t *testing.T) {
	// Module with import section but non-empty module name
	wasm := buildTestModuleWithGlobalImport("env", "my_global", 0x7f, false)

	result := RewriteEmptyModuleNames(wasm)
	if !bytes.Equal(result, wasm) {
		t.Error("expected unchanged wasm when no empty module name")
	}
}

// buildModuleWithEmptyModuleNameImport creates WASM that imports from ""
func buildModuleWithEmptyModuleNameImport() []byte {
	wasm := append([]byte{}, testMagicVersion...)

	// Import section with empty module name
	importSection := []byte{
		0x01,                     // 1 import
		0x00,                     // module name length = 0 (empty)
		0x04, 't', 'e', 's', 't', // import name = "test"
		0x00, // function import
		0x00, // type index 0
	}

	// Need type section first
	typeSection := []byte{
		0x01,             // 1 type
		0x60, 0x00, 0x00, // func () -> ()
	}

	wasm = append(wasm, 0x01)
	wasm = append(wasm, EncodeULEB128(uint32(len(typeSection)))...)
	wasm = append(wasm, typeSection...)

	wasm = append(wasm, 0x02)
	wasm = append(wasm, EncodeULEB128(uint32(len(importSection)))...)
	wasm = append(wasm, importSection...)

	return wasm
}

func TestRewriteEmptyModuleNames_ReplacesEmpty(t *testing.T) {
	wasm := buildModuleWithEmptyModuleNameImport()

	result := RewriteEmptyModuleNames(wasm)

	// Result should be larger (empty string replaced with "$")
	if len(result) <= len(wasm) {
		t.Error("expected rewritten wasm to be larger")
	}

	// Result should contain "$" as module name
	if !bytes.Contains(result, []byte{0x01, '$'}) {
		t.Error("expected '$' as replacement module name")
	}
}

func TestHasEmptyModuleNameImport_True(t *testing.T) {
	wasm := buildModuleWithEmptyModuleNameImport()
	if !hasEmptyModuleNameImport(wasm) {
		t.Error("expected to detect empty module name import")
	}
}

func TestHasEmptyModuleNameImport_False(t *testing.T) {
	wasm := buildTestModuleWithGlobalImport("env", "var", 0x7f, false)
	if hasEmptyModuleNameImport(wasm) {
		t.Error("expected no empty module name import")
	}
}

func TestRewriteEmptyModuleNames_PreservesOtherSections(t *testing.T) {
	wasm := append([]byte{}, testMagicVersion...)

	// Type section
	typeSection := []byte{0x01, 0x60, 0x00, 0x00}
	wasm = append(wasm, 0x01)
	wasm = append(wasm, EncodeULEB128(uint32(len(typeSection)))...)
	wasm = append(wasm, typeSection...)

	// Import section with empty module name
	importSection := []byte{
		0x01, 0x00, 0x04, 't', 'e', 's', 't', 0x00, 0x00,
	}
	wasm = append(wasm, 0x02)
	wasm = append(wasm, EncodeULEB128(uint32(len(importSection)))...)
	wasm = append(wasm, importSection...)

	// Export section
	exportSection := []byte{0x00} // no exports
	wasm = append(wasm, 0x07)
	wasm = append(wasm, EncodeULEB128(uint32(len(exportSection)))...)
	wasm = append(wasm, exportSection...)

	result := RewriteEmptyModuleNames(wasm)

	// Should still have magic and version
	if !bytes.HasPrefix(result, testMagicVersion) {
		t.Error("expected magic and version preserved")
	}

	// Should still have type section (0x01)
	if !bytes.Contains(result, []byte{0x01, 0x04, 0x01, 0x60}) {
		t.Error("expected type section preserved")
	}

	// Should still have export section (0x07)
	if !bytes.Contains(result, []byte{0x07, 0x01, 0x00}) {
		t.Error("expected export section preserved")
	}
}
