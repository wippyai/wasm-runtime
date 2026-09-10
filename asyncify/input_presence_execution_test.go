package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestInputPresencePreservesUnreachableExecution(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		calls      int
	}{
		{"unknown_after_call", `call $yield unreachable select select drop`, 1},
		{"unknown_before_call", `unreachable select select drop call $yield`, 0},
		{"unknown_at_call", `unreachable select call $yield drop`, 0},
		{"unknown_at_second_call", `call $yield unreachable select call $yield drop`, 1},
		{"typed_alias", `(local i32) call $yield unreachable local.tee 0 drop`, 1},
		{"nested_frame", `call $yield unreachable block (param i32) (result i32) i32.const 1 i32.add end drop`, 1},
		{"partial_branch", `call $yield block (result i32 i32) unreachable i32.const 7 i32.const 1 br_if 0 end drop drop`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := wat.Compile(`(module (import "env" "yield" (func $yield)) (memory 1) (func (export "run") ` + tc.body + `))`)
			if err != nil {
				t.Fatal(err)
			}
			check := func(code []byte) {
				t.Helper()
				ctx := context.Background()
				runtime := wazero.NewRuntime(ctx)
				defer runtime.Close(ctx)
				calls := 0
				if _, err := runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func() { calls++ }).Export("yield").Instantiate(ctx); err != nil {
					t.Fatal(err)
				}
				instance, err := runtime.Instantiate(ctx, code)
				if err != nil {
					t.Fatalf("module must validate and instantiate: %v", err)
				}
				_, err = instance.ExportedFunction("run").Call(ctx)
				if err == nil || !strings.Contains(err.Error(), "unreachable") {
					t.Fatalf("expected unreachable trap, got %v", err)
				}
				if calls != tc.calls {
					t.Fatalf("host calls=%d, want %d", calls, tc.calls)
				}
			}
			check(raw)
			transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.yield"}})
			if err != nil {
				t.Fatal(err)
			}
			check(transformed)
		})
	}
}
