package engine

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func TestAsyncifyBranchSkippedAssignmentPreservesLocal(t *testing.T) {
	ctx := context.Background()
	code := transformModule(t, `(module
  (import "env" "pause" (func $pause (result i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32) (local $handle i32)
   i32.const 2 local.set $handle
   call $pause drop
   block
    i32.const 1 br_if 0
    i32.const 0 local.set $handle
   end
   local.get $handle))`, []string{"env.pause"})
	runtime := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = runtime.Close(ctx) })
	_, err := runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithGoModuleFunction(MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp { return &traceOp{name: "pause", id: 1} }), nil, []api.ValueType{api.ValueTypeI32}).Export("pause").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	module, err := runtime.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	async := NewAsyncify()
	if err := async.Init(module); err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(async)
	ctx = WithScheduler(WithAsyncify(ctx, async), scheduler)
	if err := scheduler.Execute(ctx, module.ExportedFunction("run")); err != nil {
		t.Fatal(err)
	}
	first, err := scheduler.Step(ctx, nil)
	if err != nil || first.Status != StepContinue {
		t.Fatalf("initial suspend: %v %v", first, err)
	}
	last, err := scheduler.Step(ctx, &YieldResult{Value: 1})
	if err != nil || last.Status != StepDone {
		t.Fatalf("resume: %v %v", last, err)
	}
	if len(last.Results) != 1 || last.Results[0] != 2 {
		t.Fatalf("local lost across skipped assignment: got %v, want [2]", last.Results)
	}
}
