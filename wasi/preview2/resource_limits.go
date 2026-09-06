package preview2

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/wippyai/wasm-runtime/resource"
)

var (
	ErrResourceLimit       = errors.New("WASI resource handle limit exceeded")
	ErrSocketLimit         = errors.New("WASI socket limit exceeded")
	ErrInvalidLease        = errors.New("invalid or released socket lease")
	ErrLeaseBudgetMismatch = errors.New("socket lease budget mismatch")
	ErrLeaseTypeMismatch   = errors.New("resource type does not match socket lease")
	ErrLeaseAlreadyAdopted = errors.New("socket lease already adopted")
)

// SocketLease represents an acquired reservation in a SocketBudget.
// Release is idempotent and safe for concurrent calls.
type SocketLease struct {
	budget *SocketBudget
	state  atomic.Uint32
}

// Release drops the lease back to its owning SocketBudget.
// Multiple calls are safe. After adoption only the resource table can release
// the reservation; stale producer references cannot free a live socket.
func (l *SocketLease) Release() {
	if l == nil {
		return
	}
	if l.state.CompareAndSwap(0, 2) {
		if l.budget != nil {
			l.budget.release()
		}
	}
}

// Budget returns the owning SocketBudget, or nil if unbounded.
func (l *SocketLease) Budget() *SocketBudget {
	if l == nil {
		return nil
	}
	return l.budget
}

// IsAdopted returns whether this lease has been adopted into a resource table.
func (l *SocketLease) IsAdopted() bool {
	if l == nil {
		return false
	}
	return l.state.Load() == 1
}

// IsReleased returns whether this lease has been released.
func (l *SocketLease) IsReleased() bool {
	if l == nil {
		return true
	}
	return l.state.Load() == 2
}

// SocketBudget manages bounded concurrent socket allocations across WASI profiles and hosts.
type SocketBudget struct {
	availCh  chan struct{}
	capacity int
	used     int
	mu       sync.Mutex
}

// NewSocketBudget creates a new socket budget with positive capacity.
func NewSocketBudget(capacity int) *SocketBudget {
	if capacity <= 0 {
		panic("socket budget capacity must be positive")
	}
	return &SocketBudget{capacity: capacity}
}

// Capacity returns the maximum concurrent socket limit.
func (b *SocketBudget) Capacity() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.capacity
}

// Used returns the number of active socket reservations.
func (b *SocketBudget) Used() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// Available returns the number of remaining socket slots.
func (b *SocketBudget) Available() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.capacity - b.used
}

// Acquire reserves one socket slot and returns an idempotent lease.
// If the budget is exhausted, ErrSocketLimit is returned.
func (b *SocketBudget) Acquire() (*SocketLease, error) {
	if b == nil {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.capacity {
		return nil, ErrSocketLimit
	}
	b.used++
	return &SocketLease{budget: b}, nil
}

// release decrements the used count and notifies waiters of availability.
func (b *SocketBudget) release() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used--
	if b.availCh != nil {
		close(b.availCh)
		b.availCh = nil
	}
}

// AvailNotify returns a channel that is closed if budget capacity is currently available.
// If the budget is exhausted, it returns a channel that closes when a reservation is released.
func (b *SocketBudget) AvailNotify() <-chan struct{} {
	if b == nil {
		return closedPollableChan
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used < b.capacity {
		return closedPollableChan
	}
	if b.availCh == nil {
		b.availCh = make(chan struct{})
	}
	return b.availCh
}

// AcquireCancellable acquires a socket slot, waiting cancellably on live budget
// availability notifications if the budget is currently exhausted.
// It returns resource.ErrClosed (or nil if b is nil) when stop is signaled.
func (b *SocketBudget) AcquireCancellable(stop <-chan struct{}) (*SocketLease, error) {
	if b == nil {
		return nil, nil
	}
	for {
		select {
		case <-stop:
			return nil, resource.ErrClosed
		default:
		}
		b.mu.Lock()
		if b.used < b.capacity {
			b.used++
			b.mu.Unlock()
			return &SocketLease{budget: b}, nil
		}
		ch := b.availCh
		if ch == nil {
			ch = make(chan struct{})
			b.availCh = ch
		}
		b.mu.Unlock()

		if stop != nil {
			select {
			case <-stop:
				return nil, resource.ErrClosed
			case <-ch:
			}
		} else {
			<-ch
		}
	}
}

// resourceBudget counts live handles, including sockets which have not yet
// connected. Reservation precedes publication and release follows removal.
type resourceBudget struct {
	socketBudget *SocketBudget
	mu           sync.Mutex
	handles      int
	maxHandles   int
}

func isSocketType(kind ResourceType) bool {
	return kind == ResourceTCPSocket || kind == ResourceUDPSocket
}

func (b *resourceBudget) reserve(kind ResourceType) (*SocketLease, error) {
	b.mu.Lock()
	if b.handles >= b.maxHandles {
		b.mu.Unlock()
		return nil, ErrResourceLimit
	}
	b.handles++
	b.mu.Unlock()

	if isSocketType(kind) && b.socketBudget != nil {
		lease, err := b.socketBudget.Acquire()
		if err != nil {
			b.mu.Lock()
			b.handles--
			b.mu.Unlock()
			return nil, err
		}
		return lease, nil
	}
	return nil, nil
}

func (b *resourceBudget) releaseHandle() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handles--
}

// NewResourceTableWithBudget creates an isolated, bounded table with an explicit SocketBudget.
func NewResourceTableWithBudget(maxHandles int, socketBudget *SocketBudget) *ResourceTable {
	if maxHandles <= 0 || socketBudget == nil {
		panic("positive handle limit and socket budget required")
	}
	t := NewResourceTable()
	t.budget = &resourceBudget{
		maxHandles:   maxHandles,
		socketBudget: socketBudget,
	}
	return t
}

// NewResourceTableWithLimits creates an isolated, bounded table. Both limits
// must be positive. Existing unbounded scopes use NewResourceTable.
func NewResourceTableWithLimits(maxHandles, maxSockets int) *ResourceTable {
	if maxHandles <= 0 || maxSockets <= 0 {
		panic("resource limits must be positive")
	}
	return NewResourceTableWithBudget(maxHandles, NewSocketBudget(maxSockets))
}

// TryAdd reserves capacity and publishes a handle. On failure the caller retains
// ownership and must drop the resource. Add drops and traps on the same failure.
func (t *ResourceTable) TryAdd(r Resource) (uint32, error) {
	var lease *SocketLease
	if t.budget != nil {
		var err error
		lease, err = t.budget.reserve(r.Type())
		if err != nil {
			return 0, err
		}
	}
	handle := t.table.Add(&resourceAdapter{resource: r, budget: t.budget, lease: lease})
	if handle == 0 {
		// Publication failed: return capacity, but preserve caller ownership.
		lease.Release()
		if t.budget != nil {
			t.budget.releaseHandle()
		}
		return 0, resource.ErrClosed
	}
	return uint32(handle), nil
}

// TryAddWithSocketLease adopts an existing pre-reserved socket lease into the table
// without double charging the socket budget. It validates matching budget, type,
// and live lease, rejects double adoption, and rolls back failed publication while
// preserving caller ownership of the resource and lease.
func (t *ResourceTable) TryAddWithSocketLease(r Resource, lease *SocketLease) (uint32, error) {
	if r == nil {
		return 0, errors.New("nil resource")
	}
	if !isSocketType(r.Type()) {
		return 0, ErrLeaseTypeMismatch
	}

	tableBudget := t.SocketBudget()
	if tableBudget != nil {
		if lease == nil || lease.IsReleased() {
			return 0, ErrInvalidLease
		}
		if lease.budget != tableBudget {
			return 0, ErrLeaseBudgetMismatch
		}
	} else if lease != nil && lease.budget != nil {
		return 0, ErrLeaseBudgetMismatch
	}

	if lease != nil {
		if lease.IsReleased() {
			return 0, ErrInvalidLease
		}
		if !lease.state.CompareAndSwap(0, 1) {
			if lease.IsReleased() {
				return 0, ErrInvalidLease
			}
			return 0, ErrLeaseAlreadyAdopted
		}
	}

	if t.budget != nil {
		t.budget.mu.Lock()
		if t.budget.handles >= t.budget.maxHandles {
			t.budget.mu.Unlock()
			if lease != nil {
				lease.state.CompareAndSwap(1, 0)
			}
			return 0, ErrResourceLimit
		}
		t.budget.handles++
		t.budget.mu.Unlock()
	}

	handle := t.table.Add(&resourceAdapter{resource: r, budget: t.budget, lease: lease})
	if handle == 0 {
		if t.budget != nil {
			t.budget.releaseHandle()
		}
		if lease != nil {
			lease.state.CompareAndSwap(1, 0)
		}
		return 0, resource.ErrClosed
	}

	return uint32(handle), nil
}

// AddWithSocketLease stores a socket resource with an adopted lease, panicking on failure.
func (t *ResourceTable) AddWithSocketLease(r Resource, lease *SocketLease) uint32 {
	handle, err := t.TryAddWithSocketLease(r, lease)
	if err != nil {
		if r != nil {
			r.Drop()
		}
		lease.Release()
		panic(err)
	}
	return handle
}

// releaseTable is reserved for the owning table adapter. A caller retaining an
// adopted lease cannot release its reservation before the socket physically closes.
func (l *SocketLease) releaseTable() {
	if l == nil {
		return
	}
	for {
		state := l.state.Load()
		if state == 2 {
			return
		}
		if l.state.CompareAndSwap(state, 2) {
			if l.budget != nil {
				l.budget.release()
			}
			return
		}
	}
}
