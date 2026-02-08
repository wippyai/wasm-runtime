package clocks

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestMonotonicClockHost_Now(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewMonotonicClockHost(resources)
	ctx := context.Background()

	// Monotonic clock returns time since host creation
	now1 := host.Now(ctx)
	time.Sleep(1 * time.Millisecond)
	now2 := host.Now(ctx)

	if now2 <= now1 {
		t.Errorf("monotonic clock not monotonic: %d <= %d", now2, now1)
	}
	if now2-now1 < 1_000_000 {
		t.Errorf("expected at least 1ms elapsed, got %dns", now2-now1)
	}
}

func TestMonotonicClockHost_Resolution(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewMonotonicClockHost(resources)
	ctx := context.Background()

	res := host.Resolution(ctx)
	if res != 1 {
		t.Errorf("expected resolution 1 (nanosecond), got %d", res)
	}
}

func TestMonotonicClockHost_SubscribeInstant(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewMonotonicClockHost(resources)
	ctx := context.Background()

	// Subscribe for instant 10ms from now (in monotonic time)
	now := host.Now(ctx)
	when := now + 10_000_000 // 10ms in the future
	handle := host.SubscribeInstant(ctx, when)

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("expected pollable to be in resource table")
	}
	p, ok := r.(preview2.Pollable)
	if !ok {
		t.Fatal("expected resource to implement Pollable")
	}
	if p.Ready() {
		t.Error("expected pollable to NOT be ready yet (10ms in future)")
	}
}

func TestMonotonicClockHost_SubscribeDuration(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewMonotonicClockHost(resources)
	ctx := context.Background()

	duration := uint64(10_000_000) // 10ms
	handle := host.SubscribeDuration(ctx, duration)

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("expected pollable to be in resource table")
	}
	p, ok := r.(preview2.Pollable)
	if !ok {
		t.Fatal("expected resource to implement Pollable")
	}
	if p.Ready() {
		t.Error("expected pollable to NOT be ready yet (10ms duration)")
	}

	// Block and verify it becomes ready
	p.Block(ctx)
	if !p.Ready() {
		t.Error("expected pollable to be ready after Block()")
	}
}

func TestWallClockHost_Now(t *testing.T) {
	host := NewWallClockHost()
	ctx := context.Background()

	before := time.Now()
	dt := host.Now(ctx)
	after := time.Now()

	if dt.Seconds < uint64(before.Unix()) || dt.Seconds > uint64(after.Unix()) {
		t.Errorf("wall clock seconds (%d) outside expected range [%d, %d]",
			dt.Seconds, before.Unix(), after.Unix())
	}

	if dt.Nanoseconds >= 1000000000 {
		t.Errorf("wall clock nanoseconds (%d) should be < 1000000000", dt.Nanoseconds)
	}
}

func TestWallClockHost_Resolution(t *testing.T) {
	host := NewWallClockHost()
	ctx := context.Background()

	res := host.Resolution(ctx)
	if res.Seconds != 1 || res.Nanoseconds != 0 {
		t.Errorf("expected resolution (1s, 0ns), got (%ds, %dns)", res.Seconds, res.Nanoseconds)
	}
}

func TestMonotonicClock_Monotonicity(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewMonotonicClockHost(resources)
	ctx := context.Background()

	t1 := host.Now(ctx)
	time.Sleep(1 * time.Millisecond)
	t2 := host.Now(ctx)

	if t2 <= t1 {
		t.Errorf("monotonic clock not monotonic: t1=%d, t2=%d", t1, t2)
	}
}

func TestWallClock_Accuracy(t *testing.T) {
	host := NewWallClockHost()
	ctx := context.Background()

	goTime := time.Now()
	wasiTime := host.Now(ctx)

	diff := int64(wasiTime.Seconds) - goTime.Unix()
	if diff < -1 || diff > 1 {
		t.Errorf("wall clock differs from Go time by %d seconds", diff)
	}
}
