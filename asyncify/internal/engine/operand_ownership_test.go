package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLiteralSnapshotDoesNotOwnLocalZero(t *testing.T) {
	a := NewTempAllocator(0)
	local := a.Alloc(wasm.ValI32)
	if local != 0 {
		t.Fatalf("fixture needs local zero, got %d", local)
	}
	literal, _, err := semantics.ResolveLiteral(wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}})
	if err != nil {
		t.Fatal(err)
	}
	values := []stackEntry{semantics.LiteralOperand(literal)}
	a.PushSnapshot(values)
	a.RestoreStack(values, values)
	a.ClearStack(values)
	a.PopSnapshot()
	a.ReleaseOnPop(local)
	a.EndInstruction()
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	if reused := a.Alloc(wasm.ValI32); reused != local {
		t.Fatalf("literal pinned local zero: next allocation = %d", reused)
	}
}
