package bridge

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestForwardingWrapper_NilSourceFn(t *testing.T) {
	wrapper := ForwardingWrapper(nil, 0)
	if wrapper != nil {
		t.Error("expected nil wrapper for nil sourceFn")
	}
}

func TestForwardingWrapper_ParamCountExceedsStack(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Minimal WASM module with a function: (module (func (export "noop")))
	wasmBytes := []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type section: func type () -> ()
		0x03, 0x02, 0x01, 0x00, // function section: func 0 uses type 0
		0x07, 0x08, 0x01, 0x04, 0x6e, 0x6f, 0x6f, 0x70, 0x00, 0x00, // export "noop"
		0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b, // code section: empty function body
	}

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("noop")
	if fn == nil {
		t.Fatal("noop function not found")
	}

	wrapper := ForwardingWrapper(fn, 10)
	stack := make([]uint64, 5) // smaller than paramCount
	// Should not panic - just returns early
	wrapper(ctx, nil, stack)
}

func TestForwardingWrapper_Success(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// WASM module: (module (func (export "double") (param i32) (result i32) local.get 0 i32.const 2 i32.mul))
	wasmBytes := []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f, // type: (i32) -> i32
		0x03, 0x02, 0x01, 0x00, // function section
		0x07, 0x0a, 0x01, 0x06, 0x64, 0x6f, 0x75, 0x62, 0x6c, 0x65, 0x00, 0x00, // export "double"
		0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x41, 0x02, 0x6c, 0x0b, // code: local.get 0, i32.const 2, i32.mul, end
	}

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("double")
	if fn == nil {
		t.Fatal("double function not found")
	}
	wrapper := ForwardingWrapper(fn, 1)

	stack := make([]uint64, 1)
	stack[0] = 5
	wrapper(ctx, nil, stack)

	if stack[0] != 10 {
		t.Errorf("expected 10, got %d", stack[0])
	}
}

func TestCollector_FromModule_NilModule(t *testing.T) {
	c := NewCollector()
	exports := c.FromModule(nil)
	if exports != nil {
		t.Error("expected nil for nil module")
	}
}

func TestCollector_MergeBindings_NilHandler(t *testing.T) {
	c := NewCollector()
	bindings := []HostBinding{
		{ImportName: "test", IsTrap: false, Handler: nil}, // invalid binding
	}
	exports := c.MergeBindings(nil, bindings)
	// Should skip bindings without handler or IsTrap
	if len(exports) != 0 {
		t.Errorf("expected 0 exports for invalid binding, got %d", len(exports))
	}
}

func TestCollector_MergeBindings_TrapHandler(t *testing.T) {
	c := NewCollector()
	bindings := []HostBinding{
		{ImportName: "trap_func", IsTrap: true, ParamTypes: []api.ValueType{api.ValueTypeI32}},
	}
	exports := c.MergeBindings(nil, bindings)
	if len(exports) != 1 {
		t.Fatalf("expected 1 export, got %d", len(exports))
	}
	if exports[0].Name != "trap_func" {
		t.Errorf("expected name 'trap_func', got %s", exports[0].Name)
	}
}

func TestCollector_MergeBindings_SkipsDuplicates(t *testing.T) {
	c := NewCollector()
	existing := []Export{
		{Name: "existing_func"},
	}
	bindings := []HostBinding{
		{ImportName: "existing_func", IsTrap: true}, // duplicate
		{ImportName: "new_func", IsTrap: true},      // new
	}
	exports := c.MergeBindings(existing, bindings)
	if len(exports) != 2 {
		t.Errorf("expected 2 exports, got %d", len(exports))
	}
}

type testMockCaller struct {
	api.Module
	closed   bool
	exitCode uint32
}

func (m *testMockCaller) CloseWithExitCode(_ context.Context, exitCode uint32) error {
	m.closed = true
	m.exitCode = exitCode
	return nil
}

func (m *testMockCaller) IsClosed() bool {
	return m.closed
}

type testMockFunctionWithoutDef struct {
	api.Function
	callFn func(ctx context.Context, params ...uint64) ([]uint64, error)
}

func (m *testMockFunctionWithoutDef) Definition() api.FunctionDefinition {
	return nil
}

func (m *testMockFunctionWithoutDef) Call(ctx context.Context, params ...uint64) ([]uint64, error) {
	if m.callFn != nil {
		return m.callFn(ctx, params...)
	}
	return nil, nil
}

type testMockFuncDef struct {
	api.FunctionDefinition
	paramTypes  []api.ValueType
	resultTypes []api.ValueType
}

func (d *testMockFuncDef) ParamTypes() []api.ValueType {
	return d.paramTypes
}

func (d *testMockFuncDef) ResultTypes() []api.ValueType {
	return d.resultTypes
}

type testMockFunctionWithDef struct {
	api.Function
	def           api.FunctionDefinition
	callWithStack func(ctx context.Context, stack []uint64) error
}

func (m *testMockFunctionWithDef) Definition() api.FunctionDefinition {
	return m.def
}

func (m *testMockFunctionWithDef) CallWithStack(ctx context.Context, stack []uint64) error {
	if m.callWithStack != nil {
		return m.callWithStack(ctx, stack)
	}
	return nil
}

func TestForwardingWrapper_ZeroParamsMultipleResults(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(func (export "multi_out") (result i32 i64)
			i32.const 42
			i64.const 100
		)
	)`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("multi_out")
	wrapper := ForwardingWrapper(fn, 0)
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}

	stack := make([]uint64, 2)
	caller := &testMockCaller{}
	wrapper(ctx, caller, stack)

	if caller.closed {
		t.Fatalf("caller was unexpectedly closed with exit code %d", caller.exitCode)
	}
	if stack[0] != 42 || stack[1] != 100 {
		t.Fatalf("expected [42, 100], got [%d, %d]", stack[0], stack[1])
	}
}

func TestForwardingWrapper_ParamsGreaterThanResults(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(func (export "sum3") (param i32 i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
			local.get 2
			i32.add
		)
	)`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("sum3")
	wrapper := ForwardingWrapper(fn, 3)
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}

	stack := []uint64{10, 20, 30}
	caller := &testMockCaller{}
	wrapper(ctx, caller, stack)

	if caller.closed {
		t.Fatalf("caller was unexpectedly closed with exit code %d", caller.exitCode)
	}
	if stack[0] != 60 {
		t.Fatalf("expected 60, got %d", stack[0])
	}
}

func TestForwardingWrapper_ResultsGreaterThanParams(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(func (export "divmod") (param i32 i32) (result i32 i32 i32)
			local.get 0
			local.get 1
			i32.div_u
			local.get 0
			local.get 1
			i32.rem_u
			local.get 0
			local.get 1
			i32.add
		)
	)`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("divmod")
	wrapper := ForwardingWrapper(fn, 2)
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}

	stack := make([]uint64, 3)
	stack[0] = 17
	stack[1] = 5
	caller := &testMockCaller{}
	wrapper(ctx, caller, stack)

	if caller.closed {
		t.Fatalf("caller was unexpectedly closed with exit code %d", caller.exitCode)
	}
	if stack[0] != 3 || stack[1] != 2 || stack[2] != 22 {
		t.Fatalf("expected [3, 2, 22], got [%d, %d, %d]", stack[0], stack[1], stack[2])
	}
}

func TestForwardingWrapper_InsufficientStackAndMismatchedArity_NoGuestSideEffects(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(global $counter (mut i32) (i32.const 0))
		(func (export "get_counter") (result i32)
			global.get $counter
		)
		(func (export "side_effect") (param i32) (result i32 i32)
			global.get $counter
			i32.const 1
			i32.add
			global.set $counter
			local.get 0
			i32.const 10
			i32.add
			local.get 0
			i32.const 20
			i32.add
		)
	)`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}
	defer mod.Close(ctx)

	sideEffectFn := mod.ExportedFunction("side_effect")
	getCounterFn := mod.ExportedFunction("get_counter")

	getCounter := func() uint64 {
		res, err := getCounterFn.Call(ctx)
		if err != nil {
			t.Fatalf("get_counter failed: %v", err)
		}
		return res[0]
	}

	t.Run("insufficient result slots does not invoke side effect", func(t *testing.T) {
		wrapper := ForwardingWrapper(sideEffectFn, 1)
		caller := &testMockCaller{}
		// 1 slot: enough for 1 param, but not for 2 results
		stack := make([]uint64, 1)
		stack[0] = 5
		wrapper(ctx, caller, stack)

		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1, got closed=%v code=%d", caller.closed, caller.exitCode)
		}
		if c := getCounter(); c != 0 {
			t.Errorf("expected side effect NOT invoked (counter 0), got %d", c)
		}
	})

	t.Run("insufficient param slots does not invoke side effect", func(t *testing.T) {
		wrapper := ForwardingWrapper(sideEffectFn, 1)
		caller := &testMockCaller{}
		// 0 slots provided
		stack := make([]uint64, 0)
		wrapper(ctx, caller, stack)

		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1, got closed=%v code=%d", caller.closed, caller.exitCode)
		}
		if c := getCounter(); c != 0 {
			t.Errorf("expected side effect NOT invoked (counter 0), got %d", c)
		}
	})

	t.Run("mismatched arity greater does not invoke side effect", func(t *testing.T) {
		wrapper := ForwardingWrapper(sideEffectFn, 3)
		caller := &testMockCaller{}
		stack := make([]uint64, 5)
		wrapper(ctx, caller, stack)

		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1, got closed=%v code=%d", caller.closed, caller.exitCode)
		}
		if c := getCounter(); c != 0 {
			t.Errorf("expected side effect NOT invoked (counter 0), got %d", c)
		}
	})

	t.Run("mismatched arity less does not invoke side effect", func(t *testing.T) {
		wrapper := ForwardingWrapper(sideEffectFn, 0)
		caller := &testMockCaller{}
		stack := make([]uint64, 5)
		wrapper(ctx, caller, stack)

		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1, got closed=%v code=%d", caller.closed, caller.exitCode)
		}
		if c := getCounter(); c != 0 {
			t.Errorf("expected side effect NOT invoked (counter 0), got %d", c)
		}
	})

	t.Run("negative paramCount does not invoke side effect", func(t *testing.T) {
		wrapper := ForwardingWrapper(sideEffectFn, -1)
		caller := &testMockCaller{}
		stack := make([]uint64, 5)
		wrapper(ctx, caller, stack)

		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1, got closed=%v code=%d", caller.closed, caller.exitCode)
		}
		if c := getCounter(); c != 0 {
			t.Errorf("expected side effect NOT invoked (counter 0), got %d", c)
		}
	})
}

func TestForwardingWrapper_NestedReentrantHostForwarding(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Mod C: exports calc(x) = x + 100
	modCBytes, err := wat.Compile(`(module
		(func (export "calc") (param i32) (result i32)
			local.get 0
			i32.const 100
			i32.add
		)
	)`)
	if err != nil {
		t.Fatalf("compile modC: %v", err)
	}
	cCompiled, err := rt.CompileModule(ctx, modCBytes)
	if err != nil {
		t.Fatalf("compile c: %v", err)
	}
	defer cCompiled.Close(ctx)
	modC, err := rt.InstantiateModule(ctx, cCompiled, wazero.NewModuleConfig().WithName("modC"))
	if err != nil {
		t.Fatalf("instantiate modC: %v", err)
	}
	defer modC.Close(ctx)

	// Bridge 2: host module forwarding to modC.calc
	fnC := modC.ExportedFunction("calc")
	wrapperC := ForwardingWrapper(fnC, 1)
	_, err = rt.NewHostModuleBuilder("bridge2").
		NewFunctionBuilder().
		WithGoModuleFunction(wrapperC, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("calc").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate bridge2: %v", err)
	}

	// Mod B: imports bridge2.calc, exports step(x) = calc(x) * 2
	modBBytes, err := wat.Compile(`(module
		(import "bridge2" "calc" (func $calc (param i32) (result i32)))
		(func (export "step") (param i32) (result i32)
			local.get 0
			call $calc
			i32.const 2
			i32.mul
		)
	)`)
	if err != nil {
		t.Fatalf("compile modB: %v", err)
	}
	bCompiled, err := rt.CompileModule(ctx, modBBytes)
	if err != nil {
		t.Fatalf("compile b: %v", err)
	}
	defer bCompiled.Close(ctx)
	modB, err := rt.InstantiateModule(ctx, bCompiled, wazero.NewModuleConfig().WithName("modB"))
	if err != nil {
		t.Fatalf("instantiate modB: %v", err)
	}
	defer modB.Close(ctx)

	// Bridge 1: host module forwarding to modB.step
	fnB := modB.ExportedFunction("step")
	wrapperB := ForwardingWrapper(fnB, 1)
	_, err = rt.NewHostModuleBuilder("bridge1").
		NewFunctionBuilder().
		WithGoModuleFunction(wrapperB, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("step").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate bridge1: %v", err)
	}

	// Mod A: imports bridge1.step, exports run(x) = step(x)
	modABytes, err := wat.Compile(`(module
		(import "bridge1" "step" (func $step (param i32) (result i32)))
		(func (export "run") (param i32) (result i32)
			local.get 0
			call $step
		)
	)`)
	if err != nil {
		t.Fatalf("compile modA: %v", err)
	}
	aCompiled, err := rt.CompileModule(ctx, modABytes)
	if err != nil {
		t.Fatalf("compile a: %v", err)
	}
	defer aCompiled.Close(ctx)
	modA, err := rt.InstantiateModule(ctx, aCompiled, wazero.NewModuleConfig().WithName("modA"))
	if err != nil {
		t.Fatalf("instantiate modA: %v", err)
	}
	defer modA.Close(ctx)

	runFn := modA.ExportedFunction("run")
	res, err := runFn.Call(ctx, 7)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	// (7 + 100) * 2 = 214
	if len(res) != 1 || res[0] != 214 {
		t.Fatalf("expected 214, got %v", res)
	}
}

func TestForwardingWrapper_ErrorAndCancellation(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(func (export "trap_fn") (param i32) (result i32)
			unreachable
		)
		(func (export "echo") (param i32) (result i32)
			local.get 0
		)
	)`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}
	defer mod.Close(ctx)

	t.Run("callee error traps and closes caller", func(t *testing.T) {
		fn := mod.ExportedFunction("trap_fn")
		wrapper := ForwardingWrapper(fn, 1)
		caller := &testMockCaller{}
		stack := []uint64{1}
		expectForwardingTrap(t, func() { wrapper(ctx, caller, stack) })

		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1, got closed=%v code=%d", caller.closed, caller.exitCode)
		}
	})

	t.Run("cancellation closes caller and propagates", func(t *testing.T) {
		fn := mod.ExportedFunction("echo")
		wrapper := ForwardingWrapper(fn, 1)
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		caller := &testMockCaller{}
		stack := []uint64{42}
		expectForwardingTrap(t, func() { wrapper(cancelCtx, caller, stack) })

		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed on canceled context, got closed=%v code=%d", caller.closed, caller.exitCode)
		}
	})
}

func TestForwardingWrapper_DefinitionUnavailableFallback(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mockFn := &testMockFunctionWithoutDef{
			callFn: func(_ context.Context, params ...uint64) ([]uint64, error) {
				return []uint64{params[0] * 3}, nil
			},
		}
		wrapper := ForwardingWrapper(mockFn, 1)
		stack := []uint64{7}
		wrapper(ctx, nil, stack)
		if stack[0] != 21 {
			t.Errorf("expected 21, got %d", stack[0])
		}
	})

	t.Run("negative paramCount closes caller", func(t *testing.T) {
		mockFn := &testMockFunctionWithoutDef{}
		wrapper := ForwardingWrapper(mockFn, -1)
		caller := &testMockCaller{}
		wrapper(ctx, caller, []uint64{1})
		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1")
		}
	})

	t.Run("paramCount exceeds stack closes caller", func(t *testing.T) {
		mockFn := &testMockFunctionWithoutDef{}
		wrapper := ForwardingWrapper(mockFn, 5)
		caller := &testMockCaller{}
		wrapper(ctx, caller, []uint64{1, 2})
		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1")
		}
	})

	t.Run("callee error closes caller", func(t *testing.T) {
		mockFn := &testMockFunctionWithoutDef{
			callFn: func(_ context.Context, _ ...uint64) ([]uint64, error) {
				return nil, errors.New("callee error")
			},
		}
		wrapper := ForwardingWrapper(mockFn, 1)
		caller := &testMockCaller{}
		expectForwardingTrap(t, func() { wrapper(ctx, caller, []uint64{1}) })
		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1")
		}
	})

	t.Run("results exceed stack closes caller", func(t *testing.T) {
		mockFn := &testMockFunctionWithoutDef{
			callFn: func(_ context.Context, _ ...uint64) ([]uint64, error) {
				return []uint64{1, 2, 3}, nil
			},
		}
		wrapper := ForwardingWrapper(mockFn, 1)
		caller := &testMockCaller{}
		wrapper(ctx, caller, []uint64{1})
		if !caller.closed || caller.exitCode != 1 {
			t.Errorf("expected caller closed with exitCode 1")
		}
	})
}

func TestForwardingWrapper_MockWithCallWithStack(t *testing.T) {
	ctx := context.Background()
	called := false
	mockFn := &testMockFunctionWithDef{
		def: &testMockFuncDef{
			paramTypes:  []api.ValueType{api.ValueTypeI32},
			resultTypes: []api.ValueType{api.ValueTypeI32},
		},
		callWithStack: func(_ context.Context, stack []uint64) error {
			called = true
			stack[0] *= 2
			return nil
		},
	}

	wrapper := ForwardingWrapper(mockFn, 1)
	stack := []uint64{21}
	wrapper(ctx, nil, stack)

	if !called {
		t.Error("expected mock CallWithStack to be called")
	}
	if stack[0] != 42 {
		t.Errorf("expected 42, got %d", stack[0])
	}
}

func TestForwardingWrapper_ZeroAllocations(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
	)`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("instantiate module: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("add")
	wrapper := ForwardingWrapper(fn, 2)

	stack := make([]uint64, 2)
	caller := &testMockCaller{}

	// Warm up
	stack[0], stack[1] = 10, 20
	wrapper(ctx, caller, stack)
	if stack[0] != 30 {
		t.Fatalf("expected 30, got %d", stack[0])
	}

	allocs := testing.AllocsPerRun(100, func() {
		stack[0] = 10
		stack[1] = 20
		wrapper(ctx, caller, stack)
	})

	if allocs != 0 {
		t.Fatalf("expected 0 allocs per run, got %f", allocs)
	}
}

func BenchmarkForwardingWrapper(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
	)`)
	if err != nil {
		b.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		b.Fatalf("compile module: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		b.Fatalf("instantiate module: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("add")
	wrapper := ForwardingWrapper(fn, 2)

	stack := make([]uint64, 2)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		stack[0] = 10
		stack[1] = 20
		wrapper(ctx, nil, stack)
	}
}
