package preview2

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPollableResource_ZeroValue verifies that zero-value PollableResource is usable.
func TestPollableResource_ZeroValue(t *testing.T) {
	var p PollableResource

	if p.Type() != ResourcePollable {
		t.Fatalf("expected ResourcePollable, got %d", p.Type())
	}
	if p.Ready() {
		t.Fatal("zero-value PollableResource must not be ready")
	}

	ch := p.Notify()
	if ch == nil {
		t.Fatal("Notify() returned nil channel")
	}
	select {
	case <-ch:
		t.Fatal("Notify() channel should not be closed initially")
	default:
	}

	p.SetReady(true)
	if !p.Ready() {
		t.Fatal("expected ready after SetReady(true)")
	}
	select {
	case <-ch:
	default:
		t.Fatal("Notify() channel should be closed after SetReady(true)")
	}

	ch2 := p.Notify()
	select {
	case <-ch2:
	default:
		t.Fatal("Notify() should yield closed channel when ready")
	}
}

// TestPollableResource_NotifyPollableInterface verifies interface conformance.
func TestPollableResource_NotifyPollableInterface(t *testing.T) {
	p := &PollableResource{}
	var np NotifyPollable = p

	if np.Ready() {
		t.Fatal("should not be ready initially")
	}
	ch := np.Notify()
	select {
	case <-ch:
		t.Fatal("channel should be open initially")
	default:
	}

	p.SetReady(true)
	if !np.Ready() {
		t.Fatal("should be ready")
	}
	select {
	case <-ch:
	default:
		t.Fatal("channel should close when ready")
	}
}

// TestPollableResource_NotifyStateTransitions verifies Notify channels across state changes.
func TestPollableResource_NotifyStateTransitions(t *testing.T) {
	p := &PollableResource{}

	// When not ready, multiple calls to Notify() return the same active channel.
	ch1 := p.Notify()
	ch2 := p.Notify()
	if ch1 != ch2 {
		t.Fatal("expected identical channel while in same not-ready state")
	}

	// Idempotent SetReady(false) does not alter channel.
	p.SetReady(false)
	ch3 := p.Notify()
	if ch3 != ch1 {
		t.Fatal("SetReady(false) while already false should not alter channel")
	}

	// Setting ready closes the channel.
	p.SetReady(true)
	select {
	case <-ch1:
	default:
		t.Fatal("ch1 should be closed")
	}

	// While ready, Notify() yields closed channel.
	chReady := p.Notify()
	select {
	case <-chReady:
	default:
		t.Fatal("Notify() while ready must be closed")
	}

	// Resetting to not ready installs a new cycle.
	p.SetReady(false)
	if p.Ready() {
		t.Fatal("expected not ready after reset")
	}
	chNew := p.Notify()
	select {
	case <-chNew:
		t.Fatal("new cycle channel must be open")
	default:
	}
	if chNew == ch1 {
		t.Fatal("new cycle channel must not be the old closed channel")
	}
}

// TestPollableResource_NoLostWakeupsAcrossCycles ensures no lost wakeups over repeated cycles.
func TestPollableResource_NoLostWakeupsAcrossCycles(t *testing.T) {
	p := &PollableResource{}
	const cycles = 50

	for cycle := 0; cycle < cycles; cycle++ {
		// Pattern 1: Consumer waits on Notify() before producer signals.
		ch := p.Notify()
		woke := make(chan struct{})
		go func(c <-chan struct{}) {
			<-c
			close(woke)
		}(ch)

		p.SetReady(true)
		select {
		case <-woke:
		case <-time.After(time.Second):
			t.Fatalf("cycle %d: timed out waiting for consumer wakeup", cycle)
		}

		// Reset for next phase.
		p.SetReady(false)

		// Pattern 2: Producer signals before consumer calls Notify().
		p.SetReady(true)
		chLate := p.Notify()
		select {
		case <-chLate:
		default:
			t.Fatalf("cycle %d: late Notify() must return closed channel", cycle)
		}

		// Reset for next cycle.
		p.SetReady(false)
	}
}

// TestPollableResource_DropTerminal verifies drop semantics.
func TestPollableResource_DropTerminal(t *testing.T) {
	p := &PollableResource{}

	// Waiter blocked in Block()
	blocked := make(chan struct{})
	unblocked := make(chan struct{})
	go func() {
		close(blocked)
		p.Block(context.Background())
		close(unblocked)
	}()

	<-blocked
	// Allow goroutine to enter Block
	time.Sleep(10 * time.Millisecond)

	// Drop terminal marks ready and unblocks waiters
	p.Drop()

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("Drop did not unblock waiter in Block()")
	}

	if !p.Ready() {
		t.Fatal("Ready() must return true after Drop()")
	}

	ch := p.Notify()
	select {
	case <-ch:
	default:
		t.Fatal("Notify() must yield closed channel after Drop()")
	}

	// SetReady after Drop cannot revive
	p.SetReady(false)
	if !p.Ready() {
		t.Fatal("SetReady(false) after Drop must not clear ready state")
	}

	chAfterRevive := p.Notify()
	select {
	case <-chAfterRevive:
	default:
		t.Fatal("Notify() after failed revive must remain closed")
	}

	// Repeated Drop is idempotent
	p.Drop()
	if !p.Ready() {
		t.Fatal("Ready() must remain true after repeated Drop")
	}
}

// TestPollableResource_Block_NeverFabricatesReadiness ensures Block never fabricates readiness.
func TestPollableResource_Block_NeverFabricatesReadiness(t *testing.T) {
	p := &PollableResource{}

	// Pre-canceled context
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()

	p.Block(ctxCanceled)
	if p.Ready() {
		t.Fatal("Block must never fabricate readiness on pre-canceled context")
	}

	// Context canceled while blocking
	ctx, cancelWait := context.WithCancel(context.Background())
	unblocked := make(chan struct{})
	go func() {
		p.Block(ctx)
		close(unblocked)
	}()

	time.Sleep(10 * time.Millisecond)
	cancelWait()

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("Block did not unblock on context cancellation")
	}

	if p.Ready() {
		t.Fatal("Block must never fabricate readiness after context cancellation")
	}
}

// TestPollableResource_Block_UnblocksOnSetReady verifies Block unblocks on readiness.
func TestPollableResource_Block_UnblocksOnSetReady(t *testing.T) {
	p := &PollableResource{}

	unblocked := make(chan struct{})
	go func() {
		p.Block(context.Background())
		close(unblocked)
	}()

	time.Sleep(10 * time.Millisecond)
	p.SetReady(true)

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("Block did not unblock after SetReady(true)")
	}

	if !p.Ready() {
		t.Fatal("expected ready after SetReady(true)")
	}
}

// TestPollableResource_ConcurrentStress stresses concurrent reads, state updates, and Drop.
func TestPollableResource_ConcurrentStress(t *testing.T) {
	p := &PollableResource{}
	var stop atomic.Bool
	var wg sync.WaitGroup

	const readers = 10
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				ch := p.Notify()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
				select {
				case <-ch:
				case <-ctx.Done():
				}
				cancel()
				_ = p.Ready()
			}
		}()
	}

	const blockers = 5
	for i := 0; i < blockers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				p.Block(ctx)
				cancel()
			}
		}()
	}

	const writers = 3
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for !stop.Load() {
				p.SetReady(id%2 == 0)
				time.Sleep(2 * time.Millisecond)
				p.SetReady(id%2 != 0)
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	p.Drop()
	stop.Store(true)
	wg.Wait()

	if !p.Ready() {
		t.Fatal("expected ready after Drop")
	}
	ch := p.Notify()
	select {
	case <-ch:
	default:
		t.Fatal("channel must be closed after Drop")
	}
}
