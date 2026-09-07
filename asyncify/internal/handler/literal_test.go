package handler

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLiteralHandlerRejectsBeforeMutation(t *testing.T) {
	for _, instruction := range []wasm.Instruction{{Opcode: wasm.OpI32Const}, {Opcode: wasm.OpI32Add}, {Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: make([]byte, 15)}}} {
		if err := (LiteralHandler{}).Handle(nil, instruction); err == nil {
			t.Fatal("invalid literal touched emission context")
		}
	}
}

func TestLiteralDefinitionNeedsNoStorage(t *testing.T) {
	for _, instruction := range []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: -17}},
		{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: -1.5}},
		{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: 2.75}},
		{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: make([]byte, 16)}},
	} {
		ctx := newTestContext()
		// No local allocator exists: a literal definition must only produce a value.
		ctx.Locals = nil
		if err := (LiteralHandler{}).Handle(ctx, instruction); err != nil {
			t.Fatal(err)
		}
		if len(ctx.Emit.Bytes()) != 0 {
			t.Fatal("literal definition emitted materialization code")
		}
		snapshot := ctx.Stack.Snapshot()
		ctx.Stack.Clear()
		ctx.Stack.Restore(snapshot)
		operand := ctx.Stack.Pop()
		if _, stored := operand.LocalIndex(); stored {
			t.Fatal("literal acquired a storage slot")
		}
		ctx.Emit.Operand(operand)
		if err := ctx.Emit.Err(); err != nil {
			t.Fatal(err)
		}
		if got, want := ctx.Emit.Bytes(), wasm.EncodeInstructions([]wasm.Instruction{instruction}); !bytes.Equal(got, want) {
			t.Fatalf("restored literal bytes %x, want %x", got, want)
		}
	}
}
