package asyncify_test

import "testing"

func TestControlStorageResumedLifetimes(t *testing.T) {
	for _, tc := range []diffTestCase{
		{Name: "sibling_results_retained_across_suspensions", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (result i32)
    block (result i32) i32.const 7 call $yield end
    block (result i32) i32.const 9 call $yield end i32.add
    block (result i32) i32.const 11 call $yield end i32.add))`, ExpectedReturns: []uint64{27}, ExpectedSuspends: 3},
		{Name: "sibling_loop_parameters", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (result i32) (local $counter i32)
    i32.const 3 loop (param i32) (result i32)
     call $yield i32.const 1 i32.sub local.tee $counter
     local.get $counter br_if 0 end
    i32.const 2 loop (param i32) (result i32)
     call $yield i32.const 1 i32.sub local.tee $counter
     local.get $counter br_if 0 end i32.add))`, ExpectedReturns: []uint64{0}, ExpectedSuspends: 5},
		{Name: "condition_survives_nested_and_sibling_arms", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (result i32)
    i32.const 1 if (result i32)
     i32.const 0 if (result i32) i32.const 100 call $yield
     else i32.const 7 call $yield end
    else i32.const 200 call $yield end
    i32.const 0 if (result i32) i32.const 300 call $yield
    else i32.const 9 call $yield end i32.add))`, ExpectedReturns: []uint64{16}, ExpectedSuspends: 2},
		{Name: "sibling_multiresult_branch_tables", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (result i32 i64) (local $wide i64)
    block (result i32 i64) i32.const 7 i64.const 99 call $yield i32.const 0 br_table 0 0 end
    local.set $wide
    block (result i32 i64) i32.const 9 i64.const 101 call $yield i32.const 1 br_table 0 0 end
    local.get $wide i64.add local.set $wide i32.add local.get $wide))`, ExpectedReturns: []uint64{16, 200}, ExpectedSuspends: 2},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			tc.AsyncImports = []string{"env.yield"}
			tc.MaxSuspends = tc.ExpectedSuspends
			runDifferentialScenario(t, tc)
		})
	}
}
