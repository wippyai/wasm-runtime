package engine

import (
	"context"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

func getCompiledModuleCount(c wazero.CompilationCache) uint32 {
	v := reflect.ValueOf(c).Elem().FieldByName("engs")
	if !v.IsValid() {
		return 0
	}
	var count uint32
	for i := 0; i < v.Len(); i++ {
		engVal := v.Index(i)
		if !engVal.IsNil() {
			engExported := reflect.NewAt(engVal.Type(), unsafe.Pointer(engVal.UnsafeAddr())).Elem()
			method := engExported.MethodByName("CompiledModuleCount")
			if method.IsValid() {
				res := method.Call(nil)
				count += uint32(res[0].Uint())
			}
		}
	}
	return count
}

func compileSimpleAddWat(t *testing.T) []byte {
	t.Helper()
	wasm, err := wat.Compile(`(module
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add)
	)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	return wasm
}

// TestCompiledOwnership_SingleModule_SharedCache verifies:
//  1. Two engines sharing a CompilationCache reuse compiled machine code (count = 1).
//  2. Initializing host modules (e.g. WASI) adds runtime-local host trampolines that
//     are isolated per runtime.
//  3. Closing engine 1 releases its per-runtime compiled guest handle and evicts its
//     host trampolines, but does NOT evict sibling guest code or sibling host trampolines.
//  4. Repeat Close on engine 1 is idempotent and causes no double-close.
//  5. Sibling engine 2 continues running normally and can instantiate/call after engine 1 is closed.
//  6. Closing engine 2 releases its handles, dropping total cache count to 0.
//  7. Caller's CompilationCache is NOT closed by engine Close; subsequent engines can still use it.
func TestCompiledOwnership_SingleModule_SharedCache(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	wasmBytes := compileSimpleAddWat(t)

	cfg := &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	}

	eng1, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig eng1: %v", err)
	}
	defer eng1.Close(ctx)

	eng2, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig eng2: %v", err)
	}
	defer eng2.Close(ctx)

	mod1, err := eng1.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("eng1.LoadModule: %v", err)
	}

	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("after eng1 load: got count %d, want 1", count)
	}

	mod2, err := eng2.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("eng2.LoadModule: %v", err)
	}

	// Cache hit: guest machine code is shared across engines
	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("after eng2 load: got count %d, want 1", count)
	}

	// Instantiate on eng1: adds 1 WASI host module for eng1
	inst1, err := mod1.Instantiate(ctx)
	if err != nil {
		t.Fatalf("mod1.Instantiate: %v", err)
	}
	defer inst1.Close(ctx)

	fn1 := inst1.GetExportedFunction("add")
	res1, err := fn1.Call(ctx, 10, 20)
	if err != nil || len(res1) != 1 || res1[0] != 30 {
		t.Fatalf("inst1 add call: got %v, err %v", res1, err)
	}

	// Instantiate on eng2: adds 1 WASI host module for eng2 (total: 1 guest + 2 host = 3)
	inst2, err := mod2.Instantiate(ctx)
	if err != nil {
		t.Fatalf("mod2.Instantiate: %v", err)
	}
	defer inst2.Close(ctx)

	fn2 := inst2.GetExportedFunction("add")
	res2, err := fn2.Call(ctx, 100, 200)
	if err != nil || len(res2) != 1 || res2[0] != 300 {
		t.Fatalf("inst2 add call: got %v, err %v", res2, err)
	}

	if count := getCompiledModuleCount(cache); count != 3 {
		t.Fatalf("with both instances live: got count %d, want 3 (1 guest + 2 host)", count)
	}

	// Close engine 1: releases eng1 guest handle and closes eng1 host module
	if err := eng1.Close(ctx); err != nil {
		t.Fatalf("eng1.Close: %v", err)
	}
	if !eng1.IsClosed() {
		t.Fatal("eng1 should report closed")
	}

	// Repeat Close must be idempotent (no double close)
	for repeat := 0; repeat < 3; repeat++ {
		if err := eng1.Close(ctx); err != nil {
			t.Fatalf("eng1 repeat Close: %v", err)
		}
	}

	// Sibling engine 2 must still have its code in the shared cache (1 guest + 1 eng2 host = 2)
	if count := getCompiledModuleCount(cache); count != 2 {
		t.Fatalf("after eng1 close: got count %d, want 2 (1 guest + 1 sibling host)", count)
	}

	// Sibling engine 2 must still be able to call existing instances and instantiate new instances!
	res2Again, err := fn2.Call(ctx, 5, 7)
	if err != nil || len(res2Again) != 1 || res2Again[0] != 12 {
		t.Fatalf("sibling inst2 after eng1 close: got %v, err %v", res2Again, err)
	}

	inst2New, err := mod2.Instantiate(ctx)
	if err != nil {
		t.Fatalf("mod2 new instantiation after eng1 close: %v", err)
	}
	defer inst2New.Close(ctx)

	fn2New := inst2New.GetExportedFunction("add")
	res2New, err := fn2New.Call(ctx, 40, 60)
	if err != nil || len(res2New) != 1 || res2New[0] != 100 {
		t.Fatalf("inst2New add call: got %v, err %v", res2New, err)
	}

	// Close engine 2: all per-runtime guest handles and host modules released
	if err := eng2.Close(ctx); err != nil {
		t.Fatalf("eng2.Close: %v", err)
	}
	if !eng2.IsClosed() {
		t.Fatal("eng2 should report closed")
	}

	// Count should drop to 0
	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("after eng2 close: got count %d, want 0", count)
	}

	// Verify caller's cache was NOT closed: another engine can reuse it
	eng3, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("eng3 creation: %v", err)
	}
	defer eng3.Close(ctx)

	_, err = eng3.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("eng3.LoadModule with caller cache: %v", err)
	}
	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("after eng3 load: got count %d, want 1", count)
	}
	if err := eng3.Close(ctx); err != nil {
		t.Fatalf("eng3.Close: %v", err)
	}
	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("after eng3 close: got count %d, want 0", count)
	}
}

// TestCompiledOwnership_ModuleIndividualClose verifies explicit module.Close()
// releases the compiled module handle immediately before engine close.
func TestCompiledOwnership_ModuleIndividualClose(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	wasmBytes := compileSimpleAddWat(t)
	eng, err := NewWazeroEngineWithConfig(ctx, &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	})
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("initial count: %d, want 1", count)
	}

	// Close the module directly
	if err := mod.Close(ctx); err != nil {
		t.Fatalf("mod.Close: %v", err)
	}
	if !mod.IsClosed() {
		t.Fatal("mod should report closed")
	}

	// Count should drop to 0 immediately
	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("after mod.Close: got count %d, want 0", count)
	}

	// Repeat mod.Close is idempotent
	if err := mod.Close(ctx); err != nil {
		t.Fatalf("repeat mod.Close: %v", err)
	}

	// Operations on closed module fail cleanly
	if _, err := mod.Instantiate(ctx); err == nil {
		t.Fatal("Instantiate on closed module should fail")
	}
	if err := mod.Compile(ctx, &CompileConfig{}); err == nil {
		t.Fatal("Compile on closed module should fail")
	}

	// Subsequent eng.Close does not double-close
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("eng.Close: %v", err)
	}
}

// TestCompiledOwnership_AsyncifyTransformReplacement verifies that when Asyncify
// transforms and recompiles a module:
// 1. The old compiled module is closed.
// 2. The new compiled module replaces it.
// 3. Engine close releases the new compiled module without double-closing the old one.
func TestCompiledOwnership_AsyncifyTransformReplacement(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	watSrc := `(module
		(import "env" "do_async" (func $do_async (result i32)))
		(func (export "run") (result i32)
			call $do_async)
		(memory (export "memory") 1)
	)`
	wasmBytes, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	eng, err := NewWazeroEngineWithConfig(ctx, &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	})
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	// Register async host function
	err = mod.RegisterHostFuncRaw("env", "do_async", nil, []api.ValueType{api.ValueTypeI32},
		func(ctx context.Context, m api.Module, stack []uint64) {
			stack[0] = 42
		}, true)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	// Before compile: count = 1 (untransformed module)
	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("initial count: %d, want 1", count)
	}

	// Compile with asyncify: replaces m.compiled and closes oldCompiled
	if err := mod.Compile(ctx, &CompileConfig{EnableAsyncify: true}); err != nil {
		t.Fatalf("mod.Compile: %v", err)
	}
	if !mod.IsTransformed() {
		t.Fatal("mod should report transformed")
	}

	// Old compiled module was closed and replaced with transformed module.
	// Both were compiled in this cache; the old one was deleted upon oldCompiled.Close,
	// leaving 1 active compiled module in the cache.
	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("after asyncify compile count: %d, want 1", count)
	}

	// Repeat compile is a no-op
	if err := mod.Compile(ctx, &CompileConfig{EnableAsyncify: true}); err != nil {
		t.Fatalf("repeat mod.Compile: %v", err)
	}

	// Close engine: must release the replaced module handle cleanly
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("eng.Close: %v", err)
	}

	// Count drops to 0: no handle leaked, no double-close panic
	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("after eng.Close count: %d, want 0", count)
	}

	// Repeat Close is clean
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("repeat eng.Close: %v", err)
	}
}

// TestCompiledOwnership_MultiModuleComponent verifies that multi-module components
// compiled via linker (InstancePre) have their core compiled module handles released
// exactly once on engine Close.
func TestCompiledOwnership_MultiModuleComponent(t *testing.T) {
	ctx := context.Background()
	wasmBytes, err := os.ReadFile("../testbed/two_core_host.wasm")
	if err != nil {
		t.Fatal(err)
	}

	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	cfg := &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	}

	eng1, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("eng1 creation: %v", err)
	}
	defer eng1.Close(ctx)

	mod1, err := eng1.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("mod1 LoadModule: %v", err)
	}

	// Multi-module component compiles core modules on Compile()
	if err := mod1.Compile(ctx, &CompileConfig{}); err != nil {
		t.Fatalf("mod1.Compile: %v", err)
	}

	count1 := getCompiledModuleCount(cache)
	if count1 == 0 {
		t.Fatalf("expected positive compiled module count for multi-core component, got 0")
	}

	// Sibling engine loads and compiles the same component
	eng2, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("eng2 creation: %v", err)
	}
	defer eng2.Close(ctx)

	mod2, err := eng2.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("mod2 LoadModule: %v", err)
	}
	if err := mod2.Compile(ctx, &CompileConfig{}); err != nil {
		t.Fatalf("mod2.Compile: %v", err)
	}

	// Sharing cache: count should not double
	count2 := getCompiledModuleCount(cache)
	if count2 != count1 {
		t.Fatalf("count changed on sibling multi-core compile: before %d, after %d", count1, count2)
	}

	// Close eng1: releases mod1's InstancePre compiled handles
	if err := eng1.Close(ctx); err != nil {
		t.Fatalf("eng1.Close: %v", err)
	}

	// Sibling eng2 still holds its handles, count unchanged
	if count := getCompiledModuleCount(cache); count != count1 {
		t.Fatalf("count after eng1 close: got %d, want %d", count, count1)
	}

	// Close eng2: releases mod2's handles
	if err := eng2.Close(ctx); err != nil {
		t.Fatalf("eng2.Close: %v", err)
	}

	// All handles released, count drops to 0
	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("count after eng2 close: got %d, want 0", count)
	}
}

// TestCompiledOwnership_FactoryPinSimulation tests the root actor factory pattern:
// Root holds a factory compile pin on the shared cache.
// When actor engines spawn and close, refCounts fluctuate, but the factory pin
// guarantees machine code stays cached even when live actors = 0.
func TestCompiledOwnership_FactoryPinSimulation(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	wasmBytes := compileSimpleAddWat(t)

	// Simulate factory pin: factory compiles the module once and retains the handle
	factoryRuntime := wazero.NewRuntimeWithConfig(ctx,
		wazero.NewRuntimeConfig().
			WithCoreFeatures(api.CoreFeaturesV2).
			WithCompilationCache(cache).
			WithCloseOnContextDone(true))
	defer factoryRuntime.Close(ctx)

	factoryPin, err := factoryRuntime.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("factory compile pin: %v", err)
	}
	defer factoryPin.Close(ctx)

	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("factory pin count: %d, want 1", count)
	}

	cfg := &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	}

	// Spawn actor 1
	actor1, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("actor1 creation: %v", err)
	}
	mod1, err := actor1.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("actor1 LoadModule: %v", err)
	}
	inst1, err := mod1.Instantiate(ctx)
	if err != nil {
		t.Fatalf("actor1 Instantiate: %v", err)
	}
	// 1 guest module (pinned + actor1) + 1 WASI host module (actor1) = 2
	if count := getCompiledModuleCount(cache); count != 2 {
		t.Fatalf("actor1 running count: %d, want 2 (1 guest + 1 actor1 host)", count)
	}

	// Spawn actor 2
	actor2, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("actor2 creation: %v", err)
	}
	mod2, err := actor2.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("actor2 LoadModule: %v", err)
	}
	inst2, err := mod2.Instantiate(ctx)
	if err != nil {
		t.Fatalf("actor2 Instantiate: %v", err)
	}
	// 1 guest module + 2 WASI host modules = 3
	if count := getCompiledModuleCount(cache); count != 3 {
		t.Fatalf("actor2 running count: %d, want 3 (1 guest + 2 hosts)", count)
	}

	// Close both actors (simulating idle factory)
	_ = inst1.Close(ctx)
	_ = actor1.Close(ctx)
	_ = inst2.Close(ctx)
	_ = actor2.Close(ctx)

	// Zero actors live, but factory pin keeps compiled machine code cached!
	// Host modules are gone, only the 1 pinned guest remains.
	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("idle factory with pin: got count %d, want 1 (idle respawns stay cached)", count)
	}

	// Spawn actor 3: instant cache hit, executes correctly
	actor3, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("actor3 creation: %v", err)
	}
	mod3, err := actor3.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("actor3 LoadModule: %v", err)
	}
	inst3, err := mod3.Instantiate(ctx)
	if err != nil {
		t.Fatalf("actor3 Instantiate: %v", err)
	}
	res, err := inst3.GetExportedFunction("add").Call(ctx, 111, 222)
	if err != nil || len(res) != 1 || res[0] != 333 {
		t.Fatalf("actor3 execution: got %v, err %v", res, err)
	}
	_ = inst3.Close(ctx)
	_ = actor3.Close(ctx)

	// Still cached while pin is held
	if count := getCompiledModuleCount(cache); count != 1 {
		t.Fatalf("after actor3 close count: %d, want 1", count)
	}

	// Destroy factory: release the pin
	if err := factoryPin.Close(ctx); err != nil {
		t.Fatalf("factoryPin.Close: %v", err)
	}

	// Now count drops to 0!
	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("after factory pin close: got count %d, want 0", count)
	}
}

// TestCompiledOwnership_LoadCloseRace tests concurrent LoadModule vs Close.
// Guarantees no deadlocks, no unclosed handles, and safe cancellation.
func TestCompiledOwnership_LoadCloseRace(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	wasmBytes := compileSimpleAddWat(t)
	cfg := &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	}

	for iter := 0; iter < 10; iter++ {
		eng, err := NewWazeroEngineWithConfig(ctx, cfg)
		if err != nil {
			t.Fatalf("iter %d: engine creation: %v", iter, err)
		}

		var wg sync.WaitGroup
		const numLoaders = 8
		wg.Add(numLoaders + 1)

		for i := 0; i < numLoaders; i++ {
			go func() {
				defer wg.Done()
				_, _ = eng.LoadModule(ctx, wasmBytes)
			}()
		}

		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(iter*50) * time.Microsecond)
			_ = eng.Close(ctx)
		}()

		wg.Wait()

		// Repeat Close must be safe
		if err := eng.Close(ctx); err != nil {
			t.Fatalf("iter %d: repeat Close: %v", iter, err)
		}
	}

	// After all iterations, any handles that were loaded must have been closed
	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("after race test: leaked compiled modules in cache: count %d", count)
	}
}

// TestCompiledOwnership_ErrorCleanup verifies error conditions:
// 1. Loading on a closed engine fails and does not leak.
// 2. Operations on a closed module fail cleanly.
func TestCompiledOwnership_ErrorCleanup(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	wasmBytes := compileSimpleAddWat(t)
	cfg := &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	}

	eng, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("engine creation: %v", err)
	}

	// Close engine first
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("eng.Close: %v", err)
	}

	// LoadModule on closed engine must fail and not compile
	if _, err := eng.LoadModule(ctx, wasmBytes); err == nil {
		t.Fatal("LoadModule on closed engine should return error")
	}

	if count := getCompiledModuleCount(cache); count != 0 {
		t.Fatalf("count after failed load: %d, want 0", count)
	}
}

// TestCompiledOwnership_CancellationPreserved verifies that runtime cancellation
// functions unhindered when compilation cache and module tracking are active.
func TestCompiledOwnership_CancellationPreserved(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	loopWat, err := wat.Compile(`(module (func (export "run") (loop br 0)))`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	cfg := &Config{
		CompilationCache:   cache,
		CloseOnContextDone: true,
	}

	eng, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, loopWat)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	fn := inst.GetExportedFunction("run")
	_, err = fn.Call(callCtx)
	if err == nil {
		t.Fatal("expected cancellation error from infinite loop")
	}

	// Engine Close must proceed cleanly without locking or hanging
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("eng.Close: %v", err)
	}
}
