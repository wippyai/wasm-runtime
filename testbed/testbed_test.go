package testbed

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/wippyai/wasm-runtime/runtime"

	"go.bytecodealliance.org/wit"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

// MinimalHost implements host functions for minimal.wasm
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

func TestMinimal_Compute(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		t.Skipf("minimal.wasm not found: %v", err)
	}

	rt, err := runtime.New(ctx)
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

func TestMinimal_ComputeUsingHost(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		t.Skipf("minimal.wasm not found: %v", err)
	}

	rt, err := runtime.New(ctx)
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

	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.adds) == 0 {
		t.Error("host.add was not called")
	} else if host.adds[0].a != 7 || host.adds[0].b != 8 {
		t.Errorf("host.add called with (%d, %d), want (7, 8)", host.adds[0].a, host.adds[0].b)
	}
}

func TestMinimal_MultipleValues(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		t.Skipf("minimal.wasm not found: %v", err)
	}

	rt, err := runtime.New(ctx)
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

	tests := []struct {
		a, b     uint32
		expected uint32
	}{
		{0, 0, 0},
		{1, 1, 1},
		{10, 20, 200},
		{100, 100, 10000},
		{0xFFFF, 1, 0xFFFF},
	}

	for _, tc := range tests {
		result, err := inst.CallWithTypes(ctx, "compute",
			[]wit.Type{wit.U32{}, wit.U32{}},
			[]wit.Type{wit.U32{}},
			tc.a, tc.b)
		if err != nil {
			t.Errorf("compute(%d, %d): %v", tc.a, tc.b, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("compute(%d, %d) = %v, want %d", tc.a, tc.b, result, tc.expected)
		}
	}
}

func TestMinimal_ConcurrentInstances(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		t.Skipf("minimal.wasm not found: %v", err)
	}

	rt, err := runtime.New(ctx)
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

	const numGoroutines = 5
	const callsPerGoroutine = 20

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
		if err != nil {
			t.Errorf("concurrent error: %v", err)
		}
	}
}

func TestInvalidWasm(t *testing.T) {
	ctx := context.Background()

	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	_, err = rt.LoadComponent(ctx, []byte{0x00, 0x61, 0x73, 0x6d})
	if err == nil {
		t.Error("expected error loading invalid wasm, got nil")
	}

	_, err = rt.LoadComponent(ctx, []byte{})
	if err == nil {
		t.Error("expected error loading empty wasm, got nil")
	}
}

func TestMinimal_FunctionRegistration(t *testing.T) {
	ctx := context.Background()

	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	err = rt.RegisterFunc("test:minimal/host@0.1.0", "add",
		func(ctx context.Context, a, b uint32) uint32 {
			return a + b
		})
	if err != nil {
		t.Fatalf("register func: %v", err)
	}

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		t.Skipf("minimal.wasm not found: %v", err)
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

func TestMinimal_ContextPropagation(t *testing.T) {
	ctx := context.Background()

	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

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

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		t.Skipf("minimal.wasm not found: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	tenantCtx := context.WithValue(ctx, contextKey("tenant"), "acme-corp")
	inst, err := mod.Instantiate(tenantCtx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(tenantCtx)

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

// Benchmarks

func BenchmarkMinimal_Compute(b *testing.B) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		b.Skipf("minimal.wasm not found: %v", err)
	}

	rt, err := runtime.New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	host := &MinimalHost{}
	rt.RegisterHost(host)

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.U32{}, wit.U32{}}
	results := []wit.Type{wit.U32{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "compute", params, results, uint32(5), uint32(3))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMinimal_HostCallback(b *testing.B) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		b.Skipf("minimal.wasm not found: %v", err)
	}

	rt, err := runtime.New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	host := &MinimalHost{}
	rt.RegisterHost(host)

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.U32{}, wit.U32{}}
	results := []wit.Type{wit.U32{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "compute-using-host", params, results, uint32(5), uint32(3))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMinimal_Instantiate(b *testing.B) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("minimal.wasm")
	if err != nil {
		b.Skipf("minimal.wasm not found: %v", err)
	}

	rt, err := runtime.New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	host := &MinimalHost{}
	rt.RegisterHost(host)

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			b.Fatal(err)
		}
		inst.Close(ctx)
	}
}
