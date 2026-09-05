package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

type traceOp struct {
	name string
	id   CommandID
}

func (o *traceOp) CmdID() CommandID { return o.id }

func (o *traceOp) Execute(context.Context) (uint64, error) { return 0, nil }

func TestRepeatSuspend_LoopRecvSend(t *testing.T) {
	watSrc := `(module
		(import "env" "recv" (func $recv (result i32)))
		(import "env" "send" (func $send (param i32) (result i32)))
		(func (export "run")
			(local $msg i32)
			(local $count i32)
			(loop $l
				(local.set $msg (call $recv))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(local.set $count (i32.add (local.get $count) (i32.const 1)))
				(drop (call $send (local.get $count)))
				(br $l)))
		(memory 1))`

	runRepeatSuspend(t, watSrc, []string{"env.recv", "env.send"})
}

func TestRepeatSuspend_BranchBeforeSend(t *testing.T) {
	watSrc := `(module
		(import "env" "recv" (func $recv (result i32)))
		(import "env" "send" (func $send (param i32) (result i32)))
		(func (export "run")
			(local $msg i32)
			(local $count i32)
			(loop $l
				(local.set $msg (call $recv))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(local.set $count (i32.add (local.get $count) (i32.const 1)))
				(block $outer
                    (block $inner
                       (br_if $inner (i32.const 1))
                       (br $outer))
                    (drop (call $send (local.get $count))))
				(br $l)))
		(memory 1))`

	runRepeatSuspend(t, watSrc, []string{"env.recv", "env.send"})
}

func TestRepeatSuspend_TruthyBranchBeforeSend(t *testing.T) {
	watSrc := `(module
		(import "env" "recv" (func $recv (result i32)))
		(import "env" "send" (func $send (param i32) (result i32)))
		(func (export "run")
			(local $msg i32)
			(local $count i32)
			(loop $l
				(local.set $msg (call $recv))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(local.set $count (i32.add (local.get $count) (i32.const 1)))
				(block $outer
                    (block $inner
                       (br_if $inner (i32.const 2))
                       (br $outer))
                    (drop (call $send (local.get $count))))
				(br $l)))
		(memory 1))`

	runRepeatSuspend(t, watSrc, []string{"env.recv", "env.send"})
}

func TestRepeatSuspend_ConditionalSend(t *testing.T) {
	watSrc := `(module
		(import "env" "recv" (func $recv (result i32)))
		(import "env" "send" (func $send (param i32) (result i32)))
		(func (export "run")
			(local $msg i32)
			(local $count i32)
			(loop $l
				(local.set $msg (call $recv))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(local.set $count (i32.add (local.get $count) (i32.const 1)))
				(if (i32.gt_u (local.get $count) (i32.const 0))
                    (then (drop (call $send (local.get $count)))))
				(br $l)))
		(memory 1))`

	runRepeatSuspend(t, watSrc, []string{"env.recv", "env.send"})
}

func TestRepeatSuspend_NestedHelpers(t *testing.T) {
	watSrc := `(module
		(import "env" "recv" (func $recv (result i32)))
		(import "env" "send" (func $send (param i32) (result i32)))
		(func $do_recv (result i32)
			(call $recv))
		(func $do_send (param i32) (result i32)
			(call $send (local.get 0)))
		(func (export "run")
			(local $msg i32)
			(local $count i32)
			(loop $l
				(local.set $msg (call $do_recv))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(local.set $count (i32.add (local.get $count) (i32.const 1)))
				(drop (call $do_send (local.get $count)))
				(br $l)))
		(memory 1))`

	runRepeatSuspend(t, watSrc, []string{"env.recv", "env.send"})
}

func TestRepeatSuspend_BlockLoop(t *testing.T) {
	watSrc := `(module
		(import "env" "recv" (func $recv (result i32)))
		(import "env" "send" (func $send (param i32) (result i32)))
		(func (export "run")
			(local $msg i32)
			(local $count i32)
			(block $out
				(loop $l
					(local.set $msg (call $recv))
					(br_if $out (i32.eq (local.get $msg) (i32.const 99)))
					(local.set $count (i32.add (local.get $count) (i32.const 1)))
					(drop (call $send (local.get $count)))
					(br $l))))
		(memory 1))`

	runRepeatSuspend(t, watSrc, []string{"env.recv", "env.send"})
}

func runRepeatSuspend(t *testing.T, watSrc string, asyncImports []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: asyncImports})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	t.Cleanup(func() { _ = rt.Close(ctx) })

	var calls []string
	recvResults := []uint64{1, 2, 3, 99}
	var recvIdx int
	var sendArgs []uint64

	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(MakeAsyncHandler(func(ctx context.Context, _ api.Module, stack []uint64) PendingOp {
			calls = append(calls, "recv")
			return &traceOp{name: "recv", id: 1}
		}), nil, []api.ValueType{api.ValueTypeI32}).
		Export("recv").
		NewFunctionBuilder().
		WithGoModuleFunction(MakeAsyncHandler(func(ctx context.Context, _ api.Module, stack []uint64) PendingOp {
			calls = append(calls, fmt.Sprintf("send:%d", stack[0]))
			sendArgs = append(sendArgs, stack[0])
			return &traceOp{name: "send", id: 2}
		}), []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("send").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("host module: %v", err)
	}

	mod, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	t.Cleanup(func() { _ = mod.Close(ctx) })

	a := NewAsyncify()
	if err := a.Init(mod); err != nil {
		t.Fatalf("asyncify init: %v", err)
	}
	s := NewScheduler(a)
	ctx = WithAsyncify(ctx, a)
	ctx = WithScheduler(ctx, s)

	fn := mod.ExportedFunction("run")
	if fn == nil {
		t.Fatal("missing run export")
	}
	if err := s.Execute(ctx, fn); err != nil {
		t.Fatalf("execute: %v", err)
	}

	step := func(yr *YieldResult) StepResult {
		t.Helper()
		sr, err := s.Step(ctx, yr)
		if err != nil {
			t.Fatalf("step after calls %v state=%d: %v", calls, a.GetState(ctx), err)
		}
		return sr
	}

	sr := step(nil)
	if sr.Status != StepContinue || sr.PendingOp == nil || sr.PendingOp.CmdID() != 1 {
		t.Fatalf("initial recv park: status=%v op=%v calls=%v", sr.Status, sr.PendingOp, calls)
	}

	for i := uint64(1); i <= 3; i++ {
		sr = step(&YieldResult{Value: recvResults[recvIdx]})
		recvIdx++
		if sr.Status != StepContinue || sr.PendingOp == nil || sr.PendingOp.CmdID() != 2 {
			t.Fatalf("recv %d should park send: status=%v op=%v calls=%v state=%d",
				i, sr.Status, sr.PendingOp, calls, a.GetState(ctx))
		}
		if len(sendArgs) < int(i) || sendArgs[i-1] != i {
			t.Fatalf("send arg after recv %d: %v", i, sendArgs)
		}

		sr = step(&YieldResult{Value: 1})
		if sr.Status != StepContinue || sr.PendingOp == nil || sr.PendingOp.CmdID() != 1 {
			t.Fatalf("send %d should park recv: status=%v op=%v calls=%v state=%d",
				i, sr.Status, sr.PendingOp, calls, a.GetState(ctx))
		}
	}

	sr = step(&YieldResult{Value: 99})
	if sr.Status != StepDone {
		t.Fatalf("stop should finish: status=%v calls=%v", sr.Status, calls)
	}
}

func TestRepeatSuspend_Retptr(t *testing.T) {
	watSrc := `(module
		(import "env" "recv" (func $recv (param i32)))
		(import "env" "send" (func $send (param i32 i32)))
		(memory (export "memory") 1)
		(func (export "run")
			(local $msg i32)
			(local $buf i32)
			(local.set $buf (i32.const 256))
			(loop $l
				(call $recv (local.get $buf))
				(local.set $msg (i32.load (local.get $buf)))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(call $send (local.get $msg) (local.get $buf))
				(br $l))))`
	runRepeatSuspendRetptr(t, watSrc, []string{"env.recv", "env.send"}, "", nil)
}

func TestRepeatSuspend_ShimIndirect(t *testing.T) {
	shimWat := `(module
		(type $t_recv (func (result i32)))
		(type $t_send (func (param i32) (result i32)))
		(import "env" "recv" (func $recv (result i32)))
		(import "env" "send" (func $send (param i32) (result i32)))
		(table (export "$imports") 2 funcref)
		(elem (i32.const 0) $recv $send)
		(func (export "0") (result i32)
			(call_indirect (type $t_recv) (i32.const 0)))
		(func (export "1") (param i32) (result i32)
			(call_indirect (type $t_send) (local.get 0) (i32.const 1))))`
	mainWat := `(module
		(import "shim" "0" (func $recv (result i32)))
		(import "shim" "1" (func $send (param i32) (result i32)))
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			(i32.const 1024))
		(func (export "run")
			(local $msg i32)
			(local $count i32)
			(loop $l
				(local.set $msg (call $recv))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(local.set $count (i32.add (local.get $count) (i32.const 1)))
				(drop (call $send (local.get $count)))
				(br $l))))`
	runRepeatSuspendTwoModule(t, mainWat, shimWat)
}

func TestRepeatSuspend_ShimRetptr(t *testing.T) {
	shimWat := `(module
		(type $t_recv (func (param i32)))
		(type $t_send (func (param i32 i32)))
		(import "env" "recv" (func $recv (param i32)))
		(import "env" "send" (func $send (param i32 i32)))
		(table (export "$imports") 2 funcref)
		(elem (i32.const 0) $recv $send)
		(func (export "0") (param i32)
			(call_indirect (type $t_recv) (local.get 0) (i32.const 0)))
		(func (export "1") (param i32 i32)
			(call_indirect (type $t_send) (local.get 0) (local.get 1) (i32.const 1))))`
	mainWat := `(module
		(import "shim" "0" (func $recv (param i32)))
		(import "shim" "1" (func $send (param i32 i32)))
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			(i32.const 1024))
		(func (export "run")
			(local $msg i32)
			(local $buf i32)
			(local.set $buf (i32.const 256))
			(loop $l
				(call $recv (local.get $buf))
				(local.set $msg (i32.load (local.get $buf)))
				(if (i32.eq (local.get $msg) (i32.const 99))
					(then (return)))
				(call $send (local.get $msg) (local.get $buf))
				(br $l))))`
	runRepeatSuspendRetptr(t, mainWat, []string{"shim.0", "shim.1", "env.recv", "env.send"}, shimWat, []string{"env.recv", "env.send"})
}

func transformModule(t *testing.T, watSrc string, asyncImports []string) []byte {
	t.Helper()
	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: asyncImports})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	return transformed
}

func runRepeatSuspendTwoModule(t *testing.T, mainWat, shimWat string) {
	t.Helper()
	ctx := context.Background()
	shimBytes := transformModule(t, shimWat, []string{"env.recv", "env.send"})
	mainBytes := transformModule(t, mainWat, []string{"shim.0", "shim.1"})

	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = rt.Close(ctx) })

	var calls []string
	recvResults := []uint64{1, 2, 3, 99}
	var recvIdx int
	var sendArgs []uint64

	_, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(MakeAsyncHandler(func(ctx context.Context, _ api.Module, stack []uint64) PendingOp {
			calls = append(calls, "recv")
			return &traceOp{name: "recv", id: 1}
		}), nil, []api.ValueType{api.ValueTypeI32}).
		Export("recv").
		NewFunctionBuilder().
		WithGoModuleFunction(MakeAsyncHandler(func(ctx context.Context, _ api.Module, stack []uint64) PendingOp {
			calls = append(calls, fmt.Sprintf("send:%d", stack[0]))
			sendArgs = append(sendArgs, stack[0])
			return &traceOp{name: "send", id: 2}
		}), []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("send").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("host module: %v", err)
	}

	shimMod, err := rt.InstantiateWithConfig(ctx, shimBytes, wazero.NewModuleConfig().WithName("shim"))
	if err != nil {
		t.Fatalf("instantiate shim: %v", err)
	}
	t.Cleanup(func() { _ = shimMod.Close(ctx) })

	mod, err := rt.InstantiateWithConfig(ctx, mainBytes, wazero.NewModuleConfig().WithName("main"))
	if err != nil {
		t.Fatalf("instantiate main: %v", err)
	}
	t.Cleanup(func() { _ = mod.Close(ctx) })

	driveRecvSendLoop(ctx, t, mod, &calls, &recvIdx, recvResults, &sendArgs)
}

func runRepeatSuspendRetptr(t *testing.T, mainWat string, mainAsync []string, shimWat string, shimAsync []string) {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = rt.Close(ctx) })

	var calls []string
	recvResults := []uint64{1, 2, 3, 99}
	var recvIdx int
	var sendArgs []uint64
	var guestMem api.Memory

	recvHost := func(ctx context.Context, mod api.Module, stack []uint64) {
		retptr := uint32(stack[0])
		calls = append(calls, fmt.Sprintf("recv rewind=%v unwind=%v retptr=%d",
			GetAsyncify(ctx).IsRewinding(ctx), GetAsyncify(ctx).IsUnwinding(ctx), retptr))
		mem := guestMem
		if mem == nil {
			mem = mod.Memory()
		}
		if GetAsyncify(ctx).IsRewinding(ctx) {
			val, err := Resume(ctx)
			if err != nil {
				panic(err)
			}
			if mem != nil {
				if !mem.WriteUint32Le(retptr, uint32(val)) {
					panic("invalid return pointer")
				}
			}
			return
		}
		if err := Suspend(ctx, &traceOp{name: "recv", id: 1}); err != nil {
			panic(err)
		}
	}
	sendHost := func(ctx context.Context, mod api.Module, stack []uint64) {
		arg := stack[0]
		calls = append(calls, fmt.Sprintf("send:%d rewind=%v", arg, GetAsyncify(ctx).IsRewinding(ctx)))
		if GetAsyncify(ctx).IsRewinding(ctx) {
			_, err := Resume(ctx)
			if err != nil {
				panic(err)
			}
			return
		}
		sendArgs = append(sendArgs, arg)
		if err := Suspend(ctx, &traceOp{name: "send", id: 2}); err != nil {
			panic(err)
		}
	}

	_, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(recvHost), []api.ValueType{api.ValueTypeI32}, nil).
		Export("recv").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(sendHost), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("send").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("host module: %v", err)
	}

	var mainMod api.Module
	if shimWat != "" {
		shimBytes := transformModule(t, shimWat, shimAsync)
		shimMod, err := rt.InstantiateWithConfig(ctx, shimBytes, wazero.NewModuleConfig().WithName("shim"))
		if err != nil {
			t.Fatalf("instantiate shim: %v", err)
		}
		t.Cleanup(func() { _ = shimMod.Close(ctx) })
	}
	mainBytes := transformModule(t, mainWat, mainAsync)
	mainMod, err = rt.InstantiateWithConfig(ctx, mainBytes, wazero.NewModuleConfig().WithName("main"))
	if err != nil {
		t.Fatalf("instantiate main: %v", err)
	}
	t.Cleanup(func() { _ = mainMod.Close(ctx) })
	guestMem = mainMod.Memory()

	driveRecvSendLoop(ctx, t, mainMod, &calls, &recvIdx, recvResults, &sendArgs)
}

func driveRecvSendLoop(ctx context.Context, t *testing.T, mod api.Module, calls *[]string, recvIdx *int, recvResults []uint64, sendArgs *[]uint64) {
	t.Helper()
	a := NewAsyncify()
	if err := a.Init(mod); err != nil {
		t.Fatalf("asyncify init: %v", err)
	}
	s := NewScheduler(a)
	ctx = WithAsyncify(ctx, a)
	ctx = WithScheduler(ctx, s)

	fn := mod.ExportedFunction("run")
	if fn == nil {
		t.Fatal("missing run export")
	}
	if err := s.Execute(ctx, fn); err != nil {
		t.Fatalf("execute: %v", err)
	}

	step := func(yr *YieldResult) StepResult {
		t.Helper()
		sr, err := s.Step(ctx, yr)
		if err != nil {
			t.Fatalf("step after calls %v state=%d: %v", *calls, a.GetState(ctx), err)
		}
		return sr
	}

	sr := step(nil)
	if sr.Status != StepContinue || sr.PendingOp == nil || sr.PendingOp.CmdID() != 1 {
		t.Fatalf("initial recv park: status=%v op=%v calls=%v", sr.Status, sr.PendingOp, *calls)
	}

	for i := uint64(1); i <= 3; i++ {
		sr = step(&YieldResult{Value: recvResults[*recvIdx]})
		*recvIdx++
		if sr.Status != StepContinue || sr.PendingOp == nil || sr.PendingOp.CmdID() != 2 {
			t.Fatalf("recv %d should park send: status=%v op=%v calls=%v state=%d",
				i, sr.Status, sr.PendingOp, *calls, a.GetState(ctx))
		}
		if len(*sendArgs) < int(i) || (*sendArgs)[i-1] != i {
			t.Fatalf("send arg after recv %d: %v calls=%v", i, *sendArgs, *calls)
		}

		sr = step(&YieldResult{Value: 1})
		if sr.Status != StepContinue || sr.PendingOp == nil || sr.PendingOp.CmdID() != 1 {
			t.Fatalf("send %d should park recv: status=%v op=%v calls=%v state=%d",
				i, sr.Status, sr.PendingOp, *calls, a.GetState(ctx))
		}
	}

	sr = step(&YieldResult{Value: 99})
	if sr.Status != StepDone {
		t.Fatalf("stop should finish: status=%v calls=%v", sr.Status, *calls)
	}
}
