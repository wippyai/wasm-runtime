package sockets

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// UDPHost implements wasi:sockets/udp@0.2.0
type UDPHost struct {
	resources *preview2.ResourceTable
	mu        sync.Mutex
}

// NewUDPHost creates a new UDP host
func NewUDPHost(resources *preview2.ResourceTable) *UDPHost {
	return &UDPHost{resources: resources}
}

// Namespace returns the WASI namespace
func (h *UDPHost) Namespace() string {
	return "wasi:sockets/udp@0.2.0"
}

// IncomingDatagram represents an incoming UDP datagram
type IncomingDatagram struct {
	Data          []byte
	RemoteAddress IPSocketAddress
}

type OutgoingDatagram struct {
	RemoteAddress *IPSocketAddress
	Data          []byte
}

// getSocket retrieves and validates a UDP socket resource
func (h *UDPHost) getSocket(handle uint32) (*preview2.UDPSocketResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	socket, ok := r.(*preview2.UDPSocketResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return socket, nil
}

// [method]udp-socket.start-bind
func (h *UDPHost) MethodUDPSocketStartBind(_ context.Context, self uint32, _ uint32, localAddress IPSocketAddress) *NetworkError {
	h.mu.Lock()

	socket, err := h.getSocket(self)
	if err != nil {
		h.mu.Unlock()
		return err
	}

	if socket.State() != preview2.UDPStateUnbound {
		h.mu.Unlock()
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Set state under lock before starting goroutine
	socket.SetLocalAddr(localAddress.Address, localAddress.Port)
	socket.SetState(preview2.UDPStateBindInProgress)
	h.mu.Unlock()

	// Start bind in goroutine - all socket access is synchronized via h.mu
	go func() {
		addr := fmt.Sprintf("%s:%d", localAddress.Address, localAddress.Port)
		udpAddr, resolveErr := net.ResolveUDPAddr("udp", addr)
		if resolveErr != nil {
			h.mu.Lock()
			socket.SetPendingError(resolveErr)
			h.mu.Unlock()
			return
		}

		conn, listenErr := net.ListenUDP("udp", udpAddr)
		h.mu.Lock()
		defer h.mu.Unlock()

		if socket.State() != preview2.UDPStateBindInProgress {
			if conn != nil {
				conn.Close()
			}
			return
		}

		if listenErr != nil {
			socket.SetPendingError(listenErr)
			return
		}

		socket.SetConn(conn)
		if actualAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok && actualAddr != nil {
			socket.SetLocalAddr(actualAddr.IP.String(), uint16(actualAddr.Port))
		}
	}()

	return nil
}

// [method]udp-socket.finish-bind
func (h *UDPHost) MethodUDPSocketFinishBind(_ context.Context, self uint32) *NetworkError {
	h.mu.Lock()
	defer h.mu.Unlock()

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.UDPStateBindInProgress {
		if socket.State() == preview2.UDPStateUnbound {
			return &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	if pendingErr := socket.PendingError(); pendingErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.UDPStateUnbound)
		return mapNetError(pendingErr)
	}

	if socket.Conn() == nil {
		return &NetworkError{Code: NetworkErrorWouldBlock}
	}

	socket.SetState(preview2.UDPStateBound)
	return nil
}

// [method]udp-socket.stream
func (h *UDPHost) MethodUDPSocketStream(_ context.Context, self uint32, remoteAddress *IPSocketAddress) (uint32, uint32, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, 0, err
	}

	if socket.State() != preview2.UDPStateBound {
		return 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	var remoteAddr string
	var remotePort uint16
	if remoteAddress != nil {
		remoteAddr = remoteAddress.Address
		remotePort = remoteAddress.Port
		socket.SetRemoteAddr(remoteAddr, remotePort)
	}

	incomingStream := preview2.NewIncomingDatagramStreamResource(socket, remoteAddr, remotePort)
	outgoingStream := preview2.NewOutgoingDatagramStreamResource(socket, remoteAddr, remotePort)

	incomingHandle := h.resources.Add(incomingStream)
	outgoingHandle := h.resources.Add(outgoingStream)

	socket.SetStreamHandles(incomingHandle, outgoingHandle)

	return incomingHandle, outgoingHandle, nil
}

// Configuration methods

// [method]udp-socket.address-family
func (h *UDPHost) MethodUDPSocketAddressFamily(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.Family(), nil
}

// [method]udp-socket.local-address
func (h *UDPHost) MethodUDPSocketLocalAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	if socket.State() == preview2.UDPStateUnbound {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	return &IPSocketAddress{
		Address: socket.LocalAddr(),
		Port:    socket.LocalPort(),
	}, nil
}

// [method]udp-socket.remote-address
func (h *UDPHost) MethodUDPSocketRemoteAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	if socket.RemoteAddr() == "" {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	return &IPSocketAddress{
		Address: socket.RemoteAddr(),
		Port:    socket.RemotePort(),
	}, nil
}

// [method]udp-socket.subscribe
func (h *UDPHost) MethodUDPSocketSubscribe(_ context.Context, self uint32) uint32 {
	socket, _ := h.getSocket(self)

	pollable := &preview2.PollableResource{}
	if socket != nil {
		ready := socket.State() == preview2.UDPStateBound ||
			socket.State() == preview2.UDPStateClosed ||
			socket.PendingError() != nil ||
			socket.Conn() != nil
		pollable.SetReady(ready)
	}
	return h.resources.Add(pollable)
}

// Socket options

// [method]udp-socket.receive-buffer-size
func (h *UDPHost) MethodUDPSocketReceiveBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.ReceiveBufferSize(), nil
}

// [method]udp-socket.set-receive-buffer-size
func (h *UDPHost) MethodUDPSocketSetReceiveBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetReceiveBufferSize(value)
	return nil
}

// [method]udp-socket.send-buffer-size
func (h *UDPHost) MethodUDPSocketSendBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.SendBufferSize(), nil
}

// [method]udp-socket.set-send-buffer-size
func (h *UDPHost) MethodUDPSocketSetSendBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetSendBufferSize(value)
	return nil
}

// [method]udp-socket.unicast-hop-limit
func (h *UDPHost) MethodUDPSocketUnicastHopLimit(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.UnicastHopLimit(), nil
}

// [method]udp-socket.set-unicast-hop-limit
func (h *UDPHost) MethodUDPSocketSetUnicastHopLimit(_ context.Context, self uint32, value uint8) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetUnicastHopLimit(value)
	return nil
}

// Datagram stream methods

// getIncomingStream retrieves an incoming datagram stream resource
func (h *UDPHost) getIncomingStream(handle uint32) (*preview2.IncomingDatagramStreamResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	stream, ok := r.(*preview2.IncomingDatagramStreamResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return stream, nil
}

// getOutgoingStream retrieves an outgoing datagram stream resource
func (h *UDPHost) getOutgoingStream(handle uint32) (*preview2.OutgoingDatagramStreamResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	stream, ok := r.(*preview2.OutgoingDatagramStreamResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return stream, nil
}

// Maximum datagrams per receive call to prevent DoS
const maxDatagramsPerReceive = 1024

// [method]incoming-datagram-stream.receive
func (h *UDPHost) MethodIncomingDatagramStreamReceive(_ context.Context, self uint32, maxResults uint64) ([]IncomingDatagram, *NetworkError) {
	stream, err := h.getIncomingStream(self)
	if err != nil {
		return nil, err
	}

	socket := stream.Socket()
	if socket == nil || socket.Conn() == nil {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	conn, ok := socket.Conn().(*net.UDPConn)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Limit to prevent unbounded allocation
	if maxResults > maxDatagramsPerReceive {
		maxResults = maxDatagramsPerReceive
	}

	results := make([]IncomingDatagram, 0, maxResults)
	buf := make([]byte, preview2.DefaultBufferSize)

	for i := uint64(0); i < maxResults; i++ {
		n, addr, readErr := conn.ReadFromUDP(buf)
		if readErr != nil {
			if isWouldBlock(readErr) && len(results) > 0 {
				break
			}
			if len(results) == 0 {
				return nil, mapNetError(readErr)
			}
			break
		}

		// Filter by remote address if connected
		if remoteAddr, remotePort, hasRemote := stream.RemoteAddr(); hasRemote {
			if addr.IP.String() != remoteAddr || uint16(addr.Port) != remotePort {
				continue
			}
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		results = append(results, IncomingDatagram{
			Data: data,
			RemoteAddress: IPSocketAddress{
				Address: addr.IP.String(),
				Port:    uint16(addr.Port),
			},
		})
	}

	return results, nil
}

// [method]incoming-datagram-stream.subscribe
func (h *UDPHost) MethodIncomingDatagramStreamSubscribe(_ context.Context, _ uint32) uint32 {
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

// [method]outgoing-datagram-stream.check-send
func (h *UDPHost) MethodOutgoingDatagramStreamCheckSend(_ context.Context, self uint32) (uint64, *NetworkError) {
	stream, err := h.getOutgoingStream(self)
	if err != nil {
		return 0, err
	}

	socket := stream.Socket()
	if socket == nil || socket.Conn() == nil {
		return 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	return preview2.DefaultBufferSize, nil
}

// [method]outgoing-datagram-stream.send
func (h *UDPHost) MethodOutgoingDatagramStreamSend(_ context.Context, self uint32, datagrams []OutgoingDatagram) (uint64, *NetworkError) {
	stream, err := h.getOutgoingStream(self)
	if err != nil {
		return 0, err
	}

	socket := stream.Socket()
	if socket == nil || socket.Conn() == nil {
		return 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	conn, ok := socket.Conn().(*net.UDPConn)
	if !ok {
		return 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Get default remote address from stream
	defaultAddr, defaultPort, hasDefault := stream.RemoteAddr()

	var sent uint64
	for _, dg := range datagrams {
		var addr *net.UDPAddr

		if dg.RemoteAddress != nil {
			ip := net.ParseIP(dg.RemoteAddress.Address)
			if ip == nil {
				return sent, &NetworkError{Code: NetworkErrorInvalidArgument}
			}
			addr = &net.UDPAddr{
				IP:   ip,
				Port: int(dg.RemoteAddress.Port),
			}
		} else if hasDefault {
			ip := net.ParseIP(defaultAddr)
			if ip == nil {
				return sent, &NetworkError{Code: NetworkErrorInvalidArgument}
			}
			addr = &net.UDPAddr{
				IP:   ip,
				Port: int(defaultPort),
			}
		} else {
			return sent, &NetworkError{Code: NetworkErrorInvalidArgument}
		}

		_, writeErr := conn.WriteToUDP(dg.Data, addr)
		if writeErr != nil {
			if sent == 0 {
				return 0, mapNetError(writeErr)
			}
			break
		}
		sent++
	}

	return sent, nil
}

// [method]outgoing-datagram-stream.subscribe
func (h *UDPHost) MethodOutgoingDatagramStreamSubscribe(_ context.Context, _ uint32) uint32 {
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}
