package engine

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// Preserving a synchronous loop must preserve stock Wazero's cancellation
// checks even though no Asyncify state checks remain inside that loop.
func TestAsyncifyClosedLoopCancellation(t *testing.T) {
	const childKey = "WIPPY_CLOSED_LOOP_CANCEL_CHILD"
	if os.Getenv(childKey) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAsyncifyClosedLoopCancellation$", "-test.count=1")
		cmd.Env = append(os.Environ(), childKey+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cancellation subprocess: %v (%v)\n%s", err, ctx.Err(), out)
		}
		return
	}
	raw, err := wat.Compile(`(module
 (import "env" "yield" (func $yield))
 (memory (export "memory") 1)
 (func (export "run") (loop br 0) call $yield))`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.yield"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)
	_, err = rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func() { panic("unreachable yield") }).Export("yield").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := mod.ExportedFunction("run").Call(callCtx); err == nil {
		t.Fatal("infinite synchronous loop did not cancel")
	}
	if callCtx.Err() == nil || !mod.IsClosed() {
		t.Fatal("cancellation must close the executing instance")
	}
}
