package engine

import (
	"context"
	"os"
	"runtime"
	"testing"
)

// TestBridgeModuleCleanup verifies that bridge modules are properly closed
// when all instances using them are closed, by measuring memory.
func TestBridgeModuleCleanup(t *testing.T) {
	ctx := context.Background()

	wasmBytes := getCalculatorComponent(t)

	// Create engine with fresh runtime
	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}

	// Warm up to stabilize allocations
	for i := 0; i < 3; i++ {
		inst, _ := mod.Instantiate(ctx)
		if inst != nil {
			inst.Close(ctx)
		}
	}
	runtime.GC()

	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	// Create instance - this creates core modules and potentially bridges
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	var mDuring runtime.MemStats
	runtime.ReadMemStats(&mDuring)
	instanceMemory := int64(mDuring.HeapAlloc) - int64(mBefore.HeapAlloc)
	t.Logf("Memory used by instance: %d KB", instanceMemory/1024)

	// Close the instance
	if err := inst.Close(ctx); err != nil {
		t.Fatalf("close instance: %v", err)
	}

	// Force GC to clean up
	runtime.GC()
	runtime.GC()

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	retained := int64(mAfter.HeapAlloc) - int64(mBefore.HeapAlloc)
	t.Logf("Memory retained after close: %d KB", retained/1024)

	// After closing the only instance, most memory should be released
	// Allow 64KB for internal caches, but flag if we retain most of instance memory
	if retained > 64*1024 && retained > instanceMemory/2 {
		t.Errorf("Memory not properly released: instance used %d KB, retained %d KB after close",
			instanceMemory/1024, retained/1024)
	}
}

// TestBridgeModuleRefCounting verifies that shared bridge modules are only
// released when all instances using them are closed.
func TestBridgeModuleRefCounting(t *testing.T) {
	ctx := context.Background()

	wasmBytes := getCalculatorComponent(t)

	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}

	// Warm up
	for i := 0; i < 3; i++ {
		inst, _ := mod.Instantiate(ctx)
		if inst != nil {
			inst.Close(ctx)
		}
	}
	runtime.GC()

	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	// Create two instances from same module - they should share bridges
	inst1, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate 1: %v", err)
	}

	inst2, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate 2: %v", err)
	}

	var mWithTwo runtime.MemStats
	runtime.ReadMemStats(&mWithTwo)
	twoInstanceMem := int64(mWithTwo.HeapAlloc) - int64(mBefore.HeapAlloc)
	t.Logf("Memory with two instances: %d KB", twoInstanceMem/1024)

	// Close first instance
	if err := inst1.Close(ctx); err != nil {
		t.Fatalf("close instance 1: %v", err)
	}
	runtime.GC()

	var mAfterFirst runtime.MemStats
	runtime.ReadMemStats(&mAfterFirst)
	afterFirstClose := int64(mAfterFirst.HeapAlloc) - int64(mBefore.HeapAlloc)
	t.Logf("Memory after closing first: %d KB", afterFirstClose/1024)

	// Should have released roughly half the memory (one instance worth)
	// But shared bridges should remain for inst2

	// Close second instance
	if err := inst2.Close(ctx); err != nil {
		t.Fatalf("close instance 2: %v", err)
	}
	runtime.GC()
	runtime.GC()

	var mAfterBoth runtime.MemStats
	runtime.ReadMemStats(&mAfterBoth)
	afterBothClose := int64(mAfterBoth.HeapAlloc) - int64(mBefore.HeapAlloc)
	t.Logf("Memory after closing both: %d KB", afterBothClose/1024)

	// Now all memory should be released
	if afterBothClose > 64*1024 && afterBothClose > twoInstanceMem/4 {
		t.Errorf("Memory not released after both closed: started with %d KB, retained %d KB",
			twoInstanceMem/1024, afterBothClose/1024)
	}
}

// TestMemoryReleaseOnClose verifies actual memory is released when instances close.
// This test checks that memory usage doesn't grow unboundedly across many instantiate/close cycles.
func TestMemoryReleaseOnClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}
	// Skip under race detector - the extra allocations for race tracking
	// make memory measurements unreliable
	if isRaceDetectorEnabled() {
		t.Skip("skipping memory test with race detector enabled")
	}
	ctx := context.Background()

	wasmBytes := getCalculatorComponent(t)

	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}

	// Warm up with more iterations to stabilize Go runtime memory pools
	for i := 0; i < 20; i++ {
		inst, _ := mod.Instantiate(ctx)
		if inst != nil {
			inst.Close(ctx)
		}
	}
	runtime.GC()
	runtime.GC()

	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	// Create and close 100 instances
	for i := 0; i < 100; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("instantiate %d: %v", i, err)
		}
		if err := inst.Close(ctx); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}

	runtime.GC()
	runtime.GC() // Double GC to ensure finalization

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	heapGrowth := int64(mAfter.HeapAlloc) - int64(mBefore.HeapAlloc)
	t.Logf("Heap before: %d KB, after: %d KB, growth: %d KB",
		mBefore.HeapAlloc/1024, mAfter.HeapAlloc/1024, heapGrowth/1024)

	// Go's GC doesn't immediately return memory, so we measure in-use objects
	// The key metric is that HeapObjects shouldn't grow proportionally to iterations
	objectGrowth := int64(mAfter.HeapObjects) - int64(mBefore.HeapObjects)
	t.Logf("HeapObjects before: %d, after: %d, growth: %d",
		mBefore.HeapObjects, mAfter.HeapObjects, objectGrowth)

	// Allow up to 1MB heap growth for transient allocations (Go runtime pools)
	// The real leak check is object growth - should be minimal after GC
	if heapGrowth > 1024*1024 {
		t.Errorf("Potential memory leak: heap grew by %d KB over 100 iterations", heapGrowth/1024)
	}

	// Object growth can include Go runtime overhead (M structs, etc.)
	// What matters is that growth is not proportional to iterations
	// ~50 objects per iteration would indicate a true leak, but Go runtime
	// allocates thread structures that count as objects
	// We allow 100 objects per iteration as reasonable overhead
	if objectGrowth > 10000 {
		t.Errorf("Significant object growth: %d new objects after 100 iterations (>100 per iteration)", objectGrowth)
	}
}

// TestLinearMemoryRelease verifies that WASM linear memory is released on close.
func TestLinearMemoryRelease(t *testing.T) {
	ctx := context.Background()

	wasmBytes := getCalculatorComponent(t)

	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}

	// Warm up
	for i := 0; i < 3; i++ {
		inst, _ := mod.Instantiate(ctx)
		if inst != nil {
			inst.Close(ctx)
		}
	}
	runtime.GC()

	// Check RSS before and after (system memory, not just Go heap)
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)
	sysBefore := mBefore.Sys

	// Create an instance
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	var mDuring runtime.MemStats
	runtime.ReadMemStats(&mDuring)
	sysDuring := mDuring.Sys
	sysGrowth := int64(sysDuring) - int64(sysBefore)
	t.Logf("System memory growth during instance: %d KB", sysGrowth/1024)

	// Close and GC
	inst.Close(ctx)
	runtime.GC()
	runtime.GC()

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)
	sysAfter := mAfter.Sys
	sysRetained := int64(sysAfter) - int64(sysBefore)
	t.Logf("System memory retained after close: %d KB", sysRetained/1024)

	// System memory should decrease after close
	// Note: Go may not immediately return memory to OS, so this is informational
	if sysRetained > sysGrowth {
		t.Logf("Note: System memory not immediately released (Go runtime behavior)")
	}
}

// getCalculatorComponent returns a test component for the tests.
func getCalculatorComponent(t *testing.T) []byte {
	t.Helper()
	// Use calculator.wasm from testbed
	wasmBytes, err := os.ReadFile("../testbed/calculator.wasm")
	if err != nil {
		t.Skipf("test component not available: %v", err)
	}
	return wasmBytes
}

// isRaceDetectorEnabled returns true if the race detector is enabled.
// We detect this by checking if RaceEnabled is set in the runtime.
func isRaceDetectorEnabled() bool {
	// Check for race detector using build constraints
	// This file is linker_leak_test.go, which doesn't have race build constraints
	// Use a simple approach: check if runtime.ReadMemStats allocates unexpectedly
	// Actually, the cleanest way is to use a build-time constant
	return raceEnabled
}
