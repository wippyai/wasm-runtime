package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/linker"
)

type stepContextValueKey struct{}

func TestStepExecutionContextEmbedsFreshLeaseAndEngineSnapshot(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	parent := context.WithValue(context.Background(), stepContextValueKey{}, "parent")
	call, err := lifetime.newCall(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()

	inst := newEngineCallInstance()
	inst.linkerInst = &linker.Instance{}
	cs := &CallSession{execution: call, instance: inst}
	ctx, lease, err := cs.enterStepExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.finish()

	step, ok := ctx.(*stepExecutionContext)
	if !ok {
		t.Fatalf("step context = %T, want *stepExecutionContext", ctx)
	}
	if &step.lease != lease {
		t.Fatal("returned lease is not the context's embedded lease")
	}
	if lease.Context == ctx {
		t.Fatal("embedded lease points at the outer step context")
	}
	if marker, ok := ctx.Value(executionLeaseKey{}).(*executionLease); !ok || marker != lease {
		t.Fatalf("marker = %p, want embedded lease %p", marker, lease)
	}
	if !lifetime.heldBy(ctx) {
		t.Fatal("active step context has no execution owner")
	}
	if ctx.Value(stepContextValueKey{}) != "parent" {
		t.Fatal("step lost parent values")
	}
	assertEngineIdentity(ctx, t, inst)
	if got := linker.InstanceFromContext(ctx); got != inst.linkerInst {
		t.Fatalf("linker instance = %p, want %p", got, inst.linkerInst)
	}
}

func TestStepExecutionContextRetainedLeaseDoesNotReactivate(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	parent := context.Background()
	call, err := lifetime.newCall(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	cs := &CallSession{execution: call, instance: newEngineCallInstance()}

	first, firstLease, err := cs.enterStepExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	firstLease.finish()
	second, secondLease, err := cs.enterStepExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer secondLease.finish()
	if lifetime.heldBy(first) {
		t.Fatal("retained first step became active again")
	}
	if !lifetime.heldBy(second) {
		t.Fatal("new step has no active owner")
	}
	if !errors.Is(first.Err(), context.Canceled) || !errors.Is(context.Cause(first), context.Canceled) {
		t.Fatalf("old step was not invalidated: err=%v cause=%v", first.Err(), context.Cause(first))
	}
}

func TestStepExecutionContextRejectsReAdmittingFinishedEmbeddedLease(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	parent := context.Background()
	call, err := lifetime.newCall(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	cs := &CallSession{execution: call, instance: newEngineCallInstance()}

	ctx, lease, err := cs.enterStepExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	lease.finish()
	marker := ctx.Value(executionLeaseKey{}).(*executionLease)
	if marker != lease || lifetime.heldBy(ctx) {
		t.Fatal("finished context has an unexpected active marker")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("re-admitting a finished embedded lease succeeded")
		}
		if marker != ctx.Value(executionLeaseKey{}).(*executionLease) {
			t.Fatal("re-admission changed retained marker identity")
		}
		if lifetime.heldBy(ctx) || marker.active.Load() {
			t.Fatal("rejected re-admission reactivated retained marker")
		}
	}()
	_ = call.admitLease(lease)
}

func TestFailedEnterStepExecutionRetainsPriorCleanupOwnership(t *testing.T) {
	lifetime := newExecutionLifetime()
	parent := context.Background()
	call, err := lifetime.newCall(parent)
	if err != nil {
		t.Fatal(err)
	}
	cs := &CallSession{execution: call, instance: newEngineCallInstance()}

	_, lease, err := cs.enterStepExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	lease.finish()
	prior := cs.stepContextCleanup
	if prior == nil {
		t.Fatal("first step did not install cleanup")
	}
	cleanupCalls := 0
	cs.stepContextCleanup = func() {
		cleanupCalls++
		prior()
	}

	// Stopping makes the next entry fail. It may cancel the preceding base
	// through the lifetime, but it must not consume the preceding cleanup.
	lifetime.stop()
	if _, _, err := cs.enterStepExecution(parent); err == nil {
		t.Fatal("stopped lifetime admitted a new step")
	}
	if cleanupCalls != 0 {
		t.Fatalf("rejected entry consumed prior cleanup %d times", cleanupCalls)
	}
	cs.closeExecution()
	if cleanupCalls != 1 {
		t.Fatalf("terminal cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestStepExecutionContextPreservesNestedLeaseAncestry(t *testing.T) {
	outerLifetime := newExecutionLifetime()
	innerLifetime := newExecutionLifetime()
	defer outerLifetime.stop()
	defer innerLifetime.stop()
	outer, leaveOuter, err := outerLifetime.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer leaveOuter()

	call, err := innerLifetime.newCall(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	cs := &CallSession{execution: call, instance: newEngineCallInstance()}
	inner, leaveInner, err := cs.enterStepExecution(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer leaveInner.finish()
	if !outerLifetime.heldBy(inner) || !innerLifetime.heldBy(inner) {
		t.Fatal("nested step lost active owner ancestry")
	}
	marker := inner.Value(executionLeaseKey{}).(*executionLease)
	if marker.parent == nil || marker.parent.lifetime != outerLifetime {
		t.Fatal("embedded marker has no outer parent")
	}
}

func TestStepExecutionContextPreservesGeneralDeadlineAndCause(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	deadline := time.Now().Add(time.Minute)
	original, cancelDeadline := context.WithDeadline(context.Background(), deadline)
	defer cancelDeadline()
	original, cancel := context.WithCancelCause(original)
	call, err := lifetime.newCall(original)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	stepParent := context.WithValue(context.Background(), stepContextValueKey{}, "step")
	cs := &CallSession{execution: call, instance: newEngineCallInstance()}
	step, lease, err := cs.enterStepExecution(stepParent)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.finish()
	if got, ok := step.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("deadline = %v, %v, want %v", got, ok, deadline)
	}
	if step.Value(stepContextValueKey{}) != "step" {
		t.Fatal("general step values were replaced")
	}
	cause := errors.New("session stopped")
	cancel(cause)
	select {
	case <-step.Done():
	case <-time.After(time.Second):
		t.Fatal("session cancellation was not forwarded")
	}
	if !errors.Is(context.Cause(step), cause) {
		t.Fatalf("cause = %v, want %v", context.Cause(step), cause)
	}
}

func TestUntrackedStepExecutionContextDelegatesOuterMarker(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	outer, leave, err := lifetime.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer leave()

	inst := newEngineCallInstance()
	cs := &CallSession{instance: inst}
	ctx, lease, err := cs.enterStepExecution(outer)
	if err != nil || lease != nil {
		t.Fatalf("untracked step: ctx=%v lease=%v err=%v", ctx, lease, err)
	}
	if marker := ctx.Value(executionLeaseKey{}); marker != outer.Value(executionLeaseKey{}) {
		t.Fatal("untracked step shadowed outer execution marker")
	}
	assertEngineIdentity(ctx, t, inst)
}
