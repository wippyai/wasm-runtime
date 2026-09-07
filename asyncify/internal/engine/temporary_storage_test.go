package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestStorageDefinitionIdentityAndOwnership(t *testing.T) {
	s := newTemporaryStorage(10)
	first := s.define(wasm.ValI32)
	if !s.retainValue(first) {
		t.Fatal("cannot retain resident value")
	}
	if !s.release(first.LocalIdx) {
		t.Fatal("release failed")
	}
	other := s.define(wasm.ValI32)
	if other.LocalIdx == first.LocalIdx {
		t.Fatal("overwrote a retained value")
	}
	if !s.release(first.LocalIdx) {
		t.Fatal("last release failed")
	}
	replacement := s.define(wasm.ValI32)
	if replacement.LocalIdx != first.LocalIdx || replacement.ID == first.ID {
		t.Fatal("expected fresh definition in released slot")
	}
	if s.retainValue(first) {
		t.Fatal("accepted stale definition with same storage and type")
	}
	if s.release(999) || s.retain(999) {
		t.Fatal("accepted unknown storage")
	}
	if !s.release(replacement.LocalIdx) || s.release(replacement.LocalIdx) {
		t.Fatal("ownership underflow accepted")
	}
	if !s.retainValue(replacement) {
		t.Fatal("dead but resident value is reusable")
	}
	wide := s.define(wasm.ValI64)
	if wide.LocalIdx == first.LocalIdx || wide.LocalIdx == other.LocalIdx {
		t.Fatal("mixed storage types")
	}
}

func TestSnapshotReuseRequiresCellAndStorageProofs(t *testing.T) {
	a := NewTempAllocator(10)
	a.ObserveAction(wasm.Instruction{Opcode: wasm.OpLocalGet}, semantics.GuestExecution)
	first := a.SnapshotLocal(0, wasm.ValI32)
	if a.SnapshotLocal(0, wasm.ValI32) != first {
		t.Fatal("did not reuse immutable snapshot")
	}
	a.ReleaseOnPop(first)
	a.ReleaseOnPop(first)
	a.EndInstruction()
	a.Alloc(wasm.ValI32) // Overwrite the cached snapshot, keeping cell knowledge.
	if a.SnapshotLocal(0, wasm.ValI32) == first {
		t.Fatal("reused overwritten snapshot")
	}
	a.InvalidateLocal(0)
	last := a.SnapshotLocal(0, wasm.ValI32)
	a.ObserveAction(wasm.Instruction{Opcode: wasm.OpCall}, semantics.GuestExecution)
	a.ObserveAction(wasm.Instruction{Opcode: wasm.OpLocalGet}, semantics.GuestExecution)
	if a.SnapshotLocal(0, wasm.ValI32) == last {
		t.Fatal("cell knowledge crossed a call")
	}
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
}
