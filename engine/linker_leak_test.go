package engine

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"go.bytecodealliance.org/wit"
)

// instanceCleanupViolations inspects a WazeroInstance and the parent WazeroEngine
// to deterministically verify that all resources associated with the instance were
// properly cleaned up.
//
// If expectBridgeAlive is true (e.g. when surviving instances still share the bridge),
// the bridge module is expected to remain registered in Wazero runtime.
// If expectBridgeAlive is false (final instance closed), the bridge module must be
// deregistered and closed in Wazero runtime.
func instanceCleanupViolations(
	engine *WazeroEngine,
	inst *WazeroInstance,
	coreModNames []string,
	bridgeNames []string,
	expectBridgeAlive bool,
) []string {
	var violations []string

	// 1. Core modules must be closed and deregistered from Wazero runtime
	for _, name := range coreModNames {
		if engine.runtime.Module(name) != nil {
			violations = append(violations, fmt.Sprintf("core module %q still registered in runtime", name))
		}
	}

	// 2. Bridge modules check in Wazero runtime
	for _, name := range bridgeNames {
		mod := engine.runtime.Module(name)
		if expectBridgeAlive {
			if mod == nil {
				violations = append(violations, fmt.Sprintf("bridge module %q was prematurely closed in runtime while surviving instances exist", name))
			}
		} else {
			if mod != nil {
				violations = append(violations, fmt.Sprintf("bridge module %q still registered in runtime after final close", name))
			}
		}
	}

	// 3. Instance struct reference clearing
	if inst.linkerInst != nil {
		violations = append(violations, "inst.linkerInst is not nil")
	}
	if inst.instance != nil {
		violations = append(violations, "inst.instance is not nil")
	}
	if inst.memory != nil {
		violations = append(violations, "inst.memory is not nil")
	}
	if inst.allocFn != nil {
		violations = append(violations, "inst.allocFn is not nil")
	}
	if inst.freeFn != nil {
		violations = append(violations, "inst.freeFn is not nil")
	}
	if inst.alloc != nil {
		violations = append(violations, "inst.alloc is not nil")
	}
	if inst.resources != nil {
		violations = append(violations, "inst.resources is not nil")
	}
	if inst.exportBindings != nil {
		violations = append(violations, "inst.exportBindings is not nil")
	}
	if inst.allocatorCache != nil {
		violations = append(violations, "inst.allocatorCache is not nil")
	}
	if inst.memoryCache != nil {
		violations = append(violations, "inst.memoryCache is not nil")
	}
	if inst.asyncifyCache != nil {
		violations = append(violations, "inst.asyncifyCache is not nil")
	}
	if inst.activeSession != nil {
		violations = append(violations, "inst.activeSession is not nil")
	}

	// 4. Memory reporting state
	if inst.HasMemory() {
		violations = append(violations, "inst.HasMemory() is true")
	}
	if sz := inst.MemorySize(); sz != 0 {
		violations = append(violations, fmt.Sprintf("inst.MemorySize() = %d, want 0", sz))
	}

	return violations
}

// TestBridgeModuleCleanup checks registration, shared bridge lifetime, and
// detachment of instance-owned references. Process-wide heap deltas cannot
// establish this contract: stock Wazero 1.12 may retain closed compiler buffers
// in its sorted-module slice until overwritten or runtime close. Memory trends
// belong in isolated, repeated load measurements, not a fixed heap-delta gate.
func TestBridgeModuleCleanup(t *testing.T) {
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
	if err := mod.RegisterHostFuncTyped("wisma:calculator/host@0.1.0", "log", func(string) {}); err != nil {
		t.Fatalf("register log host func: %v", err)
	}
	if err := mod.RegisterHostFuncTyped("wisma:calculator/host@0.1.0", "compute", func(a, b uint32) uint32 { return a + b }); err != nil {
		t.Fatalf("register compute host func: %v", err)
	}

	hostBridge := "wisma:calculator/host@0.1.0"

	t.Run("SingleInstanceLifecycleAndTeardown", func(t *testing.T) {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		if inst.linkerInst == nil {
			t.Fatal("expected non-nil linkerInst on multi-module component")
		}
		if !inst.HasMemory() || inst.MemorySize() == 0 {
			t.Fatalf("expected active memory before close, got HasMemory=%v size=%d", inst.HasMemory(), inst.MemorySize())
		}

		// Collect core module names
		coreMods := inst.linkerInst.Modules()
		if len(coreMods) == 0 {
			t.Fatal("expected core modules instantiated for calculator component")
		}
		coreNames := make([]string, len(coreMods))
		for i, m := range coreMods {
			coreNames[i] = m.Name()
			if engine.runtime.Module(m.Name()) == nil {
				t.Fatalf("core module %q not registered in runtime during instance lifetime", m.Name())
			}
		}

		// Verify bridge module is registered in runtime
		if engine.runtime.Module(hostBridge) == nil {
			t.Fatalf("bridge module %q not registered in runtime during instance lifetime", hostBridge)
		}

		// Verify functional execution
		got, err := inst.CallWithTypes(ctx, "process", []wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(5), uint32(3))
		if err != nil || got != uint32(15) {
			t.Fatalf("process call result = %v, err = %v, want 15", got, err)
		}

		// Close instance
		if err := inst.Close(ctx); err != nil {
			t.Fatalf("close instance: %v", err)
		}

		// Assert deterministic cleanup
		violations := instanceCleanupViolations(engine, inst, coreNames, []string{hostBridge}, false)
		if len(violations) > 0 {
			t.Errorf("cleanup violations after single instance close: %v", violations)
		}

		// Assert post-close call fails
		_, callErr := inst.CallWithTypes(ctx, "process", []wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(5), uint32(3))
		if callErr == nil {
			t.Error("expected error calling process on closed instance, got nil")
		}
	})

	t.Run("MultiInstanceSurvivorBridgeRetentionAndTeardown", func(t *testing.T) {
		first, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("instantiate first: %v", err)
		}
		survivor, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("instantiate survivor: %v", err)
		}

		firstMods := make([]string, len(first.linkerInst.Modules()))
		for i, m := range first.linkerInst.Modules() {
			firstMods[i] = m.Name()
		}
		survivorMods := make([]string, len(survivor.linkerInst.Modules()))
		for i, m := range survivor.linkerInst.Modules() {
			survivorMods[i] = m.Name()
		}

		// Both instances execute successfully
		for _, inst := range []*WazeroInstance{first, survivor} {
			got, err := inst.CallWithTypes(ctx, "process", []wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(5), uint32(3))
			if err != nil || got != uint32(15) {
				t.Fatalf("call on open instance: %v, err=%v", got, err)
			}
		}

		// Close first instance
		if err := first.Close(ctx); err != nil {
			t.Fatalf("close first: %v", err)
		}

		// First instance must be cleaned up, but bridge must SURVIVE for survivor
		violationsFirst := instanceCleanupViolations(engine, first, firstMods, []string{hostBridge}, true)
		if len(violationsFirst) > 0 {
			t.Errorf("first instance cleanup violations (expecting bridge alive): %v", violationsFirst)
		}

		// Bridge module must still be registered in Wazero runtime
		if engine.runtime.Module(hostBridge) == nil {
			t.Fatalf("bridge module %q was prematurely closed while survivor is alive", hostBridge)
		}

		// Survivor must remain fully functional
		got, err := survivor.CallWithTypes(ctx, "process", []wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(5), uint32(3))
		if err != nil || got != uint32(15) {
			t.Fatalf("call on survivor after first close: %v, err=%v", got, err)
		}

		// Close survivor instance (final reference)
		if err := survivor.Close(ctx); err != nil {
			t.Fatalf("close survivor: %v", err)
		}

		// Survivor instance must be cleaned up AND bridge must be closed
		violationsSurvivor := instanceCleanupViolations(engine, survivor, survivorMods, []string{hostBridge}, false)
		if len(violationsSurvivor) > 0 {
			t.Errorf("survivor instance cleanup violations (expecting bridge closed): %v", violationsSurvivor)
		}

		// Bridge module must now be completely gone from runtime
		if mod := engine.runtime.Module(hostBridge); mod != nil {
			t.Fatalf("bridge module %q still registered in runtime after all instances closed", hostBridge)
		}
	})

	t.Run("ReinstantiationAfterAllClosed", func(t *testing.T) {
		fresh, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("instantiate fresh: %v", err)
		}
		freshMods := make([]string, len(fresh.linkerInst.Modules()))
		for i, m := range fresh.linkerInst.Modules() {
			freshMods[i] = m.Name()
		}

		// Bridge module is recreated
		if engine.runtime.Module(hostBridge) == nil {
			t.Fatalf("bridge module %q not created for fresh instance", hostBridge)
		}

		got, err := fresh.CallWithTypes(ctx, "process", []wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(5), uint32(3))
		if err != nil || got != uint32(15) {
			t.Fatalf("fresh process call: %v, err=%v", got, err)
		}

		if err := fresh.Close(ctx); err != nil {
			t.Fatalf("close fresh: %v", err)
		}

		violations := instanceCleanupViolations(engine, fresh, freshMods, []string{hostBridge}, false)
		if len(violations) > 0 {
			t.Errorf("cleanup violations after fresh instance close: %v", violations)
		}
	})
}

// TestBridgeCleanup_FailsOnMissingCleanup verifies that instanceCleanupViolations
// correctly fails and pinpoints missing cleanup if an instance or its modules
// are left unclosed, ensuring the test suite guards against real Wippy lifecycle bugs.
func TestBridgeCleanup_FailsOnMissingCleanup(t *testing.T) {
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
	if err := mod.RegisterHostFuncTyped("wisma:calculator/host@0.1.0", "log", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if err := mod.RegisterHostFuncTyped("wisma:calculator/host@0.1.0", "compute", func(a, b uint32) uint32 { return a + b }); err != nil {
		t.Fatal(err)
	}

	hostBridge := "wisma:calculator/host@0.1.0"
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	coreMods := make([]string, len(inst.linkerInst.Modules()))
	for i, m := range inst.linkerInst.Modules() {
		coreMods[i] = m.Name()
	}

	// 1. Evaluate cleanup verification on the active (unclosed) instance.
	// This represents a real Wippy bug where cleanup was omitted.
	violations := instanceCleanupViolations(engine, inst, coreMods, []string{hostBridge}, false)
	if len(violations) == 0 {
		t.Fatal("expected cleanup verification to detect violations on unclosed instance, but got 0")
	}

	// Ensure all key lifecycle violations are detected
	hasCoreReg := false
	hasBridgeReg := false
	hasLinkerInst := false
	hasActiveMemory := false
	for _, v := range violations {
		if strings.Contains(v, "core module") && strings.Contains(v, "still registered") {
			hasCoreReg = true
		}
		if strings.Contains(v, "bridge module") && strings.Contains(v, "still registered") {
			hasBridgeReg = true
		}
		if strings.Contains(v, "linkerInst is not nil") {
			hasLinkerInst = true
		}
		if strings.Contains(v, "HasMemory() is true") {
			hasActiveMemory = true
		}
	}

	if !hasCoreReg {
		t.Error("expected violation for core module still registered in runtime")
	}
	if !hasBridgeReg {
		t.Error("expected violation for bridge module still registered in runtime")
	}
	if !hasLinkerInst {
		t.Error("expected violation for linkerInst retained")
	}
	if !hasActiveMemory {
		t.Error("expected violation for active memory retained")
	}

	// 2. Now perform proper close and verify all violations are resolved
	if err := inst.Close(ctx); err != nil {
		t.Fatalf("proper close: %v", err)
	}

	cleanViolations := instanceCleanupViolations(engine, inst, coreMods, []string{hostBridge}, false)
	if len(cleanViolations) > 0 {
		t.Fatalf("expected 0 violations after proper Close, got %d: %v", len(cleanViolations), cleanViolations)
	}
}

// TestBridgeModuleRefCounting verifies deterministic bridge lifecycle and survivor behavior:
// when multiple instances share bridge modules, closing one instance must leave surviving
// instances fully functional. Shared bridge modules and resources must only be released when
// all instances using them are closed.
func TestBridgeModuleRefCounting(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	mod, err := engine.LoadModule(ctx, getCalculatorComponent(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = mod.RegisterHostFuncTyped("wisma:calculator/host@0.1.0", "log", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if err = mod.RegisterHostFuncTyped("wisma:calculator/host@0.1.0", "compute", func(a, b uint32) uint32 { return a + b }); err != nil {
		t.Fatal(err)
	}
	first, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(ctx)
	survivor, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer survivor.Close(ctx)
	if err = first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		got, callErr := survivor.CallWithTypes(ctx, "process", []wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(5), uint32(3))
		if callErr != nil || got != uint32(15) {
			t.Fatalf("surviving process: result=%v err=%v", got, callErr)
		}
	}
	if err = survivor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	fresh, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close(ctx)
	got, err := fresh.CallWithTypes(ctx, "process", []wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(5), uint32(3))
	if err != nil || got != uint32(15) {
		t.Fatalf("fresh process after all close: result=%v err=%v", got, err)
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
	if !inst.HasMemory() || inst.MemorySize() == 0 {
		t.Fatalf("expected active memory before close, got HasMemory=%v size=%d", inst.HasMemory(), inst.MemorySize())
	}

	// Close and GC
	if err := inst.Close(ctx); err != nil {
		t.Fatalf("close instance: %v", err)
	}
	if inst.HasMemory() {
		t.Error("expected inst.HasMemory() == false after close")
	}
	if sz := inst.MemorySize(); sz != 0 {
		t.Errorf("expected inst.MemorySize() == 0 after close, got %d", sz)
	}
	if inst.memory != nil {
		t.Error("expected inst.memory == nil after close")
	}
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
