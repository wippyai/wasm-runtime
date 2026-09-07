package engine

import (
	"context"
	"testing"
)

// A raw export must not overwrite guest state while a canonical result still
// borrows it. The same instance-level gate protects suspended Asyncify frames.
func TestInstanceFunctionRejectsUnliftedResult(t *testing.T) {
	_, instance, cleanup := setupPostReturnInstance(t)
	defer cleanup()
	ctx := context.Background()
	fn := instance.GetExportedFunction("get_post_return_called")
	session, err := instance.StartCall(ctx, "echo_str")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.Call(ctx); err == nil {
		t.Error("raw Call bypassed prepared-session ownership")
	}
	preparedStack := []uint64{123}
	if err := fn.CallWithStack(ctx, preparedStack); err == nil {
		t.Error("raw stack call bypassed prepared-session ownership")
	}
	if preparedStack[0] != 123 {
		t.Error("prepared session rejection changed stack")
	}
	result, err := session.Step(ctx, nil)
	if err != nil || result.Status != StepDone {
		t.Fatalf("step: %v %v", result, err)
	}
	if _, err := fn.Call(ctx); err == nil {
		t.Error("raw Call bypassed unlifted-result gate")
	}
	stack := []uint64{123}
	if err := fn.CallWithStack(ctx, stack); err == nil {
		t.Error("raw CallWithStack bypassed unlifted-result gate")
	}
	if stack[0] != 123 {
		t.Error("rejected raw call changed caller stack")
	}
	value, err := session.LiftResult(ctx, result.Results)
	if err != nil || value != "hello" {
		t.Fatalf("lift: %v %v", value, err)
	}
	observed, err := fn.Call(ctx)
	if err != nil || len(observed) != 1 || observed[0] != 1 {
		t.Fatalf("raw call after lift: %v %v", observed, err)
	}
}
