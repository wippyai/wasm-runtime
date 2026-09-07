// Package budget accounts for admitted bytes across independent resource owners.
// It does not allocate memory or establish when an owner's storage is safe to
// release. That lifetime remains the resource owner's responsibility.
package budget

import (
	"errors"
	"sync"
)

var (
	ErrLimit    = errors.New("memory budget exceeded")
	ErrReleased = errors.New("memory reservation released")
)

type Budget struct {
	mu                sync.Mutex
	limit, used, peak uint64
}

// Reservation is a unique owner's charge. It must not be copied. Release is
// idempotent, so cleanup paths may converge without returning capacity twice.
type Reservation struct {
	budget   *Budget
	bytes    uint64
	released bool
}
type Usage struct{ Limit, Used, Peak uint64 }

// New creates a fixed budget; zero admits zero bytes, not unlimited memory.
func New(limit uint64) *Budget { return &Budget{limit: limit} }
func (b *Budget) Reserve(bytes uint64) (*Reservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bytes > b.limit-b.used {
		return nil, ErrLimit
	}
	b.used += bytes
	if b.used > b.peak {
		b.peak = b.used
	}
	return &Reservation{budget: b, bytes: bytes}, nil
}

// Grow charges additional bytes before the owner allocates them. Denial leaves
// all counters unchanged. If allocation later fails, Shrink rolls back the
// granted increment while leaving the existing owner's charge intact.
func (r *Reservation) Grow(bytes uint64) error {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.released {
		return ErrReleased
	}
	if bytes > b.limit-b.used {
		return ErrLimit
	}
	b.used += bytes
	r.bytes += bytes
	if b.used > b.peak {
		b.peak = b.used
	}
	return nil
}

// Shrink releases bytes only after the owner has discarded the associated
// storage. It rejects over-release rather than silently corrupting accounting.
func (r *Reservation) Shrink(bytes uint64) error {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.released {
		return ErrReleased
	}
	if bytes > r.bytes {
		return errors.New("reservation shrink exceeds owned bytes")
	}
	b.used -= bytes
	r.bytes -= bytes
	return nil
}
func (r *Reservation) Release() {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.released {
		return
	}
	b.used -= r.bytes
	r.bytes = 0
	r.released = true
}
func (b *Budget) Usage() Usage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Usage{Limit: b.limit, Used: b.used, Peak: b.peak}
}
