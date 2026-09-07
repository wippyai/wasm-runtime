package asyncify_test

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestTransformRejectsInvalidSourceOperands(t *testing.T) {
	for _, tc := range []struct {
		name, body     string
		oldOutputValid bool
	}{
		{"extra-results", `call $yield i32.const 7 i32.const 9`, true},
		{"missing-operand", `call $yield i32.const 7 i32.add`, true},
		{"cross-block-operand", `i32.const 7 block call $yield i32.const 9 i32.add end`, true},
		{"wrong-numeric-type", `call $yield f64.const 7 i32.const 9 i32.add`, false},
		{"typed-tee-after-unreachable", `(local i32) call $yield unreachable local.tee 0 i64.eqz`, false},
		{"typed-branch-after-unreachable", `call $yield block (result i32) unreachable br_if 0 i64.eqz end`, false},
		{"incompatible-branch-targets", `block (result i32) block (result i64) i64.const 7 call $yield i32.const 0 br_table 0 1 end drop i32.const 9 end`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := wat.Compile(`(module (import "env" "yield" (func $yield)) (memory 1) (func (result i32) ` + tc.body + `))`)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			runtime := wazero.NewRuntime(ctx)
			defer runtime.Close(ctx)
			if _, err := runtime.CompileModule(ctx, raw); err == nil {
				t.Fatal("expected independently invalid source")
			}
			out, err := asyncify.Transform(raw, asyncify.Config{Matcher: asyncify.NewExactMatcher([]string{"env.yield"})})
			if err == nil {
				if tc.oldOutputValid {
					if _, compileErr := runtime.CompileModule(ctx, out); compileErr != nil {
						t.Fatalf("accepted invalid source, output also invalid: %v", compileErr)
					}
				}
				t.Fatal("accepted invalid source operand semantics")
			}
		})
	}
}
