package linker

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/tetratelabs/wazero/experimental"
)

func TestCoreContextDoesNotAttachInstanceNotifierToSharedHost(t *testing.T) {
	ctx := context.Background()
	starts := 0
	pre := admissionFixture(t, []string{admittedCore, admittedCore}, &starts)
	var shared, first, second atomic.Int32
	sharedCtx := experimental.WithCloseNotifier(ctx, experimental.CloseNotifyFunc(func(context.Context, uint32) { shared.Add(1) }))
	firstCtx := experimental.WithCloseNotifier(ctx, experimental.CloseNotifyFunc(func(context.Context, uint32) { first.Add(1) }))
	secondCtx := experimental.WithCloseNotifier(ctx, experimental.CloseNotifyFunc(func(context.Context, uint32) { second.Add(1) }))
	a, err := pre.NewInstanceWithCoreContext(sharedCtx, firstCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(ctx)
	b, err := pre.NewInstanceWithCoreContext(sharedCtx, secondCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close(ctx)
	if starts != 4 {
		t.Fatalf("initializers=%d", starts)
	}
	if err := a.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if first.Load() != 2 || second.Load() != 0 || shared.Load() != 0 {
		t.Fatalf("first close: first=%d second=%d shared=%d", first.Load(), second.Load(), shared.Load())
	}
	got, err := b.modules[0].ExportedFunction("grow").Call(ctx)
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("surviving instance: %v %v", got, err)
	}
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if first.Load() != 2 || second.Load() != 2 || shared.Load() != 0 {
		t.Fatalf("second close: first=%d second=%d shared=%d", first.Load(), second.Load(), shared.Load())
	}
	if err := pre.linker.runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if first.Load() != 2 || second.Load() != 2 || shared.Load() != 1 {
		t.Fatalf("runtime close: first=%d second=%d shared=%d", first.Load(), second.Load(), shared.Load())
	}
}
