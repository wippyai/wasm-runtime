package wasm_test

import (
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestValidate_Valid(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
			{Params: nil, Results: nil},
		},
		Funcs:    []uint32{0, 1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "add", Kind: wasm.KindFunc, Idx: 0},
			{Name: "memory", Kind: wasm.KindMemory, Idx: 0},
		},
	}

	if err := m.Validate(); err != nil {
		t.Errorf("valid module failed validation: %v", err)
	}
}

func TestValidate_InvalidTypeIndex(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: nil, Results: nil},
		},
		Funcs: []uint32{5}, // Invalid: references type index 5, but only 1 type exists
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid type index")
	}
	if !strings.Contains(err.Error(), "invalid type index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidFunctionExport(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: nil, Results: nil},
		},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "foo", Kind: wasm.KindFunc, Idx: 10},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid function export")
	}
	if !strings.Contains(err.Error(), "invalid function index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DuplicateExportName(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: nil, Results: nil},
		},
		Funcs:    []uint32{0, 0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "foo", Kind: wasm.KindFunc, Idx: 0},
			{Name: "foo", Kind: wasm.KindMemory, Idx: 0},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for duplicate export name")
	}
	if !strings.Contains(err.Error(), "duplicate export") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidStartSignature(t *testing.T) {
	startIdx := uint32(0)
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: nil},
		},
		Funcs: []uint32{0},
		Start: &startIdx,
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid start function signature")
	}
	if !strings.Contains(err.Error(), "start function must have signature") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DataCountMismatch(t *testing.T) {
	count := uint32(5)
	m := &wasm.Module{
		DataCount: &count,
		Data: []wasm.DataSegment{
			{MemIdx: 0, Init: []byte{1, 2, 3}},
		},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for data count mismatch")
	}
	if !strings.Contains(err.Error(), "data count section declares") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidMemoryIndex(t *testing.T) {
	m := &wasm.Module{
		Data: []wasm.DataSegment{
			{MemIdx: 5, Init: []byte{1}},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid memory index")
	}
	if !strings.Contains(err.Error(), "invalid memory index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ValidWithImports(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "add", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
		},
		Exports: []wasm.Export{
			{Name: "add", Kind: wasm.KindFunc, Idx: 0},
		},
	}

	if err := m.Validate(); err != nil {
		t.Errorf("valid module with imports failed validation: %v", err)
	}
}

func TestParseModuleValidate(t *testing.T) {
	// Create a minimal valid module
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: nil, Results: nil},
		},
		Funcs: []uint32{0},
	}

	data := m.Encode()

	parsed, err := wasm.ParseModuleValidate(data)
	if err != nil {
		t.Fatalf("ParseModuleValidate failed: %v", err)
	}

	if len(parsed.Types) != 1 {
		t.Errorf("expected 1 type, got %d", len(parsed.Types))
	}
}

func TestValidate_InvalidTableIndex(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Elements: []wasm.Element{
			{Flags: 0, TableIdx: 5, FuncIdxs: []uint32{0}},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid table index")
	}
	if !strings.Contains(err.Error(), "invalid table index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidGlobalExport(t *testing.T) {
	m := &wasm.Module{
		Exports: []wasm.Export{
			{Name: "g", Kind: wasm.KindGlobal, Idx: 10},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid global export")
	}
	if !strings.Contains(err.Error(), "invalid global index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidTagExport(t *testing.T) {
	m := &wasm.Module{
		Exports: []wasm.Export{
			{Name: "t", Kind: wasm.KindTag, Idx: 5},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid tag export")
	}
	if !strings.Contains(err.Error(), "invalid tag index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidTableExport(t *testing.T) {
	m := &wasm.Module{
		Exports: []wasm.Export{
			{Name: "t", Kind: wasm.KindTable, Idx: 3},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid table export")
	}
	if !strings.Contains(err.Error(), "invalid table index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_CodeCountMismatch(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0, 0, 0},
		Code: []wasm.FuncBody{
			{Code: []byte{wasm.OpEnd}},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for code count mismatch")
	}
	if !strings.Contains(err.Error(), "code section has") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_SharedMemoryNoMax(t *testing.T) {
	m := &wasm.Module{
		Memories: []wasm.MemoryType{
			{Limits: wasm.Limits{Min: 1, Shared: true}},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for shared memory without max")
	}
	if !strings.Contains(err.Error(), "shared memory must have maximum") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_SharedMemoryWithMax(t *testing.T) {
	max := uint64(10)
	m := &wasm.Module{
		Memories: []wasm.MemoryType{
			{Limits: wasm.Limits{Min: 1, Max: &max, Shared: true}},
		},
	}

	if err := m.Validate(); err != nil {
		t.Errorf("valid shared memory failed: %v", err)
	}
}

func TestValidate_MemoryMinExceedsMax32(t *testing.T) {
	max := wasm.MemoryMaxPages32 + 1
	m := &wasm.Module{
		Memories: []wasm.MemoryType{
			{Limits: wasm.Limits{Min: max}},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for memory min exceeding max pages")
	}
	if !strings.Contains(err.Error(), "min pages") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ImportedMemorySharedNoMax(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{
			{
				Module: "env",
				Name:   "mem",
				Desc: wasm.ImportDesc{
					Kind:   wasm.KindMemory,
					Memory: &wasm.MemoryType{Limits: wasm.Limits{Min: 1, Shared: true}},
				},
			},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for imported shared memory without max")
	}
	if !strings.Contains(err.Error(), "shared memory must have maximum") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidImportTypeIndex(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Imports: []wasm.Import{
			{Module: "env", Name: "f", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 99}},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid import type index")
	}
	if !strings.Contains(err.Error(), "invalid type index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidTagTypeIndex(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Tags:  []wasm.TagType{{TypeIdx: 10}},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid tag type index")
	}
	if !strings.Contains(err.Error(), "tag 0 references invalid type index") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PassiveElementNoTableCheck(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Elements: []wasm.Element{
			{Flags: 1, FuncIdxs: []uint32{0}},
		},
	}

	if err := m.Validate(); err != nil {
		t.Errorf("passive element validation failed: %v", err)
	}
}

func TestValidate_NoTypesWithFuncs(t *testing.T) {
	m := &wasm.Module{
		Funcs: []uint32{0},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for funcs without types")
	}
	if !strings.Contains(err.Error(), "no types defined") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ValidStartFunction(t *testing.T) {
	startIdx := uint32(0)
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Start: &startIdx,
	}

	if err := m.Validate(); err != nil {
		t.Errorf("valid start function failed: %v", err)
	}
}

func TestValidate_InvalidElementFuncIndex(t *testing.T) {
	m := &wasm.Module{
		Types:  []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:  []uint32{0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
		Elements: []wasm.Element{
			{Flags: 0, TableIdx: 0, FuncIdxs: []uint32{100}},
		},
	}

	err := m.Validate()
	if err == nil {
		t.Error("expected error for invalid element func index")
	}
	if !strings.Contains(err.Error(), "invalid function index") {
		t.Errorf("unexpected error: %v", err)
	}
}
