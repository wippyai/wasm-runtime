package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestStartupLifetimeExistsInsideInitializer(t *testing.T) {
	for _, mode := range []string{"success", "close", "trap", "reactor-close", "reactor-trap"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			engine, err := NewWazeroEngineWithConfig(ctx, &Config{CloseOnContextDone: true})
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close(ctx)
			suffix := ""
			if mode == "trap" || mode == "reactor-trap" {
				suffix = " unreachable"
			}
			start := "(start $init)"
			if mode == "reactor-close" || mode == "reactor-trap" {
				start = `(export "_initialize" (func $init))`
			}
			binary, err := wat.Compile(`(module (import "startup" "mark" (func $mark)) (func $init call $mark` + suffix + `) ` + start + ` (func (export "run") (result i32) i32.const 42))`)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := engine.LoadModule(ctx, binary)
			if err != nil {
				t.Fatal(err)
			}
			var startup *executionLifetime
			_, err = compiled.runtime.NewHostModuleBuilder("startup").NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, module api.Module, _ []uint64) {
				marker, ok := ctx.Value(executionLeaseKey{}).(*executionLease)
				if !ok || !marker.active.Load() {
					t.Error("initializer entered without active execution owner")
					return
				}
				startup = marker.lifetime
				if mode == "close" || mode == "reactor-close" {
					if err := module.Close(ctx); err != nil {
						t.Error(err)
					}
				}
			}), nil, nil).Export("mark").Instantiate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			notifications := 0
			instance, err := compiled.InstantiateWithConfig(ctx, &InstanceConfig{OnCoreModuleClosed: func(context.Context, uint32) {
				notifications++
				if startup == nil {
					t.Error("notification predates execution owner")
					return
				}
				startup.mu.Lock()
				stopped := startup.stopped
				startup.mu.Unlock()
				if !stopped {
					t.Error("observer ran before domain stopped")
				}
			}})
			if startup == nil {
				t.Fatal("initializer not observed")
			}
			if mode != "success" {
				if err == nil || instance != nil {
					t.Fatalf("closed/trapped startup published instance: %v %v", instance, err)
				}
				if err := startup.startupResult(ctx); !errors.Is(err, errExecutionLifetimeStopped) {
					t.Fatalf("failed startup retained domain: %v", err)
				}
				if err := startup.wait(ctx); err != nil {
					t.Fatal(err)
				}
				// Wazero installs CloseNotifier after the Wasm start section,
				// but before exported reactor initialization. Explicit startup failure
				// cleanup must cover the section without relying on any callback.
				expected := 0
				if mode == "reactor-close" || mode == "reactor-trap" {
					expected = 1
				}
				if notifications != expected {
					t.Fatalf("close notifications=%d", notifications)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if instance.lifetime != startup {
				t.Fatal("startup owner replaced after initialization")
			}
			result, err := instance.GetExportedFunction("run").Call(ctx)
			if err != nil || len(result) != 1 || result[0] != 42 {
				t.Fatalf("resident call: %v %v", result, err)
			}
			if err := instance.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if notifications != 1 {
				t.Fatalf("notifications=%d", notifications)
			}
		})
	}
}
