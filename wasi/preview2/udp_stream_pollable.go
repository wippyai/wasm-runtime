package preview2

import "context"

// Wake subscriptions to recheck a stream's lifetime without changing the
// socket's readiness. Other subscriptions may wake but never see false readiness.
func wakeUDPReadiness(p *PollableResource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.readyCh != nil {
		close(p.readyCh)
		p.readyCh = nil
	}
}

func (s *IncomingDatagramStreamResource) Pollable() Pollable {
	if s.socket != nil {
		s.socket.IncomingPollable()
	}
	return (*udpIncomingStreamPollable)(s)
}
func (s *OutgoingDatagramStreamResource) Pollable() Pollable {
	if s.socket != nil {
		s.socket.OutgoingPollable()
	}
	return (*udpOutgoingStreamPollable)(s)
}

type udpIncomingStreamPollable IncomingDatagramStreamResource

func (*udpIncomingStreamPollable) Type() ResourceType { return ResourcePollable }
func (*udpIncomingStreamPollable) Drop()              {}
func (p *udpIncomingStreamPollable) Ready() bool {
	return p.dropped.Load() || p.socket == nil || p.socket.incomingPoll.Ready()
}
func (p *udpIncomingStreamPollable) Notify() <-chan struct{} {
	if p.socket == nil {
		return closedPollableChan
	}
	ch := p.socket.incomingPoll.Notify()
	if p.dropped.Load() {
		return closedPollableChan
	}
	return ch
}
func (p *udpIncomingStreamPollable) Block(ctx context.Context) { blockUDPStream(ctx, p) }

type udpOutgoingStreamPollable OutgoingDatagramStreamResource

func (*udpOutgoingStreamPollable) Type() ResourceType { return ResourcePollable }
func (*udpOutgoingStreamPollable) Drop()              {}
func (p *udpOutgoingStreamPollable) Ready() bool {
	return p.dropped.Load() || p.socket == nil || p.socket.outgoingPoll.Ready()
}
func (p *udpOutgoingStreamPollable) Notify() <-chan struct{} {
	if p.socket == nil {
		return closedPollableChan
	}
	ch := p.socket.outgoingPoll.Notify()
	if p.dropped.Load() {
		return closedPollableChan
	}
	return ch
}
func (p *udpOutgoingStreamPollable) Block(ctx context.Context) { blockUDPStream(ctx, p) }

func blockUDPStream(ctx context.Context, p interface {
	Ready() bool
	Notify() <-chan struct{}
}) {
	for !p.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}
