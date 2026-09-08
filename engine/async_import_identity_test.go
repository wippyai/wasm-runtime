package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// directCallerOlderGuestWAT defines a component where a core module directly calls
// an imported async host function without any call_indirect instructions.
// Guest imports test:async/host@0.1.0, while the host registers test:async/host@0.1.2.
const directCallerOlderGuestWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $host_inst (export "yield" (func $yield_lowered)))

  (core module $m
    (import "test:async/host@0.1.0" "yield" (func $host_yield (param i32) (result i32)))
    ` + ownedAsyncifyTestMemory + `
    (global $before_count (export "before_count") (mut i32) (i32.const 0))
    (global $after_count (export "after_count") (mut i32) (i32.const 0))
    (global $token (export "token") (mut i32) (i32.const 0))

    (func (export "run") (param $val i32) (result i32)
      (local $res i32)
      ;; Direct caller: NO call_indirect anywhere in the module!
      (global.set $before_count (i32.add (global.get $before_count) (i32.const 1)))
      (local.set $res (call $host_yield (local.get $val)))
      (global.set $token (local.get $res))
      (global.set $after_count (i32.add (global.get $after_count) (i32.const 1)))
      (local.get $res)
    )
    (func (export "get_before") (result i32) (global.get $before_count))
    (func (export "get_after") (result i32) (global.get $after_count))
    (func (export "get_token") (result i32) (global.get $token))
  )

  (core instance $inst (instantiate $m (with "test:async/host@0.1.0" (instance $host_inst))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst "run" (core func $core_run))
  (alias core export $inst "get_before" (core func $core_get_before))
  (alias core export $inst "get_after" (core func $core_get_after))
  (alias core export $inst "get_token" (core func $core_get_token))
  (alias core export $inst "memory" (core memory $core_mem))

  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
  (func $lift_get_before (type $get_type) (canon lift (core func $core_get_before)))
  (export "get-before" (func $lift_get_before))
  (func $lift_get_after (type $get_type) (canon lift (core func $core_get_after)))
  (export "get-after" (func $lift_get_after))
  (func $lift_get_token (type $get_type) (canon lift (core func $core_get_token)))
  (export "get-token" (func $lift_get_token))
)
`

// multiversionGuestWAT defines a component where two core modules import different patch versions
// (0.1.0 and 0.1.1) of the same interface, served by a single host registration at 0.1.2.
const multiversionGuestWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host0 (type $host_iface)))
  (import "test:async/host@0.1.1" (instance $host1 (type $host_iface)))

  (alias export $host0 "yield" (func $host_yield0))
  (core func $yield_lowered0 (canon lower (func $host_yield0)))
  (core instance $host_inst0 (export "yield" (func $yield_lowered0)))

  (alias export $host1 "yield" (func $host_yield1))
  (core func $yield_lowered1 (canon lower (func $host_yield1)))
  (core instance $host_inst1 (export "yield" (func $yield_lowered1)))

  (core module $m0
    (import "test:async/host@0.1.0" "yield" (func $host_yield0 (param i32) (result i32)))
    ` + ownedAsyncifyTestMemory + `
    (global $before_count (export "before_count") (mut i32) (i32.const 0))
    (global $after_count (export "after_count") (mut i32) (i32.const 0))

    (func (export "run0") (param $val i32) (result i32)
      (local $res i32)
      (global.set $before_count (i32.add (global.get $before_count) (i32.const 1)))
      (local.set $res (call $host_yield0 (local.get $val)))
      (global.set $after_count (i32.add (global.get $after_count) (i32.const 1)))
      (local.get $res)
    )
    (func (export "get_before0") (result i32) (global.get $before_count))
    (func (export "get_after0") (result i32) (global.get $after_count))
  )

  (core module $m1
    (import "test:async/host@0.1.1" "yield" (func $host_yield1 (param i32) (result i32)))
    ` + ownedAsyncifyTestMemory + `
    (global $before_count (export "before_count") (mut i32) (i32.const 0))
    (global $after_count (export "after_count") (mut i32) (i32.const 0))

    (func (export "run1") (param $val i32) (result i32)
      (local $res i32)
      (global.set $before_count (i32.add (global.get $before_count) (i32.const 1)))
      (local.set $res (call $host_yield1 (local.get $val)))
      (global.set $after_count (i32.add (global.get $after_count) (i32.const 1)))
      (local.get $res)
    )
    (func (export "get_before1") (result i32) (global.get $before_count))
    (func (export "get_after1") (result i32) (global.get $after_count))
  )

  (core instance $inst0 (instantiate $m0 (with "test:async/host@0.1.0" (instance $host_inst0))))
  (core instance $inst1 (instantiate $m1 (with "test:async/host@0.1.1" (instance $host_inst1))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))

  (alias core export $inst0 "run0" (core func $core_run0))
  (alias core export $inst0 "get_before0" (core func $core_get_before0))
  (alias core export $inst0 "get_after0" (core func $core_get_after0))
  (alias core export $inst0 "memory" (core memory $core_mem0))

  (alias core export $inst1 "run1" (core func $core_run1))
  (alias core export $inst1 "get_before1" (core func $core_get_before1))
  (alias core export $inst1 "get_after1" (core func $core_get_after1))

  (func $lift_run0 (type $run_type) (canon lift (core func $core_run0)))
  (export "run0" (func $lift_run0))
  (func $lift_get_before0 (type $get_type) (canon lift (core func $core_get_before0)))
  (export "get-before0" (func $lift_get_before0))
  (func $lift_get_after0 (type $get_type) (canon lift (core func $core_get_after0)))
  (export "get-after0" (func $lift_get_after0))

  (func $lift_run1 (type $run_type) (canon lift (core func $core_run1)))
  (export "run1" (func $lift_run1))
  (func $lift_get_before1 (type $get_type) (canon lift (core func $core_get_before1)))
  (export "get-before1" (func $lift_get_before1))
  (func $lift_get_after1 (type $get_type) (canon lift (core func $core_get_after1)))
  (export "get-after1" (func $lift_get_after1))
)
`

// newerGuestWAT defines a component where the guest requires test:async/host@0.2.0.
const newerGuestWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.2.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $host_inst (export "yield" (func $yield_lowered)))

  (core module $m
    (import "test:async/host@0.2.0" "yield" (func $host_yield (param i32) (result i32)))
    (memory (export "memory") 1)
    (func (export "run") (param $val i32) (result i32)
      (call $host_yield (local.get $val))
    )
  )

  (core instance $inst (instantiate $m (with "test:async/host@0.2.0" (instance $host_inst))))

  (type $run_type (func (param "val" u32) (result u32)))
  (alias core export $inst "run" (core func $core_run))
  (alias core export $inst "memory" (core memory $core_mem))
  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
)
`

// methodNameAliasWAT defines a component where the component imports canonical WIT syntax
// "[method]stream.flush", lowers it, and exports it to the core instance as "method-stream-flush"
// (kebab alias), which the core module imports and calls directly.
const methodNameAliasWAT = `(component
  (type $host_iface
    (instance
      (export "[method]stream.flush" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/io@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "[method]stream.flush" (func $host_flush))
  (core func $flush_lowered (canon lower (func $host_flush)))
  (core instance $host_inst (export "method-stream-flush" (func $flush_lowered)))

  (core module $m
    (import "test:async/io@0.1.0" "method-stream-flush" (func $host_flush (param i32) (result i32)))
    ` + ownedAsyncifyTestMemory + `
    (global $count (export "count") (mut i32) (i32.const 0))
    (func (export "run") (param $val i32) (result i32)
      (global.set $count (i32.add (global.get $count) (i32.const 1)))
      (call $host_flush (local.get $val))
    )
    (func (export "get_count") (result i32) (global.get $count))
  )

  (core instance $inst (instantiate $m (with "test:async/io@0.1.0" (instance $host_inst))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst "run" (core func $core_run))
  (alias core export $inst "get_count" (core func $core_get_count))
  (alias core export $inst "memory" (core memory $core_mem))

  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
  (func $lift_get_count (type $get_type) (canon lift (core func $core_get_count)))
  (export "get-count" (func $lift_get_count))
)
`

// TestAsyncifyImportIdentity_DirectCallerSuspendResume verifies that:
//  1. A guest component calling an imported async function directly (without call_indirect)
//     has its actual guest import identity correctly propagated when host is a newer compatible version (0.1.2 vs 0.1.0).
//  2. The guest caller function is transformed by asyncify.
//  3. Suspend and resume succeed with side effects before and after executed EXACTLY ONCE.
func TestAsyncifyImportIdentity_DirectCallerSuspendResume(t *testing.T) {
	wasmBytes := componentFixture(t, directCallerOlderGuestWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	// Host registers newer compatible version 0.1.2 (guest imported 0.1.0)
	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.2",
		"yield",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "yield", id: 100}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	// Verify AsyncifyImports contains the guest's import identity
	asyncImports := mod.AsyncifyImports()
	foundGuestImport := false
	for _, imp := range asyncImports {
		if imp == "test:async/host@0.1.0#yield" {
			foundGuestImport = true
			break
		}
	}
	if !foundGuestImport {
		t.Fatalf("expected AsyncifyImports to contain guest import 'test:async/host@0.1.0#yield', got: %v", asyncImports)
	}

	// Instantiate with EnableAsyncify (AsyncifyImports auto-derived from mod.AsyncifyImports())
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	// Execute call
	cs, err := inst.StartCall(ctx, "run", uint32(55))
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	// First step: yields
	step1, err := cs.Step(ctx, nil)
	if err != nil {
		t.Fatalf("cs.Step initial: %v", err)
	}
	if step1.Status != StepContinue {
		t.Fatalf("cs.Step status = %v, want StepContinue", step1.Status)
	}
	if step1.PendingOp == nil || step1.PendingOp.CmdID() != 100 {
		t.Fatalf("expected PendingOp with CmdID 100, got %v", step1.PendingOp)
	}

	// Resume execution with value 77
	step2, err := cs.Step(ctx, &YieldResult{Value: 77})
	if err != nil {
		t.Fatalf("cs.Step resume: %v", err)
	}
	if step2.Status != StepDone {
		t.Fatalf("cs.Step status = %v, want StepDone", step2.Status)
	}

	lifted, err := cs.LiftResult(ctx, step2.Results)
	if err != nil {
		t.Fatalf("LiftResult: %v", err)
	}
	if lifted.(uint32) != 77 {
		t.Fatalf("LiftResult = %v, want 77", lifted)
	}

	// Verify side effects: EACH executed EXACTLY ONCE!
	beforeVal, err := inst.CallWithLift(ctx, "get-before")
	if err != nil {
		t.Fatalf("get-before: %v", err)
	}
	if beforeVal.(uint32) != 1 {
		t.Fatalf("before_count = %v, want 1 (side effect executed multiple times!)", beforeVal)
	}

	afterVal, err := inst.CallWithLift(ctx, "get-after")
	if err != nil {
		t.Fatalf("get-after: %v", err)
	}
	if afterVal.(uint32) != 1 {
		t.Fatalf("after_count = %v, want 1", afterVal)
	}

	tokenVal, err := inst.CallWithLift(ctx, "get-token")
	if err != nil {
		t.Fatalf("get-token: %v", err)
	}
	if tokenVal.(uint32) != 77 {
		t.Fatalf("token = %v, want 77", tokenVal)
	}
}

// TestAsyncifyImportIdentity_MultiversionHostServingMultipleGuests verifies that
// a single host registration (e.g. 0.1.2) propagates ALL compatible lowers when
// a guest component imports multiple patch versions (0.1.0 and 0.1.1).
func TestAsyncifyImportIdentity_MultiversionHostServingMultipleGuests(t *testing.T) {
	wasmBytes := componentFixture(t, multiversionGuestWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	// Single host registration at 0.1.2
	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.2",
		"yield",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "yield", id: 200}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	has010 := false
	has011 := false
	for _, imp := range asyncImports {
		if imp == "test:async/host@0.1.0#yield" {
			has010 = true
		}
		if imp == "test:async/host@0.1.1#yield" {
			has011 = true
		}
	}
	if !has010 || !has011 {
		t.Fatalf("expected AsyncifyImports to include both 0.1.0 and 0.1.1, got: %v", asyncImports)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	// Test run0 (calling 0.1.0 import)
	cs0, err := inst.StartCall(ctx, "run0", uint32(11))
	if err != nil {
		t.Fatalf("StartCall run0: %v", err)
	}
	step0_1, err := cs0.Step(ctx, nil)
	if err != nil || step0_1.Status != StepContinue {
		t.Fatalf("step0_1 failed: %v, status: %v", err, step0_1.Status)
	}
	step0_2, err := cs0.Step(ctx, &YieldResult{Value: 111})
	if err != nil || step0_2.Status != StepDone {
		t.Fatalf("step0_2 failed: %v, status: %v", err, step0_2.Status)
	}
	res0, _ := cs0.LiftResult(ctx, step0_2.Results)
	if res0.(uint32) != 111 {
		t.Fatalf("res0 = %v, want 111", res0)
	}
	b0, _ := inst.CallWithLift(ctx, "get-before0")
	a0, _ := inst.CallWithLift(ctx, "get-after0")
	if b0.(uint32) != 1 || a0.(uint32) != 1 {
		t.Fatalf("run0 side effects not 1: before=%v, after=%v", b0, a0)
	}

	// Test run1 (calling 0.1.1 import)
	cs1, err := inst.StartCall(ctx, "run1", uint32(22))
	if err != nil {
		t.Fatalf("StartCall run1: %v", err)
	}
	step1_1, err := cs1.Step(ctx, nil)
	if err != nil || step1_1.Status != StepContinue {
		t.Fatalf("step1_1 failed: %v, status: %v", err, step1_1.Status)
	}
	step1_2, err := cs1.Step(ctx, &YieldResult{Value: 222})
	if err != nil || step1_2.Status != StepDone {
		t.Fatalf("step1_2 failed: %v, status: %v", err, step1_2.Status)
	}
	res1, _ := cs1.LiftResult(ctx, step1_2.Results)
	if res1.(uint32) != 222 {
		t.Fatalf("res1 = %v, want 222", res1)
	}
	b1, _ := inst.CallWithLift(ctx, "get-before1")
	a1, _ := inst.CallWithLift(ctx, "get-after1")
	if b1.(uint32) != 1 || a1.(uint32) != 1 {
		t.Fatalf("run1 side effects not 1: before=%v, after=%v", b1, a1)
	}
}

// TestAsyncifyImportIdentity_NewerGuestUnsupportedHostNotWidened verifies that
// when a guest requires a newer version (0.2.0) that the host cannot satisfy (host is 0.1.0),
// the import identity is NOT widened or matched as async.
func TestAsyncifyImportIdentity_NewerGuestUnsupportedHostNotWidened(t *testing.T) {
	wasmBytes := componentFixture(t, newerGuestWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	// Host registers OLDER version 0.1.0; guest requires 0.2.0.
	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.0",
		"yield",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "yield", id: 300}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	for _, imp := range asyncImports {
		if strings.Contains(imp, "0.2.0") {
			t.Fatalf("newer guest import 0.2.0 was unexpectedly widened: %v", asyncImports)
		}
	}
}

// TestAsyncifyImportIdentity_MethodNameAliases verifies that both canonical WIT
// and kebab-case method name aliases are populated and match correctly.
func TestAsyncifyImportIdentity_MethodNameAliases(t *testing.T) {
	wasmBytes := componentFixture(t, methodNameAliasWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	// Host registers with canonical WIT method name under newer version 0.1.4
	err = mod.RegisterHostFuncRaw(
		"test:async/io@0.1.4",
		"[method]stream.flush",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "flush", id: 400}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	hasWit := false
	hasKebab := false
	for _, imp := range asyncImports {
		if imp == "test:async/io@0.1.0#[method]stream.flush" {
			hasWit = true
		}
		if imp == "test:async/io@0.1.0#method-stream-flush" {
			hasKebab = true
		}
	}
	if !hasWit || !hasKebab {
		t.Fatalf("expected both WIT and kebab aliases for 0.1.0, got: %v", asyncImports)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	cs, err := inst.StartCall(ctx, "run", uint32(9))
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	step1, err := cs.Step(ctx, nil)
	if err != nil || step1.Status != StepContinue {
		t.Fatalf("step1: %v, status: %v", err, step1.Status)
	}
	step2, err := cs.Step(ctx, &YieldResult{Value: 99})
	if err != nil || step2.Status != StepDone {
		t.Fatalf("step2: %v, status: %v", err, step2.Status)
	}
	res, _ := cs.LiftResult(ctx, step2.Results)
	if res.(uint32) != 99 {
		t.Fatalf("res = %v, want 99", res)
	}

	countVal, _ := inst.CallWithLift(ctx, "get-count")
	if countVal.(uint32) != 1 {
		t.Fatalf("count = %v, want 1 (side effect executed multiple times!)", countVal)
	}
}

// TestAsyncifyImportIdentity_RawCoreInstanceExportArgs verifies that core module
// imports derived from core instance instantiation arguments (e.g. env.yield)
// are automatically discovered and populated into AsyncifyImports.
func TestAsyncifyImportIdentity_RawCoreInstanceExportArgs(t *testing.T) {
	eng, mod := loadTwoCoreYieldModule(t)
	defer eng.Close(context.Background())

	// In loadTwoCoreYieldModule, test:async/host@0.1.0#yield is registered as async.
	// The component core module imports env.yield.
	asyncImports := mod.AsyncifyImports()
	foundEnvYield := false
	for _, imp := range asyncImports {
		if imp == "env#yield" {
			foundEnvYield = true
			break
		}
	}
	if !foundEnvYield {
		t.Fatalf("expected AsyncifyImports to contain 'env#yield', got: %v", asyncImports)
	}

	// Instantiate WITHOUT manually specifying AsyncifyImports in config:
	ctx := context.Background()
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify:     true,
		AsyncifyStackBytes: 1024, // fixture's bounded guest heap intentionally cannot hold the default reservation
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	cs, err := inst.StartCall(ctx, "func1", "auto-import-identity")
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	step1, err := cs.Step(ctx, nil)
	if err != nil || step1.Status != StepContinue {
		t.Fatalf("step1: %v, status: %v", err, step1.Status)
	}
	step2, err := cs.Step(ctx, &YieldResult{Value: 88})
	if err != nil || step2.Status != StepDone {
		t.Fatalf("step2: %v, status: %v", err, step2.Status)
	}
	res, err := cs.LiftResult(ctx, step2.Results)
	if err != nil {
		t.Fatalf("LiftResult: %v", err)
	}
	if res.(string) != "auto-import-identity" {
		t.Fatalf("res = %q, want %q", res, "auto-import-identity")
	}

	// Verify token
	tok, _ := inst.CallWithLift(ctx, "get-token1")
	if tok.(uint32) != 88 {
		t.Fatalf("token1 = %v, want 88", tok)
	}
}

// typedAsyncWAT defines a component where the guest imports test:typed/calc@0.1.0 with function compute.
const typedAsyncWAT = `(component
  (type $host_iface
    (instance
      (export "compute" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:typed/calc@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "compute" (func $host_compute))
  (core func $compute_lowered (canon lower (func $host_compute)))
  (core instance $host_inst (export "compute" (func $compute_lowered)))

  (core module $m
    (import "test:typed/calc@0.1.0" "compute" (func $host_compute (param i32) (result i32)))
    (memory (export "memory") 1)
    (global $count (export "count") (mut i32) (i32.const 0))
    (func (export "run") (param $val i32) (result i32)
      (global.set $count (i32.add (global.get $count) (i32.const 1)))
      (call $host_compute (local.get $val))
    )
    (func (export "get_count") (result i32) (global.get $count))
  )

  (core instance $inst (instantiate $m (with "test:typed/calc@0.1.0" (instance $host_inst))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst "run" (core func $core_run))
  (alias core export $inst "get_count" (core func $core_get_count))
  (alias core export $inst "memory" (core memory $core_mem))

  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
  (func $lift_get_count (type $get_type) (canon lift (core func $core_get_count)))
  (export "get-count" (func $lift_get_count))
)
`

// TestAsyncifyImportIdentity_TypedAsyncSemverPropagation verifies that RegisterHostFuncTypedAsync
// resolves compatible lower definitions via semver and populates both the guest and host import identities.
func TestAsyncifyImportIdentity_TypedAsyncSemverPropagation(t *testing.T) {
	wasmBytes := componentFixture(t, typedAsyncWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	// Register typed async host func under 0.1.5 (guest imported 0.1.0)
	err = mod.RegisterHostFuncTypedAsync(
		"test:typed/calc@0.1.5",
		"compute",
		func(ctx context.Context, val uint32) uint32 {
			return val * 2
		},
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncTypedAsync: %v", err)
	}

	imports := mod.AsyncifyImports()
	hasGuest := false
	hasHost := false
	for _, imp := range imports {
		if imp == "test:typed/calc@0.1.0#compute" {
			hasGuest = true
		}
		if imp == "test:typed/calc@0.1.5#compute" {
			hasHost = true
		}
	}
	if !hasGuest || !hasHost {
		t.Fatalf("expected both guest and host identities in AsyncifyImports, got: %v", imports)
	}
}

// tableBeforeFunctionWAT defines a component where the core module imports a table and
// memory BEFORE importing the async host function.
// Tests the regression where table import skip misparsed following imports.
const tableBeforeFunctionWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $host_inst (export "yield" (func $yield_lowered)))

  (core module $env_mod
    (table (export "tbl") 1 2 funcref)
    (memory (export "mem") 1)` + ownedAsyncifyTestAllocator + `
  )
  (core instance $env_inst (instantiate $env_mod))
  (alias core export $env_inst "tbl" (core table $tbl))
  (alias core export $env_inst "mem" (core memory $mem))
  (core instance $env_bundle
    (export "tbl" (table $tbl))
    (export "mem" (memory $mem))
  )

  (core module $m
    (import "env" "tbl" (table 1 2 funcref))
    (import "env" "mem" (memory 1))
    (import "test:async/host@0.1.0" "yield" (func $host_yield (param i32) (result i32)))
` + ownedAsyncifyTestAllocator + `
    (global $count (export "count") (mut i32) (i32.const 0))
    (func (export "run") (param $val i32) (result i32)
      (global.set $count (i32.add (global.get $count) (i32.const 1)))
      (call $host_yield (local.get $val))
    )
    (func (export "get_count") (result i32) (global.get $count))
  )

  (core instance $inst (instantiate $m
    (with "env" (instance $env_bundle))
    (with "test:async/host@0.1.0" (instance $host_inst))
  ))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst "run" (core func $core_run))
  (alias core export $inst "get_count" (core func $core_get_count))
  (alias core export $env_bundle "mem" (core memory $core_mem))

  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
  (func $lift_get_count (type $get_type) (canon lift (core func $core_get_count)))
  (export "get-count" (func $lift_get_count))
)
`

// realisticAliasesChainWAT defines a component where a core module's import is resolved
// through a multi-step alias and instantiated core instance export forwarding chain.
const realisticAliasesChainWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))

  (core instance $inst0 (export "raw_yield" (func $yield_lowered)))
  (alias core export $inst0 "raw_yield" (core func $alias1))

  (core module $forwarder
    (import "in_mod" "in_func" (func $f (param i32) (result i32)))
    (export "out_func" (func $f))
  )
  (core instance $fwd_arg (export "in_func" (func $alias1)))
  (core instance $inst1 (instantiate $forwarder (with "in_mod" (instance $fwd_arg))))

  (alias core export $inst1 "out_func" (core func $alias2))
  (core instance $inst2 (export "yield" (func $alias2)))

  (core module $guest
    (import "env" "yield" (func $host_yield (param i32) (result i32)))
    ` + ownedAsyncifyTestMemory + `
    (global $count (export "count") (mut i32) (i32.const 0))
    (func (export "run") (param $val i32) (result i32)
      (global.set $count (i32.add (global.get $count) (i32.const 1)))
      (call $host_yield (local.get $val))
    )
    (func (export "get_count") (result i32) (global.get $count))
  )
  (core instance $inst3 (instantiate $guest (with "env" (instance $inst2))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst3 "run" (core func $core_run))
  (alias core export $inst3 "get_count" (core func $core_get_count))
  (alias core export $inst3 "memory" (core memory $core_mem))

  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
  (func $lift_get_count (type $get_type) (canon lift (core func $core_get_count)))
  (export "get-count" (func $lift_get_count))
)
`

// TestAsyncifyImportIdentity_TableBeforeFunctionProfileRejected verifies that
// table and memory imports preceding a function import do not desynchronize
// async import discovery. The executable imported-table topology is then
// rejected before instantiation because it bypasses bridge continuations.
func TestAsyncifyImportIdentity_TableBeforeFunctionProfileRejected(t *testing.T) {
	wasmBytes := componentFixture(t, tableBeforeFunctionWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.2",
		"yield",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, m api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "yield", id: 42}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	found := false
	for _, imp := range asyncImports {
		if imp == "test:async/host@0.1.0#yield" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected AsyncifyImports to contain 'test:async/host@0.1.0#yield' despite preceding table import, got: %v", asyncImports)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if inst != nil {
		defer inst.Close(ctx)
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported cross-core continuation boundary") {
		t.Fatalf("expected executable imported-table profile rejection, got %v", err)
	}
}

// TestAsyncifyImportIdentity_RealisticAliasesChain verifies that when a core module
// imports a function that has been forwarded through a chain of core instances, aliases,
// and forwarder modules, AsyncifyImports correctly resolves the chain back to the
// underlying canon lower.
func TestAsyncifyImportIdentity_RealisticAliasesChain(t *testing.T) {
	wasmBytes := componentFixture(t, realisticAliasesChainWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.2",
		"yield",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, m api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "yield", id: 77}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	foundEnvYield := false
	for _, imp := range asyncImports {
		if imp == "env#yield" {
			foundEnvYield = true
			break
		}
	}
	if !foundEnvYield {
		t.Fatalf("expected AsyncifyImports to contain 'env#yield' via alias chain, got: %v", asyncImports)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	cs, err := inst.StartCall(ctx, "run", uint32(7))
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	step1, err := cs.Step(ctx, nil)
	if err != nil || step1.Status != StepContinue {
		t.Fatalf("step1: %v, status: %v", err, step1.Status)
	}
	step2, err := cs.Step(ctx, &YieldResult{Value: 700})
	if err != nil || step2.Status != StepDone {
		t.Fatalf("step2: %v, status: %v", err, step2.Status)
	}
	res, _ := cs.LiftResult(ctx, step2.Results)
	if res.(uint32) != 700 {
		t.Fatalf("res = %v, want 700", res)
	}

	countVal, _ := inst.CallWithLift(ctx, "get-count")
	if countVal.(uint32) != 1 {
		t.Fatalf("count = %v, want 1", countVal)
	}
}

// TestAsyncifyImportIdentity_IntersectionNonLowerSkipped verifies that when a host function
// is registered as async on a component module, but the component has no matching canon lower,
// the host function is NOT added to AsyncifyImports (preserving intersection semantics).
func TestAsyncifyImportIdentity_IntersectionNonLowerSkipped(t *testing.T) {
	wasmBytes := componentFixture(t, directCallerOlderGuestWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	// Register an async host function for an interface the component does NOT import
	err = mod.RegisterHostFuncRaw(
		"test:unimported/iface@0.1.0",
		"noop",
		nil,
		nil,
		MakeAsyncHandler(func(ctx context.Context, m api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "noop", id: 1}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	imports := mod.AsyncifyImports()
	for _, imp := range imports {
		if strings.HasPrefix(imp, "test:unimported/iface") {
			t.Fatalf("expected unimported host func to be excluded by intersection logic, got: %s in %v", imp, imports)
		}
	}
}

// duplicateLowerSameNameWAT defines a component where two distinct canon lowers
// are declared for the same imported function, producing multiple entries in
// CoreFuncIndexSpace with the same import identity.
const duplicateLowerSameNameWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $lower0 (canon lower (func $host_yield)))
  (core func $lower1 (canon lower (func $host_yield)))

  (core instance $inst0 (export "yield0" (func $lower0)))
  (core instance $inst1 (export "yield1" (func $lower1)))

  (core module $m
    (import "env0" "yield0" (func $f0 (param i32) (result i32)))
    (import "env1" "yield1" (func $f1 (param i32) (result i32)))
    (func (export "run") (result i32)
      (drop (call $f0 (i32.const 1)))
      (call $f1 (i32.const 2))
    )
  )

  (core instance $inst (instantiate $m
    (with "env0" (instance $inst0))
    (with "env1" (instance $inst1))
  ))

  (type $run_type (func (result u32)))
  (alias core export $inst "run" (core func $core_run))
  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
)
`

// TestAsyncifyImportIdentity_DuplicateLowerSameName verifies that when multiple canon
// lowers exist for the same imported function (which causes them to collide in FindLower
// map lookups), all corresponding core instance import identities are properly matched.
func TestAsyncifyImportIdentity_DuplicateLowerSameName(t *testing.T) {
	wasmBytes := componentFixture(t, duplicateLowerSameNameWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.2",
		"yield",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, m api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "yield", id: 1}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	imports := mod.AsyncifyImports()
	hasYield0 := false
	hasYield1 := false
	for _, imp := range imports {
		if imp == "env0#yield0" {
			hasYield0 = true
		}
		if imp == "env1#yield1" {
			hasYield1 = true
		}
	}
	if !hasYield0 || !hasYield1 {
		t.Fatalf("expected both 'env0#yield0' and 'env1#yield1' in AsyncifyImports, got: %v", imports)
	}
}
