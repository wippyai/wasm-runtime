package sockets

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestInstanceNetworkHost_InstanceNetwork(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewInstanceNetworkHost(resources)
	ctx := context.Background()

	handle := host.InstanceNetwork(ctx)

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("network not in resource table")
	}

	if _, ok := r.(*preview2.NetworkResource); !ok {
		t.Error("resource is not a NetworkResource")
	}
}

func TestInstanceNetworkHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewInstanceNetworkHost(resources)

	ns := host.Namespace()
	expected := "wasi:sockets/instance-network@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestTCPCreateSocketHost_CreateTCPSocket(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPCreateSocketHost(resources)
	ctx := context.Background()

	handle, err := host.CreateTCPSocket(ctx, AddressFamilyIPv4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("socket not in resource table")
	}

	socket, ok := r.(*preview2.TCPSocketResource)
	if !ok {
		t.Fatal("resource is not a TCPSocketResource")
	}

	if socket.Family() != AddressFamilyIPv4 {
		t.Errorf("expected IPv4, got %d", socket.Family())
	}
}

func TestTCPCreateSocketHost_CreateTCPSocket_IPv6(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPCreateSocketHost(resources)
	ctx := context.Background()

	handle, err := host.CreateTCPSocket(ctx, AddressFamilyIPv6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("socket not in resource table")
	}

	socket, ok := r.(*preview2.TCPSocketResource)
	if !ok {
		t.Fatal("resource is not a TCPSocketResource")
	}

	if socket.Family() != AddressFamilyIPv6 {
		t.Errorf("expected IPv6, got %d", socket.Family())
	}
}

func TestTCPCreateSocketHost_InvalidFamily(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPCreateSocketHost(resources)
	ctx := context.Background()

	_, err := host.CreateTCPSocket(ctx, 99)
	if err == nil {
		t.Fatal("expected error for invalid address family")
	}
	if err.Code != NetworkErrorInvalidArgument {
		t.Errorf("expected InvalidArgument, got %d", err.Code)
	}
}

func TestTCPCreateSocketHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPCreateSocketHost(resources)

	ns := host.Namespace()
	expected := "wasi:sockets/tcp-create-socket@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestTCPHost_MethodTCPSocketAddressFamily(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	ctx := context.Background()

	socket := preview2.NewTCPSocketResource(AddressFamilyIPv6)
	handle := resources.Add(socket)

	family, err := host.MethodTCPSocketAddressFamily(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if family != AddressFamilyIPv6 {
		t.Errorf("expected IPv6, got %d", family)
	}
}

func TestTCPHost_MethodTCPSocketIsListening(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	ctx := context.Background()

	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	handle := resources.Add(socket)

	listening := host.MethodTCPSocketIsListening(ctx, handle)
	if listening {
		t.Error("new socket should not be listening")
	}
}

func TestTCPHost_MethodTCPSocketSubscribe(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	ctx := context.Background()

	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	handle := resources.Add(socket)

	pollableHandle := host.MethodTCPSocketSubscribe(ctx, handle)

	r, ok := resources.Get(pollableHandle)
	if !ok {
		t.Fatal("pollable not in resource table")
	}
	if _, ok := r.(*preview2.PollableResource); !ok {
		t.Error("resource is not a PollableResource")
	}
}

func TestTCPHost_SocketOptions(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	ctx := context.Background()

	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	handle := resources.Add(socket)

	hopLimit, err := host.MethodTCPSocketHopLimit(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hopLimit != 64 {
		t.Errorf("expected hop limit 64, got %d", hopLimit)
	}

	recvBuf, err := host.MethodTCPSocketReceiveBufferSize(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recvBuf == 0 {
		t.Error("expected non-zero receive buffer size")
	}

	sendBuf, err := host.MethodTCPSocketSendBufferSize(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sendBuf == 0 {
		t.Error("expected non-zero send buffer size")
	}
}

func TestTCPHost_KeepAliveOptions(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	ctx := context.Background()

	socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	handle := resources.Add(socket)

	enabled, err := host.MethodTCPSocketKeepAliveEnabled(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("keep-alive should be disabled by default")
	}

	count, err := host.MethodTCPSocketKeepAliveCount(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count == 0 {
		t.Error("keep-alive count should be non-zero")
	}
}

func TestTCPHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)

	ns := host.Namespace()
	expected := "wasi:sockets/tcp@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestUDPCreateSocketHost_CreateUDPSocket(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPCreateSocketHost(resources)
	ctx := context.Background()

	handle, err := host.CreateUDPSocket(ctx, AddressFamilyIPv4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("socket not in resource table")
	}

	socket, ok := r.(*preview2.UDPSocketResource)
	if !ok {
		t.Fatal("resource is not a UDPSocketResource")
	}

	if socket.Family() != AddressFamilyIPv4 {
		t.Errorf("expected IPv4, got %d", socket.Family())
	}
}

func TestUDPCreateSocketHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPCreateSocketHost(resources)

	ns := host.Namespace()
	expected := "wasi:sockets/udp-create-socket@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestUDPHost_MethodUDPSocketAddressFamily(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)
	ctx := context.Background()

	socket := preview2.NewUDPSocketResource(AddressFamilyIPv6)
	handle := resources.Add(socket)

	family, err := host.MethodUDPSocketAddressFamily(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if family != AddressFamilyIPv6 {
		t.Errorf("expected IPv6, got %d", family)
	}
}

func TestUDPHost_SocketOptions(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)
	ctx := context.Background()

	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	handle := resources.Add(socket)

	hopLimit, err := host.MethodUDPSocketUnicastHopLimit(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hopLimit != 64 {
		t.Errorf("expected hop limit 64, got %d", hopLimit)
	}

	recvBuf, err := host.MethodUDPSocketReceiveBufferSize(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recvBuf == 0 {
		t.Error("expected non-zero receive buffer size")
	}
}

func TestUDPHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)

	ns := host.Namespace()
	expected := "wasi:sockets/udp@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestIPNameLookupHost_ResolveAddresses(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewIPNameLookupHost(resources)
	ctx := context.Background()

	network := preview2.NewNetworkResource()
	networkHandle := resources.Add(network)

	streamHandle, err := host.ResolveAddresses(ctx, networkHandle, "localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := resources.Get(streamHandle)
	if !ok {
		t.Fatal("resolve stream not in resource table")
	}

	_, ok = r.(*preview2.ResolveAddressStreamResource)
	if !ok {
		t.Fatal("resource is not a ResolveAddressStreamResource")
	}

	addr, err := host.MethodResolveAddressStreamResolveNextAddress(ctx, streamHandle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr == nil {
		t.Fatal("expected at least one address")
	}
	if addr.Address == "" {
		t.Error("address should not be empty")
	}
}

func TestIPNameLookupHost_ResolveAddresses_Invalid(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewIPNameLookupHost(resources)
	ctx := context.Background()

	network := preview2.NewNetworkResource()
	networkHandle := resources.Add(network)

	_, err := host.ResolveAddresses(ctx, networkHandle, "invalid-hostname-that-does-not-exist.local")
	if err == nil {
		t.Fatal("expected error for invalid hostname")
	}
	if err.Code != NetworkErrorNameUnresolvable {
		t.Errorf("expected NameUnresolvable, got %d", err.Code)
	}
}

func TestIPNameLookupHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewIPNameLookupHost(resources)

	ns := host.Namespace()
	expected := "wasi:sockets/ip-name-lookup@0.2.0"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestTCPHost_InvalidHandle(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTCPHost(resources)
	ctx := context.Background()

	_, err := host.MethodTCPSocketAddressFamily(ctx, 9999)
	if err == nil {
		t.Fatal("expected error for invalid handle")
	}
	if err.Code != NetworkErrorInvalidArgument {
		t.Errorf("expected InvalidArgument, got %d", err.Code)
	}
}

func TestUDPHost_InvalidHandle(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)
	ctx := context.Background()

	_, err := host.MethodUDPSocketAddressFamily(ctx, 9999)
	if err == nil {
		t.Fatal("expected error for invalid handle")
	}
	if err.Code != NetworkErrorInvalidArgument {
		t.Errorf("expected InvalidArgument, got %d", err.Code)
	}
}

func TestTCPHost_BindListenAccept(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPHost(resources)
	createHost := NewTCPCreateSocketHost(resources)
	ctx := context.Background()

	// Create server socket
	serverHandle, err := createHost.CreateTCPSocket(ctx, AddressFamilyIPv4)
	if err != nil {
		t.Fatalf("create socket: %v", err)
	}

	// Bind to localhost
	bindErr := tcpHost.MethodTCPSocketStartBind(ctx, serverHandle, 0, IPSocketAddress{
		Address: "127.0.0.1",
		Port:    0, // Let OS pick port
	})
	if bindErr != nil {
		t.Fatalf("start-bind: %v", bindErr)
	}

	bindErr = tcpHost.MethodTCPSocketFinishBind(ctx, serverHandle)
	if bindErr != nil {
		t.Fatalf("finish-bind: %v", bindErr)
	}

	// Start listening
	listenErr := tcpHost.MethodTCPSocketStartListen(ctx, serverHandle)
	if listenErr != nil {
		t.Fatalf("start-listen: %v", listenErr)
	}

	// Wait for listener to be ready (async operation needs time)
	for i := 0; i < 100; i++ {
		time.Sleep(time.Millisecond)
		listenErr = tcpHost.MethodTCPSocketFinishListen(ctx, serverHandle)
		if listenErr == nil {
			break
		}
		if listenErr.Code != NetworkErrorWouldBlock {
			t.Fatalf("finish-listen: %v", listenErr)
		}
	}

	if !tcpHost.MethodTCPSocketIsListening(ctx, serverHandle) {
		t.Error("socket should be listening")
	}

	// Get local address
	localAddr, addrErr := tcpHost.MethodTCPSocketLocalAddress(ctx, serverHandle)
	if addrErr != nil {
		t.Fatalf("local-address: %v", addrErr)
	}
	t.Logf("Server listening on %s:%d", localAddr.Address, localAddr.Port)
}

func TestTCPHost_ConnectToServer(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPHost(resources)
	createHost := NewTCPCreateSocketHost(resources)
	ctx := context.Background()

	// Create and start server
	serverHandle, _ := createHost.CreateTCPSocket(ctx, AddressFamilyIPv4)
	tcpHost.MethodTCPSocketStartBind(ctx, serverHandle, 0, IPSocketAddress{Address: "127.0.0.1", Port: 0})
	tcpHost.MethodTCPSocketFinishBind(ctx, serverHandle)
	tcpHost.MethodTCPSocketStartListen(ctx, serverHandle)

	for i := 0; i < 100; i++ {
		time.Sleep(time.Millisecond)
		if tcpHost.MethodTCPSocketFinishListen(ctx, serverHandle) == nil {
			break
		}
	}

	localAddr, _ := tcpHost.MethodTCPSocketLocalAddress(ctx, serverHandle)

	// Create client socket
	clientHandle, err := createHost.CreateTCPSocket(ctx, AddressFamilyIPv4)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// Connect to server
	connErr := tcpHost.MethodTCPSocketStartConnect(ctx, clientHandle, 0, IPSocketAddress{
		Address: localAddr.Address,
		Port:    localAddr.Port,
	})
	if connErr != nil {
		t.Fatalf("start-connect: %v", connErr)
	}

	// Wait for connection
	var inputHandle, outputHandle uint32
	for i := 0; i < 100; i++ {
		time.Sleep(time.Millisecond)
		inputHandle, outputHandle, connErr = tcpHost.MethodTCPSocketFinishConnect(ctx, clientHandle)
		if connErr == nil {
			break
		}
		if connErr.Code != NetworkErrorWouldBlock {
			t.Fatalf("finish-connect: %v", connErr)
		}
	}

	if inputHandle == 0 || outputHandle == 0 {
		t.Error("expected valid stream handles")
	}

	// Verify remote address
	remoteAddr, addrErr := tcpHost.MethodTCPSocketRemoteAddress(ctx, clientHandle)
	if addrErr != nil {
		t.Fatalf("remote-address: %v", addrErr)
	}
	if remoteAddr.Port != localAddr.Port {
		t.Errorf("expected port %d, got %d", localAddr.Port, remoteAddr.Port)
	}

	t.Logf("Client connected to %s:%d", remoteAddr.Address, remoteAddr.Port)
}

func TestTCPHost_Shutdown(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPHost(resources)
	createHost := NewTCPCreateSocketHost(resources)
	ctx := context.Background()

	// Create server
	serverHandle, _ := createHost.CreateTCPSocket(ctx, AddressFamilyIPv4)
	tcpHost.MethodTCPSocketStartBind(ctx, serverHandle, 0, IPSocketAddress{Address: "127.0.0.1", Port: 0})
	tcpHost.MethodTCPSocketFinishBind(ctx, serverHandle)
	tcpHost.MethodTCPSocketStartListen(ctx, serverHandle)
	for i := 0; i < 100; i++ {
		time.Sleep(time.Millisecond)
		if tcpHost.MethodTCPSocketFinishListen(ctx, serverHandle) == nil {
			break
		}
	}
	localAddr, _ := tcpHost.MethodTCPSocketLocalAddress(ctx, serverHandle)

	// Create and connect client
	clientHandle, _ := createHost.CreateTCPSocket(ctx, AddressFamilyIPv4)
	tcpHost.MethodTCPSocketStartConnect(ctx, clientHandle, 0, IPSocketAddress{
		Address: localAddr.Address,
		Port:    localAddr.Port,
	})
	for i := 0; i < 100; i++ {
		time.Sleep(time.Millisecond)
		_, _, err := tcpHost.MethodTCPSocketFinishConnect(ctx, clientHandle)
		if err == nil {
			break
		}
	}

	// Shutdown write side
	shutdownErr := tcpHost.MethodTCPSocketShutdown(ctx, clientHandle, 1)
	if shutdownErr != nil {
		t.Fatalf("shutdown: %v", shutdownErr)
	}
}
