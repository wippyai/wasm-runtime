package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestAsyncifyIncludesDirectCallerOfIndirectSuspension(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func))
  (import "env" "async" (func $async (type $t)))
  (table 1 funcref)
  (elem (i32.const 0) $async)
  (memory 1)
  (func $indirect (type $t) i32.const 0 call_indirect (type $t))
  (func (export "run") (type $t) call $indirect)
 )`)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatal(err)
	}
	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatal(err)
	}
	if !marked[1] {
		t.Fatal("indirect helper must be transformed")
	}
	if !marked[2] {
		t.Fatalf("direct caller of indirect suspension not transformed: %v", marked)
	}
}
