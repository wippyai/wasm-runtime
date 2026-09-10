package preview2

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// ErrWritePermit is returned by Write when contents exceed the permit last
// returned by CheckWrite.
var ErrWritePermit = errors.New("write exceeds check-write permit")

type closeWriter interface {
	CloseWrite() error
}

// tcpOutputBuffer is an unexported bounded output buffer for TCP connections.
// It maintains a fixed ring allocated once, a single write pump goroutine, and
// synchronizes readiness state transitions under its stream mutex.
type tcpOutputBuffer struct {
	conn       net.Conn
	err        error
	cond       *sync.Cond
	doneCh     chan struct{}
	buf        []byte
	pollable   PollableResource
	capacity   int
	readPos    int
	writePos   int
	buffered   int
	mu         sync.Mutex
	lastPermit uint64
	flushing   bool
	dropped    bool
}

func newTCPOutputBuffer(conn net.Conn, capacity int) *tcpOutputBuffer {
	if capacity <= 0 {
		capacity = 64 * 1024
	}
	if capacity > MaxAllocationSize {
		capacity = MaxAllocationSize
	}

	b := &tcpOutputBuffer{
		conn:     conn,
		capacity: capacity,
		buf:      make([]byte, capacity),
		doneCh:   make(chan struct{}),
	}
	b.cond = sync.NewCond(&b.mu)

	if conn == nil {
		b.dropped = true
		b.pollable.Drop()
		close(b.doneCh)
		return b
	}

	b.mu.Lock()
	b.updateReadyLocked()
	b.mu.Unlock()

	go b.runPump()
	return b
}

func (b *tcpOutputBuffer) runPump() {
	defer func() { b.mu.Lock(); b.conn = nil; b.buf = nil; b.mu.Unlock(); close(b.doneCh) }()

	for {
		b.mu.Lock()
		for b.buffered == 0 && !b.dropped {
			b.cond.Wait()
		}
		if b.dropped {
			b.mu.Unlock()
			return
		}

		slice := b.pendingSliceLocked()
		b.mu.Unlock()

		n, err := b.conn.Write(slice)

		b.mu.Lock()
		if b.dropped {
			b.mu.Unlock()
			return
		}

		if n < 0 || n > len(slice) {
			n = 0
			err = io.ErrShortWrite
		}
		if n > 0 {
			b.readPos = (b.readPos + n) % b.capacity
			b.buffered -= n
		}

		if n == 0 && err == nil {
			err = io.ErrNoProgress
		}
		if err != nil {
			b.err = err
			b.flushing = false
			b.lastPermit = 0
			b.updateReadyLocked()
			b.mu.Unlock()
			return
		}

		if b.flushing && b.buffered == 0 {
			b.flushing = false
		}
		b.updateReadyLocked()
		b.mu.Unlock()
	}
}

func (b *tcpOutputBuffer) pendingSliceLocked() []byte {
	if b.buffered == 0 {
		return nil
	}
	end := b.readPos + b.buffered
	if end <= b.capacity {
		return b.buf[b.readPos:end]
	}
	return b.buf[b.readPos:b.capacity]
}

func (b *tcpOutputBuffer) enqueueLocked(data []byte) {
	n := len(data)
	first := b.capacity - b.writePos
	if first > n {
		first = n
	}
	copy(b.buf[b.writePos:b.writePos+first], data[:first])
	if n > first {
		copy(b.buf[:n-first], data[first:])
	}
	b.writePos = (b.writePos + n) % b.capacity
	b.buffered += n
}

func (b *tcpOutputBuffer) availableLocked() int {
	return b.capacity - b.buffered
}

func (b *tcpOutputBuffer) updateReadyLocked() {
	if b.dropped {
		b.pollable.Drop()
		return
	}
	ready := b.err != nil || (!b.flushing && b.buffered < b.capacity)
	b.pollable.SetReady(ready)
}

// CheckWrite returns the number of bytes permitted for the next Write.
// It never blocks. A flushing stream reports 0 until queued and inflight
// bytes drain. Terminal errors are reported as StreamError LastOpFailed;
// Drop reports StreamError Closed.
func (b *tcpOutputBuffer) CheckWrite() (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.dropped {
		b.lastPermit = 0
		return 0, &StreamError{Closed: true}
	}
	if b.err != nil {
		b.lastPermit = 0
		return 0, &StreamError{LastOpFailed: true}
	}
	if b.flushing {
		b.lastPermit = 0
		return 0, nil
	}

	avail := uint64(b.availableLocked())
	b.lastPermit = avail
	return avail, nil
}

// Write copies data into the ring and returns promptly. It never calls
// net.Write. A payload larger than the last CheckWrite permit returns
// ErrWritePermit with no side effects.
func (b *tcpOutputBuffer) Write(data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.dropped {
		return &StreamError{Closed: true}
	}
	if b.err != nil {
		return &StreamError{LastOpFailed: true}
	}
	if uint64(len(data)) > b.lastPermit {
		return ErrWritePermit
	}
	if len(data) == 0 {
		return nil
	}

	b.enqueueLocked(data)
	b.lastPermit = 0
	b.cond.Signal()
	b.updateReadyLocked()
	return nil
}

// Flush marks the stream pending until all queued and inflight bytes drain.
// It never blocks. While a flush is pending, CheckWrite returns 0 and
// readiness is false. Readiness returns when there is room or a terminal error.
func (b *tcpOutputBuffer) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.dropped {
		return &StreamError{Closed: true}
	}
	if b.err != nil {
		return &StreamError{LastOpFailed: true}
	}

	b.lastPermit = 0
	if b.buffered == 0 {
		b.flushing = false
		b.updateReadyLocked()
		return nil
	}

	b.flushing = true
	b.updateReadyLocked()
	return nil
}

// Ready reports whether the stream can accept writes, has a terminal error, or is dropped.
func (b *tcpOutputBuffer) Ready() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pollable.Ready()
}

// Notify returns a notification channel that closes when readiness transitions to true.
func (b *tcpOutputBuffer) Notify() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pollable.Notify()
}

// Drop marks the buffer dropped and terminal ready, discards pending data,
// wakes the pump, and interrupts a blocked network write via CloseWrite if
// supported else SetWriteDeadline(now). It does not close the connection,
// allowing the read side to survive. Drop does not wait for the pump.
func (b *tcpOutputBuffer) Drop() {
	b.mu.Lock()
	if b.dropped {
		b.mu.Unlock()
		return
	}
	b.dropped = true
	b.flushing = false
	b.buffered = 0
	b.buf = nil
	b.readPos = 0
	b.writePos = 0
	b.lastPermit = 0
	b.cond.Broadcast()
	b.pollable.Drop()
	conn := b.conn
	b.mu.Unlock()

	if conn != nil {
		if cw, ok := conn.(closeWriter); ok {
			if cw.CloseWrite() == nil {
				return
			}
		}
		_ = conn.SetWriteDeadline(time.Now())
	}
}

// WaitClosed joins the background write pump. It must only be called when the
// socket owner has closed the connection. Drop alone may not interrupt a custom connection.
func (b *tcpOutputBuffer) WaitClosed() {
	<-b.doneCh
}
