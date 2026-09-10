package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// A resident temporary remains intact in every case. The rejected plans are
// invalid because the cell-to-snapshot relationship no longer holds.
func TestSnapshotPlanChecksCellKnowledge(t *testing.T) {
	for _, scenario := range []string{"unchanged", "other-cell", "local.set", "local.tee", "region-end", "routing", "capture", "closed-loop"} {
		t.Run(scenario, func(t *testing.T) {
			definition := testDefinition(1, 1, wasm.ValI32)
			definition.LocalRead, definition.SourceLocal = true, 0
			reuse := definition
			reuse.Reuse = true
			plan := map[int][]semantics.Materialization{0: {definition}, 2: {reuse}}
			if scenario == "local.tee" {
				plan[1] = []semantics.Materialization{testDefinition(2, 2, wasm.ValI32)}
			}
			ctx := newTestContext()
			ctx.Locals = NewLocals(0, &wasm.FuncBody{}, []wasm.ValType{wasm.ValI32, wasm.ValI32, wasm.ValI32})
			ctx.Locals.SetAllocationPlan(plan)
			ctx.Locals.BeginAction(0, semantics.GuestExecution, semantics.PreserveLocals)
			ctx.Locals.SnapshotLocal(0, wasm.ValI32)
			switch scenario {
			case "local.set", "local.tee", "other-cell":
				opcode, target := wasm.OpLocalSet, uint32(0)
				if scenario == "local.tee" {
					opcode = wasm.OpLocalTee
				}
				if scenario == "other-cell" {
					target = 2
				}
				instr := wasm.Instruction{Opcode: opcode, Imm: wasm.LocalImm{LocalIdx: target}}
				ctx.Locals.BeginAction(1, semantics.GuestExecution, semantics.LocalKnowledgeEffect(instr, semantics.GuestExecution))
				ctx.Stack.Push(1, wasm.ValI32)
				if err := emitLocalOperation(ctx, instr); err != nil {
					t.Fatal(err)
				}
			case "region-end":
				ctx.Locals.BeginAction(1, semantics.GuestExecution, semantics.ForgetLocals)
			case "routing":
				ctx.Locals.BeginAction(1, semantics.RewindRouting, semantics.PreserveLocals)
			case "capture":
				ctx.Locals.BeginCapture(1)
			case "closed-loop":
				ctx.Locals.BeginClosedRegion(1)
			}
			ctx.Locals.BeginAction(2, semantics.GuestExecution, semantics.PreserveLocals)
			ctx.Locals.SnapshotLocal(0, wasm.ValI32)
			valid := scenario == "unchanged" || scenario == "other-cell"
			if err := ctx.Locals.FinishPlan(); (err == nil) != valid {
				t.Fatalf("valid=%t err=%v", valid, err)
			}
		})
	}
}
