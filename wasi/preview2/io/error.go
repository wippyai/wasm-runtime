package io

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type ErrorHost struct {
	resources *preview2.ResourceTable
}

func NewErrorHost(resources *preview2.ResourceTable) *ErrorHost {
	return &ErrorHost{resources: resources}
}

func (h *ErrorHost) Namespace() string {
	return "wasi:io/error@0.2.8"
}

func (h *ErrorHost) MethodErrorToDebugString(_ context.Context, self uint32) string {
	r, ok := h.resources.Get(self)
	if !ok {
		return "unknown error"
	}
	if err, ok := r.(*preview2.ErrorResource); ok {
		return err.ToDebugString()
	}
	return "unknown error"
}

func (h *ErrorHost) ResourceDropError(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *ErrorHost) Register() map[string]any {
	return map[string]any{
		"[method]error.to-debug-string": h.MethodErrorToDebugString,
		"[resource-drop]error":          h.ResourceDropError,
	}
}
