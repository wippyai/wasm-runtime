package engine

import (
	"testing"
)

// reexportedAllocatorComponentWAT constructs a component with two core modules:
//
// 1. $alloc_source:
//   - Exports memory, cabi_realloc, cabi_free, get_freed_a.
//   - Tracks freed_a count when its cabi_free is invoked.
//
// 2. $reexporter:
//   - Imports cabi_realloc and cabi_free from "alloc_env".
//   - Re-exports the imported allocator as "reexported_alloc".
//   - Re-exports the imported free as "reexported_free".
//   - Exports its OWN local cabi_free that increments freed_b count.
//   - Exports "run" which takes a string and returns its length.
//   - Exports get_freed_b.
//
// The component canon lift wires "run" using $reexporter's memory and "reexported_alloc".
// Because "reexported_alloc" is an imported function on $reexporter, its direct provenance
// is unproven without reflection => ReallocMod must be nil, and freeFn must be nil.
// Under NO circumstances should $reexporter's local cabi_free (or $alloc_source's foreign free)
// be paired with the allocator.
const reexportedAllocatorComponentWAT = `(component
  (core module $alloc_source
    (memory (export "memory") 1)
    (global $freed_a (export "freed_a") (mut i32) (i32.const 0))
    (global $heap_a (export "heap_a") (mut i32) (i32.const 1024))
    (func $realloc (export "cabi_realloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      (local.set $ret (global.get $heap_a))
      (global.set $heap_a (i32.add (local.get $ret) (local.get $new_size)))
      (local.get $ret)
    )
    (func $free (export "cabi_free") (param $ptr i32)
      (global.set $freed_a (i32.add (global.get $freed_a) (i32.const 1)))
    )
    (func (export "get_freed_a") (result i32) (global.get $freed_a))
  )

  (core module $reexporter
    (import "alloc_env" "cabi_realloc" (func $imp_alloc (param i32 i32 i32 i32) (result i32)))
    (import "alloc_env" "cabi_free" (func $imp_free (param i32)))
    (memory (export "memory") 1)
    (global $freed_b (export "freed_b") (mut i32) (i32.const 0))
    ;; Re-export imported allocator:
    (export "reexported_alloc" (func $imp_alloc))
    ;; Re-export imported free:
    (export "reexported_free" (func $imp_free))
    ;; Module B's OWN local free function that mutates Module B's heap counter:
    (func $local_free (export "cabi_free") (param $ptr i32)
      (global.set $freed_b (i32.add (global.get $freed_b) (i32.const 1)))
    )
    (func (export "run") (param $ptr i32) (param $len i32) (result i32)
      (local.get $len)
    )
    (func (export "get_freed_b") (result i32) (global.get $freed_b))
  )

  (core instance $inst_a (instantiate $alloc_source))
  (core instance $inst_b (instantiate $reexporter
    (with "alloc_env" (instance $inst_a))
  ))

  (alias core export $inst_a "get_freed_a" (core func $get_freed_a))

  (alias core export $inst_b "memory" (core memory $mem_b))
  (alias core export $inst_b "reexported_alloc" (core func $re_alloc))
  (alias core export $inst_b "reexported_free" (core func $re_free))
  (alias core export $inst_b "run" (core func $run_b))
  (alias core export $inst_b "get_freed_b" (core func $get_freed_b))

  (type $run_type (func (param "s" string) (result u32)))
  (func $lift_run (type $run_type)
    (canon lift (core func $run_b) (memory $mem_b) (realloc $re_alloc))
  )
  (export "run" (func $lift_run))

  (type $scalar_type (func (result u32)))
  (func $lift_freed_a (type $scalar_type) (canon lift (core func $get_freed_a)))
  (export "get_freed_a" (func $lift_freed_a))

  (func $lift_freed_b (type $scalar_type) (canon lift (core func $get_freed_b)))
  (export "get_freed_b" (func $lift_freed_b))
)
`

func testReexportedAllocatorProvenance(t *testing.T, useInterpreter bool) {
	t.Helper()
	ctx := t.Context()
	compBytes := compileComponentWasm(t, reexportedAllocatorComponentWAT)

	cfg := &Config{UseInterpreter: useInterpreter}
	eng, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig: %v", err)
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
	instAMod := wazInst.linkerInst.GetModule(0)
	instBMod := wazInst.linkerInst.GetModule(1)
	if instAMod == nil || instBMod == nil {
		t.Fatal("expected linker modules for inst_a and inst_b")
	}

	// 1. Verify linker canonical export provenance:
	// reexported_alloc is imported on instB, so owner.ExportedFunctionDefinitions()[entry.ExportName].Import must be true.
	// Therefore ReallocMod must be nil.
	exp, ok := wazInst.linkerInst.GetExport("run")
	if !ok {
		t.Fatal("expected export run")
	}
	if exp.Canon == nil {
		t.Fatal("expected canon export for run")
	}
	if exp.Canon.Realloc == nil {
		t.Fatal("expected realloc function on canon export")
	}
	if exp.Canon.ReallocMod != nil {
		t.Fatalf("PROVENANCE ERROR: ReallocMod must be nil for re-exported imported allocator, got %v", exp.Canon.ReallocMod)
	}

	// 2. Verify engine export binding:
	// Allocator must exist, but freeFn must be nil because ReallocMod was nil.
	b, err := wazInst.getExportBinding("run")
	if err != nil {
		t.Fatalf("getExportBinding(run): %v", err)
	}
	if b.alloc == nil {
		t.Fatal("expected non-nil allocator for run")
	}
	if b.alloc.freeFn != nil {
		t.Fatalf("SAFETY VIOLATION: re-exported allocator was paired with free function: %v", b.alloc.freeFn)
	}

	// 3. Verify localFunctionOwner refuses re-exported free on instB
	if owner := localFunctionOwner(instBMod, "reexported_free"); owner != nil {
		t.Fatalf("SAFETY VIOLATION: localFunctionOwner accepted re-exported free: %v", owner)
	}
	// instB does have its own local cabi_free, but it must NOT have been paired with the allocator
	if owner := localFunctionOwner(instBMod, CabiFree); owner == nil {
		t.Fatal("expected instB local cabi_free to have local provenance on instB")
	}

	// 4. Invoke "run" with string parameter:
	// Parameter lowering uses exp.Canon.Realloc.
	// Post-call lowering cleanup must NOT call instB's cabi_free (which would mutate freed_b).
	inputStr := "testing-reexported-allocator-provenance-soundness"
	res, err := inst.CallWithLift(ctx, "run", inputStr)
	if err != nil {
		t.Fatalf("CallWithLift run: %v", err)
	}
	if res.(uint32) != uint32(len(inputStr)) {
		t.Fatalf("unexpected run result: %v", res)
	}

	// Assert guest counters:
	// freed_b must be 0 (foreign heap is NOT mutated!)
	// freed_a must be 0 (unproven allocator has no host free)
	freedA, err := inst.CallWithLift(ctx, "get_freed_a")
	if err != nil {
		t.Fatalf("get_freed_a: %v", err)
	}
	freedB, err := inst.CallWithLift(ctx, "get_freed_b")
	if err != nil {
		t.Fatalf("get_freed_b: %v", err)
	}

	if freedB.(uint32) != 0 {
		t.Fatalf("FOREIGN HEAP MUTATION! inst_b freed_b = %v, expected 0 (exporting module free was erroneously invoked)", freedB)
	}
	if freedA.(uint32) != 0 {
		t.Fatalf("inst_a freed_a = %v, expected 0 (unproven allocator should not trigger host-side free)", freedA)
	}

	// 5. Test direct manual Free on allocator
	b.alloc.setContext(ctx)
	b.alloc.Free(1024, 0, 0)

	freedAAfter, err := inst.CallWithLift(ctx, "get_freed_a")
	if err != nil {
		t.Fatalf("get_freed_a after manual Free: %v", err)
	}
	freedBAfter, err := inst.CallWithLift(ctx, "get_freed_b")
	if err != nil {
		t.Fatalf("get_freed_b after manual Free: %v", err)
	}

	if freedBAfter.(uint32) != 0 {
		t.Fatalf("FOREIGN HEAP MUTATION after manual Free! freed_b = %v, expected 0", freedBAfter)
	}
	if freedAAfter.(uint32) != 0 {
		t.Fatalf("freed_a after manual Free = %v, expected 0", freedAAfter)
	}
}

// TestReexportedAllocator_CompilerEngine verifies imported/re-exported allocator
// isolation and foreign heap protection under the Wazero compiler engine.
func TestReexportedAllocator_CompilerEngine(t *testing.T) {
	testReexportedAllocatorProvenance(t, false)
}

// TestReexportedAllocator_InterpreterEngine verifies imported/re-exported allocator
// isolation and foreign heap protection under the Wazero interpreter engine.
func TestReexportedAllocator_InterpreterEngine(t *testing.T) {
	testReexportedAllocatorProvenance(t, true)
}

// TestDefaultAllocatorFallback_NoForeignMemMismatch verifies that default allocator
// fallback during instance creation never pairs memory from one module with an allocator
// from a different module.
func TestDefaultAllocatorFallback_NoForeignMemMismatch(t *testing.T) {
	ctx := t.Context()
	// Core module 0: exports memory only, no allocator
	// Core module 1: exports allocator only, has its own separate memory
	watStr := `(component
		(core module $mod0
			(memory (export "memory") 1)
			(func (export "fn0") (result i32) i32.const 10)
		)
		(core module $mod1
			(memory (export "memory") 1)
			(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) i32.const 2048)
			(func (export "cabi_free") (param i32))
		)
		(core instance $inst0 (instantiate $mod0))
		(core instance $inst1 (instantiate $mod1))
		(alias core export $inst0 "fn0" (core func $fn0))
		(type $fn0_type (func (result u32)))
		(func $lift0 (type $fn0_type) (canon lift (core func $fn0)))
		(export "fn0" (func $lift0))
	)`

	compBytes := compileComponentWasm(t, watStr)
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

	// Since canon lift for fn0 has no memory/realloc, default memory is nil or from mod0.
	// Default allocator must NOT pick mod1's cabi_realloc if paired with mod0's memory!
	if inst.memory != nil && inst.alloc != nil {
		// If both are set, they MUST come from the same module
		mod0 := inst.linkerInst.GetModule(0)
		mod1 := inst.linkerInst.GetModule(1)
		if inst.memory.mem == mod0.Memory() && inst.allocFn == mod1.ExportedFunction("cabi_realloc") {
			t.Fatal("FOREIGN MEMORY MISMATCH! Default allocator was picked from mod1 while memory was picked from mod0!")
		}
	}
}

// reexportedFreeComponentWAT constructs a component where $proxy directly re-exports
// $source's imported cabi_free under the standard export name "cabi_free".
const reexportedFreeComponentWAT = `(component
  (core module $source
    (memory (export "memory") 1)
    (global $freed_src (export "freed_src") (mut i32) (i32.const 0))
    (func $free (export "cabi_free") (param $ptr i32)
      (global.set $freed_src (i32.add (global.get $freed_src) (i32.const 1)))
    )
    (func (export "get_freed_src") (result i32) (global.get $freed_src))
  )
  (core module $proxy
    (import "src" "cabi_free" (func $imp_free (param i32)))
    (memory (export "memory") 1)
    (export "cabi_free" (func $imp_free))
    (func $alloc (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) i32.const 1024)
  )
  (core instance $inst_src (instantiate $source))
  (core instance $inst_proxy (instantiate $proxy (with "src" (instance $inst_src))))
  (alias core export $inst_src "get_freed_src" (core func $get_freed_src))
  (type $scalar_type (func (result u32)))
  (func $lift_freed (type $scalar_type) (canon lift (core func $get_freed_src)))
  (export "get_freed_src" (func $lift_freed))
)
`

func testReexportedFreeFunctionRefusal(t *testing.T, useInterpreter bool) {
	t.Helper()
	ctx := t.Context()
	compBytes := compileComponentWasm(t, reexportedFreeComponentWAT)

	cfg := &Config{UseInterpreter: useInterpreter}
	eng, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig: %v", err)
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

	proxyMod := inst.linkerInst.GetModule(1)
	if proxyMod == nil {
		t.Fatal("expected proxy module at index 1")
	}

	// proxyMod exports "cabi_free", but it is an imported function!
	// localFunctionOwner MUST refuse it.
	owner := localFunctionOwner(proxyMod, CabiFree)
	if owner != nil {
		t.Fatalf("SAFETY VIOLATION: localFunctionOwner accepted re-exported imported free: %v", owner)
	}

	freeFn := localFreeFunction(proxyMod)
	if freeFn != nil {
		t.Fatalf("SAFETY VIOLATION: localFreeFunction returned re-exported imported free: %v", freeFn)
	}

	// Even if an allocator is bound with proxyMod as allocMod, freeFn must remain nil.
	alloc := inst.getOrCreateAllocatorLocked(proxyMod.ExportedFunction("cabi_realloc"), proxyMod)
	if alloc == nil {
		t.Fatal("expected allocator to be created")
	}
	if alloc.freeFn != nil {
		t.Fatalf("SAFETY VIOLATION: allocator acquired re-exported free function: %v", alloc.freeFn)
	}

	// Manual Free must not invoke $source's free function
	alloc.setContext(ctx)
	alloc.Free(1024, 0, 0)

	freedSrc, err := inst.CallWithLift(ctx, "get_freed_src")
	if err != nil {
		t.Fatalf("get_freed_src: %v", err)
	}
	if freedSrc.(uint32) != 0 {
		t.Fatalf("FOREIGN HEAP MUTATION! get_freed_src = %v, expected 0 (re-exported free mutated source module)", freedSrc)
	}
}

// TestReexportedFree_RefusalAndHeapProtection_Compiler verifies that re-exported
// imported free functions are refused under the compiler engine.
func TestReexportedFree_RefusalAndHeapProtection_Compiler(t *testing.T) {
	testReexportedFreeFunctionRefusal(t, false)
}

// TestReexportedFree_RefusalAndHeapProtection_Interpreter verifies that re-exported
// imported free functions are refused under the interpreter engine.
func TestReexportedFree_RefusalAndHeapProtection_Interpreter(t *testing.T) {
	testReexportedFreeFunctionRefusal(t, true)
}
