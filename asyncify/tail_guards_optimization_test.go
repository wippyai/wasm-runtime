package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// TestTailGuards_EliminatesGuardsAfterLastCall verifies that non-control-flow
// instructions and branches occurring after the last async call site do not have
// redundant StateNormal guard checks.
func TestTailGuards_EliminatesGuardsAfterLastCall(t *testing.T) {
	src := `(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "test") (param $x i32) (result i32)
			;; Code BEFORE async call: must be guarded
			(local.set $x (i32.add (local.get $x) (i32.const 1)))
			(call $yield (local.get $x))
			;; Code AFTER last async call: must NOT have StateNormal checks
			(local.set $x (i32.add (local.get $x) (i32.const 10)))
			(local.set $x (i32.mul (local.get $x) (i32.const 2)))
			(local.get $x)
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(src)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		AsyncImports: []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	mod, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("wasm.ParseModule failed: %v", err)
	}

	body := mod.Code[0]
	instrs, err := wasm.DecodeInstructions(body.Code)
	if err != nil {
		t.Fatalf("wasm.DecodeInstructions failed: %v", err)
	}

	// Locate the call to $yield in transformed body
	callIdx := -1
	for i, instr := range instrs {
		if instr.Opcode == wasm.OpCall && instr.Imm.(wasm.CallImm).FuncIdx == 0 {
			callIdx = i
			break
		}
	}
	if callIdx == -1 {
		t.Fatalf("failed to locate call to yield in transformed instructions")
	}

	// Bounded structural assertion: instructions after the last async call site
	// must NOT be wrapped in StateNormal (state == 0) guards.
	stateNormalGetsAfterCall := 0
	for i := callIdx + 1; i+1 < len(instrs); i++ {
		if instrs[i].Opcode == wasm.OpGlobalGet {
			if imm, ok := instrs[i].Imm.(wasm.GlobalImm); ok && imm.GlobalIdx == 0 {
				if instrs[i+1].Opcode == wasm.OpI32Const {
					if cimm, ok := instrs[i+1].Imm.(wasm.I32Imm); ok && cimm.Value == 0 {
						stateNormalGetsAfterCall++
					}
				}
			}
		}
	}
	if stateNormalGetsAfterCall != 0 {
		t.Errorf("found %d StateNormal checks after last async call site, want 0", stateNormalGetsAfterCall)
	}

	// Behavioral verification: execute with suspend and resume.
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	suspended := false
	var observedYieldArg int32

	_, err = r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, val int32) {
			observedYieldArg = val
			fnGetState := m.ExportedFunction("asyncify_get_state")
			st, err := fnGetState.Call(ctx)
			if err != nil {
				t.Fatalf("get_state failed: %v", err)
			}
			state := st[0]
			switch state {
			case 0:
				if !suspended {
					suspended = true
					if !m.Memory().WriteUint32Le(1024, 1032) || !m.Memory().WriteUint32Le(1028, 8192) {
						t.Fatalf("failed to write async stack")
					}
					fnStartUnwind := m.ExportedFunction("asyncify_start_unwind")
					if _, err := fnStartUnwind.Call(ctx, 1024); err != nil {
						t.Fatalf("start_unwind failed: %v", err)
					}
				}
			case 2:
				fnStopRewind := m.ExportedFunction("asyncify_stop_rewind")
				if _, err := fnStopRewind.Call(ctx); err != nil {
					t.Fatalf("stop_rewind failed: %v", err)
				}
			}
		}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host: %v", err)
	}

	compiled, err := r.CompileModule(ctx, transformed)
	if err != nil {
		t.Fatalf("compile transformed: %v", err)
	}

	inst, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}

	// Initial call with x = 5 suspends at yield(5 + 1 = 6)
	_, err = inst.ExportedFunction("test").Call(ctx, 5)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if observedYieldArg != 6 {
		t.Fatalf("unexpected yield arg: got %d, want 6", observedYieldArg)
	}

	fnStopUnwind := inst.ExportedFunction("asyncify_stop_unwind")
	if _, err := fnStopUnwind.Call(ctx); err != nil {
		t.Fatalf("stop_unwind failed: %v", err)
	}

	// Resume: should evaluate (6 + 10) * 2 = 32
	fnStartRewind := inst.ExportedFunction("asyncify_start_rewind")
	if _, err := fnStartRewind.Call(ctx, 1024); err != nil {
		t.Fatalf("start_rewind failed: %v", err)
	}

	res, err := inst.ExportedFunction("test").Call(ctx, 5)
	if err != nil {
		t.Fatalf("resumed call failed: %v", err)
	}
	if len(res) != 1 || res[0] != 32 {
		t.Fatalf("unexpected resumed result: got %v, want [32]", res)
	}
}

// TestTailGuards_ControlFlowLoopsAndBranchesAfterCall verifies that loops,
// br, br_if, and returns after the last async call execute correctly and yield valid results.
func TestTailGuards_ControlFlowLoopsAndBranchesAfterCall(t *testing.T) {
	ctx := context.Background()

	src := `(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "test") (param $n i32) (result i32)
			(local $sum i32)
			(local $i i32)
			(call $yield (i32.const 42))
			;; Loop with branch after async call
			(local.set $sum (i32.const 0))
			(local.set $i (i32.const 0))
			(loop $l
				(local.set $sum (i32.add (local.get $sum) (local.get $i)))
				(local.set $i (i32.add (local.get $i) (i32.const 1)))
				(br_if $l (i32.lt_s (local.get $i) (local.get $n)))
			)
			(return (local.get $sum))
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(src)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		AsyncImports: []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	suspended := false

	_, err = r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, val int32) {
			fnGetState := mod.ExportedFunction("asyncify_get_state")
			st, err := fnGetState.Call(ctx)
			if err != nil {
				t.Fatalf("get_state failed: %v", err)
			}
			state := st[0]
			switch state {
			case 0: // Normal: suspend once
				if !suspended {
					suspended = true
					if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 8192) {
						t.Fatalf("failed to write async stack")
					}
					fnStartUnwind := mod.ExportedFunction("asyncify_start_unwind")
					if _, err := fnStartUnwind.Call(ctx, 1024); err != nil {
						t.Fatalf("start_unwind failed: %v", err)
					}
				}
			case 2: // Rewinding: stop rewind
				fnStopRewind := mod.ExportedFunction("asyncify_stop_rewind")
				if _, err := fnStopRewind.Call(ctx); err != nil {
					t.Fatalf("stop_rewind failed: %v", err)
				}
			}
		}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host: %v", err)
	}

	compiled, err := r.CompileModule(ctx, transformed)
	if err != nil {
		t.Fatalf("compile transformed: %v", err)
	}

	inst, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}

	// 1. Initial call suspends
	_, err = inst.ExportedFunction("test").Call(ctx, 10)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}

	fnStopUnwind := inst.ExportedFunction("asyncify_stop_unwind")
	if _, err := fnStopUnwind.Call(ctx); err != nil {
		t.Fatalf("stop_unwind failed: %v", err)
	}

	// 2. Resume
	fnStartRewind := inst.ExportedFunction("asyncify_start_rewind")
	if _, err := fnStartRewind.Call(ctx, 1024); err != nil {
		t.Fatalf("start_rewind failed: %v", err)
	}

	res2, err := inst.ExportedFunction("test").Call(ctx, 10)
	if err != nil {
		t.Fatalf("resumed call failed: %v", err)
	}
	if len(res2) != 1 || res2[0] != 45 { // 0+1+2+...+9 = 45
		t.Fatalf("unexpected result: got %v, want [45]", res2)
	}
}

// TestTailGuards_MultiValueReturnAfterCall verifies that multi-value returns
// work cleanly after an async call without guard wrappers.
func TestTailGuards_MultiValueReturnAfterCall(t *testing.T) {
	ctx := context.Background()

	src := `(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "test") (result i32 i64)
			(call $yield (i32.const 1))
			(i32.const 123)
			(i64.const 456)
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(src)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		AsyncImports: []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	_, err = r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, val int32) {}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host: %v", err)
	}

	mod, err := r.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	res, err := mod.ExportedFunction("test").Call(ctx)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(res) != 2 || res[0] != 123 || res[1] != 456 {
		t.Fatalf("unexpected results: got %v, want [123 456]", res)
	}
}

// TestTailGuards_TrapAfterAsyncCall verifies that unreachables/traps after the
// last async call still produce traps as expected.
func TestTailGuards_TrapAfterAsyncCall(t *testing.T) {
	ctx := context.Background()

	src := `(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "test") (param $cond i32) (result i32)
			(call $yield (i32.const 1))
			(if (local.get $cond)
				(then (unreachable))
			)
			(i32.const 42)
		)
		(memory (export "memory") 1)
	)`

	rawWasm, err := wat.Compile(src)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	transformed, err := asyncify.Transform(rawWasm, asyncify.Config{
		AsyncImports: []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("asyncify.Transform failed: %v", err)
	}

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	_, err = r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, val int32) {}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host: %v", err)
	}

	mod, err := r.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	// 1. Cond = 0: succeeds
	res, err := mod.ExportedFunction("test").Call(ctx, 0)
	if err != nil {
		t.Fatalf("expected success with cond=0, got: %v", err)
	}
	if len(res) != 1 || res[0] != 42 {
		t.Fatalf("want 42, got %v", res)
	}

	// 2. Cond = 1: traps
	_, err = mod.ExportedFunction("test").Call(ctx, 1)
	if err == nil {
		t.Fatalf("expected trap with cond=1, got nil error")
	}
}

// BenchmarkTailGuards_Transform measures transformation throughput on functions
// with long post-call instruction tails.
func BenchmarkTailGuards_Transform(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`(module
		(import "env" "yield" (func $yield (param i32)))
		(func (export "test") (param $x i32) (result i32)
			(call $yield (local.get $x))
	`)
	for i := 0; i < 500; i++ {
		sb.WriteString("			(local.set $x (i32.add (local.get $x) (i32.const 1)))\n")
		sb.WriteString("			(if (i32.eqz (local.get $x)) (then (return (i32.const 0))))\n")
	}
	sb.WriteString(`			(local.get $x)
		)
		(memory (export "memory") 1)
	)`)

	rawWasm, err := wat.Compile(sb.String())
	if err != nil {
		b.Fatalf("wat.Compile: %v", err)
	}

	cfg := asyncify.Config{
		AsyncImports: []string{"env.yield"},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := asyncify.Transform(rawWasm, cfg)
		if err != nil {
			b.Fatalf("transform failed: %v", err)
		}
	}
}
