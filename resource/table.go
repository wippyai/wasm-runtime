package resource

import (
	"sync"
)

// UnifiedTable implements the Table interface using a Backend for storage.
type UnifiedTable struct {
	backend   *LocalBackend
	observers []Observer
	obsMu     sync.RWMutex
	closed    bool
	closeMu   sync.RWMutex
}

// NewTable creates a new unified table with a LocalBackend.
func NewTable() *UnifiedTable {
	return &UnifiedTable{
		backend: NewLocalBackend(),
	}
}

// Insert adds a value and returns its handle.
func (t *UnifiedTable) Insert(typeID uint32, value any) Handle {
	t.closeMu.RLock()
	if t.closed {
		t.closeMu.RUnlock()
		return 0
	}
	t.closeMu.RUnlock()

	handle, err := t.backend.Create(typeID, value)
	if err != nil {
		return 0
	}

	t.notify(Event{
		Type:   EventCreated,
		Handle: handle,
		TypeID: typeID,
		Value:  value,
	})

	return handle
}

// Get retrieves a value by handle.
func (t *UnifiedTable) Get(handle Handle) (any, bool) {
	return t.backend.Get(handle)
}

// GetTyped retrieves a value only if it matches the expected type.
func (t *UnifiedTable) GetTyped(handle Handle, typeID uint32) (any, bool) {
	actualTypeID, ok := t.backend.TypeID(handle)
	if !ok || actualTypeID != typeID {
		return nil, false
	}
	return t.backend.Get(handle)
}

// Remove drops a resource and returns (value, true) if found.
func (t *UnifiedTable) Remove(handle Handle) (any, bool) {
	typeID, _ := t.backend.TypeID(handle)
	value, ok := t.backend.Drop(handle)
	if !ok {
		return nil, false
	}

	if d, ok := value.(Dropper); ok {
		d.Drop()
	}

	t.notify(Event{
		Type:   EventDropped,
		Handle: handle,
		TypeID: typeID,
		Value:  value,
	})

	return value, true
}

// Subscribe adds an observer for lifecycle events.
func (t *UnifiedTable) Subscribe(o Observer) {
	t.obsMu.Lock()
	defer t.obsMu.Unlock()
	t.observers = append(t.observers, o)
}

// Unsubscribe removes an observer.
func (t *UnifiedTable) Unsubscribe(o Observer) {
	t.obsMu.Lock()
	defer t.obsMu.Unlock()
	for i, obs := range t.observers {
		if obs == o {
			t.observers = append(t.observers[:i], t.observers[i+1:]...)
			return
		}
	}
}

// Len returns the number of active resources.
func (t *UnifiedTable) Len() int {
	return t.backend.Len()
}

// Clear drops all resources.
func (t *UnifiedTable) Clear() {
	// Collect handles first to avoid holding lock during Remove
	var handles []Handle
	t.backend.Each(func(h Handle, typeID uint32, value any) bool {
		handles = append(handles, h)
		return true
	})
	for _, h := range handles {
		t.Remove(h)
	}
}

// Close releases all resources and stops accepting operations.
func (t *UnifiedTable) Close() error {
	t.closeMu.Lock()
	t.closed = true
	t.closeMu.Unlock()

	return t.backend.Close()
}

// Backend returns the underlying backend for ABI operations.
func (t *UnifiedTable) Backend() ABIBackend {
	return t.backend
}

func (t *UnifiedTable) notify(e Event) {
	t.obsMu.RLock()
	defer t.obsMu.RUnlock()
	for _, o := range t.observers {
		o.OnResourceEvent(e)
	}
}
