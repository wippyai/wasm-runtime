package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/wasm-runtime/memory/budget"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestEngineMemoryBudgetRawAdmissionGrowthAndRelease(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	starts := 0
	_, err = engine.runtime.NewHostModuleBuilder("budget-start").NewFunctionBuilder().WithFunc(func() { starts++ }).Export("mark").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := wat.Compile(`(module (import "budget-start" "mark" (func $mark)) (memory 1 3) (func $init call $mark) (start $init) (func (export "grow") (result i32) i32.const 1 memory.grow))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	ledger := budget.New(2 * 65536)
	cfg := &InstanceConfig{MemoryBudget: ledger}
	instance, err := module.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if starts != 1 || ledger.Usage().Used != 65536 {
		t.Fatalf("starts=%d usage=%+v", starts, ledger.Usage())
	}
	grow := instance.GetExportedFunction("grow")
	for _, expected := range []uint64{1, 0xffffffff} {
		result, err := grow.Call(ctx)
		if err != nil || len(result) != 1 || result[0] != expected {
			t.Fatalf("grow=%v err=%v expected=%d", result, err, expected)
		}
	}
	if ledger.Usage().Used != 2*65536 {
		t.Fatal("growth charge wrong")
	}
	rejected, err := module.InstantiateWithConfig(ctx, cfg)
	if rejected != nil || !errors.Is(err, budget.ErrLimit) || starts != 1 {
		t.Fatalf("second admission=%v err=%v starts=%d", rejected, err, starts)
	}
	if err := instance.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if ledger.Usage().Used != 0 {
		t.Fatal("instance close retained reservation")
	}
	fresh, err := module.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == nil || starts != 2 {
		t.Fatal("fresh admitted instance failed")
	}
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if ledger.Usage().Used != 0 {
		t.Fatal("engine close retained reservation")
	}
}

func TestEngineMemoryBudgetComponentOwnership(t *testing.T) {
	ctx := context.Background()
	engine, module := loadTwoCoreModule(t)
	defer engine.Close(ctx)
	// Fixture has two independently owned memories: 2 pages and 3 pages.
	ledger := budget.New(11 * 65536)
	host, err := ledger.Reserve(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Release()
	parent, cancel := context.WithCancel(ctx)
	first, err := module.InstantiateWithConfig(parent, &InstanceConfig{MemoryBudget: ledger})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	second, err := module.InstantiateWithConfig(ctx, &InstanceConfig{MemoryBudget: ledger})
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Usage().Used != 11*65536 {
		t.Fatalf("initial aggregate=%+v", ledger.Usage())
	}
	for _, instance := range []*WazeroInstance{first, second} {
		value, err := instance.CallWithLift(ctx, "func1", "resident")
		if err != nil || value != "resident" {
			t.Fatalf("call=%v err=%v", value, err)
		}
	}
	binding, err := first.getExportBinding("func1")
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.coreMod.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := first.CallWithLift(ctx, "func2", "late"); err == nil {
		t.Fatal("sibling core continued after owner stop")
	}
	if ledger.Usage().Used != 11*65536 {
		t.Fatal("partial core close released reservation too early")
	}
	if value, err := second.CallWithLift(ctx, "func2", "independent"); err != nil || value != "independent" {
		t.Fatalf("sibling instance=%v %v", value, err)
	}
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if ledger.Usage().Used != 65536 {
		t.Fatalf("engine cleanup lost host owner or retained guest: %+v", ledger.Usage())
	}
}

func TestEngineMemoryBudgetFailedInitializerRelease(t *testing.T) {
	for _, reactor := range []bool{false, true} {
		ctx := context.Background()
		engine, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
		if err != nil {
			t.Fatal(err)
		}
		start := "(start $init)"
		if reactor {
			start = `(export "_initialize" (func $init))`
		}
		binary, err := wat.Compile(`(module (memory 1) (func $init unreachable) ` + start + `)`)
		if err != nil {
			t.Fatal(err)
		}
		module, err := engine.LoadModule(ctx, binary)
		if err != nil {
			t.Fatal(err)
		}
		ledger := budget.New(65536)
		instance, err := module.InstantiateWithConfig(ctx, &InstanceConfig{MemoryBudget: ledger})
		if err == nil || instance != nil {
			t.Fatal("trapped startup published")
		}
		if ledger.Usage().Used != 0 {
			t.Fatalf("failed start retained charge: %+v", ledger.Usage())
		}
		if err := engine.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
