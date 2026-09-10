package preview2

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// parkedDuplexConn gives tests independent, deterministic gates for each
// operation involved in socket teardown. It deliberately does not implement
// CloseRead or CloseWrite, and deadline calls are no-ops, so stream Drop cannot
// make a parked pump exit before the test permits it.
type parkedDuplexConn struct {
	readEntered  chan struct{}
	writeEntered chan struct{}
	closeEntered chan struct{}
	allowRead    chan struct{}
	allowWrite   chan struct{}
	allowClose   chan struct{}

	readOnce  sync.Once
	writeOnce sync.Once
	closeOnce sync.Once
}

func newParkedDuplexConn() *parkedDuplexConn {
	return &parkedDuplexConn{
		readEntered:  make(chan struct{}),
		writeEntered: make(chan struct{}),
		closeEntered: make(chan struct{}),
		allowRead:    make(chan struct{}),
		allowWrite:   make(chan struct{}),
		allowClose:   make(chan struct{}),
	}
}

func (c *parkedDuplexConn) Read([]byte) (int, error) {
	close(c.readEntered)
	<-c.allowRead
	return 0, net.ErrClosed
}

func (c *parkedDuplexConn) Write([]byte) (int, error) {
	close(c.writeEntered)
	<-c.allowWrite
	return 0, net.ErrClosed
}

func (c *parkedDuplexConn) Close() error {
	close(c.closeEntered)
	<-c.allowClose
	return nil
}

func (*parkedDuplexConn) LocalAddr() net.Addr              { return &net.IPAddr{} }
func (*parkedDuplexConn) RemoteAddr() net.Addr             { return &net.IPAddr{} }
func (*parkedDuplexConn) SetDeadline(time.Time) error      { return nil }
func (*parkedDuplexConn) SetReadDeadline(time.Time) error  { return nil }
func (*parkedDuplexConn) SetWriteDeadline(time.Time) error { return nil }
func (c *parkedDuplexConn) releaseClose()                  { c.closeOnce.Do(func() { close(c.allowClose) }) }
func (c *parkedDuplexConn) releasePumps() {
	c.readOnce.Do(func() { close(c.allowRead) })
	c.writeOnce.Do(func() { close(c.allowWrite) })
}
func (c *parkedDuplexConn) releaseAll() { c.releaseClose(); c.releasePumps() }

func requireSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func requireNotSignaled(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s unexpectedly ran", name)
	default:
	}
}

func TestHostBufferChargeWaitsForParkedPumpsAcrossConcurrentTeardown(t *testing.T) {
	hostBuffers := NewHostBufferBudget(2 * DefaultBufferSize)
	table := NewResourceTableWithBudgets(8, NewSocketBudget(1), hostBuffers)
	conn := newParkedDuplexConn()
	defer conn.releaseAll()

	socket := NewTCPSocketResource(0)
	socket.SetState(TCPStateConnected)
	socket.SetConn(conn)
	input, output, err := table.NewTCPDuplexStreams(socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := table.TryAdd(socket); err != nil {
		t.Fatal(err)
	}
	if _, err := table.TryAdd(input); err != nil {
		t.Fatal(err)
	}
	if _, err := table.TryAdd(output); err != nil {
		t.Fatal(err)
	}

	permit, err := output.CheckWrite()
	if err != nil || permit == 0 {
		t.Fatalf("write permit=%d err=%v", permit, err)
	}
	if err := output.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	requireSignal(t, conn.readEntered, "read pump")
	requireSignal(t, conn.writeEntered, "write pump")

	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(4)
	done.Add(4)
	streamDrop := func(drop func()) {
		defer done.Done()
		ready.Done()
		<-start
		drop()
	}
	go streamDrop(input.Drop)
	go streamDrop(output.Drop)
	socketDone := make(chan struct{})
	go func() {
		defer done.Done()
		ready.Done()
		<-start
		socket.Drop()
		close(socketDone)
	}()
	tableDone := make(chan struct{})
	go func() {
		defer done.Done()
		ready.Done()
		<-start
		if err := table.Close(); err != nil {
			t.Errorf("table close: %v", err)
		}
		close(tableDone)
	}()
	ready.Wait()
	close(start)

	// The socket must start physical close, but it cannot release the duplex
	// charge while Close itself, and then both I/O pumps, remain parked.
	requireSignal(t, conn.closeEntered, "connection close")
	if usage := hostBuffers.Usage(); usage.Used != 2*DefaultBufferSize {
		t.Fatalf("charge released while Close was parked: %+v", usage)
	}
	conn.releaseClose()
	select {
	case <-socketDone:
		t.Fatal("socket returned before both parked pumps joined")
	default:
	}
	select {
	case <-tableDone:
		t.Fatal("table close returned before socket teardown completed")
	default:
	}
	if usage := hostBuffers.Usage(); usage.Used != 2*DefaultBufferSize {
		t.Fatalf("charge released before parked pumps joined: %+v", usage)
	}

	conn.releasePumps()
	done.Wait()
	if usage := hostBuffers.Usage(); usage.Used != 0 || usage.Peak != 2*DefaultBufferSize {
		t.Fatalf("concurrent teardown released the duplex charge incorrectly: %+v", usage)
	}

	// Repeat all terminal operations. The charge must stay at zero.
	input.Drop()
	output.Drop()
	socket.Drop()
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
	if usage := hostBuffers.Usage(); usage.Used != 0 {
		t.Fatalf("repeated teardown changed released usage: %+v", usage)
	}
}

func TestHostBufferBudgetDomainsAreIsolated(t *testing.T) {
	firstBudget := NewHostBufferBudget(2 * DefaultBufferSize)
	secondBudget := NewHostBufferBudget(2 * DefaultBufferSize)
	firstTable := NewResourceTableWithBudgets(4, NewSocketBudget(1), firstBudget)
	secondTable := NewResourceTableWithBudgets(4, NewSocketBudget(1), secondBudget)

	first, firstPeer := connectedTCPSocket(t)
	second, secondPeer := connectedTCPSocket(t)
	defer firstPeer.Close()
	defer secondPeer.Close()
	defer first.Drop()
	defer second.Drop()

	if _, _, err := firstTable.NewTCPDuplexStreams(first); err != nil {
		t.Fatal(err)
	}
	if firstUsage, secondUsage := firstBudget.Usage(), secondBudget.Usage(); firstUsage.Used != 2*DefaultBufferSize || secondUsage.Used != 0 {
		t.Fatalf("first domain crossed into second: first=%+v second=%+v", firstUsage, secondUsage)
	}
	if _, _, err := secondTable.NewTCPDuplexStreams(second); err != nil {
		t.Fatal(err)
	}
	if firstUsage, secondUsage := firstBudget.Usage(), secondBudget.Usage(); firstUsage.Used != 2*DefaultBufferSize || secondUsage.Used != 2*DefaultBufferSize {
		t.Fatalf("independent domains did not retain separate charges: first=%+v second=%+v", firstUsage, secondUsage)
	}

	first.Drop()
	if firstUsage, secondUsage := firstBudget.Usage(), secondBudget.Usage(); firstUsage.Used != 0 || secondUsage.Used != 2*DefaultBufferSize {
		t.Fatalf("first teardown affected second domain: first=%+v second=%+v", firstUsage, secondUsage)
	}
	second.Drop()
	if firstUsage, secondUsage := firstBudget.Usage(), secondBudget.Usage(); firstUsage.Used != 0 || secondUsage.Used != 0 {
		t.Fatalf("domain charges leaked: first=%+v second=%+v", firstUsage, secondUsage)
	}
}

func TestHostBufferBudgetBelowRingCapacityStartsNoPump(t *testing.T) {
	hostBuffers := NewHostBufferBudget(DefaultBufferSize - 1)
	table := NewResourceTableWithBudgets(4, NewSocketBudget(1), hostBuffers)
	conn := newParkedDuplexConn()
	defer conn.releaseAll()

	socket := NewTCPSocketResource(0)
	socket.SetState(TCPStateConnected)
	socket.SetConn(conn)
	input, output, err := table.NewTCPDuplexStreams(socket)
	if !errors.Is(err, ErrHostBufferLimit) || input != nil || output != nil {
		t.Fatalf("input=%v output=%v err=%v", input, output, err)
	}
	if usage := hostBuffers.Usage(); usage.Used != 0 || usage.Peak != 0 {
		t.Fatalf("below-ring admission changed accounting: %+v", usage)
	}
	requireNotSignaled(t, conn.readEntered, "read pump")
	requireNotSignaled(t, conn.writeEntered, "write pump")
	requireNotSignaled(t, conn.closeEntered, "connection close")
	if socket.input != nil || socket.output != nil || socket.duplexCharge != nil {
		t.Fatal("denied first reservation attached a duplex to the socket")
	}
}
