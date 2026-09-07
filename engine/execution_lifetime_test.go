package engine

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/experimental"
	"go.bytecodealliance.org/wit"
)

func TestExecutionLifetimeSeparatesCompletedCallCancellation(t *testing.T) {
	l := newExecutionLifetime()
	defer l.stop()
	first, cancel := context.WithCancel(context.Background())
	call, finish, err := l.enter(first)
	if err != nil {
		t.Fatal(err)
	}
	if call.Err() != nil {
		t.Fatal(call.Err())
	}
	finish()
	cancel()
	second, finish2, err := l.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Err() != nil {
		t.Fatal("previous call poisoned lifetime")
	}
	finish2()
}
func TestExecutionLifetimeStopsAndJoinsBeforeRelease(t *testing.T) {
	l := newExecutionLifetime()
	call, finish, err := l.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l.stop()
	select {
	case <-call.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel call")
	}
	if _, _, err = l.enter(context.Background()); !errors.Is(err, errExecutionLifetimeStopped) {
		t.Fatal("entry accepted after stop")
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err = l.wait(expired); !errors.Is(err, context.Canceled) {
		t.Fatal("active execution was not joined")
	}
	finish()
	finish()
	if err = l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestExecutionLifetimeConcurrentEnterStop(t *testing.T) {
	for range 100 {
		l := newExecutionLifetime()
		start, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			<-start
			call, finish, err := l.enter(context.Background())
			if err == nil {
				<-call.Done()
				finish()
			}
		}()
		close(start)
		l.stop()
		<-done
		if err := l.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecutionLifetimeSuspendedSessionDoesNotHoldExecution(t *testing.T) {
	l := newExecutionLifetime()
	call, err := l.newCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer call.close()
	first, err := call.enterLease(call.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.finish()
	if call.ctx.Err() != nil {
		t.Fatal("suspension canceled resumable call")
	}
	second, err := call.enterLease(call.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	second.finish()
	l.stop()
	// No active step remains even though the call is retained awaiting a reply.
	if err = l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = call.enterLease(call.ctx, nil); err == nil {
		t.Fatal("late reply re-entered stopped instance")
	}
}

func TestExecutionLifetimeWrapsResidentComponentCalls(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	data, err := hex.DecodeString(twoCoreFixtureWasmHex)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := eng.LoadModule(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	lifetime := newExecutionLifetime()
	defer lifetime.stop()
	initCtx := experimental.WithCloseNotifier(ctx, experimental.CloseNotifyFunc(func(context.Context, uint32) { lifetime.stop() }))
	inst, err := mod.InstantiateWithConfig(initCtx, &InstanceConfig{EntryExport: "func1"})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	inst.lifetime = lifetime
	for range 20 {
		parent, cancel := context.WithCancel(ctx)
		got, err := inst.CallWithLift(parent, "func1", "resident")
		cancel()
		if err != nil || got != "resident" {
			t.Fatalf("resident result=%v err=%v", got, err)
		}
	}
	lifetime.stop()
	if err = lifetime.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err = inst.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err = lifetime.enter(ctx); err == nil {
		t.Fatal("closed instance accepted new call")
	}
}

func TestSynchronousEntryPointsHonorInstanceLifetime(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	inst.lifetime = newExecutionLifetime()
	defer inst.lifetime.stop()
	str := []wit.Type{wit.String{}}
	got, err := inst.CallWithTypes(ctx, "func1", str, str, "typed")
	if err != nil || got != "typed" {
		t.Fatalf("typed call: %v, %v", got, err)
	}
	var into string
	if err = inst.CallInto(ctx, "func2", str, str, &into, "into"); err != nil || into != "into" {
		t.Fatalf("into: %s, %v", into, err)
	}
	inst.lifetime.stop()
	checks := []func() error{
		func() error { _, e := inst.CallWithLift(ctx, "func1", "late"); return e },
		func() error { _, e := inst.CallWithTypes(ctx, "func1", str, str, "late"); return e },
		func() error { return inst.CallInto(ctx, "func2", str, str, &into, "late") },
		func() error { _, e := inst.RunAsync(ctx, "func1", 0, 0); return e },
	}
	for i, call := range checks {
		if e := call(); !errors.Is(e, errExecutionLifetimeStopped) {
			t.Errorf("entry %d: %v", i, e)
		}
	}
	if into != "into" {
		t.Fatal("stopped call changed output")
	}
}
