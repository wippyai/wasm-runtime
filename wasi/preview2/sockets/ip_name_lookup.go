package sockets

import (
	"context"
	"net"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// IPNameLookupHost implements wasi:sockets/ip-name-lookup@0.2.0
type IPNameLookupHost struct {
	resources *preview2.ResourceTable
}

// NewIPNameLookupHost creates a new IP name lookup host
func NewIPNameLookupHost(resources *preview2.ResourceTable) *IPNameLookupHost {
	return &IPNameLookupHost{resources: resources}
}

// Namespace returns the WASI namespace
func (h *IPNameLookupHost) Namespace() string {
	return "wasi:sockets/ip-name-lookup@0.2.0"
}

// resolve-addresses
func (h *IPNameLookupHost) ResolveAddresses(ctx context.Context, _ uint32, name string) (uint32, *NetworkError) {
	resolver := net.Resolver{}
	addrs, err := resolver.LookupHost(ctx, name)
	if err != nil {
		return 0, &NetworkError{Code: NetworkErrorNameUnresolvable}
	}

	stream := preview2.NewResolveAddressStreamResource(addrs)
	handle := h.resources.Add(stream)
	return handle, nil
}

// [method]resolve-address-stream.resolve-next-address
func (h *IPNameLookupHost) MethodResolveAddressStreamResolveNextAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	stream, ok := r.(*preview2.ResolveAddressStreamResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	addr := stream.ReadNext()
	if addr == nil {
		return nil, nil
	}

	return &IPSocketAddress{
		Address: *addr,
		Port:    0,
	}, nil
}

// [method]resolve-address-stream.subscribe
func (h *IPNameLookupHost) MethodResolveAddressStreamSubscribe(_ context.Context, _ uint32) uint32 {
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}
