package engine

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestCompilationCacheRuntimeIsolation(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	t.Cleanup(func() { _ = cache.Close(ctx) })
	raw, err := wat.Compile(`(module
  (import "env" "value" (func $value (result i32)))
  (memory (export "memory") 1)
  (func (export "value") (result i32) call $value))`)
	if err != nil {
		t.Fatal(err)
	}
	newInstance := func(value uint32) (*WazeroEngine, api.Module) {
		t.Helper()
		eng, err := NewWazeroEngineWithConfig(ctx, &Config{CompilationCache: cache, CloseOnContextDone: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = eng.Close(ctx) })
		_, err = eng.runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func() uint32 { return value }).Export("value").Instantiate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := eng.runtime.CompileModule(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		mod, err := eng.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
		if err != nil {
			t.Fatal(err)
		}
		return eng, mod
	}
	first, a := newInstance(11)
	_, b := newInstance(22)
	if !a.Memory().WriteUint32Le(0, 1234) {
		t.Fatal("write")
	}
	if got, ok := b.Memory().ReadUint32Le(0); !ok || got != 0 {
		t.Fatalf("shared memory: %d", got)
	}
	check := func(mod api.Module, want uint64) {
		t.Helper()
		result, err := mod.ExportedFunction("value").Call(ctx)
		if err != nil || len(result) != 1 || result[0] != want {
			t.Fatalf("host binding: %v, %v; want %d", result, err, want)
		}
	}
	check(a, 11)
	check(b, 22)
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	check(b, 22)
	_, c := newInstance(33)
	check(c, 33)
	check(b, 22)
}

// A shared cache must never reuse code without cancellation instrumentation for
// a runtime that requests it. Bound the regression externally in case it fails.
func TestCompilationCacheCancellationPartition(t *testing.T) {
	const childKey = "WIPPY_CACHE_CANCEL_CHILD"
	if os.Getenv(childKey) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompilationCacheCancellationPartition$", "-test.count=1")
		cmd.Env = append(os.Environ(), childKey+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cancellation subprocess: %v (%v)\n%s", err, ctx.Err(), out)
		}
		return
	}
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	t.Cleanup(func() { _ = cache.Close(ctx) })
	raw, err := wat.Compile(`(module (func (export "run") (loop br 0)))`)
	if err != nil {
		t.Fatal(err)
	}
	unchecked, err := NewWazeroEngineWithConfig(ctx, &Config{CompilationCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer unchecked.Close(ctx)
	if _, err := unchecked.runtime.CompileModule(ctx, raw); err != nil {
		t.Fatal(err)
	}
	checked, err := NewWazeroEngineWithConfig(ctx, &Config{CompilationCache: cache, CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer checked.Close(ctx)
	mod, err := checked.runtime.Instantiate(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := mod.ExportedFunction("run").Call(callCtx); err == nil {
		t.Fatal("infinite loop did not cancel")
	}
	if callCtx.Err() == nil || !mod.IsClosed() {
		t.Fatal("cancellation must close the executing instance")
	}
}
