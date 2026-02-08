package sockets

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// UDPCreateSocketHost implements wasi:sockets/udp-create-socket@0.2.0
type UDPCreateSocketHost struct {
	resources *preview2.ResourceTable
}

// NewUDPCreateSocketHost creates a new UDP create socket host
func NewUDPCreateSocketHost(resources *preview2.ResourceTable) *UDPCreateSocketHost {
	return &UDPCreateSocketHost{resources: resources}
}

// Namespace returns the WASI namespace
func (h *UDPCreateSocketHost) Namespace() string {
	return "wasi:sockets/udp-create-socket@0.2.0"
}

// create-udp-socket
func (h *UDPCreateSocketHost) CreateUDPSocket(_ context.Context, addressFamily uint8) (uint32, *NetworkError) {
	if addressFamily != AddressFamilyIPv4 && addressFamily != AddressFamilyIPv6 {
		return 0, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	socket := preview2.NewUDPSocketResource(addressFamily)
	handle := h.resources.Add(socket)
	return handle, nil
}
