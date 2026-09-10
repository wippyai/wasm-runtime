package bridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

// Keep the outer test bounded even if a regression removes loop cancellation.
func TestForwardingWrapper_CancelsRunningGuest(t *testing.T) {
	const childEnv = "WIPPY_TEST_FORWARD_LOOP_CHILD"
	if os.Getenv(childEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestForwardingWrapper_CancelsRunningGuest$", "-test.count=1")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("forwarded guest cancellation: %v\n%s", err, out)
		}
		return
	}

	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)
	code, err := wat.Compile(`(module (func (export "run") (loop br 0)))`)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	caller := &testMockCaller{}
	expectForwardingTrap(t, func() { ForwardingWrapper(mod.ExportedFunction("run"), 0)(callCtx, caller, nil) })
	if !errors.Is(callCtx.Err(), context.DeadlineExceeded) || !mod.IsClosed() {
		t.Fatalf("forwarded loop escaped cancellation: err=%v closed=%v", callCtx.Err(), mod.IsClosed())
	}
	if !caller.closed || caller.exitCode != 1 {
		t.Fatal("forwarding did not propagate the callee failure to its caller")
	}
}

func expectForwardingTrap(t *testing.T, call func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("forwarded failure did not trap")
		}
	}()
	call()
}
