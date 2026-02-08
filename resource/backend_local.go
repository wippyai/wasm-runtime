package resource

import (
	"errors"
	"sync"
)

var (
	ErrClosed            = errors.New("resource backend closed")
	ErrOutstandingBorrow = errors.New("cannot drop resource with outstanding borrows")
)

// LocalBackend is an in-memory resource backend with borrow tracking.
// Implements both Backend and ABIBackend interfaces.
type LocalBackend struct {
	entries  []entry
	freeList []Handle
	mu       sync.RWMutex
	closed   bool
}

type entry struct {
	value       any
	typeID      uint32
	rep         uint32
	borrowCount uint32
	valid       bool
}

// NewLocalBackend creates a new in-memory backend.
func NewLocalBackend() *LocalBackend {
	return &LocalBackend{
		entries:  make([]entry, 0, 64),
		freeList: make([]Handle, 0, 16),
	}
}

// Create stores a value and returns a handle.
func (b *LocalBackend) Create(typeID uint32, value any) (Handle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, ErrClosed
	}

	e := entry{
		typeID: typeID,
		value:  value,
		valid:  true,
	}

	if len(b.freeList) > 0 {
		handle := b.freeList[len(b.freeList)-1]
		b.freeList = b.freeList[:len(b.freeList)-1]
		b.entries[handle-1] = e
		return handle, nil
	}

	b.entries = append(b.entries, e)
	return Handle(len(b.entries)), nil
}

// Get retrieves a value by handle.
func (b *LocalBackend) Get(handle Handle) (any, bool) {
	if handle == 0 {
		return nil, false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	idx := handle - 1
	if int(idx) >= len(b.entries) {
		return nil, false
	}

	e := b.entries[idx]
	if !e.valid {
		return nil, false
	}
	return e.value, true
}

// Drop removes a resource and returns (value, true) if destructor should be called.
func (b *LocalBackend) Drop(handle Handle) (any, bool) {
	if handle == 0 {
		return nil, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	idx := handle - 1
	if int(idx) >= len(b.entries) {
		return nil, false
	}

	e := &b.entries[idx]
	if !e.valid {
		return nil, false
	}

	if e.borrowCount > 0 {
		return nil, false
	}

	value := e.value
	e.valid = false
	e.value = nil
	e.rep = 0
	e.borrowCount = 0
	b.freeList = append(b.freeList, handle)

	return value, true
}

// Close releases all resources.
func (b *LocalBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}
	b.closed = true

	for i := range b.entries {
		if b.entries[i].valid {
			if d, ok := b.entries[i].value.(Dropper); ok {
				d.Drop()
			}
			b.entries[i].valid = false
			b.entries[i].value = nil
		}
	}

	b.entries = nil
	b.freeList = nil
	return nil
}

// NewFromRep creates a handle from a representation value.
func (b *LocalBackend) NewFromRep(typeID uint32, rep uint32) Handle {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0
	}

	e := entry{
		typeID: typeID,
		rep:    rep,
		valid:  true,
	}

	if len(b.freeList) > 0 {
		handle := b.freeList[len(b.freeList)-1]
		b.freeList = b.freeList[:len(b.freeList)-1]
		b.entries[handle-1] = e
		return handle
	}

	b.entries = append(b.entries, e)
	return Handle(len(b.entries))
}

// Rep returns the representation value for a handle.
func (b *LocalBackend) Rep(handle Handle) (uint32, bool) {
	if handle == 0 {
		return 0, false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	idx := handle - 1
	if int(idx) >= len(b.entries) {
		return 0, false
	}

	e := b.entries[idx]
	if !e.valid {
		return 0, false
	}
	return e.rep, true
}

// Borrow increments the borrow count for a handle.
func (b *LocalBackend) Borrow(handle Handle) bool {
	if handle == 0 {
		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	idx := handle - 1
	if int(idx) >= len(b.entries) {
		return false
	}

	e := &b.entries[idx]
	if !e.valid {
		return false
	}

	e.borrowCount++
	return true
}

// ReturnBorrow decrements the borrow count for a handle.
func (b *LocalBackend) ReturnBorrow(handle Handle) bool {
	if handle == 0 {
		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	idx := handle - 1
	if int(idx) >= len(b.entries) {
		return false
	}

	e := &b.entries[idx]
	if !e.valid || e.borrowCount == 0 {
		return false
	}

	e.borrowCount--
	return true
}

// TypeID returns the type ID for a handle.
func (b *LocalBackend) TypeID(handle Handle) (uint32, bool) {
	if handle == 0 {
		return 0, false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	idx := handle - 1
	if int(idx) >= len(b.entries) {
		return 0, false
	}

	e := b.entries[idx]
	if !e.valid {
		return 0, false
	}
	return e.typeID, true
}

// Len returns the number of active resources.
func (b *LocalBackend) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := 0
	for _, e := range b.entries {
		if e.valid {
			count++
		}
	}
	return count
}

// Each iterates over all active resources.
func (b *LocalBackend) Each(fn func(Handle, uint32, any) bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for i, e := range b.entries {
		if e.valid {
			if !fn(Handle(i+1), e.typeID, e.value) {
				break
			}
		}
	}
}
