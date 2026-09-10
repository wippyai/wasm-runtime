package asyncify_test

import (
	"fmt"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestCompletedCarrierScopesBeforeRewind(t *testing.T) {
	for _, condition := range []uint64{0, 1} {
		t.Run(fmt.Sprint(condition), func(t *testing.T) {
			want, log := uint64(9), int32(101)
			if condition == 0 {
				want, log = 10, 102
			}
			runDifferentialScenario(t, diffTestCase{
				Name: "completed_parameter_if_before_yield", WAT: `(module
     (import "env" "yield" (func $yield)) (import "env" "log" (func $log (param i32)))
     (memory (export "memory") 1)
     (func (export "run") (param $condition i32) (result i32)
      i32.const 7 local.get $condition if (param i32) (result i32)
       i32.const 101 call $log i32.const 2 i32.add
      else i32.const 102 call $log i32.const 3 i32.add end
      call $yield call $yield))`,
				EntryArgs: []uint64{condition}, AsyncImports: []string{"env.yield"}, ExpectedReturns: []uint64{want}, ExpectedLogs: []int32{log}, MaxSuspends: 2, ExpectedSuspends: 2,
			})
		})
	}
	for _, tc := range []diffTestCase{
		{Name: "completed_loop_and_table_before_yield", WAT: `(module
   (import "env" "yield" (func $yield)) (import "env" "log" (func $log (param i32)))
   (memory (export "memory") 1)
   (func (export "run") (result i32) (local $counter i32)
    i32.const 3 loop (param i32) (result i32)
     local.tee $counter local.get $counter call $log
     i32.const 1 i32.sub local.tee $counter local.get $counter br_if 0 end
    block (result i32) i32.const 7 i32.const 99 br_table 0 0 end i32.add
    call $yield call $yield))`, ExpectedReturns: []uint64{7}, ExpectedLogs: []int32{3, 2, 1}, ExpectedSuspends: 2},
		{Name: "active_outer_condition_after_completed_inner", WAT: `(module
   (import "env" "yield" (func $yield)) (import "env" "log" (func $log (param i32)))
   (memory (export "memory") 1)
   (func (export "run") (result i32)
    i32.const 1 if (result i32)
     i32.const 7 i32.const 1 if (param i32) (result i32)
      i32.const 101 call $log i32.const 2 i32.add
     else i32.const 102 call $log i32.const 3 i32.add end
     call $yield
    else i32.const 200 call $log i32.const 99 call $yield end
    call $yield))`, ExpectedReturns: []uint64{9}, ExpectedLogs: []int32{101}, ExpectedSuspends: 2},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			tc.AsyncImports = []string{"env.yield"}
			tc.MaxSuspends = tc.ExpectedSuspends
			runDifferentialScenario(t, tc)
		})
	}
}

func TestCompletedScopeFrameBytes(t *testing.T) {
	tc := diffTestCase{Name: "completed_scope_frame", WAT: `(module
  (import "env" "yield" (func $yield)) (memory (export "memory") 1)
  (func (export "run") (result i32)
   i32.const 7 i32.const 1 if (param i32) (result i32) i32.const 2 i32.add else i32.const 3 i32.add end
   call $yield))`, AsyncImports: []string{"env.yield"}, MaxSuspends: 1, ExpectedSuspends: 1}
	raw, err := wat.Compile(tc.WAT)
	if err != nil {
		t.Fatal(err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: tc.AsyncImports})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			result := executeDiffRuntime(t, backend, transformed, tc, true, true)
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			if len(result.Returns) != 1 || result.Returns[0] != 9 || result.SuspendCount != 1 || result.FinalState != 0 {
				t.Fatalf("wrong resumed behavior: %+v", result)
			}
			// Four bytes for the call index, four for the retained i32 operand.
			if result.PeakFrameBytes != 8 {
				t.Fatalf("frame uses %d bytes, want 8 (no completed-scope carriers)", result.PeakFrameBytes)
			}
		})
	}
}
