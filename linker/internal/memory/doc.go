// Package memory provides memory access adapters for wazero.
//
// This package bridges wazero's memory API with the transcoder's Memory and
// Allocator interfaces, enabling canonical ABI encoding/decoding operations
// on WebAssembly linear memory.
//
// # Memory Wrapper
//
// Wraps wazero api.Memory for transcoder compatibility:
//
//	mem := memory.WrapMemory(instance.Memory())
//	// mem implements transcoder.Memory
//
// # Allocator Wrapper
//
// Wraps allocation functions for string/list encoding:
//
//	alloc := memory.WrapAllocator(ctx, allocFunc)
//	// alloc implements transcoder.Allocator
//
// This package is internal to the linker and should not be used directly.
package memory
