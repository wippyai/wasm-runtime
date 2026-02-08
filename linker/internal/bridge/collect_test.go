package bridge

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
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
