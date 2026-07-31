package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"go.bytecodealliance.org/wit"
)

// A core module cannot use the typed host path: lowering goes through the Canon ABI
// and needs a component's canon imports. Registering the signature outright is the
// only way its imports can be satisfied, and without a host module instantiated for
// the namespace, instantiation fails with "module[...] not instantiated".
func TestCoreModuleCallsRawHostFunc(t *testing.T) {
	wasmBytes, err := os.ReadFile("../testbed/core-host.wasm")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer rt.Close(ctx)

	calls := 0
	err = rt.RegisterCoreFunc("wippy:http/fetch", "probe",
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32},
		func(_ context.Context, _ api.Module, stack []uint64) {
			calls++
			stack[0] = uint64(uint32(stack[0]) + uint32(stack[1]))
		}, false)
	if err != nil {
		t.Fatalf("RegisterCoreFunc: %v", err)
	}

	mod, err := rt.LoadWASM(ctx, wasmBytes, "")
	if err != nil {
		t.Fatalf("LoadWASM: %v", err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	got, err := inst.CallWithTypes(ctx, "run",
		[]wit.Type{wit.U32{}, wit.U32{}}, []wit.Type{wit.U32{}}, uint32(20), uint32(22))
	if err != nil {
		t.Fatalf("call run: %v", err)
	}
	if calls != 1 {
		t.Errorf("host function was called %d times, want 1", calls)
	}
	if got != uint32(42) {
		t.Errorf("run returned %#v, want uint32(42)", got)
	}
}

// A module with no raw host functions must instantiate exactly as before.
func TestRawHostRegistrationIsOptional(t *testing.T) {
	wasmBytes, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Skipf("minimal fixture unavailable: %v", err)
	}

	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer rt.Close(ctx)

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Skipf("minimal fixture is not a component: %v", err)
	}
	if _, err := mod.Instantiate(ctx); err != nil {
		t.Fatalf("instantiate without raw hosts: %v", err)
	}
}
