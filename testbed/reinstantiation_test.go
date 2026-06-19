package testbed

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// reinitSeedHost provides wasi:random/insecure-seed@0.2.6 with the correct
// single-tuple return shape, so the re-instantiation test isolates the bridge
// lifecycle rather than the preview2 host's seed shape/version.
type reinitSeedHost struct{}

func (reinitSeedHost) Namespace() string { return "wasi:random/insecure-seed@0.2.6" }

func (reinitSeedHost) InsecureSeed(_ context.Context) [2]uint64 {
	now := time.Now().UnixNano()
	return [2]uint64{uint64(now), uint64(now >> 32)}
}

// TestWASI_Reinstantiation guards against re-instantiation corrupting a
// WASI-importing component. A virtual import bridge re-exports a core
// instance's functions via ForwardingWrapper, which captures concrete,
// per-instance functions. Before the fix the bridge was never freed, so a
// second instance reused one still bound to the first (now-closed) instance's
// core and trapped with exit_code(1) on the first call. reinit.wasm touches a
// std HashMap (importing wasi:random/insecure-seed) and probe() returns 42;
// each iteration instantiates, calls probe, and closes.
func TestWASI_Reinstantiation(t *testing.T) {
	data, err := os.ReadFile("reinit.wasm")
	if err != nil {
		t.Skip("reinit.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	if err := rt.RegisterWASI(preview2.New()); err != nil {
		t.Fatalf("register WASI: %v", err)
	}
	if err := rt.RegisterHost(reinitSeedHost{}); err != nil {
		t.Fatalf("register seed host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("compile: %v", err)
	}

	for i := 1; i <= 3; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("instantiate #%d: %v", i, err)
		}
		result, err := inst.Call(ctx, "probe")
		if err != nil {
			_ = inst.Close(ctx)
			t.Fatalf("call probe #%d: %v", i, err)
		}
		if got, ok := result.(uint32); !ok || got != 42 {
			_ = inst.Close(ctx)
			t.Fatalf("probe #%d returned %v (%T), want uint32(42)", i, result, result)
		}
		if err := inst.Close(ctx); err != nil {
			t.Fatalf("close #%d: %v", i, err)
		}
	}
}
