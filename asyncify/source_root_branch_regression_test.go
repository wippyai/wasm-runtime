package asyncify_test

import (
	"fmt"
	"testing"
)

func TestSourceRootBranchTargets(t *testing.T) {
	for _, index := range []uint64{0, 1, 9} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			runDifferentialScenario(t, diffTestCase{
				Name: "source_root_branch", WAT: `(module
     (import "env" "yield" (func $yield))
     (memory (export "memory") 1)
     (func (export "run") (param $index i32) (result i32)
      block (result i32)
       i32.const 7
       call $yield
       local.get $index
       br_table 0 1
      end))`,
				AsyncImports: []string{"env.yield"}, EntryArgs: []uint64{index},
				ExpectedReturns: []uint64{7}, MaxSuspends: 1, ExpectedSuspends: 1,
			})
		})
	}
}

func TestSourceRootBranchValues(t *testing.T) {
	for _, tc := range []diffTestCase{
		{Name: "direct_function_branch", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (result i32) i32.const 7 call $yield br 0))`,
			ExpectedReturns: []uint64{7}, ExpectedSuspends: 1},
		{Name: "conditional_function_branch_taken", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (param $index i32) (result i32)
    block (result i32) i32.const 7 call $yield local.get $index br_if 1 drop i32.const 9 end))`,
			EntryArgs: []uint64{1}, ExpectedReturns: []uint64{7}, ExpectedSuspends: 1},
		{Name: "conditional_function_branch_fallthrough", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (param $index i32) (result i32)
    block (result i32) i32.const 7 call $yield local.get $index br_if 1 drop i32.const 9 end))`,
			EntryArgs: []uint64{0}, ExpectedReturns: []uint64{9}, ExpectedSuspends: 1},
		{Name: "multiresult_function_branch", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (result i32 i64)
    block (result i32 i64) i32.const 7 i64.const 99 call $yield i32.const 1 br_table 0 1 end))`,
			ExpectedReturns: []uint64{7, 99}, ExpectedSuspends: 1},
		{Name: "loop_parameter_or_function_result", WAT: `(module
   (import "env" "yield" (func $yield)) (memory (export "memory") 1)
   (func (export "run") (result i32) (local $counter i32)
    i32.const 2
    loop (param i32) (result i32)
     i32.const 1 i32.sub local.tee $counter call $yield
     local.get $counter i32.eqz br_table 0 1
    end))`,
			ExpectedReturns: []uint64{0}, ExpectedSuspends: 2},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			tc.AsyncImports = []string{"env.yield"}
			tc.MaxSuspends = tc.ExpectedSuspends
			runDifferentialScenario(t, tc)
		})
	}
}
