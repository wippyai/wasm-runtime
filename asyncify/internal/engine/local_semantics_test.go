package engine

import (
	"math"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLocalSimulationRejectsUnknownIdentity(t *testing.T) {
	ft := NewFunctionTransformer(DefaultRegistry(), nil, GlobalIndices{}, 0, false)
	for _, op := range []byte{wasm.OpLocalGet, wasm.OpLocalSet, wasm.OpLocalTee} {
		allocator := NewTempAllocator(1)
		allocator.SetCurrentInstr(0)
		stack := []stackEntry{semantics.StoredOperand(0, wasm.ValI32)}
		ft.simulateInstrStack(&stack, wasm.Instruction{Opcode: op, Imm: wasm.LocalImm{LocalIdx: math.MaxUint32}}, allocator, []wasm.ValType{wasm.ValI32})
		if allocator.Err() == nil {
			t.Fatalf("invalid local accepted by simulation: %#x", op)
		}
		if len(stack) != 1 {
			t.Fatalf("failed resolution mutated stack: %#x", op)
		}
	}
}

func TestAliasSimulationPreservesConsumedIdentity(t *testing.T) {
	ft := NewFunctionTransformer(DefaultRegistry(), nil, GlobalIndices{}, 0, false)
	for _, tc := range []struct {
		name        string
		instruction wasm.Instruction
		valueType   wasm.ValType
	}{
		{"local.tee", wasm.Instruction{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}}, wasm.ValI32},
		{"ref.as_non_null", wasm.Instruction{Opcode: wasm.OpRefAsNonNull}, wasm.ValFuncRef},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allocator := NewTempAllocator(1)
			allocator.SetCurrentInstr(0)
			stack := []stackEntry{semantics.StoredOperand(0, tc.valueType).WithBinding(37)}
			ft.simulateInstrStack(&stack, tc.instruction, allocator, []wasm.ValType{tc.valueType})
			if err := allocator.Err(); err != nil {
				t.Fatal(err)
			}
			if len(stack) != 1 {
				t.Fatal("alias changed stack arity")
			}
			token, bound := stack[0].Binding()
			local, stored := stack[0].LocalIndex()
			if !bound || token != 37 || !stored || local == 0 || stack[0].Type() != tc.valueType {
				t.Fatalf("lost forwarded snapshot: %+v", stack[0])
			}
		})
	}
}
