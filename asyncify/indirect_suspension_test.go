package asyncify_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// Helper to execute a full Asyncify suspend and resume cycle on Wazero.
func executeAsyncifyCycle(
	t *testing.T,
	backend string, // "compiler" or "interpreter"
	wasmBytes []byte,
	hostYield func(ctx context.Context, mod api.Module, state uint64),
	entryFunc string,
	entryArgs ...uint64,
) (finalResults []uint64, inst api.Module) {
	t.Helper()
	ctx := context.Background()

	var cfg wazero.RuntimeConfig
	if backend == "interpreter" {
		cfg = wazero.NewRuntimeConfigInterpreter()
	} else {
		cfg = wazero.NewRuntimeConfigCompiler()
	}

	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	t.Cleanup(func() { _ = rt.Close(ctx) })

	_, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module) {
			st, callErr := mod.ExportedFunction("asyncify_get_state").Call(ctx)
			if callErr != nil {
				panic(callErr)
			}
			hostYield(ctx, mod, st[0])
		}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("host module instantiate: %v", err)
	}

	inst, err = rt.Instantiate(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("module instantiate: %v", err)
	}

	// 1. Initial call -> triggers suspension and unwind
	_, err = inst.ExportedFunction(entryFunc).Call(ctx, entryArgs...)
	if err != nil {
		t.Fatalf("initial run call: %v", err)
	}

	st, err := inst.ExportedFunction("asyncify_get_state").Call(ctx)
	if err != nil {
		t.Fatalf("get_state after initial call: %v", err)
	}
	if len(st) != 1 || st[0] != 1 {
		t.Fatalf("expected state 1 (unwinding), got %v", st)
	}

	// 2. Stop unwind
	if _, err := inst.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
		t.Fatalf("stop_unwind: %v", err)
	}

	// 3. Start rewind
	if _, err := inst.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
		t.Fatalf("start_rewind: %v", err)
	}

	// 4. Resume call
	finalResults, err = inst.ExportedFunction(entryFunc).Call(ctx, entryArgs...)
	if err != nil {
		t.Fatalf("resume run call: %v", err)
	}

	st, err = inst.ExportedFunction("asyncify_get_state").Call(ctx)
	if err != nil || len(st) != 1 || st[0] != 0 {
		t.Fatalf("expected normal state 0 after resume, got: %v %v", st, err)
	}

	return finalResults, inst
}

// 1. Static pure table skips instrumentation and runs purely without transformation.
func TestIntegration_StaticPureTableSkipsInstrumentation(t *testing.T) {
	rawWat := `(module
  (type $t (func (result i32)))
  (table 1 funcref)
  (memory (export "memory") 1)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem (i32.const 0) $pure)
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`
	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		Matcher:       asyncify.NewExactMatcher([]string{"env.yield"}),
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform: %v", err)
	}

	origM, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	transM, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that internal functions were not modified (pure table skips instrumentation!)
	for i := range origM.Code {
		if !bytes.Equal(origM.Code[i].Code, transM.Code[i].Code) {
			t.Fatalf("pure table function %d was transformed unexpectedly", i)
		}
	}

	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var cfg wazero.RuntimeConfig
			if backend == "interpreter" {
				cfg = wazero.NewRuntimeConfigInterpreter()
			} else {
				cfg = wazero.NewRuntimeConfigCompiler()
			}
			rt := wazero.NewRuntimeWithConfig(ctx, cfg)
			defer rt.Close(ctx)

			inst, err := rt.Instantiate(ctx, transformed)
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			res, err := inst.ExportedFunction("run").Call(ctx)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(res) != 1 || res[0] != 42 {
				t.Fatalf("expected [42], got %v", res)
			}
		})
	}
}

// 2. Async import target direct + transitive + indirect cycle still suspends with side effects each once.
func TestIntegration_AsyncImportTargetDirectTransitiveIndirectCycle(t *testing.T) {
	// A calls B; B calls C via call_indirect; C calls A (when n > 0) or D (when n == 0); D calls yield.
	// Side effects:
	// effectA, effectB, effectC, effectD counters.
	rawWat := `(module
  (type $sig (func (param i32) (result i32)))
  (type $sig_void (func))
  (import "env" "yield" (func $yield (type $sig_void)))
  (table 1 funcref)
  (memory (export "memory") 1)

  (global $effectsA (mut i32) (i32.const 0))
  (global $effectsB (mut i32) (i32.const 0))
  (global $effectsC (mut i32) (i32.const 0))
  (global $effectsD (mut i32) (i32.const 0))

  (func $c (type $sig) (param $n i32) (result i32)
    global.get $effectsC i32.const 1 i32.add global.set $effectsC
    local.get $n
    i32.eqz
    if (result i32)
      local.get $n
      call $d
    else
      local.get $n
      i32.const 1
      i32.sub
      call $a
    end)

  (func $d (type $sig) (param $n i32) (result i32)
    global.get $effectsD i32.const 1 i32.add global.set $effectsD
    call $yield
    local.get $n
    i32.const 100
    i32.add)

  (elem (i32.const 0) $c)

  (func $b (type $sig) (param $n i32) (result i32)
    global.get $effectsB i32.const 1 i32.add global.set $effectsB
    local.get $n
    i32.const 0
    call_indirect (type $sig))

  (func $a (type $sig) (param $n i32) (result i32)
    global.get $effectsA i32.const 1 i32.add global.set $effectsA
    local.get $n
    call $b)

  (func (export "run") (param $n i32) (result i32)
    local.get $n
    call $a)

  (func (export "get_effects") (result i32 i32 i32 i32)
    global.get $effectsA
    global.get $effectsB
    global.get $effectsC
    global.get $effectsD)
)`
	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		Matcher:       asyncify.NewExactMatcher([]string{"env.yield"}),
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform: %v", err)
	}

	hostYield := func(ctx context.Context, mod api.Module, state uint64) {
		switch state {
		case 0:
			if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 8192) {
				panic("failed to write async stack")
			}
			if _, err := mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024); err != nil {
				panic(err)
			}
		case 2:
			if _, err := mod.ExportedFunction("asyncify_stop_rewind").Call(ctx); err != nil {
				panic(err)
			}
		default:
			panic("invalid state")
		}
	}

	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			// n = 1:
			// Run calls A(1) -> effectsA=1
			// A(1) calls B(1) -> effectsB=1
			// B(1) calls C(1) indirectly -> effectsC=1
			// C(1) calls A(0) [cycle!] -> effectsA=2
			// A(0) calls B(0) -> effectsB=2
			// B(0) calls C(0) indirectly -> effectsC=2
			// C(0) calls D(0) -> effectsD=1
			// D(0) calls yield and suspends!
			// After rewind: each side effect must have executed EXACTLY the above counts (no duplicates)!
			results, inst := executeAsyncifyCycle(t, backend, transformed, hostYield, "run", 1)
			if len(results) != 1 || results[0] != 100 {
				t.Fatalf("expected return value 100, got %v", results)
			}

			effects, err := inst.ExportedFunction("get_effects").Call(context.Background())
			if err != nil {
				t.Fatalf("get_effects: %v", err)
			}
			// effects: [A, B, C, D] = [2, 2, 2, 1]
			if effects[0] != 2 || effects[1] != 2 || effects[2] != 2 || effects[3] != 1 {
				t.Fatalf("side effects repeated or incorrect! got [A=%d, B=%d, C=%d, D=%d], want [2, 2, 2, 1]",
					effects[0], effects[1], effects[2], effects[3])
			}
		})
	}
}

// 3. Real unwind, rewind and cancellation across compiler and interpreter.
func TestIntegration_CompilerAndInterpreter_AbandonUnwind(t *testing.T) {
	rawWat := `(module
  (type $sig (func (result i32)))
  (type $sig_void (func))
  (import "env" "yield" (func $yield (type $sig_void)))
  (table 1 funcref)
  (memory (export "memory") 1)
  (global $effects (mut i32) (i32.const 0))

  (func $target (type $sig) (result i32)
    global.get $effects i32.const 1 i32.add global.set $effects
    call $yield
    global.get $effects i32.const 10 i32.add global.set $effects
    i32.const 99)

  (elem (i32.const 0) $target)

  (func $helper (type $sig) (result i32)
    global.get $effects i32.const 1 i32.add global.set $effects
    i32.const 0
    call_indirect (type $sig))

  (func (export "run") (result i32)
    global.get $effects i32.const 1 i32.add global.set $effects
    call $helper)

  (func (export "get_effects") (result i32)
    global.get $effects)
)`
	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		Matcher:       asyncify.NewExactMatcher([]string{"env.yield"}),
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform: %v", err)
	}

	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var cfg wazero.RuntimeConfig
			if backend == "interpreter" {
				cfg = wazero.NewRuntimeConfigInterpreter()
			} else {
				cfg = wazero.NewRuntimeConfigCompiler()
			}

			rt := wazero.NewRuntimeWithConfig(ctx, cfg)
			defer rt.Close(ctx)

			_, err := rt.NewHostModuleBuilder("env").
				NewFunctionBuilder().
				WithFunc(func(ctx context.Context, mod api.Module) {
					if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 8192) {
						panic("failed to write async stack")
					}
					if _, err := mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024); err != nil {
						panic(err)
					}
				}).
				Export("yield").
				Instantiate(ctx)
			if err != nil {
				t.Fatal(err)
			}

			inst, err := rt.Instantiate(ctx, transformed)
			if err != nil {
				t.Fatal(err)
			}

			// Run until suspension:
			_, err = inst.ExportedFunction("run").Call(ctx)
			if err != nil {
				t.Fatalf("run call: %v", err)
			}

			st, err := inst.ExportedFunction("asyncify_get_state").Call(ctx)
			if err != nil || len(st) != 1 || st[0] != 1 {
				t.Fatalf("expected unwinding state 1, got %v %v", st, err)
			}

			// Effects before suspension: run (+1) -> helper (+1) -> target (+1) = 3
			eff, err := inst.ExportedFunction("get_effects").Call(ctx)
			if err != nil || len(eff) != 1 || eff[0] != 3 {
				t.Fatalf("expected effects=3 before suspension, got %v %v", eff, err)
			}

			// Abandon this suspended call. This checks the transform's control
			// protocol, not scheduler cancellation or context cancellation.
			if _, err := inst.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
				t.Fatal(err)
			}
			// Verify the control state and that a fresh call can suspend again.
			st, err = inst.ExportedFunction("asyncify_get_state").Call(ctx)
			if err != nil || len(st) != 1 || st[0] != 0 {
				t.Fatalf("expected normal state 0 after abandoning unwind, got %v %v", st, err)
			}
			if _, err := inst.ExportedFunction("run").Call(ctx); err != nil {
				t.Fatalf("fresh call after abandoning unwind: %v", err)
			}
			eff, err = inst.ExportedFunction("get_effects").Call(ctx)
			if err != nil || len(eff) != 1 || eff[0] != 6 {
				t.Fatalf("expected exactly three fresh effects and no resumed effects, got %v %v", eff, err)
			}
		})
	}
}

// 4. Duplicate equivalent types end-to-end execution.
func TestIntegration_DuplicateEquivalentTypes_Runtime(t *testing.T) {
	rawWat := `(module
  (type $t0 (func (param i32) (result i32)))
  (type $t1 (func (param i32) (result i32)))
  (type $sig_void (func))
  (import "env" "yield" (func $yield (type $sig_void)))
  (table 1 funcref)
  (memory (export "memory") 1)

  (func $worker (type $t0) (param $x i32) (result i32)
    call $yield
    local.get $x
    i32.const 5
    i32.mul)

  (elem (i32.const 0) $worker)

  (func $caller (type $t1) (param $x i32) (result i32)
    local.get $x
    i32.const 0
    call_indirect (type $t1))

  (func (export "run") (type $t1) (param $x i32) (result i32)
    local.get $x
    call $caller)
)`
	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		Matcher:       asyncify.NewExactMatcher([]string{"env.yield"}),
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform: %v", err)
	}

	hostYield := func(ctx context.Context, mod api.Module, state uint64) {
		switch state {
		case 0:
			if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 8192) {
				panic("failed to write async stack")
			}
			if _, err := mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024); err != nil {
				panic(err)
			}
		case 2:
			if _, err := mod.ExportedFunction("asyncify_stop_rewind").Call(ctx); err != nil {
				panic(err)
			}
		default:
			panic("invalid state")
		}
	}

	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			res, _ := executeAsyncifyCycle(t, backend, transformed, hostYield, "run", 7)
			if len(res) != 1 || res[0] != 35 {
				t.Fatalf("expected 7 * 5 = 35, got %v", res)
			}
		})
	}
}

// 5. Table isolation multi-table end-to-end execution.
func TestIntegration_TableIsolation_Multi_Runtime(t *testing.T) {
	rawWat := `(module
  (type $t (func (result i32)))
  (table 1 funcref)
  (table (export "tbl1") 1 funcref)
  (memory (export "memory") 1)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem 0 (i32.const 0) $pure)
  (elem 1 (i32.const 0) $pure)

  (func $call_tbl0 (result i32)
    i32.const 0
    call_indirect 0 (type $t))

  (func $call_tbl1 (result i32)
    i32.const 0
    call_indirect 1 (type $t))

  (func (export "run_pure") (result i32)
    call $call_tbl0)

  (func (export "run_open") (result i32)
    call $call_tbl1)
)`
	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		Matcher:       asyncify.NewExactMatcher([]string{"env.yield"}),
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform: %v", err)
	}

	origM, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	transM, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatal(err)
	}

	// Func 0: $pure, Func 1: $call_tbl0, Func 2: $call_tbl1, Func 3: run_pure, Func 4: run_open
	// Func 1 ($call_tbl0) and Func 3 (run_pure) target closed table 0 -> NOT transformed!
	if !bytes.Equal(origM.Code[1].Code, transM.Code[1].Code) {
		t.Error("Func 1 ($call_tbl0) was modified unexpectedly")
	}
	if !bytes.Equal(origM.Code[3].Code, transM.Code[3].Code) {
		t.Error("Func 3 (run_pure) was modified unexpectedly")
	}

	// Func 2 ($call_tbl1) and Func 4 (run_open) target exported table 1 -> transformed!
	if bytes.Equal(origM.Code[2].Code, transM.Code[2].Code) {
		t.Error("Func 2 ($call_tbl1) should have been transformed")
	}
	if bytes.Equal(origM.Code[4].Code, transM.Code[4].Code) {
		t.Error("Func 4 (run_open) should have been transformed")
	}

	// Run both in Wazero
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var cfg wazero.RuntimeConfig
			if backend == "interpreter" {
				cfg = wazero.NewRuntimeConfigInterpreter()
			} else {
				cfg = wazero.NewRuntimeConfigCompiler()
			}
			rt := wazero.NewRuntimeWithConfig(ctx, cfg)
			defer rt.Close(ctx)

			inst, err := rt.Instantiate(ctx, transformed)
			if err != nil {
				t.Fatal(err)
			}
			resPure, err := inst.ExportedFunction("run_pure").Call(ctx)
			if err != nil || len(resPure) != 1 || resPure[0] != 42 {
				t.Fatalf("run_pure: %v %v", resPure, err)
			}
			resOpen, err := inst.ExportedFunction("run_open").Call(ctx)
			if err != nil || len(resOpen) != 1 || resOpen[0] != 42 {
				t.Fatalf("run_open: %v %v", resOpen, err)
			}
		})
	}
}

// 6. table.init mutation fallback and exact single execution of caller side effects.
func TestIntegration_TableInit_Suspension_NoRepeatedSideEffects(t *testing.T) {
	rawWat := `(module
  (type $sig (func (result i32)))
  (type $sig_void (func))
  (import "env" "yield" (func $yield (type $sig_void)))
  (table 2 funcref)
  (memory (export "memory") 1)
  (global $effects (mut i32) (i32.const 0))

  (func $worker (type $sig) (result i32)
    call $yield
    i32.const 42)

  (elem $e func $worker)

  (func $caller (type $sig) (result i32)
    ;; Side effect before suspension:
    global.get $effects i32.const 1 i32.add global.set $effects
    ;; Dynamic table mutation via table.init:
    (table.init 0 $e (i32.const 1) (i32.const 0) (i32.const 1))
    ;; Call the dynamically initialized slot:
    i32.const 1
    call_indirect 0 (type $sig)
    ;; Side effect after suspension:
    global.get $effects i32.const 10 i32.add global.set $effects)

  (func (export "run") (type $sig) (result i32)
    call $caller)

  (func (export "get_effects") (result i32)
    global.get $effects)
)`
	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		Matcher:       asyncify.NewExactMatcher([]string{"env.yield"}),
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform: %v", err)
	}

	hostYield := func(ctx context.Context, mod api.Module, state uint64) {
		switch state {
		case 0:
			if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 8192) {
				panic("failed to write async stack")
			}
			if _, err := mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024); err != nil {
				panic(err)
			}
		case 2:
			if _, err := mod.ExportedFunction("asyncify_stop_rewind").Call(ctx); err != nil {
				panic(err)
			}
		default:
			panic("invalid state")
		}
	}

	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			results, inst := executeAsyncifyCycle(t, backend, transformed, hostYield, "run")
			if len(results) != 1 || results[0] != 42 {
				t.Fatalf("expected return value 42, got %v", results)
			}

			effects, err := inst.ExportedFunction("get_effects").Call(context.Background())
			if err != nil {
				t.Fatalf("get_effects: %v", err)
			}
			// effects before indirect call (+1) and after resume (+10) = 11
			// Must NOT repeat side effect before indirect call upon rewind (would be 12)!
			if len(effects) != 1 || effects[0] != 11 {
				t.Fatalf("expected effects = 11 (no repeated side effects), got %v", effects)
			}
		})
	}
}
