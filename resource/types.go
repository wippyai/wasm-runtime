package resource

// Handle is an opaque reference to a resource in a table.
// Handle 0 is reserved and always invalid.
type Handle uint32

// Event types for resource lifecycle notifications.
type EventType uint8

const (
	EventCreated EventType = iota
	EventDropped
	EventBorrowed
	EventBorrowReturned
)

// Event represents a resource lifecycle event.
type Event struct {
	Value  any
	Handle Handle
	TypeID uint32
	Type   EventType
}

// Observer receives notifications about resource lifecycle events.
type Observer interface {
	OnResourceEvent(Event)
}

// Backend provides the underlying storage mechanism for resources.
type Backend interface {
	// Create stores a value and returns a handle.
	Create(typeID uint32, value any) (Handle, error)

	// Get retrieves a value by handle.
	Get(handle Handle) (any, bool)

	// Drop removes a resource and returns (value, true) if destructor should be called.
	// Returns (nil, false) if handle is invalid or has outstanding borrows.
	Drop(handle Handle) (any, bool)

	// Close releases all resources held by the backend.
	Close() error
}

// ABIBackend extends Backend with Component Model ABI operations.
// This is used by the canonical ABI for resource.new/resource.rep/resource.drop.
type ABIBackend interface {
	Backend

	// NewFromRep creates a handle from a representation value (typically a memory pointer).
	NewFromRep(typeID uint32, rep uint32) Handle

	// Rep returns the representation value for a handle.
	Rep(handle Handle) (uint32, bool)

	// Borrow increments the borrow count for a handle.
	Borrow(handle Handle) bool

	// ReturnBorrow decrements the borrow count for a handle.
	ReturnBorrow(handle Handle) bool
}

// Table manages resources with type information and observer support.
type Table interface {
	// Insert adds a value and returns its handle.
	Insert(typeID uint32, value any) Handle

	// Get retrieves a value by handle.
	Get(handle Handle) (any, bool)

	// GetTyped retrieves a value only if it matches the expected type.
	GetTyped(handle Handle, typeID uint32) (any, bool)

	// Remove drops a resource and returns (value, true) if found.
	Remove(handle Handle) (any, bool)

	// Subscribe adds an observer for lifecycle events.
	Subscribe(Observer)

	// Unsubscribe removes an observer.
	Unsubscribe(Observer)

	// Len returns the number of active resources.
	Len() int

	// Clear drops all resources.
	Clear()

	// Close releases all resources and stops accepting operations.
	Close() error
}

// TypedTable provides type-safe access to resources of a specific type.
type TypedTable[T any] interface {
	// Insert adds a value and returns its handle.
	Insert(value T) Handle

	// Get retrieves a value by handle.
	Get(handle Handle) (T, bool)

	// Remove drops a resource and returns (value, true) if found.
	Remove(handle Handle) (T, bool)

	// Len returns the number of active resources.
	Len() int

	// Each iterates over all active resources.
	Each(func(Handle, T) bool)
}

// Dropper is optionally implemented by resource values that need cleanup.
type Dropper interface {
	Drop()
}
