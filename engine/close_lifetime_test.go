package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestCloseLifetimeConcurrentExactlyOnce(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)
	binary, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatal(err)
	}
	var closes atomic.Int32
	module, err := runtime.Instantiate(experimental.WithCloseNotifier(ctx, experimental.CloseNotifyFunc(func(context.Context, uint32) { closes.Add(1) })), binary)
	if err != nil {
		t.Fatal(err)
	}
	instance := &WazeroInstance{lifetime: newExecutionLifetime(), instance: module}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := instance.Close(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := closes.Load(); got != 1 {
		t.Fatalf("module closed %d times", got)
	}
}

func TestCloseLifetimeHostCallbackDefersJoin(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer runtime.Close(ctx)
	var instance *WazeroInstance
	var callbackErr error
	_, err := runtime.NewHostModuleBuilder("host").NewFunctionBuilder().WithFunc(func(ctx context.Context) {
		callbackErr = instance.Close(ctx)
	}).Export("close").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := wat.Compile(`(module (import "host" "close" (func $close)) (func (export "run") call $close))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := runtime.Instantiate(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}
	instance = &WazeroInstance{lifetime: newExecutionLifetime(), instance: module}
	execution, finish, err := instance.enterExecution(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); defer finish(); _, _ = module.ExportedFunction("run").Call(execution) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("callback self-joined")
	}
	if !errors.Is(callbackErr, ErrCloseFromExecution) {
		t.Fatalf("callback close: %v", callbackErr)
	}
	if instance.closed {
		t.Fatal("callback reclaimed active execution")
	}
	if err := instance.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !instance.closed {
		t.Fatal("external close did not finish")
	}
}

func TestCloseLifetimeCanceledJoinRetainsResources(t *testing.T) {
	instance := &WazeroInstance{lifetime: newExecutionLifetime(), stackBuf: []uint64{42}}
	_, finish, err := instance.enterExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := instance.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("close: %v", err)
	}
	if instance.closed || len(instance.stackBuf) != 1 {
		t.Fatal("failed join reclaimed resources")
	}
	finish()
	if err := instance.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !instance.closed || instance.stackBuf != nil {
		t.Fatal("retry failed to release")
	}
}

func TestExecutionLeaseMarkerNestedAndExpired(t *testing.T) {
	outer, inner := newExecutionLifetime(), newExecutionLifetime()
	defer outer.stop()
	defer inner.stop()
	ctx, leaveOuter, err := outer.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	nested, leaveInner, err := inner.enter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !outer.heldBy(nested) || !inner.heldBy(nested) {
		t.Fatal("lost nested owner")
	}
	leaveInner()
	if inner.heldBy(nested) || !outer.heldBy(nested) {
		t.Fatal("incorrect nested release")
	}
	leaveOuter()
	if outer.heldBy(nested) {
		t.Fatal("stale marker remains active")
	}
}

func TestSessionLeaseMarkerTracksExecutionNotSuspension(t *testing.T) {
	instance := &WazeroInstance{lifetime: newExecutionLifetime()}
	defer instance.lifetime.stop()
	call, lowering, leave, err := instance.beginSessionExecution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	if !instance.lifetime.heldBy(lowering) {
		t.Fatal("lowering has no owner")
	}
	leave()
	if instance.lifetime.heldBy(lowering) || instance.lifetime.heldBy(call.ctx) {
		t.Fatal("suspended session holds active marker")
	}
	session := &CallSession{execution: call}
	defer session.closeExecution()
	for range 3 {
		step, finish, err := session.enterSessionExecution(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !instance.lifetime.heldBy(step) {
			t.Fatal("step/lift has no owner")
		}
		finish.finish()
		if instance.lifetime.heldBy(step) {
			t.Fatal("completed step remains active")
		}
		if step.Err() != nil {
			t.Fatal("lease release canceled pending host operation")
		}
	}
}
