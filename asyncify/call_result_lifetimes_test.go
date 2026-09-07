package asyncify_test

import (
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestDeadCallResultsUseBoundedStorage(t *testing.T) {
	const calls = 32
	tc := diffTestCase{Name: "dead_call_results", WAT: `(module
  (import "env" "yield_val" (func $yield (result i32))) (memory (export "memory") 1)
  (func (export "run") (result i32) ` + strings.Repeat("call $yield drop ", calls) + ` i32.const 123))`, AsyncImports: []string{"env.yield_val"}, YieldVal: 77, MaxSuspends: calls, ExpectedSuspends: calls, ExpectedReturns: []uint64{123}}
	raw, err := wat.Compile(tc.WAT)
	if err != nil {
		t.Fatal(err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: tc.AsyncImports})
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatal(err)
	}
	locals := uint32(0)
	for _, entry := range module.Code[0].Locals {
		locals += entry.Count
	}
	// Three protocol scratch locals plus one reusable i32 result/operand slot.
	if locals != 4 {
		t.Errorf("32 dead call results need %d locals, want 4", locals)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			result := executeDiffRuntime(t, backend, transformed, tc, true, true)
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			if len(result.Returns) != 1 || result.Returns[0] != 123 || result.SuspendCount != calls || result.FinalState != 0 {
				t.Fatalf("bad resumed result: %+v", result)
			}
			if result.PeakFrameBytes != 4 {
				t.Errorf("dead results inflate frame to %d bytes, want call-index-only 4", result.PeakFrameBytes)
			}
		})
	}
	runDifferentialScenario(t, tc)
}

func TestCallResultRetainedAcrossLaterSuspensions(t *testing.T) {
	for _, tc := range []diffTestCase{
		{Name: "retained_result_and_later_result", WAT: `(module
   (import "env" "yield_val" (func $yield (result i32))) (memory (export "memory") 1)
   (func (export "run") (result i32) call $yield call $yield i32.add call $yield i32.add))`, YieldVal: 7, ExpectedReturns: []uint64{21}, ExpectedSuspends: 3},
		{Name: "loop_result_and_carried_sum", WAT: `(module
   (import "env" "yield_val" (func $yield (result i32))) (memory (export "memory") 1)
   (func (export "run") (result i32) (local $counter i32)
    i32.const 3 local.set $counter i32.const 0 loop (param i32) (result i32)
     call $yield i32.add local.get $counter i32.const 1 i32.sub local.tee $counter br_if 0 end))`, YieldVal: 7, ExpectedReturns: []uint64{21}, ExpectedSuspends: 3},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			tc.AsyncImports = []string{"env.yield_val"}
			tc.MaxSuspends = tc.ExpectedSuspends
			runDifferentialScenario(t, tc)
		})
	}
}
