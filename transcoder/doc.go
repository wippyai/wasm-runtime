// Package transcoder provides Canonical ABI encoding and decoding.
//
// This package handles bidirectional conversion between Go types and the
// Component Model's Canonical ABI representation in WASM linear memory.
//
// # Canonical ABI Overview
//
// The Canonical ABI defines a binary protocol for representing WIT types
// in WASM linear memory and core value types:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ Go Value ←→ [Transcoder] ←→ WASM Memory / Core Values      │
//	└─────────────────────────────────────────────────────────────┘
//
// # Memory Layout
//
// Compound types are laid out in memory with specific alignment:
//
//	Type            Size    Alignment
//	──────────────────────────────────
//	bool            1       1
//	u8/s8           1       1
//	u16/s16         2       2
//	u32/s32/f32     4       4
//	u64/s64/f64     8       8
//	char            4       4
//	string          8       4 (ptr + len)
//	list<T>         8       4 (ptr + len)
//	record          sum     max field align
//	variant         varies  max case align
//	option<T>       1+size  max(1, T align)
//	flags           1/2/4   1/2/4 (per bit count)
//
// # Key Types
//
//	Encoder       - Writes Go values to WASM memory
//	Decoder       - Reads WASM memory into Go values
//	Compiler      - Pre-compiles type layouts
//	CompiledType  - Optimized type representation
//
// # Encoding Flow
//
//  1. Compiler.Compile(witType, goType) → CompiledType
//  2. Encoder.EncodeToMemory(value, addr, mem, alloc)
//     or Encoder.FlattenToStack(value) → []uint64
//
// # Decoding Flow
//
//  1. Compiler.Compile(witType, goType) → CompiledType
//  2. Decoder.DecodeFromMemory(addr, mem) → value
//     or Decoder.LiftFromStack(flat) → value
//
// # Flattening vs Memory
//
// Small types are "flattened" to core value types (i32/i64/f32/f64):
//
//	func add(a: u32, b: u32) -> u32
//	Core: (i32, i32) -> i32  [flattened]
//
// Large types use memory with a return pointer (retptr):
//
//	func get-data() -> list<u8>
//	Core: (retptr: i32) -> void  [via memory]
//
// The threshold is MaxFlatParams (16) and MaxFlatResults (1).
//
// # Type Compilation
//
// The Compiler pre-computes:
//
//   - Memory size and alignment
//   - Field offsets for records
//   - Discriminant size for variants
//   - Flattened type sequence
//   - Go reflection metadata
//
// Pre-compilation avoids per-call reflection lookups.
//
// # Allocation
//
// Encoding compound types requires memory allocation via the guest's
// allocator (typically cabi_realloc). The transcoder accepts an Allocator
// interface that wraps this function.
//
// # String Handling
//
// Strings can be processed in two modes:
//
//   - Copy mode: String data is copied to/from Go heap
//   - Zero-copy mode: Strings point directly into WASM memory
//
// Zero-copy mode is faster but strings are only valid while the
// WASM instance is alive and memory hasn't been modified.
//
// # Thread Safety
//
// Compiler and CompiledType are safe for concurrent use.
// Encoder and Decoder maintain internal state and are NOT thread-safe.
// Use separate instances per goroutine.
//
// # Error Handling
//
// Errors use the structured types from the errors package:
//
//	[encode] type_mismatch at user.address.zip: Go type int, WIT type string
//	[decode] out_of_bounds at items[5]: index 5 out of bounds (length 3)
//
// # Usage Tips
//
//   - Reuse Encoder/Decoder instances across calls
//   - Use pre-compiled types for repeated operations
//   - Zero-copy mode: strings are invalid after memory mutation or instance close
//   - Batch allocations when encoding multiple values
package transcoder
