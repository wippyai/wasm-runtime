// Package memory provides memory access adapters for wazero.
package memory

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/transcoder"
)

// WrapMemory wraps a wazero api.Memory to implement transcoder.Memory.
func WrapMemory(mem api.Memory) transcoder.Memory {
	if mem == nil {
		return nil
	}
	return &Wrapper{Mem: mem}
}

// WrapAllocator wraps a wazero api.Function to implement transcoder.Allocator.
func WrapAllocator(ctx context.Context, fn api.Function) transcoder.Allocator {
	if fn == nil {
		return nil
	}
	return &AllocatorWrapper{Ctx: ctx, Fn: fn}
}

// Wrapper adapts wazero api.Memory to transcoder.Memory interface.
type Wrapper struct {
	Mem api.Memory
}

// Read reads bytes from memory.
func (m *Wrapper) Read(offset uint32, length uint32) ([]byte, error) {
	data, ok := m.Mem.Read(offset, length)
	if !ok {
		return nil, fmt.Errorf("memory read out of bounds: offset=%d, length=%d", offset, length)
	}
	return data, nil
}

// Write writes bytes to memory.
func (m *Wrapper) Write(offset uint32, data []byte) error {
	if !m.Mem.Write(offset, data) {
		return fmt.Errorf("memory write out of bounds: offset=%d, length=%d", offset, len(data))
	}
	return nil
}

// ReadU8 reads an unsigned 8-bit value.
func (m *Wrapper) ReadU8(offset uint32) (uint8, error) {
	v, ok := m.Mem.ReadByte(offset)
	if !ok {
		return 0, fmt.Errorf("memory read out of bounds: offset=%d", offset)
	}
	return v, nil
}

// ReadU16 reads an unsigned 16-bit little-endian value.
func (m *Wrapper) ReadU16(offset uint32) (uint16, error) {
	v, ok := m.Mem.ReadUint16Le(offset)
	if !ok {
		return 0, fmt.Errorf("memory read out of bounds: offset=%d", offset)
	}
	return v, nil
}

// ReadU32 reads an unsigned 32-bit little-endian value.
func (m *Wrapper) ReadU32(offset uint32) (uint32, error) {
	v, ok := m.Mem.ReadUint32Le(offset)
	if !ok {
		return 0, fmt.Errorf("memory read out of bounds: offset=%d", offset)
	}
	return v, nil
}

// ReadU64 reads an unsigned 64-bit little-endian value.
func (m *Wrapper) ReadU64(offset uint32) (uint64, error) {
	v, ok := m.Mem.ReadUint64Le(offset)
	if !ok {
		return 0, fmt.Errorf("memory read out of bounds: offset=%d", offset)
	}
	return v, nil
}

// WriteU8 writes an unsigned 8-bit value.
func (m *Wrapper) WriteU8(offset uint32, value uint8) error {
	if !m.Mem.WriteByte(offset, value) {
		return fmt.Errorf("memory write out of bounds: offset=%d", offset)
	}
	return nil
}

// WriteU16 writes an unsigned 16-bit little-endian value.
func (m *Wrapper) WriteU16(offset uint32, value uint16) error {
	if !m.Mem.WriteUint16Le(offset, value) {
		return fmt.Errorf("memory write out of bounds: offset=%d", offset)
	}
	return nil
}

// WriteU32 writes an unsigned 32-bit little-endian value.
func (m *Wrapper) WriteU32(offset uint32, value uint32) error {
	if !m.Mem.WriteUint32Le(offset, value) {
		return fmt.Errorf("memory write out of bounds: offset=%d", offset)
	}
	return nil
}

// WriteU64 writes an unsigned 64-bit little-endian value.
func (m *Wrapper) WriteU64(offset uint32, value uint64) error {
	if !m.Mem.WriteUint64Le(offset, value) {
		return fmt.Errorf("memory write out of bounds: offset=%d", offset)
	}
	return nil
}

// AllocatorWrapper adapts wazero api.Function (cabi_realloc) to transcoder.Allocator.
type AllocatorWrapper struct {
	Ctx context.Context
	Fn  api.Function
}

// Alloc allocates memory using cabi_realloc.
func (a *AllocatorWrapper) Alloc(size, align uint32) (uint32, error) {
	results, err := a.Fn.Call(a.Ctx, 0, 0, uint64(align), uint64(size))
	if err != nil {
		return 0, fmt.Errorf("allocation failed: %w", err)
	}
	if len(results) == 0 {
		return 0, fmt.Errorf("allocation returned no result")
	}
	return uint32(results[0]), nil
}

// Free deallocates memory using cabi_realloc.
func (a *AllocatorWrapper) Free(ptr, size, align uint32) {
	_, _ = a.Fn.Call(a.Ctx, uint64(ptr), uint64(size), uint64(align), 0)
}
