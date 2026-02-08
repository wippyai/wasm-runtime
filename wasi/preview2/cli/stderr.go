package cli

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type StderrHost struct {
	resources *preview2.ResourceTable
	stderr    *preview2.OutputStreamResource
}

func NewStderrHost(resources *preview2.ResourceTable, stderr *preview2.OutputStreamResource) *StderrHost {
	return &StderrHost{
		resources: resources,
		stderr:    stderr,
	}
}

func (h *StderrHost) Namespace() string {
	return "wasi:cli/stderr@0.2.3"
}

func (h *StderrHost) GetStderr(_ context.Context) uint32 {
	return h.resources.Add(h.stderr)
}
