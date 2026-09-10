package preview2

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wippyai/wasm-runtime/resource"
)

type heldSocketDrop struct {
	entered chan struct{}
	release chan struct{}
	dropped atomic.Int32
}

func (*heldSocketDrop) Type() ResourceType { return ResourceTCPSocket }
func (s *heldSocketDrop) Drop() {
	s.dropped.Add(1)
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
}

func TestSocketBudgetHeldUntilResourceCloses(t *testing.T) {
	table := NewResourceTableWithLimits(4, 1)
	s := &heldSocketDrop{entered: make(chan struct{}), release: make(chan struct{})}
	handle := table.Add(s)
	done := make(chan struct{})
	go func() { table.Remove(handle); close(done) }()
	<-s.entered
	lease, err := table.SocketBudget().Acquire()
	close(s.release)
	<-done
	if !errors.Is(err, ErrSocketLimit) {
		lease.Release()
		t.Fatal("capacity became available before socket finished closing")
	}
	if table.SocketBudget().Used() != 0 || s.dropped.Load() != 1 {
		t.Fatal("socket reservation or resource was not released exactly once")
	}
}

func TestClosedResourceTablePreservesFailedAddOwnership(t *testing.T) {
	for _, table := range []*ResourceTable{NewResourceTable(), NewResourceTableWithLimits(4, 1)} {
		if err := table.Close(); err != nil {
			t.Fatal(err)
		}
		s := &heldSocketDrop{}
		if handle, err := table.TryAdd(s); handle != 0 || !errors.Is(err, resource.ErrClosed) {
			t.Fatalf("closed insertion: handle=%d err=%v", handle, err)
		}
		if s.dropped.Load() != 0 {
			t.Fatal("failed TryAdd consumed caller-owned resource")
		}
		if budget := table.SocketBudget(); budget != nil && budget.Used() != 0 {
			t.Fatal("failed publication leaked a socket reservation")
		}
		s.Drop()
	}
}

func TestSocketBudget_BasicAndIdempotency(t *testing.T) {
	budget := NewSocketBudget(2)
	if budget.Capacity() != 2 {
		t.Fatalf("expected capacity 2, got %d", budget.Capacity())
	}
	if budget.Used() != 0 || budget.Available() != 2 {
		t.Fatalf("expected 0 used, 2 available; got used=%d, avail=%d", budget.Used(), budget.Available())
	}

	lease1, err := budget.Acquire()
	if err != nil {
		t.Fatalf("unexpected error acquiring lease1: %v", err)
	}
	if budget.Used() != 1 || budget.Available() != 1 {
		t.Fatalf("expected 1 used, 1 available; got used=%d, avail=%d", budget.Used(), budget.Available())
	}

	lease2, err := budget.Acquire()
	if err != nil {
		t.Fatalf("unexpected error acquiring lease2: %v", err)
	}
	if budget.Used() != 2 || budget.Available() != 0 {
		t.Fatalf("expected 2 used, 0 available; got used=%d, avail=%d", budget.Used(), budget.Available())
	}

	// Over limit
	lease3, err := budget.Acquire()
	if !errors.Is(err, ErrSocketLimit) {
		t.Fatalf("expected ErrSocketLimit, got %v", err)
	}
	if lease3 != nil {
		t.Fatalf("expected nil lease on failure")
	}

	// Idempotent release
	lease1.Release()
	lease1.Release()
	lease1.Release()
	if budget.Used() != 1 || budget.Available() != 1 {
		t.Fatalf("expected 1 used after idempotent release, got used=%d, avail=%d", budget.Used(), budget.Available())
	}

	// Re-acquire succeeds
	lease3, err = budget.Acquire()
	if err != nil {
		t.Fatalf("failed to re-acquire after release: %v", err)
	}
	if budget.Used() != 2 {
		t.Fatalf("expected 2 used, got %d", budget.Used())
	}

	lease2.Release()
	lease3.Release()
	if budget.Used() != 0 {
		t.Fatalf("expected 0 used, got %d", budget.Used())
	}
}

func TestSocketBudget_ConcurrentReservationsCannotExceedCap(t *testing.T) {
	const cap = 5
	const goroutines = 50
	const iterations = 50

	budget := NewSocketBudget(cap)
	var active atomic.Int32
	var maxActive atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				lease, err := budget.Acquire()
				if err == nil {
					curr := active.Add(1)
					for {
						m := maxActive.Load()
						if curr <= m || maxActive.CompareAndSwap(m, curr) {
							break
						}
					}
					if curr > cap {
						t.Errorf("concurrent reservations exceeded cap: active=%d, cap=%d", curr, cap)
					}
					active.Add(-1)
					lease.Release()
				}
			}
		}()
	}

	wg.Wait()
	if maxActive.Load() > cap {
		t.Fatalf("max active (%d) exceeded cap (%d)", maxActive.Load(), cap)
	}
	if budget.Used() != 0 {
		t.Fatalf("expected 0 used at end, got %d", budget.Used())
	}
}

func TestResourceTable_SocketBudgetIntegrationAndCleanup(t *testing.T) {
	table := NewResourceTableWithLimits(10, 2)
	sb := table.SocketBudget()
	if sb == nil {
		t.Fatal("expected non-nil socket budget from table")
	}
	if sb.Capacity() != 2 {
		t.Fatalf("expected capacity 2, got %d", sb.Capacity())
	}

	// Add TCP socket
	tcpSocket := &countedResource{kind: ResourceTCPSocket}
	h1, err := table.TryAdd(tcpSocket)
	if err != nil {
		t.Fatalf("failed to add TCP socket: %v", err)
	}
	if sb.Used() != 1 {
		t.Fatalf("expected 1 socket used, got %d", sb.Used())
	}

	// Add UDP socket
	udpSocket := &countedResource{kind: ResourceUDPSocket}
	h2, err := table.TryAdd(udpSocket)
	if err != nil {
		t.Fatalf("failed to add UDP socket: %v", err)
	}
	if sb.Used() != 2 {
		t.Fatalf("expected 2 sockets used, got %d", sb.Used())
	}

	// Exceed socket limit with non-socket still allowed if handles remain
	thirdSocket := &countedResource{kind: ResourceTCPSocket}
	_, err = table.TryAdd(thirdSocket)
	if !errors.Is(err, ErrSocketLimit) {
		t.Fatalf("expected ErrSocketLimit, got %v", err)
	}
	// TryAdd failure: caller retains resource
	if thirdSocket.drops.Load() != 0 {
		t.Fatalf("TryAdd failure should not drop caller resource")
	}

	// Non-socket resource still succeeds
	nonSocket := &countedResource{kind: ResourcePollable}
	h3, err := table.TryAdd(nonSocket)
	if err != nil {
		t.Fatalf("failed to add non-socket resource: %v", err)
	}

	// Remove TCP socket frees slot
	table.Remove(h1)
	if tcpSocket.drops.Load() != 1 {
		t.Fatalf("expected tcpSocket to be dropped once, got %d", tcpSocket.drops.Load())
	}
	if sb.Used() != 1 {
		t.Fatalf("expected 1 socket used after remove, got %d", sb.Used())
	}

	// Now thirdSocket can be added
	h4, err := table.TryAdd(thirdSocket)
	if err != nil {
		t.Fatalf("failed to add thirdSocket after slot freed: %v", err)
	}
	if sb.Used() != 2 {
		t.Fatalf("expected 2 sockets used, got %d", sb.Used())
	}

	// Close / Clear drops all resources and releases all quotas
	if err := table.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
	if sb.Used() != 0 {
		t.Fatalf("expected 0 sockets used after table.Close(), got %d", sb.Used())
	}
	if udpSocket.drops.Load() != 1 {
		t.Fatalf("expected udpSocket dropped on Close, got %d", udpSocket.drops.Load())
	}
	if thirdSocket.drops.Load() != 1 {
		t.Fatalf("expected thirdSocket dropped on Close, got %d", thirdSocket.drops.Load())
	}
	if nonSocket.drops.Load() != 1 {
		t.Fatalf("expected nonSocket dropped on Close, got %d", nonSocket.drops.Load())
	}
	_ = h2
	_ = h3
	_ = h4
}

func TestTwoActorBudgetsIndependent(t *testing.T) {
	tableA := NewResourceTableWithLimits(5, 1)
	tableB := NewResourceTableWithLimits(5, 1)

	budgetA := tableA.SocketBudget()
	budgetB := tableB.SocketBudget()

	if budgetA == budgetB {
		t.Fatal("expected separate socket budgets for independent tables")
	}

	// Consume quota in table A
	sockA := &countedResource{kind: ResourceTCPSocket}
	hA, err := tableA.TryAdd(sockA)
	if err != nil {
		t.Fatalf("failed to add sockA: %v", err)
	}
	if budgetA.Used() != 1 {
		t.Fatalf("expected budgetA used=1, got %d", budgetA.Used())
	}
	if budgetB.Used() != 0 {
		t.Fatalf("expected budgetB used=0, got %d", budgetB.Used())
	}

	// Table B still has available quota
	sockB := &countedResource{kind: ResourceUDPSocket}
	hB, err := tableB.TryAdd(sockB)
	if err != nil {
		t.Fatalf("tableB should have available quota, got %v", err)
	}
	if budgetB.Used() != 1 {
		t.Fatalf("expected budgetB used=1, got %d", budgetB.Used())
	}

	tableA.Remove(hA)
	tableB.Remove(hB)
	if budgetA.Used() != 0 || budgetB.Used() != 0 {
		t.Fatalf("expected both budgets used=0 after removal")
	}
}
