package preview2

import (
	"testing"
)

func TestUDPStreamDropWakesOnlyItsReadiness(t *testing.T) {
	conn := newFakeUDPConn()
	socket := newUDPSocketWithConn(conn)
	defer socket.Drop()
	first := NewIncomingDatagramStreamResource(socket, "", 0)
	second := NewIncomingDatagramStreamResource(socket, "", 0)
	p := first.Pollable().(NotifyPollable)
	other := second.Pollable()
	if p.Ready() {
		t.Fatal("incoming pollable unexpectedly ready")
	}
	notified := p.Notify()
	p.Drop() // Subscription drop borrows the stream and socket.
	if p.Ready() {
		t.Fatal("incoming pollable unexpectedly ready")
	}
	first.Drop()
	select {
	case <-notified:
	default:
		t.Fatal("stream drop did not wake waiter")
	}
	if !p.Ready() {
		t.Fatal("dropped stream not ready")
	}
	if other.Ready() {
		t.Fatal("another stream inherited fabricated readiness")
	}
	if socket.Conn() == nil {
		t.Fatal("stream drop closed socket")
	}
}

func TestUDPSetConnCannotReplaceRunningPumps(t *testing.T) {
	original := newFakeUDPConn()
	socket := newUDPSocketWithConn(original)
	defer socket.Drop()
	socket.IncomingPollable()
	replacement := newFakeUDPConn()
	socket.SetConn(replacement)
	if socket.Conn() != original {
		t.Fatal("replaced active connection")
	}
	select {
	case <-replacement.closed:
	default:
		t.Fatal("rejected replacement was not closed")
	}
	socket.SetConn(original)
	select {
	case <-original.closed:
		t.Fatal("reassigning same connection closed active socket")
	default:
	}
}

func TestUDPOutgoingStreamDropWakesFullQueue(t *testing.T) {
	conn := newFakeUDPConn()
	conn.blockWrites()
	socket := newUDPSocketWithConn(conn)
	defer socket.Drop()
	first := NewOutgoingDatagramStreamResource(socket, "", 0)
	second := NewOutgoingDatagramStreamResource(socket, "", 0)
	p := first.Pollable().(NotifyPollable)
	other := second.Pollable()
	packets := make([]UDPDatagram, UDPMaxQueuedDatagrams)
	count, err := socket.SendDatagrams(packets)
	if err != nil || count != UDPMaxQueuedDatagrams {
		t.Fatalf("fill queue: count=%d err=%v", count, err)
	}
	if p.Ready() {
		t.Fatal("full outgoing queue unexpectedly ready")
	}
	notified := p.Notify()
	first.Drop()
	select {
	case <-notified:
	default:
		t.Fatal("outgoing stream drop did not wake waiter")
	}
	if !p.Ready() || other.Ready() {
		t.Fatal("drop must make only the dropped stream ready")
	}
}
