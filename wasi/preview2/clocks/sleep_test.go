package clocks_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"github.com/wippyai/wasm-runtime/wasi/preview2/clocks"
	"github.com/wippyai/wasm-runtime/wasi/preview2/io"
)

// TestSleepComponent tests async sleep via monotonic clock.
// TODO(clocks): Enable once component export resolution is fixed.
func TestSleepComponent(t *testing.T) {
	// This test requires component export resolution fixes.
	// Remove the skip when the feature is implemented.
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("requires RUN_INTEGRATION_TESTS=1 and component export resolution fix")
	}

	ctx := context.Background()

	data, err := os.ReadFile("../../../testbed/sleep-test.wasm")
	if err != nil {
		t.Skipf("sleep-test.wasm not found: %v", err)
	}

	eng, err := engine.NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine failed: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, data)
	if err != nil {
		t.Fatalf("LoadModule failed: %v", err)
	}

	resources := preview2.NewResourceTable()
	registry := runtime.NewHostRegistry()

	clockHost := clocks.NewMonotonicClockHost(resources)
	if err := registry.RegisterHost(clockHost); err != nil {
		t.Fatalf("RegisterHost clock failed: %v", err)
	}

	ioHost := io.NewHost(resources)
	if err := registry.RegisterHost(ioHost.Poll); err != nil {
		t.Fatalf("RegisterHost poll failed: %v", err)
	}

	if err := registry.Bind(mod); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate failed: %v", err)
	}
	defer inst.Close(ctx)

	t.Log("Export names:", mod.ExportNames())

	sleepMs := inst.GetExportedFunction("test-sleep#sleep-ms")
	if sleepMs == nil {
		t.Logf("sleep-ms not found, trying alternative names...")
		for _, name := range mod.ExportNames() {
			t.Logf("  export: %s", name)
		}
		t.Fatal("GetExportedFunction sleep-ms returned nil")
	}

	start := time.Now()
	results, err := sleepMs.Call(ctx, 50) // 50ms sleep
	if err != nil {
		t.Fatalf("sleep-ms call failed: %v", err)
	}
	elapsed := time.Since(start)

	if len(results) > 0 {
		t.Logf("sleep-ms returned: %v (reported elapsed: %dns)", results, results[0])
	}

	if elapsed < 40*time.Millisecond {
		t.Errorf("sleep was too short: %v (expected ~50ms)", elapsed)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("sleep was too long: %v (expected ~50ms)", elapsed)
	}

	t.Logf("Sleep test passed: actual elapsed=%v", elapsed)
}

func TestSleepWorkIterations(t *testing.T) {
	ctx := context.Background()

	data, err := os.ReadFile("../../../testbed/sleep-test.wasm")
	if err != nil {
		t.Skipf("sleep-test.wasm not found: %v", err)
	}

	eng, err := engine.NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine failed: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, data)
	if err != nil {
		t.Fatalf("LoadModule failed: %v", err)
	}

	resources := preview2.NewResourceTable()
	registry := runtime.NewHostRegistry()

	clockHost := clocks.NewMonotonicClockHost(resources)
	if err := registry.RegisterHost(clockHost); err != nil {
		t.Fatalf("RegisterHost clock failed: %v", err)
	}

	ioHost := io.NewHost(resources)
	if err := registry.RegisterHost(ioHost.Poll); err != nil {
		t.Fatalf("RegisterHost poll failed: %v", err)
	}

	if err := registry.Bind(mod); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate failed: %v", err)
	}
	defer inst.Close(ctx)

	workWithSleep := inst.GetExportedFunction("test-sleep#work-with-sleep")
	if workWithSleep == nil {
		t.Skip("work-with-sleep not found")
	}

	iterations := uint64(3)
	sleepPerIter := uint64(20) // 20ms per iteration

	start := time.Now()
	results, err := workWithSleep.Call(ctx, iterations, sleepPerIter)
	if err != nil {
		t.Fatalf("work-with-sleep call failed: %v", err)
	}
	elapsed := time.Since(start)

	expectedMin := time.Duration(iterations) * time.Duration(sleepPerIter) * time.Millisecond

	if len(results) > 0 {
		t.Logf("work-with-sleep returned: %dns (actual: %v)", results[0], elapsed)
	}

	if elapsed < expectedMin-10*time.Millisecond {
		t.Errorf("work was too short: %v (expected >= %v)", elapsed, expectedMin)
	}

	t.Logf("Work iterations test passed: actual elapsed=%v, expected >= %v", elapsed, expectedMin)
}
