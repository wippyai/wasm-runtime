package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestControlStorageScopeOwnership(t *testing.T) {
	values, err := valueFixture(t, `(module (func (result i32)
  block (result i32) block (result i32) i32.const 7 end end
  block (result i32) i32.const 9 end i32.add))`)
	if err != nil {
		t.Fatal(err)
	}
	root := values.control.root.(*SeqNode)
	outer := root.Children[0].(*BlockNode)
	inner := outer.Body.(*SeqNode).Children[0]
	sibling := root.Children[1]
	next := uint32(10)
	plan, err := planControlStorage(values.control, func(wasm.ValType) uint32 { local := next; next++; return local }, true)
	if err != nil {
		t.Fatal(err)
	}
	if next != 12 {
		t.Fatalf("expected two simultaneously live slots, got %d", next-10)
	}
	if plan.nodes[outer].results[0] == plan.nodes[inner].results[0] {
		t.Fatal("ancestor and descendant share a live carrier")
	}
	if plan.nodes[outer].results[0] != plan.nodes[sibling].results[0] {
		t.Fatal("completed sibling scope did not release its carrier")
	}
	// The unscoped storage primitive cannot use the lexical-scope reuse
	// contract. Production only reaches scoped allocation after its source
	// control-transfer contract is complete.
	next = 10
	_, err = planControlStorage(values.control, func(wasm.ValType) uint32 { local := next; next++; return local }, false)
	if err != nil {
		t.Fatal(err)
	}
	if next != 13 {
		t.Fatal("unscoped storage allowed carrier coalescing")
	}
}

func TestControlStorageRejectsAllocatorAliasing(t *testing.T) {
	values, err := valueFixture(t, `(module (func (result i32) block (result i32) block (result i32) i32.const 1 end end))`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planControlStorage(values.control, func(wasm.ValType) uint32 { return 0 }, true)
	if err == nil || plan != nil {
		t.Fatal("published aliased physical slots")
	}
}

func TestControlStorageVerifierRejectsUnsoundAssignments(t *testing.T) {
	requests := []carrierRequest{{types: []wasm.ValType{wasm.ValI32}}, {types: []wasm.ValType{wasm.ValI32}}}
	events := []storageEvent{{request: 0}, {request: 1}, {request: 1, release: true}, {request: 0, release: true}}
	if err := verifyControlSlots(requests, events, [][]int{{0}, {1}}, []wasm.ValType{wasm.ValI32, wasm.ValI32}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		assignments [][]int
		types       []wasm.ValType
	}{
		{"ancestor-alias", [][]int{{0}, {0}}, []wasm.ValType{wasm.ValI32}},
		{"wrong-type", [][]int{{0}, {1}}, []wasm.ValType{wasm.ValI32, wasm.ValI64}},
		{"missing-carrier", [][]int{{0}, {}}, []wasm.ValType{wasm.ValI32}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyControlSlots(requests, events, tc.assignments, tc.types); err == nil {
				t.Fatal("accepted unsound allocation")
			}
		})
	}
	if err := verifyControlSlots(requests, events[:3], [][]int{{0}, {1}}, []wasm.ValType{wasm.ValI32, wasm.ValI32}); err == nil {
		t.Fatal("accepted unterminated lifetime")
	}
}
