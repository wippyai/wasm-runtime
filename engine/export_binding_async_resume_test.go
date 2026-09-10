package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
	"go.bytecodealliance.org/wit"
)

const twoCoreYieldComponentWAT = `(component
  (type $host_iface
    (instance
      (export "yield" (func (result u32)))
    )
  )
  (import "test:async/host@0.1.0" (instance $host (type $host_iface)))
  (alias export $host "yield" (func $host_yield))
  (core func $yield_lowered (canon lower (func $host_yield)))
  (core instance $env (export "yield" (func $yield_lowered)))

  (core module $core1
    (import "env" "yield" (func $yield (result i32)))
    (memory (export "memory") 2)
    (global $heap (export "heap") (mut i32) (i32.const 70000))
    (global $post (export "post") (mut i32) (i32.const 0))
    (global $entered (export "entered") (mut i32) (i32.const 0))
    (global $completed (export "completed") (mut i32) (i32.const 0))
    (global $token (export "token") (mut i32) (i32.const 0))
    (func (export "cabi_realloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      (local.set $ret (global.get $heap))
      (global.set $heap (i32.add (local.get $ret) (local.get $new_size)))
      (local.get $ret)
    )
    (func (export "cabi_post_func1") (param $ptr i32)
      (global.set $post (i32.add (global.get $post) (i32.const 1)))
    )
    (func (export "get_post") (result i32) (global.get $post))
    (func (export "get_heap") (result i32) (global.get $heap))
    (func (export "get_entered") (result i32) (global.get $entered))
    (func (export "get_completed") (result i32) (global.get $completed))
    (func (export "get_token") (result i32) (global.get $token))
    (func (export "func1") (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      (local $yielded i32)
      (global.set $entered (i32.add (global.get $entered) (i32.const 1)))
      (local.set $yielded (call $yield))
      (global.set $token (local.get $yielded))
      (global.set $completed (i32.add (global.get $completed) (i32.const 1)))
      (local.set $retptr (global.get $heap))
      (global.set $heap (i32.add (global.get $heap) (i32.const 8)))
      (i32.store (local.get $retptr) (local.get $ptr))
      (i32.store (i32.add (local.get $retptr) (i32.const 4)) (local.get $len))
      (local.get $retptr)
    )
  )

  (core module $core2
    (import "env" "yield" (func $yield (result i32)))
    (memory (export "memory") 3)
    (global $heap (export "heap") (mut i32) (i32.const 140000))
    (global $post (export "post") (mut i32) (i32.const 0))
    (global $entered (export "entered") (mut i32) (i32.const 0))
    (global $completed (export "completed") (mut i32) (i32.const 0))
    (global $token (export "token") (mut i32) (i32.const 0))
    (func (export "cabi_realloc") (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32) (result i32)
      (local $ret i32)
      (local.set $ret (global.get $heap))
      (global.set $heap (i32.add (local.get $ret) (local.get $new_size)))
      (local.get $ret)
    )
    (func (export "cabi_post_func2") (param $ptr i32)
      (global.set $post (i32.add (global.get $post) (i32.const 1)))
    )
    (func (export "get_post") (result i32) (global.get $post))
    (func (export "get_heap") (result i32) (global.get $heap))
    (func (export "get_completed") (result i32) (global.get $completed))
    (func (export "get_entered") (result i32) (global.get $entered))
    (func (export "get_token") (result i32) (global.get $token))
    (func (export "func2") (param $ptr i32) (param $len i32) (result i32)
      (local $retptr i32)
      (local $yielded i32)
      (global.set $entered (i32.add (global.get $entered) (i32.const 1)))
      (local.set $yielded (call $yield))
      (global.set $token (local.get $yielded))
      (global.set $completed (i32.add (global.get $completed) (i32.const 1)))
      (local.set $retptr (global.get $heap))
      (global.set $heap (i32.add (global.get $heap) (i32.const 8)))
      (i32.store (local.get $retptr) (local.get $ptr))
      (i32.store (i32.add (local.get $retptr) (i32.const 4)) (local.get $len))
      (local.get $retptr)
    )
  )

  (core instance $inst1 (instantiate $core1 (with "env" (instance $env))))
  (core instance $inst2 (instantiate $core2 (with "env" (instance $env))))

  (alias core export $inst1 "memory" (core memory $mem1))
  (alias core export $inst1 "cabi_realloc" (core func $realloc1))
  (alias core export $inst1 "func1" (core func $func1_core))
  (alias core export $inst1 "cabi_post_func1" (core func $post1_core))
  (alias core export $inst1 "get_post" (core func $get_post1_core))
  (alias core export $inst1 "get_heap" (core func $get_heap1_core))
  (alias core export $inst1 "get_entered" (core func $get_entered1_core))
  (alias core export $inst1 "get_completed" (core func $get_completed1_core))
  (alias core export $inst1 "get_token" (core func $get_token1_core))

  (alias core export $inst2 "memory" (core memory $mem2))
  (alias core export $inst2 "cabi_realloc" (core func $realloc2))
  (alias core export $inst2 "func2" (core func $func2_core))
  (alias core export $inst2 "cabi_post_func2" (core func $post2_core))
  (alias core export $inst2 "get_post" (core func $get_post2_core))
  (alias core export $inst2 "get_heap" (core func $get_heap2_core))
  (alias core export $inst2 "get_entered" (core func $get_entered2_core))
  (alias core export $inst2 "get_completed" (core func $get_completed2_core))
  (alias core export $inst2 "get_token" (core func $get_token2_core))

  (type $str_type (func (param "msg" string) (result string)))
  (type $u32_type (func (result u32)))

  (func $lift1 (type $str_type)
    (canon lift (core func $func1_core) (memory $mem1) (realloc $realloc1) (post-return (func $post1_core)))
  )
  (export "func1" (func $lift1))
  (func $lift2 (type $str_type)
    (canon lift (core func $func2_core) (memory $mem2) (realloc $realloc2) (post-return (func $post2_core)))
  )
  (export "func2" (func $lift2))
  (func $lift_get_post1 (type $u32_type) (canon lift (core func $get_post1_core)))
  (export "get-post1" (func $lift_get_post1))
  (func $lift_get_post2 (type $u32_type) (canon lift (core func $get_post2_core)))
  (export "get-post2" (func $lift_get_post2))
  (func $lift_get_heap1 (type $u32_type) (canon lift (core func $get_heap1_core)))
  (export "get-heap1" (func $lift_get_heap1))
  (func $lift_get_heap2 (type $u32_type) (canon lift (core func $get_heap2_core)))
  (export "get-heap2" (func $lift_get_heap2))
  (func $lift_get_entered1 (type $u32_type) (canon lift (core func $get_entered1_core)))
  (export "get-entered1" (func $lift_get_entered1))
  (func $lift_get_entered2 (type $u32_type) (canon lift (core func $get_entered2_core)))
  (export "get-entered2" (func $lift_get_entered2))
  (func $lift_get_completed1 (type $u32_type) (canon lift (core func $get_completed1_core)))
  (export "get-completed1" (func $lift_get_completed1))
  (func $lift_get_completed2 (type $u32_type) (canon lift (core func $get_completed2_core)))
  (export "get-completed2" (func $lift_get_completed2))
  (func $lift_get_token1 (type $u32_type) (canon lift (core func $get_token1_core)))
  (export "get-token1" (func $lift_get_token1))
  (func $lift_get_token2 (type $u32_type) (canon lift (core func $get_token2_core)))
  (export "get-token2" (func $lift_get_token2))
)
`

func compileTwoCoreYieldComponent(t *testing.T) []byte {
	t.Helper()
	return componentFixture(t, twoCoreYieldComponentWAT)
}

func loadTwoCoreYieldModule(t *testing.T) (*WazeroEngine, *WazeroModule) {
	t.Helper()
	wasmBytes := compileTwoCoreYieldComponent(t)
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}

	var mod *WazeroModule
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		mod, lastErr = eng.LoadModule(ctx, wasmBytes)
		if lastErr == nil {
			break
		}
		if !exportParseTransient(lastErr) {
			eng.Close(ctx)
			t.Fatalf("LoadModule: %v", lastErr)
		}
		time.Sleep(time.Second)
	}
	if lastErr != nil {
		eng.Close(ctx)
		t.Fatalf("LoadModule after parser wait: %v", lastErr)
	}

	err = mod.RegisterHostFuncRaw(
		"test:async/host@0.1.0",
		"yield",
		nil,
		[]api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp {
			return &traceOp{name: "yield", id: 1}
		}),
		true,
	)
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("RegisterHostFuncRaw yield: %v", err)
	}
	return eng, mod
}

func exportParseTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "export") || strings.Contains(msg, "externtype") || strings.Contains(msg, "option")
}

func instantiateTwoCoreYield(t *testing.T, mod *WazeroModule) *WazeroInstance {
	t.Helper()
	inst, err := mod.InstantiateWithConfig(context.Background(), &InstanceConfig{
		EnableAsyncify: true,
		// These bounded fixture heaps have room for a 1 KiB owned stack.
		AsyncifyStackBytes: 1024,
		AsyncifyImports:    []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	return inst
}

func coreGlobalU32(t *testing.T, mod api.Module, name string) uint32 {
	t.Helper()
	g := mod.ExportedGlobal(name)
	if g == nil {
		t.Fatalf("missing global %q", name)
	}
	return uint32(g.Get())
}

func asyncifyStackPtr(t *testing.T, binding *exportBinding) uint32 {
	mem := binding.memory
	t.Helper()
	if mem == nil {
		t.Fatal("missing core memory")
	}
	ptr, err := mem.ReadU32(binding.asyncify.dataAddr)
	if err != nil {
		t.Fatalf("read asyncify stack pointer: %v", err)
	}
	return ptr
}

func mustRejectWhileParked(t *testing.T, inst *WazeroInstance, name string, wantSubstr string) {
	t.Helper()
	ctx := context.Background()
	stringType := []wit.Type{wit.String{}}
	check := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s %s succeeded while parked", label, name)
		}
		if wantSubstr != "" && !strings.Contains(err.Error(), wantSubstr) {
			t.Fatalf("%s %s error %q, want substring %q", label, name, err.Error(), wantSubstr)
		}
	}
	_, err := inst.CallWithLift(ctx, name, "must-not-run")
	check("CallWithLift", err)
	_, err = inst.CallWithTypes(ctx, name, stringType, stringType, "must-not-run")
	check("CallWithTypes", err)
	var out string
	err = inst.CallInto(ctx, name, stringType, stringType, &out, "must-not-run")
	check("CallInto", err)
	_, err = inst.RunAsync(ctx, name, 0, 0)
	check("RunAsync", err)
	_, err = inst.StartCall(ctx, name, "must-not-run")
	check("StartCall", err)
}

func TestExportBindingAsync_RealMultiCoreResume(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreYieldModule(t)
	defer eng.Close(ctx)

	inst := instantiateTwoCoreYield(t, mod)
	defer inst.Close(ctx)

	b1, err := inst.getExportBinding("func1")
	if err != nil {
		t.Fatalf("getExportBinding func1: %v", err)
	}
	b2, err := inst.getExportBinding("func2")
	if err != nil {
		t.Fatalf("getExportBinding func2: %v", err)
	}
	if b1.coreMod == nil || b2.coreMod == nil || b1.coreMod == b2.coreMod {
		t.Fatal("expected distinct core modules")
	}
	if b1.memory == nil || b2.memory == nil || b1.memory.mem == b2.memory.mem {
		t.Fatal("expected distinct core memories")
	}
	if b1.asyncify == nil || b2.asyncify == nil || b1.asyncify == b2.asyncify {
		t.Fatal("expected distinct asyncify state")
	}
	if b1.scheduler == nil || b2.scheduler == nil || b1.scheduler == b2.scheduler {
		t.Fatal("expected distinct schedulers")
	}
	if b1.alloc == nil || b2.alloc == nil || b1.alloc == b2.alloc {
		t.Fatal("expected distinct allocators")
	}

	initial1 := asyncifyStackPtr(t, b1)
	initial2 := asyncifyStackPtr(t, b2)
	if initial1 != b1.asyncify.dataAddr+8 {
		t.Fatalf("core1 asyncify stack ptr = %d, want %d", initial1, b1.asyncify.dataAddr+8)
	}
	if initial2 != b2.asyncify.dataAddr+8 {
		t.Fatalf("core2 asyncify stack ptr = %d, want %d", initial2, b2.asyncify.dataAddr+8)
	}
	if coreGlobalU32(t, b1.coreMod, "heap") != 71032 {
		t.Fatalf("core1 heap = %d, want 71032", coreGlobalU32(t, b1.coreMod, "heap"))
	}
	if coreGlobalU32(t, b2.coreMod, "heap") != 141032 {
		t.Fatalf("core2 heap = %d, want 141032", coreGlobalU32(t, b2.coreMod, "heap"))
	}

	cs1, err := inst.StartCall(ctx, "func1", "hello-core1")
	if err != nil {
		t.Fatalf("StartCall func1: %v", err)
	}
	if cs1.asyncify != b1.asyncify || cs1.scheduler != b1.scheduler {
		t.Fatal("func1 session bound to the wrong core asyncify/scheduler")
	}
	if coreGlobalU32(t, b1.coreMod, "heap") <= 71032 {
		t.Fatal("StartCall func1 did not allocate into core1 high-offset heap")
	}
	if coreGlobalU32(t, b2.coreMod, "heap") != 141032 {
		t.Fatalf("core2 heap moved during core1 StartCall: %d", coreGlobalU32(t, b2.coreMod, "heap"))
	}

	t.Run("overlapping prepared sessions", func(t *testing.T) {
		_, err := inst.StartCall(ctx, "func2", "must-not-prepare")
		if err == nil {
			t.Fatal("second StartCall overwrote a prepared session; required production fix: keep prepared-session ownership on StartCall (set activeSession before return) and reject another StartCall until LiftResult/abort. Retain allocList on CallSession until lift or abort; do not Release it on StartCall success.")
		}
		if !strings.Contains(err.Error(), "switching core while suspended is not supported") &&
			!strings.Contains(err.Error(), "resume or complete active call") {
			t.Fatalf("overlapping StartCall error = %v", err)
		}
	})

	step1, err := cs1.Step(ctx, nil)
	if err != nil {
		t.Fatalf("func1 Step: %v", err)
	}
	if step1.Status != StepContinue {
		t.Fatalf("func1 Step status = %v, want StepContinue", step1.Status)
	}
	if step1.PendingOp == nil || step1.PendingOp.CmdID() != 1 {
		t.Fatalf("func1 pending op = %v", step1.PendingOp)
	}
	if coreGlobalU32(t, b1.coreMod, "entered") != 1 {
		t.Fatalf("core1 entered after yield = %d, want 1", coreGlobalU32(t, b1.coreMod, "entered"))
	}
	if coreGlobalU32(t, b1.coreMod, "completed") != 0 {
		t.Fatalf("core1 completed during unwind = %d, want 0", coreGlobalU32(t, b1.coreMod, "completed"))
	}
	if coreGlobalU32(t, b2.coreMod, "entered") != 0 {
		t.Fatalf("core2 entered during core1 suspend = %d, want 0", coreGlobalU32(t, b2.coreMod, "entered"))
	}
	parked1 := asyncifyStackPtr(t, b1)
	if parked1 <= initial1 {
		t.Fatalf("core1 asyncify stack ptr = %d, want > %d after unwind", parked1, initial1)
	}
	if asyncifyStackPtr(t, b2) != initial2 {
		t.Fatalf("core2 asyncify stack ptr moved during core1 suspend: %d", asyncifyStackPtr(t, b2))
	}

	mustRejectWhileParked(t, inst, "func2", "switching core while suspended is not supported")
	mustRejectWhileParked(t, inst, "func1", "resume or complete active call")
	if coreGlobalU32(t, b2.coreMod, "entered") != 0 || coreGlobalU32(t, b2.coreMod, "completed") != 0 {
		t.Fatal("rejected core2 API still executed guest side effects")
	}

	resume1, err := cs1.Step(ctx, &YieldResult{Value: 41})
	if err != nil {
		t.Fatalf("func1 resume: %v", err)
	}
	if resume1.Status != StepDone {
		t.Fatalf("func1 resume status = %v, want StepDone", resume1.Status)
	}
	got1, err := cs1.LiftResult(ctx, resume1.Results)
	if err != nil {
		t.Fatalf("func1 LiftResult: %v", err)
	}
	if got1 != "hello-core1" {
		t.Fatalf("func1 LiftResult = %q, want %q", got1, "hello-core1")
	}
	if coreGlobalU32(t, b1.coreMod, "completed") != 1 {
		t.Fatalf("core1 completed after resume = %d, want 1", coreGlobalU32(t, b1.coreMod, "completed"))
	}
	if coreGlobalU32(t, b1.coreMod, "token") != 41 {
		t.Fatalf("core1 yield token = %d, want 41", coreGlobalU32(t, b1.coreMod, "token"))
	}
	post1, err := inst.CallWithLift(ctx, "get-post1")
	if err != nil {
		t.Fatalf("get-post1: %v", err)
	}
	if post1.(uint32) != 1 {
		t.Fatalf("post1 = %d, want 1", post1)
	}
	post2, err := inst.CallWithLift(ctx, "get-post2")
	if err != nil {
		t.Fatalf("get-post2: %v", err)
	}
	if post2.(uint32) != 0 {
		t.Fatalf("post2 = %d, want 0", post2)
	}
	entered2, err := inst.CallWithLift(ctx, "get-entered2")
	if err != nil {
		t.Fatalf("get-entered2: %v", err)
	}
	if entered2.(uint32) != 0 {
		t.Fatalf("entered2 after core1 complete = %d, want 0", entered2)
	}

	cs2, err := inst.StartCall(ctx, "func2", "greetings-from-core2")
	if err != nil {
		t.Fatalf("StartCall func2: %v", err)
	}
	if cs2.asyncify != b2.asyncify || cs2.scheduler != b2.scheduler {
		t.Fatal("func2 session bound to the wrong core asyncify/scheduler")
	}
	if coreGlobalU32(t, b2.coreMod, "heap") <= 141032 {
		t.Fatal("StartCall func2 did not allocate into core2 high-offset heap")
	}

	step2, err := cs2.Step(ctx, nil)
	if err != nil {
		t.Fatalf("func2 Step: %v", err)
	}
	if step2.Status != StepContinue {
		t.Fatalf("func2 Step status = %v, want StepContinue", step2.Status)
	}
	if coreGlobalU32(t, b2.coreMod, "entered") != 1 {
		t.Fatalf("core2 entered after yield = %d, want 1", coreGlobalU32(t, b2.coreMod, "entered"))
	}
	if coreGlobalU32(t, b2.coreMod, "completed") != 0 {
		t.Fatalf("core2 completed during unwind = %d, want 0", coreGlobalU32(t, b2.coreMod, "completed"))
	}
	if asyncifyStackPtr(t, b2) <= initial2 {
		t.Fatalf("core2 asyncify stack ptr = %d, want > %d after unwind", asyncifyStackPtr(t, b2), initial2)
	}

	mustRejectWhileParked(t, inst, "func1", "switching core while suspended is not supported")

	resume2, err := cs2.Step(ctx, &YieldResult{Value: 42})
	if err != nil {
		t.Fatalf("func2 resume: %v", err)
	}
	if resume2.Status != StepDone {
		t.Fatalf("func2 resume status = %v, want StepDone", resume2.Status)
	}
	got2, err := cs2.LiftResult(ctx, resume2.Results)
	if err != nil {
		t.Fatalf("func2 LiftResult: %v", err)
	}
	if got2 != "greetings-from-core2" {
		t.Fatalf("func2 LiftResult = %q, want %q", got2, "greetings-from-core2")
	}
	if coreGlobalU32(t, b2.coreMod, "token") != 42 {
		t.Fatalf("core2 yield token = %d, want 42", coreGlobalU32(t, b2.coreMod, "token"))
	}
	if coreGlobalU32(t, b1.coreMod, "token") != 41 {
		t.Fatalf("core1 token clobbered by core2 resume: %d", coreGlobalU32(t, b1.coreMod, "token"))
	}
	post2After, err := inst.CallWithLift(ctx, "get-post2")
	if err != nil {
		t.Fatalf("get-post2 after resume: %v", err)
	}
	if post2After.(uint32) != 1 {
		t.Fatalf("post2 after resume = %d, want 1", post2After)
	}
}

func TestExportBindingAsync_InitErrorPropagates(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreYieldModule(t)
	defer eng.Close(ctx)

	inst := instantiateTwoCoreYield(t, mod)
	defer inst.Close(ctx)

	b1, err := inst.getExportBinding("func1")
	if err != nil {
		t.Fatalf("getExportBinding func1: %v", err)
	}
	if b1.asyncify == nil || b1.scheduler == nil {
		t.Fatal("func1 asyncify init did not propagate onto the export binding")
	}
	if !b1.asyncify.trusted {
		t.Fatal("func1 asyncify missing embedded-transform provenance")
	}
	b2, err := inst.getExportBinding("func2")
	if err != nil {
		t.Fatalf("getExportBinding func2: %v", err)
	}
	if b2.asyncify == nil || b2.scheduler == nil {
		t.Fatal("func2 asyncify init did not propagate onto the export binding")
	}
	if b1.scheduler == inst.scheduler && b1.coreMod != inst.instance {
		t.Fatal("func1 fell back to instance scheduler for the wrong core")
	}
	if b2.scheduler == inst.scheduler && b2.coreMod != inst.instance {
		t.Fatal("func2 fell back to instance scheduler for the wrong core")
	}
}
