package engine

import "testing"

func TestExportBindings_PreparedSessionOwnsInstance(t *testing.T) {
	ctx := t.Context()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true, AsyncifyStackBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	first, err := inst.StartCall(ctx, "func1", "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.LiftResult(ctx, []uint64{0}); err == nil {
		t.Fatal("prepared session lifted results before execution")
	}
	if _, err := inst.StartCall(ctx, "func2", "must-not-start"); err == nil {
		t.Fatal("second StartCall overwrote prepared session ownership")
	}
	if _, err := inst.CallWithLift(ctx, "func1", "must-not-execute"); err == nil {
		t.Fatal("synchronous call bypassed prepared session")
	}
	result, err := first.Step(ctx, nil)
	if err != nil || result.Status != StepDone {
		t.Fatalf("first Step: %v %v", result, err)
	}
	if _, err := inst.StartCall(ctx, "func2", "must-wait-for-lift"); err == nil {
		t.Fatal("new call admitted before results were lifted and post-return completed")
	}
	got, err := first.LiftResult(ctx, result.Results)
	if err != nil || got != "first" {
		t.Fatalf("first result: %v %v", got, err)
	}
	second, err := inst.StartCall(ctx, "func1", "second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Step(ctx, nil); err == nil {
		t.Fatal("completed session stepped a newer call")
	}
	result, err = second.Step(ctx, nil)
	if err != nil || result.Status != StepDone {
		t.Fatalf("second Step: %v %v", result, err)
	}
	got, err = second.LiftResult(ctx, result.Results)
	if err != nil || got != "second" {
		t.Fatalf("second result: %v %v", got, err)
	}
}
