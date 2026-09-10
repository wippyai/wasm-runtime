package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestTransformRejectsLocalAliasingGeneratedScratch(t *testing.T) {
	raw, err := wat.Compile(`(module
  (import "env" "yield" (func $yield))
  (memory 1)
  (func (export "run") (result i32)
   local.get 2
   call $yield))`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	if _, err = rt.CompileModule(ctx, raw); err == nil {
		t.Fatal("fixture unexpectedly has a valid local index")
	}
	// Generated scratch locals must not turn the invalid original local index
	// into a valid reference. Resolve original identities before introducing them.
	out, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.yield"}})
	if err == nil {
		if _, compileErr := rt.CompileModule(ctx, out); compileErr != nil {
			t.Fatalf("transform accepted undeclared local; output also invalid: %v", compileErr)
		}
		t.Fatal("transform turned an undeclared guest local into valid generated scratch access")
	}
	if !strings.Contains(err.Error(), "local index 2 out of range") {
		t.Fatalf("unexpected rejection: %v", err)
	}
}
