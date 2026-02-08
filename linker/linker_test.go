package linker

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func TestNewLinker(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	if l == nil {
		t.Fatal("NewWithDefaults returned nil")
	}

	if l.Runtime() != rt {
		t.Error("Runtime() mismatch")
	}

	opts := l.Options()
	if !opts.SemverMatching {
		t.Error("expected SemverMatching to be true by default")
	}
}

func TestLinkerDefineFunc(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}
	params := []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}
	results := []api.ValueType{api.ValueTypeI32}

	err := l.DefineFunc("wasi:random/random@0.2.0#get-random-u64", handler, params, results)
	if err != nil {
		t.Fatalf("DefineFunc failed: %v", err)
	}

	def := l.Resolve("wasi:random/random@0.2.0#get-random-u64")
	if def == nil {
		t.Fatal("Resolve returned nil for defined function")
	}

	if def.Name != "get-random-u64" {
		t.Errorf("Name = %q, want %q", def.Name, "get-random-u64")
	}
}

func TestLinkerDefineFunc_InvalidPath(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}

	err := l.DefineFunc("invalid-path-no-hash", handler, nil, nil)
	if err == nil {
		t.Error("expected error for invalid path without #")
	}
}

func TestLinkerNamespace(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	ns := l.Namespace("wasi:io/streams@0.2.0")
	if ns == nil {
		t.Fatal("Namespace returned nil")
	}

	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}
	ns.DefineFunc("read", handler, nil, nil)

	def := l.Resolve("wasi:io/streams@0.2.0#read")
	if def == nil {
		t.Fatal("Resolve returned nil for function in namespace")
	}
}

func TestLinkerNamespace_ColonNoSlash(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Package with colon but no slash after it: "wasi:io" (not "wasi:io/streams")
	ns := l.Namespace("wasi:io")
	if ns == nil {
		t.Fatal("Namespace returned nil")
	}

	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}
	ns.DefineFunc("test", handler, nil, nil)

	def := l.Resolve("wasi:io#test")
	if def == nil {
		t.Fatal("Resolve returned nil for package without slash")
	}
}

func TestLinkerNamespace_NoColon(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Path without package prefix (no colon) - should still work
	ns := l.Namespace("streams/error")
	if ns == nil {
		t.Fatal("Namespace returned nil")
	}

	// Should create nested namespaces
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}
	ns.DefineFunc("read", handler, nil, nil)

	def := l.Resolve("streams/error#read")
	if def == nil {
		t.Fatal("Resolve returned nil for function in namespace without colon")
	}
}

func TestHostModuleBuilder(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	handler := func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 42
	}

	mod, err := l.NewHostModule("test").
		Func("get-value", handler, nil, []api.ValueType{api.ValueTypeI32}).
		Build(ctx)

	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if mod == nil {
		t.Fatal("Build returned nil module")
	}

	// Verify module name
	if mod.Name() != "test" {
		t.Errorf("Module name = %q, want %q", mod.Name(), "test")
	}
}

func TestLinkerRoot(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	root := l.Root()
	if root == nil {
		t.Fatal("Root() returned nil")
	}

	// Should be the same namespace each time
	root2 := l.Root()
	if root2 != root {
		t.Error("Root() should return same namespace")
	}
}

func TestLinkerClose(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Add some definitions
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}
	l.DefineFunc("test#func", handler, nil, nil)

	// Close should reset root namespace
	err := l.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Should not find previously defined function
	def := l.Resolve("test#func")
	if def != nil {
		t.Error("After Close, Resolve should return nil for previously defined function")
	}
}

func TestLinkerNew_WithOptions(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	opts := Options{
		SemverMatching: false,
	}

	l := New(rt, opts)

	got := l.Options()
	if got.SemverMatching != false {
		t.Error("SemverMatching should be false")
	}
}

func TestLinkerResolve_NestedNamespace(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Define function in nested namespace
	ns := l.Namespace("test:minimal/host@0.1.0")
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}
	ns.DefineFunc("add", handler,
		[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		[]api.ValueType{api.ValueTypeI32})

	// Resolve should find it
	def := l.Resolve("test:minimal/host@0.1.0#add")
	if def == nil {
		t.Fatal("Resolve returned nil for defined function")
	}
	if def.Name != "add" {
		t.Errorf("Name = %q, want %q", def.Name, "add")
	}
}

func TestLinkerResolve_SemverMatchingOption(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}

	// Test with SemverMatching=true (should find compatible version)
	t.Run("enabled", func(t *testing.T) {
		l := New(rt, Options{SemverMatching: true})
		ns := l.Namespace("wasi:io/streams@0.2.1")
		ns.DefineFunc("read", handler, nil, nil)

		// Query for 0.2.0 should find 0.2.1 (compatible)
		def := l.Resolve("wasi:io/streams@0.2.0#read")
		if def == nil {
			t.Fatal("with SemverMatching=true, should find compatible version")
		}
	})

	// Test with SemverMatching=false (should require exact match)
	t.Run("disabled", func(t *testing.T) {
		l := New(rt, Options{SemverMatching: false})
		ns := l.Namespace("wasi:io/streams@0.2.1")
		ns.DefineFunc("read", handler, nil, nil)

		// Query for 0.2.0 should NOT find 0.2.1 (exact match required)
		def := l.Resolve("wasi:io/streams@0.2.0#read")
		if def != nil {
			t.Fatal("with SemverMatching=false, should NOT find different version")
		}

		// Exact match should still work
		def = l.Resolve("wasi:io/streams@0.2.1#read")
		if def == nil {
			t.Fatal("exact version match should still work")
		}
	})
}
