// Package asyncify provides pure-Go WebAssembly stack switching transformation.
//
// Reference implementation: Binaryen asyncify pass
// https://github.com/WebAssembly/binaryen/blob/main/src/passes/Asyncify.cpp
//
// # Overview
//
// Asyncify enables pausing and resuming WebAssembly execution, which is required
// for implementing async/await patterns in synchronous WASM code. This allows
// host functions to suspend execution, perform async operations (I/O, timers,
// network), and resume where they left off.
//
// The transformation modifies WASM bytecode to save and restore the call stack
// to linear memory, enabling cooperative multitasking without threads.
//
// # How It Works
//
// The asyncify technique instruments WASM functions to:
//
//  1. Check a global state variable at each async call site
//  2. Save local variables and call index to a stack in linear memory during "unwind"
//  3. Restore local variables and jump to saved call site during "rewind"
//
// State machine:
//
//	Normal (0) --[start_unwind]--> Unwinding (1) --[stop_unwind]--> Normal (0)
//	Normal (0) --[start_rewind]--> Rewinding (2) --[stop_rewind]--> Normal (0)
//
// # Usage
//
// Basic usage with exact patterns:
//
//	matcher := asyncify.NewExactMatcher([]string{"env.sleep", "env.read"})
//	result, err := asyncify.Transform(wasmBytes, asyncify.Config{
//	    Matcher: matcher,
//	})
//
// WIT-style matching with version flexibility:
//
//	matcher := asyncify.NewWITMatcher([]string{
//	    "wasi:io/poll#block",        // any version
//	    "wasi:http/types@0.2.*",     // version prefix
//	})
//	result, err := asyncify.Transform(wasmBytes, asyncify.Config{
//	    Matcher: matcher,
//	})
//
// # Exported Functions
//
// The transform adds these exports to the module:
//
//	asyncify_get_state() -> i32      // Get current state (0/1/2)
//	asyncify_start_unwind(data: i32) // Begin unwinding with data pointer
//	asyncify_stop_unwind()           // Complete unwind, return to normal
//	asyncify_start_rewind(data: i32) // Begin rewinding with data pointer
//	asyncify_stop_rewind()           // Complete rewind, return to normal
//
// # Data Layout
//
// The data pointer passed to start_unwind/start_rewind points to:
//
//	offset 0: stack_ptr (i32) - current position in the stack
//	offset 4: stack_end (i32) - end of stack (for overflow detection)
//	offset 8+: stack data    - saved locals and call indices
//
// The host must allocate this structure and initialize stack_ptr and stack_end.
//
// # Compatibility
//
// This implementation produces output compatible with Binaryen's wasm-opt --asyncify.
// It supports:
//
//   - All WebAssembly MVP instructions and value types (i32, i64, f32, f64)
//   - SIMD v128 type (16-byte vectors)
//   - Control flow: async calls inside if/block/loop structures
//   - WIT Canonical ABI types (all lower to primitives)
//
// Reference types (funcref, externref) cannot be saved to linear memory and will
// cause a validation error if used as locals in async functions.
package asyncify
