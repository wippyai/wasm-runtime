package preview2

import (
	"errors"
	"sync"
)

var ErrResourceLimit = errors.New("WASI resource handle limit exceeded")
var ErrSocketLimit = errors.New("WASI socket limit exceeded")

// resourceBudget counts live handles, including sockets which have not yet
// connected. Reservation precedes publication and release follows removal.
type resourceBudget struct {
	mu                     sync.Mutex
	handles, sockets       int
	maxHandles, maxSockets int
}

// NewResourceTableWithLimits creates an isolated, bounded table. Both limits
// must be positive. Existing unbounded scopes use NewResourceTable.
func NewResourceTableWithLimits(maxHandles, maxSockets int) *ResourceTable {
	if maxHandles <= 0 || maxSockets <= 0 {
		panic("resource limits must be positive")
	}
	t := NewResourceTable()
	t.budget = &resourceBudget{maxHandles: maxHandles, maxSockets: maxSockets}
	return t
}

func isSocketType(kind ResourceType) bool {
	return kind == ResourceTCPSocket || kind == ResourceUDPSocket
}

func (b *resourceBudget) reserve(kind ResourceType) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handles >= b.maxHandles {
		return ErrResourceLimit
	}
	if isSocketType(kind) && b.sockets >= b.maxSockets {
		return ErrSocketLimit
	}
	b.handles++
	if isSocketType(kind) {
		b.sockets++
	}
	return nil
}

func (b *resourceBudget) release(kind ResourceType) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handles--
	if isSocketType(kind) {
		b.sockets--
	}
}

// TryAdd reserves capacity and publishes a handle. On failure the caller retains
// ownership and must drop the resource. Add drops and traps on the same failure.
func (t *ResourceTable) TryAdd(r Resource) (uint32, error) {
	if t.budget != nil {
		if err := t.budget.reserve(r.Type()); err != nil {
			return 0, err
		}
	}
	return uint32(t.table.Add(&resourceAdapter{resource: r, budget: t.budget})), nil
}
