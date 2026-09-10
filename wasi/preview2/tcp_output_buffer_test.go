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

type controlledWriteConn struct {
	writeErr        error
	writeBlocked    chan struct{}
	writeUnblock    chan struct{}
	written         []byte
	deadlines       []time.Time
	writeCalls      atomic.Int64
	closeCalls      atomic.Int64
	closeWriteCalls atomic.Int64
	writeMax        int
	mu              sync.Mutex
	unblockOnce     sync.Once
	closed          bool
	writeClosed     bool
}

func newControlledWriteConn() *controlledWriteConn {
	return &controlledWriteConn{writeMax: -1}
}

func (c *controlledWriteConn) unblock() {
	c.unblockOnce.Do(func() {
		if c.writeUnblock != nil {
			close(c.writeUnblock)
		}
	})
}

func (c *controlledWriteConn) snapshotWritten() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.written...)
}

func (c *controlledWriteConn) Write(p []byte) (int, error) {
	c.writeCalls.Add(1)

	c.mu.Lock()
	blockedCh := c.writeBlocked
	unblockCh := c.writeUnblock
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

	if c.closed || c.writeClosed {
		return 0, net.ErrClosed
	}

	n := len(p)
	if c.writeMax >= 0 && c.writeMax < n {
		n = c.writeMax
	}
	if n > 0 {
		c.written = append(c.written, p[:n]...)
	}
	return n, c.writeErr
}

func (c *controlledWriteConn) Read([]byte) (int, error) { return 0, io.EOF }

func (c *controlledWriteConn) Close() error {
	c.closeCalls.Add(1)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.unblock()
	return nil
}

func (c *controlledWriteConn) CloseWrite() error {
	c.closeWriteCalls.Add(1)
	c.mu.Lock()
	c.writeClosed = true
	c.mu.Unlock()
	c.unblock()
	return nil
}

func (c *controlledWriteConn) LocalAddr() net.Addr             { return &net.IPAddr{} }
func (c *controlledWriteConn) RemoteAddr() net.Addr            { return &net.IPAddr{} }
func (c *controlledWriteConn) SetDeadline(time.Time) error     { return nil }
func (c *controlledWriteConn) SetReadDeadline(time.Time) error { return nil }

func (c *controlledWriteConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadlines = append(c.deadlines, t)
	c.mu.Unlock()
	if !t.IsZero() && !t.After(time.Now()) {
		c.unblock()
	}
	return nil
}

type writeConnOnly struct {
	net.Conn
}

func joinOutputPump(t *testing.T, b *tcpOutputBuffer) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		b.WaitClosed()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not exit")
	}
}

func waitOutputReady(t *testing.T, b *tcpOutputBuffer) {
	t.Helper()
	select {
	case <-b.Notify():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output readiness")
	}
	if !b.Ready() {
		t.Fatal("Notify closed but Ready is false")
	}
}

func permitWrite(t *testing.T, b *tcpOutputBuffer, data []byte) {
	t.Helper()
	n, err := b.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if uint64(len(data)) > n {
		t.Fatalf("need permit %d, got %d", len(data), n)
	}
	if err := b.Write(data); err != nil {
		t.Fatalf("Write error: %v", err)
	}
}

func waitWritten(t *testing.T, c *controlledWriteConn, n int) []byte {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := c.snapshotWritten()
		if len(got) >= n {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for %d written bytes, have %d", n, len(c.snapshotWritten()))
	return nil
}

func TestTCPOutputBuffer_NetPipe_Backpressure(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	const capacity = 8
	buf := newTCPOutputBuffer(p1, capacity)
	defer func() {
		buf.Drop()
		_ = p1.Close()
		joinOutputPump(t, buf)
	}()

	if !buf.Ready() {
		t.Fatal("empty output buffer must be ready")
	}
	n, err := buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if n != uint64(capacity) {
		t.Fatalf("expected permit %d, got %d", capacity, n)
	}

	payload := []byte("abcdefgh")
	start := time.Now()
	if err := buf.Write(payload); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("guest Write blocked for %v", time.Since(start))
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		avail, cErr := buf.CheckWrite()
		if cErr != nil {
			t.Fatalf("CheckWrite error: %v", cErr)
		}
		if avail == 0 && !buf.Ready() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	avail, err := buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if avail != 0 {
		t.Fatalf("expected 0 permit under backpressure, got %d", avail)
	}
	if buf.Ready() {
		t.Fatal("expected not ready under backpressure")
	}

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(p2, got); err != nil {
		t.Fatalf("peer read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("expected %q, got %q", payload, got)
	}

	waitOutputReady(t, buf)
	avail, err = buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite after drain: %v", err)
	}
	if avail == 0 {
		t.Fatal("expected room after peer read")
	}
}

func TestTCPOutputBuffer_PartialWriterKeepsSuffix(t *testing.T) {
	fake := newControlledWriteConn()
	fake.writeMax = 3

	const capacity = 16
	buf := newTCPOutputBuffer(fake, capacity)
	defer func() {
		buf.Drop()
		joinOutputPump(t, buf)
	}()

	payload := []byte("0123456789abcdefghij") // 20 bytes, wraps the 16-byte ring
	offset := 0
	for offset < len(payload) {
		waitOutputReady(t, buf)
		n, err := buf.CheckWrite()
		if err != nil {
			t.Fatalf("CheckWrite error: %v", err)
		}
		if n == 0 {
			t.Fatal("ready stream returned 0 permit")
		}
		chunk := payload[offset:]
		if uint64(len(chunk)) > n {
			chunk = chunk[:n]
		}
		if err := buf.Write(chunk); err != nil {
			t.Fatalf("Write error: %v", err)
		}
		offset += len(chunk)
	}

	if err := buf.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	waitOutputReady(t, buf)

	got := waitWritten(t, fake, len(payload))
	if !bytes.Equal(got[:len(payload)], payload) {
		t.Fatalf("partial writer dropped suffix: got %q want %q", got, payload)
	}
	if fake.writeCalls.Load() < 2 {
		t.Fatalf("expected multiple partial writes, got %d", fake.writeCalls.Load())
	}
}

func TestTCPOutputBuffer_FlushWaitAndReadiness(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	buf := newTCPOutputBuffer(p1, 32)
	defer func() {
		buf.Drop()
		_ = p1.Close()
		joinOutputPump(t, buf)
	}()

	payload := []byte("flush-me")
	permitWrite(t, buf, payload)

	flushStart := time.Now()
	if err := buf.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	if time.Since(flushStart) > 50*time.Millisecond {
		t.Fatalf("Flush blocked for %v", time.Since(flushStart))
	}

	avail, err := buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite during flush: %v", err)
	}
	if avail != 0 {
		t.Fatalf("expected check-write 0 during flush, got %d", avail)
	}
	if buf.Ready() {
		t.Fatal("expected not ready during flush")
	}

	ch := buf.Notify()
	select {
	case <-ch:
		t.Fatal("Notify must stay open while flush is pending")
	default:
	}

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(p2, got); err != nil {
		t.Fatalf("peer read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("expected %q, got %q", payload, got)
	}

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for flush completion")
	}
	if !buf.Ready() {
		t.Fatal("expected ready after flush drain")
	}
	avail, err = buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite after flush: %v", err)
	}
	if avail == 0 {
		t.Fatal("expected positive permit after flush")
	}
}

func TestTCPOutputBuffer_PermitViolationNoSideEffects(t *testing.T) {
	fake := newControlledWriteConn()
	buf := newTCPOutputBuffer(fake, 8)
	defer func() {
		buf.Drop()
		joinOutputPump(t, buf)
	}()

	n, err := buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if n != 8 {
		t.Fatalf("expected permit 8, got %d", n)
	}

	oversized := make([]byte, n+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	writeErr := buf.Write(oversized)
	if !errors.Is(writeErr, ErrWritePermit) {
		t.Fatalf("expected ErrWritePermit, got %v", writeErr)
	}

	avail, err := buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite after violation: %v", err)
	}
	if avail != n {
		t.Fatalf("permit violation must not consume credit: before=%d after=%d", n, avail)
	}
	if !buf.Ready() {
		t.Fatal("permit violation must not change readiness")
	}
	if got := fake.snapshotWritten(); len(got) != 0 {
		t.Fatalf("permit violation wrote %q", got)
	}

	allowed := []byte("abcd")
	if err := buf.Write(allowed); err != nil {
		t.Fatalf("valid write after violation: %v", err)
	}
	if err := buf.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	waitOutputReady(t, buf)
	got := waitWritten(t, fake, len(allowed))
	if !bytes.Equal(got[:len(allowed)], allowed) {
		t.Fatalf("expected %q after valid write, got %q", allowed, got)
	}
}

func TestTCPOutputBuffer_WriteWithoutCheckWrite(t *testing.T) {
	fake := newControlledWriteConn()
	buf := newTCPOutputBuffer(fake, 8)
	defer func() {
		buf.Drop()
		joinOutputPump(t, buf)
	}()

	err := buf.Write([]byte("no-permit"))
	if !errors.Is(err, ErrWritePermit) {
		t.Fatalf("expected ErrWritePermit, got %v", err)
	}
	if got := fake.snapshotWritten(); len(got) != 0 {
		t.Fatalf("unpermitted write produced %q", got)
	}
}

func TestTCPOutputBuffer_WriteReturnsPromptly(t *testing.T) {
	fake := newControlledWriteConn()
	fake.writeBlocked = make(chan struct{}, 1)
	fake.writeUnblock = make(chan struct{})

	buf := newTCPOutputBuffer(fake, 32)
	permitWrite(t, buf, []byte("blocked"))

	select {
	case <-fake.writeBlocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pump to enter conn.Write")
	}

	start := time.Now()
	n, err := buf.CheckWrite()
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("CheckWrite blocked for %v", elapsed)
	}
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if n == 0 {
		t.Fatal("expected remaining room while pump is blocked")
	}

	start = time.Now()
	if err := buf.Write([]byte("more")); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("guest Write blocked for %v", time.Since(start))
	}

	buf.Drop()
	joinOutputPump(t, buf)
}

func TestTCPOutputBuffer_ZeroNilTerminalNoProgress(t *testing.T) {
	fake := newControlledWriteConn()
	fake.writeMax = 0

	buf := newTCPOutputBuffer(fake, 16)
	defer func() {
		buf.Drop()
		joinOutputPump(t, buf)
	}()

	permitWrite(t, buf, []byte("data"))

	var se *StreamError
	deadline := time.Now().Add(2 * time.Second)
	var err error
	var avail uint64
	for time.Now().Before(deadline) {
		avail, err = buf.CheckWrite()
		if errors.As(err, &se) && se.LastOpFailed {
			break
		}
		se = nil
		time.Sleep(time.Millisecond)
	}
	if avail != 0 {
		t.Fatalf("expected 0 permit after terminal write, got %d", avail)
	}
	if se == nil || !se.LastOpFailed {
		t.Fatalf("expected StreamError LastOpFailed, got %v", err)
	}

	writeErr := buf.Write([]byte("x"))
	var seWrite *StreamError
	if !errors.As(writeErr, &seWrite) || !seWrite.LastOpFailed {
		t.Fatalf("expected Write LastOpFailed, got %v", writeErr)
	}
	flushErr := buf.Flush()
	var seFlush *StreamError
	if !errors.As(flushErr, &seFlush) || !seFlush.LastOpFailed {
		t.Fatalf("expected Flush LastOpFailed, got %v", flushErr)
	}
	if got := fake.snapshotWritten(); len(got) != 0 {
		t.Fatalf("0,nil write must not consume bytes, got %q", got)
	}
}

func TestTCPOutputBuffer_LastOpFailedThenDropClosed(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()

	buf := newTCPOutputBuffer(p1, 16)
	permitWrite(t, buf, []byte("hi"))
	_ = p2.Close()

	var se *StreamError
	deadline := time.Now().Add(2 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		_, err = buf.CheckWrite()
		if errors.As(err, &se) && se.LastOpFailed {
			break
		}
		se = nil
		time.Sleep(time.Millisecond)
	}
	if se == nil || !se.LastOpFailed {
		t.Fatalf("expected LastOpFailed after peer close, got %v", err)
	}

	buf.Drop()
	joinOutputPump(t, buf)

	_, err = buf.CheckWrite()
	var seClosed *StreamError
	if !errors.As(err, &seClosed) || !seClosed.Closed {
		t.Fatalf("expected Closed after Drop, got %v", err)
	}
	writeErr := buf.Write([]byte("x"))
	if !errors.As(writeErr, &seClosed) || !seClosed.Closed {
		t.Fatalf("expected Write Closed after Drop, got %v", writeErr)
	}
}

func TestTCPOutputBuffer_DropUnblocks_CloseWrite(t *testing.T) {
	fake := newControlledWriteConn()
	fake.writeBlocked = make(chan struct{}, 1)
	fake.writeUnblock = make(chan struct{})

	buf := newTCPOutputBuffer(fake, 32)
	permitWrite(t, buf, []byte("pending"))

	select {
	case <-fake.writeBlocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pump write")
	}

	dropStart := time.Now()
	buf.Drop()
	if time.Since(dropStart) > 50*time.Millisecond {
		t.Fatalf("Drop blocked for %v", time.Since(dropStart))
	}

	if fake.closeWriteCalls.Load() != 1 {
		t.Fatalf("expected 1 CloseWrite call, got %d", fake.closeWriteCalls.Load())
	}
	if fake.closeCalls.Load() != 0 {
		t.Fatal("conn.Close() was called; input must survive")
	}
	if !buf.Ready() {
		t.Fatal("expected ready after Drop")
	}
	select {
	case <-buf.Notify():
	default:
		t.Fatal("expected Notify closed after Drop")
	}

	joinOutputPump(t, buf)

	_, err := buf.CheckWrite()
	var se *StreamError
	if !errors.As(err, &se) || !se.Closed {
		t.Fatalf("expected Closed after Drop, got %v", err)
	}
}

func TestTCPOutputBuffer_DropUnblocks_SetWriteDeadline(t *testing.T) {
	fake := newControlledWriteConn()
	fake.writeBlocked = make(chan struct{}, 1)
	fake.writeUnblock = make(chan struct{})

	buf := newTCPOutputBuffer(&writeConnOnly{Conn: fake}, 32)
	permitWrite(t, buf, []byte("pending"))

	select {
	case <-fake.writeBlocked:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pump write")
	}

	buf.Drop()

	if len(fake.deadlines) == 0 {
		t.Fatal("expected SetWriteDeadline when CloseWrite is unsupported")
	}
	if fake.closeWriteCalls.Load() != 0 {
		t.Fatal("CloseWrite must not be called through a net.Conn wrapper")
	}
	if fake.closeCalls.Load() != 0 {
		t.Fatal("conn.Close() was called; input must survive")
	}

	joinOutputPump(t, buf)
}

func TestTCPOutputBuffer_DropNetPipeUnblockAndInputSurvives(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p2.Close()

	buf := newTCPOutputBuffer(p1, 8)
	permitWrite(t, buf, []byte("blocked!"))

	time.Sleep(20 * time.Millisecond)

	buf.Drop()
	joinOutputPump(t, buf)

	writeErr := make(chan error, 1)
	go func() {
		_, err := p2.Write([]byte("in"))
		writeErr <- err
	}()

	got := make([]byte, 2)
	_ = p1.SetReadDeadline(time.Now().Add(time.Second))
	n, err := p1.Read(got)
	if err != nil {
		t.Fatalf("input read after output Drop: %v", err)
	}
	if n != 2 || string(got) != "in" {
		t.Fatalf("expected input 'in', got %q", got[:n])
	}
	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatalf("peer write after output Drop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("peer write did not complete")
	}

	_ = p1.Close()
}

func TestTCPOutputBuffer_DefaultCapacity(t *testing.T) {
	fake := newControlledWriteConn()
	buf := newTCPOutputBuffer(fake, 0)
	defer func() {
		buf.Drop()
		joinOutputPump(t, buf)
	}()

	n, err := buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if n != 64*1024 {
		t.Fatalf("expected default 64KiB permit, got %d", n)
	}
}

func TestTCPOutputBuffer_EmptyFlushStaysReady(t *testing.T) {
	fake := newControlledWriteConn()
	buf := newTCPOutputBuffer(fake, 8)
	defer func() {
		buf.Drop()
		joinOutputPump(t, buf)
	}()

	if err := buf.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	if !buf.Ready() {
		t.Fatal("empty flush must complete ready")
	}
	n, err := buf.CheckWrite()
	if err != nil {
		t.Fatalf("CheckWrite error: %v", err)
	}
	if n != 8 {
		t.Fatalf("expected permit 8 after empty flush, got %d", n)
	}
}

func TestTCPOutputBuffer_ConcurrentDrop(t *testing.T) {
	p1, p2 := net.Pipe()

	buf := newTCPOutputBuffer(p1, 128)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer p2.Close()
		scratch := make([]byte, 16)
		for {
			select {
			case <-stop:
				return
			default:
				_, err := p2.Read(scratch)
				if err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		payload := []byte("concurrent")
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = buf.CheckWrite()
				_ = buf.Write(payload)
				_ = buf.Flush()
				_ = buf.Ready()
				_ = buf.Notify()
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
	joinOutputPump(t, buf)
}
