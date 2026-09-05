package preview2

import (
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeDNSResult struct {
	fakeTrackedCloser
	addrs []string
}

func (r *fakeDNSResult) DNSAddresses() []string {
	if r == nil {
		panic("typed nil DNSAddresses")
	}
	return r.addrs
}

func (r *fakeDNSResult) Close() error {
	r.addrs = nil
	return r.fakeTrackedCloser.Close()
}

type blockingDNSResult struct {
	entered chan struct{}
	release chan struct{}
	fakeDNSResult
}

func (r *blockingDNSResult) Close() error {
	select {
	case <-r.entered:
	default:
		close(r.entered)
	}
	<-r.release
	return r.fakeDNSResult.Close()
}

func pendingDNSStream(result io.Closer, err error, ready bool) (*ResolveAddressStreamResource, *fakeOperation) {
	op := newFakeOperation(result, err, ready)
	return NewPendingResolveAddressStreamResource(op), op
}

func collectAddresses(t *testing.T, stream *ResolveAddressStreamResource) []string {
	t.Helper()
	var got []string
	for {
		addr, err, ready := stream.Next()
		if !ready {
			t.Fatal("expected ready stream")
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if addr == nil {
			return got
		}
		got = append(got, *addr)
	}
}

func TestResolveAddressStream_PendingReadiness(t *testing.T) {
	result := &fakeDNSResult{addrs: []string{"192.0.2.1"}}
	stream, op := pendingDNSStream(result, nil, false)
	defer stream.Drop()

	p := stream.Pollable().(NotifyPollable)
	if p.Ready() {
		t.Fatal("pending stream reported ready")
	}
	ch := p.Notify()
	select {
	case <-ch:
		t.Fatal("pending notify already closed")
	default:
	}

	addr, err, ready := stream.Next()
	if ready || addr != nil || err != nil {
		t.Fatalf("pending Next = (%v, %v, %v)", addr, err, ready)
	}

	op.setReady(true)
	if !p.Ready() {
		t.Fatal("completed operation not ready")
	}
	select {
	case <-ch:
	default:
		t.Fatal("operation ready did not wake pollable")
	}

	addr, err, ready = stream.Next()
	if !ready || err != nil || addr == nil || *addr != "192.0.2.1" {
		t.Fatalf("ready Next = (%v, %v, %v)", addr, err, ready)
	}
}

func TestResolveAddressStream_SuccessOrderAndSnapshot(t *testing.T) {
	source := []string{"192.0.2.1", "192.0.2.2", "2001:db8::1"}
	result := &fakeDNSResult{addrs: source}
	stream, op := pendingDNSStream(result, nil, true)
	defer stream.Drop()

	addr, err, ready := stream.Next()
	if !ready || err != nil || addr == nil || *addr != "192.0.2.1" {
		t.Fatalf("first Next = (%v, %v, %v)", addr, err, ready)
	}
	source[0] = "mutated"
	source[1] = "mutated"

	got := collectAddresses(t, stream)
	want := []string{"192.0.2.2", "2001:db8::1"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("address %d = %q, want %q", i, got[i], want[i])
		}
	}
	if result.closeCount.Load() != 1 {
		t.Fatalf("result close count = %d, want 1", result.closeCount.Load())
	}
	if result.addrs != nil {
		t.Fatal("result was not disposed")
	}
	if stream.op != op {
		t.Fatal("operation detached after success")
	}
}

func TestResolveAddressStream_LegacyConstructorSnapshot(t *testing.T) {
	source := []string{"10.0.0.1", "10.0.0.2"}
	stream := NewResolveAddressStreamResource(source)
	defer stream.Drop()

	source[0] = "mutated"

	first := stream.ReadNext()
	if first == nil || *first != "10.0.0.1" {
		t.Fatalf("ReadNext = %v, want 10.0.0.1", first)
	}
	got := collectAddresses(t, stream)
	if len(got) != 1 || got[0] != "10.0.0.2" {
		t.Fatalf("remaining = %v, want [10.0.0.2]", got)
	}
}

func TestResolveAddressStream_EOF(t *testing.T) {
	stream := NewResolveAddressStreamResource([]string{"192.0.2.9"})
	defer stream.Drop()

	addr, err, ready := stream.Next()
	if !ready || err != nil || addr == nil || *addr != "192.0.2.9" {
		t.Fatalf("first Next = (%v, %v, %v)", addr, err, ready)
	}
	for i := 0; i < 3; i++ {
		addr, err, ready = stream.Next()
		if !ready || err != nil || addr != nil {
			t.Fatalf("EOF Next %d = (%v, %v, %v)", i, addr, err, ready)
		}
	}

	empty := NewResolveAddressStreamResource(nil)
	defer empty.Drop()
	addr, err, ready = empty.Next()
	if !ready || err != nil || addr != nil {
		t.Fatalf("empty Next = (%v, %v, %v)", addr, err, ready)
	}
}

func TestResolveAddressStream_ErrorsPersist(t *testing.T) {
	want := errors.New("lookup failed")
	result := &fakeTrackedCloser{}
	stream, op := pendingDNSStream(result, want, true)
	defer stream.Drop()

	for i := 0; i < 3; i++ {
		addr, err, ready := stream.Next()
		if !ready || addr != nil || !errors.Is(err, want) {
			t.Fatalf("Next %d = (%v, %v, %v)", i, addr, err, ready)
		}
	}
	if result.closeCount.Load() != 1 {
		t.Fatalf("error result close count = %d, want 1", result.closeCount.Load())
	}
	if !op.taken.Load() {
		t.Fatal("Take was not invoked")
	}
	if stream.op != op {
		t.Fatal("operation detached after error")
	}
}

func TestResolveAddressStream_NilPendingOperation(t *testing.T) {
	stream := NewPendingResolveAddressStreamResource(nil)
	defer stream.Drop()

	addr, err, ready := stream.Next()
	if !ready || addr != nil || !errors.Is(err, errNilResolveOperation) {
		t.Fatalf("nil op Next = (%v, %v, %v)", addr, err, ready)
	}
	if !stream.Pollable().Ready() {
		t.Fatal("nil op stream must be ready")
	}
}

func TestResolveAddressStream_ExactBoundsNoRetainedExcess(t *testing.T) {
	t.Run("exact max count", func(t *testing.T) {
		addrs := make([]string, MaxResolveAddresses)
		for i := range addrs {
			addrs[i] = "1.1.1.1"
		}
		stream := NewResolveAddressStreamResource(addrs)
		defer stream.Drop()
		got := collectAddresses(t, stream)
		if len(got) != MaxResolveAddresses {
			t.Fatalf("got %d addresses, want %d", len(got), MaxResolveAddresses)
		}
	})

	t.Run("count overflow constructor", func(t *testing.T) {
		addrs := make([]string, MaxResolveAddresses+1)
		for i := range addrs {
			addrs[i] = "1.1.1.1"
		}
		stream := NewResolveAddressStreamResource(addrs)
		defer stream.Drop()
		assertLimitError(t, stream, addrs)
	})

	t.Run("exact max bytes", func(t *testing.T) {
		long := strings.Repeat("a", MaxResolveAddressBytes)
		stream := NewResolveAddressStreamResource([]string{long})
		defer stream.Drop()
		got := collectAddresses(t, stream)
		if len(got) != 1 || got[0] != long {
			t.Fatalf("got %d addresses", len(got))
		}
	})

	t.Run("byte overflow constructor", func(t *testing.T) {
		long := strings.Repeat("a", MaxResolveAddressBytes+1)
		stream := NewResolveAddressStreamResource([]string{long})
		defer stream.Drop()
		assertLimitError(t, stream, []string{long})
	})

	t.Run("byte overflow sum", func(t *testing.T) {
		addr1 := strings.Repeat("a", 2048)
		addr2 := strings.Repeat("b", 2049)
		stream := NewResolveAddressStreamResource([]string{addr1, addr2})
		defer stream.Drop()
		assertLimitError(t, stream, []string{addr1, addr2})
	})

	t.Run("pending count overflow", func(t *testing.T) {
		addrs := make([]string, MaxResolveAddresses+1)
		for i := range addrs {
			addrs[i] = "1.1.1.1"
		}
		result := &fakeDNSResult{addrs: addrs}
		stream, _ := pendingDNSStream(result, nil, true)
		defer stream.Drop()
		assertLimitError(t, stream, addrs)
		if result.closeCount.Load() != 1 {
			t.Fatalf("overflow result close count = %d, want 1", result.closeCount.Load())
		}
	})
}

func assertLimitError(t *testing.T, stream *ResolveAddressStreamResource, source []string) {
	t.Helper()
	addr, err, ready := stream.Next()
	if !ready || addr != nil || !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("limit Next = (%v, %v, %v)", addr, err, ready)
	}
	if stream.addresses != nil {
		t.Fatalf("retained %d addresses after limit", len(stream.addresses))
	}
	source[0] = "mutated"
	addr, err, ready = stream.Next()
	if !ready || addr != nil || !errors.Is(err, ErrResolveLimit) {
		t.Fatalf("persisted limit Next = (%v, %v, %v)", addr, err, ready)
	}
}

func TestResolveAddressStream_MalformedAndNilResults(t *testing.T) {
	t.Run("wrong type", func(t *testing.T) {
		wrong := &fakeTrackedCloser{}
		stream, _ := pendingDNSStream(wrong, nil, true)
		defer stream.Drop()
		addr, err, ready := stream.Next()
		if !ready || addr != nil || !errors.Is(err, errMalformedResolveResult) {
			t.Fatalf("wrong type Next = (%v, %v, %v)", addr, err, ready)
		}
		if wrong.closeCount.Load() != 1 {
			t.Fatalf("wrong type close count = %d, want 1", wrong.closeCount.Load())
		}
	})

	t.Run("nil result", func(t *testing.T) {
		stream, _ := pendingDNSStream(nil, nil, true)
		defer stream.Drop()
		addr, err, ready := stream.Next()
		if !ready || addr != nil || !errors.Is(err, errNilResolveResult) {
			t.Fatalf("nil result Next = (%v, %v, %v)", addr, err, ready)
		}
	})

	t.Run("typed nil result", func(t *testing.T) {
		var typed *fakeDNSResult
		stream, _ := pendingDNSStream(typed, nil, true)
		defer stream.Drop()
		addr, err, ready := stream.Next()
		if !ready || addr != nil || !errors.Is(err, errNilResolveResult) {
			t.Fatalf("typed nil Next = (%v, %v, %v)", addr, err, ready)
		}
	})
}

func TestResolveAddressStream_DropJoinsBlockedCompletion(t *testing.T) {
	result := &fakeDNSResult{addrs: []string{"192.0.2.1"}}
	stream, op := pendingDNSStream(result, nil, false)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	op.closeHook = func() {
		close(entered)
		<-release
	}

	go func() {
		stream.Drop()
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for op.Close")
	}
	select {
	case <-done:
		t.Fatal("Drop returned before operation join")
	case <-time.After(50 * time.Millisecond):
	}
	if stream.addresses != nil {
		t.Fatal("cleared results before join")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Drop did not join")
	}
	if stream.op != nil {
		t.Fatal("operation retained after Drop")
	}
	if result.closeCount.Load() != 1 {
		t.Fatalf("leftover result close count = %d, want 1", result.closeCount.Load())
	}
}

func TestResolveAddressStream_DropWaitsResultDisposal(t *testing.T) {
	result := &blockingDNSResult{
		fakeDNSResult: fakeDNSResult{addrs: []string{"192.0.2.1"}},
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
	}
	stream, op := pendingDNSStream(result, nil, true)

	nextDone := make(chan struct{})
	go func() {
		defer close(nextDone)
		_, _, _ = stream.Next()
	}()
	select {
	case <-result.entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result Close")
	}

	dropDone := make(chan struct{})
	go func() {
		stream.Drop()
		close(dropDone)
	}()
	select {
	case <-dropDone:
		t.Fatal("Drop returned before result disposal")
	case <-time.After(50 * time.Millisecond):
	}
	close(result.release)
	select {
	case <-nextDone:
	case <-time.After(time.Second):
		t.Fatal("Next did not finish")
	}
	select {
	case <-dropDone:
	case <-time.After(time.Second):
		t.Fatal("Drop did not join disposal")
	}
	if !op.closed.Load() {
		t.Fatal("operation not closed")
	}
}

func TestResolveAddressStream_ConcurrentNextDrop(t *testing.T) {
	for i := 0; i < 100; i++ {
		result := &fakeDNSResult{addrs: []string{"192.0.2.1", "192.0.2.2"}}
		stream, op := pendingDNSStream(result, nil, true)

		var wg sync.WaitGroup
		wg.Add(8)
		for n := 0; n < 4; n++ {
			go func() {
				defer wg.Done()
				_, _, _ = stream.Next()
			}()
		}
		for n := 0; n < 4; n++ {
			go func() {
				defer wg.Done()
				stream.Drop()
			}()
		}
		wg.Wait()

		if !op.closed.Load() {
			t.Fatalf("iteration %d: operation not closed", i)
		}
		if result.closeCount.Load() != 1 {
			t.Fatalf("iteration %d: result close count = %d, want 1", i, result.closeCount.Load())
		}
		if stream.op != nil {
			t.Fatalf("iteration %d: operation retained after Drop", i)
		}
		if stream.addresses != nil {
			t.Fatalf("iteration %d: results retained after Drop", i)
		}
	}
}

func TestResolveAddressStream_DropWakesAndSubscriptionBorrows(t *testing.T) {
	stream, _ := pendingDNSStream(&fakeDNSResult{addrs: []string{"192.0.2.1"}}, nil, false)
	p := stream.Pollable().(NotifyPollable)
	if _, ok := stream.Pollable().(NotifyPollable); !ok {
		t.Fatal("Pollable must implement NotifyPollable")
	}
	ch := p.Notify()
	p.Drop()
	addr, err, ready := stream.Next()
	if ready || addr != nil || err != nil {
		t.Fatalf("subscription Drop closed stream: (%v, %v, %v)", addr, err, ready)
	}
	select {
	case <-ch:
		t.Fatal("subscription Drop woke stream")
	default:
	}

	stream.Drop()
	select {
	case <-ch:
	default:
		t.Fatal("stream Drop did not wake pollable")
	}
	if !p.Ready() {
		t.Fatal("dropped stream pollable not ready")
	}
	addr, err, ready = stream.Next()
	if !ready || addr != nil || err != nil {
		t.Fatalf("dropped Next = (%v, %v, %v)", addr, err, ready)
	}
}

func TestResolveAddressStream_ConcurrentDropJoin(t *testing.T) {
	stream, op := pendingDNSStream(&fakeDNSResult{addrs: []string{"192.0.2.1"}}, nil, false)
	var started atomic.Int32
	release := make(chan struct{})
	op.closeHook = func() {
		started.Add(1)
		<-release
	}

	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			stream.Drop()
		}()
	}
	waitFor(t, func() bool { return started.Load() == 1 })
	time.Sleep(20 * time.Millisecond)
	if started.Load() != 1 {
		t.Fatalf("Close started %d times", started.Load())
	}
	close(release)
	wg.Wait()
	if op.closeCount.Load() != 1 {
		t.Fatalf("op close count = %d, want 1", op.closeCount.Load())
	}
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestResolveAddressStreamRejectsTypedNilOperation(t *testing.T) {
	var op *fakeOperation
	stream := NewPendingResolveAddressStreamResource(op)
	defer stream.Drop()
	address, err, ready := stream.Next()
	if address != nil || !ready || !errors.Is(err, errNilResolveOperation) {
		t.Fatalf("typed nil operation: %v %v %v", address, err, ready)
	}
	if !stream.Pollable().Ready() {
		t.Fatal("typed nil operation did not expose its error as readiness")
	}
}
