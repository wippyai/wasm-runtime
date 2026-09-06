package preview2

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestUDPBindPendingAndTransfer(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	socket := NewUDPSocketResource(0)
	defer socket.Drop()
	socket.SetState(UDPStateBindInProgress)
	op := newFakeOperation(conn, nil, false)
	if err := socket.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}
	if ready, err := socket.ResolvePendingBind(); ready || err != nil {
		t.Fatalf("pending bind: %v %v", ready, err)
	}
	if socket.Subscribe().Ready() {
		t.Fatal("pending bind reported ready")
	}
	op.setReady(true)
	if !socket.Subscribe().Ready() {
		t.Fatal("completed bind not ready")
	}
	if ready, err := socket.ResolvePendingBind(); !ready || err != nil {
		t.Fatalf("completed bind: %v %v", ready, err)
	}
	if socket.Conn() != conn || socket.LocalPort() == 0 {
		t.Fatal("connection/address not adopted")
	}
	if err := conn.SetDeadline(time.Now()); err != nil {
		t.Fatal("pending cleanup closed transferred socket:", err)
	}
}

func TestUDPBindDropJoinsPendingCleanup(t *testing.T) {
	socket := NewUDPSocketResource(0)
	socket.SetState(UDPStateBindInProgress)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	op := newFakeOperation(nil, nil, false)
	op.closeHook = func() { close(entered); <-release }
	if err := socket.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}
	go func() { socket.Drop(); close(done) }()
	<-entered
	select {
	case <-done:
		t.Fatal("Drop returned before bind cleanup")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Drop did not join")
	}
	if socket.PendingOperation() != nil {
		t.Fatal("Drop retained bind operation")
	}
}

func TestUDPBindRejectsWrongAndNilResults(t *testing.T) {
	for _, typedNil := range []bool{false, true} {
		socket := NewUDPSocketResource(0)
		socket.SetState(UDPStateBindInProgress)
		wrong := &fakeTrackedCloser{}
		op := newFakeOperation(wrong, nil, true)
		if typedNil {
			var conn *net.UDPConn
			op = newFakeOperation(conn, nil, true)
		}
		if err := socket.SetPendingOperation(op); err != nil {
			t.Fatal(err)
		}
		ready, err := socket.ResolvePendingBind()
		if ready || err == nil {
			t.Fatalf("bad result accepted: %v %v", ready, err)
		}
		if !typedNil && wrong.closeCount.Load() != 1 {
			t.Fatal("wrong result not disposed")
		}
		if socket.Conn() != nil {
			t.Fatal("bad result installed")
		}
		socket.Drop()
	}
	socket := NewUDPSocketResource(0)
	socket.SetState(UDPStateBindInProgress)
	expected := errors.New("bind failed")
	op := newFakeOperation(nil, expected, true)
	if err := socket.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}
	if _, err := socket.ResolvePendingBind(); !errors.Is(err, expected) {
		t.Fatal("bind error lost:", err)
	}
	socket.Drop()
}
