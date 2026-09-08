package engine

import (
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

func TestAsyncifyThreeCoreContinuationAndReuse(t *testing.T) {
	source := twoLevelWrapperWAT
	for _, entry := range []struct{ export, callee string }{{"wrap1", "$host_yield"}, {"wrap2", "$wrap1"}} {
		original := `(func (export "` + entry.export + `") (param $x i32) (result i32)
      (call ` + entry.callee + ` (local.get $x))
    )`
		replacement := `(global $before (export "before") (mut i32) (i32.const 0))
    (global $after (export "after") (mut i32) (i32.const 0))
    (func (export "` + entry.export + `") (param $x i32) (result i32)
      (local $result i32)
      (global.set $before (i32.add (global.get $before) (i32.const 1)))
      (local.set $result (call ` + entry.callee + ` (local.get $x)))
      (global.set $after (i32.add (global.get $after) (i32.const 1)))
      (local.get $result))`
		next := strings.Replace(source, original, replacement, 1)
		if next == source {
			t.Fatalf("fixture did not contain %s", entry.export)
		}
		source = next
	}
	ctx, eng, mod, inst, session := startCrossCoreCall(t, componentFixture(t, source))
	defer eng.Close(ctx)
	defer mod.Close(ctx)
	defer inst.Close(ctx)

	var cores []api.Module
	for _, core := range inst.linkerInst.Modules() {
		if core != nil && core.ExportedGlobal("before") != nil && core.ExportedGlobal("after") != nil {
			cores = append(cores, core)
		}
	}
	if len(cores) != 3 {
		t.Fatalf("found %d effect-bearing cores, want 3", len(cores))
	}
	checkEffects := func(before, after uint64) {
		t.Helper()
		for index, core := range cores {
			if gotBefore, gotAfter := core.ExportedGlobal("before").Get(), core.ExportedGlobal("after").Get(); gotBefore != before || gotAfter != after {
				t.Fatalf("core %d effects = %d,%d, want %d,%d", index, gotBefore, gotAfter, before, after)
			}
		}
	}
	for iteration := range uint64(8) {
		if iteration != 0 {
			var err error
			session, err = inst.StartCall(ctx, "run", uint32(iteration))
			if err != nil {
				t.Fatal(err)
			}
		}
		yielded, err := session.Step(ctx, nil)
		if err != nil || yielded.Status != StepContinue || yielded.PendingOp == nil {
			t.Fatalf("iteration %d yield: %+v, %v", iteration, yielded, err)
		}
		// Snapshot all cores without another guest entry while parked.
		checkEffects(iteration+1, iteration)
		completed, err := session.Step(ctx, &YieldResult{Value: 100 + iteration})
		if err != nil || completed.Status != StepDone {
			t.Fatalf("iteration %d resume: %+v, %v", iteration, completed, err)
		}
		result, err := session.LiftResult(ctx, completed.Results)
		if err != nil || result != uint32(100+iteration) {
			t.Fatalf("iteration %d result = %v, %v", iteration, result, err)
		}
		checkEffects(iteration+1, iteration+1)
	}
}
