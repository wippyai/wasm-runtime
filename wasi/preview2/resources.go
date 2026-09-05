package preview2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
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
	table  *resource.WASITable
	budget *resourceBudget
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
	handle, err := t.TryAdd(r)
	if err != nil {
		r.Drop()
		panic(err)
	}
	return handle
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
	t.table.Remove(resource.Handle(handle))
}

// Clear drops and removes all resources. Used during shutdown.
func (t *ResourceTable) Clear() {
	t.table.Clear()
}

// Close drops and removes all resources.
func (t *ResourceTable) Close() error {
	return t.table.Close()
}

// SocketBudget returns the socket budget associated with this table, if bounded.
func (t *ResourceTable) SocketBudget() *SocketBudget {
	if t == nil || t.budget == nil {
		return nil
	}
	return t.budget.socketBudget
}

// resourceAdapter adapts preview2.Resource to resource.WASIResource
type resourceAdapter struct {
	resource Resource
	budget   *resourceBudget
	lease    *SocketLease
	once     sync.Once
}

func (a *resourceAdapter) WASIResourceType() resource.WASIResourceType {
	return resource.WASIResourceType(a.resource.Type())
}

// Drop implements resource.Dropper to ensure resource cleanup
func (a *resourceAdapter) Drop() {
	a.once.Do(func() {
		// Keep reservations until the underlying resources actually close.
		// These fields remain immutable for concurrent Get/type inspection.
		defer a.lease.releaseTable()
		if a.budget != nil {
			defer a.budget.releaseHandle()
		}
		if a.resource != nil {
			a.resource.Drop()
		}
	})
}

// Pollable is the interface for async-ready resources that can be polled.
type Pollable interface {
	Resource
	// Ready returns true if the resource is ready for I/O.
	Ready() bool
	// Block waits until the resource becomes ready or ctx is canceled.
	Block(ctx context.Context)
}

// NotifyPollable is an optional interface for pollables that expose a notification channel.
type NotifyPollable interface {
	Pollable
	Notify() <-chan struct{}
}

var closedPollableChan = func() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()

// PollableResource is a basic pollable that can be manually set ready.
// It is zero-value usable, synchronized, and safe for concurrent use.
type PollableResource struct {
	readyCh chan struct{}
	mu      sync.Mutex
	ready   bool
	dropped bool
}

var (
	_ Pollable       = (*PollableResource)(nil)
	_ NotifyPollable = (*PollableResource)(nil)
)

func (p *PollableResource) Type() ResourceType { return ResourcePollable }

// Drop marks the pollable as dropped and ready, unblocking any waiters.
// Once dropped, the pollable is terminal and cannot be revived by SetReady.
func (p *PollableResource) Drop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dropped {
		return
	}
	p.dropped = true
	p.ready = true
	if p.readyCh != nil {
		close(p.readyCh)
		p.readyCh = nil
	}
}

// Ready returns true if the resource is ready for I/O or has been dropped.
func (p *PollableResource) Ready() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

// SetReady updates the readiness state. If the pollable has been dropped,
// SetReady is a no-op and cannot revive or alter its terminal ready state.
func (p *PollableResource) SetReady(r bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dropped || p.ready == r {
		return
	}
	p.ready = r
	if r {
		if p.readyCh != nil {
			close(p.readyCh)
			p.readyCh = nil
		}
	}
}

// Notify returns a channel that is closed if the resource is currently ready
// or dropped. If the resource is not ready, it returns a channel that will close
// when the state next becomes ready or dropped.
func (p *PollableResource) Notify() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready || p.dropped {
		return closedPollableChan
	}
	if p.readyCh == nil {
		p.readyCh = make(chan struct{})
	}
	return p.readyCh
}

// Block waits until the resource becomes ready, is dropped, or ctx is canceled.
// Block never fabricates readiness.
func (p *PollableResource) Block(ctx context.Context) {
	for {
		if ctx.Err() != nil || p.Ready() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}

// TimerPollable implements a time-based pollable that becomes ready at a deadline
type TimerPollable struct {
	deadline time.Time
}

// NewTimerPollable creates a pollable that becomes ready at the specified deadline
func NewTimerPollable(deadline time.Time) *TimerPollable {
	return &TimerPollable{deadline: deadline}
}

// Deadline exposes the readiness instant without allocating a timer.
func (p *TimerPollable) Deadline() time.Time { return p.deadline }

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
	switch {
	case e.Closed:
		return "stream error: closed"
	case e.LastOpFailed:
		return "stream error: last-operation-failed"
	default:
		return "stream error: unknown"
	}
}

// WITErrorPayload returns the wasi:io/streams stream-error variant representation
// (variant stream-error { last-operation-failed(own<error>), closed }) used when
// lowering a result<_, stream-error> error to guest memory.
func (e *StreamError) WITErrorPayload() any {
	if e.LastOpFailed {
		return map[string]any{"last-operation-failed": e.LastOpFailedErr}
	}
	return map[string]any{"closed": nil}
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
	pendingOp          TCPNetworkOperation
	acceptQueue        *TCPAcceptQueue
	output             *TCPOutputStreamResource
	input              *TCPInputStreamResource
	notifyCh           chan struct{}
	localAddr          string
	remoteAddr         string
	listenBacklogSize  uint64
	keepAliveIdleTime  uint64
	keepAliveInterval  uint64
	receiveBufferSize  uint64
	sendBufferSize     uint64
	dropOnce           sync.Once
	mu                 sync.Mutex
	keepAliveCount     uint32
	inputStreamHandle  uint32
	outputStreamHandle uint32
	localPort          uint16
	remotePort         uint16
	hopLimit           uint8
	state              TCPState
	family             uint8
	dropped            bool
	keepAliveEnabled   bool
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
	s.dropOnce.Do(func() {
		s.mu.Lock()
		s.dropped = true
		s.state = TCPStateClosed
		conn, listener, input, output, acceptQueue, pendingOp := s.conn, s.listener, s.input, s.output, s.acceptQueue, s.pendingOp
		s.conn = nil
		s.listener = nil
		s.acceptQueue = nil
		s.pendingOp = nil
		if s.notifyCh != nil {
			close(s.notifyCh)
			s.notifyCh = nil
		}
		s.mu.Unlock()
		if pendingOp != nil {
			_ = pendingOp.Close()
		}
		if acceptQueue != nil {
			acceptQueue.Drop()
		}
		if input != nil {
			input.Drop()
		}
		if output != nil {
			output.Drop()
		}
		if c, ok := conn.(interface{ Close() error }); ok {
			_ = c.Close()
		}
		if l, ok := listener.(interface{ Close() error }); ok && acceptQueue == nil {
			_ = l.Close()
		}
		if input != nil && input.buffer != nil {
			input.buffer.WaitClosed()
		}
		if output != nil && output.buffer != nil {
			output.buffer.WaitClosed()
		}
		if acceptQueue != nil {
			acceptQueue.WaitClosed()
		}
	})
}
func (s *TCPSocketResource) Family() uint8   { s.mu.Lock(); defer s.mu.Unlock(); return s.family }
func (s *TCPSocketResource) State() TCPState { s.mu.Lock(); defer s.mu.Unlock(); return s.state }
func (s *TCPSocketResource) SetState(state TCPState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dropped {
		s.state = state
		if s.notifyCh != nil {
			close(s.notifyCh)
			s.notifyCh = nil
		}
	}
}
func (s *TCPSocketResource) IsListening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == TCPStateListening
}
func (s *TCPSocketResource) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == TCPStateConnected
}

// LocalAddr returns the local address
func (s *TCPSocketResource) LocalAddr() string { s.mu.Lock(); defer s.mu.Unlock(); return s.localAddr }
func (s *TCPSocketResource) LocalPort() uint16 { s.mu.Lock(); defer s.mu.Unlock(); return s.localPort }
func (s *TCPSocketResource) RemoteAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remoteAddr
}
func (s *TCPSocketResource) RemotePort() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remotePort
}

func (s *TCPSocketResource) SetLocalAddr(addr string, port uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localAddr = addr
	s.localPort = port
}

func (s *TCPSocketResource) SetRemoteAddr(addr string, port uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remoteAddr = addr
	s.remotePort = port
}

// Conn returns the underlying connection
func (s *TCPSocketResource) Conn() interface{} { s.mu.Lock(); defer s.mu.Unlock(); return s.conn }
func (s *TCPSocketResource) SetConn(conn interface{}) {
	s.mu.Lock()
	if !s.dropped {
		s.conn = conn
		if s.notifyCh != nil {
			close(s.notifyCh)
			s.notifyCh = nil
		}
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if c, ok := conn.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}
func (s *TCPSocketResource) Listener() interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener
}
func (s *TCPSocketResource) SetListener(listener interface{}) {
	s.mu.Lock()
	if !s.dropped {
		s.listener = listener
		if s.notifyCh != nil {
			close(s.notifyCh)
			s.notifyCh = nil
		}
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if c, ok := listener.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

// AcceptQueue returns the attached accept queue, if any.
func (s *TCPSocketResource) AcceptQueue() *TCPAcceptQueue {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acceptQueue
}

// SetAcceptQueue attaches an accept queue to the socket.
// If the socket has already been dropped or closed, it rejects late attach,
// drops the provided queue, and returns resource.ErrClosed.
func (s *TCPSocketResource) SetAcceptQueue(queue *TCPAcceptQueue) error {
	if queue == nil {
		return errors.New("nil accept queue")
	}
	s.mu.Lock()
	if s.dropped || s.state == TCPStateClosed {
		s.mu.Unlock()
		queue.Drop()
		return resource.ErrClosed
	}
	if s.acceptQueue != nil && s.acceptQueue != queue {
		s.mu.Unlock()
		return errors.New("accept queue already attached")
	}
	s.acceptQueue = queue
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
	s.mu.Unlock()
	return nil
}

// PendingError returns the pending error if any
func (s *TCPSocketResource) PendingError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingErr
}
func (s *TCPSocketResource) SetPendingError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingErr = err
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
}
func (s *TCPSocketResource) ClearPendingError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingErr = nil
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
}

// StreamHandles returns the input and output stream handles
func (s *TCPSocketResource) StreamHandles() (uint32, uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inputStreamHandle, s.outputStreamHandle
}
func (s *TCPSocketResource) SetStreamHandles(input, output uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputStreamHandle = input
	s.outputStreamHandle = output
}

// HopLimit returns the hop limit
func (s *TCPSocketResource) HopLimit() uint8     { s.mu.Lock(); defer s.mu.Unlock(); return s.hopLimit }
func (s *TCPSocketResource) SetHopLimit(v uint8) { s.mu.Lock(); defer s.mu.Unlock(); s.hopLimit = v }
func (s *TCPSocketResource) ReceiveBufferSize() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.receiveBufferSize
}
func (s *TCPSocketResource) SetReceiveBufferSize(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receiveBufferSize = v
}
func (s *TCPSocketResource) SendBufferSize() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendBufferSize
}
func (s *TCPSocketResource) SetSendBufferSize(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendBufferSize = v
}
func (s *TCPSocketResource) ListenBacklogSize() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listenBacklogSize
}
func (s *TCPSocketResource) SetListenBacklogSize(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listenBacklogSize = v
}

// KeepAliveEnabled returns whether keep-alive is enabled
func (s *TCPSocketResource) KeepAliveEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keepAliveEnabled
}
func (s *TCPSocketResource) SetKeepAliveEnabled(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keepAliveEnabled = v
}
func (s *TCPSocketResource) KeepAliveIdleTime() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keepAliveIdleTime
}
func (s *TCPSocketResource) SetKeepAliveIdleTime(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keepAliveIdleTime = v
}
func (s *TCPSocketResource) KeepAliveInterval() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keepAliveInterval
}
func (s *TCPSocketResource) SetKeepAliveInterval(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keepAliveInterval = v
}
func (s *TCPSocketResource) KeepAliveCount() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keepAliveCount
}
func (s *TCPSocketResource) SetKeepAliveCount(v uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keepAliveCount = v
}

// TCPInputStreamResource reads a bounded host buffer; network reads run off-worker.
type TCPInputStreamResource struct {
	socket *TCPSocketResource
	buffer *tcpInputBuffer
}

func NewTCPInputStreamResource(socket *TCPSocketResource) *TCPInputStreamResource {
	if socket == nil {
		return &TCPInputStreamResource{}
	}
	socket.mu.Lock()
	defer socket.mu.Unlock()
	if socket.input != nil {
		return socket.input
	}
	stream := &TCPInputStreamResource{socket: socket}
	if conn, ok := socket.conn.(net.Conn); ok && !socket.dropped && socket.state != TCPStateClosed {
		stream.buffer = newTCPInputBuffer(conn, DefaultBufferSize)
	}
	socket.input = stream
	return stream
}

// AbortSocket idempotently aborts the entire owning TCP socket and joins both pumps.
func (s *TCPInputStreamResource) AbortSocket() {
	if s == nil || s.socket == nil {
		return
	}
	s.socket.Drop()
}

func (*TCPInputStreamResource) Type() ResourceType { return ResourceInputStream }
func (s *TCPInputStreamResource) Drop() {
	if s.buffer != nil {
		s.buffer.Drop()
	}
}
func (s *TCPInputStreamResource) Read(length uint64) ([]byte, error) {
	if s.buffer == nil {
		return nil, &StreamError{Closed: true}
	}
	return s.buffer.Read(length)
}
func (s *TCPInputStreamResource) Ready() bool { return s.buffer == nil || s.buffer.Ready() }
func (s *TCPInputStreamResource) Notify() <-chan struct{} {
	if s.buffer == nil {
		return closedPollableChan
	}
	return s.buffer.Notify()
}
func (s *TCPInputStreamResource) Subscribe() Pollable { return (*tcpInputPollable)(s) }

// A subscription borrows its stream. Dropping it does not close the parent.
type tcpInputPollable TCPInputStreamResource

func (*tcpInputPollable) Type() ResourceType        { return ResourcePollable }
func (*tcpInputPollable) Drop()                     {}
func (p *tcpInputPollable) Ready() bool             { return (*TCPInputStreamResource)(p).Ready() }
func (p *tcpInputPollable) Notify() <-chan struct{} { return (*TCPInputStreamResource)(p).Notify() }
func (p *tcpInputPollable) Block(ctx context.Context) {
	for !p.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}

// TCPOutputStreamResource queues bounded output; network writes run off-worker.
type TCPOutputStreamResource struct {
	socket *TCPSocketResource
	buffer *tcpOutputBuffer
}

func NewTCPOutputStreamResource(socket *TCPSocketResource) *TCPOutputStreamResource {
	if socket == nil {
		return &TCPOutputStreamResource{}
	}
	socket.mu.Lock()
	defer socket.mu.Unlock()
	if socket.output != nil {
		return socket.output
	}
	stream := &TCPOutputStreamResource{socket: socket}
	if conn, ok := socket.conn.(net.Conn); ok && !socket.dropped && socket.state != TCPStateClosed {
		stream.buffer = newTCPOutputBuffer(conn, DefaultBufferSize)
	}
	socket.output = stream
	return stream
}

// AbortSocket idempotently aborts the entire owning TCP socket and joins both pumps.
func (s *TCPOutputStreamResource) AbortSocket() {
	if s == nil || s.socket == nil {
		return
	}
	s.socket.Drop()
}

func (*TCPOutputStreamResource) Type() ResourceType { return ResourceOutputStream }
func (s *TCPOutputStreamResource) Drop() {
	if s.buffer != nil {
		s.buffer.Drop()
	}
}
func (s *TCPOutputStreamResource) Write(data []byte) error {
	if s.buffer == nil {
		return &StreamError{Closed: true}
	}
	return s.buffer.Write(data)
}
func (s *TCPOutputStreamResource) CheckWrite() (uint64, error) {
	if s.buffer == nil {
		return 0, &StreamError{Closed: true}
	}
	return s.buffer.CheckWrite()
}
func (s *TCPOutputStreamResource) Flush() error {
	if s.buffer == nil {
		return &StreamError{Closed: true}
	}
	return s.buffer.Flush()
}
func (s *TCPOutputStreamResource) Ready() bool { return s.buffer == nil || s.buffer.Ready() }
func (s *TCPOutputStreamResource) Notify() <-chan struct{} {
	if s.buffer == nil {
		return closedPollableChan
	}
	return s.buffer.Notify()
}
func (s *TCPOutputStreamResource) Subscribe() Pollable { return (*tcpOutputPollable)(s) }

type tcpOutputPollable TCPOutputStreamResource

func (*tcpOutputPollable) Type() ResourceType        { return ResourcePollable }
func (*tcpOutputPollable) Drop()                     {}
func (p *tcpOutputPollable) Ready() bool             { return (*TCPOutputStreamResource)(p).Ready() }
func (p *tcpOutputPollable) Notify() <-chan struct{} { return (*TCPOutputStreamResource)(p).Notify() }
func (p *tcpOutputPollable) Block(ctx context.Context) {
	for !p.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-p.Notify():
		}
	}
}

// UDPState represents the state of a UDP socket
type UDPState uint8

const (
	UDPStateUnbound UDPState = iota
	UDPStateBindInProgress
	UDPStateBound
	UDPStateClosed
)

// UDPSocketResource represents a UDP socket with optional connected mode
// and socket-owned bounded datagram pumps.
type UDPSocketResource struct {
	pendingOp            TCPNetworkOperation
	bindNotify           chan struct{}
	pendingErr           error
	conn                 interface{}
	recvErr              error
	sendErr              error
	recvCond             *sync.Cond
	sendCond             *sync.Cond
	recvDone             chan struct{}
	sendDone             chan struct{}
	recvQ                []UDPDatagram
	sendQ                []UDPDatagram
	remoteAddr           string
	localAddr            string
	incomingPoll         PollableResource
	outgoingPoll         PollableResource
	receiveBufferSize    uint64
	sendBufferSize       uint64
	dropOnce             sync.Once
	mu                   sync.Mutex
	incomingStreamHandle uint32
	outgoingStreamHandle uint32
	localPort            uint16
	remotePort           uint16
	state                UDPState
	unicastHopLimit      uint8
	family               uint8
	sendInflight         bool
	pumpsStarted         bool
	dropped              bool
}

func NewUDPSocketResource(family uint8) *UDPSocketResource {
	s := &UDPSocketResource{
		family:            family,
		state:             UDPStateUnbound,
		unicastHopLimit:   64,
		receiveBufferSize: DefaultBufferSize,
		sendBufferSize:    DefaultBufferSize,
	}
	s.recvCond = sync.NewCond(&s.mu)
	s.sendCond = sync.NewCond(&s.mu)
	s.outgoingPoll.SetReady(true)
	return s
}

func (s *UDPSocketResource) Type() ResourceType { return ResourceUDPSocket }
func (s *UDPSocketResource) Drop() {
	s.dropOnce.Do(s.closeJoinClear)
}
func (s *UDPSocketResource) Family() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.family
}
func (s *UDPSocketResource) State() UDPState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}
func (s *UDPSocketResource) SetState(state UDPState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dropped {
		s.state = state
	}
}
func (s *UDPSocketResource) IsBound() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == UDPStateBound
}

// LocalAddr returns the local address
func (s *UDPSocketResource) LocalAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localAddr
}
func (s *UDPSocketResource) LocalPort() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localPort
}
func (s *UDPSocketResource) RemoteAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remoteAddr
}
func (s *UDPSocketResource) RemotePort() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remotePort
}

func (s *UDPSocketResource) SetLocalAddr(addr string, port uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localAddr = addr
	s.localPort = port
}

func (s *UDPSocketResource) SetRemoteAddr(addr string, port uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remoteAddr = addr
	s.remotePort = port
}

// Conn returns the underlying connection
func (s *UDPSocketResource) Conn() interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn
}
func (s *UDPSocketResource) SetConn(conn interface{}) {
	s.mu.Lock()
	if s.conn != nil && conn != nil && reflect.TypeOf(conn).Comparable() && s.conn == conn {
		s.mu.Unlock()
		return
	}
	if !s.dropped && s.state != UDPStateClosed && s.conn == nil {
		s.conn = conn
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if c, ok := conn.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

// PendingError returns the pending error if any
func (s *UDPSocketResource) PendingError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingErr
}
func (s *UDPSocketResource) SetPendingError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingErr = err
}
func (s *UDPSocketResource) ClearPendingError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingErr = nil
}

// StreamHandles returns the incoming and outgoing stream handles
func (s *UDPSocketResource) StreamHandles() (uint32, uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.incomingStreamHandle, s.outgoingStreamHandle
}
func (s *UDPSocketResource) SetStreamHandles(incoming, outgoing uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incomingStreamHandle = incoming
	s.outgoingStreamHandle = outgoing
}

// UnicastHopLimit returns the unicast hop limit
func (s *UDPSocketResource) UnicastHopLimit() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unicastHopLimit
}
func (s *UDPSocketResource) SetUnicastHopLimit(v uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unicastHopLimit = v
}
func (s *UDPSocketResource) ReceiveBufferSize() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.receiveBufferSize
}
func (s *UDPSocketResource) SetReceiveBufferSize(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receiveBufferSize = v
}
func (s *UDPSocketResource) SendBufferSize() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendBufferSize
}
func (s *UDPSocketResource) SetSendBufferSize(v uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendBufferSize = v
}

// IncomingDatagramStreamResource wraps a UDP socket for receiving datagrams.
type IncomingDatagramStreamResource struct {
	socket *UDPSocketResource
	remote *struct {
		addr string
		port uint16
	}
	dropped atomic.Bool
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

func (s *IncomingDatagramStreamResource) Type() ResourceType { return ResourceInputStream }
func (s *IncomingDatagramStreamResource) Drop() {
	if s.dropped.Swap(true) || s.socket == nil {
		return
	}
	wakeUDPReadiness(&s.socket.incomingPoll)
}
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
	dropped atomic.Bool
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

func (s *OutgoingDatagramStreamResource) Type() ResourceType { return ResourceOutputStream }
func (s *OutgoingDatagramStreamResource) Drop() {
	if s.dropped.Swap(true) || s.socket == nil {
		return
	}
	wakeUDPReadiness(&s.socket.outgoingPoll)
}
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
