package asyncify_test

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

const yieldHostModule = `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
`

func runYieldMatrix(t *testing.T, watBody string, args []uint64, want uint64) {
	t.Helper()
	raw, err := wat.Compile(yieldHostModule + watBody + ")")
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
				got, err := inst.ExportedFunction("run").Call(ctx, args...)
				if err != nil {
					t.Fatal(err)
				}
				if variant == "transformed-resume" {
					state, stateErr := inst.ExportedFunction("asyncify_get_state").Call(ctx)
					if stateErr != nil {
						t.Fatal(stateErr)
					}
					if len(state) == 1 && state[0] == 1 {
						if _, err := inst.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
							t.Fatal(err)
						}
						if _, err := inst.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
							t.Fatal(err)
						}
						got, err = inst.ExportedFunction("run").Call(ctx, args...)
						if err != nil {
							t.Fatal(err)
						}
						state, stateErr = inst.ExportedFunction("asyncify_get_state").Call(ctx)
						if stateErr != nil || len(state) != 1 || state[0] != 0 {
							t.Fatalf("expected normal after resume: %v %v", state, stateErr)
						}
					}
				}
				if len(got) != 1 || got[0] != want {
					t.Fatalf("got %v, want [%d]", got, want)
				}
			})
		}
	}
}

func TestNonAsyncResultIfBeforeYield_ResumeUsesRestoredValue(t *testing.T) {
	body := `
  (func (export "run") (param $cond i32) (result i32)
    (local $x i32)
    local.get $cond
    if (result i32)
      i32.const 7
    else
      i32.const 9
    end
    local.set $x
    call $yield
    local.get $x)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1}, 7) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 9) })
}

func TestNonAsyncResultIfAfterYield_RunsOnContinue(t *testing.T) {
	body := `
  (func (export "run") (param $cond i32) (result i32)
    call $yield
    local.get $cond
    if (result i32)
      i32.const 7
    else
      i32.const 9
    end)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1}, 7) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 9) })
}

func TestAsyncIfStillRewindsIntoThen(t *testing.T) {
	body := `
  (func (export "run") (result i32)
    i32.const 1
    if (result i32)
      call $yield
      i32.const 7
    else
      i32.const 9
    end)`
	runYieldMatrix(t, body, nil, 7)
}

func TestAsyncIfStillRewindsIntoElse(t *testing.T) {
	body := `
  (func (export "run") (result i32)
    i32.const 0
    if (result i32)
      i32.const 7
    else
      call $yield
      i32.const 9
    end)`
	runYieldMatrix(t, body, nil, 9)
}

func TestNonAsyncResultIfNestedUnderAsyncIf(t *testing.T) {
	body := `
  (func (export "run") (param $outer i32) (param $inner i32) (result i32)
    (local $x i32)
    local.get $outer
    if (result i32)
      local.get $inner
      if (result i32)
        i32.const 7
      else
        i32.const 9
      end
      local.set $x
      call $yield
      local.get $x
    else
      i32.const 88
    end)`
	t.Run("outerThenInnerThen", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1, 1}, 7) })
	t.Run("outerThenInnerElse", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1, 0}, 9) })
	t.Run("outerElse", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0, 1}, 88) })
}

func TestNonAsyncResultIfNestedAfterYield(t *testing.T) {
	body := `
  (func (export "run") (param $outer i32) (param $inner i32) (result i32)
    call $yield
    local.get $outer
    if (result i32)
      local.get $inner
      if (result i32)
        i32.const 7
      else
        i32.const 9
      end
    else
      i32.const 88
    end)`
	t.Run("outerThenInnerThen", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1, 1}, 7) })
	t.Run("outerThenInnerElse", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1, 0}, 9) })
	t.Run("outerElse", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0, 0}, 88) })
}

func TestNonAsyncResultIfBranchLabel(t *testing.T) {
	body := `
  (func (export "run") (param $cond i32) (result i32)
    (local $x i32)
    local.get $cond
    if (result i32)
      i32.const 7
      br 0
      i32.const 1
    else
      i32.const 9
      br 0
      i32.const 2
    end
    local.set $x
    call $yield
    local.get $x)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{1}, 7) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{0}, 9) })
}

func TestNonAsyncIfParamsResults(t *testing.T) {
	body := `
  (func (export "run") (param $p i32) (param $cond i32) (result i32)
    (local $x i32)
    local.get $p
    local.get $cond
    if (param i32) (result i32)
      i32.const 1
      i32.add
    else
      i32.const 2
      i32.add
    end
    local.set $x
    call $yield
    local.get $x)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 1}, 11) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 0}, 12) })
}

func TestNonAsyncIfParamsResultsAfterYield(t *testing.T) {
	body := `
  (func (export "run") (param $p i32) (param $cond i32) (result i32)
    call $yield
    local.get $p
    local.get $cond
    if (param i32) (result i32)
      i32.const 1
      i32.add
    else
      i32.const 2
      i32.add
    end)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 1}, 11) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 0}, 12) })
}

func TestNonAsyncIfParamsNoElseIdentity(t *testing.T) {
	body := `
  (func (export "run") (param $p i32) (param $cond i32) (result i32)
    (local $x i32)
    local.get $p
    local.get $cond
    if (param i32) (result i32)
      i32.const 1
      i32.add
    end
    local.set $x
    call $yield
    local.get $x)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 1}, 11) })
	t.Run("elseIdentity", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 0}, 10) })
}

func TestNonAsyncIfParamsNoElseIdentityAfterYield(t *testing.T) {
	body := `
  (func (export "run") (param $p i32) (param $cond i32) (result i32)
    call $yield
    local.get $p
    local.get $cond
    if (param i32) (result i32)
      i32.const 1
      i32.add
    end)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 1}, 11) })
	t.Run("elseIdentity", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 0}, 10) })
}

func TestAsyncIfParamsWithElseRewindThen(t *testing.T) {
	body := `
  (func (export "run") (param $p i32) (param $cond i32) (result i32)
    local.get $p
    local.get $cond
    if (param i32) (result i32)
      call $yield
      i32.const 1
      i32.add
    else
      i32.const 2
      i32.add
    end)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 1}, 11) })
	t.Run("else", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 0}, 12) })
}

func TestAsyncIfParamsNoElseIdentity(t *testing.T) {
	body := `
  (func (export "run") (param $p i32) (param $cond i32) (result i32)
    local.get $p
    local.get $cond
    if (param i32) (result i32)
      call $yield
      i32.const 1
      i32.add
    end)`
	t.Run("then", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 1}, 11) })
	t.Run("elseIdentity", func(t *testing.T) { runYieldMatrix(t, body, []uint64{10, 0}, 10) })
}
