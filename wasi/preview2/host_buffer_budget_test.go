package preview2

import (
	"errors"
	"net"
	"testing"
)

func connectedTCPSocket(t *testing.T) (*TCPSocketResource, net.Conn) {
	t.Helper()
	local, peer := net.Pipe()
	socket := NewTCPSocketResource(0)
	socket.SetState(TCPStateConnected)
	socket.SetConn(local)
	return socket, peer
}

func TestTCPDuplexHostBufferAdmissionPreflightsBothRings(t *testing.T) {
	hostBuffers := NewHostBufferBudget(2*DefaultBufferSize - 1)
	table := NewResourceTableWithBudgets(8, NewSocketBudget(2), hostBuffers)
	socket, peer := connectedTCPSocket(t)
	defer peer.Close()
	defer socket.Drop()

	input, output, err := table.NewTCPDuplexStreams(socket)
	if !errors.Is(err, ErrHostBufferLimit) || input != nil || output != nil {
		t.Fatalf("input=%v output=%v err=%v", input, output, err)
	}
	if usage := hostBuffers.Usage(); usage.Used != 0 || usage.Peak != DefaultBufferSize {
		t.Fatalf("preflight leaked or did not roll back: %+v", usage)
	}
	if socket.input != nil || socket.output != nil || socket.duplexCharge != nil {
		t.Fatal("denied duplex admission published socket streams")
	}
}

func TestTCPDuplexHostBufferSocketOwnsReleaseAfterPumpJoin(t *testing.T) {
	hostBuffers := NewHostBufferBudget(2 * DefaultBufferSize)
	table := NewResourceTableWithBudgets(8, NewSocketBudget(2), hostBuffers)
	socket, peer := connectedTCPSocket(t)
	defer peer.Close()

	input, output, err := table.NewTCPDuplexStreams(socket)
	if err != nil {
		t.Fatal(err)
	}
	if usage := hostBuffers.Usage(); usage.Used != 2*DefaultBufferSize {
		t.Fatalf("admitted usage=%+v", usage)
	}

	// A stream handle may be dropped first, but its socket remains the sole
	// owner of the duplex reservation until socket teardown joins both pumps.
	input.Drop()
	input.buffer.WaitClosed()
	if usage := hostBuffers.Usage(); usage.Used != 2*DefaultBufferSize {
		t.Fatalf("stream drop released socket-owned reservation: %+v", usage)
	}

	socket.Drop()
	select {
	case <-output.buffer.doneCh:
	default:
		t.Fatal("socket returned before output pump joined")
	}
	if usage := hostBuffers.Usage(); usage.Used != 0 || usage.Peak != 2*DefaultBufferSize {
		t.Fatalf("socket teardown did not release exactly once: %+v", usage)
	}
	socket.Drop()
	if usage := hostBuffers.Usage(); usage.Used != 0 {
		t.Fatalf("repeated teardown changed usage: %+v", usage)
	}
}

func TestTCPDuplexHostBufferIsOptIn(t *testing.T) {
	table := NewResourceTableWithLimits(8, 2)
	socket, peer := connectedTCPSocket(t)
	defer peer.Close()
	defer socket.Drop()
	if _, _, err := table.NewTCPDuplexStreams(socket); err != nil {
		t.Fatal(err)
	}
	if table.HostBufferBudget() != nil {
		t.Fatal("legacy resource table gained an implicit host-buffer budget")
	}
}
