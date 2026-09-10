package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/wippyai/wasm-runtime/wat"
)

// TestLateEnableAsyncify_StaleExportBindingsAndSuspendResume verifies that:
// 1. Legitimate synchronous calls before EnableAsyncify cache export bindings without scheduler/asyncify.
// 2. Late EnableAsyncify consistently rebinds cached export bindings with the newly initialized asyncify/scheduler.
// 3. Reconfiguration with an active suspended session is rejected.
// 4. Real suspend, resume, and LiftResult complete successfully through the refreshed binding.
// 5. Failed asyncify Init does not publish partial or corrupted state.
func TestLateEnableAsyncify_StaleExportBindingsAndSuspendResume(t *testing.T) {
	eng, mod := loadTwoCoreYieldModule(t)
	defer eng.Close(context.Background())

	ctx := context.Background()

	// Instantiate WITHOUT EnableAsyncify
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify:  false,
		AsyncifyImports: []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	// 1. Legitimate synchronous calls before EnableAsyncify
	h1, err := inst.CallWithLift(ctx, "get-heap1")
	if err != nil {
		t.Fatalf("get-heap1: %v", err)
	}
	if h1.(uint32) != 70000 {
		t.Fatalf("unexpected heap1: %v", h1)
	}

	p1, err := inst.CallWithLift(ctx, "get-post1")
	if err != nil {
		t.Fatalf("get-post1: %v", err)
	}
	if p1.(uint32) != 0 {
		t.Fatalf("unexpected post1: %v", p1)
	}

	// Verify that the cached binding for get-heap1 has nil asyncify/scheduler
	bSync, err := inst.getExportBinding("get-heap1")
	if err != nil {
		t.Fatalf("getExportBinding(get-heap1): %v", err)
	}
	if bSync.scheduler != nil || bSync.asyncify != nil {
		t.Fatalf("expected nil asyncify/scheduler on synchronous binding, got sched=%v async=%v", bSync.scheduler, bSync.asyncify)
	}

	// Also prime binding for func1 synchronously
	bFunc1Before, err := inst.getExportBinding("func1")
	if err != nil {
		t.Fatalf("getExportBinding(func1): %v", err)
	}
	if bFunc1Before.scheduler != nil || bFunc1Before.asyncify != nil {
		t.Fatalf("expected nil asyncify/scheduler on func1 before EnableAsyncify, got sched=%v", bFunc1Before.scheduler)
	}

	// 2. Perform Late EnableAsyncify
	if err := inst.EnableAsyncify(AsyncifyConfig{StackSize: 2048, DataAddr: 16}); err != nil {
		t.Fatalf("Late EnableAsyncify: %v", err)
	}

	if !inst.asyncifyEnabled {
		t.Fatal("expected asyncifyEnabled == true after EnableAsyncify")
	}
	if inst.asyncify == nil || inst.scheduler == nil {
		t.Fatal("expected non-nil instance asyncify and scheduler after EnableAsyncify")
	}

	// Verify cached bindings were refreshed consistently
	bFunc1After, err := inst.getExportBinding("func1")
	if err != nil {
		t.Fatalf("getExportBinding(func1) after EnableAsyncify: %v", err)
	}
	if bFunc1After.scheduler == nil || bFunc1After.asyncify == nil {
		t.Fatal("late EnableAsyncify failed to update cached export binding for func1: scheduler/asyncify is nil")
	}

	// 3. Real suspend / resume regression with the refreshed binding
	cs1, err := inst.StartCall(ctx, "func1", "hello-late-asyncify")
	if err != nil {
		t.Fatalf("StartCall func1 after late EnableAsyncify: %v", err)
	}

	// Step to yield
	step1, err := cs1.Step(ctx, nil)
	if err != nil {
		t.Fatalf("cs1.Step initial: %v", err)
	}
	if step1.Status != StepContinue {
		t.Fatalf("cs1.Step status = %v, want StepContinue", step1.Status)
	}
	if step1.PendingOp == nil || step1.PendingOp.CmdID() != 1 {
		t.Fatalf("expected PendingOp with CmdID 1, got %v", step1.PendingOp)
	}

	// 4. While suspended: verify reconfiguration is REJECTED
	t.Run("ReconfigureWhileSuspendedRejected", func(t *testing.T) {
		err := inst.EnableAsyncify(AsyncifyConfig{StackSize: 4096, DataAddr: 16})
		if err == nil {
			t.Fatal("expected EnableAsyncify to be rejected while call session is suspended, got nil")
		}
		if !strings.Contains(err.Error(), "active suspended session") && !strings.Contains(err.Error(), "cannot enable asyncify") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	// Resume execution to completion
	resumeVal := &YieldResult{Value: 42}
	step2, err := cs1.Step(ctx, resumeVal)
	if err != nil {
		t.Fatalf("cs1.Step resume: %v", err)
	}
	if step2.Status != StepDone {
		t.Fatalf("cs1.Step resume status = %v, want StepDone", step2.Status)
	}

	// Lift results
	res, err := cs1.LiftResult(ctx, step2.Results)
	if err != nil {
		t.Fatalf("cs1.LiftResult: %v", err)
	}
	if res != "hello-late-asyncify" {
		t.Fatalf("expected result 'hello-late-asyncify', got %v", res)
	}

	// 5. After session completes and lifts, reconfiguration is allowed again
	if err := inst.EnableAsyncify(AsyncifyConfig{StackSize: 2048, DataAddr: 16}); err != nil {
		t.Fatalf("EnableAsyncify after completed session failed: %v", err)
	}
}

// TestEnableAsyncify_PublishOnlyAfterSuccess verifies that calling EnableAsyncify
// on a module without asyncify exports fails and does NOT corrupt or publish state.
func TestEnableAsyncify_PublishOnlyAfterSuccess(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	// WAT without asyncify
	plainWat := `(module
		(memory (export "memory") 1)
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
	)`
	wasmBytes, err := wat.Compile(plainWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call export synchronously
	bBefore, err := inst.getExportBinding("add")
	if err != nil {
		t.Fatalf("getExportBinding: %v", err)
	}
	if bBefore.scheduler != nil || bBefore.asyncify != nil {
		t.Fatal("expected nil asyncify before EnableAsyncify")
	}

	// Calling EnableAsyncify should fail because asyncify exports are absent
	err = inst.EnableAsyncify(AsyncifyConfig{StackSize: 1024, DataAddr: 16})
	if err == nil {
		t.Fatal("expected EnableAsyncify to fail on non-asyncify module")
	}

	// State must NOT be published
	if inst.asyncifyEnabled {
		t.Fatal("asyncifyEnabled should remain false after failed init")
	}
	if inst.asyncify != nil {
		t.Fatal("asyncify runtime should remain nil after failed init")
	}
	if inst.scheduler != nil {
		t.Fatal("scheduler should remain nil after failed init")
	}

	// Binding cache must remain uncorrupted
	bAfter, err := inst.getExportBinding("add")
	if err != nil {
		t.Fatalf("getExportBinding after failed EnableAsyncify: %v", err)
	}
	if bAfter.scheduler != nil || bAfter.asyncify != nil {
		t.Fatal("binding asyncify/scheduler should remain nil after failed init")
	}
}

const twoCoreAliasComponentWAT = `(component
  (core module $core1
    (memory (export "memory") 1)
    (global $core1_freed (export "core1_freed") (mut i32) (i32.const 0))
    (global $core1_allocated (export "core1_allocated") (mut i32) (i32.const 0))
    (global $core1_heap (export "core1_heap") (mut i32) (i32.const 1024))
    (func $internal_allocator (export "custom_alloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      (local.set $ret (global.get $core1_heap))
      (global.set $core1_heap (i32.add (local.get $ret) (local.get $new_size)))
      (global.set $core1_allocated (i32.add (global.get $core1_allocated) (i32.const 1)))
      (local.get $ret)
    )
    (func $cabi_free (export "cabi_free") (param $ptr i32)
      (global.set $core1_freed (i32.add (global.get $core1_freed) (i32.const 1)))
    )
    (func (export "run") (param $ptr i32) (param $len i32) (result i32)
      (call $cabi_free (local.get $ptr))
      (local.get $len)
    )
    (func (export "get_core1_freed") (result i32) (global.get $core1_freed))
    (func (export "get_core1_allocated") (result i32) (global.get $core1_allocated))
  )

  (core module $core2
    (memory (export "memory") 1)
    (global $core2_freed (export "core2_freed") (mut i32) (i32.const 0))
    (func (export "cabi_free") (param $ptr i32)
      (global.set $core2_freed (i32.add (global.get $core2_freed) (i32.const 1)))
    )
    (func (export "get_core2_freed") (result i32) (global.get $core2_freed))
  )

  (core instance $inst1 (instantiate $core1))
  (core instance $inst2 (instantiate $core2))

  (alias core export $inst1 "memory" (core memory $mem1))
  (alias core export $inst1 "custom_alloc" (core func $realloc1))
  (alias core export $inst1 "run" (core func $run_core))
  (alias core export $inst1 "get_core1_freed" (core func $get_c1_freed))
  (alias core export $inst1 "get_core1_allocated" (core func $get_c1_allocated))

  (alias core export $inst2 "memory" (core memory $mem2))
  (alias core export $inst2 "get_core2_freed" (core func $get_c2_freed))

  (type $run_func_type (func (param "s" string) (result u32)))
  (func $lift_run (type $run_func_type)
    (canon lift (core func $run_core) (memory $mem1) (realloc $realloc1))
  )
  (export "run" (func $lift_run))

  (type $scalar_type (func (result u32)))
  (func $lift_c1_freed (type $scalar_type) (canon lift (core func $get_c1_freed)))
  (export "get_core1_freed" (func $lift_c1_freed))

  (func $lift_c1_allocated (type $scalar_type) (canon lift (core func $get_c1_allocated)))
  (export "get_core1_allocated" (func $lift_c1_allocated))

  (func $lift_c2_freed (type $scalar_type) (canon lift (core func $get_c2_freed)))
  (export "get_core2_freed" (func $lift_c2_freed))
)
`

func compileComponentWasm(t *testing.T, compWat string) []byte {
	t.Helper()
	return componentFixture(t, compWat)
}

// TestAllocatorOwnership_AliasAndTwoCoreCase verifies that:
//  1. When realloc is defined with an internal name different from its export alias ("custom_alloc" vs $internal_allocator),
//     getOrCreateAllocatorLocked successfully resolves ownership to core1 via exports identity.
//  2. The selected freeFn belongs strictly to core1 and NOT coreMod (core2).
//  3. Invoking free through the allocator releases into core1 and never core2.
//  4. When allocator ownership cannot be verified, freeFn is left nil.
func TestAllocatorOwnership_AliasAndTwoCoreCase(t *testing.T) {
	ctx := context.Background()
	compBytes := compileComponentWasm(t, twoCoreAliasComponentWAT)

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, compBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EntryExport: "run"})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call "run" export: this lowers a string into core1 using core1's custom_alloc
	res, err := inst.CallWithLift(ctx, "run", "testing-allocator-ownership")
	if err != nil {
		t.Fatalf("CallWithLift run: %v", err)
	}
	if res.(uint32) != uint32(len("testing-allocator-ownership")) {
		t.Fatalf("unexpected run result: %v", res)
	}

	// Verify the export binding for "run"
	b, err := inst.getExportBinding("run")
	if err != nil {
		t.Fatalf("getExportBinding(run): %v", err)
	}
	if b.alloc == nil {
		t.Fatal("expected non-nil allocator for run")
	}

	// Verify allocator ownership:
	// reallocFn is custom_alloc from core1 ($internal_allocator)
	// coreMod is core2 ($core2)
	// freeFn must belong to core1
	if b.alloc.freeFn == nil {
		t.Fatal("expected verified freeFn on allocator, got nil")
	}

	// Check that freeFn definition belongs to core1 and NOT core2
	freeModName := b.alloc.freeFn.Definition().ModuleName()
	core1Mod := inst.linkerInst.GetModule(0)
	core2Mod := inst.linkerInst.GetModule(1)
	if core1Mod == nil || core2Mod == nil {
		t.Fatal("expected linker modules for core1 and core2")
	}

	// Verify freeFn identity matches core1's cabi_free
	if core1Mod.ExportedFunction("cabi_free").Definition() != b.alloc.freeFn.Definition() {
		t.Fatalf("allocator freeFn is not core1's cabi_free! got module=%s", freeModName)
	}
	if core2Mod.ExportedFunction("cabi_free").Definition() == b.alloc.freeFn.Definition() {
		t.Fatal("allocator freeFn erroneously bound to core2's cabi_free!")
	}

	// Verify that guest parameter consumption freed the string into core1 (never core2)
	c1FreedAfterCall, err := inst.CallWithLift(ctx, "get_core1_freed")
	if err != nil {
		t.Fatalf("get_core1_freed: %v", err)
	}
	if c1FreedAfterCall.(uint32) != 1 {
		t.Fatalf("expected core1_freed after CallWithLift = 1, got %v", c1FreedAfterCall)
	}

	c2FreedAfterCall, err := inst.CallWithLift(ctx, "get_core2_freed")
	if err != nil {
		t.Fatalf("get_core2_freed: %v", err)
	}
	if c2FreedAfterCall.(uint32) != 0 {
		t.Fatalf("core2 was corrupted by param lowering free! core2_freed = %v, want 0", c2FreedAfterCall)
	}

	// Exercise freeFn directly through the allocator and verify core1 counter increments again
	b.alloc.setContext(ctx)
	b.alloc.Free(1024, 0, 0)

	c1Freed, err := inst.CallWithLift(ctx, "get_core1_freed")
	if err != nil {
		t.Fatalf("get_core1_freed: %v", err)
	}
	if c1Freed.(uint32) != 2 {
		t.Fatalf("expected core1_freed after manual Free = 2, got %v", c1Freed)
	}

	c2Freed, err := inst.CallWithLift(ctx, "get_core2_freed")
	if err != nil {
		t.Fatalf("get_core2_freed: %v", err)
	}
	if c2Freed.(uint32) != 0 {
		t.Fatalf("core2 was corrupted by freeFn! core2_freed = %v, want 0", c2Freed)
	}

	// Test unverified owner: if realloc is not found in any module, freeFn must remain nil
	t.Run("UnverifiedOwnerLeavesFreeNil", func(t *testing.T) {
		dummyRealloc := core1Mod.ExportedFunction("custom_alloc")
		inst.bindingMu.Lock()
		allocVerified := inst.getOrCreateAllocatorLocked(dummyRealloc, core2Mod)
		inst.bindingMu.Unlock()
		if allocVerified == nil || allocVerified.freeFn == nil {
			t.Fatal("expected verified allocator for custom_alloc")
		}

		// When an export function has unverified owner (e.g. coreMod has no match and no linker match)
		standaloneModWat := `(module
			(memory (export "memory") 1)
			(func (export "foreign_alloc") (param i32 i32 i32 i32) (result i32) i32.const 0)
			(func (export "cabi_free") (param i32))
		)`
		standaloneWasm, err := wat.Compile(standaloneModWat)
		if err != nil {
			t.Fatalf("wat.Compile standalone: %v", err)
		}
		standaloneMod, err := eng.LoadModule(ctx, standaloneWasm)
		if err != nil {
			t.Fatalf("LoadModule standalone: %v", err)
		}
		standaloneInst, err := standaloneMod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("Instantiate standalone: %v", err)
		}
		defer standaloneInst.Close(ctx)

		foreignAlloc := standaloneInst.GetExportedFunction("foreign_alloc")

		inst.bindingMu.Lock()
		allocUnverified := inst.getOrCreateAllocatorLocked(foreignAlloc, nil)
		inst.bindingMu.Unlock()

		if allocUnverified == nil {
			t.Fatal("expected allocator to be created")
		}
		if allocUnverified.freeFn != nil {
			t.Fatalf("expected nil freeFn for unverified foreign realloc owner, got %v", allocUnverified.freeFn)
		}
	})
}

// TestResolveCanon_AbsentOptionsRemainAbsent verifies that when a component
// defines canon lift exports with omitted memory and realloc options:
// 1. Linker resolveCanon produces CanonExport with Memory == nil and Realloc == nil.
// 2. Engine getExportBinding resolves binding with memory == nil and alloc == nil.
// 3. Scalar calls succeed without linear memory.
// 4. Invocations requiring memory fail cleanly with "canonical memory not available".
// Note: per component model spec, a component function type taking/returning strings
// without a memory option is spec-invalid, even though the validator currently accepts it.
func TestResolveCanon_AbsentOptionsRemainAbsent(t *testing.T) {
	ctx := context.Background()
	compWat := `(component
  (core module $m
    (memory (export "memory") 1)
    (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) i32.const 1024)
    (func (export "add") (param i32 i32) (result i32)
      local.get 0
      local.get 1
      i32.add
    )
    (func (export "echo") (param i32 i32) (result i32)
      local.get 0
    )
  )
  (core instance $inst (instantiate $m))
  (alias core export $inst "memory" (core memory $mem))
  (alias core export $inst "cabi_realloc" (core func $realloc))
  (alias core export $inst "add" (core func $add_core))
  (alias core export $inst "echo" (core func $echo_core))

  ;; Export 1: scalar func, strictly NO memory and NO realloc option
  (type $add_type (func (param "a" u32) (param "b" u32) (result u32)))
  (func $lift_add (type $add_type) (canon lift (core func $add_core)))
  (export "add" (func $lift_add))

  ;; Export 2: string func, NO memory and NO realloc option (spec-invalid, validator-accepted)
  (type $echo_type (func (param "s" string) (result u32)))
  (func $lift_echo (type $echo_type) (canon lift (core func $echo_core)))
  (export "echo" (func $lift_echo))
)`

	compBytes := compileComponentWasm(t, compWat)

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, compBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// 1. Check linker export resolution
	addExp, ok := inst.linkerInst.GetExport("add")
	if !ok {
		t.Fatal("expected linker export 'add'")
	}
	if addExp.Canon == nil {
		t.Fatal("expected non-nil Canon on 'add'")
	}
	if addExp.Canon.Memory != nil {
		t.Errorf("expected linker addExp.Canon.Memory == nil when omitted, got %v", addExp.Canon.Memory)
	}
	if addExp.Canon.Realloc != nil {
		t.Errorf("expected linker addExp.Canon.Realloc == nil when omitted, got %v", addExp.Canon.Realloc)
	}

	echoExp, ok := inst.linkerInst.GetExport("echo")
	if !ok {
		t.Fatal("expected linker export 'echo'")
	}
	if echoExp.Canon == nil {
		t.Fatal("expected non-nil Canon on 'echo'")
	}
	if echoExp.Canon.Memory != nil {
		t.Errorf("expected linker echoExp.Canon.Memory == nil when omitted, got %v", echoExp.Canon.Memory)
	}
	if echoExp.Canon.Realloc != nil {
		t.Errorf("expected linker echoExp.Canon.Realloc == nil when omitted, got %v", echoExp.Canon.Realloc)
	}

	// 2. Check engine export bindings
	bAdd, err := inst.getExportBinding("add")
	if err != nil {
		t.Fatalf("getExportBinding(add): %v", err)
	}
	if bAdd.memory != nil {
		t.Errorf("expected bAdd.memory == nil, got %v", bAdd.memory)
	}
	if bAdd.alloc != nil {
		t.Errorf("expected bAdd.alloc == nil, got %v", bAdd.alloc)
	}

	bEcho, err := inst.getExportBinding("echo")
	if err != nil {
		t.Fatalf("getExportBinding(echo): %v", err)
	}
	if bEcho.memory != nil {
		t.Errorf("expected bEcho.memory == nil, got %v", bEcho.memory)
	}
	if bEcho.alloc != nil {
		t.Errorf("expected bEcho.alloc == nil, got %v", bEcho.alloc)
	}

	// 3. Scalar call succeeds without memory
	resAdd, err := inst.CallWithLift(ctx, "add", uint32(10), uint32(20))
	if err != nil {
		t.Fatalf("CallWithLift add: %v", err)
	}
	if resAdd.(uint32) != 30 {
		t.Fatalf("expected add result 30, got %v", resAdd)
	}

	// 4. String call fails cleanly because memory is absent
	_, err = inst.CallWithLift(ctx, "echo", "test")
	if err == nil {
		t.Fatal("expected CallWithLift echo to fail due to absent canonical memory, got nil")
	}
	if !strings.Contains(err.Error(), "canonical memory not available") && !strings.Contains(err.Error(), "canonical allocator not available") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestCanonicalIsolation_ConcurrentBindingsAndReconfiguration runs concurrent
// callers resolving bindings and invoking EnableAsyncify repeatedly under -race.
func TestCanonicalIsolation_ConcurrentBindingsAndReconfiguration(t *testing.T) {
	eng, mod := loadTwoCoreYieldModule(t)
	defer eng.Close(context.Background())

	ctx := context.Background()
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify:  false,
		AsyncifyImports: []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	const goroutines = 8
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(goroutines + 1)

	// Reconfiguration worker
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = inst.EnableAsyncify(AsyncifyConfig{StackSize: 2048, DataAddr: 16})
		}
	}()

	// Query/call workers
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch (id + i) % 3 {
				case 0:
					_, _ = inst.getExportBinding("get-heap1")
				case 1:
					_, _ = inst.getExportBinding("get-post1")
				case 2:
					_, _ = inst.getExportBinding("func1")
				}
			}
		}(g)
	}

	wg.Wait()
}
