// Package runtime provides the high-level API for the WebAssembly Component Model.
//
// # Quick Start
//
//	ctx := context.Background()
//	rt, err := runtime.New(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer rt.Close(ctx)
//
//	// Load a component
//	mod, err := rt.LoadComponent(ctx, wasmBytes)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create an instance
//	inst, err := mod.Instantiate(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer inst.Close(ctx)
//
//	// Call exported functions
//	result, err := inst.Call(ctx, "greet", "World")
//	fmt.Println(result) // "Hello, World!"
//
// # Loading Components
//
// The runtime supports three loading modes:
//
//	LoadComponent(bytes)  - Load a Component Model binary
//	LoadWASM(bytes)       - Load a core WASM module
//	LoadWAT(bytes)        - Load WAT text format (compiles to WASM)
//
// For core WASM modules, you can provide WIT text to enable typed calls:
//
//	mod, err := rt.LoadWASM(ctx, wasmBytes, witText)
//
// # Host Functions
//
// Register Go functions as WASI/custom host implementations:
//
//	// Register a typed function
//	rt.RegisterFunc("my:package/api@1.0.0", "greet",
//	    func(ctx context.Context, name string) string {
//	        return "Hello, " + name
//	    })
//
//	// Or implement the Host interface for a full namespace
//	rt.RegisterHost(myWASIImplementation)
//
// # WASI Support
//
// WASI preview2 host implementations are available:
//
//	import (
//	    "github.com/wippyai/wasm-runtime/wasi/preview2/clocks"
//	    "github.com/wippyai/wasm-runtime/wasi/preview2/random"
//	)
//
//	rt.RegisterHost(clocks.NewWallClockHost())
//	rt.RegisterHost(clocks.NewMonotonicClockHost(resources))
//	rt.RegisterHost(random.NewSecureRandomHost())
//
// # Type Mapping
//
// Go types are automatically mapped to WIT types:
//
//	Go Type          WIT Type
//	───────────────────────────
//	bool             bool
//	int8/uint8       s8/u8
//	int16/uint16     s16/u16
//	int32/uint32     s32/u32
//	int64/uint64     s64/u64
//	float32          f32
//	float64          f64
//	string           string
//	[]T              list<T>
//	*T               option<T>
//	struct{...}      record
//	map[string]T     record (field names from keys)
//
// # Asyncify Support
//
// For components compiled with asyncify, enable async operations:
//
//	if err := inst.EnableAsyncify(engine.AsyncifyConfig{
//	    StackSize: 1024,
//	    DataAddr:  65536,
//	}); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Now async host functions can suspend/resume the guest
//
// # Thread Safety
//
// Runtime and Module are safe for concurrent use. You can call
// Module.Instantiate() from multiple goroutines concurrently.
//
// Instance is NOT thread-safe. Each goroutine should have its own
// Instance, or access must be synchronized externally.
//
// # Memory
//
// WASM linear memory can only grow, never shrink. For long-running
// services, pool and recycle instances periodically.
//
// # Resource Management
//
// Always close instances when done. Closing releases WASM memory and
// bridge module references.
package runtime
