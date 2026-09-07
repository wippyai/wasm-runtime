package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestIndirectAnalysis_F64ParameterResultBoundary(t *testing.T) {
	// f64 is encoded as 0x7c ('|'). A delimiter-based signature key used to
	// merge (f64)->() with ()->(f64), unnecessarily marking the pure caller.
	data, err := wat.Compile(`(module
  (type $read (func (result f64)))
  (type $write (func (param f64)))
  (import "env" "yield" (func $yield))
  (table 2 funcref)
  (func $suspending (type $read) (result f64) call $yield f64.const 1)
  (func $pure (type $write) (param f64))
  (elem (i32.const 0) $suspending $pure)
  (func (export "run") f64.const 7 i32.const 1 call_indirect (type $write)))`)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatal(err)
	}
	marked, err := New(Config{Matcher: newExactMatcher([]string{"env.yield"})}).findAsyncFuncs(m)
	if err != nil {
		t.Fatal(err)
	}
	if !marked[0] || !marked[1] || marked[2] || marked[3] {
		t.Fatalf("only yield and its direct caller may suspend; got %v", marked)
	}
}
