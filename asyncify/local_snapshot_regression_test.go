package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// These fixtures compare observable results and host effects, including repeated
// unwind/rewind, across both independent Wazero execution backends.
func TestLocalSnapshotExecution(t *testing.T) {
	for _, fixture := range []struct {
		name, body string
		want       uint64
		yields     int
	}{
		{"duplicate-live-mutation", `i32.const 7 local.set $x
   local.get $x local.get $x i32.const 99 local.set $x
   local.get $x i32.add i32.add call $yield
   local.get $x local.get $x i32.add i32.add call $yield`, 311, 2},
		{"overwritten-storage", `i32.const 7 local.set $x
   local.get $x i32.eqz i32.const 123 local.get $x
   i32.add i32.add call $yield`, 130, 1},
		{"branch-merge", `i32.const 7 local.set $x local.get $x
   i32.const 1 if i32.const 99 local.set $x else i32.const 33 local.set $x end
   local.get $x i32.add call $yield`, 106, 1},
		{"duplicate-branch-owners", `i32.const 7 local.set $x local.get $x local.get $x
            i32.const 0 if i32.const 99 local.set $x else i32.const 88 local.set $x end
            i32.add call $yield local.get $x i32.add`, 102, 1},
		{"loop-repeated-resume", `i32.const 3 local.set $x local.get $x
            loop $again
             local.get $x local.get $x i32.add drop call $yield
             local.get $x i32.const 1 i32.sub local.tee $x br_if $again
            end local.get $x i32.add`, 3, 3},
		{"wide-snapshot", `i64.const 4294967301 local.set $wide
            local.get $wide local.get $wide i64.add call $yield i32.wrap_i64`, 10, 1},
		{"negative-zero-bits", `f32.const -0 local.set $single
            local.get $single call $yield i32.reinterpret_f32`, 2147483648, 1},
		{"closed-loop-mutation", `i32.const 7 local.set $x local.get $x
   loop i32.const 99 local.set $x end local.get $x i32.add call $yield`, 106, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw, err := wat.Compile(`(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result i32) (local $x i32) (local $wide i64) (local $single f32) ` + fixture.body + `))`)
			if err != nil {
				t.Fatal(err)
			}
			transformed, err := asyncify.Transform(raw, asyncify.Config{Matcher: asyncify.NewExactMatcher([]string{"env.yield"})})
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []string{"compiler", "interpreter"} {
				for _, variant := range []string{"original", "transformed", "transformed-resume", "binaryen", "binaryen-resume"} {
					t.Run(backend+"/"+variant, func(t *testing.T) {
						resume := strings.HasSuffix(variant, "-resume")
						ctx := context.Background()
						cfg := wazero.NewRuntimeConfigCompiler()
						if backend == "interpreter" {
							cfg = wazero.NewRuntimeConfigInterpreter()
						}
						rt := wazero.NewRuntimeWithConfig(ctx, cfg)
						defer rt.Close(ctx)
						effects := 0
						_, err := rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module) {
							if !resume {
								effects++
								return
							}
							state, callErr := mod.ExportedFunction("asyncify_get_state").Call(ctx)
							if callErr != nil {
								panic(callErr)
							}
							switch state[0] {
							case 0:
								effects++
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
						if strings.HasPrefix(variant, "binaryen") {
							wasmOpt, _ := discoverWasmOpt(t)
							bytes = compileBinaryen(t, wasmOpt, raw, []string{"env.yield"}, 2)
						}
						inst, err := rt.Instantiate(ctx, bytes)
						if err != nil {
							t.Fatal(err)
						}
						got, err := inst.ExportedFunction("run").Call(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if resume {
							resumes := 0
							for {
								state, stateErr := inst.ExportedFunction("asyncify_get_state").Call(ctx)
								if stateErr != nil || len(state) != 1 {
									t.Fatalf("state: %v %v", state, stateErr)
								}
								if state[0] == 0 {
									break
								}
								if state[0] != 1 || resumes >= fixture.yields {
									t.Fatalf("unexpected suspension %v after %d resumes", state, resumes)
								}
								resumes++
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
							}
							if resumes != fixture.yields {
								t.Fatalf("resumes %d, want %d", resumes, fixture.yields)
							}
						}
						if effects != fixture.yields {
							t.Fatalf("host effects %d, want %d", effects, fixture.yields)
						}
						if len(got) != 1 || got[0] != fixture.want {
							t.Fatalf("got %v, want %d", got, fixture.want)
						}
					})
				}
			}
		})
	}
}
