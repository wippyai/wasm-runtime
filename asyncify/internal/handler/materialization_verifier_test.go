package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestSnapshotPlanRejectsInvalidIdentity(t *testing.T) {
	for _, scenario := range []string{"undefined", "overwritten", "wrong-source", "wrong-origin", "zero", "duplicate-definition", "reuse-temporary", "wrong-type"} {
		t.Run(scenario, func(t *testing.T) {
			source := uint32(7)
			definition := testDefinition(1, 0, wasm.ValI32)
			definition.LocalRead, definition.SourceLocal = true, source
			reuse := definition
			reuse.Reuse = true
			prefix := []semantics.Materialization{definition}
			switch scenario {
			case "undefined":
				prefix = nil
			case "overwritten":
				prefix = append(prefix, testDefinition(2, 0, wasm.ValI32))
			case "wrong-source":
				source = 8
			case "wrong-origin":
				reuse.SourceLocal, source = 8, 8
			case "zero":
				reuse.ID = 0
			case "duplicate-definition":
				reuse.Reuse = false
			case "reuse-temporary":
				prefix[0] = testDefinition(1, 0, wasm.ValI32)
			case "wrong-type":
				reuse.Type = wasm.ValI64
			}
			plan := map[int][]semantics.Materialization{0: prefix, 1: {reuse}}
			l := NewLocals(0, &wasm.FuncBody{}, []wasm.ValType{wasm.ValI32})
			l.SetAllocationPlan(plan)
			l.BeginAction(0, semantics.GuestExecution, semantics.PreserveLocals)
			for _, e := range prefix {
				if e.LocalRead {
					l.SnapshotLocal(e.SourceLocal, e.Type)
				} else {
					l.Alloc(e.Type)
				}
			}
			l.BeginAction(1, semantics.GuestExecution, semantics.PreserveLocals)
			l.SnapshotLocal(source, wasm.ValI32)
			if l.FinishPlan() == nil {
				t.Fatal("accepted invalid value proof")
			}
		})
	}
}

func TestMaterializationPlanIsFrozen(t *testing.T) {
	entry := testDefinition(1, 0, wasm.ValI32)
	entry.LocalRead, entry.SourceLocal = true, 7
	reuse := entry
	reuse.Reuse = true
	plan := map[int][]semantics.Materialization{0: {entry, reuse}}
	types := []wasm.ValType{wasm.ValI32}
	v := newMaterializationVerifier(plan, types)
	plan[0][0].ID = 99
	types[0] = wasm.ValI64
	v.beginInDomain(0, semantics.GuestExecution, semantics.PreserveLocals)
	source := uint32(7)
	if _, ok := v.consume(wasm.ValI32, &source); !ok {
		t.Fatal("plan was externally mutated")
	}
	if _, ok := v.consume(wasm.ValI32, &source); !ok {
		t.Fatal("resident reuse failed")
	}
	if err := v.finish(); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializationExecutionDomains(t *testing.T) {
	for _, tc := range []struct {
		name              string
		slot              uint32
		declared, emitted semantics.ExecutionDomain
		valid             bool
	}{
		{"isolated-routing", 1, semantics.RewindRouting, semantics.RewindRouting, true},
		{"normal-reuse", 0, semantics.GuestExecution, semantics.GuestExecution, true},
		{"cross-domain-slot", 0, semantics.RewindRouting, semantics.RewindRouting, false},
		{"wrong-emission-domain", 1, semantics.RewindRouting, semantics.GuestExecution, false},
		{"unknown-domain", 1, 99, 99, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := testDefinition(1, 0, wasm.ValI32)
			second := testDefinition(2, tc.slot, wasm.ValI32)
			second.Domain = tc.declared
			v := newMaterializationVerifier(map[int][]semantics.Materialization{0: {first}, 1: {second}}, []wasm.ValType{wasm.ValI32, wasm.ValI32})
			v.beginInDomain(0, semantics.GuestExecution, semantics.PreserveLocals)
			if _, ok := v.consume(wasm.ValI32, nil); !ok {
				t.Fatal("rejected initial guest definition")
			}
			v.beginInDomain(1, tc.emitted, semantics.PreserveLocals)
			_, ok := v.consume(wasm.ValI32, nil)
			if ok != tc.valid {
				t.Fatalf("valid=%v accepted=%v", tc.valid, ok)
			}
			if (v.finish() == nil) != tc.valid {
				t.Fatal("final verification disagrees")
			}
			if !tc.valid && v.defined[2] {
				t.Fatal("rejected proof mutated definition ledger")
			}
		})
	}
}
