package engine

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func TestAsyncifyIndirectSuspensionDoesNotRepeatCallerEffects(t *testing.T) {
	ctx := context.Background()
	code := transformModule(t, `(module
  (type $pause_t (func (result i32)))
  (import "env" "pause" (func $pause (type $pause_t)))
  (table 1 funcref)
  (elem (i32.const 0) $pause)
  (memory (export "memory") 1)
  (global $effects (mut i32) (i32.const 0))
  (func $helper (result i32)
   i32.const 0 call_indirect (type $pause_t))
  (func (export "run") (result i32)
   global.get $effects i32.const 1 i32.add global.set $effects
   call $helper drop
   global.get $effects))`, []string{"env.pause"})
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
	if len(last.Results) != 1 || last.Results[0] != 1 {
		t.Fatalf("caller side effect repeated across indirect suspension: got %v, want [1]", last.Results)
	}
}
