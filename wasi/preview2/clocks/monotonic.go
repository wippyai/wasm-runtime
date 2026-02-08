package clocks

import (
	"context"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type MonotonicClockHost struct {
	resources *preview2.ResourceTable
	startTime time.Time
}

func NewMonotonicClockHost(resources *preview2.ResourceTable) *MonotonicClockHost {
	return &MonotonicClockHost{
		resources: resources,
		startTime: time.Now(),
	}
}

func (h *MonotonicClockHost) Namespace() string {
	return "wasi:clocks/monotonic-clock@0.2.8"
}

func (h *MonotonicClockHost) Now(_ context.Context) uint64 {
	return uint64(time.Since(h.startTime).Nanoseconds())
}

func (h *MonotonicClockHost) Resolution(_ context.Context) uint64 {
	return 1
}

func (h *MonotonicClockHost) SubscribeInstant(_ context.Context, when uint64) uint32 {
	deadline := h.startTime.Add(time.Duration(when))
	p := preview2.NewTimerPollable(deadline)
	return h.resources.Add(p)
}

func (h *MonotonicClockHost) SubscribeDuration(_ context.Context, duration uint64) uint32 {
	deadline := time.Now().Add(time.Duration(duration))
	p := preview2.NewTimerPollable(deadline)
	return h.resources.Add(p)
}
