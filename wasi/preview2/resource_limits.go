package preview2

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/wippyai/wasm-runtime/resource"
)

var ErrResourceLimit = errors.New("WASI resource handle limit exceeded")
var ErrSocketLimit = errors.New("WASI socket limit exceeded")

// SocketLease represents an acquired reservation in a SocketBudget.
// Release is idempotent and safe for concurrent calls.
type SocketLease struct {
	budget   *SocketBudget
	released atomic.Bool
}

// Release drops the lease back to its owning SocketBudget.
// Multiple calls to Release are safe and no-op after the first.
func (l *SocketLease) Release() {
	if l == nil {
		return
	}
	if l.released.CompareAndSwap(false, true) {
		if l.budget != nil {
			l.budget.release()
		}
	}
}

// SocketBudget manages bounded concurrent socket allocations across WASI profiles and hosts.
type SocketBudget struct {
	mu       sync.Mutex
	capacity int
	used     int
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

// release decrements the used count.
func (b *SocketBudget) release() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used--
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
