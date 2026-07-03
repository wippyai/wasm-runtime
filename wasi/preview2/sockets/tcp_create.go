package sockets

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type TCPCreateSocketHost struct {
	resources *preview2.ResourceTable
}

func NewTCPCreateSocketHost(resources *preview2.ResourceTable) *TCPCreateSocketHost {
	return &TCPCreateSocketHost{resources: resources}
}

func (h *TCPCreateSocketHost) Namespace() string {
	return "wasi:sockets/tcp-create-socket@0.2.0"
}

type NetworkError struct {
	Code NetworkErrorCode
}

type NetworkErrorCode uint8

const (
	NetworkErrorUnknown NetworkErrorCode = iota
	NetworkErrorAccessDenied
	NetworkErrorNotSupported
	NetworkErrorInvalidArgument
	NetworkErrorOutOfMemory
	NetworkErrorTimeout
	NetworkErrorConcurrencyConflict
	NetworkErrorNotInProgress
	NetworkErrorWouldBlock
	NetworkErrorInvalidState
	NetworkErrorNewSocketLimit
	NetworkErrorAddressNotBindable
	NetworkErrorAddressInUse
	NetworkErrorRemoteUnreachable
	NetworkErrorConnectionRefused
	NetworkErrorConnectionReset
	NetworkErrorConnectionAborted
	NetworkErrorDatagramTooLarge
	NetworkErrorNameUnresolvable
	NetworkErrorTemporaryResolverFailure
	NetworkErrorPermanentResolverFailure
)

func (e *NetworkError) Error() string {
	codes := map[NetworkErrorCode]string{
		NetworkErrorUnknown:                  "unknown",
		NetworkErrorAccessDenied:             "access-denied",
		NetworkErrorNotSupported:             "not-supported",
		NetworkErrorInvalidArgument:          "invalid-argument",
		NetworkErrorOutOfMemory:              "out-of-memory",
		NetworkErrorTimeout:                  "timeout",
		NetworkErrorConcurrencyConflict:      "concurrency-conflict",
		NetworkErrorNotInProgress:            "not-in-progress",
		NetworkErrorWouldBlock:               "would-block",
		NetworkErrorInvalidState:             "invalid-state",
		NetworkErrorNewSocketLimit:           "new-socket-limit",
		NetworkErrorAddressNotBindable:       "address-not-bindable",
		NetworkErrorAddressInUse:             "address-in-use",
		NetworkErrorRemoteUnreachable:        "remote-unreachable",
		NetworkErrorConnectionRefused:        "connection-refused",
		NetworkErrorConnectionReset:          "connection-reset",
		NetworkErrorConnectionAborted:        "connection-aborted",
		NetworkErrorDatagramTooLarge:         "datagram-too-large",
		NetworkErrorNameUnresolvable:         "name-unresolvable",
		NetworkErrorTemporaryResolverFailure: "temporary-resolver-failure",
		NetworkErrorPermanentResolverFailure: "permanent-resolver-failure",
	}

	name, ok := codes[e.Code]
	if !ok {
		name = "unknown"
	}

	return "network error: " + name
}

const (
	AddressFamilyIPv4 uint8 = 0
	AddressFamilyIPv6 uint8 = 1
)

func (h *TCPCreateSocketHost) CreateTCPSocket(_ context.Context, addressFamily uint8) (uint32, *NetworkError) {
	if addressFamily != AddressFamilyIPv4 && addressFamily != AddressFamilyIPv6 {
		return 0, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	socket := preview2.NewTCPSocketResource(addressFamily)
	handle := h.resources.Add(socket)
	return handle, nil
}
