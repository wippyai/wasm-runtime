package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// TestAsyncifyCrossCoreCancellationPoisonsInstance makes the recovery rule
// explicit. A component call can park frames in child cores, so clearing the
// root CallSession after cancellation is not a valid reset. The instance must
// reject a fresh entry until Close tears every core down.
func TestAsyncifyCrossCoreCancellationPoisonsInstance(t *testing.T) {
	ctx, eng, mod, inst, cs := startCrossCoreCall(t, componentFixture(t, multiCoreWrapperWAT))
	defer eng.Close(ctx)
	defer mod.Close(ctx)
	defer inst.Close(ctx)

	if yielded, err := cs.Step(ctx, nil); err != nil || yielded.Status != StepContinue {
		t.Fatalf("initial yield = %+v, %v", yielded, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := cs.Step(canceled, &YieldResult{Value: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled resume = %v, want context.Canceled", err)
	}

	// A failed session must not retain Canonical ABI host arguments in any
	// controller, including a child selected by a bridge transition.
	for core, state := range inst.asyncifyCache {
		if state == nil || state.asyncify == nil {
			continue
		}
		if got := state.asyncify.TakeHostArgs(); got != nil {
			t.Fatalf("core %q retained host args after cancellation: %v", core.Name(), got)
		}
	}
	if _, err := inst.StartCall(ctx, "run", uint32(2)); err == nil || !strings.Contains(err.Error(), "close the instance before reuse") {
		t.Fatalf("fresh call after canceled cross-core session = %v, want close-required failure", err)
	}
}

func TestAsyncifyCrossCoreRejectsMissingResumeResult(t *testing.T) {
	ctx, eng, mod, inst, cs := startCrossCoreCall(t, componentFixture(t, multiCoreWrapperWAT))
	defer eng.Close(ctx)
	defer mod.Close(ctx)
	defer inst.Close(ctx)

	if yielded, err := cs.Step(ctx, nil); err != nil || yielded.Status != StepContinue {
		t.Fatalf("initial yield = %+v, %v", yielded, err)
	}
	if _, err := cs.Step(ctx, nil); err == nil || !strings.Contains(err.Error(), "requires a resume result") {
		t.Fatalf("second Step without a result = %v, want invalid-resume error", err)
	}
	if _, err := inst.StartCall(ctx, "run", uint32(2)); err == nil || !strings.Contains(err.Error(), "close the instance before reuse") {
		t.Fatalf("fresh call after invalid resume = %v, want close-required failure", err)
	}
}

func TestAsyncifyCrossCoreRejectsResultBeforeFirstYield(t *testing.T) {
	ctx, eng, mod, inst, cs := startCrossCoreCall(t, componentFixture(t, multiCoreWrapperWAT))
	defer eng.Close(ctx)
	defer mod.Close(ctx)
	defer inst.Close(ctx)

	if _, err := cs.Step(ctx, &YieldResult{Value: 1}); err == nil || !strings.Contains(err.Error(), "without a yielded operation") {
		t.Fatalf("first Step with result = %v, want invalid-resume error", err)
	}
	if _, err := inst.StartCall(ctx, "run", uint32(2)); err == nil || !strings.Contains(err.Error(), "close the instance before reuse") {
		t.Fatalf("fresh call after invalid first Step = %v, want close-required failure", err)
	}
}

func TestAsyncifyCrossCoreChildTrapAfterResumePoisonsInstance(t *testing.T) {
	wasmBytes := crossCoreChildFixture(t, `(global $child_before (export "child_before") (mut i32) (i32.const 0))
    (global $child_after (export "child_after") (mut i32) (i32.const 0))
    (func (export "wrapped_yield") (param $val i32) (result i32)
      (local $result i32)
      (global.set $child_before (i32.add (global.get $child_before) (i32.const 1)))
      (local.set $result (call $host_yield (local.get $val)))
      ;; A trap after the resumed import must not run either child or caller
      ;; continuation effects, and must leave the instance close-required.
      unreachable
      (global.set $child_after (i32.add (global.get $child_after) (i32.const 1)))
      (local.get $result)
    )`)
	ctx, eng, mod, inst, cs := startCrossCoreCall(t, wasmBytes)
	defer eng.Close(ctx)
	defer mod.Close(ctx)
	defer inst.Close(ctx)

	child := crossCoreChildModule(t, inst, "child_before")
	var caller api.Module
	for _, core := range inst.linkerInst.Modules() {
		if core != nil && core.ExportedGlobal("after_count") != nil {
			caller = core
			break
		}
	}
	if caller == nil {
		t.Fatal("caller core with after_count unavailable")
	}

	if yielded, err := cs.Step(ctx, nil); err != nil || yielded.Status != StepContinue {
		t.Fatalf("initial yield = %+v, %v", yielded, err)
	}
	if got := child.ExportedGlobal("child_before").Get(); got != 1 {
		t.Fatalf("child before yield = %d, want 1", got)
	}
	if got := caller.ExportedGlobal("after_count").Get(); got != 0 {
		t.Fatalf("caller continuation ran during yield: after_count=%d", got)
	}

	if _, err := cs.Step(ctx, &YieldResult{Value: 9}); err == nil {
		t.Fatal("resumed child trap returned nil error")
	}
	if got := child.ExportedGlobal("child_after").Get(); got != 0 {
		t.Fatalf("child after-effect ran after trap: %d", got)
	}
	if got := caller.ExportedGlobal("after_count").Get(); got != 0 {
		t.Fatalf("caller after-effect ran after child trap: %d", got)
	}
	if _, err := inst.StartCall(ctx, "run", uint32(2)); err == nil || !strings.Contains(err.Error(), "close the instance before reuse") {
		t.Fatalf("fresh call after resumed child trap = %v, want close-required failure", err)
	}
}

func crossCoreChildFixture(t *testing.T, childBody string) []byte {
	t.Helper()
	source := strings.Replace(multiCoreWrapperWAT, `(func (export "wrapped_yield") (param $val i32) (result i32)
      (call $host_yield (local.get $val))
    )`, childBody, 1)
	if source == multiCoreWrapperWAT {
		t.Fatal("cross-core fixture rewrite did not match child wrapper")
	}
	return componentFixture(t, source)
}

func crossCoreChildModule(t *testing.T, inst *WazeroInstance, global string) api.Module {
	t.Helper()
	for _, core := range inst.linkerInst.Modules() {
		if core != nil && core.ExportedGlobal(global) != nil {
			return core
		}
	}
	t.Fatalf("child core exporting %q unavailable", global)
	return nil
}

func startCrossCoreCall(t *testing.T, wasmBytes []byte) (context.Context, *WazeroEngine, *WazeroModule, *WazeroInstance, *CallSession) {
	t.Helper()
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		eng.Close(ctx)
		t.Fatal(err)
	}
	if err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.2", "yield",
		[]api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp {
			return &traceOp{name: "cross-core-yield", id: 1}
		}), true,
	); err != nil {
		mod.Close(ctx)
		eng.Close(ctx)
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err != nil {
		mod.Close(ctx)
		eng.Close(ctx)
		t.Fatal(err)
	}
	cs, err := inst.StartCall(ctx, "run", uint32(1))
	if err != nil {
		inst.Close(ctx)
		mod.Close(ctx)
		eng.Close(ctx)
		t.Fatal(err)
	}
	return ctx, eng, mod, inst, cs
}

// TestAsyncifyCrossCoreChildContinuation proves the first yield stops at the
// child lower, rather than allowing the child to run after its host import.
func TestAsyncifyCrossCoreChildContinuation(t *testing.T) {
	wasmBytes := crossCoreChildFixture(t, `(global $child_before (export "child_before") (mut i32) (i32.const 0))
    (global $child_after (export "child_after") (mut i32) (i32.const 0))
    (func (export "wrapped_yield") (param $val i32) (result i32)
      (local $ret i32)
      (global.set $child_before (i32.add (global.get $child_before) (i32.const 1)))
      (local.set $ret (call $host_yield (local.get $val)))
      (global.set $child_after (i32.add (global.get $child_after) (i32.const 1)))
      (local.get $ret)
    )`)
	ctx, eng, mod, inst, cs := startCrossCoreCall(t, wasmBytes)
	defer eng.Close(ctx)
	defer mod.Close(ctx)
	defer inst.Close(ctx)

	step, err := cs.Step(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != StepContinue || step.PendingOp == nil {
		t.Fatalf("initial step = %+v, want pending yield", step)
	}
	child := crossCoreChildModule(t, inst, "child_before")
	if before, after := uint32(child.ExportedGlobal("child_before").Get()), uint32(child.ExportedGlobal("child_after").Get()); before != 1 || after != 0 {
		t.Fatalf("first yield crossed child continuation: before=%d after=%d, want 1,0", before, after)
	}

	step, err = cs.Step(ctx, &YieldResult{Value: 9})
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != StepDone {
		t.Fatalf("resumed step = %+v, want done", step)
	}
	if _, err = cs.LiftResult(ctx, step.Results); err != nil {
		t.Fatal(err)
	}
	if before, after := uint32(child.ExportedGlobal("child_before").Get()), uint32(child.ExportedGlobal("child_after").Get()); before != 1 || after != 1 {
		t.Fatalf("resume replayed child continuation: before=%d after=%d, want 1,1", before, after)
	}
}

// TestAsyncifyCrossCoreChildResuspends covers a child that consumes one result
// during parent rewind and then yields again before returning to its parent.
func TestAsyncifyCrossCoreChildResuspends(t *testing.T) {
	wasmBytes := crossCoreChildFixture(t, `(global $before_first (export "before_first") (mut i32) (i32.const 0))
    (global $between (export "between") (mut i32) (i32.const 0))
    (global $after_second (export "after_second") (mut i32) (i32.const 0))
    (func (export "wrapped_yield") (param $val i32) (result i32)
      (local $first i32)
      (local $second i32)
      (global.set $before_first (i32.add (global.get $before_first) (i32.const 1)))
      (local.set $first (call $host_yield (local.get $val)))
      (global.set $between (i32.add (global.get $between) (i32.const 1)))
      (local.set $second (call $host_yield (local.get $first)))
      (global.set $after_second (i32.add (global.get $after_second) (i32.const 1)))
      (local.get $second)
    )`)
	ctx, eng, mod, inst, cs := startCrossCoreCall(t, wasmBytes)
	defer eng.Close(ctx)
	defer mod.Close(ctx)
	defer inst.Close(ctx)
	child := crossCoreChildModule(t, inst, "before_first")

	first, err := cs.Step(ctx, nil)
	if err != nil || first.Status != StepContinue || first.PendingOp == nil {
		t.Fatalf("first yield = %+v, %v", first, err)
	}
	if a, b, c := uint32(child.ExportedGlobal("before_first").Get()), uint32(child.ExportedGlobal("between").Get()), uint32(child.ExportedGlobal("after_second").Get()); a != 1 || b != 0 || c != 0 {
		t.Fatalf("first yield counters = %d,%d,%d, want 1,0,0", a, b, c)
	}

	second, err := cs.Step(ctx, &YieldResult{Value: 9})
	if err != nil || second.Status != StepContinue || second.PendingOp == nil {
		t.Fatalf("second yield = %+v, %v", second, err)
	}
	if a, b, c := uint32(child.ExportedGlobal("before_first").Get()), uint32(child.ExportedGlobal("between").Get()), uint32(child.ExportedGlobal("after_second").Get()); a != 1 || b != 1 || c != 0 {
		t.Fatalf("second yield counters = %d,%d,%d, want 1,1,0", a, b, c)
	}

	done, err := cs.Step(ctx, &YieldResult{Value: 10})
	if err != nil || done.Status != StepDone {
		t.Fatalf("completion = %+v, %v", done, err)
	}
	if _, err = cs.LiftResult(ctx, done.Results); err != nil {
		t.Fatal(err)
	}
	if a, b, c := uint32(child.ExportedGlobal("before_first").Get()), uint32(child.ExportedGlobal("between").Get()), uint32(child.ExportedGlobal("after_second").Get()); a != 1 || b != 1 || c != 1 {
		t.Fatalf("completion counters = %d,%d,%d, want 1,1,1", a, b, c)
	}
}
