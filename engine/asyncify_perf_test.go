package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// TestAsyncify_DirectGlobals_RepeatedStatefulLoop tests repeated stateful asyncify
// suspension and resume loops using the direct mutable-global control path.
func TestAsyncify_DirectGlobals_RepeatedStatefulLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

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

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		AsyncImports:  []string{"env.recv", "env.send"},
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)

	var sendArgs []uint64
	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(MakeAsyncHandler(func(ctx context.Context, _ api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "recv", id: 1}
		}), nil, []api.ValueType{api.ValueTypeI32}).
		Export("recv").
		NewFunctionBuilder().
		WithGoModuleFunction(MakeAsyncHandler(func(ctx context.Context, _ api.Module, stack []uint64) PendingOp {
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
	defer mod.Close(ctx)

	a := NewAsyncify()
	a.trusted = true // Trusted provenance from embedded transformer in this load
	if err := a.Init(mod); err != nil {
		t.Fatalf("asyncify init: %v", err)
	}

	if !a.DirectGlobals() {
		t.Fatal("expected DirectGlobals to be active for trusted module with exported globals")
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

	recvVals := []uint64{10, 20, 30, 99}
	recvIdx := 0

	var yr *YieldResult
	for {
		sr, err := s.Step(ctx, yr)
		if err != nil {
			t.Fatalf("step error: %v", err)
		}
		if sr.Status == StepDone {
			break
		}
		if sr.PendingOp.CmdID() == 1 { // recv
			yr = &YieldResult{Value: recvVals[recvIdx]}
			recvIdx++
		} else { // send
			yr = &YieldResult{Value: 0}
		}
	}

	expectedSend := []uint64{1, 2, 3}
	if len(sendArgs) != len(expectedSend) {
		t.Fatalf("expected sendArgs %v, got %v", expectedSend, sendArgs)
	}
	for i, v := range expectedSend {
		if sendArgs[i] != v {
			t.Errorf("sendArgs[%d]: expected %d, got %d", i, v, sendArgs[i])
		}
	}
}

// TestAsyncify_Fallback_PreAsyncified verifies that pre-asyncified modules without
// trusted provenance remain on the fallback path.
func TestAsyncify_Fallback_PreAsyncified(t *testing.T) {
	ctx := context.Background()

	// External module with asyncify exports
	watSrc := `(module
		(memory 1)
		(global $state (mut i32) (i32.const 0))
		(func (export "asyncify_get_state") (result i32)
			(global.get $state))
		(func (export "asyncify_start_unwind") (param i32)
			(global.set $state (i32.const 1)))
		(func (export "asyncify_stop_unwind")
			(global.set $state (i32.const 0)))
		(func (export "asyncify_start_rewind") (param i32)
			(global.set $state (i32.const 2)))
		(func (export "asyncify_stop_rewind")
			(global.set $state (i32.const 0)))
	)`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	mod, err := rt.Instantiate(ctx, raw)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	a := NewAsyncify()
	// Untrusted by default
	if err := a.Init(mod); err != nil {
		t.Fatalf("init: %v", err)
	}

	if a.DirectGlobals() {
		t.Fatal("untrusted module must NOT have DirectGlobals active")
	}

	// Verify fallback works
	if err := a.StartUnwind(ctx); err != nil {
		t.Fatalf("start unwind: %v", err)
	}
	if a.GetState(ctx) != 1 {
		t.Fatalf("expected state 1, got %d", a.GetState(ctx))
	}
	if err := a.StopUnwind(ctx); err != nil {
		t.Fatalf("stop unwind: %v", err)
	}
	if a.GetState(ctx) != 0 {
		t.Fatalf("expected state 0, got %d", a.GetState(ctx))
	}
}

// TestAsyncify_Fallback_SpoofedGlobalNames verifies that guest modules with spoofed
// export names asyncify_state and asyncify_data WITHOUT trusted provenance remain on fallback.
func TestAsyncify_Fallback_SpoofedGlobalNames(t *testing.T) {
	ctx := context.Background()

	watSrc := `(module
		(memory 1)
		(global $state (export "asyncify_state") (mut i32) (i32.const 0))
		(global $data (export "asyncify_data") (mut i32) (i32.const 0))
		(func (export "asyncify_get_state") (result i32)
			(i32.const 42)) ;; spoofed return value
		(func (export "asyncify_start_unwind") (param i32)
			(global.set $state (i32.const 1)))
		(func (export "asyncify_stop_unwind")
			(global.set $state (i32.const 0)))
		(func (export "asyncify_start_rewind") (param i32)
			(global.set $state (i32.const 2)))
		(func (export "asyncify_stop_rewind")
			(global.set $state (i32.const 0)))
	)`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	mod, err := rt.Instantiate(ctx, raw)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	a := NewAsyncify()
	// trusted is FALSE
	if err := a.Init(mod); err != nil {
		t.Fatalf("init: %v", err)
	}

	if a.DirectGlobals() {
		t.Fatal("spoofed global names without trusted provenance must NOT enable direct globals")
	}

	// Must call fallback control function
	syncVal := a.SyncState(ctx)
	if syncVal != 42 {
		t.Fatalf("expected fallback asyncify_get_state to be called (returning 42), got %d", syncVal)
	}
}

// TestAsyncify_Fallback_GuestTrap verifies that guest-authored control functions
// that trap are actually invoked and their traps are observed on the fallback path.
func TestAsyncify_Fallback_GuestTrap(t *testing.T) {
	ctx := context.Background()

	watSrc := `(module
		(memory 1)
		(func (export "asyncify_get_state") (result i32) (i32.const 0))
		(func (export "asyncify_start_unwind") (param i32)
			(unreachable)) ;; guest custom trap
		(func (export "asyncify_stop_unwind"))
		(func (export "asyncify_start_rewind") (param i32))
		(func (export "asyncify_stop_rewind"))
	)`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	mod, err := rt.Instantiate(ctx, raw)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	a := NewAsyncify()
	if err := a.Init(mod); err != nil {
		t.Fatalf("init: %v", err)
	}

	if a.DirectGlobals() {
		t.Fatal("must not be direct globals")
	}

	err = a.StartUnwind(ctx)
	if err == nil {
		t.Fatal("expected guest trap error from fallback start_unwind, got nil")
	}
}

// TestAsyncify_DirectGlobals_StateTraps tests that state machine violations in
// direct globals path trigger unreachable trap errors exactly like generated WASM.
func TestAsyncify_DirectGlobals_StateTraps(t *testing.T) {
	ctx := context.Background()

	watSrc := `(module
		(import "env" "yield" (func $yield))
		(func (export "f")
			(call $yield))
		(memory 1))`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		AsyncImports:  []string{"env.yield"},
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func() {}).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}

	mod, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	a := NewAsyncify()
	a.trusted = true
	if err := a.Init(mod); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !a.DirectGlobals() {
		t.Fatal("expected direct globals")
	}

	// 1. StopUnwind when normal (state 0 != 1) must trap
	if err := a.StopUnwind(ctx); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable trap for invalid StopUnwind, got %v", err)
	}

	// 2. StopRewind when normal (state 0 != 2) must trap
	if err := a.StopRewind(ctx); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable trap for invalid StopRewind, got %v", err)
	}

	// 3. Normal -> StartUnwind (state becomes 1)
	if err := a.StartUnwind(ctx); err != nil {
		t.Fatalf("valid start unwind failed: %v", err)
	}

	// 4. StartUnwind when already unwinding (state 1 != 0) must trap
	if err := a.StartUnwind(ctx); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable trap for double StartUnwind, got %v", err)
	}

	// 5. StartRewind when unwinding (state 1 != 0) must trap
	if err := a.StartRewind(ctx); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable trap for StartRewind during unwind, got %v", err)
	}

	// 6. StopUnwind succeeds (state becomes 0)
	if err := a.StopUnwind(ctx); err != nil {
		t.Fatalf("valid stop unwind failed: %v", err)
	}

	// 7. Normal -> StartRewind (state becomes 2)
	if err := a.StartRewind(ctx); err != nil {
		t.Fatalf("valid start rewind failed: %v", err)
	}

	// 8. StartRewind when already rewinding (state 2 != 0) must trap
	if err := a.StartRewind(ctx); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable trap for double StartRewind, got %v", err)
	}

	// 9. StopRewind succeeds (state becomes 0)
	if err := a.StopRewind(ctx); err != nil {
		t.Fatalf("valid stop rewind failed: %v", err)
	}
}

// TestAsyncify_DirectGlobals_StackBoundsTraps tests that stack pointer exceeding
// stack end or memory out of bounds traps.
func TestAsyncify_DirectGlobals_StackBoundsTraps(t *testing.T) {
	ctx := context.Background()

	watSrc := `(module
		(import "env" "yield" (func $yield))
		(func (export "f") (call $yield))
		(memory 1))`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		AsyncImports:  []string{"env.yield"},
		ExportGlobals: true,
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	_, _ = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func() {}).Export("yield").Instantiate(ctx)

	mod, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	a := NewAsyncify()
	a.trusted = true
	if err := a.Init(mod); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Corrupt stack so stack_ptr > stack_end
	// dataAddr is 16. stack_ptr is at offset 0 (16), stack_end is at offset 4 (20).
	mem := mod.Memory()
	mem.WriteUint32Le(16, 500) // stack_ptr = 500
	mem.WriteUint32Le(20, 100) // stack_end = 100 (500 > 100!)

	err = a.StartUnwind(ctx)
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable trap on stack overflow, got %v", err)
	}

	// Reset state to Normal and test out of bounds memory access
	a.stateGlobal.Set(0)
	atomic.StoreInt32(&a.state, 0)
	a.SetDataAddr(0xFFFFFFFC)
	err = a.StartRewind(ctx)
	if err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("expected out of bounds trap on invalid dataAddr, got %v", err)
	}
}

// TestAsyncify_ControlCancellation preserves the original canceled-call behavior.
func TestAsyncify_ControlCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	a, mod := reviewAsyncifyControls(t, true)

	if err := a.StartUnwind(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from StartUnwind, got %v", err)
	}
	if err := a.StopUnwind(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from StopUnwind, got %v", err)
	}
	if err := a.StartRewind(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from StartRewind, got %v", err)
	}
	if err := a.StopRewind(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from StopRewind, got %v", err)
	}
	if !mod.IsClosed() {
		t.Fatal("canceled control did not close its module")
	}
}

// TestAsyncify_Provenance_ComponentEngine tests that provenance flows through
// Linker -> InstancePre -> Instance -> selected core module -> WazeroInstance -> Asyncify.
func TestAsyncify_Provenance_ComponentEngine(t *testing.T) {
	ctx := context.Background()
	e, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer e.Close(ctx)

	// Single module compiled via engine with asyncify enabled
	watSrc := `(module
		(import "env" "call" (func $call))
		(func (export "run") (call $call))
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) (i32.const 0))
	)`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	m, err := e.LoadModule(ctx, raw)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}

	// Register async host function
	err = m.RegisterHostFuncRaw("env", "call", nil, nil, MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
		return &traceOp{name: "call", id: 1}
	}), true)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Compile with asyncify
	if err := m.Compile(ctx, &CompileConfig{EnableAsyncify: true}); err != nil {
		t.Fatalf("compile: %v", err)
	}

	if !m.IsTransformed() {
		t.Fatal("expected WazeroModule.IsTransformed() == true")
	}

	inst, err := m.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	if !inst.IsTransformed() {
		t.Fatal("expected WazeroInstance.IsTransformed() == true")
	}

	async := inst.Asyncify()
	if async == nil {
		t.Fatal("expected asyncify to be non-nil")
	}
	if !async.trusted {
		t.Fatal("expected embedded-transform provenance")
	}
	if !async.DirectGlobals() {
		t.Fatal("expected Asyncify.DirectGlobals() == true")
	}
}

// TestAsyncify_ConcurrencyAndRace tests concurrent operations across instances
// to verify that there are no race conditions in linker or engine provenance.
func TestAsyncify_ConcurrencyAndRace(t *testing.T) {
	ctx := context.Background()
	e, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer e.Close(ctx)

	watSrc := `(module
		(import "env" "step" (func $step (result i32)))
		(func (export "run") (result i32)
			(call $step))
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) (i32.const 0))
	)`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	m, err := e.LoadModule(ctx, raw)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	err = m.RegisterHostFuncRaw("env", "step", nil, []api.ValueType{api.ValueTypeI32}, MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) PendingOp {
		return &traceOp{name: "step", id: 1}
	}), true)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := m.Compile(ctx, &CompileConfig{EnableAsyncify: true}); err != nil {
		t.Fatalf("compile: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 10
	errChan := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			inst, err := m.InstantiateWithConfig(ctx, &InstanceConfig{
				Name:           fmt.Sprintf("worker-%d", id),
				EnableAsyncify: true,
			})
			if err != nil {
				errChan <- fmt.Errorf("worker %d instantiate: %w", id, err)
				return
			}
			defer inst.Close(ctx)

			if !inst.IsTransformed() {
				errChan <- fmt.Errorf("worker %d expected transformed", id)
				return
			}
			if !inst.Asyncify().DirectGlobals() {
				errChan <- fmt.Errorf("worker %d expected DirectGlobals", id)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errChan)
	for err := range errChan {
		t.Fatal(err)
	}
}

// BenchmarkAsyncify_Suspension compares direct mutable-global control against
// fallback function calls with wazero cancellation/timeout watcher enabled.
func BenchmarkAsyncify_Suspension_DirectGlobals(b *testing.B) {
	benchmarkSuspension(b, true)
}

func BenchmarkAsyncify_Suspension_Fallback(b *testing.B) {
	benchmarkSuspension(b, false)
}

func benchmarkSuspension(b *testing.B, directGlobals bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watSrc := `(module
		(import "env" "yield" (func $yield))
		(func (export "run")
			(call $yield))
		(memory 1))`

	raw, err := wat.Compile(watSrc)
	if err != nil {
		b.Fatalf("compile wat: %v", err)
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		AsyncImports:  []string{"env.yield"},
		ExportGlobals: true,
	})
	if err != nil {
		b.Fatalf("transform: %v", err)
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)

	_, err = rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(MakeAsyncHandler(func(ctx context.Context, _ api.Module, stack []uint64) PendingOp {
			return &traceOp{name: "yield", id: 1}
		}), nil, nil).
		Export("yield").
		Instantiate(ctx)
	if err != nil {
		b.Fatalf("host: %v", err)
	}

	mod, err := rt.Instantiate(ctx, transformed)
	if err != nil {
		b.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	a := NewAsyncify()
	a.trusted = directGlobals
	if err := a.Init(mod); err != nil {
		b.Fatalf("init: %v", err)
	}

	if a.DirectGlobals() != directGlobals {
		b.Fatalf("expected DirectGlobals == %v, got %v", directGlobals, a.DirectGlobals())
	}

	s := NewScheduler(a)
	callCtx := WithAsyncify(ctx, a)
	callCtx = WithScheduler(callCtx, s)
	fn := mod.ExportedFunction("run")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := s.Execute(callCtx, fn); err != nil {
			b.Fatalf("execute: %v", err)
		}
		// Step 1: runs to yield (StartUnwind + StopUnwind)
		sr1, err := s.Step(callCtx, nil)
		if err != nil || sr1.Status != StepContinue {
			b.Fatalf("step 1 failed: %v", err)
		}
		// Step 2: resume (StartRewind + StopRewind)
		sr2, err := s.Step(callCtx, &YieldResult{Value: 0})
		if err != nil || sr2.Status != StepDone {
			b.Fatalf("step 2 failed: %v", err)
		}
	}
}
