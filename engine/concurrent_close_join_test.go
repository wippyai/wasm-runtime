package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type gatedCloseResource struct {
	entered, release chan struct{}
	once             sync.Once
}

func (r *gatedCloseResource) Drop() { r.once.Do(func() { close(r.entered); <-r.release }) }

func TestConcurrentInstanceCloseCancellationRetainsOwner(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gate := &gatedCloseResource{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	defer unblock()
	inst.resources.Insert(1, gate)
	done := make(chan error, 1)
	go func() { done <- inst.Close(ctx) }()
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("teardown not entered")
	}
	// Lookup must not wait behind destruction or expose a new raw core handle.
	lookup := make(chan bool, 1)
	go func() { lookup <- inst.GetExportedFunction("func1") == nil }()
	select {
	case absent := <-lookup:
		if !absent {
			t.Fatal("lookup exposed a core during teardown")
		}
	case <-time.After(time.Second):
		unblock()
		<-done
		<-lookup
		t.Fatal("lookup blocked behind resource teardown")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	second := make(chan error, 1)
	go func() { second <- inst.Close(canceled) }()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second Close: %v", err)
		}
	case <-time.After(time.Second):
		unblock()
		<-done
		<-second
		t.Fatal("canceled Close blocked behind resource teardown")
	}
	if inst.instance == nil && inst.linkerInst == nil {
		t.Fatal("unfinished close reclaimed core owners")
	}
	// Engine Close must not close the shared backend under unfinished instance
	// teardown just because that instance's execution leases are already drained.
	if err := eng.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("engine Close: %v", err)
	}
	if eng.closeComplete || eng.modules == nil {
		t.Fatal("engine reclaimed modules after incomplete instance close")
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !eng.closeComplete {
		t.Fatal("retry failed to complete engine teardown")
	}
}
