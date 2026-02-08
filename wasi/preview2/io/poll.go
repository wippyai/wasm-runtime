package io

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type PollHost struct {
	resources *preview2.ResourceTable
}

func NewPollHost(resources *preview2.ResourceTable) *PollHost {
	return &PollHost{resources: resources}
}

func (h *PollHost) Namespace() string {
	return "wasi:io/poll@0.2.8"
}

func (h *PollHost) Poll(_ context.Context, pollables []uint32) []uint32 {
	ready := make([]uint32, 0, len(pollables))

	for i, handle := range pollables {
		r, ok := h.resources.Get(handle)
		if !ok {
			continue
		}
		if p, ok := r.(preview2.Pollable); ok {
			if p.Ready() {
				ready = append(ready, uint32(i))
			}
		}
	}

	return ready
}

func (h *PollHost) MethodPollableReady(_ context.Context, self uint32) bool {
	r, ok := h.resources.Get(self)
	if !ok {
		return false
	}
	if p, ok := r.(preview2.Pollable); ok {
		return p.Ready()
	}
	return false
}

func (h *PollHost) MethodPollableBlock(ctx context.Context, self uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return
	}
	if p, ok := r.(preview2.Pollable); ok {
		p.Block(ctx)
	}
}

func (h *PollHost) ResourceDropPollable(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *PollHost) Register() map[string]any {
	return map[string]any{
		"poll":                    h.Poll,
		"[method]pollable.ready":  h.MethodPollableReady,
		"[method]pollable.block":  h.MethodPollableBlock,
		"[resource-drop]pollable": h.ResourceDropPollable,
	}
}
