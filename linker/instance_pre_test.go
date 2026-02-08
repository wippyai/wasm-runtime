package linker

import (
	"context"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
)

func TestLinker_Instantiate_NilComponent(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	_, err := l.Instantiate(ctx, nil)
	if err == nil {
		t.Error("Instantiate should error for nil component")
	}
}

func TestLinker_Instantiate_NilRaw(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	c := &component.ValidatedComponent{Raw: nil}

	_, err := l.Instantiate(ctx, c)
	if err == nil {
		t.Error("Instantiate should error for nil Raw")
	}
}

func TestLinker_Instantiate_EmptyComponent(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	c := &component.ValidatedComponent{
		Raw: &component.Component{},
	}

	pre, err := l.Instantiate(ctx, c)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}

	if pre == nil {
		t.Fatal("Instantiate returned nil")
	}

	if pre.Component() != c {
		t.Error("Component() mismatch")
	}
}

func TestInstancePre_CompiledModules_Empty(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	c := &component.ValidatedComponent{
		Raw: &component.Component{},
	}

	pre, _ := l.Instantiate(ctx, c)
	mods := pre.CompiledModules()

	if len(mods) != 0 {
		t.Errorf("CompiledModules() should be empty, got %d", len(mods))
	}
}

func TestInstancePre_Close(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	c := &component.ValidatedComponent{
		Raw: &component.Component{},
	}

	pre, _ := l.Instantiate(ctx, c)
	err := pre.Close(ctx)

	if err != nil {
		t.Errorf("Close error: %v", err)
	}

	if pre.compiled != nil {
		t.Error("compiled should be nil after Close")
	}
}

func TestInstancePre_NewInstance_EmptyComponent(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	c := &component.ValidatedComponent{
		Raw: &component.Component{},
	}

	pre, _ := l.Instantiate(ctx, c)
	inst, err := pre.NewInstance(ctx)

	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}

	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}
}

func loadTestComponent(t *testing.T, path string) *component.ValidatedComponent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	validated, err := component.DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("decode and validate: %v", err)
	}
	return validated
}

func TestLinker_Instantiate_MinimalComponent(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	// Define the host function that minimal.wasm expects
	ns := l.Namespace("test:minimal/host@0.1.0")
	addHandler := func(ctx context.Context, mod api.Module, stack []uint64) {
		a := uint32(stack[0])
		b := uint32(stack[1])
		stack[0] = uint64(a + b)
	}
	ns.DefineFunc("add", addHandler,
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32})

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	if len(pre.CompiledModules()) == 0 {
		t.Error("Expected compiled modules")
	}
}

func TestLinker_NewInstance_WithHostFunc(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	// Define the host function
	ns := l.Namespace("test:minimal/host@0.1.0")
	addHandler := func(ctx context.Context, mod api.Module, stack []uint64) {
		a := uint32(stack[0])
		b := uint32(stack[1])
		stack[0] = uint64(a + b)
	}
	ns.DefineFunc("add", addHandler,
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32})

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	// Create an instance
	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	if len(inst.Modules()) == 0 {
		t.Fatal("Expected at least one module")
	}

	// The core module exports "compute" (multiplication) and "compute-using-host" (uses host add)
	mod := inst.Modules()[0]

	// Test compute (does multiplication)
	computeFn := mod.ExportedFunction("compute")
	if computeFn != nil {
		results, err := computeFn.Call(ctx, 3, 4)
		if err != nil {
			t.Errorf("compute call error: %v", err)
		} else if len(results) > 0 && results[0] != 12 {
			t.Errorf("compute(3, 4) = %d, want 12", results[0])
		} else {
			t.Log("compute(3, 4) = 12 - multiplication works!")
		}
	}

	// Test compute-using-host (uses host add function)
	computeHostFn := mod.ExportedFunction("compute-using-host")
	if computeHostFn != nil {
		results, err := computeHostFn.Call(ctx, 3, 4)
		if err != nil {
			t.Errorf("compute-using-host call error: %v", err)
		} else if len(results) > 0 && results[0] != 7 {
			t.Errorf("compute-using-host(3, 4) = %d, want 7", results[0])
		} else {
			t.Log("compute-using-host(3, 4) = 7 - host function works!")
		}
	}

	// Check component exports
	exp, ok := inst.GetExport("compute")
	if ok && exp.CoreFunc != nil {
		t.Log("Component export 'compute' has CoreFunc set!")
	}

	// Test Instance.CallRaw method (raw uint64 access)
	results, err := inst.CallRaw(ctx, "compute", 5, 6)
	if err != nil {
		t.Errorf("Instance.CallRaw error: %v", err)
	} else if len(results) > 0 && results[0] != 30 {
		t.Errorf("Instance.CallRaw(compute, 5, 6) = %d, want 30", results[0])
	} else {
		t.Log("Instance.CallRaw(compute, 5, 6) = 30 - works!")
	}
}

func TestLinker_Instantiate_CompileError(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Component with invalid WASM bytes
	c := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{{0x00, 0x61, 0x73, 0x6d, 0x99, 0x99, 0x99, 0x99}}, // invalid version
		},
	}

	_, err := l.Instantiate(ctx, c)
	if err == nil {
		t.Error("Instantiate should error for invalid WASM")
	}
}

func TestResolveBindings_ModuleIndexOutOfRange(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create a component with core instances that reference non-existent module
	c := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{}, // Empty - no modules
			CoreInstances: []component.CoreInstance{
				{Parsed: &component.ParsedCoreInstance{
					Kind:        component.CoreInstanceInstantiate,
					ModuleIndex: 99, // Out of range
				}},
			},
		},
	}

	_, err := l.Instantiate(ctx, c)
	if err == nil {
		t.Error("Instantiate should error for module index out of range")
	}
}
