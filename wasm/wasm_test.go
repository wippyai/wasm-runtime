package wasm_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestInstructionRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		instrs []wasm.Instruction
	}{
		{
			"simple",
			[]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"locals",
			[]wasm.Instruction{
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
				{Opcode: wasm.OpI32Add},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"block",
			[]wasm.Instruction{
				{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -1}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"if_else",
			[]wasm.Instruction{
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpDrop},
				{Opcode: wasm.OpElse},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
				{Opcode: wasm.OpDrop},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"loop",
			[]wasm.Instruction{
				{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"call",
			[]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"return_call",
			[]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpReturnCall, Imm: wasm.CallImm{FuncIdx: 5}},
			},
		},
		{
			"return_call_indirect",
			[]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
				{Opcode: wasm.OpReturnCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 2, TableIdx: 1}},
			},
		},
		{
			"memory",
			[]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
				{Opcode: wasm.OpI32Load, Imm: wasm.MemoryImm{Align: 2, Offset: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpI32Store, Imm: wasm.MemoryImm{Align: 2, Offset: 0}},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"globals",
			[]wasm.Instruction{
				{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpI32Add},
				{Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 0}},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"br_table",
			[]wasm.Instruction{
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0, 1, 2}, Default: 3}},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"floats",
			[]wasm.Instruction{
				{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: 3.14}},
				{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: 2.718281828}},
				{Opcode: wasm.OpDrop},
				{Opcode: wasm.OpDrop},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			"i64",
			[]wasm.Instruction{
				{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 0x7FFFFFFFFFFFFFFF}},
				{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: -1}},
				{Opcode: wasm.OpI64Add},
				{Opcode: wasm.OpDrop},
				{Opcode: wasm.OpEnd},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions(tt.instrs)
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}

			if len(decoded) != len(tt.instrs) {
				t.Fatalf("instruction count: got %d, want %d", len(decoded), len(tt.instrs))
			}

			for i, want := range tt.instrs {
				got := decoded[i]
				if got.Opcode != want.Opcode {
					t.Errorf("instr %d: opcode got 0x%02x, want 0x%02x", i, got.Opcode, want.Opcode)
				}
			}
		})
	}
}

func TestTypedSelectInstruction(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{Types: []wasm.ValType{wasm.ValI32}}},
		{Opcode: wasm.OpEnd},
	}

	encoded := wasm.EncodeInstructions(instrs)
	decoded, err := wasm.DecodeInstructions(encoded)
	if err != nil {
		t.Fatalf("DecodeInstructions error: %v", err)
	}

	if len(decoded) != len(instrs) {
		t.Fatalf("instruction count: got %d, want %d", len(decoded), len(instrs))
	}

	selectInstr := decoded[3]
	if selectInstr.Opcode != wasm.OpSelectType {
		t.Errorf("expected OpSelectType, got 0x%02x", selectInstr.Opcode)
	}

	imm, ok := selectInstr.Imm.(wasm.SelectTypeImm)
	if !ok {
		t.Fatalf("expected SelectTypeImm, got %T", selectInstr.Imm)
	}

	if len(imm.Types) != 1 || imm.Types[0] != wasm.ValI32 {
		t.Errorf("expected [i32], got %v", imm.Types)
	}
}

func TestDataCountSection(t *testing.T) {
	count := uint32(2)
	m := &wasm.Module{
		Types:     []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:     []uint32{0},
		Memories:  []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		DataCount: &count,
		Data: []wasm.DataSegment{
			{MemIdx: 0, Offset: []byte{wasm.OpI32Const, 0, wasm.OpEnd}, Init: []byte{1, 2, 3}},
			{MemIdx: 0, Offset: []byte{wasm.OpI32Const, 100, wasm.OpEnd}, Init: []byte{4, 5, 6}},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if decoded.DataCount == nil {
		t.Fatal("DataCount should not be nil")
	}

	if *decoded.DataCount != 2 {
		t.Errorf("DataCount: got %d, want 2", *decoded.DataCount)
	}

	if len(decoded.Data) != 2 {
		t.Errorf("Data segments: got %d, want 2", len(decoded.Data))
	}
}

func TestBinaryReaderWriter(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32, wasm.ValI64}, Results: []wasm.ValType{wasm.ValF32}},
			{Params: nil, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "func1", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
			{Module: "env", Name: "memory", Desc: wasm.ImportDesc{Kind: wasm.KindMemory, Memory: &wasm.MemoryType{Limits: wasm.Limits{Min: 1, Max: ptr(256)}}}},
		},
		Funcs:    []uint32{1},
		Tables:   []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 10}}},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}, Init: []byte{wasm.OpI32Const, 42, wasm.OpEnd}},
		},
		Exports: []wasm.Export{
			{Name: "main", Kind: wasm.KindFunc, Idx: 1},
			{Name: "mem", Kind: wasm.KindMemory, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 2, ValType: wasm.ValI32}},
				Code:   []byte{wasm.OpI32Const, 1, wasm.OpEnd},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Types) != 2 {
		t.Errorf("types: got %d, want 2", len(decoded.Types))
	}
	if len(decoded.Imports) != 2 {
		t.Errorf("imports: got %d, want 2", len(decoded.Imports))
	}
	if len(decoded.Funcs) != 1 {
		t.Errorf("funcs: got %d, want 1", len(decoded.Funcs))
	}
	if len(decoded.Tables) != 1 {
		t.Errorf("tables: got %d, want 1", len(decoded.Tables))
	}
	if len(decoded.Exports) != 2 {
		t.Errorf("exports: got %d, want 2", len(decoded.Exports))
	}
	if len(decoded.Globals) != 1 {
		t.Errorf("globals: got %d, want 1", len(decoded.Globals))
	}
}

func ptr(v uint64) *uint64 {
	return &v
}

func TestGCInstructionsRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"struct.new", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCStructNew, TypeIdx: 5}}},
		{"struct.new_default", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCStructNewDefault, TypeIdx: 10}}},
		{"struct.get", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCStructGet, TypeIdx: 3, FieldIdx: 2}}},
		{"struct.get_s", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCStructGetS, TypeIdx: 1, FieldIdx: 0}}},
		{"struct.get_u", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCStructGetU, TypeIdx: 7, FieldIdx: 4}}},
		{"struct.set", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCStructSet, TypeIdx: 2, FieldIdx: 1}}},
		{"array.new", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayNew, TypeIdx: 8}}},
		{"array.new_default", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayNewDefault, TypeIdx: 9}}},
		{"array.new_fixed", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayNewFixed, TypeIdx: 4, Size: 100}}},
		{"array.new_data", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayNewData, TypeIdx: 6, DataIdx: 3}}},
		{"array.new_elem", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayNewElem, TypeIdx: 5, ElemIdx: 2}}},
		{"array.get", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayGet, TypeIdx: 11}}},
		{"array.get_s", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayGetS, TypeIdx: 12}}},
		{"array.get_u", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayGetU, TypeIdx: 13}}},
		{"array.set", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArraySet, TypeIdx: 14}}},
		{"array.fill", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayFill, TypeIdx: 15}}},
		{"array.init_data", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayInitData, TypeIdx: 6, DataIdx: 4}}},
		{"array.init_elem", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayInitElem, TypeIdx: 7, ElemIdx: 5}}},
		{"array.len", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayLen}}},
		{"array.copy", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCArrayCopy, TypeIdx: 16, TypeIdx2: 17}}},
		{"ref.test", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCRefTest, HeapType: 42}}},
		{"ref.test_null", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCRefTestNull, HeapType: 43}}},
		{"ref.cast", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCRefCast, HeapType: 20}}},
		{"ref.cast_null", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCRefCastNull, HeapType: 21}}},
		{"br_on_cast", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCBrOnCast, CastFlags: wasm.CastFlagsBothNull, LabelIdx: 5, HeapType: wasm.HeapTypeAny, HeapType2: wasm.HeapTypeStruct}}},
		{"br_on_cast_fail", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCBrOnCastFail, CastFlags: wasm.CastFlagsSecondNull, LabelIdx: 3, HeapType: wasm.HeapTypeFunc, HeapType2: wasm.HeapTypeExtern}}},
		{"any.convert_extern", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCAnyConvertExtern}}},
		{"extern.convert_any", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCExternConvertAny}}},
		{"ref.i31", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCRefI31}}},
		{"i31.get_s", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCI31GetS}}},
		{"i31.get_u", wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.GCImm{SubOpcode: wasm.GCI31GetU}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			if decoded[0].Opcode != wasm.OpPrefixGC {
				t.Errorf("opcode: got 0x%02x, want 0x%02x", decoded[0].Opcode, wasm.OpPrefixGC)
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ")
			}
		})
	}
}

func TestGCInstructionsUnknownSubOpcode(t *testing.T) {
	badCode := []byte{wasm.OpPrefixGC, 0xFF, 0x01}
	_, err := wasm.DecodeInstructions(badCode)
	if err == nil {
		t.Error("expected error for unknown sub-opcode")
	}
}

func TestGCInstructionsTruncated(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"struct.new truncated", []byte{wasm.OpPrefixGC, byte(wasm.GCStructNew)}},
		{"struct.get missing field", []byte{wasm.OpPrefixGC, byte(wasm.GCStructGet), 0x01}},
		{"array.new_fixed missing size", []byte{wasm.OpPrefixGC, byte(wasm.GCArrayNewFixed), 0x01}},
		{"array.new_data missing dataidx", []byte{wasm.OpPrefixGC, byte(wasm.GCArrayNewData), 0x01}},
		{"array.new_elem missing elemidx", []byte{wasm.OpPrefixGC, byte(wasm.GCArrayNewElem), 0x01}},
		{"array.copy missing typeidx2", []byte{wasm.OpPrefixGC, byte(wasm.GCArrayCopy), 0x01}},
		{"ref.test missing heaptype", []byte{wasm.OpPrefixGC, byte(wasm.GCRefTest)}},
		{"br_on_cast missing labelidx", []byte{wasm.OpPrefixGC, byte(wasm.GCBrOnCast), 0x00}},
		{"br_on_cast missing heaptype1", []byte{wasm.OpPrefixGC, byte(wasm.GCBrOnCast), 0x00, 0x01}},
		{"br_on_cast missing heaptype2", []byte{wasm.OpPrefixGC, byte(wasm.GCBrOnCast), 0x00, 0x01, 0x6F}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := wasm.DecodeInstructions(tt.data)
			if err == nil {
				t.Errorf("expected error for truncated %s", tt.name)
			}
		})
	}
}

func TestInstructionsTruncated(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"block missing type", []byte{wasm.OpBlock}},
		{"br missing label", []byte{wasm.OpBr}},
		{"br_table missing count", []byte{wasm.OpBrTable}},
		{"call missing func", []byte{wasm.OpCall}},
		{"call_indirect missing type", []byte{wasm.OpCallIndirect}},
		{"local.get missing idx", []byte{wasm.OpLocalGet}},
		{"global.get missing idx", []byte{wasm.OpGlobalGet}},
		{"i32.load missing memarg", []byte{wasm.OpI32Load}},
		{"i32.const missing value", []byte{wasm.OpI32Const}},
		{"i64.const missing value", []byte{wasm.OpI64Const}},
		{"f32.const missing value", []byte{wasm.OpF32Const}},
		{"f64.const missing value", []byte{wasm.OpF64Const}},
		{"memory.size missing mem", []byte{wasm.OpMemorySize}},
		{"select_t missing count", []byte{wasm.OpSelectType}},
		{"try_table missing type", []byte{wasm.OpTryTable}},
		{"throw missing tag", []byte{wasm.OpThrow}},
		{"ref.null missing type", []byte{wasm.OpRefNull}},
		{"ref.func missing idx", []byte{wasm.OpRefFunc}},
		{"table.get missing idx", []byte{wasm.OpTableGet}},
		{"misc prefix only", []byte{wasm.OpPrefixMisc}},
		{"simd prefix only", []byte{wasm.OpPrefixSIMD}},
		{"atomic prefix only", []byte{wasm.OpPrefixAtomic}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := wasm.DecodeInstructions(tt.data)
			if err == nil {
				t.Errorf("expected error for truncated %s", tt.name)
			}
		})
	}
}

func TestSIMDInstructionsRoundTrip(t *testing.T) {
	lane := byte(3)
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"v128.load", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x00, MemArg: &wasm.MemoryImm{Align: 4, Offset: 16}}}},
		{"v128.store", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x0B, MemArg: &wasm.MemoryImm{Align: 4, Offset: 32}}}},
		{"v128.const", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x0C, V128Bytes: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}}},
		{"i8x16.shuffle", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x0D, V128Bytes: []byte{0, 16, 1, 17, 2, 18, 3, 19, 4, 20, 5, 21, 6, 22, 7, 23}}}},
		{"i8x16.extract_lane_s", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x15, LaneIdx: &lane}}},
		{"v128.load8_lane", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x54, MemArg: &wasm.MemoryImm{Align: 0, Offset: 8}, LaneIdx: &lane}}},
		{"v128.load32_zero", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x5C, MemArg: &wasm.MemoryImm{Align: 2, Offset: 0}}}},
		{"i8x16.add", wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: 0x6E}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ")
			}
		})
	}
}

func TestAtomicInstructionsRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"atomic.fence", wasm.Instruction{Opcode: wasm.OpPrefixAtomic, Imm: wasm.AtomicImm{SubOpcode: 0x03}}},
		{"memory.atomic.notify", wasm.Instruction{Opcode: wasm.OpPrefixAtomic, Imm: wasm.AtomicImm{SubOpcode: 0x00, MemArg: &wasm.MemoryImm{Align: 2, Offset: 0}}}},
		{"memory.atomic.wait32", wasm.Instruction{Opcode: wasm.OpPrefixAtomic, Imm: wasm.AtomicImm{SubOpcode: 0x01, MemArg: &wasm.MemoryImm{Align: 2, Offset: 16}}}},
		{"i32.atomic.load", wasm.Instruction{Opcode: wasm.OpPrefixAtomic, Imm: wasm.AtomicImm{SubOpcode: 0x10, MemArg: &wasm.MemoryImm{Align: 2, Offset: 8}}}},
		{"i32.atomic.store", wasm.Instruction{Opcode: wasm.OpPrefixAtomic, Imm: wasm.AtomicImm{SubOpcode: 0x17, MemArg: &wasm.MemoryImm{Align: 2, Offset: 4}}}},
		{"i32.atomic.rmw.add", wasm.Instruction{Opcode: wasm.OpPrefixAtomic, Imm: wasm.AtomicImm{SubOpcode: 0x1E, MemArg: &wasm.MemoryImm{Align: 2, Offset: 0}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ")
			}
		})
	}
}

func TestGCTypesRoundTrip(t *testing.T) {
	t.Run("struct type", func(t *testing.T) {
		structType := wasm.StructType{
			Fields: []wasm.FieldType{
				{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}, Mutable: false},
				{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI64}, Mutable: true},
				{Type: wasm.StorageType{Kind: wasm.StorageKindPacked, Packed: wasm.PackedI8}, Mutable: false},
			},
		}
		sub := wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &structType},
		}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &sub}},
		}

		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}

		if len(decoded.TypeDefs) != 1 {
			t.Fatalf("expected 1 typedef, got %d", len(decoded.TypeDefs))
		}
		if decoded.TypeDefs[0].Kind != wasm.TypeDefKindSub {
			t.Errorf("expected TypeDefKindSub, got %d", decoded.TypeDefs[0].Kind)
		}
		if decoded.TypeDefs[0].Sub.CompType.Kind != wasm.CompKindStruct {
			t.Errorf("expected struct comptype, got %d", decoded.TypeDefs[0].Sub.CompType.Kind)
		}
		if len(decoded.TypeDefs[0].Sub.CompType.Struct.Fields) != 3 {
			t.Errorf("expected 3 fields, got %d", len(decoded.TypeDefs[0].Sub.CompType.Struct.Fields))
		}
	})

	t.Run("array type", func(t *testing.T) {
		arrType := wasm.ArrayType{
			Element: wasm.FieldType{
				Type:    wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValF64},
				Mutable: true,
			},
		}
		sub := wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &arrType},
		}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &sub}},
		}

		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}

		if len(decoded.TypeDefs) != 1 {
			t.Fatalf("expected 1 typedef, got %d", len(decoded.TypeDefs))
		}
		if decoded.TypeDefs[0].Sub.CompType.Kind != wasm.CompKindArray {
			t.Errorf("expected array comptype")
		}
	})

	t.Run("rec type group", func(t *testing.T) {
		ft := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}}
		rec := wasm.RecType{
			Types: []wasm.SubType{
				{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
				{Final: false, Parents: []uint32{0}, CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
			},
		}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindRec, Rec: &rec}},
		}

		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}

		if len(decoded.TypeDefs) != 1 {
			t.Fatalf("expected 1 typedef, got %d", len(decoded.TypeDefs))
		}
		if decoded.TypeDefs[0].Kind != wasm.TypeDefKindRec {
			t.Errorf("expected TypeDefKindRec")
		}
		if len(decoded.TypeDefs[0].Rec.Types) != 2 {
			t.Errorf("expected 2 types in rec group, got %d", len(decoded.TypeDefs[0].Rec.Types))
		}
	})

	t.Run("sub with parents", func(t *testing.T) {
		ft := wasm.FuncType{Params: nil, Results: nil}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{
				{Kind: wasm.TypeDefKindFunc, Func: &ft},
				{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
					Final:    false,
					Parents:  []uint32{0},
					CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft},
				}},
			},
		}

		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}

		if len(decoded.TypeDefs) != 2 {
			t.Fatalf("expected 2 typedefs, got %d", len(decoded.TypeDefs))
		}
		if decoded.TypeDefs[1].Sub.Final {
			t.Error("expected non-final subtype")
		}
		if len(decoded.TypeDefs[1].Sub.Parents) != 1 || decoded.TypeDefs[1].Sub.Parents[0] != 0 {
			t.Errorf("expected parent [0], got %v", decoded.TypeDefs[1].Sub.Parents)
		}
	})

	t.Run("ref type in storage", func(t *testing.T) {
		structType := wasm.StructType{
			Fields: []wasm.FieldType{
				{
					Type: wasm.StorageType{
						Kind:    wasm.StorageKindRef,
						RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc},
					},
					Mutable: false,
				},
			},
		}
		sub := wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &structType},
		}
		m := &wasm.Module{
			TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &sub}},
		}

		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}

		field := decoded.TypeDefs[0].Sub.CompType.Struct.Fields[0]
		if field.Type.Kind != wasm.StorageKindRef {
			t.Errorf("expected ref storage kind, got %d", field.Type.Kind)
		}
		if !field.Type.RefType.Nullable {
			t.Error("expected nullable ref type")
		}
		if field.Type.RefType.HeapType != wasm.HeapTypeFunc {
			t.Errorf("expected HeapTypeFunc, got %d", field.Type.RefType.HeapType)
		}
	})

	t.Run("func with ext types", func(t *testing.T) {
		// Function types with ref type params are encoded normally (0x60).
		// ExtTypes are populated during parsing for ref types in params/results.
		// This tests that ref types in params are properly round-tripped.
		ft := wasm.FuncType{
			Params:  []wasm.ValType{wasm.ValI32, wasm.ValAnyRef},
			Results: []wasm.ValType{wasm.ValFuncRef},
		}
		m := &wasm.Module{
			Types: []wasm.FuncType{ft},
		}

		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}

		if len(decoded.Types) != 1 {
			t.Fatalf("expected 1 type, got %d", len(decoded.Types))
		}
		decodedFunc := decoded.Types[0]
		if len(decodedFunc.Params) != 2 {
			t.Errorf("expected 2 params, got %d", len(decodedFunc.Params))
		}
		if decodedFunc.Params[1] != wasm.ValAnyRef {
			t.Errorf("expected anyref param, got %v", decodedFunc.Params[1])
		}
		if len(decodedFunc.Results) != 1 || decodedFunc.Results[0] != wasm.ValFuncRef {
			t.Errorf("expected funcref result, got %v", decodedFunc.Results)
		}
	})
}

func TestTableWithInitExpression(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Tables: []wasm.TableType{
			{
				ElemType: byte(wasm.ValFuncRef),
				Limits:   wasm.Limits{Min: 5},
				Init:     []byte{wasm.OpRefNull, byte(wasm.ValFuncRef & 0x7F), wasm.OpEnd},
			},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(decoded.Tables))
	}
	if len(decoded.Tables[0].Init) == 0 {
		t.Error("expected table init expression")
	}
}

func TestTableWithRefElemType(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Tables: []wasm.TableType{
			{
				ElemType:    byte(wasm.ValRefNull),
				RefElemType: &wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeAny},
				Limits:      wasm.Limits{Min: 10},
			},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(decoded.Tables))
	}
	if decoded.Tables[0].RefElemType == nil {
		t.Fatal("expected RefElemType")
	}
	if decoded.Tables[0].RefElemType.HeapType != wasm.HeapTypeAny {
		t.Errorf("expected HeapTypeAny, got %d", decoded.Tables[0].RefElemType.HeapType)
	}
}

func TestMemory64RoundTrip(t *testing.T) {
	max := uint64(1000)
	m := &wasm.Module{
		Memories: []wasm.MemoryType{
			{Limits: wasm.Limits{Min: 1, Max: &max, Memory64: true}},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(decoded.Memories))
	}
	if !decoded.Memories[0].Limits.Memory64 {
		t.Error("expected memory64 flag")
	}
	if decoded.Memories[0].Limits.Min != 1 {
		t.Errorf("expected min=1, got %d", decoded.Memories[0].Limits.Min)
	}
	if decoded.Memories[0].Limits.Max == nil || *decoded.Memories[0].Limits.Max != 1000 {
		t.Error("expected max=1000")
	}
}

func TestSharedMemoryRoundTrip(t *testing.T) {
	max := uint64(100)
	m := &wasm.Module{
		Memories: []wasm.MemoryType{
			{Limits: wasm.Limits{Min: 1, Max: &max, Shared: true}},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(decoded.Memories))
	}
	if !decoded.Memories[0].Limits.Shared {
		t.Error("expected shared flag")
	}
}

func TestElementWithExpressions(t *testing.T) {
	m := &wasm.Module{
		Types:  []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:  []uint32{0, 0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 10}}},
		Elements: []wasm.Element{
			{
				Flags: 5, // passive with exprs
				Type:  wasm.ValFuncRef,
				Exprs: [][]byte{{wasm.OpRefFunc, 0, wasm.OpEnd}, {wasm.OpRefFunc, 1, wasm.OpEnd}},
			},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}, {Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(decoded.Elements))
	}
	if len(decoded.Elements[0].Exprs) != 2 {
		t.Errorf("expected 2 exprs, got %d", len(decoded.Elements[0].Exprs))
	}
}

func TestImportWithAllKinds(t *testing.T) {
	max := uint64(10)
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: []wasm.ValType{wasm.ValI32}, Results: nil}},
		Imports: []wasm.Import{
			{Module: "env", Name: "func", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
			{Module: "env", Name: "table", Desc: wasm.ImportDesc{Kind: wasm.KindTable, Table: &wasm.TableType{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}}},
			{Module: "env", Name: "memory", Desc: wasm.ImportDesc{Kind: wasm.KindMemory, Memory: &wasm.MemoryType{Limits: wasm.Limits{Min: 1, Max: &max}}}},
			{Module: "env", Name: "global", Desc: wasm.ImportDesc{Kind: wasm.KindGlobal, Global: &wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}}},
			{Module: "env", Name: "tag", Desc: wasm.ImportDesc{Kind: wasm.KindTag, Tag: &wasm.TagType{Attribute: 0, TypeIdx: 0}}},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Imports) != 5 {
		t.Fatalf("expected 5 imports, got %d", len(decoded.Imports))
	}

	if decoded.Imports[0].Desc.Kind != wasm.KindFunc {
		t.Error("expected func import")
	}
	if decoded.Imports[1].Desc.Kind != wasm.KindTable {
		t.Error("expected table import")
	}
	if decoded.Imports[2].Desc.Kind != wasm.KindMemory {
		t.Error("expected memory import")
	}
	if decoded.Imports[3].Desc.Kind != wasm.KindGlobal {
		t.Error("expected global import")
	}
	if decoded.Imports[4].Desc.Kind != wasm.KindTag {
		t.Error("expected tag import")
	}
}

func TestGlobalWithRefType(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Globals: []wasm.Global{
			{
				Type: wasm.GlobalType{
					ValType: wasm.ValRefNull,
					Mutable: true,
					ExtType: &wasm.ExtValType{
						Kind:    wasm.ExtValKindRef,
						ValType: wasm.ValRefNull,
						RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc},
					},
				},
				Init: []byte{wasm.OpRefNull, byte(wasm.ValFuncRef & 0x7F), wasm.OpEnd},
			},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(decoded.Globals))
	}
	if decoded.Globals[0].Type.ExtType == nil {
		t.Fatal("expected ExtType")
	}
	if decoded.Globals[0].Type.ExtType.RefType.HeapType != wasm.HeapTypeFunc {
		t.Errorf("expected HeapTypeFunc, got %d", decoded.Globals[0].Type.ExtType.RefType.HeapType)
	}
}

func TestLocalWithRefType(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{
					{Count: 1, ValType: wasm.ValI32},
					{
						Count:   1,
						ValType: wasm.ValRefNull,
						ExtType: &wasm.ExtValType{
							Kind:    wasm.ExtValKindRef,
							ValType: wasm.ValRefNull,
							RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeExtern},
						},
					},
				},
				Code: []byte{wasm.OpEnd},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Code) != 1 {
		t.Fatalf("expected 1 code, got %d", len(decoded.Code))
	}
	if len(decoded.Code[0].Locals) != 2 {
		t.Fatalf("expected 2 local entries, got %d", len(decoded.Code[0].Locals))
	}
	if decoded.Code[0].Locals[1].ExtType == nil {
		t.Fatal("expected ExtType for local")
	}
	if decoded.Code[0].Locals[1].ExtType.RefType.HeapType != wasm.HeapTypeExtern {
		t.Errorf("expected HeapTypeExtern, got %d", decoded.Code[0].Locals[1].ExtType.RefType.HeapType)
	}
}

func TestExceptionHandlingInstructions(t *testing.T) {
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"throw", wasm.Instruction{Opcode: wasm.OpThrow, Imm: wasm.ThrowImm{TagIdx: 0}}},
		{"rethrow", wasm.Instruction{Opcode: wasm.OpRethrow, Imm: wasm.BranchImm{LabelIdx: 1}}},
		{"catch", wasm.Instruction{Opcode: wasm.OpCatch, Imm: wasm.ThrowImm{TagIdx: 2}}},
		{"delegate", wasm.Instruction{Opcode: wasm.OpDelegate, Imm: wasm.BranchImm{LabelIdx: 0}}},
		{"try", wasm.Instruction{Opcode: wasm.OpTry, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			if decoded[0].Opcode != tt.instr.Opcode {
				t.Errorf("opcode mismatch: got 0x%02x, want 0x%02x", decoded[0].Opcode, tt.instr.Opcode)
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ")
			}
		})
	}
}

func TestTryTableInstruction(t *testing.T) {
	instr := wasm.Instruction{
		Opcode: wasm.OpTryTable,
		Imm: wasm.TryTableImm{
			BlockType: wasm.BlockTypeVoid,
			Catches: []wasm.CatchClause{
				{Kind: wasm.CatchKindCatch, TagIdx: 0, LabelIdx: 1},
				{Kind: wasm.CatchKindCatchRef, TagIdx: 1, LabelIdx: 2},
				{Kind: wasm.CatchKindCatchAll, LabelIdx: 3},
				{Kind: wasm.CatchKindCatchAllRef, LabelIdx: 4},
			},
		},
	}

	encoded := wasm.EncodeInstructions([]wasm.Instruction{instr})
	decoded, err := wasm.DecodeInstructions(encoded)
	if err != nil {
		t.Fatalf("DecodeInstructions error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(decoded))
	}

	imm, ok := decoded[0].Imm.(wasm.TryTableImm)
	if !ok {
		t.Fatalf("expected TryTableImm, got %T", decoded[0].Imm)
	}
	if len(imm.Catches) != 4 {
		t.Errorf("expected 4 catches, got %d", len(imm.Catches))
	}

	reencoded := wasm.EncodeInstructions(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Errorf("re-encoded bytes differ")
	}
}

func TestCallRefInstructions(t *testing.T) {
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"call_ref", wasm.Instruction{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 5}}},
		{"return_call_ref", wasm.Instruction{Opcode: wasm.OpReturnCallRef, Imm: wasm.CallRefImm{TypeIdx: 10}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ")
			}
		})
	}
}

func TestBrOnNullInstructions(t *testing.T) {
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"br_on_null", wasm.Instruction{Opcode: wasm.OpBrOnNull, Imm: wasm.BranchImm{LabelIdx: 0}}},
		{"br_on_non_null", wasm.Instruction{Opcode: wasm.OpBrOnNonNull, Imm: wasm.BranchImm{LabelIdx: 5}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ")
			}
		})
	}
}

func TestMultiMemoryInstructions(t *testing.T) {
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"i32.load multi-mem", wasm.Instruction{Opcode: wasm.OpI32Load, Imm: wasm.MemoryImm{Align: 2, Offset: 0, MemIdx: 1}}},
		{"i32.store multi-mem", wasm.Instruction{Opcode: wasm.OpI32Store, Imm: wasm.MemoryImm{Align: 2, Offset: 8, MemIdx: 2}}},
		{"memory.size multi-mem", wasm.Instruction{Opcode: wasm.OpMemorySize, Imm: wasm.MemoryIdxImm{MemIdx: 3}}},
		{"memory.grow multi-mem", wasm.Instruction{Opcode: wasm.OpMemoryGrow, Imm: wasm.MemoryIdxImm{MemIdx: 4}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ: got %v, want %v", reencoded, encoded)
			}
		})
	}
}

func TestMiscInstructionsRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		instr wasm.Instruction
	}{
		{"i32.trunc_sat_f32_s", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscI32TruncSatF32S}}},
		{"i64.trunc_sat_f64_u", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscI64TruncSatF64U}}},
		{"memory.init", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryInit, Operands: []uint32{5, 0}}}},
		{"memory.init multi-memory", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryInit, Operands: []uint32{3, 2}}}},
		{"data.drop", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscDataDrop, Operands: []uint32{7}}}},
		{"memory.copy", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryCopy, Operands: []uint32{0, 0}}}},
		{"memory.copy multi-memory", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryCopy, Operands: []uint32{1, 2}}}},
		{"memory.fill", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryFill, Operands: []uint32{0}}}},
		{"memory.fill multi-memory", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryFill, Operands: []uint32{3}}}},
		{"table.init", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscTableInit, Operands: []uint32{2, 1}}}},
		{"elem.drop", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscElemDrop, Operands: []uint32{4}}}},
		{"table.copy", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscTableCopy, Operands: []uint32{0, 1}}}},
		{"table.grow", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscTableGrow, Operands: []uint32{2}}}},
		{"table.size", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscTableSize, Operands: []uint32{0}}}},
		{"table.fill", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscTableFill, Operands: []uint32{1}}}},
		{"memory.discard", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryDiscard, Operands: []uint32{0}}}},
		{"memory.discard multi-memory", wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryDiscard, Operands: []uint32{2}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := wasm.EncodeInstructions([]wasm.Instruction{tt.instr})
			decoded, err := wasm.DecodeInstructions(encoded)
			if err != nil {
				t.Fatalf("DecodeInstructions error: %v", err)
			}
			if len(decoded) != 1 {
				t.Fatalf("expected 1 instruction, got %d", len(decoded))
			}
			reencoded := wasm.EncodeInstructions(decoded)
			if !bytes.Equal(encoded, reencoded) {
				t.Errorf("re-encoded bytes differ")
			}
		})
	}
}

func TestGlobalInitExpressions(t *testing.T) {
	t.Run("i64.const", func(t *testing.T) {
		m := &wasm.Module{
			Globals: []wasm.Global{
				{
					Type: wasm.GlobalType{ValType: wasm.ValI64, Mutable: false},
					Init: []byte{wasm.OpI64Const, 0x80, 0x80, 0x80, 0x80, 0x08, wasm.OpEnd}, // 2^31
				},
			},
		}
		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}
		if len(decoded.Globals) != 1 {
			t.Fatalf("expected 1 global, got %d", len(decoded.Globals))
		}
		if decoded.Globals[0].Init[0] != wasm.OpI64Const {
			t.Error("expected i64.const opcode")
		}
	})

	t.Run("f32.const", func(t *testing.T) {
		m := &wasm.Module{
			Globals: []wasm.Global{
				{
					Type: wasm.GlobalType{ValType: wasm.ValF32, Mutable: false},
					Init: []byte{wasm.OpF32Const, 0x00, 0x00, 0x80, 0x3f, wasm.OpEnd}, // 1.0
				},
			},
		}
		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}
		if decoded.Globals[0].Init[0] != wasm.OpF32Const {
			t.Error("expected f32.const opcode")
		}
	})

	t.Run("f64.const", func(t *testing.T) {
		m := &wasm.Module{
			Globals: []wasm.Global{
				{
					Type: wasm.GlobalType{ValType: wasm.ValF64, Mutable: true},
					Init: []byte{wasm.OpF64Const, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f, wasm.OpEnd}, // 1.0
				},
			},
		}
		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}
		if decoded.Globals[0].Init[0] != wasm.OpF64Const {
			t.Error("expected f64.const opcode")
		}
	})

	t.Run("global.get", func(t *testing.T) {
		m := &wasm.Module{
			Imports: []wasm.Import{
				{Module: "env", Name: "g", Desc: wasm.ImportDesc{Kind: wasm.KindGlobal, Global: &wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}}},
			},
			Globals: []wasm.Global{
				{
					Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false},
					Init: []byte{wasm.OpGlobalGet, 0, wasm.OpEnd},
				},
			},
		}
		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}
		if decoded.Globals[0].Init[0] != wasm.OpGlobalGet {
			t.Error("expected global.get opcode")
		}
	})

	t.Run("ref.func", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{Params: nil, Results: nil}},
			Funcs: []uint32{0},
			Globals: []wasm.Global{
				{
					Type: wasm.GlobalType{ValType: wasm.ValFuncRef, Mutable: false},
					Init: []byte{wasm.OpRefFunc, 0, wasm.OpEnd},
				},
			},
			Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
		}
		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}
		if decoded.Globals[0].Init[0] != wasm.OpRefFunc {
			t.Error("expected ref.func opcode")
		}
	})
}

func TestArrayTypeInSubType(t *testing.T) {
	at := wasm.ArrayType{
		Element: wasm.FieldType{
			Type:    wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32},
			Mutable: true,
		},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:   true,
					Parents: nil,
					CompType: wasm.CompType{
						Kind:  wasm.CompKindArray,
						Array: &at,
					},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 1 {
		t.Fatalf("expected 1 typedef, got %d", len(decoded.TypeDefs))
	}
	if decoded.TypeDefs[0].Kind != wasm.TypeDefKindSub {
		t.Error("expected sub typedef")
	}
	if decoded.TypeDefs[0].Sub.CompType.Kind != wasm.CompKindArray {
		t.Error("expected array comp type")
	}
}

func TestSubTypeWithParents(t *testing.T) {
	ft := wasm.FuncType{Params: nil, Results: nil}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindFunc, Func: &ft},
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:   false,
					Parents: []uint32{0},
					CompType: wasm.CompType{
						Kind: wasm.CompKindFunc,
						Func: &ft,
					},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 2 {
		t.Fatalf("expected 2 typedefs, got %d", len(decoded.TypeDefs))
	}
	if len(decoded.TypeDefs[1].Sub.Parents) != 1 {
		t.Errorf("expected 1 parent, got %d", len(decoded.TypeDefs[1].Sub.Parents))
	}
	if decoded.TypeDefs[1].Sub.Final {
		t.Error("expected non-final subtype")
	}
}

func TestRefTypeWithHeapTypes(t *testing.T) {
	tests := []struct {
		name    string
		valType wasm.ValType
	}{
		{"funcref", wasm.ValFuncRef},
		{"externref", wasm.ValExtern},
		{"anyref", wasm.ValAnyRef},
		{"eqref", wasm.ValEqRef},
		{"i31ref", wasm.ValI31Ref},
		{"structref", wasm.ValStructRef},
		{"arrayref", wasm.ValArrayRef},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &wasm.Module{
				Types: []wasm.FuncType{{Params: []wasm.ValType{tt.valType}, Results: nil}},
			}
			encoded := m.Encode()
			decoded, err := wasm.ParseModule(encoded)
			if err != nil {
				t.Fatalf("ParseModule error: %v", err)
			}
			if len(decoded.Types) != 1 {
				t.Fatalf("expected 1 type, got %d", len(decoded.Types))
			}
			if decoded.Types[0].Params[0] != tt.valType {
				t.Errorf("expected %v, got %v", tt.valType, decoded.Types[0].Params[0])
			}
		})
	}
}

func TestCustomSectionRoundTrip(t *testing.T) {
	m := &wasm.Module{
		CustomSections: []wasm.CustomSection{
			{Name: "test", Data: []byte{1, 2, 3, 4, 5}},
			{Name: "debug", Data: []byte("debug info")},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.CustomSections) != 2 {
		t.Fatalf("expected 2 custom sections, got %d", len(decoded.CustomSections))
	}
	if decoded.CustomSections[0].Name != "test" {
		t.Errorf("expected name 'test', got %s", decoded.CustomSections[0].Name)
	}
	if !bytes.Equal(decoded.CustomSections[0].Data, []byte{1, 2, 3, 4, 5}) {
		t.Error("custom section data mismatch")
	}
}

func TestDataSegmentModes(t *testing.T) {
	t.Run("active with offset", func(t *testing.T) {
		m := &wasm.Module{
			Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
			Data: []wasm.DataSegment{
				{
					Flags:  0, // active
					MemIdx: 0,
					Offset: []byte{wasm.OpI32Const, 0x10, wasm.OpEnd},
					Init:   []byte("hello"),
				},
			},
		}
		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}
		if decoded.Data[0].Flags != 0 {
			t.Error("expected active data segment (flags=0)")
		}
		if decoded.Data[0].Offset == nil {
			t.Error("expected offset expression")
		}
	})

	t.Run("passive", func(t *testing.T) {
		m := &wasm.Module{
			Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
			Data: []wasm.DataSegment{
				{
					Flags: 1, // passive
					Init:  []byte("world"),
				},
			},
		}
		encoded := m.Encode()
		decoded, err := wasm.ParseModule(encoded)
		if err != nil {
			t.Fatalf("ParseModule error: %v", err)
		}
		if decoded.Data[0].Flags != 1 {
			t.Error("expected passive data segment (flags=1)")
		}
	})
}

func TestStartSection(t *testing.T) {
	startIdx := uint32(0)
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Start: &startIdx,
		Code:  []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if decoded.Start == nil {
		t.Fatal("expected start section")
	}
	if *decoded.Start != 0 {
		t.Errorf("expected start index 0, got %d", *decoded.Start)
	}
}

func TestRecTypeWithMixedSubTypes(t *testing.T) {
	ft := wasm.FuncType{Params: nil, Results: nil}
	st := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}
	at := wasm.ArrayType{Element: wasm.FieldType{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI64}}}

	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindRec,
				Rec: &wasm.RecType{
					Types: []wasm.SubType{
						{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
						{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st}},
						{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &at}},
					},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 1 {
		t.Fatalf("expected 1 typedef, got %d", len(decoded.TypeDefs))
	}
	rec := decoded.TypeDefs[0].Rec
	if rec == nil {
		t.Fatal("expected rec type")
	}
	if len(rec.Types) != 3 {
		t.Fatalf("expected 3 subtypes, got %d", len(rec.Types))
	}
	if rec.Types[0].CompType.Kind != wasm.CompKindFunc {
		t.Error("expected func comp type for first")
	}
	if rec.Types[1].CompType.Kind != wasm.CompKindStruct {
		t.Error("expected struct comp type for second")
	}
	if rec.Types[2].CompType.Kind != wasm.CompKindArray {
		t.Error("expected array comp type for third")
	}
}

func TestSubTypeWithStructComposite(t *testing.T) {
	st := wasm.StructType{
		Fields: []wasm.FieldType{
			{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}, Mutable: true},
			{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValF64}, Mutable: false},
		},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:    false,
					Parents:  nil,
					CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 1 {
		t.Fatalf("expected 1 typedef, got %d", len(decoded.TypeDefs))
	}
	if decoded.TypeDefs[0].Sub.CompType.Kind != wasm.CompKindStruct {
		t.Error("expected struct comp type")
	}
	if len(decoded.TypeDefs[0].Sub.CompType.Struct.Fields) != 2 {
		t.Error("expected 2 fields")
	}
}

func TestSubTypeWithArrayComposite(t *testing.T) {
	at := wasm.ArrayType{
		Element: wasm.FieldType{
			Type:    wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValF32},
			Mutable: true,
		},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:    false,
					Parents:  nil,
					CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &at},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 1 {
		t.Fatalf("expected 1 typedef, got %d", len(decoded.TypeDefs))
	}
	if decoded.TypeDefs[0].Sub.CompType.Kind != wasm.CompKindArray {
		t.Error("expected array comp type")
	}
}

func TestNonFinalSubWithParentStruct(t *testing.T) {
	st1 := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}
	st2 := wasm.StructType{Fields: []wasm.FieldType{
		{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}},
		{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI64}},
	}}

	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:    false,
					Parents:  nil,
					CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st1},
				},
			},
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:    false,
					Parents:  []uint32{0},
					CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st2},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 2 {
		t.Fatalf("expected 2 typedefs, got %d", len(decoded.TypeDefs))
	}
	if len(decoded.TypeDefs[1].Sub.Parents) != 1 || decoded.TypeDefs[1].Sub.Parents[0] != 0 {
		t.Error("expected parent type index 0")
	}
}

func TestNonFinalSubWithParentArray(t *testing.T) {
	at := wasm.ArrayType{Element: wasm.FieldType{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}

	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:    false,
					Parents:  nil,
					CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &at},
				},
			},
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Final:    true,
					Parents:  []uint32{0},
					CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &at},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 2 {
		t.Fatalf("expected 2 typedefs, got %d", len(decoded.TypeDefs))
	}
	if !decoded.TypeDefs[1].Sub.Final {
		t.Error("expected final subtype")
	}
}

func TestTableWithGCRefType(t *testing.T) {
	m := &wasm.Module{
		Tables: []wasm.TableType{
			{
				ElemType:    byte(wasm.ValRefNull),
				RefElemType: &wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeAny},
				Limits:      wasm.Limits{Min: 1},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(decoded.Tables))
	}
	if decoded.Tables[0].RefElemType == nil {
		t.Fatal("expected ref elem type")
	}
	if decoded.Tables[0].RefElemType.HeapType != wasm.HeapTypeAny {
		t.Errorf("expected HeapTypeAny, got %d", decoded.Tables[0].RefElemType.HeapType)
	}
}

func TestElementWithRefTypeExprs(t *testing.T) {
	m := &wasm.Module{
		Types:  []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:  []uint32{0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 2}}},
		Elements: []wasm.Element{
			{
				Flags:   7, // declarative with expressions and type
				RefType: &wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc},
				Exprs:   [][]byte{{wasm.OpRefNull, byte(wasm.ValFuncRef & 0x7F), wasm.OpEnd}},
			},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(decoded.Elements))
	}
}

func TestTagSection(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: nil},
			{Params: nil, Results: nil},
		},
		Tags: []wasm.TagType{
			{Attribute: 0, TypeIdx: 0},
			{Attribute: 0, TypeIdx: 1},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(decoded.Tags))
	}
	if decoded.Tags[0].TypeIdx != 0 {
		t.Errorf("expected type index 0, got %d", decoded.Tags[0].TypeIdx)
	}
}

func TestGlobalV128InitExpr(t *testing.T) {
	// v128.const with 16 bytes of immediate data
	v128Init := []byte{
		wasm.OpPrefixSIMD, 0x0C, // v128.const (0xFD 0x0C)
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, // 16 bytes
		wasm.OpEnd,
	}

	m := &wasm.Module{
		Globals: []wasm.Global{
			{
				Type: wasm.GlobalType{ValType: wasm.ValV128, Mutable: false},
				Init: v128Init,
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(decoded.Globals))
	}
	if decoded.Globals[0].Init[0] != wasm.OpPrefixSIMD {
		t.Error("expected SIMD prefix in init")
	}
}

func TestGlobalExtendedConstInit(t *testing.T) {
	// i32.add requires two operands: i32.const + i32.const + i32.add
	extendedInit := []byte{
		wasm.OpI32Const, 10,
		wasm.OpI32Const, 20,
		wasm.OpI32Add,
		wasm.OpEnd,
	}

	m := &wasm.Module{
		Globals: []wasm.Global{
			{
				Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false},
				Init: extendedInit,
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(decoded.Globals))
	}
	// Verify the init contains the extended-const ops
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpI32Add}) {
		t.Error("expected i32.add in init")
	}
}

func TestGCInitExprStructNew(t *testing.T) {
	// Test struct.new in init expression
	structType := wasm.StructType{
		Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}},
	}
	// Init: i32.const 42, struct.new 0, end
	initExpr := []byte{
		wasm.OpI32Const, 42,
		wasm.OpPrefixGC, byte(wasm.GCStructNew), 0, // struct.new type 0
		wasm.OpEnd,
	}
	// Must set ExtType for ref types so encoder writes heap type
	refType := &wasm.ExtValType{
		Kind:    wasm.ExtValKindRef,
		ValType: wasm.ValRefNull,
		RefType: wasm.RefType{Nullable: true, HeapType: 0}, // ref to type 0
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &structType},
		}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValRefNull, Mutable: false, ExtType: refType}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if len(decoded.Globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(decoded.Globals))
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCStructNew)}) {
		t.Error("expected struct.new in init")
	}
}

func TestGCInitExprStructNewDefault(t *testing.T) {
	// Test struct.new_default in init expression
	structType := wasm.StructType{
		Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}},
	}
	// Init: struct.new_default 0, end
	initExpr := []byte{
		wasm.OpPrefixGC, byte(wasm.GCStructNewDefault), 0, // struct.new_default type 0
		wasm.OpEnd,
	}
	// Must set ExtType for ref types so encoder writes heap type
	refType := &wasm.ExtValType{
		Kind:    wasm.ExtValKindRef,
		ValType: wasm.ValRefNull,
		RefType: wasm.RefType{Nullable: true, HeapType: 0}, // ref to type 0
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &structType},
		}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValRefNull, Mutable: false, ExtType: refType}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCStructNewDefault)}) {
		t.Errorf("expected struct.new_default in init, got: %v (looking for %v)", decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCStructNewDefault)})
	}
}

func TestGCInitExprArrayNew(t *testing.T) {
	// Test array.new in init expression
	arrayType := wasm.ArrayType{
		Element: wasm.FieldType{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}},
	}
	// Init: i32.const 0, i32.const 10, array.new 0, end
	initExpr := []byte{
		wasm.OpI32Const, 0, // init value
		wasm.OpI32Const, 10, // length
		wasm.OpPrefixGC, byte(wasm.GCArrayNew), 0, // array.new type 0
		wasm.OpEnd,
	}
	refType := &wasm.ExtValType{
		Kind:    wasm.ExtValKindRef,
		ValType: wasm.ValRefNull,
		RefType: wasm.RefType{Nullable: true, HeapType: 0},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &arrayType},
		}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValRefNull, Mutable: false, ExtType: refType}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCArrayNew)}) {
		t.Error("expected array.new in init")
	}
}

func TestGCInitExprArrayNewFixed(t *testing.T) {
	// Test array.new_fixed in init expression
	arrayType := wasm.ArrayType{
		Element: wasm.FieldType{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}},
	}
	// Init: i32.const 1, i32.const 2, i32.const 3, array.new_fixed 0 3, end
	initExpr := []byte{
		wasm.OpI32Const, 1,
		wasm.OpI32Const, 2,
		wasm.OpI32Const, 3,
		wasm.OpPrefixGC, byte(wasm.GCArrayNewFixed), 0, 3, // array.new_fixed type 0, count 3
		wasm.OpEnd,
	}
	refType := &wasm.ExtValType{
		Kind:    wasm.ExtValKindRef,
		ValType: wasm.ValRefNull,
		RefType: wasm.RefType{Nullable: true, HeapType: 0},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &arrayType},
		}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValRefNull, Mutable: false, ExtType: refType}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCArrayNewFixed)}) {
		t.Error("expected array.new_fixed in init")
	}
}

func TestGCInitExprArrayNewData(t *testing.T) {
	// Test array.new_data in init expression
	arrayType := wasm.ArrayType{
		Element: wasm.FieldType{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}},
	}
	dataCount := uint32(1)
	// Init: i32.const 0, i32.const 4, array.new_data 0 0, end
	initExpr := []byte{
		wasm.OpI32Const, 0, // offset
		wasm.OpI32Const, 4, // length
		wasm.OpPrefixGC, byte(wasm.GCArrayNewData), 0, 0, // array.new_data type 0, data 0
		wasm.OpEnd,
	}
	refType := &wasm.ExtValType{
		Kind:    wasm.ExtValKindRef,
		ValType: wasm.ValRefNull,
		RefType: wasm.RefType{Nullable: true, HeapType: 0},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &arrayType},
		}}},
		Memories:  []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		DataCount: &dataCount,
		Data: []wasm.DataSegment{
			{Flags: 1, Init: []byte{1, 2, 3, 4}}, // passive data segment
		},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValRefNull, Mutable: false, ExtType: refType}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCArrayNewData)}) {
		t.Error("expected array.new_data in init")
	}
}

func TestGCInitExprArrayNewElem(t *testing.T) {
	// Test array.new_elem in init expression
	arrayType := wasm.ArrayType{
		Element: wasm.FieldType{Type: wasm.StorageType{Kind: wasm.StorageKindRef, RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc}}},
	}
	// Init: i32.const 0, i32.const 1, array.new_elem 0 0, end
	initExpr := []byte{
		wasm.OpI32Const, 0, // offset
		wasm.OpI32Const, 1, // length
		wasm.OpPrefixGC, byte(wasm.GCArrayNewElem), 0, 0, // array.new_elem type 0, elem 0
		wasm.OpEnd,
	}
	refType := &wasm.ExtValType{
		Kind:    wasm.ExtValKindRef,
		ValType: wasm.ValRefNull,
		RefType: wasm.RefType{Nullable: true, HeapType: 0},
	}
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
			Final:    true,
			CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &arrayType},
		}}},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
		Elements: []wasm.Element{
			{Flags: 1, Type: wasm.ValFuncRef, FuncIdxs: []uint32{}}, // passive element
		},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValRefNull, Mutable: false, ExtType: refType}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCArrayNewElem)}) {
		t.Error("expected array.new_elem in init")
	}
}

func TestGCInitExprRefI31(t *testing.T) {
	// Test ref.i31 in init expression - use i32 global type to avoid ref type encoding
	// Init: i32.const 42, ref.i31, end
	initExpr := []byte{
		wasm.OpI32Const, 42,
		wasm.OpPrefixGC, byte(wasm.GCRefI31), // ref.i31
		wasm.OpEnd,
	}
	m := &wasm.Module{
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCRefI31)}) {
		t.Error("expected ref.i31 in init")
	}
}

func TestGCInitExprAnyConvertExtern(t *testing.T) {
	// Test any.convert_extern in init expression - use i32 global to simplify
	// HeapTypeExtern = -17, encode as signed LEB128: 0x6F
	initExpr := []byte{
		wasm.OpRefNull, 0x6F, // ref.null extern (0x6F is -17 as s33)
		wasm.OpPrefixGC, byte(wasm.GCAnyConvertExtern), // any.convert_extern
		wasm.OpEnd,
	}
	m := &wasm.Module{
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCAnyConvertExtern)}) {
		t.Error("expected any.convert_extern in init")
	}
}

func TestGCInitExprExternConvertAny(t *testing.T) {
	// Test extern.convert_any in init expression - use i32 global to simplify
	// HeapTypeAny = -18, encode as signed LEB128: 0x6E
	initExpr := []byte{
		wasm.OpRefNull, 0x6E, // ref.null any (0x6E is -18 as s33)
		wasm.OpPrefixGC, byte(wasm.GCExternConvertAny), // extern.convert_any
		wasm.OpEnd,
	}
	m := &wasm.Module{
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}, Init: initExpr},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}
	if !bytes.Contains(decoded.Globals[0].Init, []byte{wasm.OpPrefixGC, byte(wasm.GCExternConvertAny)}) {
		t.Error("expected extern.convert_any in init")
	}
}

func TestElementActiveFlags(t *testing.T) {
	// flags=0: active element segment with table 0
	m := &wasm.Module{
		Types:  []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs:  []uint32{0, 0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 5}}},
		Elements: []wasm.Element{
			{
				Flags:    0, // active, table 0, funcref
				TableIdx: 0,
				Offset:   []byte{wasm.OpI32Const, 0, wasm.OpEnd},
				FuncIdxs: []uint32{0, 1},
			},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}, {Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(decoded.Elements))
	}
	if decoded.Elements[0].Flags != 0 {
		t.Errorf("expected flags 0, got %d", decoded.Elements[0].Flags)
	}
}

func TestElementFlags2WithTableIdx(t *testing.T) {
	// flags=2: active with explicit table index
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Tables: []wasm.TableType{
			{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 2}},
			{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 3}},
		},
		Elements: []wasm.Element{
			{
				Flags:    2, // active with table index and elemkind
				TableIdx: 1,
				Offset:   []byte{wasm.OpI32Const, 1, wasm.OpEnd},
				ElemKind: 0,
				FuncIdxs: []uint32{0},
			},
		},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if decoded.Elements[0].TableIdx != 1 {
		t.Errorf("expected table index 1, got %d", decoded.Elements[0].TableIdx)
	}
}

func TestDataActiveWithMemIdx(t *testing.T) {
	// flags=2: active with explicit memory index
	m := &wasm.Module{
		Memories: []wasm.MemoryType{
			{Limits: wasm.Limits{Min: 1}},
			{Limits: wasm.Limits{Min: 1}},
		},
		Data: []wasm.DataSegment{
			{
				Flags:  2, // active with memory index
				MemIdx: 1,
				Offset: []byte{wasm.OpI32Const, 0, wasm.OpEnd},
				Init:   []byte("test"),
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if decoded.Data[0].MemIdx != 1 {
		t.Errorf("expected memory index 1, got %d", decoded.Data[0].MemIdx)
	}
}

func TestImportedGlobal(t *testing.T) {
	// Test imported global with mutable flag
	m := &wasm.Module{
		Imports: []wasm.Import{
			{
				Module: "env",
				Name:   "g",
				Desc: wasm.ImportDesc{
					Kind:   wasm.KindGlobal,
					Global: &wasm.GlobalType{ValType: wasm.ValI64, Mutable: true},
				},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(decoded.Imports))
	}
	if !decoded.Imports[0].Desc.Global.Mutable {
		t.Error("expected mutable global")
	}
}

func TestFunctionWithLocals(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: []wasm.ValType{wasm.ValI32}}},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{
					{Count: 2, ValType: wasm.ValI32},
					{Count: 1, ValType: wasm.ValI64},
				},
				Code: []byte{wasm.OpLocalGet, 0, wasm.OpEnd},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Code) != 1 {
		t.Fatalf("expected 1 code body, got %d", len(decoded.Code))
	}
	if len(decoded.Code[0].Locals) != 2 {
		t.Errorf("expected 2 local entries, got %d", len(decoded.Code[0].Locals))
	}
	if decoded.Code[0].Locals[0].Count != 2 {
		t.Errorf("expected first local count 2, got %d", decoded.Code[0].Locals[0].Count)
	}
}

func TestMixedGCAndFuncTypes(t *testing.T) {
	// This tests skipFuncType by having func types with ref params before a struct type
	ft := wasm.FuncType{Params: []wasm.ValType{wasm.ValFuncRef, wasm.ValExtern}, Results: []wasm.ValType{wasm.ValAnyRef}}
	st := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}

	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindFunc, Func: &ft},
			{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st}}},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 2 {
		t.Fatalf("expected 2 typedefs, got %d", len(decoded.TypeDefs))
	}
	if decoded.TypeDefs[0].Kind != wasm.TypeDefKindFunc {
		t.Error("expected first type to be func")
	}
	if decoded.TypeDefs[1].Kind != wasm.TypeDefKindSub {
		t.Error("expected second type to be sub")
	}
}

func TestSkipFuncTypeWithHeapTypes(t *testing.T) {
	// Test skipFuncType with ref null/ref types that have heap type immediates
	// This exercises the ValRefNull/ValRef branches in skipFuncType
	ft := wasm.FuncType{
		ExtParams: []wasm.ExtValType{
			{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: wasm.HeapTypeFunc}},
			{Kind: wasm.ExtValKindRef, ValType: wasm.ValRef, RefType: wasm.RefType{Nullable: false, HeapType: wasm.HeapTypeAny}},
		},
		ExtResults: []wasm.ExtValType{
			{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: 0}},
		},
		Params:  []wasm.ValType{wasm.ValRefNull, wasm.ValRef},
		Results: []wasm.ValType{wasm.ValRefNull},
	}
	st := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}

	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindFunc, Func: &ft},
			{Kind: wasm.TypeDefKindFunc, Func: &ft}, // Second func type to test multiple
			{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st}}},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.TypeDefs) != 3 {
		t.Fatalf("expected 3 typedefs, got %d", len(decoded.TypeDefs))
	}
}

func TestFuncTypeWithRefParams(t *testing.T) {
	// Test function types with various reference type parameters
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValFuncRef}, Results: nil},
			{Params: []wasm.ValType{wasm.ValExtern}, Results: []wasm.ValType{wasm.ValFuncRef}},
			{Params: []wasm.ValType{wasm.ValI32, wasm.ValFuncRef, wasm.ValI64}, Results: []wasm.ValType{wasm.ValExtern}},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Types) != 3 {
		t.Fatalf("expected 3 types, got %d", len(decoded.Types))
	}
	if decoded.Types[0].Params[0] != wasm.ValFuncRef {
		t.Error("expected funcref param")
	}
	if decoded.Types[1].Results[0] != wasm.ValFuncRef {
		t.Error("expected funcref result")
	}
}

func TestLocalWithMultipleRefTypes(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{Params: nil, Results: nil}},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{
					{Count: 1, ValType: wasm.ValFuncRef},
					{Count: 2, ValType: wasm.ValExtern},
				},
				Code: []byte{wasm.OpEnd},
			},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Code[0].Locals) != 2 {
		t.Errorf("expected 2 local entries, got %d", len(decoded.Code[0].Locals))
	}
	if decoded.Code[0].Locals[0].ValType != wasm.ValFuncRef {
		t.Error("expected funcref local")
	}
}

func TestTableExport(t *testing.T) {
	m := &wasm.Module{
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "table", Kind: wasm.KindTable, Idx: 0},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if len(decoded.Exports) != 1 {
		t.Fatalf("expected 1 export, got %d", len(decoded.Exports))
	}
	if decoded.Exports[0].Kind != wasm.KindTable {
		t.Error("expected table export")
	}
}

func TestMemoryExport(t *testing.T) {
	m := &wasm.Module{
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "memory", Kind: wasm.KindMemory, Idx: 0},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if decoded.Exports[0].Kind != wasm.KindMemory {
		t.Error("expected memory export")
	}
}

func TestGlobalExport(t *testing.T) {
	m := &wasm.Module{
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}, Init: []byte{wasm.OpI32Const, 42, wasm.OpEnd}},
		},
		Exports: []wasm.Export{
			{Name: "g", Kind: wasm.KindGlobal, Idx: 0},
		},
	}

	encoded := m.Encode()
	decoded, err := wasm.ParseModule(encoded)
	if err != nil {
		t.Fatalf("ParseModule error: %v", err)
	}

	if decoded.Exports[0].Kind != wasm.KindGlobal {
		t.Error("expected global export")
	}
}

func TestParseRealModules(t *testing.T) {
	files := []string{
		"../testbed/go-calculator.wasm",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Skipf("skipping %s: %v", f, err)
				return
			}

			// Skip component modules (magic + layer byte)
			if len(data) >= 8 && data[4] != 0x01 {
				t.Skipf("skipping non-core module")
				return
			}

			m, err := wasm.ParseModule(data)
			if err != nil {
				t.Fatalf("ParseModule: %v", err)
			}

			if m == nil {
				t.Fatal("expected non-nil module")
			}

			// Re-encode and re-parse to verify round-trip
			reencoded := m.Encode()
			_, err = wasm.ParseModule(reencoded)
			if err != nil {
				t.Fatalf("re-parse after round-trip failed: %v", err)
			}
		})
	}
}

// TDD: Target AddType - type reuse
func TestAddTypeReuse(t *testing.T) {
	m := &wasm.Module{}

	// Add first type
	ft1 := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64}}
	idx1 := m.AddType(ft1)

	// Add same type - should reuse
	idx2 := m.AddType(ft1)
	if idx1 != idx2 {
		t.Errorf("expected same index, got %d and %d", idx1, idx2)
	}

	// Add different type - should be new
	ft2 := wasm.FuncType{Params: []wasm.ValType{wasm.ValF32}, Results: []wasm.ValType{}}
	idx3 := m.AddType(ft2)
	if idx3 == idx1 {
		t.Errorf("expected different index for different type")
	}
}

// TDD: Target typesEqual - params mismatch
func TestAddTypeDifferentParams(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32, wasm.ValI64}, Results: []wasm.ValType{}}
	ft2 := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32, wasm.ValF32}, Results: []wasm.ValType{}} // different second param

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 == idx2 {
		t.Errorf("expected different indices for different param types")
	}
}

// TDD: Target typesEqual - results mismatch
func TestAddTypeDifferentResults(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{Params: []wasm.ValType{}, Results: []wasm.ValType{wasm.ValI32}}
	ft2 := wasm.FuncType{Params: []wasm.ValType{}, Results: []wasm.ValType{wasm.ValI64}} // different result

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 == idx2 {
		t.Errorf("expected different indices for different result types")
	}
}

// TDD: Target typesEqual - param length mismatch
func TestAddTypeDifferentParamCount(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{}}
	ft2 := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32, wasm.ValI64}, Results: []wasm.ValType{}} // more params

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 == idx2 {
		t.Errorf("expected different indices for different param counts")
	}
}

// TDD: Target typesEqual - result length mismatch
func TestAddTypeDifferentResultCount(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{Params: []wasm.ValType{}, Results: []wasm.ValType{wasm.ValI32}}
	ft2 := wasm.FuncType{Params: []wasm.ValType{}, Results: []wasm.ValType{wasm.ValI32, wasm.ValI64}} // more results

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 == idx2 {
		t.Errorf("expected different indices for different result counts")
	}
}

// TDD: Target GetFuncType with TypeDefs (GC types)
func TestGetFuncTypeWithTypeDefs(t *testing.T) {
	// Build a module with TypeDefs and a function using type 0
	ft := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64}}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindFunc, Func: &ft},
		},
		Types: []wasm.FuncType{ft},
		Funcs: []uint32{0}, // function 0 uses type 0
	}

	got := m.GetFuncType(0)
	if got == nil {
		t.Fatal("expected non-nil FuncType")
	}
	if len(got.Params) != 1 || got.Params[0] != wasm.ValI32 {
		t.Errorf("unexpected params: %v", got.Params)
	}
}

// TDD: Target getFuncTypeByIdx - SubType path
func TestGetFuncTypeWithSubType(t *testing.T) {
	ft := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{}}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
				Final:    true,
				CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft},
			}},
		},
		Types: []wasm.FuncType{ft},
		Funcs: []uint32{0}, // function 0 uses type 0
	}

	got := m.GetFuncType(0)
	if got == nil {
		t.Fatal("expected non-nil FuncType")
	}
}

// TDD: Target getFuncTypeByIdx - SubType with non-func CompType
func TestGetFuncTypeWithSubTypeNotFunc(t *testing.T) {
	st := wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindSub, Sub: &wasm.SubType{
				Final:    true,
				CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &st},
			}},
		},
		Funcs: []uint32{0}, // function 0 uses type 0 (a struct type)
	}

	// Looking for function 0 whose type is a struct, not a func
	got := m.GetFuncType(0)
	if got != nil {
		t.Error("expected nil for non-func type")
	}
}

// TDD: Target getFuncTypeByIdx - RecType path
func TestGetFuncTypeWithRecType(t *testing.T) {
	ft := wasm.FuncType{Params: []wasm.ValType{}, Results: []wasm.ValType{wasm.ValI32}}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{
				Types: []wasm.SubType{
					{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: &ft}},
				},
			}},
		},
		Types: []wasm.FuncType{ft},
		Funcs: []uint32{0}, // function 0 uses type 0 (first in rec group)
	}

	got := m.GetFuncType(0)
	if got == nil {
		t.Fatal("expected non-nil FuncType")
	}
}

// TDD: Target getFuncTypeByIdx - RecType with non-func
func TestGetFuncTypeWithRecTypeNotFunc(t *testing.T) {
	at := wasm.ArrayType{Element: wasm.FieldType{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{
				Types: []wasm.SubType{
					{Final: true, CompType: wasm.CompType{Kind: wasm.CompKindArray, Array: &at}},
				},
			}},
		},
		Funcs: []uint32{0}, // function 0 uses type 0 (array type)
	}

	got := m.GetFuncType(0)
	if got != nil {
		t.Error("expected nil for non-func type in rec")
	}
}

// TDD: Target getFuncTypeByIdx - out of bounds (simple module)
func TestGetFuncTypeOutOfBounds(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{}, Results: []wasm.ValType{}},
		},
		Funcs: []uint32{0}, // Only 1 function
	}

	got := m.GetFuncType(100) // Out of bounds function index
	if got != nil {
		t.Error("expected nil for out of bounds func idx")
	}
}

// TDD: Target getFuncTypeByIdx - TypeDefs out of bounds
func TestGetFuncTypeTypeDefsOutOfBounds(t *testing.T) {
	ft := wasm.FuncType{Params: []wasm.ValType{}, Results: []wasm.ValType{}}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindFunc, Func: &ft},
		},
		Types: []wasm.FuncType{ft},
		Funcs: []uint32{5}, // function 0 uses type 5 (doesn't exist)
	}

	got := m.GetFuncType(0)
	if got != nil {
		t.Error("expected nil for out of bounds type idx in TypeDefs")
	}
}

// TDD: Target typesEqual with ExtParams
func TestAddTypeWithExtParams(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI64}},
	}
	ft2 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI64}},
	}

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 != idx2 {
		t.Errorf("expected same index for equal ExtParams types")
	}
}

// TDD: Target typesEqual - ExtParams length mismatch
func TestAddTypeWithDifferentExtParamsCount(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{
		ExtParams: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
	}
	ft2 := wasm.FuncType{
		ExtParams: []wasm.ExtValType{
			{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32},
			{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI64},
		},
	}

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 == idx2 {
		t.Errorf("expected different indices for different ExtParams counts")
	}
}

// TDD: Target typesEqual - ExtResults mismatch
func TestAddTypeWithDifferentExtResults(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
	}
	ft2 := wasm.FuncType{
		ExtParams:  []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		ExtResults: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI64}}, // different
	}

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 == idx2 {
		t.Errorf("expected different indices for different ExtResults")
	}
}

// TDD: Target extValTypesEqual - RefType comparison
func TestAddTypeWithRefTypeParams(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{
		ExtParams: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: -16}}},
	}
	ft2 := wasm.FuncType{
		ExtParams: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: -16}}},
	}

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 != idx2 {
		t.Errorf("expected same index for equal RefType params")
	}
}

// TDD: Target extValTypesEqual - different RefType
func TestAddTypeWithDifferentRefType(t *testing.T) {
	m := &wasm.Module{}

	ft1 := wasm.FuncType{
		ExtParams: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: -16}}},
	}
	ft2 := wasm.FuncType{
		ExtParams: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRef, RefType: wasm.RefType{Nullable: false, HeapType: -16}}}, // different nullable
	}

	idx1 := m.AddType(ft1)
	idx2 := m.AddType(ft2)

	if idx1 == idx2 {
		t.Errorf("expected different indices for different RefType nullable")
	}
}
