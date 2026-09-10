package preview2

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var _ udpPacketConn = (*net.UDPConn)(nil)
var _ udpPacketConn = (*fakeUDPConn)(nil)

type fakeUDPRead struct {
	addr *net.UDPAddr
	err  error
	data []byte
}

type fakeUDPConn struct {
	writeErr        error
	reads           chan fakeUDPRead
	writeGate       chan struct{}
	writeStart      chan struct{}
	closed          chan struct{}
	written         []UDPDatagram
	delayAfterClose time.Duration
	mu              sync.Mutex
	readCalls       atomic.Int64
	writeCalls      atomic.Int64
	closeCalls      atomic.Int64
	closeOnce       sync.Once
}

func newFakeUDPConn() *fakeUDPConn {
	return &fakeUDPConn{
		reads:      make(chan fakeUDPRead, 64),
		writeStart: make(chan struct{}, 1),
		closed:     make(chan struct{}),
	}
}

func (c *fakeUDPConn) blockWrites() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeGate == nil {
		c.writeGate = make(chan struct{})
	}
}

func (c *fakeUDPConn) unblockWrites() {
	c.mu.Lock()
	gate := c.writeGate
	c.writeGate = nil
	c.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}

func (c *fakeUDPConn) pushRead(data []byte, addr *net.UDPAddr, err error) {
	pkt := fakeUDPRead{addr: copyUDPAddr(addr), err: err}
	if data != nil {
		pkt.data = append([]byte(nil), data...)
	}
	select {
	case c.reads <- pkt:
	case <-c.closed:
	}
}

func (c *fakeUDPConn) snapshotWritten() []UDPDatagram {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]UDPDatagram, len(c.written))
	for i, dg := range c.written {
		out[i] = copyUDPDatagram(dg)
	}
	return out
}

func (c *fakeUDPConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	c.readCalls.Add(1)
	select {
	case <-c.closed:
		return 0, nil, net.ErrClosed
	case r, ok := <-c.reads:
		if !ok {
			return 0, nil, net.ErrClosed
		}
		n := copy(buf, r.data)
		return n, r.addr, r.err
	}
}

func (c *fakeUDPConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	c.writeCalls.Add(1)
	select {
	case c.writeStart <- struct{}{}:
	default:
	}

	c.mu.Lock()
	gate := c.writeGate
	delay := c.delayAfterClose
	writeErr := c.writeErr
	c.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-c.closed:
			if delay > 0 {
				time.Sleep(delay)
			}
			return 0, net.ErrClosed
		}
	} else {
		select {
		case <-c.closed:
			if delay > 0 {
				time.Sleep(delay)
			}
			return 0, net.ErrClosed
		default:
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.written = append(c.written, copyUDPDatagram(UDPDatagram{Data: b, Address: addr}))
	if writeErr != nil {
		return 0, writeErr
	}
	return len(b), nil
}

func (c *fakeUDPConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeCalls.Add(1)
		close(c.closed)
	})
	return nil
}

func testUDPAddr(port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
}

func newUDPSocketWithConn(conn udpPacketConn) *UDPSocketResource {
	s := NewUDPSocketResource(4)
	s.SetConn(conn)
	s.SetState(UDPStateBound)
	return s
}

func waitPollable(t *testing.T, p Pollable) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.Block(ctx)
	if !p.Ready() {
		t.Fatal("pollable not ready")
	}
}

func waitWriteStart(t *testing.T, c *fakeUDPConn) {
	t.Helper()
	select {
	case <-c.writeStart:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not start")
	}
}

func waitStableReads(t *testing.T, c *fakeUDPConn, min int64) int64 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last int64
	stable := 0
	for time.Now().Before(deadline) {
		n := c.readCalls.Load()
		if n == last && n >= min {
			stable++
			if stable > 40 {
				return n
			}
		} else {
			stable = 0
			last = n
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("read calls did not stabilize, last=%d min=%d", last, min)
	return last
}

func listenLocalUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestUDPZeroLengthDatagramRoundTrip(t *testing.T) {
	server := listenLocalUDP(t)
	client := listenLocalUDP(t)
	socket := newUDPSocketWithConn(server)
	defer socket.Drop()

	incoming := socket.IncomingPollable()
	dst := server.LocalAddr().(*net.UDPAddr)
	n, err := client.WriteToUDP(nil, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("wrote %d bytes", n)
	}

	waitPollable(t, incoming)
	got, err := socket.ReceiveDatagrams(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d datagrams", len(got))
	}
	if len(got[0].Data) != 0 {
		t.Fatalf("payload len=%d", len(got[0].Data))
	}
	if got[0].Address == nil || got[0].Address.Port != client.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("source address %+v", got[0].Address)
	}
}

func TestUDPRealPayloadRoundTrip(t *testing.T) {
	server := listenLocalUDP(t)
	client := listenLocalUDP(t)
	socket := newUDPSocketWithConn(server)
	defer socket.Drop()

	incoming := socket.IncomingPollable()
	payload := []byte("ping")
	if _, err := client.WriteToUDP(payload, server.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	waitPollable(t, incoming)
	got, err := socket.ReceiveDatagrams(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0].Data) != "ping" {
		t.Fatalf("got %#v", got)
	}
}

func TestUDPPumpsStayIdleUntilQueueOrSubscribe(t *testing.T) {
	fake := newFakeUDPConn()
	socket := newUDPSocketWithConn(fake)
	defer socket.Drop()

	time.Sleep(30 * time.Millisecond)
	if fake.readCalls.Load() != 0 || fake.writeCalls.Load() != 0 {
		t.Fatalf("pumps started on bind: reads=%d writes=%d", fake.readCalls.Load(), fake.writeCalls.Load())
	}

	_ = socket.IncomingPollable()
	waitStableReads(t, fake, 1)
	if fake.readCalls.Load() < 1 {
		t.Fatal("read pump did not start on subscribe")
	}
}

func TestUDPCheckSendCountsInflightAndNeverOverpromises(t *testing.T) {
	fake := newFakeUDPConn()
	fake.blockWrites()
	socket := newUDPSocketWithConn(fake)
	defer socket.Drop()

	permit, err := socket.CheckSend()
	if err != nil {
		t.Fatal(err)
	}
	if permit != UDPMaxQueuedDatagrams {
		t.Fatalf("empty permit=%d", permit)
	}

	n, err := socket.SendDatagrams([]UDPDatagram{{Data: []byte("a"), Address: testUDPAddr(1)}})
	if err != nil || n != 1 {
		t.Fatalf("send n=%d err=%v", n, err)
	}
	waitWriteStart(t, fake)

	permit, err = socket.CheckSend()
	if err != nil {
		t.Fatal(err)
	}
	if permit != UDPMaxQueuedDatagrams-1 {
		t.Fatalf("inflight permit=%d", permit)
	}

	fill := make([]UDPDatagram, UDPMaxQueuedDatagrams)
	for i := range fill {
		fill[i] = UDPDatagram{Data: []byte{byte(i)}, Address: testUDPAddr(2)}
	}
	accepted, err := socket.SendDatagrams(fill)
	if err != nil {
		t.Fatal(err)
	}
	if accepted != UDPMaxQueuedDatagrams-1 {
		t.Fatalf("accepted %d filling around inflight", accepted)
	}
	permit, err = socket.CheckSend()
	if err != nil {
		t.Fatal(err)
	}
	if permit != 0 {
		t.Fatalf("full permit=%d", permit)
	}
	if socket.OutgoingPollable().Ready() {
		t.Fatal("outgoing ready while cap is exhausted")
	}

	extra, err := socket.SendDatagrams([]UDPDatagram{{Data: []byte("x"), Address: testUDPAddr(3)}})
	if err != nil {
		t.Fatal(err)
	}
	if extra != 0 {
		t.Fatalf("over-cap accepted=%d", extra)
	}
}

func TestUDPPartialSendAndTooLarge(t *testing.T) {
	fake := newFakeUDPConn()
	fake.blockWrites()
	socket := newUDPSocketWithConn(fake)
	defer socket.Drop()

	n, err := socket.SendDatagrams(nil)
	if err != nil || n != 0 {
		t.Fatalf("empty send n=%d err=%v", n, err)
	}

	tooLarge := make([]byte, UDPMaxDatagramBytes+1)
	n, err = socket.SendDatagrams([]UDPDatagram{{Data: tooLarge, Address: testUDPAddr(1)}})
	if !errors.Is(err, ErrDatagramTooLarge) || n != 0 {
		t.Fatalf("too large n=%d err=%v", n, err)
	}
	if fake.writeCalls.Load() != 0 {
		t.Fatal("too-large datagram was copied to the writer")
	}
	permit, err := socket.CheckSend()
	if err != nil || permit != UDPMaxQueuedDatagrams {
		t.Fatalf("permit after reject %d err=%v", permit, err)
	}

	maxOK := make([]byte, UDPMaxDatagramBytes)
	n, err = socket.SendDatagrams([]UDPDatagram{
		{Data: []byte("ok"), Address: testUDPAddr(1)},
		{Data: tooLarge, Address: testUDPAddr(1)},
		{Data: []byte("later"), Address: testUDPAddr(1)},
	})
	if err != nil || n != 1 {
		t.Fatalf("partial too-large n=%d err=%v", n, err)
	}

	n, err = socket.SendDatagrams([]UDPDatagram{{Data: maxOK, Address: testUDPAddr(1)}})
	if err != nil || n != 1 {
		t.Fatalf("max size n=%d err=%v", n, err)
	}
}

func TestUDPSendOwnershipCopies(t *testing.T) {
	fake := newFakeUDPConn()
	fake.blockWrites()
	socket := newUDPSocketWithConn(fake)
	defer socket.Drop()

	payload := []byte("hello")
	addr := testUDPAddr(9)
	if _, err := socket.SendDatagrams([]UDPDatagram{{Data: payload, Address: addr}}); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	addr.Port = 99
	addr.IP[0] = 8

	fake.unblockWrites()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := fake.snapshotWritten()
		if len(got) >= 1 {
			if string(got[0].Data) != "hello" {
				t.Fatalf("written payload %q", got[0].Data)
			}
			if got[0].Address == nil || got[0].Address.Port != 9 || got[0].Address.IP[0] == 8 {
				t.Fatalf("written address %+v", got[0].Address)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("writer did not observe copied datagram")
}

func TestUDPReceiveOwnershipAndEmpty(t *testing.T) {
	fake := newFakeUDPConn()
	socket := newUDPSocketWithConn(fake)
	defer socket.Drop()

	empty, err := socket.ReceiveDatagrams(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty receive %d", len(empty))
	}

	src := []byte("world")
	addr := testUDPAddr(12)
	fake.pushRead(src, addr, nil)
	waitPollable(t, socket.IncomingPollable())

	zero, err := socket.ReceiveDatagrams(0)
	if err != nil || len(zero) != 0 {
		t.Fatalf("max=0 n=%d err=%v", len(zero), err)
	}

	got, err := socket.ReceiveDatagrams(^uint64(0))
	if err != nil || len(got) != 1 {
		t.Fatalf("n=%d err=%v", len(got), err)
	}
	got[0].Data[0] = 'Z'
	got[0].Address.Port = 1
	if src[0] != 'w' {
		t.Fatal("receive shared the source payload")
	}
	if addr.Port != 12 {
		t.Fatal("receive shared the source address")
	}
}

func TestUDPReadPumpBlocksWhenQueueFull(t *testing.T) {
	fake := newFakeUDPConn()
	addr := testUDPAddr(7)
	for i := 0; i < 32; i++ {
		fake.pushRead([]byte{byte(i)}, addr, nil)
	}
	socket := newUDPSocketWithConn(fake)
	defer socket.Drop()

	_ = socket.IncomingPollable()
	calls := waitStableReads(t, fake, 17)
	if calls != 17 {
		t.Fatalf("read calls=%d want 16 queued + 1 inflight", calls)
	}

	got, err := socket.ReceiveDatagrams(UDPMaxQueuedDatagrams)
	if err != nil || len(got) != UDPMaxQueuedDatagrams {
		t.Fatalf("drained %d err=%v", len(got), err)
	}
}

func TestUDPBackpressureWakeAndDrop(t *testing.T) {
	fake := newFakeUDPConn()
	fake.blockWrites()
	socket := newUDPSocketWithConn(fake)

	fill := make([]UDPDatagram, UDPMaxQueuedDatagrams)
	for i := range fill {
		fill[i] = UDPDatagram{Data: []byte{byte(i)}, Address: testUDPAddr(4)}
	}
	n, err := socket.SendDatagrams(fill)
	if err != nil || n != UDPMaxQueuedDatagrams {
		t.Fatalf("fill n=%d err=%v", n, err)
	}
	outgoing := socket.OutgoingPollable()
	if outgoing.Ready() {
		t.Fatal("outgoing ready at cap")
	}

	fake.unblockWrites()
	waitPollable(t, outgoing)
	permit, err := socket.CheckSend()
	if err != nil {
		t.Fatal(err)
	}
	if permit == 0 {
		t.Fatal("check-send still 0 after drain")
	}

	incoming := socket.IncomingPollable()
	incoming.Drop()
	outgoing.Drop()
	fake.pushRead([]byte("wake"), testUDPAddr(5), nil)
	waitPollable(t, socket.IncomingPollable())
	got, err := socket.ReceiveDatagrams(1)
	if err != nil || len(got) != 1 || string(got[0].Data) != "wake" {
		t.Fatalf("after pollable drop got %#v err=%v", got, err)
	}

	socket.Drop()
	if socket.recvQ != nil || socket.sendQ != nil {
		t.Fatal("drop retained queued storage")
	}
	if _, err := socket.ReceiveDatagrams(1); !errors.Is(err, ErrUDPSocketClosed) {
		t.Fatalf("receive after drop err=%v", err)
	}
	if _, err := socket.CheckSend(); !errors.Is(err, ErrUDPSocketClosed) {
		t.Fatalf("check-send after drop err=%v", err)
	}
}

func TestUDPNetworkErrorBecomesReadiness(t *testing.T) {
	fake := newFakeUDPConn()
	socket := newUDPSocketWithConn(fake)
	defer socket.Drop()

	incoming := socket.IncomingPollable()
	readErr := errors.New("recv failed")
	fake.pushRead(nil, nil, readErr)
	waitPollable(t, incoming)
	_, err := socket.ReceiveDatagrams(1)
	if !errors.Is(err, readErr) {
		t.Fatalf("receive err=%v", err)
	}

	fake2 := newFakeUDPConn()
	fake2.writeErr = io.ErrClosedPipe
	socket2 := newUDPSocketWithConn(fake2)
	defer socket2.Drop()
	if _, err := socket2.SendDatagrams([]UDPDatagram{{Data: []byte("x"), Address: testUDPAddr(1)}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = socket2.CheckSend()
		if errors.Is(err, io.ErrClosedPipe) {
			break
		}
		if err != nil {
			t.Fatalf("check-send err=%v", err)
		}
		if !time.Now().Before(deadline) {
			t.Fatal("write error did not surface on check-send")
		}
		time.Sleep(time.Millisecond)
	}
	n, err := socket2.SendDatagrams([]UDPDatagram{{Data: []byte("y"), Address: testUDPAddr(1)}})
	if n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("send after write error n=%d err=%v", n, err)
	}
}

func TestUDPDelayedConcurrentDropJoinsClose(t *testing.T) {
	fake := newFakeUDPConn()
	fake.blockWrites()
	fake.mu.Lock()
	fake.delayAfterClose = 80 * time.Millisecond
	fake.mu.Unlock()
	socket := newUDPSocketWithConn(fake)

	if _, err := socket.SendDatagrams([]UDPDatagram{{Data: []byte("blocked"), Address: testUDPAddr(1)}}); err != nil {
		t.Fatal(err)
	}
	waitWriteStart(t, fake)

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			socket.Drop()
		}()
	}
	wg.Wait()
	if time.Since(start) < 80*time.Millisecond {
		t.Fatal("concurrent Drop returned before pump join")
	}
	if fake.closeCalls.Load() != 1 {
		t.Fatalf("close calls=%d", fake.closeCalls.Load())
	}
	if socket.State() != UDPStateClosed {
		t.Fatal("state after drop")
	}
	if socket.recvQ != nil || socket.sendQ != nil {
		t.Fatal("queued storage retained after concurrent drop")
	}
}

func TestUDPSetConnAfterClosed(t *testing.T) {
	socket := NewUDPSocketResource(4)
	socket.Drop()

	late := listenLocalUDP(t)
	socket.SetConn(late)
	if socket.Conn() != nil {
		t.Fatal("closed socket stored a new conn")
	}
	if socket.State() != UDPStateClosed {
		t.Fatal("closed socket state changed")
	}
	buf := make([]byte, 1)
	if _, _, err := late.ReadFromUDP(buf); err == nil {
		t.Fatal("late conn was not closed")
	}
}

func TestUDPBufferRace(t *testing.T) {
	fake := newFakeUDPConn()
	socket := newUDPSocketWithConn(fake)
	addr := testUDPAddr(3)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				fake.pushRead([]byte("r"), addr, nil)
			}
		}
	}()

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				_, _ = socket.ReceiveDatagrams(4)
				_, _ = socket.CheckSend()
				_, _ = socket.SendDatagrams([]UDPDatagram{{Data: []byte("s"), Address: addr}})
				in := socket.IncomingPollable()
				out := socket.OutgoingPollable()
				_ = in.Ready()
				_ = out.Ready()
				if np, ok := in.(NotifyPollable); ok {
					_ = np.Notify()
				}
				in.Drop()
				out.Drop()
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	socket.Drop()
	close(stop)
	wg.Wait()
}

func TestUDPQueueOperationsRequireDatagramConnection(t *testing.T) {
	for _, invalid := range []any{nil, &fakeTrackedCloser{}} {
		socket := NewUDPSocketResource(0)
		socket.SetConn(invalid)
		if _, err := socket.ReceiveDatagrams(1); !errors.Is(err, ErrUDPSocketNotBound) {
			t.Fatalf("receive without datagram connection: %v", err)
		}
		if capacity, err := socket.CheckSend(); capacity != 0 || !errors.Is(err, ErrUDPSocketNotBound) {
			t.Fatalf("check-send without datagram connection: %d %v", capacity, err)
		}
		if accepted, err := socket.SendDatagrams([]UDPDatagram{{Data: []byte("lost")}}); accepted != 0 || !errors.Is(err, ErrUDPSocketNotBound) {
			t.Fatalf("send without datagram connection: %d %v", accepted, err)
		}
		if len(socket.sendQ) != 0 || socket.pumpsStarted {
			t.Fatal("invalid connection retained a packet or started pumps")
		}
		socket.Drop()
	}
}
