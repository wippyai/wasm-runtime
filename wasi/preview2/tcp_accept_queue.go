package preview2

import (
	"context"
	"errors"
	"net"
	"sync"
)

const (
	// MaxAcceptQueueCapacity is the maximum allowed accept queue capacity.
	MaxAcceptQueueCapacity = 128

	// DefaultAcceptQueueCapacity is the default capacity if an invalid or zero capacity is given.
	DefaultAcceptQueueCapacity = 128
)

var (
	// ErrTCPAcceptWouldBlock indicates no accepted connections are currently queued.
	ErrTCPAcceptWouldBlock = errors.New("tcp accept would block")

	// ErrTCPAcceptClosed indicates the accept queue has been dropped or closed.
	ErrTCPAcceptClosed = errors.New("tcp accept queue closed")
)

type acceptedConn struct {
	conn  net.Conn
	lease *SocketLease
}

// TCPAcceptQueue owns a net.Listener and runs a single accept pump goroutine.
// It reserves capacity in SocketBudget BEFORE every underlying Accept (including inflight),
// bounds queue capacity (max 128), and provides nonblocking TryAccept with live readiness
// notification compatible with WASI polling.
type TCPAcceptQueue struct {
	listener net.Listener
	termErr  error
	budget   *SocketBudget
	doneCh   chan struct{}
	cond     *sync.Cond
	stopCh   chan struct{}
	pollable PollableResource
	queue    []acceptedConn
	capacity int
	head     int
	size     int
	dropOnce sync.Once
	mu       sync.Mutex
	dropped  bool
}

// NewTCPAcceptQueue creates a new bounded accept queue for listener with budget.
func NewTCPAcceptQueue(listener net.Listener, budget *SocketBudget, capacity int) *TCPAcceptQueue {
	if capacity <= 0 {
		capacity = DefaultAcceptQueueCapacity
	}
	if capacity > MaxAcceptQueueCapacity {
		capacity = MaxAcceptQueueCapacity
	}

	q := &TCPAcceptQueue{
		listener: listener,
		budget:   budget,
		capacity: capacity,
		queue:    make([]acceptedConn, capacity),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	q.cond = sync.NewCond(&q.mu)

	if listener == nil {
		q.dropped = true
		q.pollable.Drop()
		close(q.doneCh)
		q.dropOnce.Do(func() {})
		return q
	}

	go q.runPump()
	return q
}

func (q *TCPAcceptQueue) runPump() {
	defer close(q.doneCh)

	for {
		// Wait until queue has space or is dropped
		q.mu.Lock()
		for q.size >= q.capacity && !q.dropped {
			q.cond.Wait()
		}
		if q.dropped {
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		// Reserve a socket lease BEFORE calling listener.Accept.
		// If exhausted, wait cancellably on live budget availability notification, no polling timers.
		lease, err := q.budget.AcquireCancellable(q.stopCh)
		if err != nil {
			// Stop signaled (Drop called)
			return
		}

		// Double-check dropped state after acquiring lease
		q.mu.Lock()
		if q.dropped {
			q.mu.Unlock()
			if lease != nil {
				lease.Release()
			}
			return
		}
		q.mu.Unlock()

		conn, err := q.listener.Accept()
		if conn == nil && err == nil {
			err = ErrTCPAcceptClosed
		}
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			if lease != nil {
				lease.Release()
			}
			q.mu.Lock()
			if !q.dropped {
				q.termErr = err
				q.pollable.SetReady(true)
			}
			q.mu.Unlock()
			return
		}

		q.mu.Lock()
		if q.dropped {
			// Late connection after Drop: close conn BEFORE releasing lease!
			q.mu.Unlock()
			_ = conn.Close()
			if lease != nil {
				lease.Release()
			}
			return
		}

		q.queue[(q.head+q.size)%q.capacity] = acceptedConn{conn: conn, lease: lease}
		q.size++
		q.pollable.SetReady(true)
		q.mu.Unlock()
	}
}

// TryAccept dequeues an accepted connection and its associated budget lease nonblockingly.
// If open and empty, it returns ErrTCPAcceptWouldBlock.
// If dropped or terminated with an error and empty, it returns the terminal error or ErrTCPAcceptClosed.
func (q *TCPAcceptQueue) TryAccept() (net.Conn, *SocketLease, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.size > 0 {
		item := q.queue[q.head]
		q.queue[q.head] = acceptedConn{}
		q.head = (q.head + 1) % q.capacity
		q.size--

		if q.size == 0 && !q.dropped && q.termErr == nil {
			q.pollable.SetReady(false)
		}

		q.cond.Signal()
		return item.conn, item.lease, nil
	}

	if q.dropped {
		return nil, nil, ErrTCPAcceptClosed
	}

	if q.termErr != nil {
		return nil, nil, q.termErr
	}

	return nil, nil, ErrTCPAcceptWouldBlock
}

// Ready returns true if an accepted connection is ready to be dequeued,
// or if the queue is dropped / in terminal error state.
func (q *TCPAcceptQueue) Ready() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.size > 0 || q.dropped || q.termErr != nil
}

// Notify returns a channel that is closed if the queue is ready or dropped.
// If not ready, it returns a channel that will close when an accept occurs or the queue terminates.
func (q *TCPAcceptQueue) Notify() <-chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pollable.Notify()
}

// Block waits until the queue becomes ready, is dropped, or ctx is canceled.
func (q *TCPAcceptQueue) Block(ctx context.Context) {
	for !q.Ready() {
		select {
		case <-ctx.Done():
			return
		case <-q.Notify():
		}
	}
}

// Drop closes the listener, joins the pump, and closes queued and late connections
// BEFORE releasing their budget leases. It is idempotent and never closes consumer-dequeued connections.
func (q *TCPAcceptQueue) Drop() {
	q.dropOnce.Do(func() {
		q.mu.Lock()
		q.dropped = true
		close(q.stopCh)
		q.cond.Broadcast()
		q.pollable.Drop()
		listener := q.listener
		q.mu.Unlock()

		if listener != nil {
			_ = listener.Close()
		}

		// Wait for accept pump to exit
		<-q.doneCh

		// Pump has exited. Close queued conns BEFORE releasing leases.
		q.mu.Lock()
		queued := q.queue
		q.queue = nil
		q.size = 0
		q.listener = nil
		q.mu.Unlock()

		for _, item := range queued {
			if item.conn != nil {
				_ = item.conn.Close()
			}
			if item.lease != nil {
				item.lease.Release()
			}
		}
	})
}

// Close closes the accept queue, implementing io.Closer.
func (q *TCPAcceptQueue) Close() error {
	q.Drop()
	return nil
}

// Capacity returns the maximum queued connection limit.
func (q *TCPAcceptQueue) Capacity() int {
	return q.capacity
}

// Queued returns the current number of buffered accepted connections.
func (q *TCPAcceptQueue) Queued() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.size
}

// Listener returns the underlying listener.
func (q *TCPAcceptQueue) Listener() net.Listener {
	return q.listener
}

// Budget returns the associated SocketBudget, if any.
func (q *TCPAcceptQueue) Budget() *SocketBudget {
	return q.budget
}

// WaitClosed blocks until the accept pump goroutine has completely exited.
func (q *TCPAcceptQueue) WaitClosed() {
	<-q.doneCh
}

// Subscribe returns a WASI Pollable representation of this queue's readiness.
func (q *TCPAcceptQueue) Subscribe() Pollable {
	return (*tcpAcceptPollable)(q)
}

type tcpAcceptPollable TCPAcceptQueue

func (*tcpAcceptPollable) Type() ResourceType        { return ResourcePollable }
func (*tcpAcceptPollable) Drop()                     {}
func (p *tcpAcceptPollable) Ready() bool             { return (*TCPAcceptQueue)(p).Ready() }
func (p *tcpAcceptPollable) Notify() <-chan struct{} { return (*TCPAcceptQueue)(p).Notify() }
func (p *tcpAcceptPollable) Block(ctx context.Context) {
	(*TCPAcceptQueue)(p).Block(ctx)
}

var (
	_ Pollable       = (*tcpAcceptPollable)(nil)
	_ NotifyPollable = (*tcpAcceptPollable)(nil)
)
