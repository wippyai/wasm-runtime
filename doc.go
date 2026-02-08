// Package wasmruntime provides a Go implementation of the WebAssembly Component Model.
//
// This library enables running WebAssembly components in Go with full support for
// the Component Model specification, including WIT types, canonical ABI, and WASI
// preview2 host implementations.
//
// # Architecture Overview
//
// The library is organized into several packages with distinct responsibilities:
//
//	wasmruntime/         Root package with core Memory and Allocator interfaces
//	├── runtime/         High-level API for loading and running components
//	├── engine/          Low-level wazero integration and canonical ABI
//	├── linker/          Component instantiation and import resolution
//	├── component/       Component binary parsing and validation
//	├── transcoder/      Canonical ABI encoding/decoding between Go and WASM
//	├── wasm/            Core WASM binary manipulation primitives
//	├── wat/             WAT text format to WASM binary compiler
//	├── asyncify/        Pure Go asyncify transform for async operations
//	├── resource/        Resource handle table implementation
//	├── errors/          Structured error types for debugging
//	└── wasi/            WASI preview2 host implementations
//
// # Quick Start
//
// Load and run a component:
//
//	rt := runtime.New()
//	defer rt.Close(ctx)
//
//	mod, err := rt.LoadComponent(ctx, wasmBytes)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	inst, err := mod.Instantiate(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer inst.Close(ctx)
//
//	result, err := inst.Call(ctx, "greet", "World")
//	fmt.Println(result) // "Hello, World!"
//
// # Component Model Support
//
// The library supports the full WIT type system:
//
//   - Primitives: bool, u8-u64, s8-s64, f32, f64, char, string
//   - Compound: list<T>, option<T>, result<T, E>, tuple<...>
//   - Named: record, variant, enum, flags
//   - Resources: resource handles with lifecycle management
//
// # Host Functions
//
// Register Go functions as host implementations:
//
//	registry := runtime.NewHostRegistry()
//	registry.RegisterFunc("wasi:random/random@0.2.0", "get-random-u64",
//	    func(ctx context.Context) uint64 {
//	        return rand.Uint64()
//	    },
//	    nil, []api.ValueType{api.ValueTypeI64},
//	)
//
// # Thread Safety
//
// Runtime and Module are safe for concurrent use. Instance is NOT thread-safe
// and should be used by a single goroutine, or access must be synchronized.
//
// # Memory Model
//
// WASM linear memory can only grow, never shrink. This is a WebAssembly
// specification limitation. When guest applications free memory, it remains
// allocated but available for reuse within the WASM instance.
//
// For memory-intensive workloads, consider instance pooling where instances
// are periodically recycled to reclaim memory.
package wasmruntime
