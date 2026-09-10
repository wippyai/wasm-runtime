package engine

import (
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

// Compiled FunctionDefinitions are shared across instances. Definition identity
// alone cannot prove which heap an allocator function is bound to.
func TestAllocatorWithoutProvenanceDoesNotInferSiblingOwner(t *testing.T) {
	ctx := t.Context()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	code, err := wat.Compile(`(module
		(memory (export "memory") 1)
		(global $freed (export "freed") (mut i32) (i32.const 0))
		(func $internal (export "custom_alloc") (param i32 i32 i32 i32) (result i32) i32.const 1024)
		(func (export "cabi_free") (param i32)
			(global.set $freed (i32.add (global.get $freed) (i32.const 1))))
		(func (export "get_freed") (result i32) (global.get $freed)))`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := rt.CompileModule(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	left, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("left"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("right"))
	if err != nil {
		t.Fatal(err)
	}
	if left.ExportedFunction("custom_alloc").Definition() != right.ExportedFunction("custom_alloc").Definition() {
		t.Fatal("fixture must share compiled function definitions")
	}
	if left.ExportedFunction("cabi_free") == nil || right.ExportedFunction("cabi_free") == nil {
		t.Fatal("fixture must export cabi_free on both instances so test is not vacuous")
	}
	unproven := (&WazeroInstance{instance: left}).getOrCreateAllocatorLocked(right.ExportedFunction("custom_alloc"), nil)
	if unproven.freeFn != nil {
		t.Fatal("allocator without provenance acquired a free function")
	}
	unproven.setContext(ctx)
	unproven.Free(1024, 0, 0)
	leftFreed, err := left.ExportedFunction("get_freed").Call(ctx)
	if err != nil || leftFreed[0] != 0 {
		t.Fatalf("left instance was mutated: %v, %v", leftFreed, err)
	}
	rightFreed, err := right.ExportedFunction("get_freed").Call(ctx)
	if err != nil || rightFreed[0] != 0 {
		t.Fatalf("right instance was mutated: %v, %v", rightFreed, err)
	}

	// Verify interpreter engine also maintains instance isolation
	rtInterp := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer rtInterp.Close(ctx)
	compiledInterp, err := rtInterp.CompileModule(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	leftInterp, err := rtInterp.InstantiateModule(ctx, compiledInterp, wazero.NewModuleConfig().WithName("left"))
	if err != nil {
		t.Fatal(err)
	}
	rightInterp, err := rtInterp.InstantiateModule(ctx, compiledInterp, wazero.NewModuleConfig().WithName("right"))
	if err != nil {
		t.Fatal(err)
	}
	if leftInterp.ExportedFunction("cabi_free") == nil || rightInterp.ExportedFunction("cabi_free") == nil {
		t.Fatal("interpreter fixture must export cabi_free on both instances so test is not vacuous")
	}
	unprovenInterp := (&WazeroInstance{instance: leftInterp}).getOrCreateAllocatorLocked(rightInterp.ExportedFunction("custom_alloc"), nil)
	if unprovenInterp.freeFn != nil {
		t.Fatal("interpreter allocator without provenance acquired a free function")
	}
	unprovenInterp.setContext(ctx)
	unprovenInterp.Free(1024, 0, 0)
	leftInterpFreed, err := leftInterp.ExportedFunction("get_freed").Call(ctx)
	if err != nil || leftInterpFreed[0] != 0 {
		t.Fatalf("interpreter left instance was mutated: %v, %v", leftInterpFreed, err)
	}
	rightInterpFreed, err := rightInterp.ExportedFunction("get_freed").Call(ctx)
	if err != nil || rightInterpFreed[0] != 0 {
		t.Fatalf("interpreter right instance was mutated: %v, %v", rightInterpFreed, err)
	}
}

const sameCoreInstantiatedTwiceWAT = `(component
  (core module $shared_core
    (memory (export "memory") 1)
    (global $freed (export "freed") (mut i32) (i32.const 0))
    (global $heap (export "heap") (mut i32) (i32.const 1024))
    (func $realloc (export "cabi_realloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      (local.set $ret (global.get $heap))
      (global.set $heap (i32.add (local.get $ret) (local.get $new_size)))
      (local.get $ret)
    )
    (func (export "cabi_free") (param $ptr i32)
      (global.set $freed (i32.add (global.get $freed) (i32.const 1)))
    )
    (func (export "run") (param $ptr i32) (param $len i32) (result i32)
      (local.get $len)
    )
    (func (export "get_freed") (result i32) (global.get $freed))
  )

  (core instance $inst0 (instantiate $shared_core))
  (core instance $inst1 (instantiate $shared_core))

  (alias core export $inst0 "memory" (core memory $mem0))
  (alias core export $inst0 "cabi_realloc" (core func $realloc0))
  (alias core export $inst0 "get_freed" (core func $get_freed0))

  (alias core export $inst1 "memory" (core memory $mem1))
  (alias core export $inst1 "cabi_realloc" (core func $realloc1))
  (alias core export $inst1 "run" (core func $run1))
  (alias core export $inst1 "get_freed" (core func $get_freed1))

  ;; Export run_on_inst1_alloc_on_inst0: export function is on inst1, canonical allocator/memory is on inst0
  (type $run_func_type (func (param "s" string) (result u32)))
  (func $lift_run (type $run_func_type)
    (canon lift (core func $run1) (memory $mem0) (realloc $realloc0))
  )
  (export "run" (func $lift_run))

  ;; Export run_on_inst0_alloc_on_inst1: inverse case
  (alias core export $inst0 "run" (core func $run0))
  (func $lift_run_rev (type $run_func_type)
    (canon lift (core func $run0) (memory $mem1) (realloc $realloc1))
  )
  (export "run_rev" (func $lift_run_rev))

  (type $scalar_type (func (result u32)))
  (func $lift_freed0 (type $scalar_type) (canon lift (core func $get_freed0)))
  (export "get_inst0_freed" (func $lift_freed0))

  (func $lift_freed1 (type $scalar_type) (canon lift (core func $get_freed1)))
  (export "get_inst1_freed" (func $lift_freed1))
)
`

// TestSameCompiledCoreInstantiatedTwice_CanonicalAllocatorSiblingRegression is a REAL regression test
// where the same compiled core module is instantiated twice in a component:
//   - Instance 0 provides the canonical memory and realloc
//   - Instance 1 provides the exported function to be lifted
//
// Both instances share identical FunctionDefinitions, module bytecode, and export names.
// Canonical provenance via CanonOptRealloc.Index -> CoreFuncIndexSpace alias instance guarantees
// that allocator ownership is correctly bound to Instance 0, preventing foreign heap mutation.
func TestSameCompiledCoreInstantiatedTwice_CanonicalAllocatorSiblingRegression(t *testing.T) {
	ctx := t.Context()
	compBytes := compileComponentWasm(t, sameCoreInstantiatedTwiceWAT)

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

	wazInst := inst
	core0Mod := wazInst.linkerInst.GetModule(0)
	core1Mod := wazInst.linkerInst.GetModule(1)
	if core0Mod == nil || core1Mod == nil {
		t.Fatal("expected linker modules for both instances of the same compiled core")
	}

	// Verify that compiled FunctionDefinitions are indeed shared across sibling instances
	def0 := core0Mod.ExportedFunction("cabi_realloc").Definition()
	def1 := core1Mod.ExportedFunction("cabi_realloc").Definition()
	if def0 != def1 {
		t.Fatal("precondition failed: definitions must be shared for sibling instances")
	}

	// 1. Invoke "run": lowers string into inst0's memory using inst0's allocator,
	// runs export on inst1. Under Canonical ABI, ownership is transferred to the guest callee;
	// since the guest retains the string, host deallocation must NOT occur.
	res, err := inst.CallWithLift(ctx, "run", "testing-sibling-provenance-allocator")
	if err != nil {
		t.Fatalf("CallWithLift run: %v", err)
	}
	if res.(uint32) != uint32(len("testing-sibling-provenance-allocator")) {
		t.Fatalf("unexpected run result: %v", res)
	}

	// Assert guest retained ownership: neither inst0 nor inst1 free counters increment
	freed0, err := inst.CallWithLift(ctx, "get_inst0_freed")
	if err != nil {
		t.Fatalf("get_inst0_freed: %v", err)
	}
	freed1, err := inst.CallWithLift(ctx, "get_inst1_freed")
	if err != nil {
		t.Fatalf("get_inst1_freed: %v", err)
	}

	if freed0.(uint32) != 0 {
		t.Fatalf("expected inst0_freed = 0 (guest retained ownership), got %v", freed0)
	}
	if freed1.(uint32) != 0 {
		t.Fatalf("FOREIGN HEAP MUTATION! inst1_freed = %v, expected 0", freed1)
	}

	// 2. Verify export binding allocator and freeFn
	b, err := wazInst.getExportBinding("run")
	if err != nil {
		t.Fatalf("getExportBinding(run): %v", err)
	}
	if b.alloc == nil || b.alloc.freeFn == nil {
		t.Fatal("expected non-nil allocator and freeFn on binding for run")
	}

	// The linker records the exact allocator instance; calls below prove the
	// corresponding free counter changes without touching its sibling.
	exp, ok := inst.linkerInst.GetExport("run")
	if !ok || exp.Canon.ReallocMod != core0Mod {
		t.Fatal("wrong canonical allocator provenance")
	}

	// 3. Exercise manual Free via the allocator and verify counter increments on inst0 only
	b.alloc.setContext(ctx)
	b.alloc.Free(1024, 0, 0)

	freed0After, err := inst.CallWithLift(ctx, "get_inst0_freed")
	if err != nil {
		t.Fatalf("get_inst0_freed after manual Free: %v", err)
	}
	freed1After, err := inst.CallWithLift(ctx, "get_inst1_freed")
	if err != nil {
		t.Fatalf("get_inst1_freed after manual Free: %v", err)
	}

	if freed0After.(uint32) != 1 {
		t.Fatalf("expected inst0_freed = 1 after manual Free, got %v", freed0After)
	}
	if freed1After.(uint32) != 0 {
		t.Fatalf("FOREIGN HEAP MUTATION! inst1_freed = %v after manual Free, expected 0", freed1After)
	}

	// 4. Test inverse case: run_rev (export on inst0, allocator on inst1)
	resRev, err := inst.CallWithLift(ctx, "run_rev", "testing-reverse-sibling-provenance")
	if err != nil {
		t.Fatalf("CallWithLift run_rev: %v", err)
	}
	if resRev.(uint32) != uint32(len("testing-reverse-sibling-provenance")) {
		t.Fatalf("unexpected run_rev result: %v", resRev)
	}

	freed0Rev, err := inst.CallWithLift(ctx, "get_inst0_freed")
	if err != nil {
		t.Fatalf("get_inst0_freed after run_rev: %v", err)
	}
	freed1Rev, err := inst.CallWithLift(ctx, "get_inst1_freed")
	if err != nil {
		t.Fatalf("get_inst1_freed after run_rev: %v", err)
	}

	// Under canonical ownership, guest retains string: inst0 remains 1, inst1 remains 0
	if freed0Rev.(uint32) != 1 {
		t.Fatalf("expected inst0_freed = 1 after run_rev, got %v", freed0Rev)
	}
	if freed1Rev.(uint32) != 0 {
		t.Fatalf("expected inst1_freed = 0 after run_rev (guest retained), got %v", freed1Rev)
	}

	// Exercise manual Free via run_rev's allocator (inst1) to verify counter increments on inst1 only
	bRev, err := wazInst.getExportBinding("run_rev")
	if err != nil {
		t.Fatalf("getExportBinding(run_rev): %v", err)
	}
	if bRev.alloc == nil || bRev.alloc.freeFn == nil {
		t.Fatal("expected non-nil allocator and freeFn on binding for run_rev")
	}
	bRev.alloc.setContext(ctx)
	bRev.alloc.Free(1024, 0, 0)

	freed0RevAfter, err := inst.CallWithLift(ctx, "get_inst0_freed")
	if err != nil {
		t.Fatalf("get_inst0_freed after run_rev manual Free: %v", err)
	}
	freed1RevAfter, err := inst.CallWithLift(ctx, "get_inst1_freed")
	if err != nil {
		t.Fatalf("get_inst1_freed after run_rev manual Free: %v", err)
	}

	if freed0RevAfter.(uint32) != 1 {
		t.Fatalf("expected inst0_freed = 1 after run_rev manual Free, got %v", freed0RevAfter)
	}
	if freed1RevAfter.(uint32) != 1 {
		t.Fatalf("expected inst1_freed = 1 after run_rev manual Free, got %v", freed1RevAfter)
	}
}
