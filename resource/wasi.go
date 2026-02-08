package resource

// WASIResourceType identifies WASI preview2 resource types.
type WASIResourceType uint32

const (
	WASIPollable WASIResourceType = iota
	WASIInputStream
	WASIOutputStream
	WASIError
	WASIDescriptor
	WASIDirectoryEntryStream
	WASINetwork
	WASITCPSocket
	WASIUDPSocket
	WASIIPNameLookup
	WASITerminalInput
	WASITerminalOutput
)

// WASIResource is implemented by WASI resource types.
type WASIResource interface {
	WASIResourceType() WASIResourceType
}

// WASITable is a WASI-specific resource table that provides
// the API expected by wasi/preview2 while using UnifiedTable internally.
type WASITable struct {
	table *UnifiedTable
}

// NewWASITable creates a new WASI resource table.
func NewWASITable() *WASITable {
	return &WASITable{
		table: NewTable(),
	}
}

// Add adds a WASI resource and returns its handle.
func (t *WASITable) Add(r WASIResource) Handle {
	return t.table.Insert(uint32(r.WASIResourceType()), r)
}

// Get retrieves a WASI resource by handle.
func (t *WASITable) Get(handle Handle) (WASIResource, bool) {
	value, ok := t.table.Get(handle)
	if !ok {
		return nil, false
	}
	r, ok := value.(WASIResource)
	return r, ok
}

// GetTyped retrieves a WASI resource of specific type.
func (t *WASITable) GetTyped(handle Handle, resType WASIResourceType) (WASIResource, bool) {
	value, ok := t.table.GetTyped(handle, uint32(resType))
	if !ok {
		return nil, false
	}
	r, ok := value.(WASIResource)
	return r, ok
}

// Remove removes a WASI resource by handle.
func (t *WASITable) Remove(handle Handle) {
	t.table.Remove(handle)
}

// Clear drops all resources.
func (t *WASITable) Clear() {
	t.table.Clear()
}

// Close releases all resources.
func (t *WASITable) Close() error {
	return t.table.Close()
}

// Len returns the number of active resources.
func (t *WASITable) Len() int {
	return t.table.Len()
}

// Subscribe adds an observer for lifecycle events.
func (t *WASITable) Subscribe(o Observer) {
	t.table.Subscribe(o)
}

// Unsubscribe removes an observer.
func (t *WASITable) Unsubscribe(o Observer) {
	t.table.Unsubscribe(o)
}

// Table returns the underlying unified table.
func (t *WASITable) Table() *UnifiedTable {
	return t.table
}
