package asyncify_test

import "testing"

func TestSourceBranchPolymorphicOperands(t *testing.T) {
	for _, result := range []string{"i32", "i64", "f32", "f64"} {
		for _, branch := range []struct{ name, code string }{
			{"br", "br 0"}, {"br_if", "br_if 0"}, {"br_table", "br_table 0 0"},
		} {
			t.Run(result+"/"+branch.name, func(t *testing.T) {
				checkPolymorphicExit(t, result, `block (result `+result+`) call $yield unreachable select `+branch.code+` end`)
			})
		}
	}
}

func TestSourceBranchIfPreservesFallthroughSnapshot(t *testing.T) {
	body := `(func (export "run") (param $cond i32) (result i64) (local $value i64)
  i64.const 42 local.set $value
  block (result i64)
   local.get $value
   local.get $cond
   call $yield
   br_if 0
   i64.const 999 local.set $value
   i64.const 1 i64.add
  end)`
	t.Run("taken", func(t *testing.T) { runYieldMatrix(t, body, []uint64{2}, 42) })
	t.Run("fallthrough", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 43) })
}

func TestSourceBranchTableSelectsOneTargetAcrossResume(t *testing.T) {
	body := `(func (export "run") (param $index i32) (result i64)
  block $outer (result i64)
   block $inner (result i64)
    i64.const 42 local.get $index call $yield
    br_table $inner $outer $inner $outer
   end
   i64.const 1 i64.add
  end)`
	for _, test := range []struct{ index, want uint64 }{{0, 43}, {1, 42}, {2, 43}, {3, 42}, {0xffffffff, 42}} {
		runYieldMatrix(t, body, []uint64{test.index}, test.want)
	}
}

// An unreachable br_if still pushes the declared label types for validation.
// Later consumers and suspension metadata must never invent carrier values.
func TestSourceBranchDeadFallthroughRetainsDeclaredType(t *testing.T) {
	checkPolymorphicExit(t, "i64", `block (result i64)
  call $yield unreachable br_if 0
  call $yield i64.const 1 i64.add
 end`)
}

func TestSourceBranchDiscardsOnlyActiveValidationSuffix(t *testing.T) {
	body := `(func (export "run") (param $cond i32) (result i64)
  block $done (result i64)
   i32.const 123 local.get $cond
   if
    f64.const -0 i64.const 42 call $yield br $done
   else
    i32.const 99 i64.const 77 call $yield br $done
   end
   drop call $yield i64.const 999
  end)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1}, 42) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 77) })
}

func TestSourceBranchLoopParameterPermutationAcrossResume(t *testing.T) {
	runYieldMatrix(t, `(func (export "run") (result i64) (local $a i64) (local $b i64) (local $n i32)
  i64.const 5 i64.const 9 i32.const 0
  loop $again (param i64 i64 i32) (result i64)
   local.set $n local.set $b local.set $a
   local.get $n i32.eqz if call $yield end
   local.get $n i32.const 3 i32.lt_u
   if
    local.get $b local.get $a local.get $n i32.const 1 i32.add
    br $again
   end
   local.get $a i64.const 100 i64.mul local.get $b i64.add
  end)`, nil, 905)
}
