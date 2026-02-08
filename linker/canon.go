package linker

import (
	"context"
	"errors"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

var (
	// ErrNilMemory is returned when memory operations are attempted without memory
	ErrNilMemory = errors.New("nil memory")

	// ErrNilRealloc is returned when allocation is needed but realloc is nil
	ErrNilRealloc = errors.New("nil realloc function")

	// ErrMemoryRead is returned when memory read fails
	ErrMemoryRead = errors.New("memory read failed")

	// ErrMemoryWrite is returned when memory write fails
	ErrMemoryWrite = errors.New("memory write failed")

	// ErrUnsupportedEncoding is returned for non-UTF8 string encodings
	ErrUnsupportedEncoding = errors.New("only UTF-8 encoding is currently supported")
)

// CanonicalOptions holds options for canonical ABI operations
type CanonicalOptions struct {
	Memory   api.Memory
	Realloc  api.Function
	Encoding StringEncoding
}

// StringEncoding represents the string encoding for canonical ABI
type StringEncoding byte

const (
	StringEncodingUTF8 StringEncoding = iota
	StringEncodingUTF16
	StringEncodingLatin1
)

// LiftContext holds context for lifting values from WASM
type LiftContext struct {
	Ctx     context.Context
	Store   *ResourceStore
	Options CanonicalOptions
}

// LowerContext holds context for lowering values to WASM
type LowerContext struct {
	Ctx     context.Context
	Store   *ResourceStore
	Options CanonicalOptions
}

// CanonicalHandler wraps a component function with lift/lower
type CanonicalHandler struct {
	Lift     LiftFunc
	Lower    LowerFunc
	CoreFunc api.Function
	FuncType uint32
}

// LiftFunc converts flat WASM values to component values
type LiftFunc func(ctx *LiftContext, flat []uint64) (any, error)

// LowerFunc converts component values to flat WASM values
type LowerFunc func(ctx *LowerContext, value any) ([]uint64, error)

// NewLiftContext creates context for lifting WASM values to Go types.
// NewLiftContext is for implementing host functions that receive component values.
func NewLiftContext(ctx context.Context, opts CanonicalOptions, store *ResourceStore) *LiftContext {
	return &LiftContext{
		Ctx:     ctx,
		Options: opts,
		Store:   store,
	}
}

// NewLowerContext creates context for lowering Go types to WASM values.
// NewLowerContext is for implementing host functions that return component values.
func NewLowerContext(ctx context.Context, opts CanonicalOptions, store *ResourceStore) *LowerContext {
	return &LowerContext{
		Ctx:     ctx,
		Options: opts,
		Store:   store,
	}
}

// LiftString reads a string from memory.
// LiftString currently only supports UTF-8 encoding.
func LiftString(ctx *LiftContext, ptr, len uint32) (string, error) {
	if ctx.Options.Encoding != StringEncodingUTF8 {
		return "", ErrUnsupportedEncoding
	}
	if ctx.Options.Memory == nil {
		return "", ErrNilMemory
	}

	data, ok := ctx.Options.Memory.Read(ptr, len)
	if !ok {
		return "", fmt.Errorf("%w: ptr=%d len=%d", ErrMemoryRead, ptr, len)
	}

	return string(data), nil
}

// LowerString writes a string to memory using realloc.
// LowerString currently only supports UTF-8 encoding.
func LowerString(ctx *LowerContext, s string) (ptr, length uint32, err error) {
	if ctx.Options.Encoding != StringEncodingUTF8 {
		return 0, 0, ErrUnsupportedEncoding
	}
	if ctx.Options.Memory == nil {
		return 0, 0, ErrNilMemory
	}
	if ctx.Options.Realloc == nil {
		return 0, 0, ErrNilRealloc
	}

	data := []byte(s)
	length = uint32(len(data))

	// Call realloc(0, 0, 1, len) to allocate
	results, err := ctx.Options.Realloc.Call(ctx.Ctx, 0, 0, 1, uint64(length))
	if err != nil {
		return 0, 0, err
	}

	ptr = uint32(results[0])
	if !ctx.Options.Memory.Write(ptr, data) {
		return 0, 0, fmt.Errorf("%w: ptr=%d len=%d", ErrMemoryWrite, ptr, length)
	}

	return ptr, length, nil
}

// LiftList reads a list from linear memory into a Go slice.
// elemSize is the byte size of each element; liftElem decodes individual elements.
func LiftList(ctx *LiftContext, ptr, length uint32, elemSize uint32, liftElem func([]byte) (any, error)) ([]any, error) {
	if ctx.Options.Memory == nil {
		return nil, ErrNilMemory
	}

	result := make([]any, length)
	for i := uint32(0); i < length; i++ {
		elemPtr := ptr + i*elemSize
		data, ok := ctx.Options.Memory.Read(elemPtr, elemSize)
		if !ok {
			return nil, fmt.Errorf("%w: element %d at ptr=%d", ErrMemoryRead, i, elemPtr)
		}
		elem, err := liftElem(data)
		if err != nil {
			return nil, fmt.Errorf("lift element %d: %w", i, err)
		}
		result[i] = elem
	}

	return result, nil
}

// LiftOwn transfers ownership of a resource.
// LiftOwn removes the handle from the table and returns the representation.
// The destructor is NOT called - ownership is being transferred, not destroyed.
func LiftOwn(ctx *LiftContext, typeID uint32, handle Handle) (uint32, error) {
	table := ctx.Store.Table(typeID)

	// Get the rep first to return it
	rep, ok := table.Rep(handle)
	if !ok {
		return 0, fmt.Errorf("canon: invalid handle %d for type %d", handle, typeID)
	}

	// Drop the handle without running destructor
	// When lifting own, we're transferring ownership to the callee
	_, _, err := table.Drop(handle)
	if err != nil {
		return 0, err
	}

	return rep, nil
}

// LiftBorrow borrows a resource for the duration of a call.
// LiftBorrow returns the representation value. The caller MUST call EndLiftBorrow when done.
func LiftBorrow(ctx *LiftContext, typeID uint32, handle Handle) (uint32, error) {
	table := ctx.Store.Table(typeID)

	// First check the handle is valid
	rep, ok := table.Rep(handle)
	if !ok {
		return 0, fmt.Errorf("canon: invalid handle %d for type %d", handle, typeID)
	}

	// Mark as borrowed so owner can't drop during call
	if err := table.Borrow(handle); err != nil {
		return 0, err
	}

	return rep, nil
}

// EndLiftBorrow ends a borrow that was started with LiftBorrow.
// EndLiftBorrow must be called when the borrowed resource is no longer needed.
func EndLiftBorrow(ctx *LiftContext, typeID uint32, handle Handle) error {
	table := ctx.Store.Table(typeID)
	return table.EndBorrow(handle)
}

// LowerList allocates memory via realloc and writes list elements.
// LowerList returns (pointer, count) tuple for passing to WASM.
func LowerList(ctx *LowerContext, elemSize uint32, elems [][]byte) (ptr, length uint32, err error) {
	length = uint32(len(elems))
	if length == 0 {
		return 0, 0, nil
	}

	if ctx.Options.Memory == nil {
		return 0, 0, ErrNilMemory
	}
	if ctx.Options.Realloc == nil {
		return 0, 0, ErrNilRealloc
	}

	totalSize := uint64(length) * uint64(elemSize)
	if totalSize > 0xFFFFFFFF {
		return 0, 0, fmt.Errorf("canon: list too large: %d elements * %d bytes", length, elemSize)
	}

	// Call realloc(0, 0, align, size) to allocate
	results, err := ctx.Options.Realloc.Call(ctx.Ctx, 0, 0, uint64(elemSize), totalSize)
	if err != nil {
		return 0, 0, err
	}

	ptr = uint32(results[0])
	for i, elem := range elems {
		elemPtr := ptr + uint32(i)*elemSize
		if !ctx.Options.Memory.Write(elemPtr, elem) {
			return 0, 0, fmt.Errorf("%w: element %d at ptr=%d", ErrMemoryWrite, i, elemPtr)
		}
	}

	return ptr, length, nil
}

// LowerOwn creates an owned handle from a representation value.
// LowerOwn is for passing resource ownership from host to guest.
func LowerOwn(ctx *LowerContext, typeID uint32, rep uint32) Handle {
	table := ctx.Store.Table(typeID)
	return table.New(rep)
}

// LowerBorrow creates a borrowed handle.
// LowerBorrow requires the caller to call EndLowerBorrow when the borrow ends.
func LowerBorrow(ctx *LowerContext, typeID uint32, handle Handle) (Handle, error) {
	table := ctx.Store.Table(typeID)
	if err := table.Borrow(handle); err != nil {
		return 0, err
	}
	return handle, nil
}

// EndLowerBorrow ends a borrow that was started with LowerBorrow.
// EndLowerBorrow must be called when the borrowed resource is no longer needed.
func EndLowerBorrow(ctx *LowerContext, typeID uint32, handle Handle) error {
	table := ctx.Store.Table(typeID)
	return table.EndBorrow(handle)
}

// ResourceNew implements canon resource.new - creates a new resource handle.
// ResourceNew creates owned handle from representation value per Component Model spec.
func ResourceNew(store *ResourceStore, typeID uint32, rep uint32) Handle {
	table := store.Table(typeID)
	return table.New(rep)
}

// ResourceDrop implements canon resource.drop - destroys a resource.
// ResourceDrop drops owned handle and calls destructor if needed per Component Model spec.
func ResourceDrop(store *ResourceStore, typeID uint32, handle Handle) error {
	table := store.Table(typeID)
	rep, needsDtor, err := table.Drop(handle)
	if err != nil {
		return fmt.Errorf("resource.drop: %w", err)
	}
	if needsDtor {
		table.RunDestructor(rep)
	}
	return nil
}

// ResourceRep implements canon resource.rep - returns representation from handle.
// ResourceRep returns rep value per Component Model spec; handle must be valid.
func ResourceRep(store *ResourceStore, typeID uint32, handle Handle) (uint32, error) {
	table := store.Table(typeID)
	rep, ok := table.Rep(handle)
	if !ok {
		return 0, fmt.Errorf("canon: invalid handle %d for type %d", handle, typeID)
	}
	return rep, nil
}
