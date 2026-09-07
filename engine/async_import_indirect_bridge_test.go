package engine

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// indirectBridge3CoreWAT defines a 3-core component:
// - Module A: provides a table containing async lowered host target.
// - Module B: imports table from A (via table instance) and exports wrapper with ONLY call_indirect, no func imports.
// - Module C: imports B wrapper and has direct caller with before/after effect counters.
const indirectBridge3CoreWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $host_inst (export "yield" (func $yield_lowered)))

  ;; Module A: provides table containing async lowered host target
  (core module $m_a
    (import "host_ns" "yield" (func $host_yield (param i32) (result i32)))
    (table $t (export "table") 1 funcref)
    (elem (i32.const 0) $host_yield)
  )
  (core instance $inst_a (instantiate $m_a (with "host_ns" (instance $host_inst))))

  (alias core export $inst_a "table" (core table $tbl))
  (core instance $inst_table (export "table" (table $tbl)))

  ;; Module B: imports table and exports wrapper with ONLY call_indirect, no func imports
  (core module $m_b
    (import "table_ns" "table" (table 1 funcref))
    (type $sig (func (param i32) (result i32)))
    (func (export "wrapped_call") (param $val i32) (result i32)
      (call_indirect (type $sig) (local.get $val) (i32.const 0))
    )
  )
  (core instance $inst_b (instantiate $m_b (with "table_ns" (instance $inst_table))))

  ;; Module C: imports B wrapper and has direct caller with before/after effect counters
  (core module $m_c
    (import "b_ns" "wrapped_call" (func $wrapped_call (param i32) (result i32)))
    (memory (export "memory") 1)
    (global $before_count (export "before_count") (mut i32) (i32.const 0))
    (global $after_count (export "after_count") (mut i32) (i32.const 0))
    (global $token (export "token") (mut i32) (i32.const 0))

    (func (export "run") (param $val i32) (result i32)
      (local $res i32)
      (global.set $before_count (i32.add (global.get $before_count) (i32.const 1)))
      (local.set $res (call $wrapped_call (local.get $val)))
      (global.set $token (local.get $res))
      (global.set $after_count (i32.add (global.get $after_count) (i32.const 1)))
      (local.get $res)
    )
    (func (export "get_before") (result i32) (global.get $before_count))
    (func (export "get_after") (result i32) (global.get $after_count))
    (func (export "get_token") (result i32) (global.get $token))
  )
  (core instance $inst_c (instantiate $m_c (with "b_ns" (instance $inst_b))))

  (type $run_type (func (param "val" u32) (result u32)))
  (type $get_type (func (result u32)))
  (alias core export $inst_c "run" (core func $core_run))
  (alias core export $inst_c "get_before" (core func $core_get_before))
  (alias core export $inst_c "get_after" (core func $core_get_after))
  (alias core export $inst_c "get_token" (core func $core_get_token))
  (alias core export $inst_c "memory" (core memory $core_mem))

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

func TestAsyncifyImportSoundness_IndirectBridge3Core(t *testing.T) {
	wasmBytes := componentFixture(t, indirectBridge3CoreWAT)

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
			return &traceOp{name: "yield", id: 777}
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
		if imp == "b_ns#wrapped_call" {
			foundWrapperImport = true
			break
		}
	}
	if !foundWrapperImport {
		t.Fatalf("BEFOREPROOF: expected AsyncifyImports to contain 'b_ns#wrapped_call' but got: %v", asyncImports)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify: true,
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	cs, err := inst.StartCall(ctx, "run", uint32(42))
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
	if step1.PendingOp == nil || step1.PendingOp.CmdID() != 777 {
		t.Fatalf("expected PendingOp with CmdID 777, got %v", step1.PendingOp)
	}

	step2, err := cs.Step(ctx, &YieldResult{Value: 100})
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
	if lifted.(uint32) != 100 {
		t.Fatalf("LiftResult = %v, want 100", lifted)
	}

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
	if tokVal.(uint32) != 100 {
		t.Fatalf("token = %v, want 100", tokVal)
	}
}
