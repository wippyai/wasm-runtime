package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

// multiCoreWrapperWAT defines a component where:
// - Module 1 imports host async function and defines an exported wrapper function calling it.
// - Module 2 is instantiated directly with Module 1, imports the wrapper, and calls it directly.
// - NO call_indirect anywhere!
const multiCoreWrapperWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $host_inst (export "yield" (func $yield_lowered)))

  ;; Module 1: imports host yield and defines an exported wrapper function calling it directly
  (core module $m_wrapper
    (import "host_ns" "yield" (func $host_yield (param i32) (result i32)))
    (memory (export "memory") 1)
    (func (export "wrapped_yield") (param $val i32) (result i32)
      (call $host_yield (local.get $val))
    )
  )
  (core instance $inst_wrapper (instantiate $m_wrapper (with "host_ns" (instance $host_inst))))

  ;; Module 2: imports the wrapper function directly from Module 1, without call_indirect
  (core module $m_caller
    (import "wrapper_ns" "wrapped_yield" (func $wrapped_yield (param i32) (result i32)))
    (memory (export "memory") 1)
    (global $before_count (export "before_count") (mut i32) (i32.const 0))
    (global $after_count (export "after_count") (mut i32) (i32.const 0))
    (global $token (export "token") (mut i32) (i32.const 0))

    (func (export "run") (param $val i32) (result i32)
      (local $res i32)
      ;; Direct caller: NO call_indirect anywhere in the module!
      (global.set $before_count (i32.add (global.get $before_count) (i32.const 1)))
      (local.set $res (call $wrapped_yield (local.get $val)))
      (global.set $token (local.get $res))
      (global.set $after_count (i32.add (global.get $after_count) (i32.const 1)))
      (local.get $res)
    )
    (func (export "get_before") (result i32) (global.get $before_count))
    (func (export "get_after") (result i32) (global.get $after_count))
    (func (export "get_token") (result i32) (global.get $token))
  )
  (core instance $inst_caller (instantiate $m_caller (with "wrapper_ns" (instance $inst_wrapper))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst_caller "run" (core func $core_run))
  (alias core export $inst_caller "get_before" (core func $core_get_before))
  (alias core export $inst_caller "get_after" (core func $core_get_after))
  (alias core export $inst_caller "get_token" (core func $core_get_token))
  (alias core export $inst_caller "memory" (core memory $core_mem))

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

// buildChainWAT builds a component where a lowered host function is forwarded
// through chainLen core instances and aliases before being imported and called
// by the caller core module.
func buildChainWAT(chainLen int) string {
	var sb strings.Builder
	sb.WriteString(`(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
`)

	sb.WriteString(`  (core instance $c0 (export "f" (func $yield_lowered)))` + "\n")
	for i := 1; i <= chainLen; i++ {
		fmt.Fprintf(&sb, "  (alias core export $c%d \"f\" (core func $f%d))\n", i-1, i)
		fmt.Fprintf(&sb, "  (core instance $c%d (export \"f\" (func $f%d)))\n", i, i)
	}

	fmt.Fprintf(&sb, `
  (core module $m
    (import "chain_ns" "f" (func $chain_f (param i32) (result i32)))
    (memory (export "memory") 1)
    (global $count (export "count") (mut i32) (i32.const 0))
    (func (export "run") (param $val i32) (result i32)
      (global.set $count (i32.add (global.get $count) (i32.const 1)))
      (call $chain_f (local.get $val))
    )
    (func (export "get_count") (result i32) (global.get $count))
  )
  (core instance $inst (instantiate $m (with "chain_ns" (instance $c%d))))

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
`, chainLen)

	return sb.String()
}

func TestAsyncifyImportSoundness_MultiCoreWrapperSuspendResume(t *testing.T) {
	wasmBytes := componentFixture(t, multiCoreWrapperWAT)

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
			return &traceOp{name: "yield", id: 888}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	t.Logf("Derived AsyncifyImports: %v", asyncImports)

	foundWrapperImport := false
	for _, imp := range asyncImports {
		if imp == "wrapper_ns#wrapped_yield" {
			foundWrapperImport = true
			break
		}
	}
	if !foundWrapperImport {
		t.Fatalf("BEFOREPROOF: expected AsyncifyImports to contain 'wrapper_ns#wrapped_yield' but got: %v", asyncImports)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	cs, err := inst.StartCall(ctx, "run", uint32(123))
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	step1, err := cs.Step(ctx, nil)
	if err != nil {
		t.Fatalf("Step initial: %v", err)
	}
	if step1.Status != StepContinue {
		t.Fatalf("status = %v, want StepContinue", step1.Status)
	}
	if step1.PendingOp == nil || step1.PendingOp.CmdID() != 888 {
		t.Fatalf("expected PendingOp with CmdID 888, got %v", step1.PendingOp)
	}

	step2, err := cs.Step(ctx, &YieldResult{Value: 456})
	if err != nil {
		t.Fatalf("Step resume: %v", err)
	}
	if step2.Status != StepDone {
		t.Fatalf("status = %v, want StepDone", step2.Status)
	}

	lifted, err := cs.LiftResult(ctx, step2.Results)
	if err != nil {
		t.Fatalf("LiftResult: %v", err)
	}
	if lifted.(uint32) != 456 {
		t.Fatalf("LiftResult = %v, want 456", lifted)
	}

	// Side effects must execute EXACTLY ONCE
	beforeVal, err := inst.CallWithLift(ctx, "get-before")
	if err != nil {
		t.Fatalf("get-before: %v", err)
	}
	if beforeVal.(uint32) != 1 {
		t.Fatalf("before_count = %v, want 1 (side effect replayed!)", beforeVal)
	}

	afterVal, err := inst.CallWithLift(ctx, "get-after")
	if err != nil {
		t.Fatalf("get-after: %v", err)
	}
	if afterVal.(uint32) != 1 {
		t.Fatalf("after_count = %v, want 1", afterVal)
	}

	tokVal, err := inst.CallWithLift(ctx, "get-token")
	if err != nil {
		t.Fatalf("get-token: %v", err)
	}
	if tokVal.(uint32) != 456 {
		t.Fatalf("token = %v, want 456", tokVal)
	}
}

func TestAsyncifyImportSoundness_ChainGreaterThan32(t *testing.T) {
	// Chain length of 36 (> 32)
	wat := buildChainWAT(36)
	wasmBytes := componentFixture(t, wat)

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
			return &traceOp{name: "yield", id: 999}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	t.Logf("Derived AsyncifyImports: %v", asyncImports)

	foundChainImport := false
	for _, imp := range asyncImports {
		if imp == "chain_ns#f" {
			foundChainImport = true
			break
		}
	}
	if !foundChainImport {
		t.Fatalf("BEFOREPROOF: expected AsyncifyImports to contain 'chain_ns#f' for chain length 36 (> 32), got: %v", asyncImports)
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
		t.Fatalf("step1 failed: %v, status: %v", err, step1.Status)
	}

	step2, err := cs.Step(ctx, &YieldResult{Value: 777})
	if err != nil || step2.Status != StepDone {
		t.Fatalf("step2 failed: %v, status: %v", err, step2.Status)
	}

	res, err := cs.LiftResult(ctx, step2.Results)
	if err != nil || res.(uint32) != 777 {
		t.Fatalf("res = %v, want 777", res)
	}

	countVal, _ := inst.CallWithLift(ctx, "get-count")
	if countVal.(uint32) != 1 {
		t.Fatalf("count = %v, want 1", countVal)
	}
}

// twoLevelWrapperWAT defines a 3-core module chain where:
// - Module 1 wraps the host async yield.
// - Module 2 wraps Module 1's wrapper.
// - Module 3 directly calls Module 2's wrapper.
const twoLevelWrapperWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $host_inst (export "yield" (func $yield_lowered)))

  ;; Module 1: Level 1 wrapper
  (core module $m1
    (import "h" "yield" (func $host_yield (param i32) (result i32)))
    (func (export "wrap1") (param $x i32) (result i32)
      (call $host_yield (local.get $x))
    )
  )
  (core instance $inst1 (instantiate $m1 (with "h" (instance $host_inst))))

  ;; Module 2: Level 2 wrapper calling Module 1's wrap1
  (core module $m2
    (import "w1" "wrap1" (func $wrap1 (param i32) (result i32)))
    (func (export "wrap2") (param $x i32) (result i32)
      (call $wrap1 (local.get $x))
    )
  )
  (core instance $inst2 (instantiate $m2 (with "w1" (instance $inst1))))

  ;; Module 3: Caller calling Module 2's wrap2
  (core module $m3
    (import "w2" "wrap2" (func $wrap2 (param i32) (result i32)))
    (memory (export "memory") 1)
    (global $before (export "before") (mut i32) (i32.const 0))
    (global $after (export "after") (mut i32) (i32.const 0))

    (func (export "run") (param $val i32) (result i32)
      (local $res i32)
      (global.set $before (i32.add (global.get $before) (i32.const 1)))
      (local.set $res (call $wrap2 (local.get $val)))
      (global.set $after (i32.add (global.get $after) (i32.const 1)))
      (local.get $res)
    )
    (func (export "get_before") (result i32) (global.get $before))
    (func (export "get_after") (result i32) (global.get $after))
  )
  (core instance $inst3 (instantiate $m3 (with "w2" (instance $inst2))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst3 "run" (core func $core_run))
  (alias core export $inst3 "get_before" (core func $core_get_before))
  (alias core export $inst3 "get_after" (core func $core_get_after))
  (alias core export $inst3 "memory" (core memory $core_mem))

  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
  (func $lift_get_before (type $get_type) (canon lift (core func $core_get_before)))
  (export "get-before" (func $lift_get_before))
  (func $lift_get_after (type $get_type) (canon lift (core func $core_get_after)))
  (export "get-after" (func $lift_get_after))
)
`

func TestAsyncifyImportSoundness_TwoLevelWrapperChain(t *testing.T) {
	wasmBytes := componentFixture(t, twoLevelWrapperWAT)

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
			return &traceOp{name: "yield", id: 222}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	asyncImports := mod.AsyncifyImports()
	t.Logf("Derived AsyncifyImports: %v", asyncImports)

	hasW1 := false
	hasW2 := false
	for _, imp := range asyncImports {
		if imp == "w1#wrap1" {
			hasW1 = true
		}
		if imp == "w2#wrap2" {
			hasW2 = true
		}
	}
	if !hasW1 || !hasW2 {
		t.Fatalf("expected both 'w1#wrap1' and 'w2#wrap2' in AsyncifyImports, got: %v", asyncImports)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	cs, err := inst.StartCall(ctx, "run", uint32(50))
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	step1, err := cs.Step(ctx, nil)
	if err != nil || step1.Status != StepContinue {
		t.Fatalf("step1 failed: %v, status: %v", err, step1.Status)
	}

	step2, err := cs.Step(ctx, &YieldResult{Value: 500})
	if err != nil || step2.Status != StepDone {
		t.Fatalf("step2 failed: %v, status: %v", err, step2.Status)
	}

	res, err := cs.LiftResult(ctx, step2.Results)
	if err != nil || res.(uint32) != 500 {
		t.Fatalf("res = %v, want 500", res)
	}

	before, _ := inst.CallWithLift(ctx, "get-before")
	after, _ := inst.CallWithLift(ctx, "get-after")
	if before.(uint32) != 1 || after.(uint32) != 1 {
		t.Fatalf("side effects not 1: before=%v, after=%v", before, after)
	}
}

// arbitraryNamespacesWAT defines a component with arbitrary namespace characters and depth
const arbitraryNamespacesWAT = `(component
  (type $host_iface
    (instance
      (export "compute-data" (func (param "val" u32) (result u32)))
    )
  )
  (import "custom.org/services:api@1.2.0" (instance $host (type $host_iface)))
  (alias export $host "compute-data" (func $host_compute))
  (core func $compute_lowered (canon lower (func $host_compute)))
  (core instance $host_inst (export "compute-data" (func $compute_lowered)))

  (core module $m
    (import "custom.org/services:api@1.2.0" "compute-data" (func $compute (param i32) (result i32)))
    (memory (export "memory") 1)
    (func (export "run") (param $val i32) (result i32)
      (call $compute (local.get $val))
    )
  )
  (core instance $inst (instantiate $m (with "custom.org/services:api@1.2.0" (instance $host_inst))))

  (type $run_type (func (param "val" u32) (result u32)))
  (alias core export $inst "run" (core func $core_run))
  (alias core export $inst "memory" (core memory $core_mem))

  (func $lift_run (type $run_type) (canon lift (core func $core_run)))
  (export "run" (func $lift_run))
)
`

func TestAsyncifyImportSoundness_ArbitraryNamespaces(t *testing.T) {
	wasmBytes := componentFixture(t, arbitraryNamespacesWAT)

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

	// Register with newer compatible semver 1.2.5
	err = mod.RegisterHostFuncRaw(
		"custom.org/services:api@1.2.5",
		"compute-data",
		[]api.ValueType{api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(ctx context.Context, m api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "compute", id: 333}
		}),
		true,
	)
	if err != nil {
		t.Fatalf("RegisterHostFuncRaw: %v", err)
	}

	imports := mod.AsyncifyImports()
	found := false
	for _, imp := range imports {
		if imp == "custom.org/services:api@1.2.0#compute-data" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected AsyncifyImports to contain 'custom.org/services:api@1.2.0#compute-data', got: %v", imports)
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
	step2, err := cs.Step(ctx, &YieldResult{Value: 999})
	if err != nil || step2.Status != StepDone {
		t.Fatalf("step2: %v, status: %v", err, step2.Status)
	}
	res, _ := cs.LiftResult(ctx, step2.Results)
	if res.(uint32) != 999 {
		t.Fatalf("res = %v, want 999", res)
	}
}

func TestAsyncifyImportSoundness_FailClosedOnCorruptedCoreModule(t *testing.T) {
	// Start from a valid component and corrupt its embedded core module bytecode
	validBytes := componentFixture(t, multiCoreWrapperWAT)

	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, validBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	defer mod.Close(ctx)

	// Register async host function
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

	// Corrupt one core module in validated component
	if mod.validated != nil && mod.validated.Raw != nil && len(mod.validated.Raw.CoreModules) > 0 {
		mod.validated.Raw.CoreModules[0] = []byte{0x00, 0x61, 0x73, 0x6d, 0xff, 0xff, 0xff, 0xff}
	}

	// Instantiation with asyncify must FAIL closed with error, not proceed with broken state
	_, err = mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err == nil {
		t.Fatal("expected InstantiateWithConfig to fail closed on corrupt core module, but got nil error")
	}
	if !strings.Contains(err.Error(), "ensure linker") && !strings.Contains(err.Error(), "parse core module") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestAsyncifyImportSoundness_StandaloneFallbackUsesImportNotName(t *testing.T) {
	// A simple core module WAT importing env.yield
	const coreWat = `(module
    (import "env" "yield" (func $yield (param i32) (result i32)))
    (func (export "run") (param i32) (result i32)
      (call $yield (local.get 0))
    )
  )`

	wasmBytes, err := wat.Compile(coreWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

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
		"env",
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

	// Corrupt rawBytes to force the fallback to m.compiled.ImportedFunctions()
	mod.rawBytes = []byte{0x00, 0x61, 0x73, 0x6d, 0xff, 0xff, 0xff, 0xff}

	// The fallback should correctly use fn.Import() to recover "env#yield"
	imports := mod.AsyncifyImports()
	found := false
	for _, imp := range imports {
		if imp == "env#yield" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected fallback via fn.Import() to find 'env#yield', got: %v", imports)
	}
}
