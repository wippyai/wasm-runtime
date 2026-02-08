// Package engine provides the low-level WebAssembly Component Model runtime.
//
// This package wraps wazero to provide Component Model semantics, including
// canonical ABI type lifting/lowering, asyncify support for async operations,
// and WASI preview1-to-preview2 adaptation.
//
// # Architecture
//
// The engine package provides three main types:
//
//	WazeroEngine  - Creates and manages wazero runtime instances
//	WazeroModule  - Represents a compiled component, can create instances
//	WazeroInstance - A running component instance with exports
//
// # Component Instantiation Flow
//
//  1. WazeroEngine.LoadModule() parses and validates the component binary
//  2. WazeroModule holds the validated component and linker configuration
//  3. WazeroModule.Instantiate() creates a WazeroInstance via the linker
//  4. WazeroInstance provides Call methods for invoking exports
//
// # Canonical ABI
//
// The canonical ABI defines how WIT types map to WASM core types:
//
//	WIT Type        Core Representation    Flat Count
//	─────────────────────────────────────────────────
//	bool, u8-u32    i32                    1
//	u64, s64        i64                    1
//	f32             f32                    1
//	f64             f64                    1
//	string          (ptr, len) as i32×2    2
//	list<T>         (ptr, len) as i32×2    2
//	record          flattened fields       sum of fields
//	variant         (disc, payload)        1 + max(cases)
//	option<T>       variant with none/some varies
//	result<T,E>     variant with ok/err    varies
//
// When flat count exceeds MaxFlatParams (16) or MaxFlatResults (1), values
// are passed via linear memory using a return pointer (retptr).
//
// # Asyncify Support
//
// Asyncify enables cooperative multitasking in WASM. Modules compiled with
// wasm-opt --asyncify can suspend execution (unwind) and resume later (rewind).
//
// Usage:
//
//	if err := inst.EnableAsyncify(config); err != nil {
//	    log.Fatal(err)
//	}
//
//	// In host function that needs to block:
//	asyncify := inst.Asyncify()
//	if asyncify.State() == StateNormal {
//	    asyncify.StartUnwind(ctx) // Save stack, return to caller
//	    return                     // Guest sees function return
//	}
//	// On rewind, execution continues here
//	asyncify.StopRewind(ctx)
//	// Perform actual work, return result
//
// # WASI Adaptation
//
// The engine provides automatic WASI preview1-to-preview2 adaptation for
// components that import preview1 interfaces. This allows legacy modules
// to run with modern WASI preview2 host implementations.
//
// # Resource Table
//
// Resources (handles) are managed via ResourceTable, which maps integer
// handles to Go values. The table supports borrow semantics for the
// Component Model's resource lifecycle.
//
// # Thread Safety
//
// WazeroEngine and WazeroModule are safe for concurrent use.
// WazeroInstance is NOT thread-safe and should be used by a single goroutine.
//
// # Experimental Features
//
// Threads/Atomics: Enable via EngineConfig.EnableThreads. This enables the
// WebAssembly threads proposal (shared memory, atomic operations). Note that
// atomic operations are guest-only and not exposed to host functions.
//
// # Known Limitations
//
// Memory64: The WebAssembly Memory64 proposal (64-bit memory addressing) is
// not supported. This limitation comes from the underlying wazero runtime
// (v1.10.1) which does not implement Memory64. Implementation would require:
//   - wazero upstream Memory64 support
//   - Updates to memory access code for uint64 addresses
//   - Canonical ABI changes (pointer types become i64)
//
// Most users should use the runtime package for a simpler API.
// This package is for advanced use cases requiring direct control.
package engine
