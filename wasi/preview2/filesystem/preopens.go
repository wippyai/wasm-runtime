package filesystem

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type PreopensHost struct {
	resources *preview2.ResourceTable
	preopens  map[string]string
}

func NewPreopensHost(resources *preview2.ResourceTable, preopens map[string]string) *PreopensHost {
	if preopens == nil {
		preopens = make(map[string]string)
	}
	return &PreopensHost{
		resources: resources,
		preopens:  preopens,
	}
}

func (h *PreopensHost) Namespace() string {
	return "wasi:filesystem/preopens@0.2.3"
}

func (h *PreopensHost) GetDirectories(_ context.Context) [][2]interface{} {
	result := make([][2]interface{}, 0, len(h.preopens))

	for logicalPath, physicalPath := range h.preopens {
		desc := preview2.NewDescriptorResource(physicalPath, true, false)
		handle := h.resources.Add(desc)
		result = append(result, [2]interface{}{handle, logicalPath})
	}

	return result
}
