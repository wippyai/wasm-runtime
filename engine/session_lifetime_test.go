package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionLifetimeResidentStepAndLift(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true, AsyncifyStackBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	inst.lifetime = newExecutionLifetime()
	defer inst.lifetime.stop()
	for range 10 {
		parent, cancel := context.WithCancel(ctx)
		cs, err := inst.StartCall(parent, "func1", "retained")
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if cs.execution.ctx.Err() != nil {
			t.Fatal("lowering canceled session")
		}
		step, err := cs.Step(parent, nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if step.Status != StepDone || cs.execution.ctx.Err() != nil {
			t.Fatal("StepDone did not preserve lifting context")
		}
		got, err := cs.LiftResult(parent, step.Results)
		cancel()
		if err != nil || got != "retained" {
			t.Fatalf("lift=%v err=%v", got, err)
		}
		if cs.execution.ctx.Err() == nil {
			t.Fatal("terminal lift retained call context")
		}
		if got, err = cs.LiftResult(ctx, step.Results); err != nil || got != "retained" {
			t.Fatal("cached lift no longer available")
		}
	}
	cs, err := inst.StartCall(ctx, "func1", "late")
	if err != nil {
		t.Fatal(err)
	}
	inst.lifetime.stop()
	if err = inst.lifetime.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = cs.Step(ctx, nil); err == nil {
		t.Fatal("stopped session executed")
	}
}

func TestSessionExecutionMergesCancellationDeadlineAndValues(t *testing.T) {
	type key struct{}
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	deadline := time.Now().Add(time.Minute)
	original, deadlineCancel := context.WithDeadline(context.Background(), deadline)
	defer deadlineCancel()
	original, cancel := context.WithCancelCause(original)
	call, err := lifetime.newCall(original)
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	cs := &CallSession{execution: call}
	step := context.WithValue(context.Background(), key{}, "step value")
	merged, leave, err := cs.enterSessionExecution(step)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Value(key{}) != "step value" {
		t.Fatal("step values replaced")
	}
	if d, ok := merged.Deadline(); !ok || !d.Equal(deadline) {
		t.Fatal("original deadline lost")
	}
	cause := errors.New("session canceled")
	cancel(cause)
	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("original cancellation not forwarded")
	}
	if !errors.Is(context.Cause(merged), cause) {
		t.Fatal("original cancellation cause lost")
	}
	leave.finish()
	if _, _, err = cs.enterSessionExecution(step); err == nil {
		t.Fatal("canceled original session resumed")
	}
}

func TestSessionStepContextSurvivesSuspendedHostOperation(t *testing.T) {
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	call, err := lifetime.newCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	cs := &CallSession{execution: call}
	captured, leave, err := cs.enterSessionExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The dispatcher can retain the host's supplied context while the guest is
	// suspended awaiting that operation. Releasing the active lease must not
	// cancel that outstanding I/O operation.
	leave.finish()
	if captured.Err() != nil {
		t.Fatal("leaving guest step canceled pending host operation")
	}
	next, leaveNext, err := cs.enterSessionExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if captured.Err() == nil {
		t.Fatal("consumed operation retained its step context")
	}
	leaveNext.finish()
	if next.Err() != nil {
		t.Fatal("next pending operation was canceled")
	}
	cs.closeExecution()
	if next.Err() == nil {
		t.Fatal("terminal cleanup retained pending operation context")
	}
}

func TestGuardedLiftCancellationPreservesCauseAndSkipsPostReturn(t *testing.T) {
	ctx := context.Background()
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()
	session, err := inst.StartCall(ctx, "echo_str")
	if err != nil {
		t.Fatal(err)
	}
	step, err := session.Step(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, first := session.LiftResult(canceled, step.Results)
	if !errors.Is(first, context.Canceled) {
		t.Fatalf("cancellation cause lost: %v", first)
	}
	if session.postReturnCalled {
		t.Fatal("canceled entry marked guest post-return executed")
	}
	_, again := session.LiftResult(ctx, step.Results)
	if !errors.Is(again, first) {
		t.Fatal("terminal error was not retained")
	}
}
