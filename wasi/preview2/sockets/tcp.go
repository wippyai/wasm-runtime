package sockets

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// TCPHost implements wasi:sockets/tcp@0.2.0
type TCPHost struct {
	resources *preview2.ResourceTable
	mu        sync.Mutex
}

// NewTCPHost creates a new TCP host
func NewTCPHost(resources *preview2.ResourceTable) *TCPHost {
	return &TCPHost{resources: resources}
}

// Namespace returns the WASI namespace
func (h *TCPHost) Namespace() string {
	return "wasi:sockets/tcp@0.2.0"
}

// IPSocketAddress represents an IP address and port
type IPSocketAddress struct {
	Address string
	Port    uint16
}

func (a *IPSocketAddress) String() string {
	if a == nil {
		return ""
	}
	return net.JoinHostPort(a.Address, strconv.Itoa(int(a.Port)))
}

// getSocket retrieves and validates a TCP socket resource
func (h *TCPHost) getSocket(handle uint32) (*preview2.TCPSocketResource, *NetworkError) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	socket, ok := r.(*preview2.TCPSocketResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return socket, nil
}

// Connection management methods

// [method]tcp-socket.start-bind
func (h *TCPHost) MethodTCPSocketStartBind(_ context.Context, self uint32, _ uint32, localAddress IPSocketAddress) *NetworkError {
	h.mu.Lock()
	defer h.mu.Unlock()

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateUnbound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Store bind address for finish-bind
	socket.SetLocalAddr(localAddress.Address, localAddress.Port)
	socket.SetState(preview2.TCPStateBindInProgress)

	return nil
}

// [method]tcp-socket.finish-bind
func (h *TCPHost) MethodTCPSocketFinishBind(_ context.Context, self uint32) *NetworkError {
	h.mu.Lock()
	defer h.mu.Unlock()

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateBindInProgress {
		if socket.State() == preview2.TCPStateUnbound {
			return &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Binding is just address reservation for TCP, actual binding happens at listen/connect
	socket.SetState(preview2.TCPStateBound)
	return nil
}

// [method]tcp-socket.start-connect
func (h *TCPHost) MethodTCPSocketStartConnect(ctx context.Context, self uint32, _ uint32, remoteAddress IPSocketAddress) *NetworkError {
	h.mu.Lock()
	defer h.mu.Unlock()

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	state := socket.State()
	if state != preview2.TCPStateUnbound && state != preview2.TCPStateBound {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Store remote address
	socket.SetRemoteAddr(remoteAddress.Address, remoteAddress.Port)
	socket.SetState(preview2.TCPStateConnectInProgress)

	// Start async connection in goroutine
	go func() {
		addr := remoteAddress.String()
		dialer := net.Dialer{}

		// Build local address if bound
		if socket.LocalAddr() != "" {
			localAddr := fmt.Sprintf("%s:%d", socket.LocalAddr(), socket.LocalPort())
			tcpAddr, err := net.ResolveTCPAddr("tcp", localAddr)
			if err != nil {
				// Log but continue - connection will use system default
				h.mu.Lock()
				socket.SetPendingError(fmt.Errorf("resolve local address %q: %w", localAddr, err))
				h.mu.Unlock()
				return
			}
			dialer.LocalAddr = tcpAddr
		}

		conn, connErr := dialer.DialContext(ctx, "tcp", addr)
		h.mu.Lock()
		defer h.mu.Unlock()

		if socket.State() != preview2.TCPStateConnectInProgress {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}

		if connErr != nil {
			socket.SetPendingError(connErr)
			return
		}

		socket.SetConn(conn)
		// Update local address from actual connection
		if tcpAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
			socket.SetLocalAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
		}
	}()

	return nil
}

// [method]tcp-socket.finish-connect
func (h *TCPHost) MethodTCPSocketFinishConnect(_ context.Context, self uint32) (uint32, uint32, *NetworkError) {
	h.mu.Lock()
	defer h.mu.Unlock()

	socket, err := h.getSocket(self)
	if err != nil {
		return 0, 0, err
	}

	if socket.State() != preview2.TCPStateConnectInProgress {
		if socket.State() == preview2.TCPStateUnbound || socket.State() == preview2.TCPStateBound {
			return 0, 0, &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Check for pending error
	if pendingErr := socket.PendingError(); pendingErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.TCPStateClosed)
		return 0, 0, mapNetError(pendingErr)
	}

	// Check if connection is ready
	if socket.Conn() == nil {
		return 0, 0, &NetworkError{Code: NetworkErrorWouldBlock}
	}

	socket.SetState(preview2.TCPStateConnected)

	// Create input and output streams
	inputStream := preview2.NewTCPInputStreamResource(socket)
	outputStream := preview2.NewTCPOutputStreamResource(socket)

	inputHandle := h.resources.Add(inputStream)
	outputHandle := h.resources.Add(outputStream)

	socket.SetStreamHandles(inputHandle, outputHandle)

	return inputHandle, outputHandle, nil
}

// [method]tcp-socket.start-listen
func (h *TCPHost) MethodTCPSocketStartListen(ctx context.Context, self uint32) *NetworkError {
	h.mu.Lock()
	defer h.mu.Unlock()

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	state := socket.State()
	if state != preview2.TCPStateBound {
		if state == preview2.TCPStateUnbound {
			return &NetworkError{Code: NetworkErrorInvalidState}
		}
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	socket.SetState(preview2.TCPStateListenInProgress)

	// Start listener in goroutine
	go func() {
		addr := fmt.Sprintf("%s:%d", socket.LocalAddr(), socket.LocalPort())
		lc := net.ListenConfig{}
		listener, listenErr := lc.Listen(ctx, "tcp", addr)

		h.mu.Lock()
		defer h.mu.Unlock()

		if socket.State() != preview2.TCPStateListenInProgress {
			if listener != nil {
				_ = listener.Close()
			}
			return
		}

		if listenErr != nil {
			socket.SetPendingError(listenErr)
			return
		}

		socket.SetListener(listener)
		// Update local address from actual listener
		if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
			socket.SetLocalAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
		}
	}()

	return nil
}

// [method]tcp-socket.finish-listen
func (h *TCPHost) MethodTCPSocketFinishListen(_ context.Context, self uint32) *NetworkError {
	h.mu.Lock()
	defer h.mu.Unlock()

	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateListenInProgress {
		if socket.State() == preview2.TCPStateBound {
			return &NetworkError{Code: NetworkErrorNotInProgress}
		}
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Check for pending error
	if pendingErr := socket.PendingError(); pendingErr != nil {
		socket.ClearPendingError()
		socket.SetState(preview2.TCPStateBound)
		return mapNetError(pendingErr)
	}

	// Check if listener is ready
	if socket.Listener() == nil {
		return &NetworkError{Code: NetworkErrorWouldBlock}
	}

	socket.SetState(preview2.TCPStateListening)
	return nil
}

// [method]tcp-socket.accept
func (h *TCPHost) MethodTCPSocketAccept(_ context.Context, self uint32) (uint32, uint32, uint32, *NetworkError) {
	h.mu.Lock()
	socket, err := h.getSocket(self)
	if err != nil {
		h.mu.Unlock()
		return 0, 0, 0, err
	}

	if socket.State() != preview2.TCPStateListening {
		h.mu.Unlock()
		return 0, 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	listener := socket.Listener()
	if listener == nil {
		h.mu.Unlock()
		return 0, 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}
	h.mu.Unlock()

	netListener, ok := listener.(net.Listener)
	if !ok {
		return 0, 0, 0, &NetworkError{Code: NetworkErrorInvalidState}
	}

	// Accept connection (this will block if no connection pending)
	conn, acceptErr := netListener.Accept()
	if acceptErr != nil {
		return 0, 0, 0, mapNetError(acceptErr)
	}

	// Create new socket resource for accepted connection
	newSocket := preview2.NewTCPSocketResource(socket.Family())
	newSocket.SetState(preview2.TCPStateConnected)
	newSocket.SetConn(conn)

	if tcpAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		newSocket.SetLocalAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
	}
	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		newSocket.SetRemoteAddr(tcpAddr.IP.String(), uint16(tcpAddr.Port))
	}

	socketHandle := h.resources.Add(newSocket)

	// Create streams for the new socket
	inputStream := preview2.NewTCPInputStreamResource(newSocket)
	outputStream := preview2.NewTCPOutputStreamResource(newSocket)

	inputHandle := h.resources.Add(inputStream)
	outputHandle := h.resources.Add(outputStream)

	newSocket.SetStreamHandles(inputHandle, outputHandle)

	return socketHandle, inputHandle, outputHandle, nil
}

// [method]tcp-socket.shutdown
func (h *TCPHost) MethodTCPSocketShutdown(_ context.Context, self uint32, shutdownType uint8) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}

	if socket.State() != preview2.TCPStateConnected {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	conn := socket.Conn()
	if conn == nil {
		return &NetworkError{Code: NetworkErrorInvalidState}
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return &NetworkError{Code: NetworkErrorNotSupported}
	}

	var shutdownErr error
	switch shutdownType {
	case 0: // Receive
		shutdownErr = tcpConn.CloseRead()
	case 1: // Send
		shutdownErr = tcpConn.CloseWrite()
	case 2: // Both
		shutdownErr = tcpConn.CloseRead()
		if shutdownErr == nil {
			shutdownErr = tcpConn.CloseWrite()
		}
	default:
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	if shutdownErr != nil {
		return mapNetError(shutdownErr)
	}

	return nil
}

// Configuration methods

// [method]tcp-socket.address-family
func (h *TCPHost) MethodTCPSocketAddressFamily(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.Family(), nil
}

// [method]tcp-socket.local-address
func (h *TCPHost) MethodTCPSocketLocalAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	state := socket.State()
	if state == preview2.TCPStateUnbound {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	return &IPSocketAddress{
		Address: socket.LocalAddr(),
		Port:    socket.LocalPort(),
	}, nil
}

// [method]tcp-socket.remote-address
func (h *TCPHost) MethodTCPSocketRemoteAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return nil, err
	}

	if socket.State() != preview2.TCPStateConnected {
		return nil, &NetworkError{Code: NetworkErrorInvalidState}
	}

	return &IPSocketAddress{
		Address: socket.RemoteAddr(),
		Port:    socket.RemotePort(),
	}, nil
}

// [method]tcp-socket.is-listening
func (h *TCPHost) MethodTCPSocketIsListening(_ context.Context, self uint32) bool {
	socket, err := h.getSocket(self)
	if err != nil {
		return false
	}
	return socket.IsListening()
}

// [method]tcp-socket.subscribe
func (h *TCPHost) MethodTCPSocketSubscribe(_ context.Context, self uint32) uint32 {
	socket, _ := h.getSocket(self)

	pollable := &preview2.PollableResource{}
	// Pollable is ready when socket is in a ready state
	if socket != nil {
		state := socket.State()
		ready := state == preview2.TCPStateConnected ||
			state == preview2.TCPStateListening ||
			state == preview2.TCPStateClosed ||
			socket.PendingError() != nil ||
			socket.Conn() != nil ||
			socket.Listener() != nil
		pollable.SetReady(ready)
	}
	return h.resources.Add(pollable)
}

// Socket options

// [method]tcp-socket.hop-limit
func (h *TCPHost) MethodTCPSocketHopLimit(_ context.Context, self uint32) (uint8, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.HopLimit(), nil
}

// [method]tcp-socket.set-hop-limit
func (h *TCPHost) MethodTCPSocketSetHopLimit(_ context.Context, self uint32, value uint8) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetHopLimit(value)
	return nil
}

// [method]tcp-socket.receive-buffer-size
func (h *TCPHost) MethodTCPSocketReceiveBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.ReceiveBufferSize(), nil
}

// [method]tcp-socket.set-receive-buffer-size
func (h *TCPHost) MethodTCPSocketSetReceiveBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetReceiveBufferSize(value)
	return nil
}

// [method]tcp-socket.send-buffer-size
func (h *TCPHost) MethodTCPSocketSendBufferSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.SendBufferSize(), nil
}

// [method]tcp-socket.set-send-buffer-size
func (h *TCPHost) MethodTCPSocketSetSendBufferSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetSendBufferSize(value)
	return nil
}

// [method]tcp-socket.listen-backlog-size
func (h *TCPHost) MethodTCPSocketListenBacklogSize(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.ListenBacklogSize(), nil
}

// [method]tcp-socket.set-listen-backlog-size
func (h *TCPHost) MethodTCPSocketSetListenBacklogSize(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetListenBacklogSize(value)
	return nil
}

// Keep-alive options

// [method]tcp-socket.keep-alive-enabled
func (h *TCPHost) MethodTCPSocketKeepAliveEnabled(_ context.Context, self uint32) (bool, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return false, err
	}
	return socket.KeepAliveEnabled(), nil
}

// [method]tcp-socket.set-keep-alive-enabled
func (h *TCPHost) MethodTCPSocketSetKeepAliveEnabled(_ context.Context, self uint32, value bool) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveEnabled(value)
	return nil
}

// [method]tcp-socket.keep-alive-idle-time
func (h *TCPHost) MethodTCPSocketKeepAliveIdleTime(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.KeepAliveIdleTime(), nil
}

// [method]tcp-socket.set-keep-alive-idle-time
func (h *TCPHost) MethodTCPSocketSetKeepAliveIdleTime(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveIdleTime(value)
	return nil
}

// [method]tcp-socket.keep-alive-interval
func (h *TCPHost) MethodTCPSocketKeepAliveInterval(_ context.Context, self uint32) (uint64, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.KeepAliveInterval(), nil
}

// [method]tcp-socket.set-keep-alive-interval
func (h *TCPHost) MethodTCPSocketSetKeepAliveInterval(_ context.Context, self uint32, value uint64) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveInterval(value)
	return nil
}

// [method]tcp-socket.keep-alive-count
func (h *TCPHost) MethodTCPSocketKeepAliveCount(_ context.Context, self uint32) (uint32, *NetworkError) {
	socket, err := h.getSocket(self)
	if err != nil {
		return 0, err
	}
	return socket.KeepAliveCount(), nil
}

// [method]tcp-socket.set-keep-alive-count
func (h *TCPHost) MethodTCPSocketSetKeepAliveCount(_ context.Context, self uint32, value uint32) *NetworkError {
	socket, err := h.getSocket(self)
	if err != nil {
		return err
	}
	socket.SetKeepAliveCount(value)
	return nil
}
