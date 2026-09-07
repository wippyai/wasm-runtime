package linker

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero/experimental"
	"github.com/wippyai/wasm-runtime/memory/budget"
)

// MemoryAdmission owns the logical-linear-memory reservation for one component
// instance. It is deliberately separate from execution lifetime: its owner
// supplies onStop, which must be nonblocking and must make the owner domain
// stop accepting new guest work. MemoryAdmission itself stores no context.
//
// Admission accounts for guest linear-memory length, not Go heap or RSS. The
// owner must call ReleaseAfterModulesClosed only after it has stopped new work
// and joined all guest calls that could access the buffers.
type MemoryAdmission struct {
	charge   *budget.Reservation
	onStop   func()
	slots    []*admittedMemory
	next     int
	mu       sync.Mutex
	stopOnce sync.Once
	stopped  bool
	closed   bool
}

type admittedMemory struct {
	owner       *MemoryAdmission
	data        []byte
	plan        OwnedMemory
	maximum     uint64
	initialized bool
}

// AdmitMemoryPlan reserves the initial logical bytes of plan before a module
// can run. onStop is called once, outside the admission lock, when any backing
// memory is freed or the owner explicitly stops the admission. It must not
// block or panic.
func AdmitMemoryPlan(plan []OwnedMemory, b *budget.Budget, onStop func()) (*MemoryAdmission, error) {
	if b == nil {
		return nil, fmt.Errorf("nil memory budget")
	}
	total, err := InitialMemoryBytes(plan)
	if err != nil {
		return nil, err
	}

	// Wazero's allocator API has no initial-allocation error return. Validate
	// every possible backing slice length before accepting the reservation so a
	// valid component cannot later fail only because its address range is not
	// representable by this host.
	maxHostBytes := uint64(^uint(0) >> 1)
	for _, p := range plan {
		if p.MinimumPages > maxHostBytes/wasmPageSize {
			return nil, fmt.Errorf("memory minimum exceeds host address range")
		}
		if p.HasMaximum && p.MaximumPages > maxHostBytes/wasmPageSize {
			return nil, fmt.Errorf("memory maximum exceeds host address range")
		}
	}

	charge, err := b.Reserve(total)
	if err != nil {
		return nil, err
	}
	a := &MemoryAdmission{charge: charge, onStop: onStop}
	for _, p := range plan {
		a.slots = append(a.slots, &admittedMemory{owner: a, plan: p})
	}
	return a, nil
}

const wasmPageSize = 65536

// AdmitMemory derives the allocations actually owned by pre and reserves their
// initial bytes. The returned admission is independent of the caller's
// context; use WithAllocator to decorate the context used for instantiation.
func (pre *InstancePre) AdmitMemory(b *budget.Budget, onStop func()) (*MemoryAdmission, error) {
	plan, err := pre.OwnedMemoryPlan()
	if err != nil {
		return nil, err
	}
	return AdmitMemoryPlan(plan, b, onStop)
}

// WithAllocator installs this admission as Wazero's memory allocator. It does
// not install cancellation, a close notifier, or any other execution-lifetime
// behavior. All allocations in the admitted plan retain their initial-byte
// reservation if the owner stops during Wazero construction; see Stop.
func (a *MemoryAdmission) WithAllocator(ctx context.Context) context.Context {
	return experimental.WithMemoryAllocator(ctx, a)
}

// Stop prevents memory growth and asks the owner to stop its domain. It is
// idempotent. Every slot in the admitted plan may still initialize its already-
// reserved minimum: Wazero offers no Allocate error result and cancellation can
// arrive between core constructors. onStop runs outside the lock and is
// therefore allowed to call back into the owner, but it must remain nonblocking.
func (a *MemoryAdmission) Stop() {
	if a == nil {
		return
	}
	a.stopOnce.Do(func() {
		a.mu.Lock()
		a.stopped = true
		onStop := a.onStop
		a.mu.Unlock()
		if onStop != nil {
			onStop()
		}
	})
}

// Allocate implements experimental.MemoryAllocator. Allocation order is an
// instantiation-plan invariant. Every admitted slot remains assignable after
// Stop because its minimum was reserved before construction began. Assignment
// after release or beyond the plan is owner/programming misuse.
func (a *MemoryAdmission) Allocate(_ uint64, maximum uint64) experimental.LinearMemory {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		panic("memory allocation after admission release")
	}
	if a.next >= len(a.slots) {
		panic("memory allocation differs from admitted component plan")
	}
	slot := a.slots[a.next]
	a.next++
	minimum := slot.plan.MinimumPages * wasmPageSize
	if minimum > maximum {
		panic("backend memory maximum below admitted minimum")
	}
	slot.maximum = maximum
	return slot
}

func (m *admittedMemory) Reallocate(size uint64) []byte {
	a := m.owner
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		panic("memory reallocation after admission release")
	}
	if size > m.maximum {
		return nil
	}
	if !m.initialized {
		// Initial bytes were admitted before any module/start function was run.
		if size != m.plan.MinimumPages*wasmPageSize {
			panic("initial memory differs from admitted minimum")
		}
		capacity := size
		if m.plan.Shared {
			capacity = m.maximum
		}
		m.data = make([]byte, size, capacity)
		m.initialized = true
		return m.data
	}
	if a.stopped {
		return nil
	}
	old := uint64(len(m.data))
	if size < old {
		panic("backend attempted to shrink linear memory")
	}
	if size == old {
		return m.data
	}
	if err := a.charge.Grow(size - old); err != nil {
		return nil
	}
	if m.plan.Shared {
		// Shared memory needs a fixed backing address. Allocate reserved maximum
		// capacity during initialization and extend only the logical view here.
		m.data = m.data[:size]
	} else {
		next := make([]byte, size)
		copy(next, m.data)
		m.data = next
	}
	return m.data
}

// Free is Wazero's raw backend callback. Imported aliases can trigger it and
// failed startup can leave some slots unused, so it stops the owner domain but
// cannot release buffers or budget until owner teardown has reached quiescence.
func (m *admittedMemory) Free() { m.owner.Stop() }

// ReleaseAfterModulesClosed drops all backing buffers and releases the logical
// reservation. It is idempotent. Its caller must have stopped and joined the
// owner domain, including every module close and every guest call.
func (a *MemoryAdmission) ReleaseAfterModulesClosed() {
	if a == nil {
		return
	}
	a.Stop()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.closed = true
	for _, m := range a.slots {
		m.data = nil
	}
	a.charge.Release()
}
