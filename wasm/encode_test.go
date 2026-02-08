package wasm_test

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestEncodeEmptyModule(t *testing.T) {
	m := &wasm.Module{}
	data := m.Encode()

	if len(data) != 8 {
		t.Errorf("expected 8 bytes for empty module, got %d", len(data))
	}
	if !bytes.Equal(data[:4], []byte{0x00, 0x61, 0x73, 0x6D}) {
		t.Error("invalid magic number")
	}
	if !bytes.Equal(data[4:8], []byte{0x01, 0x00, 0x00, 0x00}) {
		t.Error("invalid version")
	}
}

func TestEncodeTypes(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: nil, Results: nil},
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
			{Params: []wasm.ValType{wasm.ValI32, wasm.ValI64}, Results: []wasm.ValType{wasm.ValF32, wasm.ValF64}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Types) != 3 {
		t.Fatalf("expected 3 types, got %d", len(parsed.Types))
	}

	// Verify type 0
	if len(parsed.Types[0].Params) != 0 || len(parsed.Types[0].Results) != 0 {
		t.Error("type 0 should be () -> ()")
	}

	// Verify type 1
	if len(parsed.Types[1].Params) != 1 || parsed.Types[1].Params[0] != wasm.ValI32 {
		t.Error("type 1 params mismatch")
	}
}

func TestEncodeFunctions(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: nil, Results: nil},
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Funcs: []uint32{0, 1, 0},
		Code: []wasm.FuncBody{
			{Locals: nil, Code: []byte{wasm.OpEnd}},
			{Locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, Code: []byte{wasm.OpLocalGet, 0, wasm.OpEnd}},
			{Locals: nil, Code: []byte{wasm.OpEnd}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Funcs) != 3 {
		t.Errorf("expected 3 funcs, got %d", len(parsed.Funcs))
	}
	if len(parsed.Code) != 3 {
		t.Errorf("expected 3 code entries, got %d", len(parsed.Code))
	}
}

func TestEncodeImportsExports(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Imports: []wasm.Import{
			{Module: "env", Name: "log", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Code:  []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
		Exports: []wasm.Export{
			{Name: "main", Kind: wasm.KindFunc, Idx: 1},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Imports) != 1 {
		t.Errorf("expected 1 import, got %d", len(parsed.Imports))
	}
	if len(parsed.Exports) != 1 {
		t.Errorf("expected 1 export, got %d", len(parsed.Exports))
	}
}

func TestEncodeMemories(t *testing.T) {
	max := uint64(10)
	m := &wasm.Module{
		Memories: []wasm.MemoryType{
			{Limits: wasm.Limits{Min: 1, Max: &max}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(parsed.Memories))
	}
	if parsed.Memories[0].Limits.Min != 1 {
		t.Errorf("min mismatch")
	}
	if parsed.Memories[0].Limits.Max == nil || *parsed.Memories[0].Limits.Max != 10 {
		t.Errorf("max mismatch")
	}
}

func TestEncodeTables(t *testing.T) {
	max := uint64(100)
	m := &wasm.Module{
		Tables: []wasm.TableType{
			{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 10, Max: &max}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(parsed.Tables))
	}
	if parsed.Tables[0].Limits.Min != 10 {
		t.Errorf("min mismatch: got %d", parsed.Tables[0].Limits.Min)
	}
}

func TestEncodeGlobals(t *testing.T) {
	m := &wasm.Module{
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}, Init: []byte{wasm.OpI32Const, 42, wasm.OpEnd}},
			{Type: wasm.GlobalType{ValType: wasm.ValI64, Mutable: true}, Init: []byte{wasm.OpI64Const, 0, wasm.OpEnd}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Globals) != 2 {
		t.Fatalf("expected 2 globals, got %d", len(parsed.Globals))
	}
	if parsed.Globals[0].Type.ValType != wasm.ValI32 {
		t.Error("global 0 should be i32")
	}
	if parsed.Globals[0].Type.Mutable {
		t.Error("global 0 should be immutable")
	}
	if !parsed.Globals[1].Type.Mutable {
		t.Error("global 1 should be mutable")
	}
}

func TestEncodeData(t *testing.T) {
	m := &wasm.Module{
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Data: []wasm.DataSegment{
			{Flags: 0, MemIdx: 0, Offset: []byte{wasm.OpI32Const, 0, wasm.OpEnd}, Init: []byte("hello")},
			{Flags: 1, Init: []byte("world")},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Data) != 2 {
		t.Fatalf("expected 2 data segments, got %d", len(parsed.Data))
	}
	if !bytes.Equal(parsed.Data[0].Init, []byte("hello")) {
		t.Errorf("data 0 init mismatch")
	}
	if !bytes.Equal(parsed.Data[1].Init, []byte("world")) {
		t.Errorf("data 1 init mismatch")
	}
}

func TestEncodeElements(t *testing.T) {
	m := &wasm.Module{
		Types:  []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:  []uint32{0, 0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 10}}},
		Elements: []wasm.Element{
			{Flags: 0, Offset: []byte{wasm.OpI32Const, 0, wasm.OpEnd}, FuncIdxs: []uint32{0, 1}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(parsed.Elements))
	}
	if len(parsed.Elements[0].FuncIdxs) != 2 {
		t.Errorf("expected 2 func indices, got %d", len(parsed.Elements[0].FuncIdxs))
	}
}

func TestEncodeCustomSections(t *testing.T) {
	m := &wasm.Module{
		CustomSections: []wasm.CustomSection{
			{Name: "name", Data: []byte{1, 2, 3}},
			{Name: "debug", Data: []byte{4, 5, 6, 7}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.CustomSections) != 2 {
		t.Fatalf("expected 2 custom sections, got %d", len(parsed.CustomSections))
	}
	if parsed.CustomSections[0].Name != "name" {
		t.Errorf("section 0 name mismatch")
	}
	if !bytes.Equal(parsed.CustomSections[1].Data, []byte{4, 5, 6, 7}) {
		t.Errorf("section 1 data mismatch")
	}
}

func TestEncodeStart(t *testing.T) {
	startIdx := uint32(0)
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Code:  []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
		Start: &startIdx,
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if parsed.Start == nil {
		t.Fatal("expected start section")
	}
	if *parsed.Start != 0 {
		t.Errorf("expected start=0, got %d", *parsed.Start)
	}
}

func TestEncodeDataCount(t *testing.T) {
	count := uint32(1)
	m := &wasm.Module{
		Memories:  []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		DataCount: &count,
		Data:      []wasm.DataSegment{{Flags: 1, Init: []byte{1}}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if parsed.DataCount == nil {
		t.Fatal("expected data count")
	}
	if *parsed.DataCount != 1 {
		t.Errorf("expected count=1, got %d", *parsed.DataCount)
	}
}

func TestEncodeTags(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: []wasm.ValType{wasm.ValI32}, Results: nil}},
		Tags:  []wasm.TagType{{Attribute: 0, TypeIdx: 0}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(parsed.Tags))
	}
	if parsed.Tags[0].TypeIdx != 0 {
		t.Errorf("tag type index mismatch")
	}
}

func TestModuleRoundTrip(t *testing.T) {
	startIdx := uint32(0)
	max := uint64(10)

	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: nil, Results: nil},
			{Params: []wasm.ValType{wasm.ValI32, wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "log", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
		},
		Funcs:    []uint32{0, 1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1, Max: &max}}},
		Tables:   []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}, Init: []byte{wasm.OpI32Const, 0, wasm.OpEnd}},
		},
		Exports: []wasm.Export{
			{Name: "main", Kind: wasm.KindFunc, Idx: 1},
		},
		Start: &startIdx,
		Code: []wasm.FuncBody{
			{Code: []byte{wasm.OpEnd}},
			{Locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, Code: []byte{wasm.OpLocalGet, 0, wasm.OpLocalGet, 1, wasm.OpI32Add, wasm.OpEnd}},
		},
		Data: []wasm.DataSegment{
			{Flags: 0, MemIdx: 0, Offset: []byte{wasm.OpI32Const, 0, wasm.OpEnd}, Init: []byte("test")},
		},
		CustomSections: []wasm.CustomSection{
			{Name: "custom", Data: []byte{1, 2, 3}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	// Re-encode and compare
	data2 := parsed.Encode()
	if !bytes.Equal(data, data2) {
		t.Error("round-trip produced different output")
	}
}
