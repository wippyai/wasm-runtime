package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type executionFastPathKey struct{}

// nonComparableExecutionContext is legal context.Context implementation whose
// dynamic value cannot be compared. The fast path must decline it, not panic.
type nonComparableExecutionContext struct {
	context.Context
	values []string
}

func TestSessionExecutionFastPathKeepsInitialContextAndInvalidatesStaleStep(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	deadline := time.Now().Add(time.Minute)
	parent, cancelDeadline := context.WithDeadline(context.Background(), deadline)
	defer cancelDeadline()
	parent = context.WithValue(parent, executionFastPathKey{}, "initial")
	parent, cancel := context.WithCancelCause(parent)
	call, err := lifetime.newCall(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	cs := &CallSession{execution: call}

	first, leaveFirst, err := cs.enterSessionExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	if first.Value(executionFastPathKey{}) != "initial" {
		t.Fatal("initial values were not retained")
	}
	if got, ok := first.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("deadline = %v, %v", got, ok)
	}
	leaveFirst.finish()

	second, leaveSecond, err := cs.enterSessionExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer leaveSecond.finish()
	if !errors.Is(first.Err(), context.Canceled) || !errors.Is(context.Cause(first), context.Canceled) {
		t.Fatalf("stale step was not canceled: err=%v cause=%v", first.Err(), context.Cause(first))
	}
	cause := errors.New("initial call canceled")
	cancel(cause)
	select {
	case <-second.Done():
	case <-time.After(time.Second):
		t.Fatal("initial cancellation was not forwarded")
	}
	if !errors.Is(context.Cause(second), cause) {
		t.Fatalf("cause = %v, want %v", context.Cause(second), cause)
	}
}

func TestSessionExecutionNonComparableParentUsesGeneralPath(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	parent := nonComparableExecutionContext{Context: context.Background(), values: []string{"step"}}
	call, err := lifetime.newCall(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	cs := &CallSession{execution: call}
	step, leave, err := cs.enterSessionExecution(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer leave.finish()
	if step.Value(executionFastPathKey{}) != nil {
		t.Fatal("unexpected value changed fallback context")
	}
}

func TestExecutionLeaseConcurrentFinishAndStop(t *testing.T) {
	for range 100 {
		lifetime := newExecutionLifetime()
		ctx, finish, err := lifetime.enter(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(finish)
		}
		lifetime.stop()
		wg.Wait()
		if lifetime.heldBy(ctx) {
			t.Fatal("concurrent finish retained active marker")
		}
		if err := lifetime.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecutionLeaseRetainedContextStaysInactiveDuringNewEntry(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	call, err := lifetime.newCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	first, err := call.enterLease(call.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.finish()
	second, err := call.enterLease(call.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.finish()
	if lifetime.heldBy(first) {
		t.Fatal("retained old lease became active again")
	}
	if !lifetime.heldBy(second) {
		t.Fatal("new lease not active")
	}
}

func TestUntrackedSessionLeaseRelease(t *testing.T) {
	parent := context.WithValue(context.Background(), executionFastPathKey{}, "untracked")
	session := &CallSession{}
	ctx, lease, err := session.enterSessionExecution(parent)
	if err != nil || ctx != parent || lease != nil {
		t.Fatalf("untracked admission: context=%v lease=%v err=%v", ctx, lease, err)
	}
	lease.finish()
	lease.finish()
}
