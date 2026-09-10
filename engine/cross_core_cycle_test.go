package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// crossCoreCycleWAT has an acyclic core-instance graph (A, then B, then the
// table-only initializer F), but an execution cycle. F installs B.b into A's
// private table; A.a calls that table and B.b imports A.a. A second traversal
// would re-enter the same cached Go forwarding wrapper before its first call
// returns. That must fail closed before it reaches the host lower.
const crossCoreCycleWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (param "val" u32) (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $host_inst (export "yield" (func $yield_lowered)))

  ;; A owns its table and imports only the real host lower.
  (core module $m_a
    (import "host" "yield" (func $host_yield (param i32) (result i32)))
    ` + ownedAsyncifyTestMemory + `
    (table $table (export "table") 1 funcref)
    (type $sig (func (param i32) (result i32)))
    (global $a_after (export "a_after") (mut i32) (i32.const 0))
    (global $entry_after (export "entry_after") (mut i32) (i32.const 0))
    (func (export "a") (param $n i32) (result i32)
      (local $result i32)
      (local.set $result
        (if (result i32) (i32.eqz (local.get $n))
          (then (call $host_yield (local.get $n)))
          (else
            (call_indirect (type $sig)
              (i32.sub (local.get $n) (i32.const 1))
              (i32.const 0)))))
      (global.set $a_after (i32.add (global.get $a_after) (i32.const 1)))
      (local.get $result))
    (func (export "run") (param $n i32) (result i32)
      (local $result i32)
      (local.set $result (call_indirect (type $sig) (local.get $n) (i32.const 0)))
      (global.set $entry_after (i32.add (global.get $entry_after) (i32.const 1)))
      (local.get $result))
  )
  (core instance $a (instantiate $m_a (with "host" (instance $host_inst))))

  ;; B can be instantiated after A and calls A.a through a normal function
  ;; bridge. It has no table import.
  (alias core export $a "a" (core func $a_fn))
  (core instance $a_fn_inst (export "a" (func $a_fn)))
  (core module $m_b
    (import "a" "a" (func $a (param i32) (result i32)))
    (global $b_after (export "b_after") (mut i32) (i32.const 0))
    (func (export "b") (param $n i32) (result i32)
      (local $result i32)
      (local.set $result (call $a (local.get $n)))
      (global.set $b_after (i32.add (global.get $b_after) (i32.const 1)))
      (local.get $result))
  )
  (core instance $b (instantiate $m_b (with "a" (instance $a_fn_inst))))

  ;; F is table-only: it may import A's table and B's function, then installs
  ;; B.b into A's private table. It cannot execute an indirect call itself.
  (alias core export $a "table" (core table $a_table))
  (core instance $a_table_inst (export "table" (table $a_table)))
  (alias core export $b "b" (core func $b_fn))
  (core instance $b_fn_inst (export "b" (func $b_fn)))
  (core module $m_f
    (import "a" "table" (table 1 funcref))
    (import "b" "b" (func $b (param i32) (result i32)))
    (elem (i32.const 0) $b)
  )
  (core instance $f (instantiate $m_f
    (with "a" (instance $a_table_inst))
    (with "b" (instance $b_fn_inst))))

  (type $run_type (func (param "n" u32) (result u32)))
  (alias core export $a "run" (core func $run))
  (alias core export $a "memory" (core memory $memory))
  (func $lift_run (type $run_type) (canon lift (core func $run)))
  (export "run" (func $lift_run))
)
`

func TestAsyncifyCrossCoreCycleRejectsReentrantForwarder(t *testing.T) {
	ctx := context.Background()
	wasmBytes := componentFixture(t, crossCoreCycleWAT)
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close(ctx)

	var hostCalls int
	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.2", "yield",
		[]api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp {
			hostCalls++
			return &traceOp{name: "must-not-yield", id: 1}
		}), true,
	)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)

	cs, err := inst.StartCall(ctx, "run", uint32(2))
	if err != nil {
		t.Fatal(err)
	}
	step, err := cs.Step(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "recursive invocation") {
		t.Fatalf("cyclic bridge Step = %+v, %v, want reentrant-forwarder rejection", step, err)
	}
	if hostCalls != 0 {
		t.Fatalf("cyclic bridge reached host lower %d times", hostCalls)
	}
	for _, global := range []string{"a_after", "b_after", "entry_after"} {
		for _, core := range inst.linkerInst.Modules() {
			if core != nil && core.ExportedGlobal(global) != nil && core.ExportedGlobal(global).Get() != 0 {
				t.Fatalf("%s ran after rejected cycle: %d", global, core.ExportedGlobal(global).Get())
			}
		}
	}
	if _, err := inst.StartCall(ctx, "run", uint32(0)); err == nil {
		t.Fatal("fresh call after cyclic bridge failure succeeded")
	}
}
