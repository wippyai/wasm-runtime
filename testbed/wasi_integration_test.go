package testbed

import (
	"context"
	"os"
	"testing"

	"github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var calcWasm []byte

func init() {
	data, err := os.ReadFile("calculator.wasm")
	if err == nil {
		calcWasm = data
	}
}

// CalculatorHost implements wisma:calculator/host@0.1.0
type CalculatorHost struct {
	logs []string
}

func (h *CalculatorHost) Namespace() string {
	return "wisma:calculator/host@0.1.0"
}

func (h *CalculatorHost) Log(ctx context.Context, msg string) {
	h.logs = append(h.logs, msg)
}

func (h *CalculatorHost) Compute(ctx context.Context, a, b uint32) uint32 {
	return a * b
}

func TestWASI_Calculator(t *testing.T) {
	if calcWasm == nil {
		t.Skip("calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	// Register WASI with test configuration
	wasi := preview2.New().
		WithEnv(map[string]string{
			"TEST_VAR": "test_value",
		}).
		WithArgs([]string{"test.wasm"}).
		WithCwd("/")

	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI: %v", err)
	}

	// Register custom calculator host
	host := &CalculatorHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, calcWasm)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call process function
	result, err := inst.Call(ctx, "process", uint32(6), uint32(7))
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	if result != uint32(42) {
		t.Errorf("expected 42, got %v", result)
	}

	t.Logf("process(6, 7) = %v", result)
}

func TestWASI_CalculatorWithLog(t *testing.T) {
	if calcWasm == nil {
		t.Skip("calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	wasi := preview2.New()
	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI: %v", err)
	}

	host := &CalculatorHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, calcWasm)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call process-with-log function
	result, err := inst.Call(ctx, "process-with-log", uint32(3), uint32(4), "test message")
	if err != nil {
		t.Fatalf("process-with-log: %v", err)
	}

	if result != uint32(12) {
		t.Errorf("expected 12, got %v", result)
	}

	// Verify log was called
	if len(host.logs) == 0 {
		t.Error("expected log to be called")
	} else {
		t.Logf("Captured logs: %v", host.logs)
	}
}

func TestWASI_MultipleInstances(t *testing.T) {
	if calcWasm == nil {
		t.Skip("calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	wasi := preview2.New()
	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI: %v", err)
	}

	host := &CalculatorHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, calcWasm)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Create multiple instances
	for i := 0; i < 5; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("instantiate %d: %v", i, err)
		}

		result, err := inst.Call(ctx, "process", uint32(i+1), uint32(10))
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}

		expected := uint32((i + 1) * 10)
		if result != expected {
			t.Errorf("instance %d: expected %d, got %v", i, expected, result)
		}

		inst.Close(ctx)
	}
}

func TestWASI_EnvironmentConfig(t *testing.T) {
	if calcWasm == nil {
		t.Skip("calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	// Configure WASI with environment
	wasi := preview2.New().
		WithEnv(map[string]string{
			"APP_NAME":    "test-app",
			"APP_VERSION": "1.0.0",
			"DEBUG":       "true",
		}).
		WithArgs([]string{"test.wasm", "--flag", "value"}).
		WithCwd("/workspace")

	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI: %v", err)
	}

	host := &CalculatorHost{}
	if err := rt.RegisterHost(host); err != nil {
		t.Fatalf("register host: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, calcWasm)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Just verify it instantiates correctly with env config
	result, err := inst.Call(ctx, "process", uint32(5), uint32(5))
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	if result != uint32(25) {
		t.Errorf("expected 25, got %v", result)
	}

	t.Log("Component instantiated successfully with environment configuration")
}

func BenchmarkWASI_Process(b *testing.B) {
	if calcWasm == nil {
		b.Skip("calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	wasi := preview2.New()
	rt.RegisterWASI(wasi)

	host := &CalculatorHost{}
	rt.RegisterHost(host)

	mod, err := rt.LoadComponent(ctx, calcWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := inst.Call(ctx, "process", uint32(6), uint32(7))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWASI_InstantiateAndCall(b *testing.B) {
	if calcWasm == nil {
		b.Skip("calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	wasi := preview2.New()
	rt.RegisterWASI(wasi)

	host := &CalculatorHost{}
	rt.RegisterHost(host)

	mod, err := rt.LoadComponent(ctx, calcWasm)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			b.Fatal(err)
		}

		_, err = inst.Call(ctx, "process", uint32(6), uint32(7))
		if err != nil {
			b.Fatal(err)
		}

		inst.Close(ctx)
	}
}
