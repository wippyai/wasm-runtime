package asyncify_test

import (
	"context"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// Keep the argument computed: direct literals bypass storage and would no longer
// exercise the provisional-result overwrite regression this test protects.
func TestCallUnwindResultPreservesRewindArgument(t *testing.T) {
	raw, err := wat.Compile(`(module (import "env" "echo" (func $echo (param i32) (result i32)))
  (memory (export "memory") 1) (func (export "run") (result i32) i32.const 40 i32.const 2 i32.add call $echo))`)
	if err != nil {
		t.Fatal(err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.echo"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			config := wazero.NewRuntimeConfigCompiler()
			if backend == "interpreter" {
				config = wazero.NewRuntimeConfigInterpreter()
			}
			runtime := wazero.NewRuntimeWithConfig(ctx, config.WithCloseOnContextDone(true))
			defer runtime.Close(ctx)
			var arguments []uint32
			if _, err := runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func(ctx context.Context, module api.Module, argument uint32) uint32 {
				arguments = append(arguments, argument)
				if len(arguments) == 1 {
					if !module.Memory().WriteUint32Le(1024, 1032) || !module.Memory().WriteUint32Le(1028, 8192) {
						panic("stack setup failed")
					}
					if _, err := module.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024); err != nil {
						panic(err)
					}
					// The caller must store this provisional result without overwriting42.
					return 0xdead
				}
				if len(arguments) != 2 {
					panic("unexpected host replay")
				}
				if _, err := module.ExportedFunction("asyncify_stop_rewind").Call(ctx); err != nil {
					panic(err)
				}
				return argument
			}).Export("echo").Instantiate(ctx); err != nil {
				t.Fatal(err)
			}
			module, err := runtime.Instantiate(ctx, transformed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := module.ExportedFunction("run").Call(ctx); err != nil {
				t.Fatal(err)
			}
			state, err := module.ExportedFunction("asyncify_get_state").Call(ctx)
			if err != nil || state[0] != 1 {
				t.Fatalf("did not unwind: state=%v err=%v", state, err)
			}
			if _, err := module.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := module.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
				t.Fatal(err)
			}
			result, err := module.ExportedFunction("run").Call(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(arguments) != 2 || arguments[0] != 42 || arguments[1] != 42 || len(result) != 1 || result[0] != 42 {
				t.Fatalf("argument/result corrupted: args=%v result=%v", arguments, result)
			}
		})
	}
}
