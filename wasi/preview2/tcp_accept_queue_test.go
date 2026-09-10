package preview2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/resource"
)

// Helper to create a loopback TCP listener.
func newLoopbackListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on loopback: %v", err)
	}
	return l
}

func TestTCPAcceptQueue_LoopbackAcceptAndReadiness(t *testing.T) {
	listener := newLoopbackListener(t)
	budget := NewSocketBudget(10)
	queue := NewTCPAcceptQueue(listener, budget, 16)
	defer queue.Drop()

	// Initially empty: TryAccept returns ErrTCPAcceptWouldBlock
	conn, lease, err := queue.TryAccept()
	if !errors.Is(err, ErrTCPAcceptWouldBlock) {
		t.Fatalf("expected ErrTCPAcceptWouldBlock on empty queue, got %v", err)
	}
	if conn != nil || lease != nil {
		t.Fatalf("expected nil conn and lease on would block, got conn=%v lease=%v", conn, lease)
	}
	if queue.Ready() {
		t.Fatal("empty queue should not be ready")
	}

	notifyCh := queue.Notify()
	select {
	case <-notifyCh:
		t.Fatal("notifyCh should not be closed when empty")
	default:
	}

	// Dial loopback from client
	clientConn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial listener: %v", err)
	}
	defer clientConn.Close()

	// Wait for readiness notification
	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for accept queue notification")
	}

	if !queue.Ready() {
		t.Fatal("queue should be ready after accept")
	}

	// Dequeue connection
	serverConn, sLease, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept failed: %v", err)
	}
	if serverConn == nil || sLease == nil {
		t.Fatal("expected non-nil serverConn and sLease")
	}
	defer serverConn.Close()
	defer sLease.Release()

	// Now empty again: TryAccept must return ErrTCPAcceptWouldBlock and Ready() must be false
	if queue.Ready() {
		t.Fatal("queue should not be ready after draining single conn")
	}
	_, _, err = queue.TryAccept()
	if !errors.Is(err, ErrTCPAcceptWouldBlock) {
		t.Fatalf("expected ErrTCPAcceptWouldBlock after draining, got %v", err)
	}

	// Verify bidirectional communication over the accepted connection
	msg := []byte("hello accept queue")
	if _, err := clientConn.Write(msg); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(serverConn, buf); err != nil {
		t.Fatalf("server read failed: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("payload mismatch: expected %q, got %q", msg, buf)
	}
}

func TestTCPAcceptQueue_FullQueueNoExtraAccept(t *testing.T) {
	listener := newLoopbackListener(t)
	budget := NewSocketBudget(20)
	// Bounded capacity of 2
	queue := NewTCPAcceptQueue(listener, budget, 2)
	defer queue.Drop()

	var clients []net.Conn
	defer func() {
		for _, c := range clients {
			if c != nil {
				c.Close()
			}
		}
	}()

	// Connect client 1 and 2
	for i := 0; i < 2; i++ {
		c, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
		if err != nil {
			t.Fatalf("dial %d failed: %v", i, err)
		}
		clients = append(clients, c)
	}

	// Wait until queue has 2 items
	deadline := time.Now().Add(2 * time.Second)
	for queue.Queued() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for queue to fill; queued=%d", queue.Queued())
		}
		time.Sleep(5 * time.Millisecond)
	}

	if queue.Queued() != 2 {
		t.Fatalf("expected queue length 2, got %d", queue.Queued())
	}

	// Connect 3rd client: listener pump must NOT accept it into the queue!
	c3, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial 3 failed: %v", err)
	}
	clients = append(clients, c3)

	// Sleep slightly to give pump any chance to illegally accept if it were running
	time.Sleep(50 * time.Millisecond)

	if queue.Queued() > 2 {
		t.Fatalf("full queue accepted extra connection: queued=%d", queue.Queued())
	}

	// Dequeue 1 item
	conn1, lease1, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept 1 failed: %v", err)
	}
	defer conn1.Close()
	defer lease1.Release()

	// Now pump should wake up and accept the 3rd connection
	deadline = time.Now().Add(2 * time.Second)
	for queue.Queued() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for pump to accept 3rd client; queued=%d", queue.Queued())
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Dequeue remaining 2 items
	conn2, lease2, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept 2 failed: %v", err)
	}
	defer conn2.Close()
	defer lease2.Release()

	conn3, lease3, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept 3 failed: %v", err)
	}
	defer conn3.Close()
	defer lease3.Release()

	if queue.Queued() != 0 {
		t.Fatalf("expected empty queue, got %d", queue.Queued())
	}
}

func TestTCPAcceptQueue_QuotaExhaustionAndReleaseWakeup(t *testing.T) {
	listener := newLoopbackListener(t)
	// Budget capacity exactly 2!
	budget := NewSocketBudget(2)
	queue := NewTCPAcceptQueue(listener, budget, 10)
	defer queue.Drop()

	// Inflight accept reserves 1 budget slot BEFORE any connection arrives
	deadline := time.Now().Add(2 * time.Second)
	for budget.Used() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for inflight accept to reserve lease")
		}
		time.Sleep(5 * time.Millisecond)
	}

	var clients []net.Conn
	defer func() {
		for _, c := range clients {
			if c != nil {
				c.Close()
			}
		}
	}()

	// Connect client 1
	c1, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial 1 failed: %v", err)
	}
	clients = append(clients, c1)

	// Wait for connection 1 to be queued
	deadline = time.Now().Add(2 * time.Second)
	for queue.Queued() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for connection 1")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Connect client 2
	c2, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial 2 failed: %v", err)
	}
	clients = append(clients, c2)

	// Wait for connection 2 to be queued
	deadline = time.Now().Add(2 * time.Second)
	for queue.Queued() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for connection 2")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// At this point, budget used is 2 (capacity 2). Budget is EXHAUSTED!
	if budget.Available() != 0 {
		t.Fatalf("expected 0 available budget slots, got %d", budget.Available())
	}

	// Connect client 3: pump must NOT accept client 3 because budget is exhausted!
	c3, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial 3 failed: %v", err)
	}
	clients = append(clients, c3)

	// Wait a bit to ensure pump does not accept client 3
	time.Sleep(50 * time.Millisecond)
	if queue.Queued() > 2 {
		t.Fatalf("quota exhausted but queue accepted extra conn: queued=%d", queue.Queued())
	}

	// Dequeue conn1 and release its lease
	conn1, lease1, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept failed: %v", err)
	}
	_ = conn1.Close()
	lease1.Release()

	// Releasing lease1 must trigger live budget notification and wake the pump to accept client 3!
	deadline = time.Now().Add(2 * time.Second)
	for queue.Queued() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for pump to wake up on budget release; queued=%d", queue.Queued())
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Dequeue conn 2 and conn 3
	conn2, lease2, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept conn2 failed: %v", err)
	}
	conn2.Close()
	lease2.Release()

	conn3, lease3, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept conn3 failed: %v", err)
	}
	conn3.Close()
	lease3.Release()

	// Dropping queue stops pump and releases any inflight reservation
	queue.Drop()
	if budget.Used() != 0 {
		t.Fatalf("expected 0 used budget slots after queue drop, got %d", budget.Used())
	}
}

func TestTCPAcceptQueue_DropLateCompletionAndConsumerDequeuedConns(t *testing.T) {
	listener := newLoopbackListener(t)
	budget := NewSocketBudget(10)
	queue := NewTCPAcceptQueue(listener, budget, 10)

	// Connect a client that will be dequeued by consumer
	clientConn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer clientConn.Close()

	// Wait for item in queue
	deadline := time.Now().Add(2 * time.Second)
	for queue.Queued() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for client conn")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Consumer dequeues connection
	dequeuedConn, dequeuedLease, err := queue.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept failed: %v", err)
	}
	defer dequeuedConn.Close()
	defer dequeuedLease.Release()

	// Connect another client that remains queued when Drop is called
	clientConn2, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial 2 failed: %v", err)
	}
	defer clientConn2.Close()

	for queue.Queued() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for client 2 conn")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Now DROP the queue
	queue.Drop()

	// Drop must be idempotent
	queue.Drop()
	queue.Drop()

	// Verify pump has exited
	queue.WaitClosed()

	// Verify consumer-dequeued conn is STILL ALIVE and not closed!
	testPayload := []byte("survived drop")
	if _, err := dequeuedConn.Write(testPayload); err != nil {
		t.Fatalf("consumer-dequeued conn was closed by Drop: %v", err)
	}
	buf := make([]byte, len(testPayload))
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatalf("client read failed on consumer conn: %v", err)
	}
	if string(buf) != string(testPayload) {
		t.Fatalf("unexpected payload: %s", buf)
	}

	// Subsequent TryAccept on dropped queue returns ErrTCPAcceptClosed
	_, _, err = queue.TryAccept()
	if !errors.Is(err, ErrTCPAcceptClosed) {
		t.Fatalf("expected ErrTCPAcceptClosed, got %v", err)
	}

	// Queued conn2 was closed by Drop before its lease was released.
	// clientConn2 should observe EOF/closed.
	_ = clientConn2.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	b := make([]byte, 1)
	_, readErr := clientConn2.Read(b)
	if readErr == nil {
		t.Fatal("expected queued connection to be closed by Drop, but read succeeded")
	}
}

func TestTCPAcceptQueue_TerminalErrorObservable(t *testing.T) {
	listener := newLoopbackListener(t)
	budget := NewSocketBudget(5)
	queue := NewTCPAcceptQueue(listener, budget, 5)
	defer queue.Drop()

	// Close listener externally to induce terminal error
	_ = listener.Close()

	// Pump should observe terminal error and mark pollable ready
	select {
	case <-queue.Notify():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal error readiness notification")
	}

	if !queue.Ready() {
		t.Fatal("queue should be ready on terminal error")
	}

	_, _, err := queue.TryAccept()
	if err == nil || errors.Is(err, ErrTCPAcceptWouldBlock) {
		t.Fatalf("expected terminal error, got %v", err)
	}
}

func TestResourceTable_TryAddWithSocketLease(t *testing.T) {
	socketBudget := NewSocketBudget(5)
	table := NewResourceTableWithBudget(10, socketBudget)

	// 1. Valid adoption: acquire lease first, then adopt
	lease, err := socketBudget.Acquire()
	if err != nil {
		t.Fatalf("acquire lease failed: %v", err)
	}
	if socketBudget.Used() != 1 {
		t.Fatalf("expected 1 used, got %d", socketBudget.Used())
	}

	sock := NewTCPSocketResource(4)
	handle, err := table.TryAddWithSocketLease(sock, lease)
	if err != nil {
		t.Fatalf("TryAddWithSocketLease failed: %v", err)
	}
	if handle == 0 {
		t.Fatal("expected non-zero handle")
	}

	// Verify no double charging: budget used count must still be 1!
	if socketBudget.Used() != 1 {
		t.Fatalf("expected budget used to remain 1 (no double charge), got %d", socketBudget.Used())
	}
	if !lease.IsAdopted() {
		t.Fatal("lease should be marked adopted")
	}

	// 2. Replay / Double adoption rejection
	sock2 := NewTCPSocketResource(4)
	_, err = table.TryAddWithSocketLease(sock2, lease)
	if !errors.Is(err, ErrLeaseAlreadyAdopted) {
		t.Fatalf("expected ErrLeaseAlreadyAdopted, got %v", err)
	}

	// 3. Reject non-socket resource type
	lease3, err := socketBudget.Acquire()
	if err != nil {
		t.Fatalf("acquire lease 3 failed: %v", err)
	}
	nonSocket := &PollableResource{}
	_, err = table.TryAddWithSocketLease(nonSocket, lease3)
	if !errors.Is(err, ErrLeaseTypeMismatch) {
		t.Fatalf("expected ErrLeaseTypeMismatch, got %v", err)
	}
	if lease3.IsAdopted() {
		t.Fatal("rejected lease should not be marked adopted")
	}
	lease3.Release()

	// 4. Reject expired / released lease
	lease4, err := socketBudget.Acquire()
	if err != nil {
		t.Fatalf("acquire lease 4 failed: %v", err)
	}
	lease4.Release()
	_, err = table.TryAddWithSocketLease(NewTCPSocketResource(4), lease4)
	if !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("expected ErrInvalidLease for released lease, got %v", err)
	}

	// 5. Reject budget mismatch
	otherBudget := NewSocketBudget(5)
	leaseOther, err := otherBudget.Acquire()
	if err != nil {
		t.Fatalf("acquire other lease failed: %v", err)
	}
	defer leaseOther.Release()
	_, err = table.TryAddWithSocketLease(NewTCPSocketResource(4), leaseOther)
	if !errors.Is(err, ErrLeaseBudgetMismatch) {
		t.Fatalf("expected ErrLeaseBudgetMismatch, got %v", err)
	}
	if leaseOther.IsAdopted() {
		t.Fatal("mismatched lease should not be marked adopted")
	}

	// 6. Rollback on handle limit exceeded: preserves caller ownership and lease
	tightTable := NewResourceTableWithBudget(1, socketBudget)
	// Fill handle limit
	tightHandle := tightTable.Add(NewTCPSocketResource(4))

	leaseRollback, err := socketBudget.Acquire()
	if err != nil {
		t.Fatalf("acquire rollback lease failed: %v", err)
	}
	sockRollback := NewTCPSocketResource(4)
	_, err = tightTable.TryAddWithSocketLease(sockRollback, leaseRollback)
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("expected ErrResourceLimit, got %v", err)
	}
	// Caller retains ownership: lease must NOT be released and adoption rolled back!
	if leaseRollback.IsReleased() {
		t.Fatal("lease should not be released on handle limit failure")
	}
	if leaseRollback.IsAdopted() {
		t.Fatal("lease adoption should be rolled back to false")
	}
	// Caller can now release it or use it
	leaseRollback.Release()
	tightTable.Remove(tightHandle)

	// 7. Rollback on closed table
	closedTable := NewResourceTableWithBudget(5, socketBudget)
	closedTable.Close()

	leaseClosed, err := socketBudget.Acquire()
	if err != nil {
		t.Fatalf("acquire leaseClosed failed: %v", err)
	}
	_, err = closedTable.TryAddWithSocketLease(NewTCPSocketResource(4), leaseClosed)
	if !errors.Is(err, resource.ErrClosed) {
		t.Fatalf("expected resource.ErrClosed, got %v", err)
	}
	if leaseClosed.IsReleased() {
		t.Fatal("lease should not be released on closed table failure")
	}
	if leaseClosed.IsAdopted() {
		t.Fatal("lease adoption should be rolled back on closed table failure")
	}
	leaseClosed.Release()

	// 8. Dropping table entry releases adopted lease
	table.Remove(handle)
	if socketBudget.Used() != 0 {
		t.Fatalf("expected 0 used budget slots after table Remove, got %d", socketBudget.Used())
	}
	if !lease.IsReleased() {
		t.Fatal("adopted lease should be released when table entry dropped")
	}
}

func TestTCPSocketResource_AcceptQueueIntegration(t *testing.T) {
	listener := newLoopbackListener(t)
	budget := NewSocketBudget(5)
	queue := NewTCPAcceptQueue(listener, budget, 5)

	socket := NewTCPSocketResource(4)
	if err := socket.SetAcceptQueue(queue); err != nil {
		t.Fatalf("SetAcceptQueue failed: %v", err)
	}
	if socket.AcceptQueue() != queue {
		t.Fatal("AcceptQueue did not return attached queue")
	}

	// Reject duplicate attach with different queue
	listener2 := newLoopbackListener(t)
	queue2 := NewTCPAcceptQueue(listener2, budget, 5)
	defer queue2.Drop()
	if err := socket.SetAcceptQueue(queue2); err == nil {
		t.Fatal("expected error attaching second queue")
	}

	// Dropping socket drops the accept queue
	socket.Drop()

	// Verify queue was dropped and joined
	queue.WaitClosed()
	if !queue.Ready() {
		t.Fatal("dropped queue should be ready")
	}
	_, _, err := queue.TryAccept()
	if !errors.Is(err, ErrTCPAcceptClosed) {
		t.Fatalf("expected ErrTCPAcceptClosed after socket drop, got %v", err)
	}

	// Reject late attach on dropped socket
	listener3 := newLoopbackListener(t)
	queue3 := NewTCPAcceptQueue(listener3, budget, 5)
	err = socket.SetAcceptQueue(queue3)
	if !errors.Is(err, resource.ErrClosed) {
		t.Fatalf("expected resource.ErrClosed on late attach, got %v", err)
	}
	// Verify late attached queue was dropped to prevent resource leak
	queue3.WaitClosed()
}

func TestTCPAcceptQueue_ConcurrentStressAndRace(t *testing.T) {
	listener := newLoopbackListener(t)
	budget := NewSocketBudget(8)
	queue := NewTCPAcceptQueue(listener, budget, 16)
	defer queue.Drop()

	const totalClients = 30
	var wg sync.WaitGroup
	var accepted atomic.Int32

	// Consumer goroutines
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				conn, lease, err := queue.TryAccept()
				if err == nil {
					accepted.Add(1)
					// Simulate some work
					time.Sleep(time.Millisecond)
					conn.Close()
					lease.Release()
					if accepted.Load() >= totalClients {
						return
					}
					continue
				}

				if errors.Is(err, ErrTCPAcceptWouldBlock) {
					select {
					case <-ctx.Done():
						return
					case <-queue.Notify():
					}
					continue
				}

				return
			}
		}()
	}

	// Producer / client dials
	for i := 0; i < totalClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener.Addr().String())
			if err != nil {
				return
			}
			defer c.Close()
			_, _ = fmt.Fprintf(c, "msg-%d", id)
			time.Sleep(2 * time.Millisecond)
		}(i)
	}

	wg.Wait()

	if accepted.Load() < totalClients {
		t.Fatalf("expected at least %d accepted, got %d", totalClients, accepted.Load())
	}
}

func TestTCPAcceptQueue_EdgeCasesAndCoverage(t *testing.T) {
	// 1. Nil listener
	nilQueue := NewTCPAcceptQueue(nil, nil, 0)
	if !nilQueue.Ready() {
		t.Fatal("nil listener queue should be ready/dropped immediately")
	}
	_, _, err := nilQueue.TryAccept()
	if !errors.Is(err, ErrTCPAcceptClosed) {
		t.Fatalf("expected ErrTCPAcceptClosed, got %v", err)
	}
	nilQueue.Drop()
	if nilQueue.Capacity() != DefaultAcceptQueueCapacity {
		t.Fatalf("expected default capacity %d, got %d", DefaultAcceptQueueCapacity, nilQueue.Capacity())
	}

	// 2. Capacity bounds: <= 0 and > MaxAcceptQueueCapacity
	listener := newLoopbackListener(t)
	q1 := NewTCPAcceptQueue(listener, nil, -5)
	if q1.Capacity() != DefaultAcceptQueueCapacity {
		t.Fatalf("expected default capacity %d, got %d", DefaultAcceptQueueCapacity, q1.Capacity())
	}
	_ = q1.Close()

	listener2 := newLoopbackListener(t)
	q2 := NewTCPAcceptQueue(listener2, nil, 1000)
	if q2.Capacity() != MaxAcceptQueueCapacity {
		t.Fatalf("expected clamped capacity %d, got %d", MaxAcceptQueueCapacity, q2.Capacity())
	}
	_ = q2.Close()

	// 3. Inspect getters: Listener, Budget, Close
	listener3 := newLoopbackListener(t)
	budget := NewSocketBudget(5)
	q3 := NewTCPAcceptQueue(listener3, budget, 32)
	if q3.Listener() != listener3 {
		t.Fatal("Listener mismatch")
	}
	if q3.Budget() != budget {
		t.Fatal("Budget mismatch")
	}

	// 4. Block with context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	q3.Block(ctx) // should return promptly on timeout

	// 5. Subscribe and pollable interface
	pollable := q3.Subscribe()
	if pollable.Type() != ResourcePollable {
		t.Fatalf("expected ResourcePollable, got %v", pollable.Type())
	}
	pollable.Drop() // no-op
	if pollable.Ready() != q3.Ready() {
		t.Fatal("pollable readiness mismatch")
	}
	if np, ok := pollable.(NotifyPollable); ok {
		_ = np.Notify()
	}

	// Connect client to verify Block unblocks on accept
	c, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(t.Context(), "tcp", listener3.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer c.Close()

	unblockCtx, unblockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer unblockCancel()
	pollable.Block(unblockCtx)
	if !pollable.Ready() {
		t.Fatal("pollable should be ready after accept")
	}

	sc, sl, err := q3.TryAccept()
	if err != nil {
		t.Fatalf("TryAccept failed: %v", err)
	}
	sc.Close()
	sl.Release()

	_ = q3.Close()

	// 6. ResourceTable AddWithSocketLease success & panic
	b := NewSocketBudget(2)
	tbl := NewResourceTableWithBudget(5, b)
	l, _ := b.Acquire()
	h := tbl.AddWithSocketLease(NewTCPSocketResource(4), l)
	if h == 0 {
		t.Fatal("expected non-zero handle")
	}
	tbl.Remove(h)

	// Panics on invalid lease
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on invalid lease adoption")
			}
		}()
		tbl.AddWithSocketLease(NewTCPSocketResource(4), nil)
	}()

	// 7. TryAddWithSocketLease nil resource
	_, err = tbl.TryAddWithSocketLease(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil resource")
	}

	// 8. SocketLease helpers on nil lease
	var nilLease *SocketLease
	nilLease.Release()
	if nilLease.Budget() != nil {
		t.Fatal("nil lease budget should be nil")
	}
	if nilLease.IsAdopted() {
		t.Fatal("nil lease should not be adopted")
	}
	if !nilLease.IsReleased() {
		t.Fatal("nil lease should report released")
	}

	// 9. SocketBudget AvailNotify and AcquireCancellable nil stop
	availCh := b.AvailNotify()
	select {
	case <-availCh:
	default:
		t.Fatal("AvailNotify should be closed when budget has available slots")
	}

	leaseAcq, err := b.AcquireCancellable(nil)
	if err != nil {
		t.Fatalf("AcquireCancellable with nil stop failed: %v", err)
	}
	if leaseAcq.Budget() != b {
		t.Fatal("lease budget mismatch")
	}
	leaseAcq.Release()
}
