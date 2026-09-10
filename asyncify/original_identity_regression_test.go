package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestTransformRejectsOriginalIdentityAliasing(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"global", `(module (import "env" "yield" (func $yield)) (memory 1)
   (func (export "run") (result i32) global.get 0 call $yield))`},
		{"function", `(module (import "env" "yield" (func $yield)) (memory 1)
   (func (export "run") call 4 call $yield))`},
		{"global_export", `(module (memory 1) (export "leaked" (global 0)))`},
		{"function_export", `(module (memory 1) (export "leaked" (func 1)))`},
		{"element_function", `(module (memory 1) (table 1 funcref) (elem (i32.const 0) func 1))`},
		{"memory_export", `(module (export "leaked" (memory 0)))`},
		{"data_memory", `(module (data (i32.const 0) "x"))`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := wat.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			rt := wazero.NewRuntime(ctx)
			defer rt.Close(ctx)
			if _, err := rt.CompileModule(ctx, raw); err == nil {
				t.Fatal("raw invalid identity accepted by independent compiler")
			}
			out, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.yield"}})
			if err == nil {
				if _, compileErr := rt.CompileModule(ctx, out); compileErr != nil {
					t.Fatalf("transform accepted invalid identity; output also invalid: %v", compileErr)
				}
				t.Fatal("transform converted invalid guest identity into valid generated identity")
			}
			if !strings.Contains(err.Error(), "input module identities:") {
				t.Fatalf("rejection did not come from original-module boundary: %v", err)
			}
		})
	}
}
