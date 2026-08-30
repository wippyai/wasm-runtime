package linker

import (
	"fmt"
	"sync"
)

// Handle is a resource handle (32-bit unsigned integer per spec)
type Handle uint32

// ResourceStore manages multiple resource tables by type
type ResourceStore struct {
	tables map[uint32]*ResourceTable
	mu     sync.RWMutex
	closed bool
}

// NewResourceStore creates an empty resource store.
func NewResourceStore() *ResourceStore {
	return &ResourceStore{
		tables: make(map[uint32]*ResourceTable),
	}
}

// Table returns or creates a resource table for the given type ID
func (s *ResourceStore) Table(typeID uint32) *ResourceTable {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return &ResourceTable{closed: true}
	}

	if t, ok := s.tables[typeID]; ok {
		return t
	}

	t := NewResourceTable(nil)
	s.tables[typeID] = t
	return t
}

// Close drops every live resource in the store. It is safe to call more than
// once.
func (s *ResourceStore) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	tables := make([]*ResourceTable, 0, len(s.tables))
	for _, table := range s.tables {
		tables = append(tables, table)
	}
	s.mu.Unlock()

	for _, table := range tables {
		table.close()
	}
}

// TableWithDtor returns or creates a resource table with a destructor
func (s *ResourceStore) TableWithDtor(typeID uint32, dtor func(rep uint32)) *ResourceTable {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return &ResourceTable{dtor: dtor, closed: true}
	}

	if t, ok := s.tables[typeID]; ok {
		return t
	}

	t := NewResourceTable(dtor)
	s.tables[typeID] = t
	return t
}

// ResourceEntry represents an entry in the resource table
type ResourceEntry struct {
	Rep       uint32 // The representation value
	RefCount  int32  // Reference count (0 = dropped, >0 = owned)
	LendCount int32  // Number of active borrows (prevents drop while borrowed)
}

// ResourceTable manages resources of a single type
type ResourceTable struct {
	dtor     func(rep uint32)
	entries  []ResourceEntry
	freeList []Handle
	mu       sync.Mutex
	closed   bool
}

func (t *ResourceTable) close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	if t.dtor == nil {
		for i := range t.entries {
			t.entries[i].RefCount = 0
		}
		t.freeList = nil
		t.mu.Unlock()
		return
	}

	reps := make([]uint32, 0, len(t.entries))
	for i := range t.entries {
		if t.entries[i].RefCount > 0 {
			reps = append(reps, t.entries[i].Rep)
			t.entries[i].RefCount = 0
		}
	}
	t.freeList = nil
	t.mu.Unlock()

	for _, rep := range reps {
		t.dtor(rep)
	}
}

// NewResourceTable creates an empty table with optional destructor.
func NewResourceTable(dtor func(rep uint32)) *ResourceTable {
	return &ResourceTable{
		dtor: dtor,
	}
}

// New creates a resource with ref count 1.
func (t *ResourceTable) New(rep uint32) Handle {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0
	}

	entry := ResourceEntry{
		Rep:      rep,
		RefCount: 1,
	}

	// Try to reuse a free slot
	if len(t.freeList) > 0 {
		handle := t.freeList[len(t.freeList)-1]
		t.freeList = t.freeList[:len(t.freeList)-1]
		t.entries[handle] = entry
		return handle
	}

	// Allocate new slot
	handle := Handle(len(t.entries))
	t.entries = append(t.entries, entry)
	return handle
}

// Rep returns the representation value, or false if invalid/dropped.
func (t *ResourceTable) Rep(h Handle) (uint32, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(h) >= len(t.entries) {
		return 0, false
	}

	entry := &t.entries[h]
	if entry.RefCount == 0 {
		return 0, false
	}

	return entry.Rep, true
}

// Drop decrements ref count. Returns needsDtor=true when count reaches zero.
func (t *ResourceTable) Drop(h Handle) (rep uint32, needsDtor bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(h) >= len(t.entries) {
		return 0, false, fmt.Errorf("resource: invalid handle %d", h)
	}

	entry := &t.entries[h]
	if entry.RefCount == 0 {
		return 0, false, fmt.Errorf("resource: handle %d already dropped", h)
	}

	if entry.LendCount > 0 {
		return 0, false, fmt.Errorf("resource: handle %d has %d active borrows", h, entry.LendCount)
	}

	entry.RefCount--
	if entry.RefCount == 0 {
		rep = entry.Rep
		t.freeList = append(t.freeList, h)
		return rep, t.dtor != nil, nil
	}

	return 0, false, nil
}

// Borrow increments lend count. Prevents drop until EndBorrow is called.
func (t *ResourceTable) Borrow(h Handle) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(h) >= len(t.entries) {
		return fmt.Errorf("resource: invalid handle %d", h)
	}

	entry := &t.entries[h]
	if entry.RefCount == 0 {
		return fmt.Errorf("resource: handle %d is dropped", h)
	}

	entry.LendCount++
	return nil
}

// EndBorrow decrements lend count. Must be called once per Borrow.
func (t *ResourceTable) EndBorrow(h Handle) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(h) >= len(t.entries) {
		return fmt.Errorf("resource: invalid handle %d", h)
	}

	entry := &t.entries[h]
	if entry.LendCount <= 0 {
		return fmt.Errorf("resource: handle %d has no active borrows", h)
	}

	entry.LendCount--
	return nil
}

// Clone increments ref count for own handles.
func (t *ResourceTable) Clone(h Handle) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if int(h) >= len(t.entries) {
		return fmt.Errorf("resource: invalid handle %d", h)
	}

	entry := &t.entries[h]
	if entry.RefCount <= 0 {
		return fmt.Errorf("resource: handle %d is dropped or borrowed", h)
	}

	entry.RefCount++
	return nil
}

// Len returns the count of live resources for diagnostics.
// Len does not include dropped resources awaiting reuse.
func (t *ResourceTable) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for _, e := range t.entries {
		if e.RefCount != 0 {
			count++
		}
	}
	return count
}

// RunDestructor invokes the destructor if one was configured.
// RunDestructor is called automatically by ResourceDrop when appropriate.
func (t *ResourceTable) RunDestructor(rep uint32) {
	if t.dtor != nil {
		t.dtor(rep)
	}
}
