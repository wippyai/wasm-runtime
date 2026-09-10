package asyncify_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// TestTempReuse_RepeatedArithmeticThousandInstrBounded verifies that 1000 repeated
// arithmetic instructions with low stack depth result in a strictly bounded local count
// (rather than exploding to 1000-2000 locals) and execute correctly.
func TestTempReuse_RepeatedArithmeticThousandInstrBounded(t *testing.T) {
	ctx := context.Background()

	// Generate 1000 additions:
	// sum = 0 + 1 + 1 + 1 + ... + 1
	var sb strings.Builder
	sb.WriteString(`(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "compute") (result i32)
			(call $yield (i32.const 1))
			(i32.const 0)
	`)
	for i := 0; i < 1000; i++ {
		sb.WriteString("			(i32.const 1)\n")
		sb.WriteString("			(i32.add)\n")
	}
	sb.WriteString(`		)
		(memory (export "memory") 1)
	)`)

	rawWasm, err := wat.Compile(sb.String())
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		Matcher: asyncify.NewExactMatcher([]string{"env.yield"}),
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	mod, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("wasm.ParseModule failed: %v", err)
	}

	// Function 0 in Code is the original "compute" function (subsequent functions are asyncify helpers)
	body := mod.Code[0]
	totalLocals := uint32(0)
	for _, le := range body.Locals {
		totalLocals += le.Count
	}

	// Previously without reuse: 1000 instructions allocated >1000 locals.
	// With bounded reuse: totalLocals should be <= 20 (including scratch locals).
	t.Logf("1000 arithmetic instructions produced totalLocals = %d", totalLocals)
	if totalLocals > 20 {
		t.Fatalf("temporary local count exploded! expected <= 20, got %d", totalLocals)
	}

	// Verify execution in Wazero
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	var yieldCalls []uint32
	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(param uint32) {
			yieldCalls = append(yieldCalls, param)
		}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate env: %v", err)
	}

	inst, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate transformed: %v", err)
	}
	defer inst.Close(ctx)

	compute := inst.ExportedFunction("compute")
	if compute == nil {
		t.Fatal("missing compute export")
	}

	results, err := compute.Call(ctx)
	if err != nil {
		t.Fatalf("compute() error: %v", err)
	}
	if len(results) != 1 || results[0] != 1000 {
		t.Fatalf("expected result 1000, got %v", results)
	}
	if len(yieldCalls) != 1 || yieldCalls[0] != 1 {
		t.Fatalf("expected yield call with 1, got %v", yieldCalls)
	}
}

// TestTempReuse_MixedValTypes verifies that temporary locals for i32, i64, f32, and f64
// are segregated into per-type pools and bounded, and all produce accurate arithmetic results.
func TestTempReuse_MixedValTypes(t *testing.T) {
	ctx := context.Background()

	watSrc := `(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "testMixed") (result i32 i64 f32 f64)
			(local $i i32)
			(call $yield (i32.const 0))
			;; Low stack depth interleaved arithmetic
			(i32.const 10)
			(i32.const 20)
			(i32.add)
			(i32.const 5)
			(i32.sub) ;; i32 = 25

			(i64.const 100000000000)
			(i64.const 200000000000)
			(i64.add)
			(i64.const 50000000000)
			(i64.sub) ;; i64 = 250000000000

			(f32.const 1.5)
			(f32.const 2.5)
			(f32.add)
			(f32.const 0.5)
			(f32.mul) ;; f32 = 2.0

			(f64.const 10.5)
			(f64.const 20.5)
			(f64.add)
			(f64.const 2.0)
			(f64.div) ;; f64 = 15.5
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		Matcher: asyncify.NewExactMatcher([]string{"env.yield"}),
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	mod, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("wasm.ParseModule failed: %v", err)
	}

	body := mod.Code[0]
	totalLocals := uint32(0)
	for _, le := range body.Locals {
		totalLocals += le.Count
	}
	t.Logf("mixed ValTypes totalLocals = %d", totalLocals)
	if totalLocals > 25 {
		t.Fatalf("expected <= 25 locals for mixed types, got %d", totalLocals)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(param uint32) {}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate env: %v", err)
	}

	inst, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate transformed: %v", err)
	}
	defer inst.Close(ctx)

	fn := inst.ExportedFunction("testMixed")
	results, err := fn.Call(ctx)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	resI32 := uint32(results[0])
	resI64 := int64(results[1])
	resF32 := math.Float32frombits(uint32(results[2]))
	resF64 := math.Float64frombits(results[3])

	if resI32 != 25 {
		t.Errorf("expected i32=25, got %d", resI32)
	}
	if resI64 != 250000000000 {
		t.Errorf("expected i64=250000000000, got %d", resI64)
	}
	if resF32 != 2.0 {
		t.Errorf("expected f32=2.0, got %f", resF32)
	}
	if resF64 != 15.5 {
		t.Errorf("expected f64=15.5, got %f", resF64)
	}
}

// TestTempReuse_NestedIfElse verifies that pinned snapshots preserve values
// pushed before outer and inner if blocks across multiple levels of nesting.
func TestTempReuse_NestedIfElse(t *testing.T) {
	ctx := context.Background()

	watSrc := `(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "testNested") (param $c1 i32) (param $c2 i32) (result i32)
			(local $res i32)
			(call $yield (i32.const 0))
			;; Push outer value 100
			(i32.const 100)
			(if (param i32) (local.get $c1)
				(then
					;; Push inner value 200
					(i32.const 200)
					(if (param i32) (local.get $c2)
						(then
							;; c1 true, c2 true
							(i32.const 1)
							(i32.add)
							(local.set $res)
						)
						(else
							;; c1 true, c2 false
							(i32.const 2)
							(i32.add)
							(local.set $res)
						)
					)
					;; Outer value 100 is still on stack! Add 10 to it.
					(i32.const 10)
					(i32.add)
					(local.set $res (i32.add (local.get $res)))
				)
				(else
					;; c1 false
					(i32.const 300)
					(if (param i32) (local.get $c2)
						(then
							(i32.const 3)
							(i32.add)
							(local.set $res)
						)
						(else
							(i32.const 4)
							(i32.add)
							(local.set $res)
						)
					)
					;; Outer value 100 is still on stack! Add 20 to it.
					(i32.const 20)
					(i32.add)
					(local.set $res (i32.add (local.get $res)))
				)
			)
			(local.get $res)
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	validator := wazero.NewRuntime(ctx)
	defer validator.Close(ctx)
	if _, err := validator.CompileModule(ctx, rawWasm); err != nil {
		t.Fatalf("invalid source fixture: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		Matcher: asyncify.NewExactMatcher([]string{"env.yield"}),
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(param uint32) {}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate env: %v", err)
	}

	inst, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate transformed: %v", err)
	}
	defer inst.Close(ctx)

	fn := inst.ExportedFunction("testNested")

	// Test all 4 permutations of (c1, c2):
	// 1. (1, 1): res = (200 + 1) + (100 + 10) = 201 + 110 = 311
	// 2. (1, 0): res = (200 + 2) + (100 + 10) = 202 + 110 = 312
	// 3. (0, 1): res = (300 + 3) + (100 + 20) = 303 + 120 = 423
	// 4. (0, 0): res = (300 + 4) + (100 + 20) = 304 + 120 = 424
	testCases := []struct {
		c1, c2 uint64
		want   uint64
	}{
		{1, 1, 311},
		{1, 0, 312},
		{0, 1, 423},
		{0, 0, 424},
	}

	for _, tc := range testCases {
		res, err := fn.Call(ctx, tc.c1, tc.c2)
		if err != nil {
			t.Fatalf("call(%d, %d) error: %v", tc.c1, tc.c2, err)
		}
		if res[0] != tc.want {
			t.Errorf("call(%d, %d) = %d, want %d", tc.c1, tc.c2, res[0], tc.want)
		}
	}
}

// TestTempReuse_LoopBackEdge verifies that loops with repeated back-edges (br_if 0)
// safely reuse temporary locals across iterations without corruption.
func TestTempReuse_LoopBackEdge(t *testing.T) {
	ctx := context.Background()

	watSrc := `(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "sumLoop") (param $n i32) (result i32)
			(local $i i32)
			(local $acc i32)
			(call $yield (i32.const 0))
			(local.set $i (i32.const 1))
			(local.set $acc (i32.const 0))
			(loop $header
				;; acc = acc + i
				(local.get $acc)
				(local.get $i)
				(i32.add)
				(local.set $acc)

				;; i = i + 1
				(local.get $i)
				(i32.const 1)
				(i32.add)
				(local.set $i)

				;; if i <= n continue loop
				(local.get $i)
				(local.get $n)
				(i32.le_s)
				(br_if $header)
			)
			(local.get $acc)
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		Matcher: asyncify.NewExactMatcher([]string{"env.yield"}),
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(param uint32) {}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate env: %v", err)
	}

	inst, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate transformed: %v", err)
	}
	defer inst.Close(ctx)

	fn := inst.ExportedFunction("sumLoop")

	// sum 1..100 = 5050
	res, err := fn.Call(ctx, 100)
	if err != nil {
		t.Fatalf("sumLoop(100) error: %v", err)
	}
	if res[0] != 5050 {
		t.Fatalf("sumLoop(100) = %d, want 5050", res[0])
	}
}

// TestTempReuse_MultipleValuesHeldAcrossAsync verifies that multiple operand stack values
// of mixed types held across an actual async suspend and resume are correctly preserved.
func TestTempReuse_MultipleValuesHeldAcrossAsync(t *testing.T) {
	ctx := context.Background()

	// In this test, we push an i32 (42), an i64 (1000000000), and another i32 (8)
	// then call $suspend, which unwinds the stack.
	// When resumed, the function rewinds, pops the values, computes:
	// (42 + 8) + i64_as_i32(1000000000) = 50 + 1000000000
	watSrc := `(module
		(import "env" "suspend" (func $suspend (result i32)))
		(func (export "run") (result i64)
			(i64.const 1000000000)
			(i32.const 42)
			(i32.const 8)
			(call $suspend)
			;; $suspend returned an i32 offset (e.g. 5)
			(i32.add) ;; 8 + 5 = 13
			(i32.add) ;; 42 + 13 = 55
			(i64.extend_i32_u)
			(i64.add) ;; 1000000000 + 55 = 1000000055
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		Matcher:       asyncify.NewExactMatcher([]string{"env.suspend"}),
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	mod, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("wasm.ParseModule failed: %v", err)
	}

	body := mod.Code[0]
	totalLocals := uint32(0)
	for _, le := range body.Locals {
		totalLocals += le.Count
	}
	t.Logf("multiple values held across async: totalLocals = %d", totalLocals)

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	var modInstance api.Module
	suspended := false

	// Host function that suspends on first call, unwinds, and returns 5 on resume.
	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context) uint32 {
			if !suspended {
				suspended = true
				startUnwind := modInstance.ExportedFunction("asyncify_start_unwind")
				if startUnwind == nil {
					panic("missing asyncify_start_unwind export")
				}
				// Set asyncify data buffer pointer: offset 1024
				mem := modInstance.Memory()
				// buf: [stack_ptr at 1024, stack_end at 1028]
				mem.WriteUint32Le(1024, 1032) // stack_ptr starts at 1032
				mem.WriteUint32Le(1028, 4096) // stack_end at 4096
				if _, err := startUnwind.Call(ctx, 1024); err != nil {
					panic(err)
				}
				return 0 // dummy return during unwind
			}
			// When resumed during rewind: stop rewinding so remaining execution is normal!
			stopRewind := modInstance.ExportedFunction("asyncify_stop_rewind")
			if stopRewind == nil {
				panic("missing asyncify_stop_rewind export")
			}
			if _, err := stopRewind.Call(ctx); err != nil {
				panic(err)
			}
			return 5 // resume return value
		}).
		Export("suspend").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate env: %v", err)
	}

	inst, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate transformed: %v", err)
	}
	defer inst.Close(ctx)
	modInstance = inst

	fn := inst.ExportedFunction("run")
	stopUnwind := inst.ExportedFunction("asyncify_stop_unwind")
	startRewind := inst.ExportedFunction("asyncify_start_rewind")
	getState := inst.ExportedFunction("asyncify_get_state")

	// First call: should unwind
	results, err := fn.Call(ctx)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	stateRes, err := getState.Call(ctx)
	if err != nil || stateRes[0] != 1 {
		t.Fatalf("expected StateUnwinding (1), got %v (err: %v)", stateRes, err)
	}
	t.Logf("Unwound successfully, result = %v", results)

	// Stop unwind
	if _, err := stopUnwind.Call(ctx); err != nil {
		t.Fatalf("stopUnwind error: %v", err)
	}

	// Start rewind: set asyncify_state to 2 (rewind) with data pointer 1024
	if _, err := startRewind.Call(ctx, 1024); err != nil {
		t.Fatalf("startRewind error: %v", err)
	}

	// Second call: will rewind to suspend, suspend calls stopRewind and returns 5, then finishes!
	results, err = fn.Call(ctx)
	if err != nil {
		t.Fatalf("second call (rewind) error: %v", err)
	}

	stateRes, err = getState.Call(ctx)
	if err != nil || stateRes[0] != 0 {
		t.Fatalf("expected StateNormal (0) after resume, got %v (err: %v)", stateRes, err)
	}

	// Result should be 1000000055
	if len(results) != 1 || results[0] != 1000000055 {
		t.Fatalf("expected result 1000000055, got %v", results)
	}
	t.Logf("Resumed successfully with preserved stack values! result = %d", results[0])
}
