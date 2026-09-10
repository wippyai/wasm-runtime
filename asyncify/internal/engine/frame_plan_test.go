package engine

import (
	"math"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestFramePlanTypedLayoutAndOwnership(t *testing.T) {
	types := []wasm.ValType{wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64, wasm.ValV128}
	live := map[uint32]bool{4: true, 2: true, 0: true, 3: true, 1: true}
	plan, err := newFramePlan(types, live)
	if err != nil {
		t.Fatal(err)
	}
	want := []frameSlot{
		{localIdx: 0, valType: wasm.ValI32, offset: 4},
		{localIdx: 1, valType: wasm.ValI64, offset: 8},
		{localIdx: 2, valType: wasm.ValF32, offset: 16},
		{localIdx: 3, valType: wasm.ValF64, offset: 20},
		{localIdx: 4, valType: wasm.ValV128, offset: 28},
	}
	if plan.frameSize != 44 {
		t.Fatalf("frame size %d, want 44", plan.frameSize)
	}
	if len(plan.slots) != len(want) {
		t.Fatalf("slot count %d, want %d", len(plan.slots), len(want))
	}
	for i, w := range want {
		got := plan.slots[i]
		if got != w {
			t.Fatalf("slot %d: %+v, want %+v", i, got, w)
		}
	}
	// Frame metadata must outlive, and be independent of, the analysis inputs.
	types[0] = wasm.ValExtern
	delete(live, 4)
	live[99] = true
	if len(plan.slots) != len(want) {
		t.Fatalf("slot count %d, want %d", len(plan.slots), len(want))
	}
	for i, w := range want {
		got := plan.slots[i]
		if got != w {
			t.Fatalf("input mutation changed slot %d: %+v", i, got)
		}
	}
}

func TestFramePlanRejectsInvalidSavedValues(t *testing.T) {
	cases := []struct {
		name  string
		live  map[uint32]bool
		types []wasm.ValType
	}{
		{name: "missing local", types: nil, live: map[uint32]bool{0: true}},
		{name: "maximum index", types: []wasm.ValType{wasm.ValI32}, live: map[uint32]bool{math.MaxUint32: true}},
		{name: "funcref", types: []wasm.ValType{wasm.ValFuncRef}, live: map[uint32]bool{0: true}},
		{name: "externref", types: []wasm.ValType{wasm.ValExtern}, live: map[uint32]bool{0: true}},
		{name: "unknown type", types: []wasm.ValType{0}, live: map[uint32]bool{0: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if plan, err := newFramePlan(tc.types, tc.live); err == nil || plan != nil {
				t.Fatalf("invalid plan accepted: %+v, %v", plan, err)
			}
		})
	}
}

func TestFramePlanEmptyAndSparseLayout(t *testing.T) {
	empty, err := newFramePlan(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.frameSize != 4 || len(empty.slots) != 0 {
		t.Fatalf("empty frame must contain just call index: %+v", empty)
	}
	types := []wasm.ValType{wasm.ValI32, wasm.ValExtern, wasm.ValF64}
	plan, err := newFramePlan(types, map[uint32]bool{2: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.slots) != 1 {
		t.Fatalf("sparse slot count %d", len(plan.slots))
	}
	slot := plan.slots[0]
	if slot != (frameSlot{localIdx: 2, valType: wasm.ValF64, offset: 4}) || plan.frameSize != 12 {
		t.Fatalf("sparse frame: %+v", plan)
	}
}
