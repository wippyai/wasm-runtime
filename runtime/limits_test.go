package runtime

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A subprocess bounds this regression even if guest interruption breaks.
func TestConfiguredRuntimeCancelsGuestLoop(t *testing.T) {
	if os.Getenv("WASM_CANCEL_LOOP_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConfiguredRuntimeCancelsGuestLoop$")
		cmd.Env = append(os.Environ(), "WASM_CANCEL_LOOP_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("guest cancellation did not finish: %v\n%s", err, out)
		}
		return
	}
	ctx := context.Background()
	rt, err := NewWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	mod, err := rt.LoadWAT(ctx, `(module (func (export "run") (loop $forever br $forever)))`, `run: func();`)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := inst.Call(callCtx, "run"); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("loop result: %v", err)
	}
	if _, err := inst.Call(ctx, "run"); err == nil {
		t.Fatal("terminated instance was reusable")
	}
}

func TestConfiguredRuntimeBoundsMemoryGrowth(t *testing.T) {
	ctx := context.Background()
	rt, err := NewWithConfig(ctx, &Config{MemoryLimitPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	mod, err := rt.LoadWAT(ctx, `(module
 (memory (export "memory") 1)
 (func (export "grow") (param i32) (result i32) local.get 0 memory.grow))`, `grow: func(pages: s32) -> s32;`)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	if _, err := inst.Call(ctx, "grow", int32(1)); err != nil {
		t.Fatal(err)
	}
	if inst.MemorySize() != 2*65536 {
		t.Fatalf("memory = %d", inst.MemorySize())
	}
	if _, err := inst.Call(ctx, "grow", int32(1)); err != nil {
		t.Fatal(err)
	}
	if inst.MemorySize() != 2*65536 {
		t.Fatalf("memory exceeded cap: %d", inst.MemorySize())
	}
}
