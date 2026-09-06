package preview2

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/resource"
)

// fakeTrackedCloser tracks Close calls and allows verifying disposal.
type fakeTrackedCloser struct {
	closeErr   error
	closeCount atomic.Int32
}

func (c *fakeTrackedCloser) Close() error {
	c.closeCount.Add(1)
	return c.closeErr
}

// fakeConn implements net.Conn and tracks Close calls.
type fakeConn struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	fakeTrackedCloser
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		localAddr:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345},
		remoteAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080},
	}
}

func (c *fakeConn) Read(b []byte) (n int, err error)   { return 0, io.EOF }
func (c *fakeConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (c *fakeConn) LocalAddr() net.Addr                { return c.localAddr }
func (c *fakeConn) RemoteAddr() net.Addr               { return c.remoteAddr }
func (c *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

// fakeListener implements net.Listener and tracks Close calls.
type fakeListener struct {
	addr net.Addr
	fakeTrackedCloser
}

func newFakeListener() *fakeListener {
	return &fakeListener{
		addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080},
	}
}

func (l *fakeListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (l *fakeListener) Addr() net.Addr            { return l.addr }

// fakeOperation implements TCPNetworkOperation with inspection and coordination hooks.
type fakeOperation struct {
	result        io.Closer
	err           error
	takeRetCloser io.Closer
	notifyCh      chan struct{}
	closeHook     func()
	takeHook      func()
	mu            sync.Mutex
	closeCount    atomic.Int32
	closed        atomic.Bool
	taken         atomic.Bool
	ready         bool
}

func newFakeOperation(result io.Closer, err error, ready bool) *fakeOperation {
	op := &fakeOperation{
		result:   result,
		err:      err,
		ready:    ready,
		notifyCh: make(chan struct{}),
	}
	if ready {
		close(op.notifyCh)
	}
	return op
}

func (op *fakeOperation) Ready() bool {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.ready || op.closed.Load()
}

func (op *fakeOperation) Notify() <-chan struct{} {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.notifyCh
}

func (op *fakeOperation) setReady(ready bool) {
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.ready == ready {
		return
	}
	op.ready = ready
	if ready {
		select {
		case <-op.notifyCh:
		default:
			close(op.notifyCh)
		}
	}
}

func (op *fakeOperation) Take() (io.Closer, error, bool) { //nolint:revive // Matches TCPNetworkOperation readiness-last contract.
	if op.takeHook != nil {
		op.takeHook()
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.ready || op.closed.Load() {
		return nil, nil, false
	}
	op.taken.Store(true)
	res := op.result
	op.takeRetCloser = res
	op.result = nil
	return res, op.err, true
}

func (op *fakeOperation) Close() error {
	op.closeCount.Add(1)
	op.closed.Store(true)

	op.mu.Lock()
	select {
	case <-op.notifyCh:
	default:
		close(op.notifyCh)
	}
	res := op.result
	op.result = nil
	op.mu.Unlock()

	if res != nil {
		_ = res.Close()
	}

	if op.closeHook != nil {
		op.closeHook()
	}
	return nil
}

// -----------------------------------------------------------------------------
// Attachment Tests: nil, repeated, late, invalid state
// -----------------------------------------------------------------------------

func TestTCPSocket_SetPendingOperation_NilAndStates(t *testing.T) {
	// 1. Nil op rejection: op is nil, rejected
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)
	if err := sock.SetPendingOperation(nil); err == nil {
		t.Fatal("expected error on nil pending op")
	}

	// 2. Invalid state rejection: unbound
	sockUnbound := NewTCPSocketResource(4)
	op := newFakeOperation(nil, nil, false)
	if err := sockUnbound.SetPendingOperation(op); err == nil {
		t.Fatal("expected error attaching to unbound socket")
	}
	if op.closed.Load() {
		t.Fatal("rejected op must remain caller-owned and not closed")
	}

	// 3. Invalid state rejection: bound
	sockBound := NewTCPSocketResource(4)
	sockBound.SetState(TCPStateBound)
	opBound := newFakeOperation(nil, nil, false)
	if err := sockBound.SetPendingOperation(opBound); err == nil {
		t.Fatal("expected error attaching to bound socket")
	}
	if opBound.closed.Load() {
		t.Fatal("rejected op must remain caller-owned")
	}

	// 4. Invalid state rejection: connected
	sockConn := NewTCPSocketResource(4)
	sockConn.SetState(TCPStateConnected)
	opConn := newFakeOperation(nil, nil, false)
	if err := sockConn.SetPendingOperation(opConn); err == nil {
		t.Fatal("expected error attaching to connected socket")
	}
	if opConn.closed.Load() {
		t.Fatal("rejected op must remain caller-owned")
	}
}

func TestTCPSocket_SetPendingOperation_DuplicateAttach(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	op1 := newFakeOperation(nil, nil, false)
	if err := sock.SetPendingOperation(op1); err != nil {
		t.Fatalf("unexpected error on first attach: %v", err)
	}
	if sock.PendingOperation() != op1 {
		t.Fatal("expected op1 to be attached")
	}

	// Duplicate attach must fail
	op2 := newFakeOperation(nil, nil, false)
	if err := sock.SetPendingOperation(op2); err == nil {
		t.Fatal("expected error on duplicate attach")
	}
	if op2.closed.Load() {
		t.Fatal("rejected duplicate op must remain caller-owned and not closed")
	}
	if sock.PendingOperation() != op1 {
		t.Fatal("op1 must remain attached after duplicate rejection")
	}
}

func TestTCPSocket_SetPendingOperation_LateAttachOnDroppedSocket(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)
	sock.Drop()

	conn := newFakeConn()
	op := newFakeOperation(conn, nil, false)

	err := sock.SetPendingOperation(op)
	if !errors.Is(err, resource.ErrClosed) {
		t.Fatalf("expected ErrClosed on dropped socket attach, got: %v", err)
	}

	// Clearly documented behavior: dropped socket closes late attached operation
	// to prevent resource leaks.
	if !op.closed.Load() {
		t.Fatal("expected dropped socket to close late-attached op")
	}
	if conn.closeCount.Load() != 1 {
		t.Fatalf("expected underlying conn to be closed once, got %d", conn.closeCount.Load())
	}
}

func TestTCPSocket_SetPendingOperation_AlreadyResolved(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)
	sock.SetConn(newFakeConn())

	op := newFakeOperation(nil, nil, false)
	if err := sock.SetPendingOperation(op); err == nil {
		t.Fatal("expected error attaching when conn is already installed")
	}
	if op.closed.Load() {
		t.Fatal("rejected op must remain caller-owned")
	}
}

// -----------------------------------------------------------------------------
// Readiness and Notification Tests
// -----------------------------------------------------------------------------

func TestTCPSocket_ReadinessAndNotify(t *testing.T) {
	// 1. Terminal readiness: Dropped socket
	sockDrop := NewTCPSocketResource(4)
	sockDrop.Drop()
	if !sockDrop.Ready() {
		t.Fatal("dropped socket must be ready")
	}
	select {
	case <-sockDrop.Notify():
	default:
		t.Fatal("dropped socket Notify() channel must be closed")
	}

	// 2. Terminal readiness: Closed state
	sockClosed := NewTCPSocketResource(4)
	sockClosed.SetState(TCPStateClosed)
	if !sockClosed.Ready() {
		t.Fatal("closed socket must be ready")
	}
	select {
	case <-sockClosed.Notify():
	default:
		t.Fatal("closed socket Notify() channel must be closed")
	}

	// 3. Terminal readiness: Pending error
	sockErr := NewTCPSocketResource(4)
	sockErr.SetState(TCPStateConnectInProgress)
	sockErr.SetPendingError(errors.New("connection failed"))
	if !sockErr.Ready() {
		t.Fatal("socket with pending error must be ready")
	}
	select {
	case <-sockErr.Notify():
	default:
		t.Fatal("socket with pending error Notify() channel must be closed")
	}

	// 4. Idle readiness: Unbound, Bound, Connected
	for _, st := range []TCPState{TCPStateUnbound, TCPStateBound, TCPStateConnected} {
		sockIdle := NewTCPSocketResource(4)
		sockIdle.SetState(st)
		if !sockIdle.Ready() {
			t.Fatalf("idle state %d must be ready", st)
		}
		select {
		case <-sockIdle.Notify():
		default:
			t.Fatalf("idle state %d Notify() must be closed", st)
		}
	}

	// 5. In-progress connect: op readiness transitions
	sockConn := NewTCPSocketResource(4)
	sockConn.SetState(TCPStateConnectInProgress)
	op := newFakeOperation(newFakeConn(), nil, false)
	if err := sockConn.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	if sockConn.Ready() {
		t.Fatal("connect in progress with unready op must not be ready")
	}
	ch := sockConn.Notify()
	select {
	case <-ch:
		t.Fatal("Notify() should be open before op is ready")
	default:
	}

	// Signal op readiness
	op.setReady(true)
	if !sockConn.Ready() {
		t.Fatal("socket must be ready once op is ready")
	}
	select {
	case <-ch:
	default:
		t.Fatal("Notify() must be closed once op is ready")
	}

	// 6. Listening accept queue readiness
	sockListen := NewTCPSocketResource(4)
	sockListen.SetState(TCPStateListening)
	listener := newFakeListener()
	budget := NewSocketBudget(10)
	q := NewTCPAcceptQueue(listener, budget, 5)
	defer q.Drop()

	if err := sockListen.SetAcceptQueue(q); err != nil {
		t.Fatal(err)
	}

	// Queue is empty -> not ready
	if sockListen.Ready() {
		t.Fatal("listening socket with empty queue must not be ready")
	}
	listenCh := sockListen.Notify()
	select {
	case <-listenCh:
		t.Fatal("listening socket Notify() should be open when queue empty")
	default:
	}

	// Dropping queue makes it ready
	q.Drop()
	if !sockListen.Ready() {
		t.Fatal("listening socket must be ready after accept queue dropped")
	}
	select {
	case <-listenCh:
	default:
		t.Fatal("listening socket Notify() must close after queue dropped")
	}
}

func TestTCPSocket_NotifyNoMissedWakesBeforeAttach(t *testing.T) {
	// A consumer calls Notify() before SetPendingOperation is called
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	ch := sock.Notify()
	select {
	case <-ch:
		t.Fatal("should not be closed yet")
	default:
	}

	// Attach operation -> closes previous notifyCh to wake consumer
	op := newFakeOperation(newFakeConn(), nil, false)
	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
		// Woke up successfully!
	case <-time.After(time.Second):
		t.Fatal("missed wake when pending operation was attached")
	}

	// Next Notify() returns op's notification channel
	ch2 := sock.Notify()
	select {
	case <-ch2:
		t.Fatal("ch2 should not be closed yet")
	default:
	}

	op.setReady(true)
	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("missed wake when op became ready")
	}
}

// -----------------------------------------------------------------------------
// Resolve Tests: Success, Error, Nil Success, Wrong Type, No Mutex in Close()
// -----------------------------------------------------------------------------

func TestTCPSocket_ResolvePendingConnect_Success(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	conn := newFakeConn()
	op := newFakeOperation(conn, nil, true)

	// Verify op.Close() is called outside socket lock
	closeSawUnlocked := false
	op.closeHook = func() {
		// If sock.mu was locked by caller of Close, State() would deadlock.
		_ = sock.State()
		closeSawUnlocked = true
	}

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolved, err := sock.ResolvePendingConnect()
	if err != nil || !resolved {
		t.Fatalf("expected resolved=true, err=nil; got resolved=%v, err=%v", resolved, err)
	}

	// State must NOT have changed (W1 finish handles state)
	if sock.State() != TCPStateConnectInProgress {
		t.Fatalf("expected state to remain ConnectInProgress, got %d", sock.State())
	}

	// Conn must be installed
	if sock.Conn() != conn {
		t.Fatal("expected conn to be installed on socket")
	}
	if sock.LocalAddr() != "127.0.0.1" || sock.LocalPort() != 12345 {
		t.Fatalf("expected local addr 127.0.0.1:12345, got %s:%d", sock.LocalAddr(), sock.LocalPort())
	}
	if sock.RemoteAddr() != "127.0.0.1" || sock.RemotePort() != 8080 {
		t.Fatalf("expected remote addr 127.0.0.1:8080, got %s:%d", sock.RemoteAddr(), sock.RemotePort())
	}

	// Pending op must be cleared and closed outside lock
	if sock.PendingOperation() != nil {
		t.Fatal("expected pending op to be detached")
	}
	if !op.closed.Load() || !closeSawUnlocked {
		t.Fatal("expected pending op to be closed outside socket lock")
	}
	// Conn must NOT be closed!
	if conn.closeCount.Load() != 0 {
		t.Fatal("transferred conn must not be closed")
	}
}

func TestTCPSocket_ResolvePendingConnect_NotReady(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	op := newFakeOperation(newFakeConn(), nil, false)
	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolved, err := sock.ResolvePendingConnect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Fatal("expected resolved=false when op is not ready")
	}
	if sock.PendingOperation() != op {
		t.Fatal("pending op must remain attached when not ready")
	}
}

func TestTCPSocket_ResolvePendingConnect_WrongTypeDisposal(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	// Op returns a closer that is NOT net.Conn
	wrongCloser := &fakeTrackedCloser{}
	op := newFakeOperation(wrongCloser, nil, true)

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolved, err := sock.ResolvePendingConnect()
	if resolved {
		t.Fatal("expected resolved=false on wrong type")
	}
	if err == nil {
		t.Fatal("expected error on wrong type")
	}

	// Result must be closed immediately to prevent leaks
	if wrongCloser.closeCount.Load() != 1 {
		t.Fatalf("expected wrong closer to be closed once, got %d", wrongCloser.closeCount.Load())
	}

	// Pending op must be cleaned up
	if sock.PendingOperation() != nil {
		t.Fatal("pending op must be detached on error")
	}
	if !op.closed.Load() {
		t.Fatal("op must be closed after error")
	}
}

func TestTCPSocket_ResolvePendingConnect_NilSuccess(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	// Op returns nil closer with nil error
	op := newFakeOperation(nil, nil, true)
	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolved, err := sock.ResolvePendingConnect()
	if resolved {
		t.Fatal("expected resolved=false on nil success")
	}
	if err == nil {
		t.Fatal("expected error on nil success")
	}
	if sock.PendingOperation() != nil {
		t.Fatal("pending op must be detached on error")
	}
}

func TestTCPSocket_ResolvePendingConnect_OperationError(t *testing.T) {
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	expectedErr := errors.New("connection refused")
	leakedCloser := &fakeTrackedCloser{}
	op := newFakeOperation(leakedCloser, expectedErr, true)

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolved, err := sock.ResolvePendingConnect()
	if resolved {
		t.Fatal("expected resolved=false on op error")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	// Closer returned with error must be closed
	if leakedCloser.closeCount.Load() != 1 {
		t.Fatalf("expected leaked closer to be closed, got %d", leakedCloser.closeCount.Load())
	}

	if !errors.Is(sock.PendingError(), expectedErr) {
		t.Fatalf("expected PendingError to be set, got %v", sock.PendingError())
	}
}

func TestTCPSocket_ResolvePendingListen_SuccessAndWrongType(t *testing.T) {
	// 1. Success
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateListenInProgress)

	listener := newFakeListener()
	op := newFakeOperation(listener, nil, true)

	closeSawUnlocked := false
	op.closeHook = func() {
		_ = sock.State()
		closeSawUnlocked = true
	}

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolved, err := sock.ResolvePendingListen()
	if err != nil || !resolved {
		t.Fatalf("expected resolved=true, err=nil; got resolved=%v, err=%v", resolved, err)
	}
	if sock.State() != TCPStateListenInProgress {
		t.Fatalf("expected state to remain ListenInProgress, got %d", sock.State())
	}
	if sock.Listener() != listener {
		t.Fatal("expected listener to be installed")
	}
	if sock.LocalPort() != 8080 {
		t.Fatalf("expected local port 8080, got %d", sock.LocalPort())
	}
	if !op.closed.Load() || !closeSawUnlocked {
		t.Fatal("expected op to be closed outside lock")
	}
	if listener.closeCount.Load() != 0 {
		t.Fatal("transferred listener must not be closed")
	}

	// 2. Wrong type (e.g. conn passed instead of listener)
	sock2 := NewTCPSocketResource(4)
	sock2.SetState(TCPStateListenInProgress)
	wrongConn := newFakeConn()
	opWrong := newFakeOperation(wrongConn, nil, true)
	if err := sock2.SetPendingOperation(opWrong); err != nil {
		t.Fatal(err)
	}

	resolved2, err2 := sock2.ResolvePendingListen()
	if resolved2 || err2 == nil {
		t.Fatalf("expected resolved=false and error on wrong listener type")
	}
	if wrongConn.closeCount.Load() != 1 {
		t.Fatalf("expected wrong closer to be closed immediately")
	}
}

// -----------------------------------------------------------------------------
// Deterministic Concurrency Races: Resolve vs Drop & Quota Lifecycle
// -----------------------------------------------------------------------------

func TestTCPSocket_Race_DropWins_NoOrphanedSocketAndNoEarlyQuotaRelease(t *testing.T) {
	// Setup budget with capacity 1
	budget := NewSocketBudget(1)
	table := NewResourceTableWithBudget(10, budget)

	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	handle, err := table.TryAdd(sock)
	if err != nil {
		t.Fatalf("failed to add socket: %v", err)
	}
	if budget.Available() != 0 {
		t.Fatalf("expected 0 available slots in budget, got %d", budget.Available())
	}

	conn := newFakeConn()
	op := newFakeOperation(conn, nil, true)

	// Step 1: Drop closes op outside lock. We use closeHook to verify that
	// while op.Close() is running, the socket quota has NOT been released!
	dropCloseStarted := make(chan struct{})
	dropCloseBlock := make(chan struct{})
	quotaChecked := make(chan struct{})

	op.closeHook = func() {
		close(dropCloseStarted)
		<-dropCloseBlock
	}

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	// Launch Drop in goroutine
	dropDone := make(chan struct{})
	go func() {
		defer close(dropDone)
		table.Remove(handle)
	}()

	// Wait until Drop enters op.Close()
	<-dropCloseStarted

	// At this point, Drop is executing pendingOp.Close() outside socket lock.
	// Verify that quota has NOT been released early!
	if budget.Available() != 0 {
		t.Fatalf("QUOTA RELEASED EARLY! Expected 0 available slots while op.Close() running, got %d", budget.Available())
	}
	close(quotaChecked)

	// Concurrent ResolvePendingConnect while Drop holds/held socket
	resolved, resErr := sock.ResolvePendingConnect()
	if resolved {
		t.Fatal("ResolvePendingConnect should have failed on dropped socket")
	}
	if !errors.Is(resErr, resource.ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", resErr)
	}

	// Unblock op.Close() so Drop can finish
	close(dropCloseBlock)
	<-dropDone

	// Now that Drop finished, the socket quota should be released
	if budget.Available() != 1 {
		t.Fatalf("expected 1 available slot after Drop finished, got %d", budget.Available())
	}

	// Physical connection must be closed exactly once (no orphaned socket)
	if conn.closeCount.Load() != 1 {
		t.Fatalf("expected physical socket to be closed exactly once, got %d", conn.closeCount.Load())
	}
}

func TestTCPSocket_Race_ResolveWins_NoOrphanedSocketAndNoEarlyQuotaRelease(t *testing.T) {
	budget := NewSocketBudget(1)
	table := NewResourceTableWithBudget(10, budget)

	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	handle, err := table.TryAdd(sock)
	if err != nil {
		t.Fatalf("failed to add socket: %v", err)
	}

	conn := newFakeConn()
	op := newFakeOperation(conn, nil, true)

	// Synchronize so Resolve executes Take and installs conn,
	// while Drop waits for lock and runs immediately after.
	takeStarted := make(chan struct{})
	dropProceed := make(chan struct{})

	op.takeHook = func() {
		close(takeStarted)
		// Wait for Drop goroutine to be actively attempting to acquire sock.mu
		<-dropProceed
	}

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolveDone := make(chan struct{})
	var resolved bool
	var resolveErr error

	go func() {
		defer close(resolveDone)
		resolved, resolveErr = sock.ResolvePendingConnect()
	}()

	<-takeStarted

	// Start Drop in another goroutine while Resolve holds sock.mu across Take + install
	dropDone := make(chan struct{})
	go func() {
		defer close(dropDone)
		table.Remove(handle)
	}()

	// Let Resolve finish its atomic installation and unlock
	time.Sleep(10 * time.Millisecond)
	close(dropProceed)

	<-resolveDone
	if !resolved || resolveErr != nil {
		t.Fatalf("expected resolve success, got resolved=%v, err=%v", resolved, resolveErr)
	}

	<-dropDone

	// Drop has completed:
	// 1. Quota is now released
	if budget.Available() != 1 {
		t.Fatalf("expected budget available=1 after Drop, got %d", budget.Available())
	}
	// 2. Physical conn transferred to sock.conn was closed by Drop (no orphaned socket!)
	if conn.closeCount.Load() != 1 {
		t.Fatalf("expected physical socket to be closed exactly once by Drop, got %d", conn.closeCount.Load())
	}
}

func TestTCPSocket_Race_ResolveListenAndDrop(t *testing.T) {
	budget := NewSocketBudget(1)
	table := NewResourceTableWithBudget(10, budget)

	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateListenInProgress)

	handle, err := table.TryAdd(sock)
	if err != nil {
		t.Fatal(err)
	}

	listener := newFakeListener()
	op := newFakeOperation(listener, nil, true)

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolved, err := sock.ResolvePendingListen()
	if !resolved || err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	// Now drop the socket
	table.Remove(handle)

	if listener.closeCount.Load() != 1 {
		t.Fatalf("expected listener to be closed once by Drop, got %d", listener.closeCount.Load())
	}
	if budget.Available() != 1 {
		t.Fatalf("expected budget available=1, got %d", budget.Available())
	}
}

// -----------------------------------------------------------------------------
// Stress Race Test: Concurrent Resolve and Drop hammered
// -----------------------------------------------------------------------------

func TestTCPSocket_Stress_ResolveVsDrop(t *testing.T) {
	for i := 0; i < 200; i++ {
		budget := NewSocketBudget(1)
		table := NewResourceTableWithBudget(10, budget)

		sock := NewTCPSocketResource(4)
		sock.SetState(TCPStateConnectInProgress)

		handle, err := table.TryAdd(sock)
		if err != nil {
			t.Fatal(err)
		}

		conn := newFakeConn()
		op := newFakeOperation(conn, nil, true)

		if err := sock.SetPendingOperation(op); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_, _ = sock.ResolvePendingConnect()
		}()

		go func() {
			defer wg.Done()
			table.Remove(handle)
		}()

		wg.Wait()

		// Verify invariants:
		// 1. Physical connection closed exactly once (no leak / no double close)
		if conn.closeCount.Load() != 1 {
			t.Fatalf("iteration %d: expected conn close count 1, got %d", i, conn.closeCount.Load())
		}
		// 2. Budget quota fully restored
		if budget.Available() != 1 {
			t.Fatalf("iteration %d: expected budget available 1, got %d", i, budget.Available())
		}
	}
}

// -----------------------------------------------------------------------------
// Deterministic Error Gate Tests: Quota Preservation and Rejection During Cleanup
// -----------------------------------------------------------------------------

func TestTCPSocket_ResolvePendingConnect_ErrorGate_ReservationHeldUntilCloseJoined(t *testing.T) {
	testCases := []struct {
		name      string
		useRemove bool
	}{
		{name: "TableRemove", useRemove: true},
		{name: "TableClose", useRemove: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			budget := NewSocketBudget(1)
			table := NewResourceTableWithBudget(10, budget)

			sock := NewTCPSocketResource(4)
			sock.SetState(TCPStateConnectInProgress)

			handle, err := table.TryAdd(sock)
			if err != nil {
				t.Fatalf("failed to add socket: %v", err)
			}
			if budget.Available() != 0 {
				t.Fatalf("expected budget available=0, got %d", budget.Available())
			}

			closer := &fakeTrackedCloser{}
			expectedErr := errors.New("connection failed")
			op := newFakeOperation(closer, expectedErr, true)

			gate := make(chan struct{})
			closeStarted := make(chan struct{})
			var startOnce sync.Once

			op.closeHook = func() {
				startOnce.Do(func() {
					close(closeStarted)
				})
				<-gate
			}

			if err := sock.SetPendingOperation(op); err != nil {
				t.Fatalf("failed to set pending operation: %v", err)
			}

			resolveDone := make(chan struct{})
			var resolved bool
			var resolveErr error

			go func() {
				defer close(resolveDone)
				resolved, resolveErr = sock.ResolvePendingConnect()
			}()

			select {
			case <-closeStarted:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for op.Close to start")
			}

			// 1. Returned erroneous closer must physically close before ownership is detached.
			if closer.closeCount.Load() != 1 {
				t.Fatalf("expected erroneous closer to be closed during Take error processing, got %d", closer.closeCount.Load())
			}

			// 2. New pending operation must not attach during cleanup.
			newOp := newFakeOperation(nil, nil, false)
			if err := sock.SetPendingOperation(newOp); err == nil {
				t.Fatal("expected SetPendingOperation to fail while cleanup is in progress")
			}
			if newOp.closed.Load() {
				t.Fatal("rejected new operation must not be closed")
			}

			// 3. Resolve in progress then table Remove/Close must not return reservation until gate released.
			dropDone := make(chan struct{})
			go func() {
				defer close(dropDone)
				if tc.useRemove {
					table.Remove(handle)
				} else {
					_ = table.Close()
				}
			}()

			select {
			case <-dropDone:
				t.Fatal("table drop returned before gate was released")
			case <-time.After(50 * time.Millisecond):
			}

			if budget.Available() != 0 {
				t.Fatalf("quota released early! expected available=0 while gate blocked, got %d", budget.Available())
			}

			// Release the gate: op.Close finishes, Drop can finish, quota is returned.
			close(gate)

			select {
			case <-resolveDone:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for ResolvePendingConnect to finish")
			}

			select {
			case <-dropDone:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for table drop to finish")
			}

			if resolved {
				t.Fatal("expected resolved=false")
			}
			if !errors.Is(resolveErr, expectedErr) {
				t.Fatalf("expected resolve error %v, got %v", expectedErr, resolveErr)
			}
			if budget.Available() != 1 {
				t.Fatalf("expected budget available=1 after gate released and Drop finished, got %d", budget.Available())
			}
		})
	}
}

func TestTCPSocket_ResolvePendingListen_ErrorGate_ReservationHeldUntilCloseJoined(t *testing.T) {
	testCases := []struct {
		name      string
		useRemove bool
	}{
		{name: "TableRemove", useRemove: true},
		{name: "TableClose", useRemove: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			budget := NewSocketBudget(1)
			table := NewResourceTableWithBudget(10, budget)

			sock := NewTCPSocketResource(4)
			sock.SetState(TCPStateListenInProgress)

			handle, err := table.TryAdd(sock)
			if err != nil {
				t.Fatalf("failed to add socket: %v", err)
			}
			if budget.Available() != 0 {
				t.Fatalf("expected budget available=0, got %d", budget.Available())
			}

			closer := &fakeTrackedCloser{}
			expectedErr := errors.New("listen failed")
			op := newFakeOperation(closer, expectedErr, true)

			gate := make(chan struct{})
			closeStarted := make(chan struct{})
			var startOnce sync.Once

			op.closeHook = func() {
				startOnce.Do(func() {
					close(closeStarted)
				})
				<-gate
			}

			if err := sock.SetPendingOperation(op); err != nil {
				t.Fatalf("failed to set pending operation: %v", err)
			}

			resolveDone := make(chan struct{})
			var resolved bool
			var resolveErr error

			go func() {
				defer close(resolveDone)
				resolved, resolveErr = sock.ResolvePendingListen()
			}()

			select {
			case <-closeStarted:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for op.Close to start")
			}

			// 1. Returned erroneous closer must physically close before ownership is detached.
			if closer.closeCount.Load() != 1 {
				t.Fatalf("expected erroneous closer to be closed during Take error processing, got %d", closer.closeCount.Load())
			}

			// 2. New pending operation must not attach during cleanup.
			newOp := newFakeOperation(nil, nil, false)
			if err := sock.SetPendingOperation(newOp); err == nil {
				t.Fatal("expected SetPendingOperation to fail while cleanup is in progress")
			}
			if newOp.closed.Load() {
				t.Fatal("rejected new operation must not be closed")
			}

			// 3. Resolve in progress then table Remove/Close must not return reservation until gate released.
			dropDone := make(chan struct{})
			go func() {
				defer close(dropDone)
				if tc.useRemove {
					table.Remove(handle)
				} else {
					_ = table.Close()
				}
			}()

			select {
			case <-dropDone:
				t.Fatal("table drop returned before gate was released")
			case <-time.After(50 * time.Millisecond):
			}

			if budget.Available() != 0 {
				t.Fatalf("quota released early! expected available=0 while gate blocked, got %d", budget.Available())
			}

			// Release the gate: op.Close finishes, Drop can finish, quota is returned.
			close(gate)

			select {
			case <-resolveDone:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for ResolvePendingListen to finish")
			}

			select {
			case <-dropDone:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for table drop to finish")
			}

			if resolved {
				t.Fatal("expected resolved=false")
			}
			if !errors.Is(resolveErr, expectedErr) {
				t.Fatalf("expected resolve error %v, got %v", expectedErr, resolveErr)
			}
			if budget.Available() != 1 {
				t.Fatalf("expected budget available=1 after gate released and Drop finished, got %d", budget.Available())
			}
		})
	}
}

func TestTCPSocket_ResolvePendingError_NoDrop_PendingOpClearedAfterClose(t *testing.T) {
	// Verify that when no Drop occurs, pendingOp is cleared after Close finishes.
	sock := NewTCPSocketResource(4)
	sock.SetState(TCPStateConnectInProgress)

	expectedErr := errors.New("standalone connect error")
	closer := &fakeTrackedCloser{}
	op := newFakeOperation(closer, expectedErr, true)

	gate := make(chan struct{})
	closeStarted := make(chan struct{})
	var startOnce sync.Once

	op.closeHook = func() {
		startOnce.Do(func() {
			close(closeStarted)
		})
		<-gate
	}

	if err := sock.SetPendingOperation(op); err != nil {
		t.Fatal(err)
	}

	resolveDone := make(chan struct{})
	var resolved bool
	var resolveErr error

	go func() {
		defer close(resolveDone)
		resolved, resolveErr = sock.ResolvePendingConnect()
	}()

	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for op.Close to start")
	}

	// While blocked, pendingOp is still attached
	if sock.PendingOperation() != op {
		t.Fatal("pendingOp must remain attached during op.Close")
	}

	close(gate)

	select {
	case <-resolveDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resolve to finish")
	}

	if resolved || !errors.Is(resolveErr, expectedErr) {
		t.Fatalf("expected resolved=false, err=%v; got resolved=%v, err=%v", expectedErr, resolved, resolveErr)
	}

	// After Close finishes, pendingOp is cleared
	if sock.PendingOperation() != nil {
		t.Fatal("pendingOp must be cleared after op.Close finished")
	}
	if !errors.Is(sock.PendingError(), expectedErr) {
		t.Fatalf("expected PendingError=%v, got %v", expectedErr, sock.PendingError())
	}
}

func TestTCPSocket_Stress_ResolveErrorVsDrop(t *testing.T) {
	for i := 0; i < 200; i++ {
		budget := NewSocketBudget(1)
		table := NewResourceTableWithBudget(10, budget)

		sock := NewTCPSocketResource(4)
		sock.SetState(TCPStateConnectInProgress)

		handle, err := table.TryAdd(sock)
		if err != nil {
			t.Fatal(err)
		}

		closer := &fakeTrackedCloser{}
		op := newFakeOperation(closer, errors.New("transient error"), true)

		if err := sock.SetPendingOperation(op); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_, _ = sock.ResolvePendingConnect()
		}()

		go func() {
			defer wg.Done()
			table.Remove(handle)
		}()

		wg.Wait()

		if closer.closeCount.Load() != 1 {
			t.Fatalf("iteration %d: expected closer close count 1, got %d", i, closer.closeCount.Load())
		}
		if budget.Available() != 1 {
			t.Fatalf("iteration %d: expected budget available 1, got %d", i, budget.Available())
		}
	}
}
