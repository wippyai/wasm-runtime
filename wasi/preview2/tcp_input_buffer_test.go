package preview2

import (
	"bytes"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// controlledConn is a configurable mock net.Conn for unit testing.
type controlledConn struct {
	readErr         error
	readBlocked     chan struct{}
	readUnblock     chan struct{}
	readData        []byte
	deadlines       []time.Time
	mu              sync.Mutex
	readCalls       atomic.Int64
	bytesRead       atomic.Int64
	closeCalls      atomic.Int64
	closeReadCalls  atomic.Int64
	readErrWithData bool
	closed          bool
}

func newControlledConn() *controlledConn {
	return &controlledConn{}
}

func (c *controlledConn) Read(b []byte) (int, error) {
	c.readCalls.Add(1)

	c.mu.Lock()
	blockedCh := c.readBlocked
	unblockCh := c.readUnblock
	c.mu.Unlock()

	if blockedCh != nil {
		select {
		case blockedCh <- struct{}{}:
		default:
		}
	}

	if unblockCh != nil {
		<-unblockCh
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, net.ErrClosed
	}

	if len(c.readData) == 0 && c.readErr != nil {
		return 0, c.readErr
	}

	if len(c.readData) > 0 {
		n := copy(b, c.readData)
		c.readData = c.readData[n:]
		c.bytesRead.Add(int64(n))

		if c.readErrWithData && c.readErr != nil {
			return n, c.readErr
		}
		if len(c.readData) == 0 && c.readErr != nil && !c.readErrWithData {
			return n, c.readErr
		}
		return n, nil
	}

	return 0, io.EOF
}

func (c *controlledConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	return len(b), nil
}

func (c *controlledConn) Close() error {
	c.closeCalls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.readUnblock != nil {
		select {
		case <-c.readUnblock:
		default:
			close(c.readUnblock)
		}
	}
	return nil
}

func (c *controlledConn) CloseRead() error {
	c.closeReadCalls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readErr == nil {
		c.readErr = io.EOF
	}
	if c.readUnblock != nil {
		select {
		case <-c.readUnblock:
		default:
			close(c.readUnblock)
		}
	}
	return nil
}

func (c *controlledConn) LocalAddr() net.Addr                { return &net.IPAddr{} }
func (c *controlledConn) RemoteAddr() net.Addr               { return &net.IPAddr{} }
func (c *controlledConn) SetDeadline(t time.Time) error      { return nil }
func (c *controlledConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *controlledConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlines = append(c.deadlines, t)
	if !t.IsZero() && !t.After(time.Now()) {
		if c.readErr == nil {
			c.readErr = errors.New("i/o timeout")
		}
		if c.readUnblock != nil {
			select {
			case <-c.readUnblock:
			default:
				close(c.readUnblock)
			}
		}
	}
	return nil
}

// netConnOnly wraps a net.Conn to obscure any CloseRead method.
type netConnOnly struct {
	net.Conn
}

func TestTCPInputBuffer_NetPipe_ReadAndDrain(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	buf := newTCPInputBuffer(p1, 1024)
	defer buf.Drop()

	// Initially empty: promptly returns empty slice without error
	if buf.Ready() {
		t.Fatal("expected buffer to not be ready initially")
	}
	data, err := buf.Read(10)
	if err != nil {
		t.Fatalf("expected nil error on open empty buffer, got: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected 0 bytes, got: %d", len(data))
	}

	// Write data to pipe
	go func() {
		_, _ = p2.Write([]byte("hello world"))
	}()

	select {
	case <-buf.Notify():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for buffer notification")
	}

	if !buf.Ready() {
		t.Fatal("expected buffer to be ready")
	}

	// Read partial chunk
	chunk1, err := buf.Read(5)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(chunk1) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(chunk1))
	}

	// Read remaining chunk
	chunk2, err := buf.Read(10)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(chunk2) != " world" {
		t.Fatalf("expected ' world', got %q", string(chunk2))
	}

	// Buffer drained, still open: returns empty promptly
	if buf.Ready() {
		t.Fatal("expected buffer to not be ready when drained")
	}
	chunk3, err := buf.Read(10)
	if err != nil {
		t.Fatalf("expected nil error on drained open buffer, got %v", err)
	}
	if len(chunk3) != 0 {
		t.Fatalf("expected empty read, got: %q", string(chunk3))
	}

	// Send EOF by closing writing side
	_ = p2.Close()

	// Wait for EOF signal
	select {
	case <-buf.Notify():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EOF readiness")
	}

	if !buf.Ready() {
		t.Fatal("expected ready at EOF")
	}

	// Drained read on EOF yields StreamError{Closed: true}
	eofData, eofErr := buf.Read(10)
	if len(eofData) != 0 {
		t.Fatalf("expected empty data at EOF, got %q", string(eofData))
	}
	var se *StreamError
	if !errors.As(eofErr, &se) || !se.Closed {
		t.Fatalf("expected StreamError with Closed: true, got: %v", eofErr)
	}

	_ = p1.Close()
	buf.WaitClosed()
}

func TestTCPInputBuffer_EOFWithBytes(t *testing.T) {
	fake := newControlledConn()
	fake.readData = []byte("initial-payload")
	fake.readErr = io.EOF
	fake.readErrWithData = true // Delivers payload and io.EOF together

	buf := newTCPInputBuffer(fake, 128)
	defer buf.Drop()

	select {
	case <-buf.Notify():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for buffer readiness")
	}

	if !buf.Ready() {
		t.Fatal("expected buffer to be ready")
	}

	// Delivers n > 0 before returning EOF error
	data, err := buf.Read(100)
	if err != nil {
		t.Fatalf("expected nil error on first read delivering buffered data, got: %v", err)
	}
	if string(data) != "initial-payload" {
		t.Fatalf("expected 'initial-payload', got %q", string(data))
	}

	// Readiness remains true because EOF is terminal and waiting to be delivered
	if !buf.Ready() {
		t.Fatal("expected buffer to remain ready when EOF is pending")
	}

	// Subsequent read when drained returns StreamError{Closed: true}
	drainedData, drainedErr := buf.Read(100)
	if len(drainedData) != 0 {
		t.Fatalf("expected 0 bytes, got %d", len(drainedData))
	}
	var se *StreamError
	if !errors.As(drainedErr, &se) || !se.Closed {
		t.Fatalf("expected StreamError with Closed: true, got: %v", drainedErr)
	}

	// Further reads stay terminal Closed
	_, nextErr := buf.Read(100)
	var seNext *StreamError
	if !errors.As(nextErr, &seNext) || !seNext.Closed {
		t.Fatalf("expected repeated StreamError Closed, got: %v", nextErr)
	}

	buf.WaitClosed()
}

func TestTCPInputBuffer_NonEOFErrorWithBytes_LastOpFailed(t *testing.T) {
	fake := newControlledConn()
	fake.readData = []byte("error-payload")
	fake.readErr = errors.New("connection reset by peer")
	fake.readErrWithData = true

	buf := newTCPInputBuffer(fake, 128)
	defer buf.Drop()

	select {
	case <-buf.Notify():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for buffer readiness")
	}

	// First read delivers buffered data before error
	data, err := buf.Read(100)
	if err != nil {
		t.Fatalf("expected nil error on first read, got: %v", err)
	}
	if string(data) != "error-payload" {
		t.Fatalf("expected 'error-payload', got %q", string(data))
	}

	// Subsequent read returns StreamError{LastOpFailed: true}
	_, drainedErr := buf.Read(100)
	var se *StreamError
	if !errors.As(drainedErr, &se) || !se.LastOpFailed {
		t.Fatalf("expected StreamError with LastOpFailed: true, got: %v", drainedErr)
	}

	buf.WaitClosed()
}

func TestTCPInputBuffer_ExactBufferBackpressure(t *testing.T) {
	fake := newControlledConn()
	payload := []byte("0123456789abcdefghij") // 20 bytes
	fake.readData = payload
	fake.readErr = io.EOF

	// Capacity of 8 bytes: ring buffer must not grow, must stop after reading 8 bytes
	capacity := 8
	buf := newTCPInputBuffer(fake, capacity)
	defer buf.Drop()

	// Wait for buffer to fill up to capacity
	time.Sleep(50 * time.Millisecond)

	readCount := fake.bytesRead.Load()
	if readCount != int64(capacity) {
		t.Fatalf("expected exactly %d bytes read due to backpressure, got %d", capacity, readCount)
	}

	// Drain 4 bytes: should wake pump and read exactly 4 more bytes
	chunk1, err := buf.Read(4)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(chunk1) != "0123" {
		t.Fatalf("expected '0123', got %q", string(chunk1))
	}

	time.Sleep(50 * time.Millisecond)
	readCountAfter := fake.bytesRead.Load()
	if readCountAfter != 12 {
		t.Fatalf("expected exactly 12 bytes read after draining 4 bytes, got %d", readCountAfter)
	}

	// Drain remaining 16 bytes in chunks
	var collected bytes.Buffer
	collected.Write(chunk1)

	for collected.Len() < len(payload) {
		chunk, rErr := buf.Read(4)
		if rErr != nil {
			t.Fatalf("unexpected error reading chunk: %v", rErr)
		}
		if len(chunk) > 0 {
			collected.Write(chunk)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !bytes.Equal(collected.Bytes(), payload) {
		t.Fatalf("data mismatch: expected %q, got %q", payload, collected.Bytes())
	}

	// Buffer drained, EOF delivered
	_, eofErr := buf.Read(10)
	var se *StreamError
	if !errors.As(eofErr, &se) || !se.Closed {
		t.Fatalf("expected StreamError Closed at EOF, got %v", eofErr)
	}

	buf.WaitClosed()
}

func TestTCPInputBuffer_NoPinnedRead(t *testing.T) {
	fake := newControlledConn()
	fake.readBlocked = make(chan struct{}, 1)
	fake.readUnblock = make(chan struct{})

	buf := newTCPInputBuffer(fake, 1024)

	// Wait until pump enters conn.Read
	select {
	case <-fake.readBlocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pump to enter conn.Read")
	}

	// Calling Read on calling thread must NOT block on network I/O
	start := time.Now()
	data, err := buf.Read(1024)
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("calling Read blocked for %v; must return promptly", elapsed)
	}
	if err != nil {
		t.Fatalf("expected nil error on prompt empty read, got: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty data, got %d bytes", len(data))
	}

	buf.Drop()
	buf.WaitClosed()
}

func TestTCPInputBuffer_ZeroLenMustNotConsume(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	buf := newTCPInputBuffer(p1, 64)
	defer buf.Drop()

	go func() {
		_, _ = p2.Write([]byte("abcdef"))
	}()

	select {
	case <-buf.Notify():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readiness")
	}

	// Read(0) must not consume anything
	zeroChunk, err := buf.Read(0)
	if err != nil {
		t.Fatalf("unexpected error on Read(0): %v", err)
	}
	if len(zeroChunk) != 0 {
		t.Fatalf("expected 0 bytes from Read(0), got %d", len(zeroChunk))
	}

	// All 6 bytes must still be present in buffer
	fullChunk, err := buf.Read(6)
	if err != nil {
		t.Fatalf("unexpected error on Read(6): %v", err)
	}
	if string(fullChunk) != "abcdef" {
		t.Fatalf("expected 'abcdef', got %q", string(fullChunk))
	}
}

func TestTCPInputBuffer_MaxAllocationSize(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	buf := newTCPInputBuffer(p1, 64)
	defer buf.Drop()

	go func() {
		_, _ = p2.Write([]byte("data"))
	}()

	select {
	case <-buf.Notify():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readiness")
	}

	// Requesting more than MaxAllocationSize clamps to MaxAllocationSize,
	// but only allocates min(request, buffered) = 4 bytes
	chunk, err := buf.Read(MaxAllocationSize + 500)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(chunk) != "data" {
		t.Fatalf("expected 'data', got %q", string(chunk))
	}

	// Next read with MaxAllocationSize on empty buffer returns empty slice without allocating MaxAllocationSize
	emptyChunk, err := buf.Read(MaxAllocationSize)
	if err != nil {
		t.Fatalf("unexpected read error on empty: %v", err)
	}
	if len(emptyChunk) != 0 {
		t.Fatalf("expected 0 bytes, got %d", len(emptyChunk))
	}
}

func TestTCPInputBuffer_CancellationDropWakeup_CloseRead(t *testing.T) {
	fake := newControlledConn()
	fake.readBlocked = make(chan struct{}, 1)
	fake.readUnblock = make(chan struct{})

	buf := newTCPInputBuffer(fake, 128)

	select {
	case <-fake.readBlocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pump read")
	}

	dropStart := time.Now()
	buf.Drop()
	dropElapsed := time.Since(dropStart)

	if dropElapsed > 50*time.Millisecond {
		t.Fatalf("Drop blocked for %v; must not block", dropElapsed)
	}

	// Verify CloseRead was called and conn itself was NOT closed (output survives)
	if fake.closeReadCalls.Load() != 1 {
		t.Fatalf("expected 1 CloseRead call, got %d", fake.closeReadCalls.Load())
	}
	if fake.closeCalls.Load() != 0 {
		t.Fatalf("conn.Close() was called; output must survive!")
	}

	// Drop marks terminal ready and notifies
	if !buf.Ready() {
		t.Fatal("expected ready after Drop")
	}
	select {
	case <-buf.Notify():
	default:
		t.Fatal("expected Notify channel to be closed after Drop")
	}

	// Pump unblocks and joins promptly
	buf.WaitClosed()

	// Read after Drop yields StreamError Closed
	_, err := buf.Read(10)
	var se *StreamError
	if !errors.As(err, &se) || !se.Closed {
		t.Fatalf("expected StreamError Closed after Drop, got: %v", err)
	}
}

func TestTCPInputBuffer_CancellationDropWakeup_SetReadDeadline(t *testing.T) {
	fake := newControlledConn()
	fake.readBlocked = make(chan struct{}, 1)
	fake.readUnblock = make(chan struct{})

	// Wrap in netConnOnly to hide any CloseRead method so it falls back to SetReadDeadline
	buf := newTCPInputBuffer(&netConnOnly{Conn: fake}, 128)

	select {
	case <-fake.readBlocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pump read")
	}

	buf.Drop()

	if len(fake.deadlines) == 0 {
		t.Fatal("expected SetReadDeadline to be called when CloseRead is unsupported")
	}
	if fake.closeCalls.Load() != 0 {
		t.Fatalf("conn.Close() was called; output must survive!")
	}

	buf.WaitClosed()

	if !buf.Ready() {
		t.Fatal("expected ready after Drop")
	}
	_, err := buf.Read(10)
	var se *StreamError
	if !errors.As(err, &se) || !se.Closed {
		t.Fatalf("expected StreamError Closed after Drop, got: %v", err)
	}
}

func TestTCPInputBuffer_DropFullBufferWakeup(t *testing.T) {
	fake := newControlledConn()
	fake.readData = []byte("12345678") // 8 bytes for capacity of 4
	fake.readErr = io.EOF

	buf := newTCPInputBuffer(fake, 4)

	// Allow buffer to fill to capacity and wait on cond
	time.Sleep(50 * time.Millisecond)

	buf.Drop()

	// Pump must be woken from cond.Wait and terminate
	buf.WaitClosed()

	// Buffered data was discarded by Drop
	_, err := buf.Read(10)
	var se *StreamError
	if !errors.As(err, &se) || !se.Closed {
		t.Fatalf("expected StreamError Closed after Drop with discarded data, got: %v", err)
	}
}

func TestTCPInputBuffer_Concurrency(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	buf := newTCPInputBuffer(p1, 256)
	defer buf.Drop()

	totalBytes := 16 * 1024
	src := make([]byte, totalBytes)
	for i := range src {
		src[i] = byte(i % 251)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer goroutine
	go func() {
		defer wg.Done()
		offset := 0
		for offset < totalBytes {
			chunkSize := 17
			if offset+chunkSize > totalBytes {
				chunkSize = totalBytes - offset
			}
			n, err := p2.Write(src[offset : offset+chunkSize])
			if err != nil {
				return
			}
			offset += n
		}
		_ = p2.Close()
	}()

	// Reader goroutine
	dst := make([]byte, 0, totalBytes)
	go func() {
		defer wg.Done()
		for {
			if !buf.Ready() {
				select {
				case <-buf.Notify():
				case <-time.After(2 * time.Second):
					t.Errorf("timed out waiting for readiness")
					return
				}
			}

			chunk, err := buf.Read(31)
			if len(chunk) > 0 {
				dst = append(dst, chunk...)
			}
			if err != nil {
				var se *StreamError
				if errors.As(err, &se) && se.Closed {
					return
				}
				t.Errorf("unexpected read error: %v", err)
				return
			}
		}
	}()

	wg.Wait()

	if !bytes.Equal(src, dst) {
		t.Fatalf("concurrent stream data corrupted; received %d bytes of %d", len(dst), len(src))
	}
}

func TestTCPInputBuffer_ConcurrentDrop(t *testing.T) {
	p1, p2 := net.Pipe()

	buf := newTCPInputBuffer(p1, 128)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer p2.Close()
		for {
			select {
			case <-stop:
				return
			default:
				_, err := p2.Write([]byte("concurrent data chunk"))
				if err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, err := buf.Read(16)
				_ = buf.Ready()
				_ = buf.Notify()
				if err != nil {
					return
				}
				runtime.Gosched()
			}
		}
	}()

	time.Sleep(5 * time.Millisecond)
	buf.Drop()
	close(stop)
	_ = p2.Close()
	_ = p1.Close()

	wg.Wait()
	buf.WaitClosed()
}
