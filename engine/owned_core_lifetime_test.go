package engine

import (
	"context"
	"sync/atomic"
	"testing"
)

// A component owns its core instances, but its linker hosts and bridges can be
// shared by sibling component instances. Closing an owned core must therefore
// stop only that component's execution lifetime.
func TestOwnedCoreClosureStopsOnlyItsExecutionLifetime(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)

	var closedA atomic.Uint32
	instA, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		OnCoreModuleClosed: func(context.Context, uint32) { closedA.Add(1) },
	})
	if err != nil {
		t.Fatalf("instantiate first component: %v", err)
	}
	defer instA.Close(ctx)

	var closedB atomic.Uint32
	instB, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		OnCoreModuleClosed: func(context.Context, uint32) { closedB.Add(1) },
	})
	if err != nil {
		t.Fatalf("instantiate second component: %v", err)
	}
	defer instB.Close(ctx)

	firstBinding, err := instA.getExportBinding("func1")
	if err != nil {
		t.Fatalf("resolve first core export: %v", err)
	}
	if firstBinding.coreMod == nil {
		t.Fatal("func1 has no owned core module")
	}

	// This is an external core close, as happens when wazero closes an owned
	// guest. The configured notifier must stop the whole instance execution
	// domain, so a call through func2 cannot use a surviving sibling core.
	if err := firstBinding.coreMod.Close(ctx); err != nil {
		t.Fatalf("close func1 core: %v", err)
	}
	if got := closedA.Load(); got != 1 {
		t.Fatalf("first component close notifications = %d, want 1", got)
	}
	if got := closedB.Load(); got != 0 {
		t.Fatalf("second component notified by first core close: %d", got)
	}
	if _, err := instA.CallWithLift(ctx, "func2", "must-not-run"); err == nil {
		t.Fatal("func2 succeeded after its component's other core closed")
	}

	// The second component has distinct owned cores. Its resident execution
	// lifetime is unaffected by closing the first component's core; shared
	// linker hosts or bridges must not have inherited instA's notifier.
	got, err := instB.CallWithLift(ctx, "func2", "sibling-still-live")
	if err != nil {
		t.Fatalf("second component call after sibling core close: %v", err)
	}
	if got != "sibling-still-live" {
		t.Fatalf("second component result = %v, want %q", got, "sibling-still-live")
	}
	if got := closedA.Load(); got != 1 {
		t.Fatalf("first component received a notification from shared state: %d", got)
	}

	// Closing the second component closes its two owned cores. It must never
	// invoke the first component observer; that observer is scoped to instA.
	if err := instB.Close(ctx); err != nil {
		t.Fatalf("close second component: %v", err)
	}
	if got := closedA.Load(); got != 1 {
		t.Fatalf("first component observer received a second component close: %d", got)
	}
	if got := closedB.Load(); got != 2 {
		t.Fatalf("second component close notifications = %d, want 2 owned cores", got)
	}
}

func TestConstructorCancellationDoesNotPoisonResidentCalls(t *testing.T) {
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(context.Background())

	constructorCtx, cancelConstructor := context.WithCancel(context.Background())
	inst, err := mod.Instantiate(constructorCtx)
	if err != nil {
		cancelConstructor()
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(context.Background())
	cancelConstructor()

	got, err := inst.CallWithLift(context.Background(), "func2", "resident-after-constructor-cancel")
	if err != nil {
		t.Fatalf("resident call inherited constructor cancellation: %v", err)
	}
	if got != "resident-after-constructor-cancel" {
		t.Fatalf("resident result = %v, want %q", got, "resident-after-constructor-cancel")
	}
}
