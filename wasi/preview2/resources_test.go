package preview2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResourceTable_AddGetRemove(t *testing.T) {
	table := NewResourceTable()

	// Add a resource
	r := &PollableResource{}
	handle := table.Add(r)

	if handle == 0 {
		t.Error("expected non-zero handle")
	}

	// Get the resource
	got, ok := table.Get(handle)
	if !ok {
		t.Fatal("resource not found")
	}
	if got != r {
		t.Error("got different resource")
	}

	// Remove the resource
	table.Remove(handle)

	// Should not be found after removal
	_, ok = table.Get(handle)
	if ok {
		t.Error("resource should not exist after removal")
	}
}

func TestResourceTable_Clear(t *testing.T) {
	table := NewResourceTable()

	// Add multiple resources
	h1 := table.Add(&PollableResource{})
	h2 := table.Add(&PollableResource{})
	h3 := table.Add(&PollableResource{})

	// Clear all
	table.Clear()

	// None should be found
	_, ok := table.Get(h1)
	if ok {
		t.Error("h1 should not exist after clear")
	}
	_, ok = table.Get(h2)
	if ok {
		t.Error("h2 should not exist after clear")
	}
	_, ok = table.Get(h3)
	if ok {
		t.Error("h3 should not exist after clear")
	}
}

func TestResourceTable_GetInvalid(t *testing.T) {
	table := NewResourceTable()

	_, ok := table.Get(9999)
	if ok {
		t.Error("should not find invalid handle")
	}
}

func TestPollableResource(t *testing.T) {
	p := &PollableResource{}

	if p.Type() != ResourcePollable {
		t.Errorf("expected ResourcePollable, got %d", p.Type())
	}

	// Initially not ready
	if p.Ready() {
		t.Error("should not be ready initially")
	}

	// Set ready
	p.SetReady(true)
	if !p.Ready() {
		t.Error("should be ready after SetReady(true)")
	}

	// Block makes it ready
	p.SetReady(false)
	ctx := context.Background()
	p.Block(ctx)
	if !p.Ready() {
		t.Error("should be ready after Block")
	}

	// Drop should not panic
	p.Drop()
}

func TestTimerPollable(t *testing.T) {
	// Create timer that expires in the past
	past := NewTimerPollable(time.Now().Add(-time.Second))
	if past.Type() != ResourcePollable {
		t.Errorf("expected ResourcePollable, got %d", past.Type())
	}
	if !past.Ready() {
		t.Error("past timer should be ready")
	}

	// Create timer that expires in the future
	future := NewTimerPollable(time.Now().Add(100 * time.Millisecond))
	if future.Ready() {
		t.Error("future timer should not be ready yet")
	}

	// Block until ready
	ctx := context.Background()
	future.Block(ctx)
	if !future.Ready() {
		t.Error("future timer should be ready after Block")
	}

	// Drop should not panic
	future.Drop()
}

func TestTimerPollable_BlockWithCancel(t *testing.T) {
	future := NewTimerPollable(time.Now().Add(10 * time.Second))

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	future.Block(ctx)
	elapsed := time.Since(start)

	// Should have been canceled quickly
	if elapsed > time.Second {
		t.Errorf("Block should have been canceled quickly, took %v", elapsed)
	}
}

func TestInputStreamResource_Bytes(t *testing.T) {
	data := []byte("hello, world!")
	s := NewInputStreamResource(data)

	if s.Type() != ResourceInputStream {
		t.Errorf("expected ResourceInputStream, got %d", s.Type())
	}

	// Read some bytes
	buf, err := s.Read(5)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("expected 'hello', got %q", buf)
	}

	// Read remaining
	buf, err = s.Read(100)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf) != ", world!" {
		t.Errorf("expected ', world!', got %q", buf)
	}

	// Read when empty should return closed error
	_, err = s.Read(10)
	if err == nil {
		t.Error("expected error when reading empty stream")
	}
	se, ok := err.(*StreamError)
	if !ok || !se.Closed {
		t.Error("expected closed stream error")
	}
}

func TestInputStreamResource_Reader(t *testing.T) {
	reader := strings.NewReader("test data from reader")
	s := NewInputStreamResource(reader)

	buf, err := s.Read(4)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf) != "test" {
		t.Errorf("expected 'test', got %q", buf)
	}

	// Read remaining
	buf, err = s.Read(100)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf) != " data from reader" {
		t.Errorf("expected ' data from reader', got %q", buf)
	}

	// Drop should close the reader
	s.Drop()
}

func TestInputStreamResource_ReaderWithCloser(t *testing.T) {
	// Create a reader that implements io.Closer
	reader := io.NopCloser(strings.NewReader("closable reader"))
	s := NewInputStreamResource(reader)

	// Drop should close
	s.Drop()
}

func TestInputStreamResource_MaxAllocation(t *testing.T) {
	data := make([]byte, 1000)
	s := NewInputStreamResource(data)

	// Request more than MaxAllocationSize
	buf, err := s.Read(MaxAllocationSize + 1000)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	// Should have limited to MaxAllocationSize or available data
	if len(buf) > int(MaxAllocationSize) && len(buf) > len(data) {
		t.Errorf("should limit allocation size")
	}
}

func TestOutputStreamResource_Buffer(t *testing.T) {
	s := NewOutputStreamResource(nil)

	if s.Type() != ResourceOutputStream {
		t.Errorf("expected ResourceOutputStream, got %d", s.Type())
	}

	// Write some data
	err := s.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	err = s.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Check bytes
	if string(s.Bytes()) != "hello world" {
		t.Errorf("expected 'hello world', got %q", s.Bytes())
	}

	// Check write capacity
	cap, err := s.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if cap != 65536 {
		t.Errorf("expected capacity 65536, got %d", cap)
	}

	// Drop should not panic
	s.Drop()
}

func TestOutputStreamResource_WithBuffer(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewOutputStreamResource(buf)

	err := s.Write([]byte("test data"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Should write to the buffer
	if buf.String() != "test data" {
		t.Errorf("expected 'test data', got %q", buf.String())
	}

	// Bytes() should return buffer bytes
	if string(s.Bytes()) != "test data" {
		t.Errorf("expected 'test data' from Bytes(), got %q", s.Bytes())
	}
}

func TestOutputStreamResource_Closed(t *testing.T) {
	s := NewOutputStreamResource(nil)
	s.closed = true

	err := s.Write([]byte("test"))
	if err == nil {
		t.Error("expected error writing to closed stream")
	}
	se, ok := err.(*StreamError)
	if !ok || !se.Closed {
		t.Error("expected closed stream error")
	}

	_, err = s.CheckWrite()
	if err == nil {
		t.Error("expected error for CheckWrite on closed stream")
	}
}

func TestFileOutputStreamResource(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")

	s, err := NewFileOutputStreamResource(testFile, 0, false)
	if err != nil {
		t.Fatalf("create file output stream: %v", err)
	}

	if s.Type() != ResourceOutputStream {
		t.Errorf("expected ResourceOutputStream, got %d", s.Type())
	}

	// Write some data
	err = s.Write([]byte("hello file"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Check write capacity
	cap, cErr := s.CheckWrite()
	if cErr != nil {
		t.Fatalf("CheckWrite error: %v", cErr)
	}
	if cap != 65536 {
		t.Errorf("expected capacity 65536, got %d", cap)
	}

	// Flush
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	// Drop
	s.Drop()

	// Verify file contents
	contents, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(contents) != "hello file" {
		t.Errorf("expected 'hello file', got %q", contents)
	}
}

func TestFileOutputStreamResource_Append(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "append.txt")

	// Create initial content
	os.WriteFile(testFile, []byte("initial"), 0644)

	// Open for append
	s, err := NewFileOutputStreamResource(testFile, 0, true)
	if err != nil {
		t.Fatalf("create file output stream: %v", err)
	}

	err = s.Write([]byte(" appended"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	s.Drop()

	// Verify
	contents, _ := os.ReadFile(testFile)
	if string(contents) != "initial appended" {
		t.Errorf("expected 'initial appended', got %q", contents)
	}
}

func TestFileOutputStreamResource_WithOffset(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "offset.txt")

	// Create initial content
	os.WriteFile(testFile, []byte("0123456789"), 0644)

	// Open with offset
	s, err := NewFileOutputStreamResource(testFile, 5, false)
	if err != nil {
		t.Fatalf("create file output stream: %v", err)
	}

	err = s.Write([]byte("XXXXX"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	s.Drop()

	// Verify
	contents, _ := os.ReadFile(testFile)
	if string(contents) != "01234XXXXX" {
		t.Errorf("expected '01234XXXXX', got %q", contents)
	}
}

func TestFileOutputStreamResource_Closed(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "closed.txt")

	s, _ := NewFileOutputStreamResource(testFile, 0, false)
	s.Drop()

	err := s.Write([]byte("test"))
	if err == nil {
		t.Error("expected error writing to closed stream")
	}

	_, err = s.CheckWrite()
	if err == nil {
		t.Error("expected error for CheckWrite on closed stream")
	}

	err = s.Flush()
	if err == nil {
		t.Error("expected error for Flush on closed stream")
	}
}

func TestStreamError(t *testing.T) {
	closed := &StreamError{Closed: true}
	if closed.Error() != "stream closed" {
		t.Errorf("expected 'stream closed', got %q", closed.Error())
	}

	failed := &StreamError{LastOpFailed: true}
	if failed.Error() != "stream error" {
		t.Errorf("expected 'stream error', got %q", failed.Error())
	}
}

func TestErrorResource(t *testing.T) {
	e := NewErrorResource("test error message")

	if e.Type() != ResourceError {
		t.Errorf("expected ResourceError, got %d", e.Type())
	}

	if e.ToDebugString() != "test error message" {
		t.Errorf("expected 'test error message', got %q", e.ToDebugString())
	}

	// Drop should not panic
	e.Drop()
}

func TestDescriptorResource(t *testing.T) {
	d := NewDescriptorResource("/path/to/file", false, true)

	if d.Type() != ResourceDescriptor {
		t.Errorf("expected ResourceDescriptor, got %d", d.Type())
	}

	if d.Path() != "/path/to/file" {
		t.Errorf("expected '/path/to/file', got %q", d.Path())
	}

	if d.IsDir() {
		t.Error("should not be directory")
	}

	if !d.ReadOnly() {
		t.Error("should be read-only")
	}

	// Drop should not panic
	d.Drop()
}

func TestDirectoryEntryStreamResource(t *testing.T) {
	entries := []DirectoryEntry{
		{Type: 4, Name: "file.txt"},
		{Type: 3, Name: "subdir"},
		{Type: 4, Name: "data.json"},
	}

	stream := NewDirectoryEntryStreamResource(entries)

	if stream.Type() != ResourceDirectoryEntryStream {
		t.Errorf("expected ResourceDirectoryEntryStream, got %d", stream.Type())
	}

	// Read all entries
	e := stream.ReadNext()
	if e == nil || e.Name != "file.txt" {
		t.Errorf("expected file.txt, got %v", e)
	}

	e = stream.ReadNext()
	if e == nil || e.Name != "subdir" {
		t.Errorf("expected subdir, got %v", e)
	}

	e = stream.ReadNext()
	if e == nil || e.Name != "data.json" {
		t.Errorf("expected data.json, got %v", e)
	}

	// No more entries
	e = stream.ReadNext()
	if e != nil {
		t.Errorf("expected nil, got %v", e)
	}

	// Drop should not panic
	stream.Drop()
}

func TestNetworkResource(t *testing.T) {
	n := NewNetworkResource()

	if n.Type() != ResourceNetwork {
		t.Errorf("expected ResourceNetwork, got %d", n.Type())
	}

	// Drop should not panic
	n.Drop()
}

func TestTCPSocketResource(t *testing.T) {
	socket := NewTCPSocketResource(4) // IPv4

	if socket.Type() != ResourceTCPSocket {
		t.Errorf("expected ResourceTCPSocket, got %d", socket.Type())
	}

	// Check defaults
	if socket.Family() != 4 {
		t.Errorf("expected family 4, got %d", socket.Family())
	}
	if socket.State() != TCPStateUnbound {
		t.Errorf("expected TCPStateUnbound, got %d", socket.State())
	}
	if socket.HopLimit() != 64 {
		t.Errorf("expected hop limit 64, got %d", socket.HopLimit())
	}
	if socket.ReceiveBufferSize() != 65536 {
		t.Errorf("expected receive buffer 65536, got %d", socket.ReceiveBufferSize())
	}

	// Test state transitions
	socket.SetState(TCPStateBound)
	if socket.State() != TCPStateBound {
		t.Error("state should be bound")
	}

	socket.SetState(TCPStateListening)
	if !socket.IsListening() {
		t.Error("should be listening")
	}

	socket.SetState(TCPStateConnected)
	if !socket.IsConnected() {
		t.Error("should be connected")
	}

	// Test address setters
	socket.SetLocalAddr("127.0.0.1", 8080)
	if socket.LocalAddr() != "127.0.0.1" || socket.LocalPort() != 8080 {
		t.Error("local address not set correctly")
	}

	socket.SetRemoteAddr("192.168.1.1", 443)
	if socket.RemoteAddr() != "192.168.1.1" || socket.RemotePort() != 443 {
		t.Error("remote address not set correctly")
	}

	// Test stream handles
	socket.SetStreamHandles(1, 2)
	in, out := socket.StreamHandles()
	if in != 1 || out != 2 {
		t.Error("stream handles not set correctly")
	}

	// Test pending error
	socket.SetPendingError(io.EOF)
	if !errors.Is(socket.PendingError(), io.EOF) {
		t.Error("pending error not set correctly")
	}
	socket.ClearPendingError()
	if socket.PendingError() != nil {
		t.Error("pending error not cleared")
	}

	// Drop should not panic
	socket.Drop()
	if socket.State() != TCPStateClosed {
		t.Error("should be closed after Drop")
	}
}

func TestUDPSocketResource(t *testing.T) {
	socket := NewUDPSocketResource(6) // IPv6

	if socket.Type() != ResourceUDPSocket {
		t.Errorf("expected ResourceUDPSocket, got %d", socket.Type())
	}

	// Check defaults
	if socket.Family() != 6 {
		t.Errorf("expected family 6, got %d", socket.Family())
	}
	if socket.State() != UDPStateUnbound {
		t.Errorf("expected UDPStateUnbound, got %d", socket.State())
	}
	if socket.UnicastHopLimit() != 64 {
		t.Errorf("expected unicast hop limit 64, got %d", socket.UnicastHopLimit())
	}

	// Test state transitions
	socket.SetState(UDPStateBound)
	if !socket.IsBound() {
		t.Error("should be bound")
	}

	// Test address setters
	socket.SetLocalAddr("::1", 8080)
	if socket.LocalAddr() != "::1" || socket.LocalPort() != 8080 {
		t.Error("local address not set correctly")
	}

	socket.SetRemoteAddr("::ffff:192.168.1.1", 443)
	if socket.RemoteAddr() != "::ffff:192.168.1.1" || socket.RemotePort() != 443 {
		t.Error("remote address not set correctly")
	}

	// Test stream handles
	socket.SetStreamHandles(3, 4)
	in, out := socket.StreamHandles()
	if in != 3 || out != 4 {
		t.Error("stream handles not set correctly")
	}

	// Test options
	socket.SetUnicastHopLimit(128)
	if socket.UnicastHopLimit() != 128 {
		t.Error("unicast hop limit not set correctly")
	}

	socket.SetReceiveBufferSize(131072)
	if socket.ReceiveBufferSize() != 131072 {
		t.Error("receive buffer size not set correctly")
	}

	socket.SetSendBufferSize(65536)
	if socket.SendBufferSize() != 65536 {
		t.Error("send buffer size not set correctly")
	}

	// Test pending error
	socket.SetPendingError(io.EOF)
	if !errors.Is(socket.PendingError(), io.EOF) {
		t.Error("pending error not set correctly")
	}
	socket.ClearPendingError()
	if socket.PendingError() != nil {
		t.Error("pending error not cleared")
	}

	// Drop should not panic
	socket.Drop()
	if socket.State() != UDPStateClosed {
		t.Error("should be closed after Drop")
	}
}

func TestTCPInputStreamResource(t *testing.T) {
	socket := NewTCPSocketResource(4)
	s := NewTCPInputStreamResource(socket)

	if s.Type() != ResourceInputStream {
		t.Errorf("expected ResourceInputStream, got %d", s.Type())
	}

	// Read without connection should fail
	_, err := s.Read(10)
	if err == nil {
		t.Error("expected error reading without connection")
	}

	// Drop should not panic
	s.Drop()

	// Read after drop should fail
	_, err = s.Read(10)
	if err == nil {
		t.Error("expected error reading after drop")
	}
}

func TestTCPOutputStreamResource(t *testing.T) {
	socket := NewTCPSocketResource(4)
	s := NewTCPOutputStreamResource(socket)

	if s.Type() != ResourceOutputStream {
		t.Errorf("expected ResourceOutputStream, got %d", s.Type())
	}

	// Write without connection should fail
	err := s.Write([]byte("test"))
	if err == nil {
		t.Error("expected error writing without connection")
	}

	// CheckWrite without connection should fail
	_, err = s.CheckWrite()
	if err == nil {
		t.Error("expected error for CheckWrite without connection")
	}

	// Drop should not panic
	s.Drop()

	// Write after drop should fail
	err = s.Write([]byte("test"))
	if err == nil {
		t.Error("expected error writing after drop")
	}
}

func TestIncomingDatagramStreamResource(t *testing.T) {
	socket := NewUDPSocketResource(4)
	s := NewIncomingDatagramStreamResource(socket, "", 0)

	if s.Type() != ResourceInputStream {
		t.Errorf("expected ResourceInputStream, got %d", s.Type())
	}

	if s.Socket() != socket {
		t.Error("socket not set correctly")
	}

	// No remote address
	_, _, has := s.RemoteAddr()
	if has {
		t.Error("should not have remote address")
	}

	// With remote address
	s2 := NewIncomingDatagramStreamResource(socket, "192.168.1.1", 5000)
	addr, port, has := s2.RemoteAddr()
	if !has {
		t.Error("should have remote address")
	}
	if addr != "192.168.1.1" || port != 5000 {
		t.Error("remote address not set correctly")
	}

	// Drop should not panic
	s.Drop()
}

func TestOutgoingDatagramStreamResource(t *testing.T) {
	socket := NewUDPSocketResource(4)
	s := NewOutgoingDatagramStreamResource(socket, "", 0)

	if s.Type() != ResourceOutputStream {
		t.Errorf("expected ResourceOutputStream, got %d", s.Type())
	}

	if s.Socket() != socket {
		t.Error("socket not set correctly")
	}

	// No remote address
	_, _, has := s.RemoteAddr()
	if has {
		t.Error("should not have remote address")
	}

	// With remote address
	s2 := NewOutgoingDatagramStreamResource(socket, "10.0.0.1", 3000)
	addr, port, has := s2.RemoteAddr()
	if !has {
		t.Error("should have remote address")
	}
	if addr != "10.0.0.1" || port != 3000 {
		t.Error("remote address not set correctly")
	}

	// Drop should not panic
	s.Drop()
}

func TestResolveAddressStreamResource(t *testing.T) {
	addresses := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	stream := NewResolveAddressStreamResource(addresses)

	if stream.Type() != ResourceIPNameLookup {
		t.Errorf("expected ResourceIPNameLookup, got %d", stream.Type())
	}

	// Read all addresses
	addr := stream.ReadNext()
	if addr == nil || *addr != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %v", addr)
	}

	addr = stream.ReadNext()
	if addr == nil || *addr != "192.168.1.2" {
		t.Errorf("expected 192.168.1.2, got %v", addr)
	}

	addr = stream.ReadNext()
	if addr == nil || *addr != "192.168.1.3" {
		t.Errorf("expected 192.168.1.3, got %v", addr)
	}

	// No more addresses
	addr = stream.ReadNext()
	if addr != nil {
		t.Errorf("expected nil, got %v", addr)
	}

	// Drop should not panic
	stream.Drop()
}

func TestResourceAdapter(t *testing.T) {
	table := NewResourceTable()

	// Test that resource adapter properly wraps and unwraps
	pollable := &PollableResource{ready: true}
	handle := table.Add(pollable)

	got, ok := table.Get(handle)
	if !ok {
		t.Fatal("resource not found")
	}

	p, ok := got.(*PollableResource)
	if !ok {
		t.Fatal("wrong resource type")
	}

	if !p.Ready() {
		t.Error("should be ready")
	}
}
