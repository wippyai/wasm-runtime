package preview2

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/wippyai/wasm-runtime/resource"
)

// Pending bind ownership follows the same contract as TCP: a socket drop joins
// the operation before releasing quota, including failed and late results.
func (s *UDPSocketResource) SetPendingOperation(op TCPNetworkOperation) error {
	if op == nil {
		return errors.New("nil UDP bind operation")
	}
	s.mu.Lock()
	if s.dropped || s.state == UDPStateClosed {
		s.mu.Unlock()
		_ = op.Close()
		return resource.ErrClosed
	}
	if s.state != UDPStateBindInProgress || s.pendingOp != nil || s.conn != nil {
		s.mu.Unlock()
		return errors.New("UDP socket cannot attach bind operation")
	}
	s.pendingOp = op
	if s.bindNotify != nil {
		close(s.bindNotify)
		s.bindNotify = nil
	}
	s.mu.Unlock()
	return nil
}
func (s *UDPSocketResource) PendingOperation() TCPNetworkOperation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingOp
}

func (s *UDPSocketResource) ResolvePendingBind() (bool, error) {
	s.mu.Lock()
	if s.dropped || s.state == UDPStateClosed {
		s.mu.Unlock()
		return false, resource.ErrClosed
	}
	if s.pendingErr != nil {
		err := s.pendingErr
		s.mu.Unlock()
		return false, err
	}
	if s.state != UDPStateBindInProgress {
		s.mu.Unlock()
		return false, errors.New("UDP bind not in progress")
	}
	op := s.pendingOp
	if op == nil {
		ready := s.conn != nil
		s.mu.Unlock()
		return ready, nil
	}
	value, err, ready := op.Take()
	if !ready {
		s.mu.Unlock()
		return false, nil
	}
	if err != nil {
		return s.finishUDPBindErrorLocked(op, value, err)
	}
	conn, ok := value.(*net.UDPConn)
	if !ok || conn == nil {
		if ok {
			value = nil
		}
		return s.finishUDPBindErrorLocked(op, value, errors.New("UDP bind returned no UDP connection"))
	}
	s.conn = conn
	if address, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		s.localAddr = address.IP.String()
		if address.Zone != "" {
			s.localAddr += "%" + address.Zone
		}
		s.localPort = uint16(address.Port)
	}
	s.pendingOp = nil
	if s.bindNotify != nil {
		close(s.bindNotify)
		s.bindNotify = nil
	}
	s.mu.Unlock()
	_ = op.Close()
	return true, nil
}

// Keep the operation attached while disposing failure, so Drop must join it.
func (s *UDPSocketResource) finishUDPBindErrorLocked(op TCPNetworkOperation, value io.Closer, err error) (bool, error) {
	if value != nil {
		_ = value.Close()
	}
	s.pendingErr = err
	s.mu.Unlock()
	_ = op.Close()
	s.mu.Lock()
	if s.pendingOp == op {
		s.pendingOp = nil
	}
	s.mu.Unlock()
	return false, err
}

func (s *UDPSocketResource) bindReadyLocked() bool {
	if s.dropped || s.state != UDPStateBindInProgress || s.pendingErr != nil || s.conn != nil {
		return true
	}
	return s.pendingOp != nil && s.pendingOp.Ready()
}
func (s *UDPSocketResource) Subscribe() Pollable { return (*udpSocketPollable)(s) }

type udpSocketPollable UDPSocketResource

func (*udpSocketPollable) Type() ResourceType { return ResourcePollable }
func (*udpSocketPollable) Drop()              {}
func (p *udpSocketPollable) Ready() bool {
	s := (*UDPSocketResource)(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bindReadyLocked()
}
func (p *udpSocketPollable) Notify() <-chan struct{} {
	s := (*UDPSocketResource)(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bindReadyLocked() {
		return closedPollableChan
	}
	if s.pendingOp != nil {
		return s.pendingOp.Notify()
	}
	if s.bindNotify == nil {
		s.bindNotify = make(chan struct{})
	}
	return s.bindNotify
}
func (p *udpSocketPollable) Block(ctx context.Context) { blockUDPStream(ctx, p) }
