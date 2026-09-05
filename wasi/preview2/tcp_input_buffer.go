package preview2

import (
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"time"
)

type closeReader interface {
	CloseRead() error
}

// tcpInputBuffer is an unexported bounded input buffer for TCP connections.
// It maintains a fixed bounded ring buffer allocated once, a single read pump
// goroutine, and synchronizes readiness state transitions under its stream mutex.
type tcpInputBuffer struct {
	conn     net.Conn
	err      error
	cond     *sync.Cond
	doneCh   chan struct{}
	buf      []byte
	pollable PollableResource
	capacity int
	readPos  int
	writePos int
	buffered int
	mu       sync.Mutex
	dropped  bool
}

func newTCPInputBuffer(conn net.Conn, capacity int) *tcpInputBuffer {
	if capacity <= 0 {
		capacity = 64 * 1024
	}
	if capacity > MaxAllocationSize {
		capacity = MaxAllocationSize
	}

	b := &tcpInputBuffer{
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

	go b.runPump()
	return b
}

func (b *tcpInputBuffer) runPump() {
	defer func() { b.mu.Lock(); b.conn = nil; b.mu.Unlock(); close(b.doneCh) }()
	emptyReads := 0

	for {
		b.mu.Lock()
		for b.buffered == b.capacity && !b.dropped {
			b.cond.Wait()
		}
		if b.dropped {
			b.mu.Unlock()
			return
		}

		if b.buffered == 0 {
			b.readPos = 0
			b.writePos = 0
		}

		var freeSlice []byte
		if b.writePos >= b.readPos {
			freeSlice = b.buf[b.writePos:b.capacity]
		} else {
			freeSlice = b.buf[b.writePos:b.readPos]
		}
		b.mu.Unlock()

		n, err := b.conn.Read(freeSlice)
		if n < 0 || n > len(freeSlice) {
			n = 0
			err = io.ErrNoProgress
		}
		if n == 0 && err == nil {
			emptyReads++
			if emptyReads >= 100 {
				err = io.ErrNoProgress
			}
		} else {
			emptyReads = 0
		}

		b.mu.Lock()
		if b.dropped {
			b.mu.Unlock()
			return
		}

		if n > 0 {
			b.writePos += n
			if b.writePos == b.capacity {
				b.writePos = 0
			}
			b.buffered += n
		}

		if err != nil {
			b.err = err
			b.updateReadyLocked()
			b.mu.Unlock()
			return
		}

		b.updateReadyLocked()
		b.mu.Unlock()

		if n == 0 {
			runtime.Gosched()
		}
	}
}

func (b *tcpInputBuffer) updateReadyLocked() {
	if b.dropped {
		b.pollable.Drop()
		return
	}
	ready := b.buffered > 0 || b.err != nil
	b.pollable.SetReady(ready)
}

// Read returns promptly without blocking on the network.
// If open with no bytes buffered, it returns an empty slice and nil error.
// If bytes are buffered, it delivers up to min(length, buffered) bytes before
// any accompanying EOF/error is returned on subsequent reads.
// When EOF is encountered and the buffer is drained, it returns StreamError{Closed: true}.
// On other errors, it returns StreamError{LastOpFailed: true}.
// A length of 0 does not consume buffered data.
func (b *tcpInputBuffer) Read(length uint64) ([]byte, error) {
	if length > MaxAllocationSize {
		length = MaxAllocationSize
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.dropped {
		return nil, &StreamError{Closed: true}
	}

	if b.buffered > 0 {
		if length == 0 {
			return []byte{}, nil
		}

		toRead := int(length)
		if toRead > b.buffered {
			toRead = b.buffered
		}

		out := make([]byte, toRead)
		first := b.capacity - b.readPos
		if first > toRead {
			first = toRead
		}
		copy(out, b.buf[b.readPos:b.readPos+first])
		if toRead > first {
			copy(out[first:], b.buf[:toRead-first])
		}
		b.readPos = (b.readPos + toRead) % b.capacity
		b.buffered -= toRead
		if b.buffered == 0 && b.err != nil {
			b.buf = nil
		}

		b.cond.Signal()
		b.updateReadyLocked()
		return out, nil
	}

	if b.err != nil {
		if errors.Is(b.err, io.EOF) {
			return nil, &StreamError{Closed: true}
		}
		return nil, &StreamError{LastOpFailed: true}
	}

	return []byte{}, nil
}

// Ready returns whether the buffer is ready for reading (buffered data available,
// terminal error/EOF reached, or dropped).
func (b *tcpInputBuffer) Ready() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pollable.Ready()
}

// Notify returns a notification channel that closes when readiness transitions to true.
func (b *tcpInputBuffer) Notify() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pollable.Notify()
}

// Drop marks the buffer dropped and terminal ready, discards all buffered data,
// wakes any pump goroutine waiting on a full buffer, and interrupts any blocked
// network read via CloseRead if supported else SetReadDeadline(now). It does NOT
// close the entire connection, allowing the write side to survive.
func (b *tcpInputBuffer) Drop() {
	b.mu.Lock()
	if b.dropped {
		b.mu.Unlock()
		return
	}
	b.dropped = true
	b.buffered = 0
	b.buf = nil
	b.readPos = 0
	b.writePos = 0
	b.cond.Broadcast()
	b.pollable.Drop()
	conn := b.conn
	b.mu.Unlock()

	if conn != nil {
		if cr, ok := conn.(closeReader); ok {
			if cr.CloseRead() == nil {
				return
			}
		}
		_ = conn.SetReadDeadline(time.Now())
	}
}

// WaitClosed joins the background read pump. It must only be called when the socket
// owner has closed the connection. Drop alone may not interrupt a custom connection.
func (b *tcpInputBuffer) WaitClosed() {
	<-b.doneCh
}
