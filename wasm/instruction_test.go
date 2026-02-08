package wasm_test

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestControlInstructions(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpUnreachable},
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -2}},
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 1}},
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0, 1, 2}, Default: 3}},
		{Opcode: wasm.OpReturn},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", tt.Opcode, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction, got %d", tt.Opcode, len(decoded))
		}
		if decoded[0].Opcode != tt.Opcode {
			t.Errorf("opcode mismatch: got 0x%02x, want 0x%02x", decoded[0].Opcode, tt.Opcode)
		}
	}
}

func TestCallInstructions(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 42}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 1, TableIdx: 0}},
		{Opcode: wasm.OpReturnCall, Imm: wasm.CallImm{FuncIdx: 10}},
		{Opcode: wasm.OpReturnCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 2, TableIdx: 1}},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", tt.Opcode, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction", tt.Opcode)
		}
	}
}

func TestLocalGlobalInstructions(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 2}},
		{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}},
		{Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 1}},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", tt.Opcode, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction", tt.Opcode)
		}
	}
}

func TestMemoryInstructions(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpI32Load, Imm: wasm.MemoryImm{Align: 2, Offset: 0}},
		{Opcode: wasm.OpI64Load, Imm: wasm.MemoryImm{Align: 3, Offset: 8}},
		{Opcode: wasm.OpF32Load, Imm: wasm.MemoryImm{Align: 2, Offset: 0}},
		{Opcode: wasm.OpF64Load, Imm: wasm.MemoryImm{Align: 3, Offset: 0}},
		{Opcode: wasm.OpI32Store, Imm: wasm.MemoryImm{Align: 2, Offset: 4}},
		{Opcode: wasm.OpI64Store, Imm: wasm.MemoryImm{Align: 3, Offset: 8}},
		{Opcode: wasm.OpMemorySize, Imm: wasm.MemoryIdxImm{MemIdx: 0}},
		{Opcode: wasm.OpMemoryGrow, Imm: wasm.MemoryIdxImm{MemIdx: 0}},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", tt.Opcode, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction", tt.Opcode)
		}
	}
}

func TestConstantInstructions(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: -1}},
		{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 0x7FFFFFFFFFFFFFFF}},
		{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: -0x8000000000000000}},
		{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: 3.14}},
		{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: 2.71828}},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", tt.Opcode, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction", tt.Opcode)
		}
	}
}

func TestRefTypeInstructions(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: -16}},
		{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: -17}},
		{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: 5}},
		{Opcode: wasm.OpRefIsNull},
		{Opcode: wasm.OpRefFunc, Imm: wasm.RefFuncImm{FuncIdx: 42}},
		{Opcode: wasm.OpRefAsNonNull},
		{Opcode: wasm.OpRefEq},
		{Opcode: wasm.OpBrOnNull, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpBrOnNonNull, Imm: wasm.BranchImm{LabelIdx: 1}},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", tt.Opcode, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction", tt.Opcode)
		}
	}
}

func TestTableInstructions(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpTableGet, Imm: wasm.TableImm{TableIdx: 0}},
		{Opcode: wasm.OpTableSet, Imm: wasm.TableImm{TableIdx: 1}},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", tt.Opcode, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction", tt.Opcode)
		}
	}
}

func TestTypedSelect(t *testing.T) {
	tests := []wasm.Instruction{
		{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{Types: []wasm.ValType{wasm.ValI32}}},
		{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{Types: []wasm.ValType{wasm.ValI64}}},
		{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{
			Types:    []wasm.ValType{wasm.ValRefNull},
			ExtTypes: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRefNull, RefType: wasm.RefType{Nullable: true, HeapType: -16}}},
		}},
		{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{
			Types:    []wasm.ValType{wasm.ValRef},
			ExtTypes: []wasm.ExtValType{{Kind: wasm.ExtValKindRef, ValType: wasm.ValRef, RefType: wasm.RefType{Nullable: false, HeapType: 0}}},
		}},
		{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{
			Types:    []wasm.ValType{wasm.ValI32},
			ExtTypes: []wasm.ExtValType{{Kind: wasm.ExtValKindSimple, ValType: wasm.ValI32}},
		}},
	}

	for _, tt := range tests {
		encoded := wasm.EncodeInstructions([]wasm.Instruction{tt})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("SelectType: decode error: %v", err)
		}
		if len(decoded) != 1 {
			t.Fatalf("SelectType: expected 1 instruction, got %d", len(decoded))
		}
		if decoded[0].Opcode != wasm.OpSelectType {
			t.Errorf("expected SelectType opcode")
		}
	}
}

func TestNumericInstructions(t *testing.T) {
	tests := []byte{
		wasm.OpI32Eqz, wasm.OpI32Eq, wasm.OpI32Ne, wasm.OpI32LtS, wasm.OpI32LtU, wasm.OpI32GtS, wasm.OpI32GtU,
		wasm.OpI32LeS, wasm.OpI32LeU, wasm.OpI32GeS, wasm.OpI32GeU,
		wasm.OpI64Eqz, wasm.OpI64Eq, wasm.OpI64Ne, wasm.OpI64LtS, wasm.OpI64LtU, wasm.OpI64GtS, wasm.OpI64GtU,
		wasm.OpI32Clz, wasm.OpI32Ctz, wasm.OpI32Popcnt, wasm.OpI32Add, wasm.OpI32Sub, wasm.OpI32Mul,
		wasm.OpI64Add, wasm.OpI64Sub, wasm.OpI64Mul,
		wasm.OpF32Abs, wasm.OpF32Neg, wasm.OpF32Add, wasm.OpF32Sub, wasm.OpF32Mul, wasm.OpF32Div,
		wasm.OpF64Abs, wasm.OpF64Neg, wasm.OpF64Add, wasm.OpF64Sub, wasm.OpF64Mul, wasm.OpF64Div,
	}

	for _, op := range tests {
		instr := wasm.Instruction{Opcode: op}
		encoded := wasm.EncodeInstructions([]wasm.Instruction{instr})
		decoded, err := wasm.DecodeInstructions(encoded)
		if err != nil {
			t.Fatalf("opcode 0x%02x: decode error: %v", op, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("opcode 0x%02x: expected 1 instruction", op)
		}
		if decoded[0].Opcode != op {
			t.Errorf("opcode mismatch: got 0x%02x, want 0x%02x", decoded[0].Opcode, op)
		}
	}
}

func TestInstructionGetCallTarget(t *testing.T) {
	call := wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 42}}
	idx, ok := call.GetCallTarget()
	if !ok {
		t.Error("expected call target")
	}
	if idx != 42 {
		t.Errorf("expected 42, got %d", idx)
	}

	nop := wasm.Instruction{Opcode: wasm.OpNop}
	_, ok = nop.GetCallTarget()
	if ok {
		t.Error("nop should not have call target")
	}
}

func TestInstructionIsIndirectCall(t *testing.T) {
	callInd := wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}}
	if !callInd.IsIndirectCall() {
		t.Error("expected indirect call")
	}

	call := wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}
	if call.IsIndirectCall() {
		t.Error("call should not be indirect")
	}
}

func TestEncodeInstructionsTo(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 10}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 20}},
		{Opcode: wasm.OpI32Add},
	}

	var buf bytes.Buffer
	wasm.EncodeInstructionsTo(&buf, instrs)

	decoded, err := wasm.DecodeInstructions(buf.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("expected 3 instructions, got %d", len(decoded))
	}
}

func TestUnknownOpcode(t *testing.T) {
	data := []byte{0xFF}
	_, err := wasm.DecodeInstructions(data)
	if err == nil {
		t.Error("expected error for unknown opcode 0xFF")
	}
}
