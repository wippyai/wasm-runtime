package wasm_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestWalkInstructionsPreservesDecodedValues(t *testing.T) {
	values := []wasm.Instruction{
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 129}},
		{Opcode: wasm.OpReturnCall, Imm: wasm.CallImm{FuncIdx: 257}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 2, TableIdx: 1}},
		{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 3}},
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0, 1, 127}, Default: 2}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: -123}},
		{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: -123456789}},
		{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: 1.5}},
		{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: -2.5}},
		{Opcode: wasm.OpI32Load, Imm: wasm.MemoryImm{Align: 2, Offset: 256}},
		{Opcode: wasm.OpMemorySize, Imm: wasm.MemoryIdxImm{MemIdx: 1}},
		{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: -16}},
		{Opcode: wasm.OpRefFunc, Imm: wasm.RefFuncImm{FuncIdx: 2}},
		{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{Types: []wasm.ValType{wasm.ValI32}}},
		{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryCopy, Operands: []uint32{0, 1}}},
		{Opcode: wasm.OpThrow, Imm: wasm.ThrowImm{TagIdx: 1}},
		{Opcode: wasm.OpEnd},
	}
	code := wasm.EncodeInstructions(values)
	var walked []wasm.Instruction
	if err := wasm.WalkInstructions(code, func(i wasm.Instruction) error {
		walked = append(walked, i)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(walked, values) {
		t.Fatalf("walked values differ: got %#v want %#v", walked, values)
	}
	decoded, err := wasm.DecodeInstructions(code)
	if err != nil || !reflect.DeepEqual(decoded, walked) {
		t.Fatalf("slice decoder differs from walk: %v", err)
	}
}

func TestWalkInstructionsErrorsAndEarlyStop(t *testing.T) {
	for _, code := range [][]byte{
		{wasm.OpNop, wasm.OpCall}, {wasm.OpCall, 0x80},
		{wasm.OpF64Const, 0}, {wasm.OpPrefixSIMD},
		{wasm.OpPrefixAtomic}, {wasm.OpPrefixGC}, {0xff},
	} {
		err := wasm.WalkInstructions(code, func(wasm.Instruction) error { return nil })
		decoded, sliceErr := wasm.DecodeInstructions(code)
		if err == nil || sliceErr == nil || err.Error() != sliceErr.Error() || decoded != nil {
			t.Fatalf("error contract differs for %x: walk=%v slice=%v", code, err, sliceErr)
		}
	}
	want := errors.New("stop")
	calls := 0
	if err := wasm.WalkInstructions([]byte{wasm.OpNop, 0xff}, func(wasm.Instruction) error {
		calls++
		return want
	}); !errors.Is(err, want) || calls != 1 {
		t.Fatalf("visitor error not preserved: %v (%d callbacks)", err, calls)
	}
	for _, code := range [][]byte{nil, {}} {
		if err := wasm.WalkInstructions(code, func(wasm.Instruction) error {
			t.Fatal("empty input visited")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		decoded, err := wasm.DecodeInstructions(code)
		if err != nil || decoded == nil || len(decoded) != 0 {
			t.Fatal("empty-input slice contract changed")
		}
	}
}
