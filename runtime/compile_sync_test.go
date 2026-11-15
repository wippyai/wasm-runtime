package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/engine"
)

func loadAsyncCoreHost(t *testing.T) (context.Context, *Module) {
	t.Helper()
	wasmBytes, err := os.ReadFile("../testbed/core-host.wasm")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close(ctx) })
	err = rt.RegisterCoreFunc("wippy:http/fetch", "probe",
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = uint64(uint32(stack[0]) + uint32(stack[1]))
		}, true)
	if err != nil {
		t.Fatalf("RegisterCoreFunc: %v", err)
	}
	mod, err := rt.LoadWASM(ctx, wasmBytes, "")
	if err != nil {
		t.Fatalf("LoadWASM: %v", err)
	}
	return ctx, mod
}

func TestCompileTransformsModuleWithAsyncHostImport(t *testing.T) {
	ctx, mod := loadAsyncCoreHost(t)
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !mod.wazeroModule.IsTransformed() {
		t.Fatal("module with an async host import was not transformed")
	}
}

func TestCompileSynchronousSkipsAsyncifyTransform(t *testing.T) {
	ctx, mod := loadAsyncCoreHost(t)
	if err := mod.CompileWithConfig(ctx, &engine.CompileConfig{Synchronous: true}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if mod.wazeroModule.IsTransformed() {
		t.Fatal("synchronous module was transformed")
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)
	got, err := inst.CallWithTypes(ctx, "run",
		[]wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(20), uint32(22))
	if err != nil {
		t.Fatalf("call run: %v", err)
	}
	if got != uint32(42) {
		t.Fatalf("run returned %#v, want uint32(42)", got)
	}
}

func TestCompileSynchronousComponent(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(ctx) })
	if err := rt.RegisterHost(&MinimalHost{}); err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.LoadComponent(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.CompileWithConfig(ctx, &engine.CompileConfig{Synchronous: true}); err != nil {
		t.Fatal(err)
	}
	instance, err := mod.InstantiateWithConfig(ctx, &engine.InstanceConfig{EnableAsyncify: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close(ctx) })
	if instance.Scheduler() != nil {
		t.Fatal("synchronous component acquired an async scheduler")
	}
	got, err := instance.Call(ctx, "compute", uint32(5), uint32(3))
	if err != nil {
		t.Fatal(err)
	}
	if got != uint32(15) {
		t.Fatalf("compute = %#v, want 15", got)
	}
}
