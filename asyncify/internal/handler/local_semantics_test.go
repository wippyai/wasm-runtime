package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLocalEmissionRejectsUndeclaredLocal(t *testing.T) {
	r := NewRegistry()
	RegisterVariableHandlers(r)
	for _, op := range []byte{wasm.OpLocalGet, wasm.OpLocalSet, wasm.OpLocalTee} {
		ctx := newTestContext()
		ctx.Stack.Push(0, wasm.ValI32)
		if err := r.Get(op).Handle(ctx, wasm.Instruction{Opcode: op, Imm: wasm.LocalImm{LocalIdx: 5}}); err == nil {
			t.Fatalf("undeclared local accepted: %#x", op)
		}
		if ctx.Stack.Len() != 1 || ctx.Emit.Len() != 0 {
			t.Fatalf("failed resolution mutated emitter/stack: %#x", op)
		}
	}
}

func TestAliasEmissionPreservesConsumedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		handler     Handler
		instruction wasm.Instruction
		valueType   wasm.ValType
	}{
		{"local.tee", LocalTeeHandler{}, wasm.Instruction{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}}, wasm.ValI32},
		{"ref.as_non_null", RefAsNonNullHandler{}, wasm.Instruction{Opcode: wasm.OpRefAsNonNull}, wasm.ValFuncRef},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext()
			ctx.Locals = NewLocals(1, &wasm.FuncBody{}, []wasm.ValType{tc.valueType})
			ctx.Stack.PushOperand(semantics.StoredOperand(0, tc.valueType).WithBinding(37))
			if err := tc.handler.Handle(ctx, tc.instruction); err != nil {
				t.Fatal(err)
			}
			output := ctx.Stack.Pop()
			token, bound := output.Binding()
			local, stored := output.LocalIndex()
			if !bound || token != 37 || !stored || local == 0 || output.Type() != tc.valueType {
				t.Fatalf("lost forwarded snapshot: %+v", output)
			}
		})
	}
}
