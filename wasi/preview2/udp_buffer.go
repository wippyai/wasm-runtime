package preview2

import (
	"context"
	"errors"
	"io"
	"net"
)

const (
	// UDPMaxQueuedDatagrams is the maximum number of datagrams queued per direction.
	// CheckSend occupancy includes the in-flight write.
	UDPMaxQueuedDatagrams = 16

	// UDPMaxDatagramBytes is the maximum payload size accepted or received.
	UDPMaxDatagramBytes = 65535
)

var (
	// ErrUDPSocketClosed is returned by queue operations after Drop.
	ErrUDPSocketClosed = errors.New("udp socket closed")

	// ErrUDPSocketNotBound is returned when no datagram-capable connection is attached.
	ErrUDPSocketNotBound = errors.New("udp socket has no datagram connection")

	// ErrDatagramTooLarge is returned when a datagram exceeds UDPMaxDatagramBytes.
	ErrDatagramTooLarge = errors.New("datagram too large")
)

// UDPDatagram is an owned UDP payload and destination/source address.
type UDPDatagram struct {
	Address *net.UDPAddr
	Data    []byte
}

type udpPacketConn interface {
	ReadFromUDP([]byte) (int, *net.UDPAddr, error)
	WriteToUDP([]byte, *net.UDPAddr) (int, error)
	Close() error
}

func copyUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	out := &net.UDPAddr{Port: addr.Port, Zone: addr.Zone}
	if addr.IP != nil {
		out.IP = make(net.IP, len(addr.IP))
		copy(out.IP, addr.IP)
	}
	return out
}

func copyUDPDatagram(dg UDPDatagram) UDPDatagram {
	out := UDPDatagram{Address: copyUDPAddr(dg.Address)}
	if dg.Data != nil {
		out.Data = make([]byte, len(dg.Data))
		copy(out.Data, dg.Data)
	}
	return out
}

func (s *UDPSocketResource) sendOccupancyLocked() int {
	n := len(s.sendQ)
	if s.sendInflight {
		n++
	}
	return n
}

func (s *UDPSocketResource) updateIncomingReadyLocked() {
	if s.dropped {
		s.incomingPoll.Drop()
		return
	}
	s.incomingPoll.SetReady(len(s.recvQ) > 0 || s.recvErr != nil)
}

func (s *UDPSocketResource) updateOutgoingReadyLocked() {
	if s.dropped {
		s.outgoingPoll.Drop()
		return
	}
	s.outgoingPoll.SetReady(s.sendErr != nil || s.sendOccupancyLocked() < UDPMaxQueuedDatagrams)
}

func (s *UDPSocketResource) ensurePumpsLocked() {
	if s.dropped || s.pumpsStarted {
		return
	}
	conn, ok := s.conn.(udpPacketConn)
	if !ok || conn == nil {
		return
	}
	s.pumpsStarted = true
	s.recvDone = make(chan struct{})
	s.sendDone = make(chan struct{})
	if cap(s.recvQ) < UDPMaxQueuedDatagrams {
		s.recvQ = make([]UDPDatagram, 0, UDPMaxQueuedDatagrams)
	}
	if cap(s.sendQ) < UDPMaxQueuedDatagrams {
		s.sendQ = make([]UDPDatagram, 0, UDPMaxQueuedDatagrams)
	}
	go s.runReadPump(conn)
	go s.runWritePump(conn)
}

func (s *UDPSocketResource) runReadPump(conn udpPacketConn) {
	defer close(s.recvDone)
	buf := make([]byte, UDPMaxDatagramBytes)

	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if n < 0 || n > len(buf) {
			n = 0
			if err == nil {
				err = io.ErrNoProgress
			}
		}

		got := err == nil || n > 0
		var pkt UDPDatagram
		if got {
			pkt.Data = make([]byte, n)
			copy(pkt.Data, buf[:n])
			pkt.Address = copyUDPAddr(addr)
		}

		s.mu.Lock()
		if s.dropped {
			s.mu.Unlock()
			return
		}
		if got {
			for len(s.recvQ) >= UDPMaxQueuedDatagrams && !s.dropped {
				s.recvCond.Wait()
			}
			if s.dropped {
				s.mu.Unlock()
				return
			}
			s.recvQ = append(s.recvQ, pkt)
			s.updateIncomingReadyLocked()
		}
		if err != nil {
			s.recvErr = err
			s.updateIncomingReadyLocked()
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

func (s *UDPSocketResource) runWritePump(conn udpPacketConn) {
	defer close(s.sendDone)

	for {
		s.mu.Lock()
		for len(s.sendQ) == 0 && !s.dropped {
			s.sendCond.Wait()
		}
		if s.dropped {
			s.mu.Unlock()
			return
		}
		pkt := s.sendQ[0]
		s.sendQ[0] = UDPDatagram{}
		copy(s.sendQ, s.sendQ[1:])
		s.sendQ[len(s.sendQ)-1] = UDPDatagram{}
		s.sendQ = s.sendQ[:len(s.sendQ)-1]
		s.sendInflight = true
		s.updateOutgoingReadyLocked()
		s.mu.Unlock()

		n, err := conn.WriteToUDP(pkt.Data, pkt.Address)
		if err == nil && n != len(pkt.Data) {
			err = io.ErrShortWrite
		}

		s.mu.Lock()
		s.sendInflight = false
		if s.dropped {
			s.mu.Unlock()
			return
		}
		if err != nil {
			s.sendErr = err
			s.updateOutgoingReadyLocked()
			s.mu.Unlock()
			return
		}
		s.updateOutgoingReadyLocked()
		s.mu.Unlock()
	}
}

func (s *UDPSocketResource) closeJoinClear() {
	s.mu.Lock()
	s.dropped = true
	s.state = UDPStateClosed
	conn := s.conn
	s.conn = nil
	pending := s.pendingOp
	if s.bindNotify != nil {
		close(s.bindNotify)
		s.bindNotify = nil
	}
	started := s.pumpsStarted
	recvDone := s.recvDone
	sendDone := s.sendDone
	s.incomingPoll.Drop()
	s.outgoingPoll.Drop()
	if s.recvCond != nil {
		s.recvCond.Broadcast()
	}
	if s.sendCond != nil {
		s.sendCond.Broadcast()
	}
	s.mu.Unlock()

	if pending != nil {
		_ = pending.Close()
	}
	if c, ok := conn.(interface{ Close() error }); ok {
		_ = c.Close()
	}

	if started {
		if recvDone != nil {
			<-recvDone
		}
		if sendDone != nil {
			<-sendDone
		}
	}

	s.mu.Lock()
	s.recvQ = nil
	s.sendQ = nil
	s.pendingOp = nil
	s.sendInflight = false
	s.mu.Unlock()
}

// ReceiveDatagrams returns up to max owned datagrams without blocking.
// An empty queue while open returns an empty slice and a nil error.
func (s *UDPSocketResource) ReceiveDatagrams(max uint64) ([]UDPDatagram, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensurePumpsLocked()

	if s.dropped {
		return nil, ErrUDPSocketClosed
	}
	if !s.pumpsStarted {
		return nil, ErrUDPSocketNotBound
	}
	if max == 0 {
		return []UDPDatagram{}, nil
	}
	if len(s.recvQ) == 0 {
		if s.recvErr != nil {
			return nil, s.recvErr
		}
		return []UDPDatagram{}, nil
	}

	n := len(s.recvQ)
	if max < uint64(n) {
		n = int(max)
	}
	out := make([]UDPDatagram, n)
	for i := 0; i < n; i++ {
		out[i] = s.recvQ[i]
		s.recvQ[i] = UDPDatagram{}
	}
	copy(s.recvQ, s.recvQ[n:])
	clear(s.recvQ[len(s.recvQ)-n:])
	s.recvQ = s.recvQ[:len(s.recvQ)-n]
	s.recvCond.Signal()
	s.updateIncomingReadyLocked()
	return out, nil
}

// CheckSend returns the number of datagrams that currently fit in the send
// queue, counting the in-flight write against the cap. It never blocks.
func (s *UDPSocketResource) CheckSend() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensurePumpsLocked()

	if s.dropped {
		return 0, ErrUDPSocketClosed
	}
	if !s.pumpsStarted {
		return 0, ErrUDPSocketNotBound
	}
	if s.sendErr != nil {
		return 0, s.sendErr
	}
	occ := s.sendOccupancyLocked()
	if occ >= UDPMaxQueuedDatagrams {
		return 0, nil
	}
	return uint64(UDPMaxQueuedDatagrams - occ), nil
}

// SendDatagrams copies datagrams into the send queue up to remaining capacity.
// After any accepted packet, a later failure returns the accepted count and a nil error.
func (s *UDPSocketResource) SendDatagrams(datagrams []UDPDatagram) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensurePumpsLocked()

	if s.dropped {
		return 0, ErrUDPSocketClosed
	}
	if !s.pumpsStarted {
		return 0, ErrUDPSocketNotBound
	}
	if s.sendErr != nil {
		return 0, s.sendErr
	}
	if len(datagrams) == 0 {
		return 0, nil
	}

	var accepted uint64
	for _, dg := range datagrams {
		if len(dg.Data) > UDPMaxDatagramBytes {
			if accepted > 0 {
				break
			}
			return 0, ErrDatagramTooLarge
		}
		if s.sendOccupancyLocked() >= UDPMaxQueuedDatagrams {
			break
		}
		s.sendQ = append(s.sendQ, copyUDPDatagram(dg))
		accepted++
	}
	if accepted > 0 {
		s.sendCond.Signal()
	}
	s.updateOutgoingReadyLocked()
	return accepted, nil
}

// IncomingPollable returns a borrowed live pollable for inbound datagrams.
func (s *UDPSocketResource) IncomingPollable() Pollable {
	s.mu.Lock()
	s.ensurePumpsLocked()
	s.mu.Unlock()
	return (*udpIncomingPollable)(s)
}

// OutgoingPollable returns a borrowed live pollable for outbound capacity.
func (s *UDPSocketResource) OutgoingPollable() Pollable {
	s.mu.Lock()
	s.ensurePumpsLocked()
	s.mu.Unlock()
	return (*udpOutgoingPollable)(s)
}

type udpIncomingPollable UDPSocketResource

func (*udpIncomingPollable) Type() ResourceType { return ResourcePollable }
func (*udpIncomingPollable) Drop()              {}
func (p *udpIncomingPollable) Ready() bool {
	return (*UDPSocketResource)(p).incomingPoll.Ready()
}
func (p *udpIncomingPollable) Notify() <-chan struct{} {
	return (*UDPSocketResource)(p).incomingPoll.Notify()
}
func (p *udpIncomingPollable) Block(ctx context.Context) {
	for !p.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}

type udpOutgoingPollable UDPSocketResource

func (*udpOutgoingPollable) Type() ResourceType { return ResourcePollable }
func (*udpOutgoingPollable) Drop()              {}
func (p *udpOutgoingPollable) Ready() bool {
	return (*UDPSocketResource)(p).outgoingPoll.Ready()
}
func (p *udpOutgoingPollable) Notify() <-chan struct{} {
	return (*UDPSocketResource)(p).outgoingPoll.Notify()
}
func (p *udpOutgoingPollable) Block(ctx context.Context) {
	for !p.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}

var (
	_ Pollable       = (*udpIncomingPollable)(nil)
	_ NotifyPollable = (*udpIncomingPollable)(nil)
	_ Pollable       = (*udpOutgoingPollable)(nil)
	_ NotifyPollable = (*udpOutgoingPollable)(nil)
)
