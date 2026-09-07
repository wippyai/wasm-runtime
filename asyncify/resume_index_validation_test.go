package asyncify_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestResumeRejectsInvalidSavedCallBeforeGuestEffects(t *testing.T) {
	raw, err := wat.Compile(`(module (import "env" "yield" (func $yield)) (memory (export "memory") 1)
 (func (export "run") (result i32) call $yield i32.const 0 i32.const 99 i32.store i32.const 7))`)
	if err != nil {
		t.Fatal(err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{Matcher: asyncify.NewExactMatcher([]string{"env.yield"})})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		for _, index := range []uint32{0, 1, ^uint32(0)} {
			t.Run(fmt.Sprintf("%s/index=%d", backend, index), func(t *testing.T) {
				ctx := context.Background()
				config := wazero.NewRuntimeConfigCompiler()
				if backend == "interpreter" {
					config = wazero.NewRuntimeConfigInterpreter()
				}
				runtime := wazero.NewRuntimeWithConfig(ctx, config)
				defer runtime.Close(ctx)
				calls := 0
				_, err := runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module) {
					calls++
					var err error
					if calls == 1 {
						if !mod.Memory().WriteUint32Le(1024, 2048) || !mod.Memory().WriteUint32Le(1028, 8192) {
							panic("invalid fixture memory")
						}
						_, err = mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024)
					} else {
						_, err = mod.ExportedFunction("asyncify_stop_rewind").Call(ctx)
					}
					if err != nil {
						panic(err)
					}
				}).Export("yield").Instantiate(ctx)
				if err != nil {
					t.Fatal(err)
				}
				module, err := runtime.Instantiate(ctx, transformed)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := module.ExportedFunction("run").Call(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := module.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
					t.Fatal(err)
				}
				saved, ok := module.Memory().ReadUint32Le(2048)
				if !ok || saved != 0 || calls != 1 {
					t.Fatal("fixture did not save first call")
				}
				if !module.Memory().WriteUint32Le(2048, index) {
					t.Fatal("cannot set saved index")
				}
				if _, err := module.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
					t.Fatal(err)
				}
				result, resumeErr := module.ExportedFunction("run").Call(ctx)
				effect, ok := module.Memory().ReadUint32Le(0)
				if !ok {
					t.Fatal("lost fixture memory")
				}
				if index == 0 {
					if resumeErr != nil || len(result) != 1 || result[0] != 7 || effect != 99 || calls != 2 {
						t.Fatalf("valid resume: result=%v error=%v effect=%d calls=%d", result, resumeErr, effect, calls)
					}
				} else if resumeErr == nil || effect != 0 || calls != 1 {
					t.Fatalf("invalid saved index reached guest execution: result=%v error=%v effect=%d calls=%d", result, resumeErr, effect, calls)
				}
			})
		}
	}
}
