package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestInstanceFunctionRetainedHandleStopsAtClose(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	module, err := engine.LoadModule(ctx, compileSimpleAddWat(t))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close(ctx)
	fn := instance.GetExportedFunction("add")
	if fn == nil {
		t.Fatal("missing add")
	}
	result, err := fn.Call(ctx, 4, 5)
	if err != nil || len(result) != 1 || result[0] != 9 {
		t.Fatalf("call: %v %v", result, err)
	}
	stack := []uint64{7, 8}
	if err = fn.CallWithStack(ctx, stack); err != nil || stack[0] != 15 {
		t.Fatalf("stack: %v %v", stack, err)
	}
	if instance.GetExportedFunction("absent") != nil {
		t.Fatal("missing export should be nil")
	}
	if err = instance.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = fn.Call(ctx, 1, 2); !errors.Is(err, errExecutionLifetimeStopped) {
		t.Fatalf("retained call: %v", err)
	}
	stack[0], stack[1] = 10, 20
	if err = fn.CallWithStack(ctx, stack); !errors.Is(err, errExecutionLifetimeStopped) {
		t.Fatalf("retained stack: %v", err)
	}
	if stack[0] != 10 || stack[1] != 20 {
		t.Fatal("rejected call modified stack")
	}
	if len(fn.Definition().ParamTypes()) != 2 {
		t.Fatal("retained metadata unavailable")
	}
	if instance.GetExportedFunction("add") != nil {
		t.Fatal("export lookup after close")
	}
}

func TestInstanceFunctionCloseJoinsActiveHost(t *testing.T) {
	for _, withStack := range []bool{false, true} {
		name := "Call"
		if withStack {
			name = "CallWithStack"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
			defer runtime.Close(ctx)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			_, err := runtime.NewHostModuleBuilder("host").NewFunctionBuilder().WithFunc(func(context.Context) { close(entered); <-release }).Export("park").Instantiate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			binary, err := wat.Compile(`(module (import "host" "park" (func $park)) (func (export "run") call $park))`)
			if err != nil {
				t.Fatal(err)
			}
			module, err := runtime.Instantiate(ctx, binary)
			if err != nil {
				t.Fatal(err)
			}
			instance := &WazeroInstance{lifetime: newExecutionLifetime(), instance: module, stackBuf: []uint64{42}}
			fn := instance.GetExportedFunction("run")
			done := make(chan error, 1)
			go func() {
				if withStack {
					done <- fn.CallWithStack(ctx, nil)
				} else {
					_, err := fn.Call(ctx)
					done <- err
				}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("host not entered")
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err := instance.Close(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("join: %v", err)
			}
			if instance.closed || len(instance.stackBuf) != 1 {
				t.Fatal("active call resources released")
			}
			unblock()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("call did not return")
			}
			if err := instance.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInstanceFunctionLookupConcurrentClose(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)
	module, err := runtime.Instantiate(ctx, compileSimpleAddWat(t))
	if err != nil {
		t.Fatal(err)
	}
	instance := &WazeroInstance{lifetime: newExecutionLifetime(), instance: module}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				fn := instance.GetExportedFunction("add")
				if fn != nil {
					_ = fn.Definition()
				}
			}
		})
	}
	if err := instance.Close(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}
