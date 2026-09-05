package preview2

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/wippyai/wasm-runtime/resource"
)

// TCPNetworkOperation represents a nonblocking/async pending network operation
// (such as connect or listen) owned by a TCPSocketResource.
type TCPNetworkOperation interface {
	Ready() bool
	Notify() <-chan struct{}
	Take() (io.Closer, error, bool)
	Close() error
}

// SetPendingOperation attaches an owned pending network operation to the socket.
// The operation is attached only if the socket is in TCPStateConnectInProgress or
// TCPStateListenInProgress, has not already been resolved, and does not already have
// a pending operation attached.
//
// Ownership lifecycle:
//   - If the socket has already been dropped or closed (late attach), the attach is rejected,
//     the provided operation is closed by the dropped socket to prevent leaked resources/connections,
//     and resource.ErrClosed is returned.
//   - In all other rejection cases (nil operation, invalid state, duplicate attach, or already resolved),
//     the operation is rejected without being closed and remains caller-owned.
func (s *TCPSocketResource) SetPendingOperation(op TCPNetworkOperation) error {
	if op == nil {
		return errors.New("nil pending operation")
	}

	s.mu.Lock()
	if s.dropped || s.state == TCPStateClosed {
		s.mu.Unlock()
		_ = op.Close()
		return resource.ErrClosed
	}

	if s.state != TCPStateConnectInProgress && s.state != TCPStateListenInProgress {
		s.mu.Unlock()
		return errors.New("socket not in connect or listen in progress state")
	}

	if s.pendingOp != nil {
		s.mu.Unlock()
		return errors.New("pending operation already attached")
	}

	if (s.state == TCPStateConnectInProgress && s.conn != nil) ||
		(s.state == TCPStateListenInProgress && s.listener != nil) {
		s.mu.Unlock()
		return errors.New("operation already resolved")
	}

	s.pendingOp = op
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
	s.mu.Unlock()
	return nil
}

// PendingOperation returns the currently attached pending network operation, if any.
func (s *TCPSocketResource) PendingOperation() TCPNetworkOperation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingOp
}

// ResolvePendingConnect attempts to resolve a pending connect operation.
// It holds the socket lock across nonblocking op.Take and installing net.Conn atomically,
// ensuring concurrent Drop cannot release socket quota while taken physical socket is between owners.
//
// Return values:
//   - (false, nil) if the operation is not yet ready.
//   - (true, nil) on success. Sets Conn and clears pendingOp, but DOES NOT change TCP state
//     (W1 finish handles state transitions and streams).
//     Pending op is cleaned up (Close) outside the socket lock after safe transfer.
//   - (false, err) on error (operation error, nil connection, or wrong type). Any returned
//     closer is closed before removing ownership, pending op is kept attached during op.Close
//     outside the lock and cleared afterwards, and the error is returned.
func (s *TCPSocketResource) ResolvePendingConnect() (bool, error) {
	s.mu.Lock()

	if s.dropped || s.state == TCPStateClosed {
		s.mu.Unlock()
		return false, resource.ErrClosed
	}

	if s.pendingErr != nil {
		err := s.pendingErr
		s.mu.Unlock()
		return false, err
	}

	if s.pendingOp == nil {
		if s.conn != nil {
			s.mu.Unlock()
			return true, nil
		}
		if s.state != TCPStateConnectInProgress {
			s.mu.Unlock()
			return false, errors.New("socket not in connect in progress state")
		}
		s.mu.Unlock()
		return false, nil
	}

	op := s.pendingOp

	// Hold socket lock across nonblocking op.Take and atomic installation.
	closer, err, ready := op.Take()
	if !ready {
		s.mu.Unlock()
		return false, nil
	}

	if err != nil {
		return s.finishPendingErrorLocked(op, closer, err)
	}

	if closer == nil {
		return s.finishPendingErrorLocked(op, nil, errors.New("nil connection from pending connect operation"))
	}

	conn, ok := closer.(net.Conn)
	if !ok {
		return s.finishPendingErrorLocked(op, closer, errors.New("pending connect operation did not produce net.Conn"))
	}

	// Safe transfer: install net.Conn atomically while holding socket lock.
	s.conn = conn
	if tcpAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		s.localAddr = tcpAddressHost(tcpAddr)
		s.localPort = uint16(tcpAddr.Port)
	}
	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		s.remoteAddr = tcpAddressHost(tcpAddr)
		s.remotePort = uint16(tcpAddr.Port)
	}

	s.pendingOp = nil
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
	s.mu.Unlock()

	// Safe transfer complete: cleanup pending op outside socket lock.
	_ = op.Close()

	return true, nil
}

// ResolvePendingListen attempts to resolve a pending listen operation.
// It holds the socket lock across nonblocking op.Take and installing net.Listener atomically,
// ensuring concurrent Drop cannot release socket quota while taken physical socket is between owners.
//
// Return values:
//   - (false, nil) if the operation is not yet ready.
//   - (true, nil) on success. Sets Listener and clears pendingOp, but DOES NOT change TCP state
//     (W1 finish handles state transitions and acceptqueue).
//     Pending op is cleaned up (Close) outside the socket lock after safe transfer.
//   - (false, err) on error (operation error, nil listener, or wrong type). Any returned
//     closer is closed before removing ownership, pending op is kept attached during op.Close
//     outside the lock and cleared afterwards, and the error is returned.
func (s *TCPSocketResource) ResolvePendingListen() (bool, error) {
	s.mu.Lock()

	if s.dropped || s.state == TCPStateClosed {
		s.mu.Unlock()
		return false, resource.ErrClosed
	}

	if s.pendingErr != nil {
		err := s.pendingErr
		s.mu.Unlock()
		return false, err
	}

	if s.pendingOp == nil {
		if s.listener != nil {
			s.mu.Unlock()
			return true, nil
		}
		if s.state != TCPStateListenInProgress {
			s.mu.Unlock()
			return false, errors.New("socket not in listen in progress state")
		}
		s.mu.Unlock()
		return false, nil
	}

	op := s.pendingOp

	// Hold socket lock across nonblocking op.Take and atomic installation.
	closer, err, ready := op.Take()
	if !ready {
		s.mu.Unlock()
		return false, nil
	}

	if err != nil {
		return s.finishPendingErrorLocked(op, closer, err)
	}

	if closer == nil {
		return s.finishPendingErrorLocked(op, nil, errors.New("nil listener from pending listen operation"))
	}

	listener, ok := closer.(net.Listener)
	if !ok {
		return s.finishPendingErrorLocked(op, closer, errors.New("pending listen operation did not produce net.Listener"))
	}

	s.listener = listener
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		s.localAddr = tcpAddressHost(tcpAddr)
		s.localPort = uint16(tcpAddr.Port)
	}

	s.pendingOp = nil
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
	s.mu.Unlock()

	// Safe transfer complete: cleanup pending op outside socket lock.
	_ = op.Close()

	return true, nil
}

func (s *TCPSocketResource) finishPendingErrorLocked(op TCPNetworkOperation, closer io.Closer, err error) (bool, error) {
	// Erroneous closer must physically close before ownership is detached.
	if closer != nil {
		_ = closer.Close()
	}
	s.pendingErr = err
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
	s.mu.Unlock()

	// Keep operation attached during Close outside socket mutex so concurrent Drop
	// will observe the operation and wait for its cleanup before quota release.
	_ = op.Close()

	s.mu.Lock()
	if s.pendingOp == op {
		s.pendingOp = nil
	}
	s.mu.Unlock()

	return false, err
}

// Ready returns true if the socket is ready for immediate I/O or polling progress:
// - Dropped, closed, or pending error (terminal readiness).
// - Idle states (unbound, bound, connected).
// - In-progress connect/listen: reflects op.Ready() (or true if conn/listener already installed).
// - Listening: reflects acceptQueue.Ready() (or true if no accept queue attached).
func (s *TCPSocketResource) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyLocked()
}

func (s *TCPSocketResource) readyLocked() bool {
	if s.dropped || s.state == TCPStateClosed || s.pendingErr != nil {
		return true
	}

	switch s.state {
	case TCPStateConnectInProgress:
		if s.conn != nil {
			return true
		}
		if s.pendingOp != nil {
			return s.pendingOp.Ready()
		}
		return false
	case TCPStateListenInProgress:
		if s.listener != nil {
			return true
		}
		if s.pendingOp != nil {
			return s.pendingOp.Ready()
		}
		return false
	case TCPStateListening:
		if s.acceptQueue != nil {
			return s.acceptQueue.Ready()
		}
		return true
	default: // TCPStateUnbound, TCPStateBindInProgress, TCPStateBound, TCPStateConnected
		return true
	}
}

// Notify returns a channel that is closed when the socket is ready or dropped.
// If not currently ready, it returns the pending operation's or accept queue's
// notification channel, or a channel that will close when an operation is attached,
// state transitions, or the socket is dropped.
func (s *TCPSocketResource) Notify() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.readyLocked() {
		return closedPollableChan
	}

	switch s.state {
	case TCPStateListening:
		if s.acceptQueue != nil {
			return s.acceptQueue.Notify()
		}
	case TCPStateConnectInProgress, TCPStateListenInProgress:
		if s.pendingOp != nil {
			return s.pendingOp.Notify()
		}
	}

	if s.notifyCh == nil {
		s.notifyCh = make(chan struct{})
	}
	return s.notifyCh
}

// Subscribe returns a WASI Pollable representation of this socket's readiness.
func (s *TCPSocketResource) Subscribe() Pollable {
	return (*tcpSocketPollable)(s)
}

type tcpSocketPollable TCPSocketResource

func (*tcpSocketPollable) Type() ResourceType        { return ResourcePollable }
func (*tcpSocketPollable) Drop()                     {}
func (p *tcpSocketPollable) Ready() bool             { return (*TCPSocketResource)(p).Ready() }
func (p *tcpSocketPollable) Notify() <-chan struct{} { return (*TCPSocketResource)(p).Notify() }
func (p *tcpSocketPollable) Block(ctx context.Context) {
	for !p.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}

var (
	_ Pollable       = (*tcpSocketPollable)(nil)
	_ NotifyPollable = (*tcpSocketPollable)(nil)
)

func tcpAddressHost(addr *net.TCPAddr) string {
	host := addr.IP.String()
	if addr.Zone != "" {
		host += "%" + addr.Zone
	}
	return host
}
