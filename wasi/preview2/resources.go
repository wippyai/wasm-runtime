package preview2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/wippyai/wasm-runtime/resource"
)

// MaxAllocationSize is the maximum size for single allocations (1 GB) to prevent DoS
const MaxAllocationSize = 1 << 30

// DefaultBufferSize is the default buffer size for streams and sockets (64 KB)
const DefaultBufferSize = 65536

// ResourceTable manages WASI preview2 resource handles.
// It is an adapter over the unified resource.WASITable.
type ResourceTable struct {
	table *resource.WASITable
}

// Resource is a WASI preview2 resource that can be managed by ResourceTable.
type Resource interface {
	// Type returns the resource type identifier.
	Type() ResourceType
	// Drop releases any underlying resources.
	Drop()
}

// ResourceType identifies the type of a WASI resource for type-safe handle management.
type ResourceType uint8

const (
	ResourcePollable ResourceType = iota
	ResourceInputStream
	ResourceOutputStream
	ResourceError
	ResourceDescriptor
	ResourceDirectoryEntryStream
	ResourceNetwork
	ResourceTCPSocket
	ResourceUDPSocket
	ResourceIPNameLookup
	ResourceTerminalInput
	ResourceTerminalOutput
	ResourceFields
)

// NewResourceTable creates a new resource table
func NewResourceTable() *ResourceTable {
	return &ResourceTable{
		table: resource.NewWASITable(),
	}
}

// Add stores a resource and returns a stable handle.
func (t *ResourceTable) Add(r Resource) uint32 {
	return uint32(t.table.Add(&resourceAdapter{r}))
}

// Get returns the resource for a handle, or (nil, false) if invalid.
func (t *ResourceTable) Get(handle uint32) (Resource, bool) {
	res, ok := t.table.Get(resource.Handle(handle))
	if !ok {
		return nil, false
	}
	if adapter, ok := res.(*resourceAdapter); ok {
		return adapter.resource, true
	}
	return nil, false
}

// Remove calls Drop on the resource and removes it from the table.
func (t *ResourceTable) Remove(handle uint32) {
	if res, ok := t.table.Get(resource.Handle(handle)); ok {
		if adapter, ok := res.(*resourceAdapter); ok {
			adapter.resource.Drop()
		}
	}
	t.table.Remove(resource.Handle(handle))
}

// Clear drops and removes all resources. Used during shutdown.
func (t *ResourceTable) Clear() {
	t.table.Clear()
}

// resourceAdapter adapts preview2.Resource to resource.WASIResource
type resourceAdapter struct {
	resource Resource
}

func (a *resourceAdapter) WASIResourceType() resource.WASIResourceType {
	return resource.WASIResourceType(a.resource.Type())
}

// Drop implements resource.Dropper to ensure resource cleanup
func (a *resourceAdapter) Drop() {
	if a.resource != nil {
		a.resource.Drop()
	}
}

// Pollable is the interface for async-ready resources that can be polled.
type Pollable interface {
	Resource
	// Ready returns true if the resource is ready for I/O.
	Ready() bool
	// Block waits until the resource becomes ready or ctx is canceled.
	Block(ctx context.Context)
}

// PollableResource is a basic pollable that can be manually set ready.
type PollableResource struct {
	ready bool
}

func (p *PollableResource) Type() ResourceType { return ResourcePollable }
func (p *PollableResource) Drop()              {}
func (p *PollableResource) Ready() bool        { return p.ready }
func (p *PollableResource) SetReady(r bool)    { p.ready = r }
func (p *PollableResource) Block(ctx context.Context) {
	p.ready = true
}

// TimerPollable implements a time-based pollable that becomes ready at a deadline
type TimerPollable struct {
	deadline time.Time
}

// NewTimerPollable creates a pollable that becomes ready at the specified deadline
func NewTimerPollable(deadline time.Time) *TimerPollable {
	return &TimerPollable{deadline: deadline}
}

func (p *TimerPollable) Type() ResourceType { return ResourcePollable }
func (p *TimerPollable) Drop()              {}
func (p *TimerPollable) Ready() bool        { return time.Now().After(p.deadline) }
func (p *TimerPollable) Block(ctx context.Context) {
	remaining := time.Until(p.deadline)
	if remaining <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(remaining):
	}
}

// InputStreamResource wraps byte data or io.Reader for WASI input streams.
type InputStreamResource struct {
	reader io.Reader
	data   []byte
	offset int
	closed bool
}

func NewInputStreamResource(source interface{}) *InputStreamResource {
	s := &InputStreamResource{}
	switch v := source.(type) {
	case []byte:
		s.data = v
	case io.Reader:
		s.reader = v
	}
	return s
}

func (s *InputStreamResource) Type() ResourceType { return ResourceInputStream }
func (s *InputStreamResource) Drop() {
	if s.reader != nil {
		if closer, ok := s.reader.(io.Closer); ok {
			closer.Close()
		}
	}
}

func (s *InputStreamResource) Read(length uint64) ([]byte, error) {
	if s.closed {
		return nil, &StreamError{Closed: true}
	}
	// Limit allocation to prevent DoS
	if length > MaxAllocationSize {
		length = MaxAllocationSize
	}
	if s.reader != nil {
		buf := make([]byte, length)
		n, err := s.reader.Read(buf)
		if err != nil {
			s.closed = true
			if errors.Is(err, io.EOF) {
				if n > 0 {
					return buf[:n], nil
				}
				return nil, &StreamError{Closed: true}
			}
			return nil, &StreamError{Closed: true}
		}
		return buf[:n], nil
	}
	remaining := len(s.data) - s.offset
	if remaining == 0 {
		s.closed = true
		return nil, &StreamError{Closed: true}
	}
	toRead := int(length)
	if toRead > remaining {
		toRead = remaining
	}
	result := s.data[s.offset : s.offset+toRead]
	s.offset += toRead
	return result, nil
}

// OutputStreamResource wraps a buffer for WASI output streams.
type OutputStreamResource struct {
	bufferPtr *bytes.Buffer
	buf       []byte
	closed    bool
}

func NewOutputStreamResource(dest interface{}) *OutputStreamResource {
	s := &OutputStreamResource{}
	if b, ok := dest.(*bytes.Buffer); ok {
		s.bufferPtr = b
	}
	return s
}

func (s *OutputStreamResource) Type() ResourceType { return ResourceOutputStream }
func (s *OutputStreamResource) Drop()              {}

func (s *OutputStreamResource) Write(data []byte) error {
	if s.closed {
		return &StreamError{Closed: true}
	}
	if s.bufferPtr != nil {
		_, _ = s.bufferPtr.Write(data)
	} else {
		s.buf = append(s.buf, data...)
	}
	return nil
}

func (s *OutputStreamResource) Bytes() []byte {
	if s.bufferPtr != nil {
		return s.bufferPtr.Bytes()
	}
	return s.buf
}

func (s *OutputStreamResource) CheckWrite() (uint64, error) {
	if s.closed {
		return 0, &StreamError{Closed: true}
	}
	return DefaultBufferSize, nil
}

// FileOutputStreamResource implements an output stream that writes to a file
type FileOutputStreamResource struct {
	file   *os.File
	offset int64
	append bool
	closed bool
}

// NewFileOutputStreamResource creates a file output stream
func NewFileOutputStreamResource(path string, offset int64, append bool) (*FileOutputStreamResource, error) {
	flags := os.O_WRONLY | os.O_CREATE
	if append {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(path, flags, 0644) //nolint:gosec // WASI filesystem uses standard file permissions
	if err != nil {
		return nil, err
	}
	if !append && offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &FileOutputStreamResource{file: f, offset: offset, append: append}, nil
}

func (s *FileOutputStreamResource) Type() ResourceType { return ResourceOutputStream }

func (s *FileOutputStreamResource) Drop() {
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
	s.closed = true
}

func (s *FileOutputStreamResource) Write(data []byte) error {
	if s.closed || s.file == nil {
		return &StreamError{Closed: true}
	}
	_, err := s.file.Write(data)
	if err != nil {
		return &StreamError{LastOpFailed: true}
	}
	return nil
}

func (s *FileOutputStreamResource) CheckWrite() (uint64, error) {
	if s.closed || s.file == nil {
		return 0, &StreamError{Closed: true}
	}
	return DefaultBufferSize, nil
}

func (s *FileOutputStreamResource) Flush() error {
	if s.closed || s.file == nil {
		return &StreamError{Closed: true}
	}
	return s.file.Sync()
}

// FieldsResource stores HTTP header fields as key -> []value pairs.
// Shared across wasi:http/types and wasi:http/outgoing-handler.
type FieldsResource struct {
	values map[string][]string
}

func NewFieldsResource() *FieldsResource {
	return &FieldsResource{values: make(map[string][]string)}
}

func (f *FieldsResource) Type() ResourceType { return ResourceFields }
func (f *FieldsResource) Drop()              { f.values = nil }

func (f *FieldsResource) Values() map[string][]string { return f.values }

func (f *FieldsResource) Append(name, value string) {
	f.values[name] = append(f.values[name], value)
}

func (f *FieldsResource) Set(name string, values []string) {
	f.values[name] = values
}

func (f *FieldsResource) Get(name string) []string {
	return f.values[name]
}

func (f *FieldsResource) Delete(name string) {
	delete(f.values, name)
}

func (f *FieldsResource) Has(name string) bool {
	_, ok := f.values[name]
	return ok
}

func (f *FieldsResource) Clone() *FieldsResource {
	c := NewFieldsResource()
	for k, v := range f.values {
		c.values[k] = append([]string{}, v...)
	}
	return c
}

// StreamError represents a WASI stream error with error codes.
type StreamError struct {
	Closed          bool   // Stream is closed
	LastOpFailed    bool   // Previous operation failed
	LastOpFailedErr uint32 // Error code from last failed operation
}

func (e *StreamError) Error() string {
	if e.Closed {
		return "stream closed"
	}
	return "stream error"
}

// ErrorResource holds an error message that can be retrieved via ToDebugString.
type ErrorResource struct {
	msg string
}

func NewErrorResource(msg string) *ErrorResource {
	return &ErrorResource{msg: msg}
}

func (e *ErrorResource) Type() ResourceType    { return ResourceError }
func (e *ErrorResource) Drop()                 {}
func (e *ErrorResource) ToDebugString() string { return e.msg }

// DescriptorResource represents an open file or directory handle.
type DescriptorResource struct {
	path     string
	isDir    bool
	readOnly bool
	position int64
}

func NewDescriptorResource(path string, isDir bool, readOnly bool) *DescriptorResource {
	return &DescriptorResource{
		path:     path,
		isDir:    isDir,
		readOnly: readOnly,
		position: 0,
	}
}

func (d *DescriptorResource) Type() ResourceType  { return ResourceDescriptor }
func (d *DescriptorResource) Drop()               {}
func (d *DescriptorResource) Path() string        { return d.path }
func (d *DescriptorResource) IsDir() bool         { return d.isDir }
func (d *DescriptorResource) ReadOnly() bool      { return d.readOnly }
func (d *DescriptorResource) Position() int64     { return d.position }
func (d *DescriptorResource) SetPosition(p int64) { d.position = p }

// DirectoryEntryStreamResource iterates over directory entries.
type DirectoryEntryStreamResource struct {
	entries []DirectoryEntry
	offset  int
}

// DirectoryEntry represents a single entry in a directory listing.
type DirectoryEntry struct {
	Name string
	Type uint8
}

func NewDirectoryEntryStreamResource(entries []DirectoryEntry) *DirectoryEntryStreamResource {
	return &DirectoryEntryStreamResource{
		entries: entries,
	}
}

func (d *DirectoryEntryStreamResource) Type() ResourceType { return ResourceDirectoryEntryStream }
func (d *DirectoryEntryStreamResource) Drop()              {}
func (d *DirectoryEntryStreamResource) ReadNext() *DirectoryEntry {
	if d.offset >= len(d.entries) {
		return nil
	}
	entry := d.entries[d.offset]
	d.offset++
	return &entry
}

// NetworkResource represents a network instance for socket creation.
type NetworkResource struct{}

func NewNetworkResource() *NetworkResource {
	return &NetworkResource{}
}

func (n *NetworkResource) Type() ResourceType { return ResourceNetwork }
func (n *NetworkResource) Drop()              {}

// TCPState represents the state of a TCP socket
type TCPState uint8

const (
	TCPStateUnbound TCPState = iota
	TCPStateBindInProgress
	TCPStateBound
	TCPStateListenInProgress
	TCPStateListening
	TCPStateConnectInProgress
	TCPStateConnected
	TCPStateClosed
)

// TCPSocketResource represents a TCP socket with full connection lifecycle.
type TCPSocketResource struct {
	listener           interface{}
	pendingErr         error
	conn               interface{}
	localAddr          string
	remoteAddr         string
	keepAliveIdleTime  uint64
	keepAliveInterval  uint64
	receiveBufferSize  uint64
	sendBufferSize     uint64
	listenBacklogSize  uint64
	outputStreamHandle uint32
	inputStreamHandle  uint32
	keepAliveCount     uint32
	remotePort         uint16
	localPort          uint16
	keepAliveEnabled   bool
	hopLimit           uint8
	state              TCPState
	family             uint8
}

func NewTCPSocketResource(family uint8) *TCPSocketResource {
	return &TCPSocketResource{
		family:            family,
		state:             TCPStateUnbound,
		hopLimit:          64,
		receiveBufferSize: DefaultBufferSize,
		sendBufferSize:    DefaultBufferSize,
		listenBacklogSize: 128,
		keepAliveIdleTime: 7200000000000, // 2 hours in ns
		keepAliveInterval: 75000000000,   // 75 seconds in ns
		keepAliveCount:    9,
	}
}

func (s *TCPSocketResource) Type() ResourceType { return ResourceTCPSocket }
func (s *TCPSocketResource) Drop() {
	if s.conn != nil {
		if c, ok := s.conn.(interface{ Close() error }); ok {
			_ = c.Close()
		}
		s.conn = nil
	}
	if s.listener != nil {
		if l, ok := s.listener.(interface{ Close() error }); ok {
			_ = l.Close()
		}
		s.listener = nil
	}
	s.state = TCPStateClosed
}
func (s *TCPSocketResource) Family() uint8           { return s.family }
func (s *TCPSocketResource) State() TCPState         { return s.state }
func (s *TCPSocketResource) SetState(state TCPState) { s.state = state }
func (s *TCPSocketResource) IsListening() bool       { return s.state == TCPStateListening }
func (s *TCPSocketResource) IsConnected() bool       { return s.state == TCPStateConnected }

// LocalAddr returns the local address
func (s *TCPSocketResource) LocalAddr() string  { return s.localAddr }
func (s *TCPSocketResource) LocalPort() uint16  { return s.localPort }
func (s *TCPSocketResource) RemoteAddr() string { return s.remoteAddr }
func (s *TCPSocketResource) RemotePort() uint16 { return s.remotePort }

func (s *TCPSocketResource) SetLocalAddr(addr string, port uint16) {
	s.localAddr = addr
	s.localPort = port
}

func (s *TCPSocketResource) SetRemoteAddr(addr string, port uint16) {
	s.remoteAddr = addr
	s.remotePort = port
}

// Conn returns the underlying connection
func (s *TCPSocketResource) Conn() interface{}         { return s.conn }
func (s *TCPSocketResource) SetConn(conn interface{})  { s.conn = conn }
func (s *TCPSocketResource) Listener() interface{}     { return s.listener }
func (s *TCPSocketResource) SetListener(l interface{}) { s.listener = l }

// PendingError returns the pending error if any
func (s *TCPSocketResource) PendingError() error       { return s.pendingErr }
func (s *TCPSocketResource) SetPendingError(err error) { s.pendingErr = err }
func (s *TCPSocketResource) ClearPendingError()        { s.pendingErr = nil }

// StreamHandles returns the input and output stream handles
func (s *TCPSocketResource) StreamHandles() (uint32, uint32) {
	return s.inputStreamHandle, s.outputStreamHandle
}
func (s *TCPSocketResource) SetStreamHandles(input, output uint32) {
	s.inputStreamHandle = input
	s.outputStreamHandle = output
}

// HopLimit returns the hop limit
func (s *TCPSocketResource) HopLimit() uint8               { return s.hopLimit }
func (s *TCPSocketResource) SetHopLimit(v uint8)           { s.hopLimit = v }
func (s *TCPSocketResource) ReceiveBufferSize() uint64     { return s.receiveBufferSize }
func (s *TCPSocketResource) SetReceiveBufferSize(v uint64) { s.receiveBufferSize = v }
func (s *TCPSocketResource) SendBufferSize() uint64        { return s.sendBufferSize }
func (s *TCPSocketResource) SetSendBufferSize(v uint64)    { s.sendBufferSize = v }
func (s *TCPSocketResource) ListenBacklogSize() uint64     { return s.listenBacklogSize }
func (s *TCPSocketResource) SetListenBacklogSize(v uint64) { s.listenBacklogSize = v }

// KeepAliveEnabled returns whether keep-alive is enabled
func (s *TCPSocketResource) KeepAliveEnabled() bool        { return s.keepAliveEnabled }
func (s *TCPSocketResource) SetKeepAliveEnabled(v bool)    { s.keepAliveEnabled = v }
func (s *TCPSocketResource) KeepAliveIdleTime() uint64     { return s.keepAliveIdleTime }
func (s *TCPSocketResource) SetKeepAliveIdleTime(v uint64) { s.keepAliveIdleTime = v }
func (s *TCPSocketResource) KeepAliveInterval() uint64     { return s.keepAliveInterval }
func (s *TCPSocketResource) SetKeepAliveInterval(v uint64) { s.keepAliveInterval = v }
func (s *TCPSocketResource) KeepAliveCount() uint32        { return s.keepAliveCount }
func (s *TCPSocketResource) SetKeepAliveCount(v uint32)    { s.keepAliveCount = v }

// TCPInputStreamResource wraps a TCP connection for reading.
type TCPInputStreamResource struct {
	socket *TCPSocketResource
	closed bool
}

func NewTCPInputStreamResource(socket *TCPSocketResource) *TCPInputStreamResource {
	return &TCPInputStreamResource{socket: socket}
}

func (s *TCPInputStreamResource) Type() ResourceType { return ResourceInputStream }
func (s *TCPInputStreamResource) Drop() {
	s.closed = true
}

func (s *TCPInputStreamResource) Read(length uint64) ([]byte, error) {
	if s.closed {
		return nil, &StreamError{Closed: true}
	}
	if s.socket == nil || s.socket.conn == nil {
		return nil, &StreamError{Closed: true}
	}
	conn, ok := s.socket.conn.(interface{ Read([]byte) (int, error) })
	if !ok {
		return nil, &StreamError{Closed: true}
	}
	buf := make([]byte, length)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, &StreamError{Closed: true}
	}
	return buf[:n], nil
}

// TCPOutputStreamResource wraps a TCP connection for writing.
type TCPOutputStreamResource struct {
	socket *TCPSocketResource
	closed bool
}

func NewTCPOutputStreamResource(socket *TCPSocketResource) *TCPOutputStreamResource {
	return &TCPOutputStreamResource{socket: socket}
}

func (s *TCPOutputStreamResource) Type() ResourceType { return ResourceOutputStream }
func (s *TCPOutputStreamResource) Drop() {
	s.closed = true
}

func (s *TCPOutputStreamResource) Write(data []byte) error {
	if s.closed {
		return &StreamError{Closed: true}
	}
	if s.socket == nil || s.socket.conn == nil {
		return &StreamError{Closed: true}
	}
	conn, ok := s.socket.conn.(interface{ Write([]byte) (int, error) })
	if !ok {
		return &StreamError{Closed: true}
	}
	_, err := conn.Write(data)
	return err
}

func (s *TCPOutputStreamResource) CheckWrite() (uint64, error) {
	if s.closed || s.socket == nil || s.socket.conn == nil {
		return 0, &StreamError{Closed: true}
	}
	return DefaultBufferSize, nil
}

// UDPState represents the state of a UDP socket
type UDPState uint8

const (
	UDPStateUnbound UDPState = iota
	UDPStateBindInProgress
	UDPStateBound
	UDPStateClosed
)

// UDPSocketResource represents a UDP socket with optional connected mode.
type UDPSocketResource struct {
	pendingErr           error
	conn                 interface{}
	remoteAddr           string
	localAddr            string
	receiveBufferSize    uint64
	sendBufferSize       uint64
	incomingStreamHandle uint32
	outgoingStreamHandle uint32
	localPort            uint16
	remotePort           uint16
	state                UDPState
	unicastHopLimit      uint8
	family               uint8
}

func NewUDPSocketResource(family uint8) *UDPSocketResource {
	return &UDPSocketResource{
		family:            family,
		state:             UDPStateUnbound,
		unicastHopLimit:   64,
		receiveBufferSize: DefaultBufferSize,
		sendBufferSize:    DefaultBufferSize,
	}
}

func (s *UDPSocketResource) Type() ResourceType { return ResourceUDPSocket }
func (s *UDPSocketResource) Drop() {
	if s.conn != nil {
		if c, ok := s.conn.(interface{ Close() error }); ok {
			_ = c.Close()
		}
		s.conn = nil
	}
	s.state = UDPStateClosed
}
func (s *UDPSocketResource) Family() uint8           { return s.family }
func (s *UDPSocketResource) State() UDPState         { return s.state }
func (s *UDPSocketResource) SetState(state UDPState) { s.state = state }
func (s *UDPSocketResource) IsBound() bool           { return s.state == UDPStateBound }

// LocalAddr returns the local address
func (s *UDPSocketResource) LocalAddr() string  { return s.localAddr }
func (s *UDPSocketResource) LocalPort() uint16  { return s.localPort }
func (s *UDPSocketResource) RemoteAddr() string { return s.remoteAddr }
func (s *UDPSocketResource) RemotePort() uint16 { return s.remotePort }

func (s *UDPSocketResource) SetLocalAddr(addr string, port uint16) {
	s.localAddr = addr
	s.localPort = port
}

func (s *UDPSocketResource) SetRemoteAddr(addr string, port uint16) {
	s.remoteAddr = addr
	s.remotePort = port
}

// Conn returns the underlying connection
func (s *UDPSocketResource) Conn() interface{}        { return s.conn }
func (s *UDPSocketResource) SetConn(conn interface{}) { s.conn = conn }

// PendingError returns the pending error if any
func (s *UDPSocketResource) PendingError() error       { return s.pendingErr }
func (s *UDPSocketResource) SetPendingError(err error) { s.pendingErr = err }
func (s *UDPSocketResource) ClearPendingError()        { s.pendingErr = nil }

// StreamHandles returns the incoming and outgoing stream handles
func (s *UDPSocketResource) StreamHandles() (uint32, uint32) {
	return s.incomingStreamHandle, s.outgoingStreamHandle
}
func (s *UDPSocketResource) SetStreamHandles(incoming, outgoing uint32) {
	s.incomingStreamHandle = incoming
	s.outgoingStreamHandle = outgoing
}

// UnicastHopLimit returns the unicast hop limit
func (s *UDPSocketResource) UnicastHopLimit() uint8        { return s.unicastHopLimit }
func (s *UDPSocketResource) SetUnicastHopLimit(v uint8)    { s.unicastHopLimit = v }
func (s *UDPSocketResource) ReceiveBufferSize() uint64     { return s.receiveBufferSize }
func (s *UDPSocketResource) SetReceiveBufferSize(v uint64) { s.receiveBufferSize = v }
func (s *UDPSocketResource) SendBufferSize() uint64        { return s.sendBufferSize }
func (s *UDPSocketResource) SetSendBufferSize(v uint64)    { s.sendBufferSize = v }

// IncomingDatagramStreamResource wraps a UDP socket for receiving datagrams.
type IncomingDatagramStreamResource struct {
	socket *UDPSocketResource
	remote *struct {
		addr string
		port uint16
	}
}

func NewIncomingDatagramStreamResource(socket *UDPSocketResource, remoteAddr string, remotePort uint16) *IncomingDatagramStreamResource {
	r := &IncomingDatagramStreamResource{socket: socket}
	if remoteAddr != "" {
		r.remote = &struct {
			addr string
			port uint16
		}{remoteAddr, remotePort}
	}
	return r
}

func (s *IncomingDatagramStreamResource) Type() ResourceType         { return ResourceInputStream }
func (s *IncomingDatagramStreamResource) Drop()                      {}
func (s *IncomingDatagramStreamResource) Socket() *UDPSocketResource { return s.socket }
func (s *IncomingDatagramStreamResource) RemoteAddr() (string, uint16, bool) {
	if s.remote == nil {
		return "", 0, false
	}
	return s.remote.addr, s.remote.port, true
}

// OutgoingDatagramStreamResource wraps a UDP socket for sending datagrams.
type OutgoingDatagramStreamResource struct {
	socket *UDPSocketResource
	remote *struct {
		addr string
		port uint16
	}
}

func NewOutgoingDatagramStreamResource(socket *UDPSocketResource, remoteAddr string, remotePort uint16) *OutgoingDatagramStreamResource {
	r := &OutgoingDatagramStreamResource{socket: socket}
	if remoteAddr != "" {
		r.remote = &struct {
			addr string
			port uint16
		}{remoteAddr, remotePort}
	}
	return r
}

func (s *OutgoingDatagramStreamResource) Type() ResourceType         { return ResourceOutputStream }
func (s *OutgoingDatagramStreamResource) Drop()                      {}
func (s *OutgoingDatagramStreamResource) Socket() *UDPSocketResource { return s.socket }
func (s *OutgoingDatagramStreamResource) RemoteAddr() (string, uint16, bool) {
	if s.remote == nil {
		return "", 0, false
	}
	return s.remote.addr, s.remote.port, true
}

// ResolveAddressStreamResource iterates over DNS resolution results.
type ResolveAddressStreamResource struct {
	addresses []string
	offset    int
}

func NewResolveAddressStreamResource(addresses []string) *ResolveAddressStreamResource {
	return &ResolveAddressStreamResource{
		addresses: addresses,
	}
}

func (r *ResolveAddressStreamResource) Type() ResourceType { return ResourceIPNameLookup }
func (r *ResolveAddressStreamResource) Drop()              {}
func (r *ResolveAddressStreamResource) ReadNext() *string {
	if r.offset >= len(r.addresses) {
		return nil
	}
	addr := r.addresses[r.offset]
	r.offset++
	return &addr
}
