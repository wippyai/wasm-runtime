package semantics

import (
	"math"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLocalValueFlow(t *testing.T) {
	types := []wasm.ValType{wasm.ValI64}
	for _, tc := range []struct {
		op                          byte
		consumes, assigns, snapshot bool
	}{
		{wasm.OpLocalGet, false, false, true},
		{wasm.OpLocalSet, true, true, false},
		{wasm.OpLocalTee, true, true, true},
	} {
		instr := wasm.Instruction{Opcode: tc.op, Imm: wasm.LocalImm{LocalIdx: 0}}
		op, handled, err := ResolveLocal(instr, types)
		if err != nil || !handled {
			t.Fatalf("resolve %#x: %v", tc.op, err)
		}
		if op.Type != wasm.ValI64 || op.Index != 0 {
			t.Fatalf("resolved wrong identity/type: %+v", op)
		}
		if (op.Effects&ConsumeOperand != 0) != tc.consumes || (op.Effects&AssignLocal != 0) != tc.assigns || (op.Effects&ProduceSnapshot != 0) != tc.snapshot {
			t.Fatalf("incorrect value flow for %#x: %+v", tc.op, op)
		}
		types[0] = wasm.ValF32
		if op.Type != wasm.ValI64 {
			t.Fatal("resolved operation aliases mutable type environment")
		}
		types[0] = wasm.ValI64
	}
}

func TestLocalResolutionRejectsMalformedIdentities(t *testing.T) {
	for _, op := range []byte{wasm.OpLocalGet, wasm.OpLocalSet, wasm.OpLocalTee} {
		for _, imm := range []any{nil, wasm.LocalImm{LocalIdx: 1}, wasm.LocalImm{LocalIdx: math.MaxUint32}} {
			_, handled, err := ResolveLocal(wasm.Instruction{Opcode: op, Imm: imm}, []wasm.ValType{wasm.ValI32})
			if !handled || err == nil {
				t.Fatalf("invalid %#x immediate %v accepted", op, imm)
			}
		}
	}
	if _, handled, err := ResolveLocal(wasm.Instruction{Opcode: wasm.OpNop}, nil); handled || err != nil {
		t.Fatalf("other instruction misclassified: %v %v", handled, err)
	}
}
