package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestCallResolutionFailureDoesNotMutateConsumers(t *testing.T) {
	m := &wasm.Module{Types: []wasm.FuncType{{}}, Funcs: []uint32{0}, Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef)}}}
	for _, instr := range []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 8}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 8}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TableIdx: 8}},
		{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 8}},
	} {
		ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{}, 0, false)
		allocator := NewTempAllocator(1)
		allocator.SetCurrentInstr(0)
		stack := []stackEntry{semantics.StoredOperand(0, wasm.ValI32)}
		ft.simulateInstrStack(&stack, instr, allocator, []wasm.ValType{wasm.ValI32})
		if allocator.Err() == nil || len(stack) != 1 {
			t.Fatalf("simulation accepted or mutated on invalid call: %+v, %v", instr, allocator.Err())
		}
		operandStack := handler.NewStack(0)
		operandStack.Push(0, wasm.ValI32)
		body := &wasm.FuncBody{}
		ctx := handler.NewContext(codegen.NewEmitter(), operandStack, handler.NewLocals(1, body, []wasm.ValType{wasm.ValI32}), 0, 1)
		ctx.Module = m
		if err := ft.emitSingleInstruction(ctx, instr); err == nil {
			t.Fatalf("emitter accepted invalid call: %+v", instr)
		}
		if operandStack.Len() != 1 || ctx.Emit.Len() != 0 || len(body.Locals) != 0 {
			t.Fatal("call resolution failure mutated emitter state")
		}
	}
}

func TestCallConsumersKeepFrozenSignature(t *testing.T) {
	m := &wasm.Module{Types: []wasm.FuncType{{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64, wasm.ValF64}}}, Funcs: []uint32{0}}
	ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{}, 0, false)
	m.Types[0].Params = nil
	m.Types[0].Results[0] = wasm.ValI32
	m.Types[0].Results[1] = wasm.ValI32
	allocator := NewTempAllocator(1)
	allocator.SetCurrentInstr(0)
	stack := []stackEntry{semantics.StoredOperand(0, wasm.ValI32)}
	instr := wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}
	ft.simulateInstrStack(&stack, instr, allocator, []wasm.ValType{wasm.ValI32})
	if allocator.Err() != nil || len(stack) != 2 || stack[0].Type() != wasm.ValI64 || stack[1].Type() != wasm.ValF64 {
		t.Fatalf("planning used changed signature: %+v, %v", stack, allocator.Err())
	}
	operandStack := handler.NewStack(0)
	operandStack.Push(0, wasm.ValI32)
	ctx := handler.NewContext(codegen.NewEmitter(), operandStack, handler.NewLocals(1, &wasm.FuncBody{}, []wasm.ValType{wasm.ValI32}), 0, 1)
	ctx.Module = m
	if err := ft.emitSingleInstruction(ctx, instr); err != nil {
		t.Fatal(err)
	}
	if operandStack.Len() != 2 {
		t.Fatal("emitter used changed parameter count")
	}
	if operandStack.Pop().Type() != wasm.ValF64 || operandStack.Pop().Type() != wasm.ValI64 {
		t.Fatal("emitter used changed result signature")
	}
}
