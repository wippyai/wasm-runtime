package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// indirectBridge3CoreWAT defines a 3-core component outside the supported
// Asyncify execution profile:
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
    ` + ownedAsyncifyTestMemory + `
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

func TestAsyncifyImportSoundness_IndirectBridge3CoreRejected(t *testing.T) {
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
	if inst != nil {
		defer inst.Close(ctx)
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported cross-core continuation boundary") {
		t.Fatalf("expected untracked indirect table boundary rejection, got %v", err)
	}
}
