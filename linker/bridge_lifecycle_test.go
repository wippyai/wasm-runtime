package linker

import (
	"context"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

// TestBridgeLifecycle_SurvivorBehavior verifies that when multiple instances share
// a bridge module, closing one instance leaves surviving instances intact and
// functional with the bridge module kept alive. The bridge module is only closed
// when the last surviving instance closes.
func TestBridgeLifecycle_SurvivorBehavior(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	defer l.Close()

	pre := &InstancePre{linker: l}

	// Create two instances that share the same bridge module
	inst1, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("create inst1: %v", err)
	}
	inst2, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("create inst2: %v", err)
	}

	// Build a shared bridge WASM module that exports a callable function
	bridgeModName := "shared-bridge-alpha"
	modBytes, err := wat.Compile(`(module
		(func $compute (result i32)
			i32.const 42
		)
		(export "compute" (func $compute))
	)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	compiled, err := rt.CompileModule(ctx, modBytes)
	if err != nil {
		t.Fatalf("compile bridge module: %v", err)
	}
	defer compiled.Close(ctx)

	_, err = rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(bridgeModName))
	if err != nil {
		t.Fatalf("instantiate bridge module: %v", err)
	}

	// Both instances register and reference the shared bridge
	inst1.bridgeModules[bridgeModName] = true
	l.addBridgeRefs(map[string]bool{bridgeModName: true})

	inst2.bridgeModules[bridgeModName] = true
	l.addBridgeRefs(map[string]bool{bridgeModName: true})

	// Private inspection: ref count must be exactly 2
	l.hostModuleMu.Lock()
	initialCount := l.bridgeRefCount[bridgeModName]
	l.hostModuleMu.Unlock()
	if initialCount != 2 {
		t.Fatalf("bridgeRefCount[%q] = %d, want 2", bridgeModName, initialCount)
	}
	if rt.Module(bridgeModName) == nil {
		t.Fatal("bridge module should exist in runtime")
	}

	// Close inst1
	if err := inst1.Close(ctx); err != nil {
		t.Fatalf("close inst1: %v", err)
	}

	// Assert inst1 teardown
	inst1BM := inst1.bridgeModules
	if inst1BM != nil {
		t.Error("inst1.bridgeModules should be nil after Close")
	}

	// Private inspection of survivor behavior:
	// Ref count must have decremented to 1, and the bridge module MUST SURVIVE in runtime.
	l.hostModuleMu.Lock()
	survivorCount := l.bridgeRefCount[bridgeModName]
	l.hostModuleMu.Unlock()
	if survivorCount != 1 {
		t.Fatalf("bridgeRefCount after inst1 close = %d, want 1", survivorCount)
	}
	mod := rt.Module(bridgeModName)
	if mod == nil {
		t.Fatal("bridge module was prematurely closed while inst2 is still surviving")
	}

	// Survivor inst2 behavior: inst2 is still open, references the bridge, and its function works
	hasBridge := inst2.bridgeModules[bridgeModName]
	if !hasBridge {
		t.Fatal("inst2 should still reference the bridge module")
	}

	fn := mod.ExportedFunction("compute")
	if fn == nil {
		t.Fatal("surviving bridge module compute function not found")
	}
	results, err := fn.Call(ctx)
	if err != nil {
		t.Fatalf("calling compute on surviving bridge: %v", err)
	}
	if len(results) == 0 || results[0] != 42 {
		t.Fatalf("compute result = %v, want 42", results)
	}

	// Create inst3 while inst2 is still alive - increments ref count back to 2
	inst3, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("create inst3: %v", err)
	}
	inst3.bridgeModules[bridgeModName] = true
	l.addBridgeRefs(map[string]bool{bridgeModName: true})

	l.hostModuleMu.Lock()
	countWith3 := l.bridgeRefCount[bridgeModName]
	l.hostModuleMu.Unlock()
	if countWith3 != 2 {
		t.Fatalf("bridgeRefCount with inst3 = %d, want 2", countWith3)
	}

	// Close inst2 (the original survivor)
	if err := inst2.Close(ctx); err != nil {
		t.Fatalf("close inst2: %v", err)
	}

	// Bridge module must survive inst2's closure because inst3 is alive
	l.hostModuleMu.Lock()
	countAfter2 := l.bridgeRefCount[bridgeModName]
	l.hostModuleMu.Unlock()
	if countAfter2 != 1 {
		t.Fatalf("bridgeRefCount after inst2 close = %d, want 1", countAfter2)
	}
	if rt.Module(bridgeModName) == nil {
		t.Fatal("bridge module prematurely closed while inst3 is still surviving")
	}

	// Close inst3 (the final instance holding the bridge)
	if err := inst3.Close(ctx); err != nil {
		t.Fatalf("close inst3: %v", err)
	}

	// Final teardown verification:
	// Bridge module must now be removed from bridgeRefCount and closed in runtime.
	l.hostModuleMu.Lock()
	_, exists := l.bridgeRefCount[bridgeModName]
	l.hostModuleMu.Unlock()
	if exists {
		t.Fatalf("bridge %q still present in bridgeRefCount after last instance closed", bridgeModName)
	}
	if rt.Module(bridgeModName) != nil {
		t.Fatal("bridge module should be closed in runtime after last reference released")
	}
}

// TestBridgeLifecycle_MultiModuleSurvivorMatrix verifies that instances sharing
// some bridges and holding exclusive bridges release only the unshared bridges
// on partial close, keeping shared bridges alive for surviving instances.
func TestBridgeLifecycle_MultiModuleSurvivorMatrix(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	defer l.Close()

	pre := &InstancePre{linker: l}

	instA, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("create instA: %v", err)
	}
	instB, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("create instB: %v", err)
	}

	bridges := []string{"bridge-shared", "bridge-only-a", "bridge-only-b"}
	for _, name := range bridges {
		_, err := rt.NewHostModuleBuilder(name).
			NewFunctionBuilder().
			WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}), nil, nil).
			Export("dummy").
			Instantiate(ctx)
		if err != nil {
			t.Fatalf("create bridge %s: %v", name, err)
		}
	}

	// instA references bridge-shared and bridge-only-a
	instA.bridgeModules["bridge-shared"] = true
	instA.bridgeModules["bridge-only-a"] = true
	l.addBridgeRefs(map[string]bool{"bridge-shared": true, "bridge-only-a": true})

	// instB references bridge-shared and bridge-only-b
	instB.bridgeModules["bridge-shared"] = true
	instB.bridgeModules["bridge-only-b"] = true
	l.addBridgeRefs(map[string]bool{"bridge-shared": true, "bridge-only-b": true})

	// Inspect initial ref counts
	l.hostModuleMu.Lock()
	cntShared := l.bridgeRefCount["bridge-shared"]
	cntA := l.bridgeRefCount["bridge-only-a"]
	cntB := l.bridgeRefCount["bridge-only-b"]
	l.hostModuleMu.Unlock()

	if cntShared != 2 || cntA != 1 || cntB != 1 {
		t.Fatalf("initial ref counts: shared=%d, A=%d, B=%d (want 2, 1, 1)", cntShared, cntA, cntB)
	}

	// Close instA
	if err := instA.Close(ctx); err != nil {
		t.Fatalf("close instA: %v", err)
	}

	// Inspect after instA close:
	// - bridge-only-a must be released (count 0, deleted, closed in runtime)
	// - bridge-shared must SURVIVE for instB (count 1, still in runtime)
	// - bridge-only-b must be unaffected (count 1, still in runtime)
	l.hostModuleMu.Lock()
	cntSharedAfterA := l.bridgeRefCount["bridge-shared"]
	_, hasA := l.bridgeRefCount["bridge-only-a"]
	cntBAfterA := l.bridgeRefCount["bridge-only-b"]
	l.hostModuleMu.Unlock()

	if cntSharedAfterA != 1 {
		t.Errorf("bridge-shared ref count after instA close = %d, want 1", cntSharedAfterA)
	}
	if rt.Module("bridge-shared") == nil {
		t.Error("bridge-shared was prematurely closed while instB survives")
	}
	if hasA {
		t.Errorf("bridge-only-a still present in bridgeRefCount after instA close")
	}
	if rt.Module("bridge-only-a") != nil {
		t.Error("bridge-only-a was not closed in runtime when its only reference closed")
	}
	if cntBAfterA != 1 {
		t.Errorf("bridge-only-b ref count = %d, want 1", cntBAfterA)
	}
	if rt.Module("bridge-only-b") == nil {
		t.Error("bridge-only-b should still exist in runtime")
	}

	// Close instB
	if err := instB.Close(ctx); err != nil {
		t.Fatalf("close instB: %v", err)
	}

	// After instB close, all bridge modules must be closed and deleted
	l.hostModuleMu.Lock()
	for _, name := range bridges {
		if cnt, exists := l.bridgeRefCount[name]; exists {
			t.Errorf("bridge %q still in ref count map with count %d after all closed", name, cnt)
		}
	}
	l.hostModuleMu.Unlock()

	for _, name := range bridges {
		if rt.Module(name) != nil {
			t.Errorf("bridge %q still alive in runtime after all closed", name)
		}
	}
}

// TestBridgeLifecycle_ConcurrentSurvivorCloseRace tests concurrent closing of
// multiple instances sharing bridges to ensure thread-safety and no data races.
func TestBridgeLifecycle_ConcurrentSurvivorCloseRace(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	defer l.Close()

	pre := &InstancePre{linker: l}

	const numInstances = 16
	bridgeNames := []string{"race-bridge-1", "race-bridge-2"}

	for _, name := range bridgeNames {
		_, err := rt.NewHostModuleBuilder(name).
			NewFunctionBuilder().
			WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}), nil, nil).
			Export("noop").
			Instantiate(ctx)
		if err != nil {
			t.Fatalf("create bridge %s: %v", name, err)
		}
	}

	instances := make([]*Instance, numInstances)
	for i := 0; i < numInstances; i++ {
		inst, err := pre.NewInstance(ctx)
		if err != nil {
			t.Fatalf("create instance %d: %v", i, err)
		}
		for _, name := range bridgeNames {
			inst.bridgeModules[name] = true
			l.addBridgeRefs(map[string]bool{name: true})
		}
		instances[i] = inst
	}

	// Verify initial counts
	l.hostModuleMu.Lock()
	for _, name := range bridgeNames {
		if l.bridgeRefCount[name] != numInstances {
			t.Fatalf("initial count for %s = %d, want %d", name, l.bridgeRefCount[name], numInstances)
		}
	}
	l.hostModuleMu.Unlock()

	// Concurrently close all instances
	var wg sync.WaitGroup
	errCh := make(chan error, numInstances)

	for _, inst := range instances {
		wg.Add(1)
		go func(it *Instance) {
			defer wg.Done()
			if err := it.Close(ctx); err != nil {
				errCh <- err
			}
		}(inst)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent Close error: %v", err)
	}

	// Verify final state: all bridge ref counts cleared and modules closed
	l.hostModuleMu.Lock()
	for _, name := range bridgeNames {
		if cnt, exists := l.bridgeRefCount[name]; exists {
			t.Errorf("bridge %s still tracked in bridgeRefCount with count %d", name, cnt)
		}
	}
	l.hostModuleMu.Unlock()

	for _, name := range bridgeNames {
		if rt.Module(name) != nil {
			t.Errorf("bridge %s still in runtime after all instances closed", name)
		}
	}
}

// TestBridgeLifecycle_IdempotentClose verifies that calling Close multiple times
// on an instance does not double-decrement bridge reference counts.
func TestBridgeLifecycle_IdempotentClose(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	defer l.Close()

	pre := &InstancePre{linker: l}

	inst1, _ := pre.NewInstance(ctx)
	inst2, _ := pre.NewInstance(ctx)

	bridgeName := "idempotent-bridge"
	_, err := rt.NewHostModuleBuilder(bridgeName).
		NewFunctionBuilder().
		WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}), nil, nil).
		Export("noop").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("create bridge: %v", err)
	}

	inst1.bridgeModules[bridgeName] = true
	l.addBridgeRefs(map[string]bool{bridgeName: true})

	inst2.bridgeModules[bridgeName] = true
	l.addBridgeRefs(map[string]bool{bridgeName: true})

	// Close inst1 three times
	for i := 0; i < 3; i++ {
		if err := inst1.Close(ctx); err != nil {
			t.Errorf("close inst1 iteration %d: %v", i, err)
		}
	}

	// Ref count must remain 1 because inst2 still holds a reference
	l.hostModuleMu.Lock()
	count := l.bridgeRefCount[bridgeName]
	l.hostModuleMu.Unlock()
	if count != 1 {
		t.Fatalf("ref count after repeated inst1 Close = %d, want 1", count)
	}
	if rt.Module(bridgeName) == nil {
		t.Fatal("bridge module was closed despite surviving inst2")
	}

	// Close inst2 three times
	for i := 0; i < 3; i++ {
		if err := inst2.Close(ctx); err != nil {
			t.Errorf("close inst2 iteration %d: %v", i, err)
		}
	}

	// Now ref count must be 0 and module closed
	l.hostModuleMu.Lock()
	_, exists := l.bridgeRefCount[bridgeName]
	l.hostModuleMu.Unlock()
	if exists {
		t.Errorf("bridge still tracked after inst2 closed")
	}
	if rt.Module(bridgeName) != nil {
		t.Errorf("bridge module still alive in runtime after all closed")
	}
}
