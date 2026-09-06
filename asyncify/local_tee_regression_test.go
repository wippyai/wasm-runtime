package asyncify_test

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// local.tee leaves a value on the operand stack, not a reference to the
// mutable guest local. A later assignment must not change that saved value.
func TestLocalTeeOperandSurvivesLocalMutation(t *testing.T) {
	raw, err := wat.Compile(`(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result i32) (local $x i32)
   i32.const 7
   local.tee $x
   i32.const 99
   local.set $x
   call $yield
   local.get $x
   i32.add))`)
	if err != nil {
		t.Fatal(err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{Matcher: asyncify.NewExactMatcher([]string{"env.yield"})})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		for _, variant := range []string{"original", "transformed", "transformed-resume"} {
			t.Run(backend+"/"+variant, func(t *testing.T) {
				ctx := context.Background()
				cfg := wazero.NewRuntimeConfigCompiler()
				if backend == "interpreter" {
					cfg = wazero.NewRuntimeConfigInterpreter()
				}
				rt := wazero.NewRuntimeWithConfig(ctx, cfg)
				defer rt.Close(ctx)
				_, err := rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module) {
					if variant != "transformed-resume" {
						return
					}
					state, callErr := mod.ExportedFunction("asyncify_get_state").Call(ctx)
					if callErr != nil {
						panic(callErr)
					}
					switch state[0] {
					case 0:
						if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 8192) {
							panic("invalid async stack")
						}
						_, callErr = mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024)
					case 2:
						_, callErr = mod.ExportedFunction("asyncify_stop_rewind").Call(ctx)
					default:
						panic("unexpected async state")
					}
					if callErr != nil {
						panic(callErr)
					}
				}).Export("yield").Instantiate(ctx)
				if err != nil {
					t.Fatal(err)
				}
				bytes := raw
				if variant != "original" {
					bytes = transformed
				}
				inst, err := rt.Instantiate(ctx, bytes)
				if err != nil {
					t.Fatal(err)
				}
				got, err := inst.ExportedFunction("run").Call(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if variant == "transformed-resume" {
					state, stateErr := inst.ExportedFunction("asyncify_get_state").Call(ctx)
					if stateErr != nil || len(state) != 1 || state[0] != 1 {
						t.Fatalf("expected unwind: %v %v", state, stateErr)
					}
					if _, err := inst.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
						t.Fatal(err)
					}
					if _, err := inst.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
						t.Fatal(err)
					}
					got, err = inst.ExportedFunction("run").Call(ctx)
					if err != nil {
						t.Fatal(err)
					}
					state, stateErr = inst.ExportedFunction("asyncify_get_state").Call(ctx)
					if stateErr != nil || len(state) != 1 || state[0] != 0 {
						t.Fatalf("expected normal: %v %v", state, stateErr)
					}
				}
				if len(got) != 1 || got[0] != 106 {
					t.Fatalf("got %v, want 106", got)
				}
			})
		}
	}
}
