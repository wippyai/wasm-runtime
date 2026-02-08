package sockets

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type InstanceNetworkHost struct {
	resources *preview2.ResourceTable
}

func NewInstanceNetworkHost(resources *preview2.ResourceTable) *InstanceNetworkHost {
	return &InstanceNetworkHost{resources: resources}
}

func (h *InstanceNetworkHost) Namespace() string {
	return "wasi:sockets/instance-network@0.2.0"
}

func (h *InstanceNetworkHost) InstanceNetwork(_ context.Context) uint32 {
	network := preview2.NewNetworkResource()
	return h.resources.Add(network)
}
