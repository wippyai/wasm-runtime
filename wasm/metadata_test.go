package wasm

import (
	"bytes"
	"reflect"
	"testing"
)

func TestParseModuleMetadataMatchesFullParse(t *testing.T) {
	source := &Module{
		Types:   []FuncType{{}},
		Funcs:   []uint32{0},
		Exports: []Export{{Name: "run", Kind: KindFunc, Idx: 0}},
		Code:    []FuncBody{{Code: []byte{OpEnd}}},
		Data:    []DataSegment{{Flags: 1, Init: bytes.Repeat([]byte{42}, 4096)}},
	}
	data := source.Encode()
	full, err := ParseModule(data)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := ParseModuleMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Code) != 1 || len(full.Data) != 1 {
		t.Fatal("fixture lost code or data")
	}
	full.Code = nil
	full.Data = nil
	if !reflect.DeepEqual(full, metadata) {
		t.Fatal("metadata differs from full parser beyond skipped code/data")
	}
}

func TestParseModuleMetadataChecksSkippedSectionExtent(t *testing.T) {
	header := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	for _, section := range []byte{SectionCode, SectionData} {
		input := append(append([]byte{}, header...), section, 2, 0)
		if _, err := ParseModuleMetadata(input); err == nil {
			t.Fatalf("accepted truncated section %d", section)
		}
	}
	// Metadata inspection intentionally does not decode the skipped payload.
	// Callers must still validate executable modules with their Wasm engine.
	input := append(append([]byte{}, header...), SectionCode, 1, 255)
	if _, err := ParseModule(input); err == nil {
		t.Fatal("malformed code fixture unexpectedly parsed")
	}
	if _, err := ParseModuleMetadata(input); err != nil {
		t.Fatalf("decoded skipped code: %v", err)
	}
}
