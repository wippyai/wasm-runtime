package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/memory/budget"
	"github.com/wippyai/wasm-runtime/wat"
)

type engineCloseDrop struct{ count atomic.Int32 }

func (d *engineCloseDrop) Drop() { d.count.Add(1) }

func TestEngineCloseRetiresLiveInstances(t *testing.T) {
	ctx := context.Background()
	engine, module := loadTwoCoreModule(t)
	defer engine.Close(ctx)
	var owners []*WazeroInstance
	drops := &engineCloseDrop{}
	for range 2 {
		instance, err := module.Instantiate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		instance.resources.Insert(1, drops)
		owners = append(owners, instance)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := engine.Close(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if drops.count.Load() != 2 {
		t.Fatalf("resource drops=%d", drops.count.Load())
	}
	for _, instance := range owners {
		if !instance.closed {
			t.Fatal("engine did not close instance owner")
		}
		if _, err := instance.CallWithLift(ctx, "func1", "late"); err == nil {
			t.Fatal("closed instance executed")
		}
	}
	if len(engine.instances) != 0 {
		t.Fatal("closed engine retained owners")
	}
}

func TestEngineCloseRetainsActiveCallAndRetries(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	_, err = engine.runtime.NewHostModuleBuilder("park").NewFunctionBuilder().WithFunc(func(context.Context) { close(entered); <-release }).Export("wait").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := wat.Compile(`(module (import "park" "wait" (func $wait)) (memory 1) (func (export "run") call $wait))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	ledger := budget.New(65536)
	instance, err := module.InstantiateWithConfig(ctx, &InstanceConfig{MemoryBudget: ledger})
	if err != nil {
		t.Fatal(err)
	}
	drops := &engineCloseDrop{}
	instance.resources.Insert(1, drops)
	done := make(chan struct{})
	go func() { defer close(done); _, _ = instance.GetExportedFunction("run").Call(ctx) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("host not entered")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := engine.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("close=%v", err)
	}
	if drops.count.Load() != 0 || engine.closeComplete || ledger.Usage().Used != 65536 {
		t.Fatal("active owner reclaimed after failed join")
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("call did not return")
	}
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if drops.count.Load() != 1 || !engine.closeComplete || ledger.Usage().Used != 0 {
		t.Fatal("retry did not reclaim owner")
	}
}

func TestEngineCloseFromInitializerDefersJoin(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	var closeErr error
	_, err = engine.runtime.NewHostModuleBuilder("shutdown").NewFunctionBuilder().WithFunc(func(ctx context.Context) { closeErr = engine.Close(ctx) }).Export("now").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := wat.Compile(`(module (import "shutdown" "now" (func $now)) (func $init call $now) (start $init))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		instance, err := module.Instantiate(ctx)
		if instance != nil {
			_ = instance.Close(ctx)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown startup published instance")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initializer self-joined engine")
	}
	if !errors.Is(closeErr, ErrCloseFromExecution) {
		t.Fatalf("callback close=%v", closeErr)
	}
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCloseJoinsUnpublishedStartup(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	_, err = engine.runtime.NewHostModuleBuilder("startup-park").NewFunctionBuilder().WithFunc(func(context.Context) { close(entered); <-release }).Export("wait").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := wat.Compile(`(module (import "startup-park" "wait" (func $wait)) (func $init call $wait) (start $init))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		instance, err := module.Instantiate(ctx)
		if instance != nil {
			_ = instance.Close(ctx)
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("initializer not entered")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := engine.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("close=%v", err)
	}
	if engine.closeComplete {
		t.Fatal("engine reclaimed backend during unpublished startup")
	}
	engine.modulesMu.Lock()
	pendingModules := len(engine.modules)
	engine.modulesMu.Unlock()
	if pendingModules != 1 {
		t.Fatalf("compiled module owner lost while startup active: %d", pendingModules)
	}
	unblock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("startup published after engine shutdown")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not join")
	}
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if len(engine.instances) != 0 || !engine.closeComplete {
		t.Fatal("shutdown retry retained startup ownership")
	}
}
