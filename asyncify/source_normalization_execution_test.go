package asyncify_test

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestSourceNormalizationElidesDeadTypedOperations(t *testing.T) {
	for _, test := range []struct{ name, result, body string }{
		{"binary_i64", "i64", "call $yield unreachable i64.add"},
		{"binary_f32", "f32", "call $yield unreachable f32.mul"},
		{"binary_f64_known_suffix", "f64", "call $yield unreachable f64.const 7 f64.sub"},
		{"conversion", "f64", "call $yield unreachable f64.convert_i64_s"},
		{"store", "i64", "call $yield unreachable i64.store i64.const 7"},
		{"dead_call", "i64", "call $yield unreachable call $yield i64.add"},
		{"nested_dead_frame", "i64", "call $yield unreachable block (result i64) unreachable i64.add end"},
		{"branch_escape", "i64", "block (result i64) call $yield i64.const 7 br 0 unreachable i64.add end drop unreachable i64.add"},
	} {
		t.Run(test.name, func(t *testing.T) { checkPolymorphicExit(t, test.result, test.body) })
	}
}

func TestSourceNormalizationOnlyDeadSuspensionKeepsOriginalBody(t *testing.T) {
	raw, err := wat.Compile(`(module (import "env" "yield" (func $yield)) (memory 1)
  (func (export "run") (result i64) unreachable call $yield i64.add))`)
	if err != nil {
		t.Fatal(err)
	}
	original, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.yield"}})
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original.Code[0].Code, module.Code[0].Code) {
		t.Fatal("only dead suspension changed function code")
	}
}

func TestSourceNormalizationDoesNotHideInvalidDeadNestedFrame(t *testing.T) {
	raw, err := wat.Compile(`(module (import "env" "yield" (func $yield)) (memory 1)
  (func (export "run") call $yield unreachable block i64.add drop end))`)
	if err != nil {
		t.Fatal(err)
	}
	// Entering block resets Wasm validation reachability even though execution
	// cannot reach it. Its operand underflow must be rejected before elision.
	if _, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.yield"}}); err == nil {
		t.Fatal("normalization hid invalid nested source")
	}
}

func TestSourceNormalizationTrapArmPreservesOuterPrefix(t *testing.T) {
	runYieldMatrix(t, `(func (export "run") (result i64)
  i64.const 77
  i32.const 1
  if (result i32)
   i32.const 42
  else
   i32.const 99 unreachable i64.add drop
  end
  drop call $yield)`, nil, 77)
}

func TestSourceNormalizationRetainsCallIdentityAcrossElisions(t *testing.T) {
	runYieldMatrix(t, `(func (export "run") (result i64)
  block
   br 0 call $yield
  end
  i64.const 42 call $yield)`, nil, 42)
}
