package wasm_test

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func ptrTo[T any](v T) *T { return &v }

func TestParseMinimalModule(t *testing.T) {
	data := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil module")
	}
}

func TestParseInvalidMagic(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for invalid magic")
	}
}

func TestParseInvalidVersion(t *testing.T) {
	data := []byte{0x00, 0x61, 0x73, 0x6D, 0x02, 0x00, 0x00, 0x00}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for invalid version")
	}
}

func TestParseTruncatedHeader(t *testing.T) {
	data := []byte{0x00, 0x61, 0x73}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated header")
	}
}

func TestParseSectionOrdering(t *testing.T) {
	m := &wasm.Module{
		Types:    []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
	}
	data := m.Encode()

	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Types) != 1 {
		t.Errorf("expected 1 type, got %d", len(parsed.Types))
	}
	if len(parsed.Funcs) != 1 {
		t.Errorf("expected 1 func, got %d", len(parsed.Funcs))
	}
	if len(parsed.Memories) != 1 {
		t.Errorf("expected 1 memory, got %d", len(parsed.Memories))
	}
}

func TestParseDataCountSection(t *testing.T) {
	count := uint32(2)
	m := &wasm.Module{
		Memories:  []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		DataCount: &count,
		Data: []wasm.DataSegment{
			{Flags: 1, Init: []byte{1, 2, 3}},
			{Flags: 1, Init: []byte{4, 5, 6}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if parsed.DataCount == nil {
		t.Fatal("DataCount should not be nil")
	}
	if *parsed.DataCount != 2 {
		t.Errorf("expected DataCount=2, got %d", *parsed.DataCount)
	}
	if len(parsed.Data) != 2 {
		t.Errorf("expected 2 data segments, got %d", len(parsed.Data))
	}
}

func TestParseCustomSection(t *testing.T) {
	m := &wasm.Module{
		CustomSections: []wasm.CustomSection{
			{Name: "test", Data: []byte{1, 2, 3}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.CustomSections) != 1 {
		t.Fatalf("expected 1 custom section, got %d", len(parsed.CustomSections))
	}
	if parsed.CustomSections[0].Name != "test" {
		t.Errorf("expected name 'test', got %q", parsed.CustomSections[0].Name)
	}
}

func TestParseImports(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}}},
		Imports: []wasm.Import{
			{Module: "env", Name: "add", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
			{Module: "env", Name: "mem", Desc: wasm.ImportDesc{Kind: wasm.KindMemory, Memory: &wasm.MemoryType{Limits: wasm.Limits{Min: 1}}}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(parsed.Imports))
	}
	if parsed.Imports[0].Module != "env" || parsed.Imports[0].Name != "add" {
		t.Errorf("unexpected import[0]: %+v", parsed.Imports[0])
	}
}

func TestParseExports(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "main", Kind: wasm.KindFunc, Idx: 0},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Exports) != 1 {
		t.Fatalf("expected 1 export, got %d", len(parsed.Exports))
	}
	if parsed.Exports[0].Name != "main" {
		t.Errorf("expected export name 'main', got %q", parsed.Exports[0].Name)
	}
}

func TestParseGlobals(t *testing.T) {
	m := &wasm.Module{
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}, Init: []byte{wasm.OpI32Const, 0x2a, wasm.OpEnd}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(parsed.Globals))
	}
	if parsed.Globals[0].Type.ValType != wasm.ValI32 {
		t.Errorf("expected i32, got %v", parsed.Globals[0].Type.ValType)
	}
	if !parsed.Globals[0].Type.Mutable {
		t.Error("expected mutable global")
	}
}

func TestParseStartSection(t *testing.T) {
	startIdx := uint32(0)
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Code:  []wasm.FuncBody{{Locals: nil, Code: []byte{wasm.OpEnd}}},
		Start: &startIdx,
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if parsed.Start == nil {
		t.Fatal("expected start function")
	}
	if *parsed.Start != 0 {
		t.Errorf("expected start=0, got %d", *parsed.Start)
	}
}

func TestParseTables(t *testing.T) {
	m := &wasm.Module{
		Tables: []wasm.TableType{
			{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 10}},
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
		t.Errorf("expected min=10, got %d", parsed.Tables[0].Limits.Min)
	}
}

func TestParseElements(t *testing.T) {
	m := &wasm.Module{
		Types:  []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:  []uint32{0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
		Elements: []wasm.Element{
			{Flags: 0, Offset: []byte{wasm.OpI32Const, 0, wasm.OpEnd}, FuncIdxs: []uint32{0}},
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
}

func TestParseMemoryLimits(t *testing.T) {
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
		t.Errorf("expected min=1, got %d", parsed.Memories[0].Limits.Min)
	}
	if parsed.Memories[0].Limits.Max == nil || *parsed.Memories[0].Limits.Max != 10 {
		t.Errorf("expected max=10")
	}
}

func TestParseSectionOutOfOrder(t *testing.T) {
	// Build a module with sections out of order manually
	// Memory section (5) followed by Function section (3) - invalid order
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, 0x01, 0x00, 0x01, // memory section: 1 memory, no max, 1 page
		0x03, 0x02, 0x01, 0x00, // function section: 1 function with type 0
	}

	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for out-of-order sections")
	}
}

func TestParseTruncatedSectionSize(t *testing.T) {
	// Valid header, section ID but no size
	data := []byte{
		0x00, 0x61, 0x73, 0x6D,
		0x01, 0x00, 0x00, 0x00,
		0x01, // type section ID, no size
	}

	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated section size")
	}
}

func TestParseTruncatedSectionData(t *testing.T) {
	// Section claims 100 bytes but only has 2
	data := []byte{
		0x00, 0x61, 0x73, 0x6D,
		0x01, 0x00, 0x00, 0x00,
		0x01, 0x64, // type section, size=100
		0x01, 0x60, // only 2 bytes
	}

	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated section data")
	}
}

func TestParseInvalidTypeForm(t *testing.T) {
	// Type section with invalid type form (not 0x60)
	data := []byte{
		0x00, 0x61, 0x73, 0x6D,
		0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, // type section, size=4
		0x01,       // 1 type
		0x99,       // invalid form (not 0x60)
		0x00, 0x00, // params/results
	}

	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for invalid type form")
	}
}

func TestParseEmptyModule(t *testing.T) {
	// Just magic and version, no sections
	data := []byte{
		0x00, 0x61, 0x73, 0x6D,
		0x01, 0x00, 0x00, 0x00,
	}

	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil module")
	}
}

func TestParseMultipleCustomSections(t *testing.T) {
	m := &wasm.Module{
		CustomSections: []wasm.CustomSection{
			{Name: "name", Data: []byte{1, 2, 3}},
			{Name: "debug", Data: []byte{4, 5, 6}},
			{Name: "sourcemap", Data: []byte{7, 8, 9}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.CustomSections) != 3 {
		t.Fatalf("expected 3 custom sections, got %d", len(parsed.CustomSections))
	}
}

func TestParseCodeWithLocals(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: []wasm.ValType{wasm.ValI32}}},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{
					{Count: 3, ValType: wasm.ValI32},
					{Count: 2, ValType: wasm.ValI64},
					{Count: 1, ValType: wasm.ValF32},
				},
				Code: []byte{wasm.OpI32Const, 42, wasm.OpEnd},
			},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Code) != 1 {
		t.Fatalf("expected 1 code body, got %d", len(parsed.Code))
	}
	if len(parsed.Code[0].Locals) != 3 {
		t.Errorf("expected 3 local entries, got %d", len(parsed.Code[0].Locals))
	}
	total := uint32(0)
	for _, l := range parsed.Code[0].Locals {
		total += l.Count
	}
	if total != 6 {
		t.Errorf("expected 6 total locals, got %d", total)
	}
}

func TestParseTagImport(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: []wasm.ValType{wasm.ValI32}, Results: nil}},
		Imports: []wasm.Import{
			{
				Module: "env",
				Name:   "my_tag",
				Desc: wasm.ImportDesc{
					Kind: wasm.KindTag,
					Tag:  &wasm.TagType{Attribute: 0, TypeIdx: 0},
				},
			},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(parsed.Imports))
	}
	if parsed.Imports[0].Desc.Kind != wasm.KindTag {
		t.Error("expected tag import")
	}
	if parsed.Imports[0].Desc.Tag.TypeIdx != 0 {
		t.Errorf("expected type index 0, got %d", parsed.Imports[0].Desc.Tag.TypeIdx)
	}
}

func TestParseTagSection(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: []wasm.ValType{wasm.ValI32}, Results: nil}},
		Tags: []wasm.TagType{
			{Attribute: 0, TypeIdx: 0},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(parsed.Tags))
	}
	if parsed.Tags[0].Attribute != 0 {
		t.Errorf("expected attribute 0, got %d", parsed.Tags[0].Attribute)
	}
	if parsed.Tags[0].TypeIdx != 0 {
		t.Errorf("expected type index 0, got %d", parsed.Tags[0].TypeIdx)
	}
}

func TestParseInvalidCompType(t *testing.T) {
	// Type section with invalid composite type byte (not 0x60, 0x5F, 0x5E)
	// Using rec type (0x4E) with subtype prefix (0x50), 0 supertypes, then invalid type
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x06, // type section, size=6
		0x01, // 1 type
		0x4E, // rec type marker
		0x01, // 1 subtype
		0x50, // 0x50=subtype prefix (non-final)
		0x00, // 0 supertypes
		0x99, // invalid composite type
	}

	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for invalid composite type")
	}
}

func TestParseFuncTypeWithRefParams(t *testing.T) {
	// Function type with funcref parameter and externref result
	// Must use TypeDefs to get proper ref type encoding
	ft := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc}}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeExtern}}},
		Params:     []wasm.ValType{wasm.ValRefNull},
		Results:    []wasm.ValType{wasm.ValRefNull},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{
			Kind: wasm.TypeDefKindSub,
			Sub: &wasm.SubType{
				Final:    true,
				CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft},
			},
		}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	// Parser optimizes simple subtypes into Types for compatibility
	if len(parsed.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(parsed.Types))
	}
	ft2 := &parsed.Types[0]
	if len(ft2.ExtParams) != 1 {
		t.Errorf("expected 1 ext param, got %d", len(ft2.ExtParams))
	}
	if len(ft2.ExtResults) != 1 {
		t.Errorf("expected 1 ext result, got %d", len(ft2.ExtResults))
	}
	// Verify the heap types
	if ft2.ExtParams[0].RefType.HeapType != wasm.HeapTypeFunc {
		t.Errorf("expected HeapTypeFunc, got %d", ft2.ExtParams[0].RefType.HeapType)
	}
	if ft2.ExtResults[0].RefType.HeapType != wasm.HeapTypeExtern {
		t.Errorf("expected HeapTypeExtern, got %d", ft2.ExtResults[0].RefType.HeapType)
	}
}

func TestParseStructWithPackedFields(t *testing.T) {
	// Struct with packed i8 and i16 fields
	st := wasm.StructType{
		Fields: []wasm.FieldType{
			{Type: wasm.StorageType{Kind: wasm.StorageKindPacked, Packed: wasm.PackedI8}, Mutable: false},
			{Type: wasm.StorageType{Kind: wasm.StorageKindPacked, Packed: wasm.PackedI16}, Mutable: true},
			{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}, Mutable: false},
		},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st},
		}}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.TypeDefs) != 1 {
		t.Fatalf("expected 1 type def, got %d", len(parsed.TypeDefs))
	}
	parsedStruct := parsed.TypeDefs[0].Sub.CompType.Struct
	if len(parsedStruct.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(parsedStruct.Fields))
	}
	if parsedStruct.Fields[0].Type.Kind != wasm.StorageKindPacked || parsedStruct.Fields[0].Type.Packed != wasm.PackedI8 {
		t.Error("expected i8 packed field")
	}
	if parsedStruct.Fields[1].Type.Kind != wasm.StorageKindPacked || parsedStruct.Fields[1].Type.Packed != wasm.PackedI16 {
		t.Error("expected i16 packed field")
	}
	if !parsedStruct.Fields[1].Mutable {
		t.Error("expected mutable field")
	}
}

func TestParseArrayWithRefElement(t *testing.T) {
	// Array with reference type element
	at := wasm.ArrayType{
		Element: wasm.FieldType{
			Type:    wasm.StorageType{Kind: wasm.StorageKindRef, RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc}},
			Mutable: false,
		},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &at},
		}}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.TypeDefs) != 1 {
		t.Fatalf("expected 1 type def, got %d", len(parsed.TypeDefs))
	}
	parsedArray := parsed.TypeDefs[0].Sub.CompType.Array
	if parsedArray.Element.Type.Kind != wasm.StorageKindRef {
		t.Error("expected ref storage kind")
	}
	if parsedArray.Element.Type.RefType.HeapType != wasm.HeapTypeFunc {
		t.Errorf("expected HeapTypeFunc, got %d", parsedArray.Element.Type.RefType.HeapType)
	}
}

func TestParseRecTypeWithFunc(t *testing.T) {
	// rec type containing a function type (tests skipFuncType path)
	ft := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc}}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRef, RefType: wasm.RefType{Nullable: false, HeapType: wasm.HeapTypeAny}}},
		Params:     []wasm.ValType{wasm.ValRefNull},
		Results:    []wasm.ValType{wasm.ValRef},
	}
	structType := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{Types: []wasm.SubType{
				{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
				{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &structType}},
			}}},
		},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.TypeDefs) != 1 {
		t.Fatalf("expected 1 type def, got %d", len(parsed.TypeDefs))
	}
	if parsed.TypeDefs[0].Kind != wasm.TypeDefKindRec {
		t.Error("expected rec type")
	}
}

func TestParseTableWithGCRefType(t *testing.T) {
	// Table with typed ref (0x63 or 0x64 prefix)
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{
			Kind: wasm.TypeDefKindSub,
			Sub: &wasm.SubType{
				Final:    true,
				CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}},
			},
		}},
		Tables: []wasm.TableType{{
			ElemType:    byte(wasm.ValRefNull),
			RefElemType: &wasm.RefType{Nullable: true, HeapType: 0},
			Limits:      wasm.Limits{Min: 10},
		}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(parsed.Tables))
	}
	if parsed.Tables[0].RefElemType == nil {
		t.Error("expected RefElemType to be set")
	}
}

func TestEncodeTableNonNullableRef(t *testing.T) {
	// Table with non-nullable ref type (hits the else branch in writeTableElemType)
	m := &wasm.Module{
		Tables: []wasm.TableType{{
			ElemType:    byte(wasm.ValRef),
			RefElemType: &wasm.RefType{Nullable: false, HeapType: 0}, // Non-nullable!
			Limits:      wasm.Limits{Min: 5},
		}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(parsed.Tables))
	}
	if parsed.Tables[0].RefElemType == nil {
		t.Fatal("expected RefElemType to be set")
	}
	if parsed.Tables[0].RefElemType.Nullable {
		t.Error("expected RefElemType to be non-nullable")
	}
}

func TestEncodeGlobalNonNullableRef(t *testing.T) {
	// Global with non-nullable ref type (hits the else branch in writeGlobalType)
	m := &wasm.Module{
		Globals: []wasm.Global{{
			Type: wasm.GlobalType{
				ValType: wasm.ValRef,
				Mutable: false,
				ExtType: &wasm.ExtValType{
					Kind:    wasm.ExtValKindRef,
					RefType: wasm.RefType{Nullable: false, HeapType: 0}, // Non-nullable!
				},
			},
			Init: []byte{wasm.OpRefNull, 0x00, wasm.OpEnd},
		}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(parsed.Globals))
	}
}

func TestEncodeExtValTypesSimple(t *testing.T) {
	// Function type with ExtParams containing simple val types (hits default case in writeExtValTypes)
	// Must use TypeDefs to go through writeFuncType which checks ExtParams/ExtResults
	ft := wasm.FuncType{
		ExtParams: []wasm.ExtValType{
			{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32},
			{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI64},
		},
		ExtResults: []wasm.ExtValType{
			{Kind: wasm.ExtValKindSimple, ValType: wasm.ValF32},
		},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{
			Kind: wasm.TypeDefKindFunc,
			Func: &ft,
		}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(parsed.Types))
	}
}

func TestEncodeStorageTypeNonNullableRef(t *testing.T) {
	// Struct with a field that has a non-nullable ref type (hits else branch in writeStorageType)
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{
			Kind: wasm.TypeDefKindSub,
			Sub: &wasm.SubType{
				Final: true,
				CompType: wasm.CompType{
					Kind: wasm.CompKindStruct,
					Struct: &wasm.StructType{
						Fields: []wasm.FieldType{{
							Type: wasm.StorageType{
								Kind:    wasm.StorageKindRef,
								RefType: wasm.RefType{Nullable: false, HeapType: 0}, // Non-nullable!
							},
							Mutable: false,
						}},
					},
				},
			},
		}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.TypeDefs) != 1 {
		t.Fatalf("expected 1 typedef, got %d", len(parsed.TypeDefs))
	}
}

func TestParseTableWithInitExpr(t *testing.T) {
	// Table with 0x40 0x00 prefix and init expression
	m := &wasm.Module{
		Tables: []wasm.TableType{{
			ElemType: byte(wasm.ValFuncRef),
			Limits:   wasm.Limits{Min: 5, Max: ptrTo(uint64(10))},
			Init:     []byte{wasm.OpRefNull, 0x70, wasm.OpEnd},
		}},
	}

	data := m.Encode()
	parsed, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}

	if len(parsed.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(parsed.Tables))
	}
	if parsed.Tables[0].Init == nil {
		t.Error("expected Init to be set")
	}
}

func TestParseInvalidTablePrefix(t *testing.T) {
	// 0x40 followed by non-zero byte
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section, size=4
		0x01,       // 1 table
		0x40, 0x01, // 0x40 followed by 0x01 (invalid, should be 0x00)
		0x70, // funcref (but we won't get here)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for invalid table prefix")
	}
}

func TestParseTagTruncated(t *testing.T) {
	// Tag section with truncated data
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x01, 0x7F, // 1 func type (i32) -> ()
		0x0D, 0x02, // tag section, size=2
		0x01, // 1 tag
		0x00, // attribute (but missing type idx)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated tag")
	}
}

func TestParseLimitsMinExceedsMax(t *testing.T) {
	// Memory with min > max
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x04, // memory section, size=4
		0x01, // 1 memory
		0x01, // has max flag
		0x0A, // min=10
		0x05, // max=5 (less than min)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for min > max")
	}
}

func TestParseUnknownSectionID(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0xFF, 0x01, // unknown section ID 0xFF, size 1
		0x00, // dummy data
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for unknown section ID")
	}
}

func TestParseTruncatedCodeSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type () -> ()
		0x03, 0x02, // function section
		0x01, 0x00, // 1 function, type 0
		0x0A, 0x05, // code section, size 5
		0x01, // 1 function
		0x03, // body size 3
		0x00, // 0 locals
		0x41, // i32.const (missing immediate)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated code section")
	}
}

func TestParseTruncatedTypeSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x02, // type section, size 2
		0x02, // claims 2 types but only provides partial data
		0x60, // func type marker (incomplete)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated type section")
	}
}

func TestParseTruncatedImportSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x02, 0x03, // import section, size 3
		0x01,       // 1 import
		0x01, 0x61, // module name "a" (but missing rest)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated import section")
	}
}

func TestParseDuplicateExport(t *testing.T) {
	// Module with duplicate export names
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0, 0},
		Exports: []wasm.Export{
			{Name: "foo", Kind: wasm.KindFunc, Idx: 0},
			{Name: "foo", Kind: wasm.KindFunc, Idx: 1}, // duplicate name
		},
	}
	data := m.Encode()
	_, err := wasm.ParseModuleValidate(data)
	if err == nil {
		t.Error("expected validation error for duplicate export")
	}
}

func TestParseInvalidImportKind(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type () -> ()
		0x02, 0x08, // import section, size 8
		0x01,       // 1 import
		0x01, 0x61, // module "a"
		0x01, 0x62, // name "b"
		0x05, // invalid import kind (should be 0-4)
		0x00, // dummy
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for invalid import kind")
	}
}

func TestParseTruncatedGlobalSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x06, 0x03, // global section, size 3
		0x01, // 1 global
		0x7F, // i32
		0x00, // immutable (but missing init expr)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated global section")
	}
}

func TestParseTruncatedElementSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x10, // 1 table: funcref, min=16
		0x09, 0x03, // element section, size 3
		0x01, // 1 element
		0x00, // flags (active, table 0) - missing offset expr
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated element section")
	}
}

func TestParseTruncatedDataSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory, min=1
		0x0B, 0x03, // data section, size 3
		0x01, // 1 data segment
		0x00, // flags (active, memory 0) - missing offset expr
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated data section")
	}
}

func TestParseTruncatedFuncTypeParams(t *testing.T) {
	// Type section with func type missing param types
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x03, // type section, size 3
		0x01, // 1 type
		0x60, // func type
		0x02, // 2 params (but no param data)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated func type params")
	}
}

func TestParseTruncatedFuncTypeResults(t *testing.T) {
	// Type section with func type missing result types
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section, size 4
		0x01, // 1 type
		0x60, // func type
		0x00, // 0 params
		0x02, // 2 results (but no result data)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated func type results")
	}
}

func TestParseTruncatedRefParam(t *testing.T) {
	// Func type with ref param missing heap type
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section, size 4
		0x01, // 1 type
		0x60, // func type
		0x01, // 1 param
		0x63, // ref null (but missing heap type)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated ref param")
	}
}

func TestParseTruncatedSubType(t *testing.T) {
	// SubType missing parent indices
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section, size 4
		0x01, // 1 type
		0x50, // sub (non-final) marker
		0x01, // 1 parent (but missing parent index)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated sub type")
	}
}

func TestParseTruncatedFieldType(t *testing.T) {
	// Struct with field missing mutability
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x05, // type section, size 5
		0x01, // 1 type
		0x5F, // struct marker
		0x01, // 1 field
		0x7F, // i32 type (but missing mutability byte)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated field type")
	}
}

func TestParseTruncatedArrayType(t *testing.T) {
	// Array with element type missing mutability
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section, size 4
		0x01, // 1 type
		0x5E, // array marker
		0x7F, // i32 element type (but missing mutability)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated array type")
	}
}

func TestParseTruncatedFunctionSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type () -> ()
		0x03, 0x02, // function section, size 2
		0x03, // claims 3 functions (but only has room for partial)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated function section")
	}
}

func TestParseTruncatedExportSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x03, 0x02, // function section
		0x01, 0x00, // 1 function, type 0
		0x07, 0x04, // export section, size 4
		0x01,       // 1 export
		0x01, 0x66, // name "f"
		0x00, // kind: function (but missing index)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated export section")
	}
}

func TestParseTruncatedCustomSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x00, 0x03, // custom section, size 3
		0x05,       // name length 5 (but only 2 more bytes available)
		0x61, 0x62, // partial name "ab"
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated custom section")
	}
}

func TestParseTruncatedTableSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x03, // table section, size 3
		0x01, // 1 table
		0x70, // funcref (but missing limits)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated table section")
	}
}

func TestParseTruncatedStorageType(t *testing.T) {
	// Storage type with ref type missing heap type
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x05, // type section, size 5
		0x01, // 1 type
		0x5F, // struct marker
		0x01, // 1 field
		0x64, // ref (non-null) - missing heap type
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated storage type")
	}
}

func TestParseTruncatedStartSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x08, 0x00, // start section, size 0 (missing function index)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated start section")
	}
}

func TestParseInvalidExportKind(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x07, 0x05, // export section, size 5
		0x01,       // 1 export
		0x01, 0x66, // name "f"
		0x05, // invalid export kind (should be 0-4)
		0x00, // index
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for invalid export kind")
	}
}

func TestParseTruncatedRecType(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x03, // type section, size 3
		0x01, // 1 type
		0x4E, // rec type marker
		0x02, // 2 subtypes (but missing data)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error for truncated rec type")
	}
}

// TDD: Target readFuncType line 363 - params OK, results fail
// To hit readFuncType (not skipFuncType), we need hasGCTypes=true
// so the first pass breaks early and readFuncType runs in second pass
func TestParseFuncTypeResultsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x0B, // type section, size 11
		0x02, // 2 types
		// Type 0: rec type (triggers hasGCTypes=true, bypasses skipFuncType)
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x5F, // struct
		0x01, // 1 field
		0x7F, // i32
		0x00, // immutable
		// Type 1: func type with truncated results (will fail in readFuncType)
		0x60,       // func type
		0x01, 0x7F, // 1 param: i32 (succeeds)
		0x02, // 2 results (but no result types - fails)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: readFuncType results truncated")
	}
}

// TDD: Target readFuncType line 359 - params fail
func TestParseFuncTypeParamsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x0A, // type section, size 10
		0x02, // 2 types
		// Type 0: rec type (triggers hasGCTypes=true)
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x5F, // struct
		0x01, // 1 field
		0x7F, // i32
		0x00, // immutable
		// Type 1: func type with truncated params
		0x60, // func type
		0x02, // 2 params (but no param types - fails in readFuncType)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: readFuncType params truncated")
	}
}

// TDD: Target readTagType line 1116 - attribute read fails
func TestParseTagAttributeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x0D, 0x01, // tag section, size 1
		0x01, // 1 tag (but no attribute byte)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: tag attribute truncated")
	}
}

// TDD: Target readTagType line 1120 - typeIdx read fails
func TestParseTagTypeIdxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x0D, 0x02, // tag section, size 2
		0x01, // 1 tag
		0x00, // attribute (but no typeIdx)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: tag typeIdx truncated")
	}
}

// TDD: Target readStorageType line 518 - ref heap type fails
func TestParseStructRefFieldHeapTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x05, // type section, size 5
		0x01, // 1 type
		0x5F, // struct marker
		0x01, // 1 field
		0x63, // ref null type (needs heap type LEB128 but truncated)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: ref field heap type truncated")
	}
}

// TDD: Target parseImportSection - import func type idx truncated
func TestParseImportFuncTypeIdxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x02, 0x06, // import section, size 6
		0x01,       // 1 import
		0x01, 0x61, // module "a"
		0x01, 0x62, // name "b"
		0x00, // kind: function (but no type idx)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: import func type idx truncated")
	}
}

// TDD: Target parseImportSection - import table limits truncated
func TestParseImportTableLimitsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x02, 0x07, // import section, size 7
		0x01,       // 1 import
		0x01, 0x61, // module "a"
		0x01, 0x62, // name "b"
		0x01, // kind: table
		0x70, // funcref (but no limits)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: import table limits truncated")
	}
}

// TDD: Target parseImportSection - import memory limits truncated
func TestParseImportMemoryLimitsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x02, 0x06, // import section, size 6
		0x01,       // 1 import
		0x01, 0x61, // module "a"
		0x01, 0x62, // name "b"
		0x02, // kind: memory (but no limits)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: import memory limits truncated")
	}
}

// TDD: Target parseImportSection - import global type truncated
func TestParseImportGlobalTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x02, 0x06, // import section, size 6
		0x01,       // 1 import
		0x01, 0x61, // module "a"
		0x01, 0x62, // name "b"
		0x03, // kind: global (but no type)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: import global type truncated")
	}
}

// TDD: Target parseImportSection - import tag truncated
func TestParseImportTagTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x02, 0x06, // import section, size 6
		0x01,       // 1 import
		0x01, 0x61, // module "a"
		0x01, 0x62, // name "b"
		0x04, // kind: tag (but no tag type)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: import tag truncated")
	}
}

// TDD: Target parseDataSection - data segment mode/memory truncated
func TestParseDataSegmentModeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory
		0x0B, 0x02, // data section, size 2
		0x01, // 1 segment
		0x02, // mode 2 (explicit memory index) but truncated
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: data segment mode truncated")
	}
}

// TDD: Target parseCodeSection - local count truncated
func TestParseCodeLocalCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x03, 0x02, // function section
		0x01, 0x00, // 1 function
		0x0A, 0x04, // code section, size 4
		0x01, // 1 body
		0x02, // body size 2
		0x01, // 1 local entry (but no local type info)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: code local count truncated")
	}
}

// TDD: Target parseElementSection - elem offset expr truncated
func TestParseElementOffsetTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x04, // element section, size 4
		0x01, // 1 element
		0x00, // flags: active table 0
		0x41, // i32.const (but no immediate or end)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element offset truncated")
	}
}

// TDD: Target readFieldType line 495 - storage type truncated (via struct)
func TestParseStructFieldStorageTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section, size 4
		0x01, // 1 type
		0x5F, // struct marker
		0x01, // 1 field (but no field data)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: struct field storage type truncated")
	}
}

// TDD: Target readCompType line 438 - kind byte truncated
func TestParseCompTypeKindTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x02, // type section, size 2
		0x01, // 1 type (but no type data)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: comp type kind truncated")
	}
}

// TDD: Target readCompType line 452 - struct type fails
func TestParseCompTypeStructFails(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x03, // type section, size 3
		0x01, // 1 type
		0x5F, // struct marker
		// missing field count
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: struct type field count truncated")
	}
}

// TDD: Target readCompType line 459 - array type fails
func TestParseCompTypeArrayFails(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x03, // type section, size 3
		0x01, // 1 type
		0x5E, // array marker
		// missing element type
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: array element type truncated")
	}
}

// TDD: Target readStructType line 471 - field count truncated
func TestParseStructTypeFieldCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x02, // type section, size 2
		0x01, // 1 type
		0x5F, // struct marker (no field count follows)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: struct field count truncated")
	}
}

// TDD: Target readStructType line 477 - reading 2nd field fails
func TestParseStructTypeSecondFieldFails(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x07, // type section, size 7
		0x01,       // 1 type
		0x5F,       // struct marker
		0x02,       // 2 fields
		0x7F, 0x00, // first field: i32, immutable (OK)
		0x7E, // second field: i64 (but missing mutability)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: second struct field truncated")
	}
}

// TDD: Target parseDataSection line 848 - flags truncated
func TestParseDataSectionFlagsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory, 0-1 pages
		0x0B, 0x02, // data section, size 2
		0x01, // 1 segment (but no flags)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: data segment flags truncated")
	}
}

// TDD: Target parseDataSection line 852 - invalid flags
func TestParseDataSectionInvalidFlags(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory, 0-1 pages
		0x0B, 0x03, // data section, size 3
		0x01, // 1 segment
		0x05, // invalid flags (>2)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: invalid data segment flags")
	}
}

// TDD: Target parseDataSection line 862 - memIdx truncated (flags=2)
func TestParseDataSectionMemIdxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory
		0x0B, 0x03, // data section, size 3
		0x01, // 1 segment
		0x02, // flags=2 (active with memIdx, but memIdx missing)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: memIdx truncated")
	}
}

// TDD: Target parseDataSection line 876 - initLen truncated
func TestParseDataSectionInitLenTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory
		0x0B, 0x06, // data section, size 6
		0x01,             // 1 segment
		0x00,             // flags=0 (active, table 0)
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		// missing init length
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: data init length truncated")
	}
}

// TDD: Target parseDataSection line 880 - init bytes truncated
func TestParseDataSectionInitBytesTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory
		0x0B, 0x08, // data section, size 8
		0x01,             // 1 segment
		0x00,             // flags=0
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		0x05, // init length 5 (but only 1 byte follows)
		0xAA,
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: data init bytes truncated")
	}
}

// TDD: Target parseFunctionSection line 596 - func count truncated
func TestParseFunctionSectionCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x03, 0x01, // function section, size 1 (but no count byte value fits)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: function section count truncated")
	}
}

// TDD: Target parseCodeSection line 785 - body count truncated
func TestParseCodeSectionCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x03, 0x02, // function section
		0x01, 0x00, // 1 function
		0x0A, 0x00, // code section, size 0 (no body count)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: code section count truncated")
	}
}

// TDD: Target readTableType line 1014 - ref type truncated
func TestParseTableTypeRefTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x01, // table section, size 1
		0x01, // 1 table (but no ref type)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: table ref type truncated")
	}
}

// TDD: Target readTableType line 1031 - limits truncated
func TestParseTableTypeLimitsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x02, // table section, size 2
		0x01, // 1 table
		0x70, // funcref (but no limits)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: table limits truncated")
	}
}

// TDD: Target readRefType line 1063 - heap type truncated
func TestParseRefTypeHeapTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x03, // table section, size 3
		0x01, // 1 table
		0x64, // ref (non-nullable, needs heap type)
		// missing heap type
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: ref heap type truncated")
	}
}

// TDD: Target parseElementSection line 699 - count truncated
func TestParseElementSectionCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x00, // element section, size 0 (no count)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element count truncated")
	}
}

// TDD: Target parseElementSection line 703 - flags truncated
func TestParseElementSectionFlagsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x01, // element section, size 1
		0x01, // 1 element (but no flags)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element flags truncated")
	}
}

// TDD: Target readCompType via SubTypeByte (0x50) - comptype truncated
func TestParseSubTypeCompTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x05, // type section, size 5
		0x01, // 1 type
		0x4E, // rec type marker (triggers GC path)
		0x01, // 1 subtype in rec
		0x50, // sub (non-final) marker
		0x00, // 0 parents (but no comp type follows)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: subtype comp type truncated")
	}
}

// TDD: Target readCompType line 445 - func type fails via subtype
func TestParseSubTypeFuncTypeFails(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x07, // type section, size 7
		0x01, // 1 type
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x50, // sub marker
		0x00, // 0 parents
		0x60, // func type marker
		0x02, // 2 params (but no param types)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: subtype func type params truncated")
	}
}

// TDD: Target readCompType line 452 - struct type fails via subtype
func TestParseSubTypeStructTypeFails(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x06, // type section, size 6
		0x01, // 1 type
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x50, // sub marker
		0x00, // 0 parents
		0x5F, // struct marker (but no field count)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: subtype struct type truncated")
	}
}

// TDD: Target readCompType line 459 - array type fails via subtype
func TestParseSubTypeArrayTypeFails(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x06, // type section, size 6
		0x01, // 1 type
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x50, // sub marker
		0x00, // 0 parents
		0x5E, // array marker (but no element type)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: subtype array type truncated")
	}
}

// TDD: Target readCompType line 464 - invalid comp type via subtype
func TestParseSubTypeInvalidCompType(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x06, // type section, size 6
		0x01, // 1 type
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x50, // sub marker
		0x00, // 0 parents
		0x99, // invalid comp type marker
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: invalid comp type")
	}
}

// TDD: Target readSubTypeWithPrefix line 389 - parent count truncated
func TestParseSubTypeParentCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section, size 4
		0x01, // 1 type
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x50, // sub marker (but no parent count)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: subtype parent count truncated")
	}
}

// TDD: Target readSubTypeWithPrefix line 395 - parent index truncated
func TestParseSubTypeParentIndexTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x05, // type section, size 5
		0x01, // 1 type
		0x4E, // rec type marker
		0x01, // 1 subtype in rec
		0x50, // sub marker
		0x02, // 2 parents (but no parent indices)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: subtype parent index truncated")
	}
}

// TDD: Target readArrayType error path (line 487) via direct array type
func TestParseArrayTypeElementTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x03, // type section, size 3
		0x01, // 1 type
		0x5E, // array marker (no element type follows)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: array element type truncated")
	}
}

// TDD: Target readStorageType error path (line 519) - ref type heap truncated
func TestParseStorageTypeRefHeapTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x06, // type section, size 6
		0x01, // 1 type
		0x5F, // struct marker
		0x01, // 1 field
		0x64, // ref (non-nullable, needs heap type, but none)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: storage type ref heap truncated")
	}
}

// TDD: Target parseFunctionSection line 601 - func type idx truncated
func TestParseFunctionSectionTypeIdxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x03, 0x02, // function section, size 2
		0x02, // 2 functions (but only enough bytes for first, if any)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: function type idx truncated")
	}
}

// TDD: Target parseGlobalSection line 640 - global type truncated
func TestParseGlobalSectionTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x06, 0x02, // global section, size 2
		0x01, // 1 global (but no type data)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: global type truncated")
	}
}

// TDD: Target parseGlobalSection line 647 - global init expr truncated
func TestParseGlobalSectionInitExprTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x06, 0x04, // global section, size 4
		0x01, // 1 global
		0x7F, // i32 type
		0x00, // immutable (but no init expr)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: global init expr truncated")
	}
}

// TDD: Target parseExportSection line 669 - export name truncated
func TestParseExportSectionNameTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x07, 0x02, // export section, size 2
		0x01, // 1 export
		0x05, // name length 5 (but no name bytes)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: export name truncated")
	}
}

// TDD: Target parseExportSection line 673 - export kind truncated
func TestParseExportSectionKindTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x07, 0x04, // export section, size 4
		0x01,       // 1 export
		0x01, 0x66, // name "f" (but no kind)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: export kind truncated")
	}
}

// TDD: Target parseExportSection line 681 - export idx truncated
func TestParseExportSectionIdxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x07, 0x05, // export section, size 5
		0x01,       // 1 export
		0x01, 0x66, // name "f"
		0x00, // kind: func (but no idx)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: export idx truncated")
	}
}

// TDD: Target parseCodeSection line 789 - body size truncated
func TestParseCodeSectionBodySizeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x03, 0x02, // function section
		0x01, 0x00, // 1 function
		0x0A, 0x02, // code section, size 2
		0x01, // 1 body (but no body size)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: code body size truncated")
	}
}

// TDD: Target parseCodeSection line 801 - local count truncated
func TestParseCodeSectionLocalCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, // type section
		0x01, 0x60, 0x00, 0x00, // 1 func type
		0x03, 0x02, // function section
		0x01, 0x00, // 1 function
		0x0A, 0x04, // code section, size 4
		0x01, // 1 body
		0x02, // body size 2
		// missing local count
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: code local count truncated")
	}
}

// TDD: Target parseDataCountSection line 891 - count truncated
func TestParseDataCountSectionTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x0C, 0x00, // data count section, size 0 (no count)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: data count truncated")
	}
}

// TDD: Target readTableType line 1028 - limits flag truncated
func TestParseTableTypeLimitsFlagsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x02, // table section, size 2
		0x01, // 1 table
		0x70, // funcref (but no limits flags)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: table limits flags truncated")
	}
}

// TDD: Target readTableType line 1034 - limits min truncated
func TestParseTableTypeLimitsMinTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x03, // table section, size 3
		0x01, // 1 table
		0x70, // funcref
		0x00, // limits flags: no max (but missing min)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: table limits min truncated")
	}
}

// TDD: Target readTableType line 1042 - limits max truncated
func TestParseTableTypeLimitsMaxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section, size 4
		0x01, // 1 table
		0x70, // funcref
		0x01, // limits flags: has max
		0x00, // min=0 (but missing max)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: table limits max truncated")
	}
}

// TDD: Target copyInitExprImmediate line 1162 - block type truncated
func TestParseInitExprBlockTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x06, 0x05, // global section, size 5
		0x01, // 1 global
		0x7F, // i32 type
		0x00, // immutable
		0x02, // block instruction (but no block type)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: block type truncated")
	}
}

// TDD: Target copyBytes line 1227 - copy bytes truncated
func TestParseCopyBytesTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x06, 0x0A, // global section, size 10
		0x01, // 1 global
		0x7F, // i32 type
		0x00, // immutable
		0xFD, // SIMD prefix
		0x0C, // v128.const opcode
		// needs 16 bytes for v128, but we'll provide fewer
		0x01, 0x02, 0x03,
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: v128 bytes truncated")
	}
}

// TDD: Target parseElementSection line 708 - invalid element flags
func TestParseElementSectionInvalidFlags(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x03, // element section, size 3
		0x01, // 1 element
		0x08, // invalid flags (>7)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: invalid element flags")
	}
}

// TDD: Target parseElementSection line 722 - table idx truncated (flags=2)
func TestParseElementSectionTableIdxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x03, // element section, size 3
		0x01, // 1 element
		0x02, // flags=2 (active, explicit table idx)
		// missing table idx
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element table idx truncated")
	}
}

// TDD: Target parseElementSection line 748 - elemkind truncated (flags=1)
func TestParseElementSectionElemKindTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x03, // element section, size 3
		0x01, // 1 element
		0x01, // flags=1 (passive, with elemkind - but missing it)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element elemkind truncated")
	}
}

// TDD: Target parseElementSection line 756 - vec count truncated
func TestParseElementSectionVecCountTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x07, // element section
		0x01,             // 1 element
		0x00,             // flags=0 (active, table 0, no elemkind)
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		// missing vec count
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element vec count truncated")
	}
}

// TDD: Target parseElementSection line 772 - func idx truncated
func TestParseElementSectionFuncIdxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x08, // element section
		0x01,             // 1 element
		0x00,             // flags=0 (active)
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		0x02, // vec count = 2 (but no func indices)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element func idx truncated")
	}
}

// TDD: Target parseElementSection line 740 - reftype truncated (flags=5)
func TestParseElementSectionRefTypeTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x03, // element section
		0x01, // 1 element
		0x05, // flags=5 (declarative, with exprs and reftype - but missing)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element reftype truncated")
	}
}

// TDD: Target parseElementSection line 764 - expr truncated (flags=4)
func TestParseElementSectionExprTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x04, 0x04, // table section
		0x01, 0x70, 0x00, 0x01, // 1 table
		0x09, 0x08, // element section
		0x01,             // 1 element
		0x04,             // flags=4 (active, no explicit table, with exprs)
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		0x01, // vec count = 1
		// missing expr
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: element expr truncated")
	}
}

// TDD: Target parseDataSection line 870 - data offset truncated (flags=0)
func TestParseDataSectionOffsetTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, 0x00, 0x01, // 1 memory
		0x0B, 0x04, // data section
		0x01, // 1 segment
		0x00, // flags=0 (active)
		0x41, // i32.const (but no immediate or end)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: data offset truncated")
	}
}

// TDD: Target readGlobalType line 1093 - mutability truncated
func TestParseGlobalTypeMutabilityTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x06, 0x03, // global section
		0x01, // 1 global
		0x7F, // i32 (but no mutability byte)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: global mutability truncated")
	}
}

// TDD: Target readLimits line 1100 - limits flags truncated
func TestParseLimitsFlagsTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x01, // memory section, size 1
		0x01, // 1 memory (but no limits)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: limits flags truncated")
	}
}

// TDD: Target readLimits line 1104 - limits min truncated
func TestParseLimitsMinTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x02, // memory section
		0x01, // 1 memory
		0x00, // limits flags=0 (but no min)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: limits min truncated")
	}
}

// TDD: Target readLimits line 1110 - limits max truncated
func TestParseLimitsMaxTruncated(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, // memory section
		0x01, // 1 memory
		0x01, // limits flags=1 (has max)
		0x00, // min=0 (but no max)
	}
	_, err := wasm.ParseModule(data)
	if err == nil {
		t.Error("expected error: limits max truncated")
	}
}
