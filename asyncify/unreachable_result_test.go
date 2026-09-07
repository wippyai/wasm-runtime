package asyncify_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestAsyncifyUnreachableResult(t *testing.T) {
	for _, result := range []string{"i32", "i64", "f32", "f64", "i32 f64"} {
		t.Run(result, func(t *testing.T) {
			raw, err := wat.Compile(fmt.Sprintf(`(module (import "env" "yield" (func $yield)) (memory 1) (func (export "run") (result %s) call $yield unreachable))`, result))
			if err != nil {
				t.Fatal(err)
			}
			transformed, err := asyncify.Transform(raw, asyncify.Config{Matcher: asyncify.NewExactMatcher([]string{"env.yield"})})
			if err != nil {
				t.Fatal(err)
			}
			for _, code := range [][]byte{raw, transformed} {
				ctx := context.Background()
				rt := wazero.NewRuntime(ctx)
				defer rt.Close(ctx)
				if _, err := rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func() {}).Export("yield").Instantiate(ctx); err != nil {
					t.Fatal(err)
				}
				inst, err := rt.Instantiate(ctx, code)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := inst.ExportedFunction("run").Call(ctx); err == nil {
					t.Fatal("unreachable must trap")
				}
			}
		})
	}
}
