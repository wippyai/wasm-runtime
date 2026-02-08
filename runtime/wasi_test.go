package runtime

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestWASIRegistration(t *testing.T) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	wasi := preview2.New()

	// Verify all 24 hosts register without errors
	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI hosts: %v", err)
	}

	// Verify host count
	hostCount := 0
	for _, funcs := range rt.hosts.funcs {
		hostCount += len(funcs)
	}

	t.Logf("Registered %d WASI functions across 24 hosts", hostCount)

	// We expect at least 100+ functions registered (119 total)
	if hostCount < 100 {
		t.Errorf("expected at least 100 WASI functions, got %d", hostCount)
	}
}
