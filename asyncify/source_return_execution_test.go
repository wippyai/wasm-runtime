package asyncify_test

import "testing"

// Returning consumes the declared result suffix and discards unrelated lower
// operands. A suspend in either arm must resume into that source return while
// the other arm and trailing call retain their original execution behavior.
func TestSourceReturnSelectsResultSuffixAcrossResume(t *testing.T) {
	body := `(func (export "run") (param $cond i32) (result i64)
   i32.const 123
   local.get $cond
   if
    f64.const -0
    i64.const 42
    call $yield
    return
   else
    i32.const 99
    i64.const 77
    call $yield
    return
   end
   drop
   call $yield
   i64.const 999)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1}, 42) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 77) })
}

func TestSourceVoidReturnDiscardsStackAcrossResume(t *testing.T) {
	// The nested helper's void return must discard its lower numeric values,
	// while its caller's i64 result is preserved across helper suspension.
	body := `(func $helper
   i32.const 123 f64.const -0 call $yield return)
  (func (export "run") (result i64) i64.const 42 call $helper)`
	runYieldMatrix(t, body, nil, 42)
}

func TestSourceReturnAfterDeadKnownValuesPreservesTrap(t *testing.T) {
	checkPolymorphicExit(t, "i64", `call $yield unreachable i64.const 42 return`)
}

// The return exits the taken arm, but the non-returning arm still owns the
// enclosing value. Exercise its storage snapshot across mutation and resume.
func TestSourceReturnPreservesEnclosingValueOnOtherPath(t *testing.T) {
	body := `(func (export "run") (param $cond i32) (result i64) (local $value i64)
   i64.const 123 local.set $value
   local.get $value
   local.get $cond
   if
    i64.const 42 call $yield return
   else
    i64.const 999 local.set $value
    call $yield
   end
   i64.const 1 i64.add)`
	t.Run("return", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1}, 42) })
	t.Run("fallthrough", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 124) })
}
