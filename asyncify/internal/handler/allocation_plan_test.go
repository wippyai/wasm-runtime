package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestAllocationPlanRejectsDivergence(t *testing.T) {
	for _, scenario := range []string{"missing", "exhausted", "underconsumed", "wrong-type", "out-of-range", "unvisited", "revisited"} {
		t.Run(scenario, func(t *testing.T) {
			body := &wasm.FuncBody{}
			locals := NewLocals(0, body, []wasm.ValType{wasm.ValI32, wasm.ValI64})
			plan := map[int][]semantics.Materialization{0: {testDefinition(1, 0, wasm.ValI32)}}
			if scenario == "missing" {
				plan = map[int][]semantics.Materialization{}
			}
			if scenario == "out-of-range" {
				plan[0] = []semantics.Materialization{testDefinition(1, 9, wasm.ValI32)}
			}
			locals.SetAllocationPlan(plan)
			if scenario != "unvisited" {
				locals.BeginAction(0, semantics.GuestExecution, semantics.PreserveLocals)
			}
			switch scenario {
			case "underconsumed":
				locals.BeginAction(1, semantics.GuestExecution, semantics.PreserveLocals)
			case "unvisited":
			case "wrong-type":
				locals.Alloc(wasm.ValI64)
			default:
				idx := locals.Alloc(wasm.ValI32)
				if (scenario == "missing" || scenario == "out-of-range") && idx < 2 {
					t.Fatal("fallback aliased a planned local")
				}
				if scenario == "exhausted" {
					if locals.Alloc(wasm.ValI32) < 2 {
						t.Fatal("fallback aliased a planned local")
					}
				}
				if scenario == "revisited" {
					locals.BeginAction(0, semantics.GuestExecution, semantics.PreserveLocals)
					locals.Alloc(wasm.ValI32)
				}
			}
			if err := locals.FinishPlan(); err == nil {
				t.Fatal("accepted inconsistent allocation plan")
			}
		})
	}
}

func TestAllocationPlanAllowsTypedReuse(t *testing.T) {
	locals := NewLocals(0, &wasm.FuncBody{}, []wasm.ValType{wasm.ValI32, wasm.ValI64})
	locals.SetAllocationPlan(map[int][]semantics.Materialization{0: {testDefinition(1, 0, wasm.ValI32), testDefinition(2, 1, wasm.ValI64)}, 2: {testDefinition(3, 0, wasm.ValI32)}})
	locals.BeginAction(0, semantics.GuestExecution, semantics.PreserveLocals)
	if locals.Alloc(wasm.ValI32) != 0 || locals.Alloc(wasm.ValI64) != 1 {
		t.Fatal("wrong planned locals")
	}
	locals.BeginAction(1, semantics.GuestExecution, semantics.PreserveLocals)
	locals.BeginAction(2, semantics.GuestExecution, semantics.PreserveLocals)
	if locals.Alloc(wasm.ValI32) != 0 {
		t.Fatal("lost planned reuse")
	}
	if err := locals.FinishPlan(); err != nil {
		t.Fatal(err)
	}
}

func testDefinition(id uint64, index uint32, vt wasm.ValType) semantics.Materialization {
	return semantics.Materialization{Value: semantics.Value{ID: id, LocalIdx: index, Type: vt}}
}
