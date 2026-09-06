package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

func reviewAsyncifyControls(t *testing.T, direct bool) (*Asyncify, api.Module) {
	t.Helper()
	ctx := context.Background()
	raw, err := wat.Compile(`(module (memory (export "memory") 1) (func (export "run")))`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := asyncify.Transform(raw, asyncify.Config{ExportGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	t.Cleanup(func() { _ = rt.Close(ctx) })
	mod, err := rt.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	a := NewAsyncify()
	a.trusted = direct
	if err := a.Init(mod); err != nil {
		t.Fatal(err)
	}
	if a.DirectGlobals() != direct {
		t.Fatal("unexpected control path")
	}
	return a, mod
}

func TestAsyncifyControlTrapPreservesCachedState(t *testing.T) {
	for _, direct := range []bool{false, true} {
		for _, name := range []string{"start-unwind", "stop-unwind", "start-rewind", "stop-rewind"} {
			t.Run(name+map[bool]string{false: "/functions", true: "/globals"}[direct], func(t *testing.T) {
				ctx := context.Background()
				a, mod := reviewAsyncifyControls(t, direct)
				var call func(context.Context) error
				switch name {
				case "start-unwind":
					call = a.StartUnwind
				case "stop-unwind":
					if err := a.StartUnwind(ctx); err != nil {
						t.Fatal(err)
					}
					call = a.StopUnwind
				case "start-rewind":
					call = a.StartRewind
				case "stop-rewind":
					if err := a.StartRewind(ctx); err != nil {
						t.Fatal(err)
					}
					call = a.StopRewind
				}
				before := a.GetState(ctx)
				// Force the generated helper's stack-pointer ordering trap.
				if !mod.Memory().WriteUint32Le(AsyncifyDataAddr, 4096) || !mod.Memory().WriteUint32Le(AsyncifyDataAddr+4, 2048) {
					t.Fatal("could not corrupt stack header")
				}
				if err := call(ctx); err == nil {
					t.Fatal("corrupt stack did not trap")
				}
				if got := a.GetState(ctx); got != before {
					t.Fatalf("failed control changed cached state from %d to %d", before, got)
				}
				wantGuest := uint64(0)
				if name == "start-unwind" {
					wantGuest = 1
				}
				if name == "start-rewind" {
					wantGuest = 2
				}
				if got := mod.ExportedGlobal("asyncify_state").Get(); got != wantGuest {
					t.Fatalf("trap guest-global effect = %d, want %d", got, wantGuest)
				}
			})
		}
	}
}

func TestAsyncifyDirectControlsKeepLoopCancellation(t *testing.T) {
	const childKey = "WIPPY_ASYNCIFY_CONTROL_CANCEL_CHILD"
	if os.Getenv(childKey) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAsyncifyDirectControlsKeepLoopCancellation$", "-test.count=1")
		cmd.Env = append(os.Environ(), childKey+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cancellation subprocess: %v (%v)\n%s", err, ctx.Err(), out)
		}
		return
	}
	ctx := context.Background()
	raw, err := wat.Compile(`(module
		(import "env" "recv" (func $recv (result i32)))
		(memory (export "memory") 1)
		(func (export "run") (drop (call $recv)) (loop $forever (br $forever))))`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.recv"}, ExportGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)
	_, err = rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithGoModuleFunction(
		MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp { return &traceOp{id: 1} }),
		nil, []api.ValueType{api.ValueTypeI32}).Export("recv").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	a := NewAsyncify()
	a.trusted = true
	if err := a.Init(mod); err != nil {
		t.Fatal(err)
	}
	if !a.DirectGlobals() {
		t.Fatal("direct controls are not active")
	}
	scheduler := NewScheduler(a)
	ctx = WithScheduler(WithAsyncify(ctx, a), scheduler)
	if err := scheduler.Execute(ctx, mod.ExportedFunction("run")); err != nil {
		t.Fatal(err)
	}
	step, err := scheduler.Step(ctx, nil)
	if err != nil || step.PendingOp == nil {
		t.Fatalf("initial yield: %v %v", step, err)
	}
	loopCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, err = scheduler.Step(loopCtx, &YieldResult{Value: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("loop cancellation: %v", err)
	}
	if !mod.IsClosed() {
		t.Fatal("non-yielding guest was not closed")
	}
}

func TestAsyncifyDirectControlRejectsClosedModule(t *testing.T) {
	a, mod := reviewAsyncifyControls(t, true)
	ctx := context.Background()
	if err := mod.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.StartUnwind(ctx); err == nil {
		t.Fatal("direct control succeeded on a closed module")
	}
}
