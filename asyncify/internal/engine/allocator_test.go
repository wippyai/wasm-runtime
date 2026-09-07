package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestTempAllocator_BasicReuse(t *testing.T) {
	alloc := NewTempAllocator(10)

	// Instruction 0: push two i32 constants
	alloc.SetCurrentInstr(0)
	c1 := alloc.Alloc(wasm.ValI32)
	c2 := alloc.Alloc(wasm.ValI32)
	if c1 != 10 || c2 != 11 {
		t.Fatalf("expected c1=10, c2=11; got %d, %d", c1, c2)
	}
	alloc.EndInstruction()

	// Instruction 1: i32.add pops two, pushes one result
	alloc.SetCurrentInstr(1)
	alloc.ReleaseOnPop(c2)
	alloc.ReleaseOnPop(c1)
	// While inside instruction 1, c1 and c2 are in releasedThisInstr, so refCount > 0
	r1 := alloc.Alloc(wasm.ValI32)
	if r1 == c1 || r1 == c2 {
		t.Fatalf("result r1=%d should not alias inputs c1=%d, c2=%d", r1, c1, c2)
	}
	if r1 != 12 {
		t.Fatalf("expected r1=12; got %d", r1)
	}
	alloc.EndInstruction()

	// Instruction 2: push another constant, should reuse c1 (10) which is now freed!
	alloc.SetCurrentInstr(2)
	c3 := alloc.Alloc(wasm.ValI32)
	if c3 != 10 {
		t.Fatalf("expected c3 to reuse slot 10; got %d", c3)
	}
	alloc.EndInstruction()

	// Total unique locals allocated: 3 (slots 10, 11, 12)
	if alloc.TotalAllocated() != 3 {
		t.Fatalf("expected TotalAllocated=3; got %d", alloc.TotalAllocated())
	}
}

func TestTempAllocator_RepeatedArithmeticBounded(t *testing.T) {
	alloc := NewTempAllocator(5)

	// Simulate 1000 repeated additions:
	// acc = acc + 1
	alloc.SetCurrentInstr(0)
	acc := alloc.Alloc(wasm.ValI32)
	alloc.EndInstruction()

	for i := 1; i <= 1000; i++ {
		alloc.SetCurrentInstr(i * 2)
		c := alloc.Alloc(wasm.ValI32)
		alloc.EndInstruction()

		alloc.SetCurrentInstr(i*2 + 1)
		alloc.ReleaseOnPop(c)
		alloc.ReleaseOnPop(acc)
		res := alloc.Alloc(wasm.ValI32)
		alloc.EndInstruction()
		acc = res
	}

	// Should be strictly bounded to <= 4 locals regardless of 1000 iterations!
	if alloc.TotalAllocated() > 4 {
		t.Fatalf("expected bounded allocations <= 4, got %d", alloc.TotalAllocated())
	}
}

func TestTempAllocator_TypeSegregation(t *testing.T) {
	alloc := NewTempAllocator(20)

	alloc.SetCurrentInstr(0)
	i32Slot := alloc.Alloc(wasm.ValI32)
	i64Slot := alloc.Alloc(wasm.ValI64)
	f32Slot := alloc.Alloc(wasm.ValF32)
	f64Slot := alloc.Alloc(wasm.ValF64)
	alloc.EndInstruction()

	if alloc.AllocatedTypes()[i32Slot] != wasm.ValI32 {
		t.Errorf("i32Slot type mismatch: got %v", alloc.AllocatedTypes()[i32Slot])
	}
	if alloc.AllocatedTypes()[i64Slot] != wasm.ValI64 {
		t.Errorf("i64Slot type mismatch: got %v", alloc.AllocatedTypes()[i64Slot])
	}
	if alloc.AllocatedTypes()[f32Slot] != wasm.ValF32 {
		t.Errorf("f32Slot type mismatch: got %v", alloc.AllocatedTypes()[f32Slot])
	}
	if alloc.AllocatedTypes()[f64Slot] != wasm.ValF64 {
		t.Errorf("f64Slot type mismatch: got %v", alloc.AllocatedTypes()[f64Slot])
	}

	// Free all
	alloc.SetCurrentInstr(1)
	alloc.ReleaseOnPop(i32Slot)
	alloc.ReleaseOnPop(i64Slot)
	alloc.ReleaseOnPop(f32Slot)
	alloc.ReleaseOnPop(f64Slot)
	alloc.EndInstruction()

	// Allocating an i64 must reuse i64Slot, NEVER i32Slot!
	alloc.SetCurrentInstr(2)
	newI64 := alloc.Alloc(wasm.ValI64)
	if newI64 != i64Slot {
		t.Errorf("expected i64 to reuse i64Slot %d, got %d", i64Slot, newI64)
	}
	alloc.EndInstruction()
}

func TestTempAllocator_PinnedSnapshots(t *testing.T) {
	alloc := NewTempAllocator(0)

	// Value pushed before if
	alloc.SetCurrentInstr(0)
	v0 := alloc.Alloc(wasm.ValI32)
	alloc.EndInstruction()

	stack := []stackEntry{semantics.StoredOperand(v0, wasm.ValI32)}

	// OpIf: snapshot taken
	alloc.SetCurrentInstr(1)
	alloc.PushSnapshot(stack)
	alloc.EndInstruction()

	// Inside then: pop v0 and push v1
	alloc.SetCurrentInstr(2)
	alloc.ReleaseOnPop(v0)
	v1 := alloc.Alloc(wasm.ValI32)
	alloc.EndInstruction()

	// v1 must NOT reuse v0 because v0 is pinned in snapshot!
	if v1 == v0 {
		t.Fatalf("v1=%d must not reuse v0=%d pinned in snapshot", v1, v0)
	}

	// OpElse: restore stack
	alloc.SetCurrentInstr(3)
	oldStack := []stackEntry{semantics.StoredOperand(v1, wasm.ValI32)}
	newStack := []stackEntry{semantics.StoredOperand(v0, wasm.ValI32)}
	alloc.RestoreStack(oldStack, newStack)
	alloc.EndInstruction()

	// Inside else: v1 is now freed! Can allocate v2, should reuse v1!
	alloc.SetCurrentInstr(4)
	v2 := alloc.Alloc(wasm.ValI32)
	if v2 != v1 {
		t.Fatalf("expected v2 to reuse freed v1 slot %d, got %d", v1, v2)
	}
	alloc.EndInstruction()

	// OpEnd: pop snapshot
	alloc.SetCurrentInstr(5)
	alloc.PopSnapshot()
	alloc.EndInstruction()
}

func TestTempAllocatorRejectsOwnershipUnderflow(t *testing.T) {
	for _, scenario := range []string{"double-pop", "unknown-local", "snapshot-underflow", "double-clear"} {
		t.Run(scenario, func(t *testing.T) {
			a := NewTempAllocator(10)
			a.SetCurrentInstr(0)
			idx := a.Alloc(wasm.ValI32)
			a.EndInstruction()
			a.SetCurrentInstr(1)
			switch scenario {
			case "double-pop":
				a.ReleaseOnPop(idx)
				a.ReleaseOnPop(idx)
			case "unknown-local":
				a.ReleaseOnPop(idx + 100)
			case "snapshot-underflow":
				a.PopSnapshot()
			case "double-clear":
				stack := []stackEntry{semantics.StoredOperand(idx, wasm.ValI32)}
				a.ClearStack(stack)
				a.ClearStack(stack)
			}
			a.EndInstruction()
			if a.Err() == nil {
				t.Fatal("accepted inconsistent ownership")
			}
		})
	}
}

func TestTempAllocatorSeparatesReplayedRoutingWrites(t *testing.T) {
	a := NewTempAllocator(10)
	a.SetCurrentInstr(0)
	a.ObserveAction(wasm.Instruction{Opcode: wasm.OpI32Const, Synthetic: true}, semantics.GuestExecution)
	guest := a.Alloc(wasm.ValI32)
	a.ReleaseOnPop(guest)
	a.EndInstruction()
	// Normal-path death does not mean a restored continuation's value may be
	// overwritten by routing which executes before that continuation is reached.
	a.SetCurrentInstr(1)
	a.ObserveAction(wasm.Instruction{Opcode: wasm.OpI32Const}, semantics.RewindRouting)
	routing := a.Alloc(wasm.ValI32)
	if routing == guest {
		t.Fatal("routing write aliases restored guest storage")
	}
	a.ReleaseOnPop(routing)
	a.EndInstruction()
	a.SetCurrentInstr(2)
	a.ObserveAction(wasm.Instruction{Opcode: wasm.OpI32Const, Synthetic: true}, semantics.GuestExecution)
	if got := a.Alloc(wasm.ValI32); got != guest {
		t.Fatal("normal execution lost bounded reuse")
	}
	if a.TotalAllocated() != 2 {
		t.Fatal("domains should each need one slot")
	}
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestTempAllocatorRejectsUnknownActionDomain(t *testing.T) {
	a := NewTempAllocator(10)
	a.ObserveAction(wasm.Instruction{Opcode: wasm.OpLocalGet}, semantics.ExecutionDomain(255))
	if a.Err() == nil || a.TotalAllocated() != 0 {
		t.Fatal("unknown action domain was accepted")
	}
}
