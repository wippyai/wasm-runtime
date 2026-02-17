package runtime

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"go.bytecodealliance.org/wit"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

// MinimalHost implements the host functions for minimal.wasm
type MinimalHost struct {
	adds []struct{ a, b, result uint32 }
	mu   sync.Mutex
}

func (h *MinimalHost) Namespace() string {
	return "test:minimal/host@0.1.0"
}

func (h *MinimalHost) Add(ctx context.Context, a, b uint32) uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := a + b
	h.adds = append(h.adds, struct{ a, b, result uint32 }{a, b, result})
	return result
}

// NOTE: This test duplicates testbed/testbed_test.go:TestMinimal_Compute.
// Consider consolidating to testbed package for E2E tests.
func TestComponent_Minimal_Compute(t *testing.T) {
	ctx := context.Background()

	// Load minimal.wasm
	wasmBytes, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	// Create runtime
	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Register host
	host := &MinimalHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	// Load component
	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	// Create instance
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call compute(5, 3) - should return 5 * 3 = 15
	result, err := inst.CallWithTypes(ctx, "compute",
		[]wit.Type{wit.U32{}, wit.U32{}},
		[]wit.Type{wit.U32{}},
		uint32(5), uint32(3))
	if err != nil {
		t.Fatalf("call compute: %v", err)
	}

	if result != uint32(15) {
		t.Errorf("compute(5, 3) = %v, want 15", result)
	}
}

func TestInstance_MemorySize(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	host := &MinimalHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	size := inst.MemorySize()
	if size == 0 {
		t.Error("expected non-zero memory size for component with memory")
	}
	if size%65536 != 0 {
		t.Errorf("memory size %d should be a multiple of page size (65536)", size)
	}
}

// NOTE: This test duplicates testbed/testbed_test.go:TestMinimal_ComputeUsingHost.
// Consider consolidating to testbed package for E2E tests.
func TestComponent_Minimal_ComputeUsingHost(t *testing.T) {
	ctx := context.Background()

	// Load minimal.wasm
	wasmBytes, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	// Create runtime
	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Register host
	host := &MinimalHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	// Load component
	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	// Create instance
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call compute-using-host(7, 8) - calls host.add and returns 7 + 8 = 15
	result, err := inst.CallWithTypes(ctx, "compute-using-host",
		[]wit.Type{wit.U32{}, wit.U32{}},
		[]wit.Type{wit.U32{}},
		uint32(7), uint32(8))
	if err != nil {
		t.Fatalf("call compute-using-host: %v", err)
	}

	if result != uint32(15) {
		t.Errorf("compute-using-host(7, 8) = %v, want 15", result)
	}

	// Verify host function was called
	host.mu.Lock()
	defer host.mu.Unlock()

	if len(host.adds) == 0 {
		t.Error("host.add was not called")
	} else if host.adds[0].a != 7 || host.adds[0].b != 8 {
		t.Errorf("host.add called with (%d, %d), want (7, 8)", host.adds[0].a, host.adds[0].b)
	}
}

func TestRegisterFunc(t *testing.T) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Test function-based registration
	err = rt.RegisterFunc("test:minimal/host@0.1.0", "add",
		func(ctx context.Context, a, b uint32) uint32 {
			return a + b
		})
	if err != nil {
		t.Fatalf("register func: %v", err)
	}

	// Load and test
	wasmBytes, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	result, err := inst.CallWithTypes(ctx, "compute",
		[]wit.Type{wit.U32{}, wit.U32{}},
		[]wit.Type{wit.U32{}},
		uint32(2), uint32(3))
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	if result != uint32(6) {
		t.Errorf("compute(2, 3) = %v, want 6", result)
	}
}

// CalculatorHost implements host functions for calculator.wasm
type CalculatorHost struct {
	logs     []string
	computes []struct{ a, b, result uint32 }
	mu       sync.Mutex
}

func (h *CalculatorHost) Namespace() string {
	return "wisma:calculator/host@0.1.0"
}

func (h *CalculatorHost) Log(ctx context.Context, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, msg)
}

func (h *CalculatorHost) Compute(ctx context.Context, a, b uint32) uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := a + b
	h.computes = append(h.computes, struct{ a, b, result uint32 }{a, b, result})
	return result
}

func TestComponent_Calculator_Process(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("../testbed/calculator.wasm")
	if err != nil {
		t.Skipf("calculator.wasm not found: %v", err)
	}

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Register WASI (required by calculator.wasm)
	wasi := preview2.New()
	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI: %v", err)
	}

	host := &CalculatorHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call process(5, 3) - should return 5 * 3 = 15
	result, err := inst.CallWithTypes(ctx, "process",
		[]wit.Type{wit.U32{}, wit.U32{}},
		[]wit.Type{wit.U32{}},
		uint32(5), uint32(3))
	if err != nil {
		t.Fatalf("call process: %v", err)
	}

	if result != uint32(15) {
		t.Errorf("process(5, 3) = %v, want 15", result)
	}
}

// StringsHost implements the host functions for strings.wasm
type StringsHost struct {
	logs []string
	mu   sync.Mutex
}

func (h *StringsHost) Namespace() string {
	return "test:strings/host@0.1.0"
}

func (h *StringsHost) Log(ctx context.Context, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, msg)
}

func (h *StringsHost) Concat(ctx context.Context, a, b string) string {
	return a + b
}

func TestComponent_Strings_Echo(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("../testbed/strings.wasm")
	if err != nil {
		t.Skipf("strings.wasm not found: %v", err)
	}

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	host := &StringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Test echo - simple round-trip
	result, err := inst.CallWithTypes(ctx, "echo",
		[]wit.Type{wit.String{}},
		[]wit.Type{wit.String{}},
		"hello world")
	if err != nil {
		t.Fatalf("call echo: %v", err)
	}

	if result != "hello world" {
		t.Errorf("echo(\"hello world\") = %q, want %q", result, "hello world")
	}
}

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Register host that captures context
	var capturedTenant string
	err = rt.RegisterFunc("test:minimal/host@0.1.0", "add",
		func(ctx context.Context, a, b uint32) uint32 {
			if v := ctx.Value(contextKey("tenant")); v != nil {
				capturedTenant = v.(string)
			}
			return a + b
		})
	if err != nil {
		t.Fatalf("register func: %v", err)
	}

	wasmBytes, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	// Instantiate with tenant context
	tenantCtx := context.WithValue(ctx, contextKey("tenant"), "acme-corp")
	inst, err := mod.Instantiate(tenantCtx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(tenantCtx)

	// Call function that triggers host.add
	_, err = inst.CallWithTypes(tenantCtx, "compute-using-host",
		[]wit.Type{wit.U32{}, wit.U32{}},
		[]wit.Type{wit.U32{}},
		uint32(1), uint32(2))
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	if capturedTenant != "acme-corp" {
		t.Errorf("captured tenant = %q, want %q", capturedTenant, "acme-corp")
	}
}

// Edge case tests

func TestComponent_Strings_EmptyString(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("../testbed/strings.wasm")
	if err != nil {
		t.Skipf("strings.wasm not found: %v", err)
	}

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	host := &StringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Test empty string echo
	result, err := inst.CallWithTypes(ctx, "echo",
		[]wit.Type{wit.String{}},
		[]wit.Type{wit.String{}},
		"")
	if err != nil {
		t.Fatalf("call echo with empty string: %v", err)
	}

	if result != "" {
		t.Errorf("echo(\"\") = %q, want empty string", result)
	}
}

func TestComponent_Strings_LargeString(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("../testbed/strings.wasm")
	if err != nil {
		t.Skipf("strings.wasm not found: %v", err)
	}

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	host := &StringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Test with a 64KB string
	largeString := make([]byte, 64*1024)
	for i := range largeString {
		largeString[i] = byte('a' + (i % 26))
	}

	result, err := inst.CallWithTypes(ctx, "echo",
		[]wit.Type{wit.String{}},
		[]wit.Type{wit.String{}},
		string(largeString))
	if err != nil {
		t.Fatalf("call echo with large string: %v", err)
	}

	if result != string(largeString) {
		t.Errorf("large string echo failed, got length %d, want %d", len(result.(string)), len(largeString))
	}
}

func TestInvalidComponent(t *testing.T) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Try to load invalid wasm bytes
	_, err = rt.LoadComponent(ctx, []byte{0x00, 0x61, 0x73, 0x6d}) // Invalid magic
	if err == nil {
		t.Error("expected error loading invalid wasm, got nil")
	}

	// Try to load empty bytes
	_, err = rt.LoadComponent(ctx, []byte{})
	if err == nil {
		t.Error("expected error loading empty wasm, got nil")
	}
}

func TestConcurrentInstances(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	host := &MinimalHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	const numGoroutines = 10
	const callsPerGoroutine = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			inst, err := mod.Instantiate(ctx)
			if err != nil {
				errors <- err
				return
			}
			defer inst.Close(ctx)

			for i := 0; i < callsPerGoroutine; i++ {
				a := uint32(goroutineID)
				b := uint32(i)
				result, err := inst.CallWithTypes(ctx, "compute",
					[]wit.Type{wit.U32{}, wit.U32{}},
					[]wit.Type{wit.U32{}},
					a, b)
				if err != nil {
					errors <- err
					return
				}

				expected := a * b
				if result != expected {
					errors <- err
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent error: %v", err)
	}
}

// TestWAT_DataSection verifies WAT parser handles data sections
func TestWAT_DataSection(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		(memory (export "memory") 1)
		(data (i32.const 100) "test data")
		
		(func (export "identity") (param i32) (result i32)
			local.get 0
		)
	)`

	witTypes := `identity: func(x: u32) -> u32`

	mod, err := rt.LoadWAT(ctx, watSource, witTypes)
	if err != nil {
		t.Fatalf("LoadWAT failed: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate failed: %v", err)
	}
	defer inst.Close(ctx)

	result, err := inst.CallWithTypes(ctx, "identity",
		[]wit.Type{wit.U32{}},
		[]wit.Type{wit.U32{}},
		uint32(42))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result != uint32(42) {
		t.Errorf("expected 42, got %v", result)
	}
}

// TestWAT_WASI_Import verifies WAT parser handles WASI imports
func TestWAT_WASI_Import(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	watSource := `(module
		(import "wasi_snapshot_preview1" "fd_write" 
			(func $fd_write (param i32 i32 i32 i32) (result i32)))
		
		(memory (export "memory") 1)
		(data (i32.const 0) "test")
		
		(func (export "_start")
			(call $fd_write
				(i32.const 1)
				(i32.const 100)
				(i32.const 1)
				(i32.const 108))
			drop
		)
	)`

	mod, err := rt.LoadWAT(ctx, watSource, "")
	if err != nil {
		t.Fatalf("LoadWAT failed: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate failed: %v", err)
	}
	defer inst.Close(ctx)

	_, err = inst.CallWithTypes(ctx, "_start", nil, nil)
	if err != nil {
		t.Fatalf("Call _start failed: %v", err)
	}
}
