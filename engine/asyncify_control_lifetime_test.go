package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/memory/budget"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestRetainedAsyncifyControlsRejectStoppedOwner(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "functions", true: "globals"}[direct], func(t *testing.T) {
			a, mod := reviewAsyncifyControls(t, direct)
			a.lifetime = newExecutionLifetime()
			a.lifetime.stop()
			for _, control := range []func(context.Context) error{a.StartUnwind, a.StopUnwind, a.StartRewind, a.StopRewind} {
				if err := control(context.Background()); !errors.Is(err, errExecutionLifetimeStopped) {
					t.Fatalf("stopped control: %v", err)
				}
			}
			const sentinel = 0x123456
			mod.Memory().WriteUint32Le(a.dataAddr, sentinel)
			a.ResetStack()
			if got, _ := mod.Memory().ReadUint32Le(a.dataAddr); got != sentinel {
				t.Fatalf("reset changed stopped memory: %x", got)
			}
			if err := a.ResetStackContext(context.Background()); !errors.Is(err, errExecutionLifetimeStopped) {
				t.Fatal(err)
			}
			if got := mod.ExportedGlobal("asyncify_state").Get(); got != 0 {
				t.Fatalf("stopped controls changed guest state: %d", got)
			}
			if err := a.Init(mod); err == nil {
				t.Fatal("owned controller rebound")
			}
		})
	}
}

// A pre-instrumented module's controls are arbitrary guest code. In particular,
// a fallback control or debug state read can enter a blocking host callback.
func TestInstanceCloseJoinsPublicAsyncifyExecution(t *testing.T) {
	for _, entry := range []string{"start-unwind", "sync-state", "scheduler-step"} {
		t.Run(entry, func(t *testing.T) {
			ctx := context.Background()
			eng, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
			if err != nil {
				t.Fatal(err)
			}
			defer eng.Close(ctx)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			_, err = eng.runtime.NewHostModuleBuilder("park").NewFunctionBuilder().WithFunc(func(context.Context) { close(entered); <-release }).Export("wait").Instantiate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := wat.Compile(`(module
    (import "park" "wait" (func $wait))
    (memory (export "memory") 1)
    (func (export "asyncify_get_state") (result i32) call $wait i32.const 0)
    (func (export "asyncify_start_unwind") (param i32) call $wait)
    (func (export "run") call $wait))`)
			if err != nil {
				t.Fatal(err)
			}
			m, err := eng.LoadModule(ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			ledger := budget.New(65536)
			inst, err := m.InstantiateWithConfig(ctx, &InstanceConfig{MemoryBudget: ledger})
			if err != nil {
				t.Fatal(err)
			}
			if err = inst.EnableAsyncify(AsyncifyConfig{DataAddr: 32768, StackSize: 1024}); err != nil {
				t.Fatal(err)
			}
			a, s := inst.Asyncify(), inst.Scheduler()
			if a.lifetime != inst.lifetime {
				t.Fatal("controls have no instance owner")
			}
			if entry == "scheduler-step" {
				// Deliberately use the raw core function: the public scheduler must own
				// execution itself instead of depending on a wrapped function argument.
				if err = s.Execute(ctx, inst.instance.ExportedFunction("run")); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				switch entry {
				case "start-unwind":
					_ = a.StartUnwind(ctx)
				case "sync-state":
					a.SyncState(ctx)
				case "scheduler-step":
					_, _ = s.Step(ctx, nil)
				}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("host not entered")
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err = inst.Close(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("Close: %v", err)
			}
			if ledger.Usage().Used != 65536 {
				t.Fatal("control memory released before callback return")
			}
			unblock()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("control did not return")
			}
			if err = inst.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if ledger.Usage().Used != 0 {
				t.Fatal("memory not released after join")
			}
			if err = a.StartUnwind(ctx); !errors.Is(err, errExecutionLifetimeStopped) {
				t.Fatalf("retained control: %v", err)
			}
			if err = s.Execute(ctx, nil); !errors.Is(err, errExecutionLifetimeStopped) {
				t.Fatalf("retained scheduler: %v", err)
			}
		})
	}
}

func TestSchedulerExecuteDoesNotPublishFailedStackReset(t *testing.T) {
	a, _ := reviewAsyncifyControls(t, false)
	a.SetDataAddr(^uint32(0) - 3)
	s := NewScheduler(a)
	if err := s.Execute(context.Background(), nil); err == nil {
		t.Fatal("out-of-bounds reset succeeded")
	}
	if s.initialized {
		t.Fatal("failed reset published execution")
	}
}
