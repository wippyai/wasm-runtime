// Example: Basic WebAssembly Component Model usage
//
// This example demonstrates loading and executing a WebAssembly component
// with host function callbacks.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/wippyai/wasm-runtime/runtime"
	"go.bytecodealliance.org/wit"
)

// Host implements host functions that the WASM component can call.
// The Namespace() method returns the WIT interface name.
// Method names are converted from PascalCase to kebab-case:
//   - Add -> "add"
//   - GetValue -> "get-value"
type Host struct{}

func (h *Host) Namespace() string {
	return "test:minimal/host@0.1.0"
}

func (h *Host) Add(ctx context.Context, a, b uint32) uint32 {
	fmt.Printf("Host.Add called: %d + %d\n", a, b)
	return a + b
}

func main() {
	ctx := context.Background()

	// Load the WASM component bytes
	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		log.Fatalf("Failed to read WASM file: %v", err)
	}

	// Create runtime
	rt, err := runtime.New(ctx)
	if err != nil {
		log.Fatalf("Failed to create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Register host functions
	host := &Host{}
	if err := rt.RegisterHost(host); err != nil {
		log.Fatalf("Failed to register host: %v", err)
	}

	// Load component
	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		log.Fatalf("Failed to load component: %v", err)
	}

	// Instantiate
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		log.Fatalf("Failed to instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call exported function with explicit types
	result, err := inst.CallWithTypes(ctx, "compute",
		[]wit.Type{wit.U32{}, wit.U32{}}, // param types
		[]wit.Type{wit.U32{}},            // result types
		uint32(5), uint32(3))
	if err != nil {
		log.Fatalf("Failed to call compute: %v", err)
	}

	fmt.Printf("compute(5, 3) = %v\n", result)

	// Call function that uses host callback
	result, err = inst.CallWithTypes(ctx, "compute-using-host",
		[]wit.Type{wit.U32{}, wit.U32{}},
		[]wit.Type{wit.U32{}},
		uint32(10), uint32(20))
	if err != nil {
		log.Fatalf("Failed to call compute-using-host: %v", err)
	}

	fmt.Printf("compute-using-host(10, 20) = %v\n", result)
}
