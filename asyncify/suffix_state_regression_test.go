package asyncify_test

import (
	"fmt"
	"testing"
)

// A suffix after the final suspension site can still branch to value-carrying
// labels. Removing state guards must preserve label depths and branch values.
func TestDifferential_PostSuspendBranchTable(t *testing.T) {
	for _, selector := range []uint64{0, 1, 9} {
		want := uint64(42)
		if selector == 0 {
			want = 43
		}
		t.Run(fmt.Sprint(selector), func(t *testing.T) {
			runDifferentialScenario(t, diffTestCase{
				Name: "post-suspend branch table",
				WAT: `(module
      (import "env" "yield" (func $yield))
      (import "env" "log" (func $log (param i32)))
      (memory (export "memory") 1)
      (func (export "run") (param $selector i32) (result i64)
        (local $answer i64)
        block $outer (result i64)
          block $inner (result i64)
            call $yield
            i64.const 42
            local.get $selector
            br_table $inner $outer
          end
          i64.const 1
          i64.add
        end
        local.set $answer
        i32.const 7
        call $log
        local.get $answer))`,
				AsyncImports: []string{"env.yield"}, EntryArgs: []uint64{selector},
				MaxSuspends: 1, ExpectedSuspends: 1,
				ExpectedReturns: []uint64{want}, ExpectedLogs: []int32{7},
			})
		})
	}
}

func TestDifferential_PostSuspendEvenBranchCondition(t *testing.T) {
	runDifferentialScenario(t, diffTestCase{
		Name: "post-suspend even branch condition",
		WAT: `(module
    (import "env" "yield" (func $yield))
    (import "env" "log" (func $log (param i32)))
    (memory (export "memory") 1)
    (func (export "run") (result i32)
      (local $i i32)
      loop $again
        call $yield
        local.get $i
        call $log
        local.get $i
        i32.const 1
        i32.add
        local.tee $i
        i32.const 3
        i32.lt_u
        i32.const 2
        i32.mul
        br_if $again
      end
      local.get $i
      return))`,
		AsyncImports: []string{"env.yield"}, MaxSuspends: 3, ExpectedSuspends: 3,
		ExpectedReturns: []uint64{3}, ExpectedLogs: []int32{0, 1, 2},
	})
}

func TestDifferential_PostSuspendBrValueNestedBlocks(t *testing.T) {
	for _, targetOuter := range []bool{false, true} {
		want := uint64(102)
		targetVal := uint64(0)
		if targetOuter {
			want = 100
			targetVal = 1
		}
		t.Run(fmt.Sprintf("targetOuter=%v", targetOuter), func(t *testing.T) {
			wat := `(module
      (import "env" "yield" (func $yield))
      (import "env" "log" (func $log (param i32)))
      (memory (export "memory") 1)
      (func (export "run") (param $target i32) (result i64)
        (local $ans i64)
        block $outer (result i64)
          block $inner (result i64)
            call $yield
            local.get $target
            if
              i64.const 100
              br $outer
            end
            i64.const 100
            br $inner
          end
          i64.const 2
          i64.add
        end
        local.set $ans
        i32.const 7
        call $log
        local.get $ans))`

			runDifferentialScenario(t, diffTestCase{
				Name:             "post-suspend br value nested",
				WAT:              wat,
				AsyncImports:     []string{"env.yield"},
				EntryArgs:        []uint64{targetVal},
				MaxSuspends:      1,
				ExpectedSuspends: 1,
				ExpectedReturns:  []uint64{want},
				ExpectedLogs:     []int32{7},
			})
		})
	}
}

func TestDifferential_PostSuspendBrIfValueNestedBlocks(t *testing.T) {
	for _, takeBranch := range []uint64{0, 1} {
		want := uint64(52)
		if takeBranch == 1 {
			want = 50
		}
		t.Run(fmt.Sprintf("takeBranch=%d", takeBranch), func(t *testing.T) {
			wat := `(module
      (import "env" "yield" (func $yield))
      (import "env" "log" (func $log (param i32)))
      (memory (export "memory") 1)
      (func (export "run") (param $take i32) (result i64)
        (local $ans i64)
        block $outer (result i64)
          block $inner (result i64)
            call $yield
            i64.const 50
            local.get $take
            br_if $outer
          end
          i64.const 2
          i64.add
        end
        local.set $ans
        i32.const 7
        call $log
        local.get $ans))`

			runDifferentialScenario(t, diffTestCase{
				Name:             "post-suspend br_if value nested",
				WAT:              wat,
				AsyncImports:     []string{"env.yield"},
				EntryArgs:        []uint64{takeBranch},
				MaxSuspends:      1,
				ExpectedSuspends: 1,
				ExpectedReturns:  []uint64{want},
				ExpectedLogs:     []int32{7},
			})
		})
	}
}

func TestDifferential_PostSuspendBranchTableLoop(t *testing.T) {
	wat := `(module
      (import "env" "yield" (func $yield))
      (import "env" "log" (func $log (param i32)))
      (memory (export "memory") 1)
      (func (export "run") (param $rounds i32) (result i64)
        (local $count i32)
        (local $accum i64)
        block $exit (result i64)
          i64.const 100
          loop $loop (param i64)
            local.set $accum
            call $yield
            local.get $count
            call $log
            local.get $count
            i32.const 1
            i32.add
            local.set $count
            local.get $accum
            i64.const 1
            i64.add
            local.get $count
            local.get $rounds
            i32.lt_s
            if (result i32)
              i32.const 0
            else
              i32.const 1
            end
            br_table $loop $exit
          end
          unreachable
        end))`

	runDifferentialScenario(t, diffTestCase{
		Name:             "post-suspend br_table loop and block",
		WAT:              wat,
		AsyncImports:     []string{"env.yield"},
		EntryArgs:        []uint64{2},
		MaxSuspends:      3,
		ExpectedSuspends: 2,
		ExpectedReturns:  []uint64{102},
		ExpectedLogs:     []int32{0, 1},
	})
}

func TestDifferential_PostSuspendBranchTableMultiValue(t *testing.T) {
	for _, selector := range []uint64{0, 1, 9} {
		want0 := uint64(10)
		want1 := uint64(20)
		if selector == 0 {
			want1 = 21
		}
		t.Run(fmt.Sprint(selector), func(t *testing.T) {
			wat := `(module
      (import "env" "yield" (func $yield))
      (import "env" "log" (func $log (param i32)))
      (memory (export "memory") 1)
      (func (export "run") (param $selector i32) (result i32 i64)
        block $outer (result i32 i64)
          block $inner (result i32 i64)
            call $yield
            i32.const 10
            i64.const 20
            local.get $selector
            br_table $inner $outer
          end
          i64.const 1
          i64.add
        end
        i32.const 7
        call $log))`

			runDifferentialScenario(t, diffTestCase{
				Name:             "post-suspend br_table multi-value",
				WAT:              wat,
				AsyncImports:     []string{"env.yield"},
				EntryArgs:        []uint64{selector},
				MaxSuspends:      1,
				ExpectedSuspends: 1,
				ExpectedReturns:  []uint64{want0, want1},
				ExpectedLogs:     []int32{7},
			})
		})
	}
}

func TestDifferential_PostSuspendBrIfMultiValue(t *testing.T) {
	for _, take := range []uint64{0, 1} {
		want0 := uint64(10)
		want1 := uint64(21)
		if take == 1 {
			want1 = 20
		}
		t.Run(fmt.Sprintf("take=%d", take), func(t *testing.T) {
			wat := `(module
      (import "env" "yield" (func $yield))
      (import "env" "log" (func $log (param i32)))
      (memory (export "memory") 1)
      (func (export "run") (param $take i32) (result i32 i64)
        block $outer (result i32 i64)
          block $inner (result i32 i64)
            call $yield
            i32.const 10
            i64.const 20
            local.get $take
            br_if $outer
          end
          i64.const 1
          i64.add
        end
        i32.const 7
        call $log))`

			runDifferentialScenario(t, diffTestCase{
				Name:             "post-suspend br_if multi-value",
				WAT:              wat,
				AsyncImports:     []string{"env.yield"},
				EntryArgs:        []uint64{take},
				MaxSuspends:      1,
				ExpectedSuspends: 1,
				ExpectedReturns:  []uint64{want0, want1},
				ExpectedLogs:     []int32{7},
			})
		})
	}
}
