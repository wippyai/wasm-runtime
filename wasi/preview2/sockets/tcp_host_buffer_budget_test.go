package sockets

import (
	"context"
	"net"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestFinishConnectHostBufferDenialClosesExistingSocket(t *testing.T) {
	hostBuffers := preview2.NewHostBufferBudget(2*preview2.DefaultBufferSize - 1)
	socketBudget := preview2.NewSocketBudget(2)
	table := preview2.NewResourceTableWithBudgets(8, socketBudget, hostBuffers)
	defer table.Close()

	local, peer := net.Pipe()
	defer peer.Close()
	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.TCPStateConnectInProgress)
	socket.SetConn(local)
	handle, err := table.TryAdd(socket)
	if err != nil {
		t.Fatal(err)
	}

	_, _, netErr := NewTCPHost(table).MethodTCPSocketFinishConnect(context.Background(), handle)
	if netErr == nil || netErr.Code != NetworkErrorOutOfMemory {
		t.Fatalf("error=%v", netErr)
	}
	if socket.State() != preview2.TCPStateClosed {
		t.Fatalf("state=%v want closed", socket.State())
	}
	if usage := hostBuffers.Usage(); usage.Used != 0 || usage.Peak != preview2.DefaultBufferSize {
		t.Fatalf("host buffer usage=%+v", usage)
	}
}

func TestAcceptHostBufferDenialRollsBackAcceptedSocket(t *testing.T) {
	hostBuffers := preview2.NewHostBufferBudget(2*preview2.DefaultBufferSize - 1)
	socketBudget := preview2.NewSocketBudget(2)
	table := preview2.NewResourceTableWithBudgets(8, socketBudget, hostBuffers)
	defer table.Close()

	ctx := context.Background()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	parent := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	parent.SetState(preview2.TCPStateListening)
	parent.SetListener(listener)
	handle, err := table.TryAdd(parent)
	if err != nil {
		t.Fatal(err)
	}
	client, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	socketHandle, inputHandle, outputHandle, netErr := NewTCPHost(table).MethodTCPSocketAccept(ctx, handle)
	if netErr == nil || netErr.Code != NetworkErrorOutOfMemory {
		t.Fatalf("error=%v", netErr)
	}
	if socketHandle != 0 || inputHandle != 0 || outputHandle != 0 {
		t.Fatalf("failed accept published handles: socket=%d input=%d output=%d", socketHandle, inputHandle, outputHandle)
	}
	if usage := hostBuffers.Usage(); usage.Used != 0 || usage.Peak != preview2.DefaultBufferSize {
		t.Fatalf("host buffer usage=%+v", usage)
	}
	if used := socketBudget.Used(); used != 1 {
		t.Fatalf("accepted socket lease leaked: used=%d", used)
	}
}

func TestAcceptAdmitsDuplexBeforePublishingChildSocket(t *testing.T) {
	hostBuffers := preview2.NewHostBufferBudget(2 * preview2.DefaultBufferSize)
	// The listener owns the only socket slot. A child handle therefore cannot
	// publish, but its full duplex must have been admitted first so failure has
	// a single socket-owned rollback path.
	socketBudget := preview2.NewSocketBudget(1)
	table := preview2.NewResourceTableWithBudgets(8, socketBudget, hostBuffers)
	defer table.Close()

	ctx := context.Background()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	parent := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	parent.SetState(preview2.TCPStateListening)
	parent.SetListener(listener)
	handle, err := table.TryAdd(parent)
	if err != nil {
		t.Fatal(err)
	}
	client, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	socketHandle, inputHandle, outputHandle, netErr := NewTCPHost(table).MethodTCPSocketAccept(ctx, handle)
	if netErr == nil || netErr.Code != NetworkErrorNewSocketLimit {
		t.Fatalf("error=%v", netErr)
	}
	if socketHandle != 0 || inputHandle != 0 || outputHandle != 0 {
		t.Fatalf("failed accept published handles: socket=%d input=%d output=%d", socketHandle, inputHandle, outputHandle)
	}
	if usage := hostBuffers.Usage(); usage.Used != 0 || usage.Peak != 2*preview2.DefaultBufferSize {
		t.Fatalf("duplex was not admitted then rolled back: %+v", usage)
	}
	if used := socketBudget.Used(); used != 1 {
		t.Fatalf("failed child changed parent socket lease count: used=%d", used)
	}
}
