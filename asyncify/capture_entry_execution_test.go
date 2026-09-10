package asyncify_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// Entry operands must keep their order and exact bits across multiple resumes.
// The host changes the source selector between unwind and rewind, so rereading
// the mutable global instead of preserving the captured selector selects the
// wrong arm. Provisional host results must not replace any captured parameter.
func TestCaptureEntryPreservesOperandsAcrossResumes(t *testing.T) {
	for _, cond := range []uint64{0, 1} {
		t.Run(fmt.Sprintf("selector-%d", cond), func(t *testing.T) {
			raw, err := wat.Compile(`(module
  (import "env" "step" (func $step (param i32) (result i32)))
  (memory (export "memory") 1)
  (global $selector (export "selector") (mut i32) (i32.const 0))
  (func (export "run") (param i32) (result i32 i32 i64 f32 f64 i32)
    local.get 0 global.set $selector
    i32.const 7 i32.const 9 i64.const 1234567890123
    f32.const -0 f64.const -0 global.get $selector
    if (param i32 i32 i64 f32 f64) (result i32 i32 i64 f32 f64 i32)
      i32.const 11 call $step drop
      i32.const 13 call $step drop
      i32.const 101
    else
      i32.const 17 call $step drop
      i32.const 19 call $step drop
      i32.const 202
    end))`)
			if err != nil {
				t.Fatal(err)
			}
			want := []uint64{7, 9, 1234567890123, 0x80000000, 0x8000000000000000, 202}
			calls := []uint32{17, 19}
			if cond == 1 {
				want[5] = 101
				calls = []uint32{11, 13}
			}
			checkCaptureEntryMatrix(t, raw, cond, want, calls)
		})
	}
}

func checkCaptureEntryMatrix(t *testing.T, raw []byte, cond uint64, want []uint64, wantCalls []uint32) {
	t.Helper()
	// Validate and execute the source independently before transforming it.
	for _, backend := range []string{"compiler", "interpreter"} {
		checkCaptureEntryExecution(t, backend, "source", raw, cond, want, wantCalls)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.step"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		checkCaptureEntryExecution(t, backend, "normal", transformed, cond, want, wantCalls)
		checkCaptureEntryExecution(t, backend, "resume", transformed, cond, want, wantCalls)
	}
}

func checkCaptureEntryExecution(t *testing.T, backend, variant string, code []byte, cond uint64, want []uint64, wantCalls []uint32) {
	t.Helper()
	t.Run(backend+"/"+variant, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cfg := wazero.NewRuntimeConfigCompiler()
		if backend == "interpreter" {
			cfg = wazero.NewRuntimeConfigInterpreter()
		}
		rt := wazero.NewRuntimeWithConfig(ctx, cfg.WithCloseOnContextDone(true))
		defer rt.Close(ctx)
		var calls []uint32
		_, err := rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, arg uint32) uint32 {
			calls = append(calls, arg)
			if variant != "resume" {
				return arg
			}
			state, callErr := mod.ExportedFunction("asyncify_get_state").Call(ctx)
			if callErr != nil {
				panic(callErr)
			}
			switch state[0] {
			case 0:
				if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 16384) {
					panic("invalid async stack")
				}
				mod.ExportedGlobal("selector").(api.MutableGlobal).Set(1 - cond)
				_, callErr = mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024)
				if callErr != nil {
					panic(callErr)
				}
				return 0xdeadbeef
			case 2:
				_, callErr = mod.ExportedFunction("asyncify_stop_rewind").Call(ctx)
				if callErr != nil {
					panic(callErr)
				}
				return arg
			default:
				panic("unexpected async state")
			}
		}).Export("step").Instantiate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		mod, err := rt.Instantiate(ctx, code)
		if err != nil {
			t.Fatal(err)
		}
		got, err := mod.ExportedFunction("run").Call(ctx, cond)
		if err != nil {
			t.Fatal(err)
		}
		resumes := 0
		if variant == "resume" {
			for {
				state, stateErr := mod.ExportedFunction("asyncify_get_state").Call(ctx)
				if stateErr != nil || len(state) != 1 {
					t.Fatalf("read async state: %v, %v", state, stateErr)
				}
				if state[0] == 0 {
					break
				}
				if state[0] != 1 || resumes >= len(wantCalls) {
					t.Fatalf("unexpected state %v after %d resumes", state, resumes)
				}
				if _, err := mod.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := mod.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
					t.Fatal(err)
				}
				got, err = mod.ExportedFunction("run").Call(ctx, cond)
				if err != nil {
					t.Fatal(err)
				}
				resumes++
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("results %x, want %x", got, want)
		}
		expectedCalls := wantCalls
		if variant == "resume" {
			if resumes != len(wantCalls) {
				t.Fatalf("resumes=%d, want %d", resumes, len(wantCalls))
			}
			expectedCalls = nil
			for _, arg := range wantCalls {
				expectedCalls = append(expectedCalls, arg, arg)
			}
		}
		if !slices.Equal(calls, expectedCalls) {
			t.Fatalf("host arguments %v, want %v", calls, expectedCalls)
		}
	})
}

// All these source modules are valid, including unknown values produced by
// select in an unreachable frame. They trap before capture; transformation
// must still preserve validation and must not invent a concrete fallback input.
func TestCaptureEntryPolymorphicInputs(t *testing.T) {
	for _, tc := range []struct{ prefix, params, drops string }{
		{"unreachable", "i32 i32", "drop drop"},
		{"unreachable select", "i32 i32", "drop drop"},
		{"unreachable select select", "i32 i32", "drop drop"},
		{"unreachable i32.const 7 i32.const 1", "i32 i32", "drop drop"},
		{"unreachable select i32.const 1", "i64", "drop"},
		{"unreachable select i32.const 1", "f64", "drop"},
	} {
		t.Run(strings.ReplaceAll(tc.prefix+"/"+tc.params, " ", "_"), func(t *testing.T) {
			raw, err := wat.Compile(`(module
 (import "env" "yield" (func $yield)) (memory 1)
 (func (export "run")
  ` + tc.prefix + `
  if (param ` + tc.params + `) (result ` + tc.params + `)
    call $yield
  else
    call $yield
  end ` + tc.drops + `))`)
			if err != nil {
				t.Fatal(err)
			}
			check := func(code []byte) {
				t.Helper()
				ctx := context.Background()
				rt := wazero.NewRuntime(ctx)
				defer rt.Close(ctx)
				calls := 0
				if _, err := rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func() { calls++ }).Export("yield").Instantiate(ctx); err != nil {
					t.Fatal(err)
				}
				mod, err := rt.Instantiate(ctx, code)
				if err != nil {
					t.Fatalf("module must validate: %v", err)
				}
				if _, err := mod.ExportedFunction("run").Call(ctx); err == nil || !strings.Contains(err.Error(), "unreachable") {
					t.Fatalf("expected unreachable trap: %v", err)
				}
				if calls != 0 {
					t.Fatalf("unreachable host calls: %d", calls)
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
