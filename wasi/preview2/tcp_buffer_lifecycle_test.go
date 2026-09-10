package preview2

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type noProgressReadConn struct {
	net.Conn
	calls atomic.Int32
}

func (c *noProgressReadConn) Read([]byte) (int, error) { c.calls.Add(1); return 0, nil }

type invalidWriteConn struct{ net.Conn }

func (*invalidWriteConn) Write([]byte) (int, error) { return -1, nil }

func TestTCPBuffersStopInvalidNetworkProgress(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	conn := &noProgressReadConn{Conn: left}
	input := newTCPInputBuffer(conn, 8)
	select {
	case <-input.doneCh:
	case <-time.After(time.Second):
		t.Fatal("zero-byte reader spins indefinitely")
	}
	if conn.calls.Load() != 100 {
		t.Fatalf("read retries=%d", conn.calls.Load())
	}
	if _, err := input.Read(8); err == nil {
		t.Fatal("missing no-progress stream failure")
	}
	output := newTCPOutputBuffer(&invalidWriteConn{Conn: left}, 8)
	if _, err := output.CheckWrite(); err != nil {
		t.Fatal(err)
	}
	if err := output.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-output.doneCh:
	case <-time.After(time.Second):
		t.Fatal("invalid writer count spins indefinitely")
	}
	if _, err := output.CheckWrite(); err == nil {
		t.Fatal("missing invalid-count stream failure")
	}
}

func TestTCPSocketDropJoinsBothPumpsAndRejectsLateConnection(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	socket := NewTCPSocketResource(4)
	socket.SetConn(left)
	input := NewTCPInputStreamResource(socket)
	output := NewTCPOutputStreamResource(socket)
	if _, err := output.CheckWrite(); err != nil {
		t.Fatal(err)
	}
	if err := output.Write([]byte("pending")); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { socket.Drop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("socket close did not join pumps")
	}
	for _, closed := range []chan struct{}{input.buffer.doneCh, output.buffer.doneCh} {
		select {
		case <-closed:
		default:
			t.Fatal("socket released before pump exit")
		}
	}
	if input.buffer.buf != nil || output.buffer.buf != nil {
		t.Fatal("closed socket retains ring storage")
	}
	late, peer := net.Pipe()
	defer peer.Close()
	socket.SetConn(late)
	socket.SetState(TCPStateConnected)
	if socket.Conn() != nil || socket.State() != TCPStateClosed {
		t.Fatal("closed socket revived")
	}
	if _, err := peer.Write([]byte("closed")); err == nil {
		t.Fatal("late connection was not closed")
	}
}
