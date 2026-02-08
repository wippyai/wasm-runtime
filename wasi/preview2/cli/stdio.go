package cli

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type StdioHost struct {
	resources *preview2.ResourceTable
	stdin     *preview2.InputStreamResource
	stdout    *preview2.OutputStreamResource
	stderr    *preview2.OutputStreamResource
}

func NewStdioHost(resources *preview2.ResourceTable,
	stdin *preview2.InputStreamResource,
	stdout *preview2.OutputStreamResource,
	stderr *preview2.OutputStreamResource) *StdioHost {
	return &StdioHost{
		resources: resources,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
	}
}

func (h *StdioHost) Namespace() string {
	return "wasi:cli/stdin@0.2.3"
}

func (h *StdioHost) GetStdin(_ context.Context) uint32 {
	return h.resources.Add(h.stdin)
}
