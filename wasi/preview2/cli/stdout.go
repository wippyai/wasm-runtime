package cli

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type StdoutHost struct {
	resources *preview2.ResourceTable
	stdout    *preview2.OutputStreamResource
}

func NewStdoutHost(resources *preview2.ResourceTable, stdout *preview2.OutputStreamResource) *StdoutHost {
	return &StdoutHost{
		resources: resources,
		stdout:    stdout,
	}
}

func (h *StdoutHost) Namespace() string {
	return "wasi:cli/stdout@0.2.3"
}

func (h *StdoutHost) GetStdout(_ context.Context) uint32 {
	return h.resources.Add(h.stdout)
}
