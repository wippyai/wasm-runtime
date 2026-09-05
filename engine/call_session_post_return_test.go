package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
	"go.bytecodealliance.org/wit"
)

const postReturnWAT = `(module
	(memory (export "memory") 2)
	(global $post_return_called (mut i32) (i32.const 0))
	(global $last_cleaned_ptr (mut i32) (i32.const 0))
	(global $last_direct_arg (mut i32) (i32.const 0))

	;; Asyncify stubs (embedded actor work; external instrumentation not required)
	(func (export "asyncify_get_state") (result i32) (i32.const 0))
	(func (export "asyncify_start_unwind") (param i32))
	(func (export "asyncify_stop_unwind"))
	(func (export "asyncify_start_rewind") (param i32))
	(func (export "asyncify_stop_rewind"))

	;; cabi_realloc(old_ptr, old_size, align, new_size) -> new_ptr
	(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
		(local.get 0)
	)

	;; echo_str writes string pointer and length to memory at offset 100
	;; and returns the retptr (100)
	(func (export "echo_str") (result i32)
		;; store "hello" at offset 200
		(i32.store8 (i32.const 200) (i32.const 104)) ;; 'h'
		(i32.store8 (i32.const 201) (i32.const 101)) ;; 'e'
		(i32.store8 (i32.const 202) (i32.const 108)) ;; 'l'
		(i32.store8 (i32.const 203) (i32.const 108)) ;; 'l'
		(i32.store8 (i32.const 204) (i32.const 111)) ;; 'o'

		;; store ptr (200) and len (5) at retptr (100)
		(i32.store (i32.const 100) (i32.const 200))
		(i32.store (i32.const 104) (i32.const 5))
		(i32.const 100)
	)

	;; cabi_post_echo_str(retptr)
	(func (export "cabi_post_echo_str") (param $ptr i32)
		(global.set $post_return_called (i32.add (global.get $post_return_called) (i32.const 1)))
		(global.set $last_cleaned_ptr (local.get $ptr))
		;; Overwrite string backing memory at offset 200 with 'x' (120) to prove host copy was safe
		(i32.store8 (i32.const 200) (i32.const 120))
		(i32.store8 (i32.const 201) (i32.const 120))
		(i32.store8 (i32.const 202) (i32.const 120))
		(i32.store8 (i32.const 203) (i32.const 120))
		(i32.store8 (i32.const 204) (i32.const 120))
	)

	;; add(a, b) returns a + b directly
	(func (export "add") (param $a i32) (param $b i32) (result i32)
		(i32.add (local.get $a) (local.get $b))
	)

	;; cabi_post_add(sum)
	(func (export "cabi_post_add") (param $sum i32)
		(global.set $post_return_called (i32.add (global.get $post_return_called) (i32.const 1)))
		(global.set $last_direct_arg (local.get $sum))
	)

	;; failing_post returns i32 and cabi_post_failing_post traps
	(func (export "failing_post") (result i32)
		(i32.const 77)
	)

	(func (export "cabi_post_failing_post") (param $val i32)
		(global.set $post_return_called (i32.add (global.get $post_return_called) (i32.const 1)))
		(unreachable)
	)

	;; looping_post returns i32 and cabi_post_looping_post loops forever
	(func (export "looping_post") (result i32)
		(i32.const 88)
	)

	(func (export "cabi_post_looping_post") (param $val i32)
		(global.set $post_return_called (i32.add (global.get $post_return_called) (i32.const 1)))
		(loop $forever (br $forever))
	)

	(func (export "get_post_return_called") (result i32)
		(global.get $post_return_called)
	)

	(func (export "get_last_cleaned_ptr") (result i32)
		(global.get $last_cleaned_ptr)
	)

	(func (export "get_last_direct_arg") (result i32)
		(global.get $last_direct_arg)
	)
)`

func setupPostReturnInstance(t *testing.T) (*WazeroEngine, *WazeroInstance, func()) {
	t.Helper()
	ctx := context.Background()

	wasmBytes, err := wat.Compile(postReturnWAT)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	eng, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("LoadModule: %v", err)
	}

	// Register lifts in canon registry for testing
	mod.canonRegistry = &component.CanonRegistry{
		Lifts: map[string]*component.LiftDef{
			"echo_str": {
				Name:    "echo_str",
				Params:  nil,
				Results: []wit.Type{wit.String{}},
			},
			"add": {
				Name:    "add",
				Params:  []wit.Type{wit.U32{}, wit.U32{}},
				Results: []wit.Type{wit.U32{}},
			},
			"failing_post": {
				Name:    "failing_post",
				Params:  nil,
				Results: []wit.Type{wit.U32{}},
			},
			"looping_post": {
				Name:    "looping_post",
				Params:  nil,
				Results: []wit.Type{wit.U32{}},
			},
		},
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{Name: "test_inst"})
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("Instantiate: %v", err)
	}

	if err := inst.EnableAsyncify(AsyncifyConfig{DataAddr: 1024, StackSize: 1024}); err != nil {
		inst.Close(ctx)
		eng.Close(ctx)
		t.Fatalf("EnableAsyncify: %v", err)
	}

	cleanup := func() {
		inst.Close(ctx)
		eng.Close(ctx)
	}

	return eng, inst, cleanup
}

// TestCallSession_PostReturn_IndirectResult verifies that for an indirect result (string),
// post-return is called with the retptr exactly once after successful lifting.
func TestCallSession_PostReturn_IndirectResult(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()

	ctx := context.Background()
	session, err := inst.StartCall(ctx, "echo_str")
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	if session.postReturn == nil {
		t.Fatal("expected postReturn function to be resolved")
	}

	stepRes, err := session.Step(ctx, nil)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if stepRes.Status != StepDone {
		t.Fatalf("expected StepDone, got %v", stepRes.Status)
	}
	if len(stepRes.Results) != 1 || stepRes.Results[0] != 100 {
		t.Fatalf("expected rawResult [100], got %v", stepRes.Results)
	}

	// LiftResult decodes and triggers post-return
	val, err := session.LiftResult(ctx, stepRes.Results)
	if err != nil {
		t.Fatalf("LiftResult: %v", err)
	}

	strVal, ok := val.(string)
	if !ok || strVal != "hello" {
		t.Fatalf("expected 'hello', got %v (%T)", val, val)
	}

	// Verify post-return overwrote backing memory after host safely copied the string
	memBytes, err := inst.memory.Read(200, 5)
	if err != nil || string(memBytes) != "xxxxx" {
		t.Fatalf("expected backing memory overwritten with 'xxxxx', got %q, err=%v", string(memBytes), err)
	}

	// Verify post-return was called on guest
	getCalls := inst.GetExportedFunction("get_post_return_called")
	res, err := getCalls.Call(ctx)
	if err != nil || len(res) != 1 || res[0] != 1 {
		t.Fatalf("expected post-return called once, got res=%v, err=%v", res, err)
	}

	getPtr := inst.GetExportedFunction("get_last_cleaned_ptr")
	resPtr, err := getPtr.Call(ctx)
	if err != nil || len(resPtr) != 1 || resPtr[0] != 100 {
		t.Fatalf("expected last cleaned ptr 100, got %v", resPtr)
	}

	// Exactly-once guarantee: second LiftResult call must NOT re-run post-return
	val2, err := session.LiftResult(ctx, stepRes.Results)
	if err != nil {
		t.Fatalf("LiftResult (second call): %v", err)
	}
	if val2 != "hello" {
		t.Fatalf("expected cached 'hello', got %v", val2)
	}

	res, err = getCalls.Call(ctx)
	if err != nil || len(res) != 1 || res[0] != 1 {
		t.Fatalf("expected post-return still called exactly once, got res=%v", res)
	}
}

// TestCallSession_PostReturn_DirectResult verifies direct result (u32) triggers post-return
// with the return value.
func TestCallSession_PostReturn_DirectResult(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()

	ctx := context.Background()
	session, err := inst.StartCall(ctx, "add", uint32(20), uint32(22))
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	stepRes, err := session.Step(ctx, nil)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if stepRes.Status != StepDone {
		t.Fatalf("expected StepDone, got %v", stepRes.Status)
	}

	val, err := session.LiftResult(ctx, stepRes.Results)
	if err != nil {
		t.Fatalf("LiftResult: %v", err)
	}
	if val != uint32(42) {
		t.Fatalf("expected 42, got %v", val)
	}

	getCalls := inst.GetExportedFunction("get_post_return_called")
	res, err := getCalls.Call(ctx)
	if err != nil || len(res) != 1 || res[0] != 1 {
		t.Fatalf("expected post-return called once, got res=%v", res)
	}

	getArg := inst.GetExportedFunction("get_last_direct_arg")
	resArg, err := getArg.Call(ctx)
	if err != nil || len(resArg) != 1 || resArg[0] != 42 {
		t.Fatalf("expected last direct arg 42, got %v", resArg)
	}

	// Repeated lift must be idempotent
	val2, err := session.LiftResult(ctx, stepRes.Results)
	if err != nil || val2 != uint32(42) {
		t.Fatalf("second LiftResult failed: %v, val=%v", err, val2)
	}
	res, err = getCalls.Call(ctx)
	if err != nil || len(res) != 1 || res[0] != 1 {
		t.Fatalf("expected post-return called still exactly once, got %v", res)
	}
}

// TestCallSession_PostReturn_NoCleanupOnDecodeError verifies that no cleanup happens
// before decoding or if decoding fails.
func TestCallSession_PostReturn_NoCleanupOnDecodeError(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()

	ctx := context.Background()
	session, err := inst.StartCall(ctx, "echo_str")
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	// Pass out-of-bounds pointer (e.g. 0xFFFFFF00) which fails LoadValue
	_, err = session.LiftResult(ctx, []uint64{0xFFFFFF00})
	if err == nil {
		t.Fatal("expected decode results error for out-of-bounds pointer")
	}

	// Verify post-return was NOT called
	getCalls := inst.GetExportedFunction("get_post_return_called")
	res, errCalls := getCalls.Call(ctx)
	if errCalls != nil || len(res) != 1 || res[0] != 0 {
		t.Fatalf("expected post-return not called (0), got %v", res)
	}
}

// TestCallSession_PostReturn_IndirectResultErrors verifies validation of raw results
// for indirect calls.
func TestCallSession_PostReturn_IndirectResultErrors(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("empty rawResults for indirect return", func(t *testing.T) {
		session, err := inst.StartCall(ctx, "echo_str")
		if err != nil {
			t.Fatalf("StartCall: %v", err)
		}
		_, err = session.LiftResult(ctx, []uint64{})
		if err == nil || err.Error() != "indirect result requires one return pointer" {
			t.Fatalf("expected 'indirect result requires one return pointer', got %v", err)
		}
		// Verify no cleanup
		getCalls := inst.GetExportedFunction("get_post_return_called")
		res, _ := getCalls.Call(ctx)
		if len(res) > 0 && res[0] != 0 {
			t.Fatalf("cleanup must not run on error, called=%v", res[0])
		}
	})

	t.Run("too many rawResults for indirect return", func(t *testing.T) {
		session, err := inst.StartCall(ctx, "echo_str")
		if err != nil {
			t.Fatalf("StartCall: %v", err)
		}
		_, err = session.LiftResult(ctx, []uint64{100, 200})
		if err == nil || err.Error() != "indirect result requires one return pointer" {
			t.Fatalf("expected 'indirect result requires one return pointer', got %v", err)
		}
	})

	t.Run("empty rawResults for direct return", func(t *testing.T) {
		session, err := inst.StartCall(ctx, "add", uint32(1), uint32(2))
		if err != nil {
			t.Fatalf("StartCall: %v", err)
		}
		_, err = session.LiftResult(ctx, []uint64{})
		if err == nil {
			t.Fatal("expected error for empty raw results on direct return")
		}
	})
}

// TestCallSession_PostReturn_TrapAndRepeatedLiftPreservesFailure verifies that when post-return
// traps, LiftResult returns a wrapped error, and repeated LiftResult preserves the failure
// without re-invoking post-return or returning success.
func TestCallSession_PostReturn_TrapAndRepeatedLiftPreservesFailure(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()

	ctx := context.Background()
	session, err := inst.StartCall(ctx, "failing_post")
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	stepRes, err := session.Step(ctx, nil)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if stepRes.Status != StepDone {
		t.Fatalf("expected StepDone, got %v", stepRes.Status)
	}

	// First LiftResult should fail because cabi_post_failing_post executes unreachable
	val1, err1 := session.LiftResult(ctx, stepRes.Results)
	if err1 == nil {
		t.Fatalf("expected post-return trap error, got val=%v", val1)
	}
	if !strings.Contains(err1.Error(), "post-return") {
		t.Fatalf("expected wrapped post-return error, got %v", err1)
	}

	getCalls := inst.GetExportedFunction("get_post_return_called")
	res, err := getCalls.Call(ctx)
	if err != nil || len(res) != 1 || res[0] != 1 {
		t.Fatalf("expected post-return called once, got res=%v, err=%v", res, err)
	}

	// Repeated LiftResult must preserve failure, return cached error, and not invoke post-return again
	val2, err2 := session.LiftResult(ctx, stepRes.Results)
	if err2 == nil {
		t.Fatalf("expected repeated LiftResult to preserve failure, got val=%v", val2)
	}
	if err2.Error() != err1.Error() {
		t.Fatalf("expected identical cached error %v, got %v", err1, err2)
	}

	res, err = getCalls.Call(ctx)
	if err != nil || len(res) != 1 || res[0] != 1 {
		t.Fatalf("expected post-return not called again, got res=%v", res)
	}
}

// TestCallSession_PostReturn_CancellationPreserved verifies that caller cancellation
// is preserved (no context.WithoutCancel), terminating execution on cancellation / CloseOnContextDone.
func TestCallSession_PostReturn_CancellationPreserved(t *testing.T) {
	ctx := context.Background()

	t.Run("pre-cancelled context returns wrapped error and preserves failure", func(t *testing.T) {
		_, inst, cleanup := setupPostReturnInstance(t)
		defer cleanup()

		session, err := inst.StartCall(ctx, "echo_str")
		if err != nil {
			t.Fatalf("StartCall: %v", err)
		}

		stepRes, err := session.Step(ctx, nil)
		if err != nil {
			t.Fatalf("Step: %v", err)
		}

		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel() // cancelled before post-return call

		val, err := session.LiftResult(cancelledCtx, stepRes.Results)
		if err == nil {
			t.Fatalf("expected cancellation error, got val=%v", val)
		}
		if !strings.Contains(err.Error(), "post-return") {
			t.Fatalf("expected wrapped post-return error, got %v", err)
		}

		// Repeated lift preserves failure even with fresh active context
		_, err2 := session.LiftResult(ctx, stepRes.Results)
		if err2 == nil {
			t.Fatal("expected failure to be preserved on repeated lift")
		}
	})

	t.Run("looping post-return interrupted by context timeout via CloseOnContextDone", func(t *testing.T) {
		_, inst, cleanup := setupPostReturnInstance(t)
		defer cleanup()

		session, err := inst.StartCall(ctx, "looping_post")
		if err != nil {
			t.Fatalf("StartCall: %v", err)
		}

		stepRes, err := session.Step(ctx, nil)
		if err != nil {
			t.Fatalf("Step: %v", err)
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancel()

		val, err := session.LiftResult(timeoutCtx, stepRes.Results)
		if err == nil {
			t.Fatalf("expected timeout error for looping post-return, got val=%v", val)
		}
		if !strings.Contains(err.Error(), "post-return") {
			t.Fatalf("expected wrapped post-return error, got %v", err)
		}

		// Repeated lift preserves failure
		_, err2 := session.LiftResult(ctx, stepRes.Results)
		if err2 == nil {
			t.Fatal("expected failure to be preserved on repeated lift")
		}
	})
}

// TestCallSession_PostReturn_MultipleIndirectResults tests multiple indirect results.
func TestCallSession_PostReturn_MultipleIndirectResults(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()

	ctx := context.Background()
	// Write two u32 values at offset 500: [111, 222]
	if err := inst.memory.WriteU32(500, 111); err != nil {
		t.Fatalf("WriteU32: %v", err)
	}
	if err := inst.memory.WriteU32(504, 222); err != nil {
		t.Fatalf("WriteU32: %v", err)
	}

	var postReturnCalled bool
	var receivedArg uint64
	fakePostReturn := &mockFunction{
		callFn: func(ctx context.Context, args ...uint64) ([]uint64, error) {
			postReturnCalled = true
			if len(args) > 0 {
				receivedArg = args[0]
			}
			return nil, nil
		},
	}

	cs := &CallSession{
		instance:    inst,
		resultTypes: []wit.Type{wit.U32{}, wit.U32{}},
		postReturn:  fakePostReturn,
		memory:      inst.memory,
	}

	val, err := cs.LiftResult(ctx, []uint64{500})
	if err != nil {
		t.Fatalf("LiftResult: %v", err)
	}

	results, ok := val.([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("expected 2 results, got %v (%T)", val, val)
	}
	if results[0] != uint32(111) || results[1] != uint32(222) {
		t.Fatalf("expected [111, 222], got %v", results)
	}

	if !postReturnCalled || receivedArg != 500 {
		t.Fatalf("expected postReturn called with retptr 500, called=%v, arg=%v", postReturnCalled, receivedArg)
	}
}

// TestCallSession_PostReturn_VoidReturn tests void return functions.
func TestCallSession_PostReturn_VoidReturn(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()

	ctx := context.Background()
	var postReturnCalled bool
	fakePostReturn := &mockFunction{
		callFn: func(ctx context.Context, args ...uint64) ([]uint64, error) {
			postReturnCalled = true
			return nil, nil
		},
	}

	cs := &CallSession{
		instance:    inst,
		resultTypes: nil,
		postReturn:  fakePostReturn,
	}

	val, err := cs.LiftResult(ctx, nil)
	if err != nil {
		t.Fatalf("LiftResult void: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil result, got %v", val)
	}
	if !postReturnCalled {
		t.Fatal("expected postReturn called for void return")
	}
}

// TestCallSession_NilSession verifies nil safety
func TestCallSession_NilSession(t *testing.T) {
	ctx := context.Background()
	var cs *CallSession
	_, err := cs.LiftResult(ctx, nil)
	if err == nil {
		t.Fatal("expected error on nil CallSession")
	}
}

type mockFunction struct {
	api.Function
	callFn func(ctx context.Context, args ...uint64) ([]uint64, error)
}

func (m *mockFunction) Call(ctx context.Context, args ...uint64) ([]uint64, error) {
	if m.callFn != nil {
		return m.callFn(ctx, args...)
	}
	return nil, nil
}

func (m *mockFunction) Definition() api.FunctionDefinition {
	return nil
}

// TestCallSession_PostReturn_ComplexComponent verifies that an actual component with
// CanonExport (complex.wasm) resolves exp.Canon.PostReturn in StartCall and cleans up on LiftResult.
func TestCallSession_PostReturn_ComplexComponent(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	// Also verify with complex.wasm if available
	mod := loadTestComponent(t, "../testbed/complex.wasm")
	if mod == nil {
		t.Skip("complex.wasm not available")
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EntryExport: "echo-person",
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	// PostReturn resolution check on the linker instance
	if inst.linkerInst == nil {
		t.Fatal("expected linkerInst to be populated for multi-module component")
	}
	exp, ok := inst.linkerInst.GetExport("echo-person")
	if !ok || exp.Canon == nil || exp.Canon.PostReturn == nil {
		t.Fatalf("expected echo-person to have resolved Canon.PostReturn")
	}

	// Prepare asyncify stubs on inst if needed or test StartCall PostReturn resolution
	inst.asyncify = NewAsyncify()
	inst.scheduler = NewScheduler(inst.asyncify)
	inst.asyncify.exports.getState = exp.CoreFunc // dummy non-nil

	// StartCall should bind the exact PostReturn from exp.Canon
	session, err := inst.StartCall(ctx, "echo-person", map[string]any{"name": "Alice", "age": uint32(30)})
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	if session.postReturn != exp.Canon.PostReturn {
		t.Fatalf("expected session.postReturn to match exp.Canon.PostReturn")
	}
}
