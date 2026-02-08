package ast

import (
	"testing"
)

func TestFuncTypeEqual(t *testing.T) {
	tests := []struct {
		name  string
		a, b  FuncType
		equal bool
	}{
		{
			"empty",
			FuncType{},
			FuncType{},
			true,
		},
		{
			"same_params",
			FuncType{Params: []ValType{ValTypeI32, ValTypeI64}},
			FuncType{Params: []ValType{ValTypeI32, ValTypeI64}},
			true,
		},
		{
			"same_results",
			FuncType{Results: []ValType{ValTypeF32}},
			FuncType{Results: []ValType{ValTypeF32}},
			true,
		},
		{
			"same_both",
			FuncType{Params: []ValType{ValTypeI32}, Results: []ValType{ValTypeI64}},
			FuncType{Params: []ValType{ValTypeI32}, Results: []ValType{ValTypeI64}},
			true,
		},
		{
			"diff_params_len",
			FuncType{Params: []ValType{ValTypeI32}},
			FuncType{Params: []ValType{ValTypeI32, ValTypeI64}},
			false,
		},
		{
			"diff_params_type",
			FuncType{Params: []ValType{ValTypeI32}},
			FuncType{Params: []ValType{ValTypeI64}},
			false,
		},
		{
			"diff_results_len",
			FuncType{Results: []ValType{ValTypeI32}},
			FuncType{Results: []ValType{ValTypeI32, ValTypeI64}},
			false,
		},
		{
			"diff_results_type",
			FuncType{Results: []ValType{ValTypeF32}},
			FuncType{Results: []ValType{ValTypeF64}},
			false,
		},
		{
			"ref_types",
			FuncType{Params: []ValType{ValTypeFuncref}, Results: []ValType{ValTypeExternref}},
			FuncType{Params: []ValType{ValTypeFuncref}, Results: []ValType{ValTypeExternref}},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Equal(tt.b)
			if got != tt.equal {
				t.Errorf("FuncType.Equal() = %v, want %v", got, tt.equal)
			}
		})
	}
}

func TestValTypeConstants(t *testing.T) {
	// Verify value type constants match WASM spec
	tests := []struct {
		vt   ValType
		want byte
	}{
		{ValTypeI32, 0x7F},
		{ValTypeI64, 0x7E},
		{ValTypeF32, 0x7D},
		{ValTypeF64, 0x7C},
		{ValTypeFuncref, 0x70},
		{ValTypeExternref, 0x6F},
	}

	for _, tt := range tests {
		if byte(tt.vt) != tt.want {
			t.Errorf("ValType %d = 0x%02X, want 0x%02X", tt.vt, byte(tt.vt), tt.want)
		}
	}
}

func TestKindConstants(t *testing.T) {
	// Verify import/export kind constants match WASM spec
	tests := []struct {
		kind byte
		want byte
	}{
		{KindFunc, 0x00},
		{KindTable, 0x01},
		{KindMemory, 0x02},
		{KindGlobal, 0x03},
	}

	for _, tt := range tests {
		if tt.kind != tt.want {
			t.Errorf("Kind 0x%02X != 0x%02X", tt.kind, tt.want)
		}
	}
}

func TestSectionIDConstants(t *testing.T) {
	// Verify section ID constants match WASM spec
	tests := []struct {
		id   byte
		want byte
	}{
		{SectionCustom, 0},
		{SectionType, 1},
		{SectionImport, 2},
		{SectionFunc, 3},
		{SectionTable, 4},
		{SectionMemory, 5},
		{SectionGlobal, 6},
		{SectionExport, 7},
		{SectionStart, 8},
		{SectionElem, 9},
		{SectionCode, 10},
		{SectionData, 11},
		{SectionDataCount, 12},
	}

	for _, tt := range tests {
		if tt.id != tt.want {
			t.Errorf("Section ID 0x%02X != 0x%02X", tt.id, tt.want)
		}
	}
}

func TestElemModeConstants(t *testing.T) {
	if ElemModeActive != 0 {
		t.Error("ElemModeActive should be 0")
	}
	if ElemModePassive != 1 {
		t.Error("ElemModePassive should be 1")
	}
	if ElemModeActiveTable != 2 {
		t.Error("ElemModeActiveTable should be 2")
	}
	if ElemModeDeclarative != 3 {
		t.Error("ElemModeDeclarative should be 3")
	}
}

func TestRefTypeConstants(t *testing.T) {
	if RefTypeFuncref != 0x70 {
		t.Errorf("RefTypeFuncref = 0x%02X, want 0x70", RefTypeFuncref)
	}
	if RefTypeExternref != 0x6F {
		t.Errorf("RefTypeExternref = 0x%02X, want 0x6F", RefTypeExternref)
	}
}

func TestOpcodeConstants(t *testing.T) {
	// Verify opcode constants match WASM spec
	tests := []struct {
		op   byte
		want byte
	}{
		{OpUnreachable, 0x00},
		{OpNop, 0x01},
		{OpBlock, 0x02},
		{OpLoop, 0x03},
		{OpIf, 0x04},
		{OpElse, 0x05},
		{OpEnd, 0x0B},
		{OpBr, 0x0C},
		{OpBrIf, 0x0D},
		{OpBrTable, 0x0E},
		{OpReturn, 0x0F},
		{OpCall, 0x10},
		{OpCallIndirect, 0x11},
		{OpDrop, 0x1A},
		{OpSelect, 0x1B},
		{OpSelectTyped, 0x1C},
		{OpLocalGet, 0x20},
		{OpLocalSet, 0x21},
		{OpLocalTee, 0x22},
		{OpGlobalGet, 0x23},
		{OpGlobalSet, 0x24},
		{OpTableGet, 0x25},
		{OpTableSet, 0x26},
		{OpI32Const, 0x41},
		{OpI64Const, 0x42},
		{OpF32Const, 0x43},
		{OpF64Const, 0x44},
		{OpMemorySize, 0x3F},
		{OpMemoryGrow, 0x40},
		{OpRefNull, 0xD0},
		{OpRefIsNull, 0xD1},
		{OpRefFunc, 0xD2},
		{OpPrefixMisc, 0xFC},
	}

	for _, tt := range tests {
		if tt.op != tt.want {
			t.Errorf("Opcode 0x%02X != 0x%02X", tt.op, tt.want)
		}
	}
}

func TestMiscOpConstants(t *testing.T) {
	tests := []struct {
		op   uint32
		want uint32
	}{
		{MiscOpMemoryInit, 8},
		{MiscOpDataDrop, 9},
		{MiscOpMemoryCopy, 10},
		{MiscOpMemoryFill, 11},
		{MiscOpTableInit, 12},
		{MiscOpElemDrop, 13},
		{MiscOpTableCopy, 14},
		{MiscOpTableGrow, 15},
		{MiscOpTableSize, 16},
		{MiscOpTableFill, 17},
	}

	for _, tt := range tests {
		if tt.op != tt.want {
			t.Errorf("MiscOp %d != %d", tt.op, tt.want)
		}
	}
}
