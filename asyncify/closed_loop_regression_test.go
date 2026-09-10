package asyncify_test

import "testing"

const closedIncrementLoopWAT = `
    (local $i i32)
    (loop $l
      (local.get $i)
      (local.get $n)
      (i32.lt_u)
      (if
        (then
          (local.set $i (i32.add (local.get $i) (i32.const 1)))
          (br $l))))`

func TestClosedVoidLoopBeforeYield_ZeroAndMany(t *testing.T) {
	body := `
  (func (export "run") (param $n i32) (result i32)
` + closedIncrementLoopWAT + `
    call $yield
    local.get $i)`
	t.Run("zero", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 0) })
	t.Run("many", func(t *testing.T) { runYieldMatrix(t, body, []uint64{7}, 7) })
}

func TestClosedVoidLoopAfterYield_ZeroAndMany(t *testing.T) {
	body := `
  (func (export "run") (param $n i32) (result i32)
    (local $i i32)
    call $yield
    (loop $l
      (local.get $i)
      (local.get $n)
      (i32.lt_u)
      (if
        (then
          (local.set $i (i32.add (local.get $i) (i32.const 1)))
          (br $l))))
    local.get $i)`
	t.Run("zero", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 0) })
	t.Run("many", func(t *testing.T) { runYieldMatrix(t, body, []uint64{7}, 7) })
}

func TestClosedVoidLoopNestedBranches(t *testing.T) {
	body := `
  (func (export "run") (param $n i32) (result i32)
    (local $i i32)
    call $yield
    (loop $l
      (local.get $i)
      (local.get $n)
      (i32.lt_u)
      (if
        (then
          (local.get $i)
          (i32.const 2)
          (i32.and)
          (if
            (then (local.set $i (i32.add (local.get $i) (i32.const 1))))
            (else (local.set $i (i32.add (local.get $i) (i32.const 1)))))
          (br $l))))
    local.get $i)`
	t.Run("zero", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 0) })
	t.Run("many", func(t *testing.T) { runYieldMatrix(t, body, []uint64{8}, 8) })
}

func TestClosedVoidLoopTeeAfterYield(t *testing.T) {
	body := `
  (func (export "run") (param $n i32) (result i32)
    (local $i i32)
    call $yield
    (loop $l
      (local.get $i)
      (local.get $n)
      (i32.lt_u)
      (if
        (then
          (local.get $i)
          (i32.const 1)
          (i32.add)
          (local.tee $i)
          drop
          (br $l))))
    local.get $i)`
	t.Run("zero", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 0) })
	t.Run("many", func(t *testing.T) { runYieldMatrix(t, body, []uint64{6}, 6) })
}

func TestClosedVoidLoopValueAcrossRegion(t *testing.T) {
	body := `
  (func (export "run") (param $n i32) (result i32)
    (local $i i32)
    call $yield
    i32.const 5
    (loop $l
      (local.get $i)
      (local.get $n)
      (i32.lt_u)
      (if
        (then
          (local.set $i (i32.add (local.get $i) (i32.const 1)))
          (br $l))))
    local.get $i
    i32.add)`
	t.Run("zero", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 5) })
	t.Run("many", func(t *testing.T) { runYieldMatrix(t, body, []uint64{4}, 9) })
}

func TestEscapingBranchLoopStillCorrect(t *testing.T) {
	body := `
  (func (export "run") (param $n i32) (result i32)
    (local $i i32)
    (block $out
      (loop $l
        (br_if $out (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $l)))
    call $yield
    local.get $i)`
	t.Run("zero", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 0) })
	t.Run("many", func(t *testing.T) { runYieldMatrix(t, body, []uint64{5}, 5) })
}

func TestAsyncInsideLoopStillRewinds(t *testing.T) {
	body := `
  (func (export "run") (param $n i32) (result i32)
    (local $i i32)
    (local $once i32)
    (loop $l
      (local.get $i)
      (local.get $n)
      (i32.lt_u)
      (if
        (then
          (local.get $once)
          (i32.eqz)
          (if
            (then
              (local.set $once (i32.const 1))
              (call $yield)))
          (local.set $i (i32.add (local.get $i) (i32.const 1)))
          (br $l))))
    local.get $i)`
	t.Run("zero", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 0) })
	t.Run("many", func(t *testing.T) { runYieldMatrix(t, body, []uint64{4}, 4) })
}
