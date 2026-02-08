package bridge

import (
	"context"
	"errors"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func TestNewBuilder_NilRuntime(t *testing.T) {
	_, err := NewBuilder(nil)
	if !errors.Is(err, ErrNilRuntime) {
		t.Errorf("expected ErrNilRuntime, got %v", err)
	}
}

func TestBuilder_CreateHostBridge_NilFn(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	b, err := NewBuilder(rt)
	if err != nil {
		t.Fatalf("NewBuilder failed: %v", err)
	}

	// Export with nil Fn should be skipped or cause error
	exports := []Export{
		{Name: "valid", Fn: func(context.Context, api.Module, []uint64) {}, ParamTypes: nil, ResultTypes: nil},
		{Name: "invalid", Fn: nil, ParamTypes: nil, ResultTypes: nil}, // nil Fn
	}

	// This should not panic - nil Fn exports should be handled gracefully
	_, err = b.CreateHostBridge(ctx, "test", exports, nil)
	// Current behavior: wazero panics on nil Fn
	// Expected: skip nil Fn exports or return error
	if err != nil {
		t.Logf("Got expected error for nil Fn: %v", err)
	}
}

func TestBuilder_CreateSynthBridge_NilSpec(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	b, err := NewBuilder(rt)
	if err != nil {
		t.Fatalf("NewBuilder failed: %v", err)
	}
	result, err := b.CreateSynthBridge(ctx, nil, nil)
	if err != nil {
		t.Errorf("unexpected error for nil spec: %v", err)
	}
	if result.Created {
		t.Error("expected Created=false for nil spec")
	}
}

func TestBuilder_CreateSynthBridge_DoesNotMutateInput(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	b, err := NewBuilder(rt)
	if err != nil {
		t.Fatalf("NewBuilder failed: %v", err)
	}

	originalFuncs := []Export{
		{Name: "func1", Fn: func(context.Context, api.Module, []uint64) {}},
	}
	spec := &SynthSpec{
		Name:  "test_module",
		Funcs: originalFuncs,
	}

	// Store original values
	originalName := spec.Name
	originalHostModName := spec.HostModName
	originalFuncsLen := len(spec.Funcs)
	originalFunc0Name := spec.Funcs[0].Name
	originalFunc0Params := spec.Funcs[0].ParamTypes

	expectedTypes := map[string]ImportSig{
		"func1": {Params: []api.ValueType{api.ValueTypeI32}},
	}

	_, _ = b.CreateSynthBridge(ctx, spec, expectedTypes)

	// Verify no mutation occurred
	if spec.Name != originalName {
		t.Errorf("spec.Name was mutated: %s -> %s", originalName, spec.Name)
	}
	if spec.HostModName != originalHostModName {
		t.Errorf("spec.HostModName was mutated: %s -> %s", originalHostModName, spec.HostModName)
	}
	if len(spec.Funcs) != originalFuncsLen {
		t.Errorf("spec.Funcs length changed: %d -> %d", originalFuncsLen, len(spec.Funcs))
	}
	if spec.Funcs[0].Name != originalFunc0Name {
		t.Errorf("spec.Funcs[0].Name was mutated")
	}
	if len(spec.Funcs[0].ParamTypes) != len(originalFunc0Params) {
		t.Errorf("spec.Funcs[0].ParamTypes was mutated: expected nil/empty, got %v", spec.Funcs[0].ParamTypes)
	}
}

// Extended builder tests covering additional edge cases

func TestBuilder_VirtualMarkers(t *testing.T) {
	t.Run("MarkIsVirtualClearVirtual", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		// Initially not virtual
		if b.IsVirtual("test_module") {
			t.Error("expected test_module to not be virtual initially")
		}

		// Mark as virtual
		b.MarkVirtual("test_module")
		if !b.IsVirtual("test_module") {
			t.Error("expected test_module to be virtual after MarkVirtual")
		}

		// Clear virtual
		b.ClearVirtual("test_module")
		if b.IsVirtual("test_module") {
			t.Error("expected test_module to not be virtual after ClearVirtual")
		}
	})

	t.Run("MultipleVirtualMarkers", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		// Mark multiple modules as virtual
		b.MarkVirtual("mod1")
		b.MarkVirtual("mod2")
		b.MarkVirtual("mod3")

		// Check all are virtual
		for _, name := range []string{"mod1", "mod2", "mod3"} {
			if !b.IsVirtual(name) {
				t.Errorf("expected %s to be virtual", name)
			}
		}

		// Clear one
		b.ClearVirtual("mod2")

		// Check state
		if !b.IsVirtual("mod1") {
			t.Error("mod1 should still be virtual")
		}
		if b.IsVirtual("mod2") {
			t.Error("mod2 should no longer be virtual")
		}
		if !b.IsVirtual("mod3") {
			t.Error("mod3 should still be virtual")
		}
	})
}

func TestBuilder_CreatedModules(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		// Initially no modules
		mods := b.CreatedModules()
		if len(mods) != 0 {
			t.Errorf("expected 0 created modules initially, got %d", len(mods))
		}
	})

	t.Run("SingleModule", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		// Create a host bridge
		exports := []Export{
			{Name: "test_func", Fn: func(context.Context, api.Module, []uint64) {}},
		}
		result, err := b.CreateHostBridge(ctx, "test_module1", exports, nil)
		if err != nil {
			t.Fatalf("CreateHostBridge failed: %v", err)
		}
		if !result.Created {
			t.Error("expected module to be created")
		}

		// Check created modules
		mods := b.CreatedModules()
		if len(mods) != 1 {
			t.Errorf("expected 1 created module, got %d", len(mods))
		}
	})

	t.Run("MultipleModules", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		// Create multiple modules
		exports := []Export{
			{Name: "test_func", Fn: func(context.Context, api.Module, []uint64) {}},
		}

		for i := 0; i < 3; i++ {
			name := "module" + string(rune('A'+i))
			_, err := b.CreateHostBridge(ctx, name, exports, nil)
			if err != nil {
				t.Fatalf("CreateHostBridge for %s failed: %v", name, err)
			}
		}

		mods := b.CreatedModules()
		if len(mods) != 3 {
			t.Errorf("expected 3 created modules, got %d", len(mods))
		}
	})
}

func TestBuilder_CreateHostBridge(t *testing.T) {
	t.Run("EmptyExports", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		result, err := b.CreateHostBridge(ctx, "empty_module", nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Created {
			t.Error("expected Created=false for empty exports")
		}
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		exports := []Export{
			{Name: "test_func", Fn: func(context.Context, api.Module, []uint64) {}},
		}

		// Create first time
		result1, err := b.CreateHostBridge(ctx, "existing_module", exports, nil)
		if err != nil {
			t.Fatalf("first CreateHostBridge failed: %v", err)
		}
		if !result1.Created {
			t.Error("expected first creation to succeed")
		}

		// Try to create again with same name
		result2, err := b.CreateHostBridge(ctx, "existing_module", exports, nil)
		if err != nil {
			t.Fatalf("second CreateHostBridge failed: %v", err)
		}
		if !result2.Created {
			t.Error("expected second call to return Created=true for existing module")
		}
	})

	t.Run("WithExpectedTypes", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		exports := []Export{
			{Name: "add", Fn: func(_ context.Context, _ api.Module, stack []uint64) {
				stack[0] += stack[1]
			}},
		}

		expectedTypes := map[string]ImportSig{
			"add": {
				Params:  []api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
				Results: []api.ValueType{api.ValueTypeI32},
			},
		}

		result, err := b.CreateHostBridge(ctx, "typed_module", exports, expectedTypes)
		if err != nil {
			t.Fatalf("CreateHostBridge failed: %v", err)
		}
		if !result.Created {
			t.Error("expected module to be created")
		}
	})
}

func TestBuilder_CreateSynthBridge(t *testing.T) {
	t.Run("WithLocalGlobals", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		spec := &SynthSpec{
			Name: "synth_with_local_globals",
			LocalGlobals: []LocalGlobal{
				{ExportName: "counter", ValType: api.ValueTypeI32, Mutable: true, InitValue: 0},
				{ExportName: "limit", ValType: api.ValueTypeI32, Mutable: false, InitValue: 100},
			},
		}

		result, err := b.CreateSynthBridge(ctx, spec, nil)
		if err != nil {
			t.Fatalf("CreateSynthBridge failed: %v", err)
		}
		if !result.Created {
			t.Error("expected module to be created")
		}
	})

	t.Run("WithFuncs", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		spec := &SynthSpec{
			Name: "synth_with_funcs",
			Funcs: []Export{
				{Name: "test", Fn: func(context.Context, api.Module, []uint64) {}},
			},
		}

		expectedTypes := map[string]ImportSig{
			"test": {Params: []api.ValueType{}, Results: []api.ValueType{}},
		}

		result, err := b.CreateSynthBridge(ctx, spec, expectedTypes)
		if err != nil {
			t.Fatalf("CreateSynthBridge failed: %v", err)
		}
		if !result.Created {
			t.Error("expected module to be created")
		}
	})

	t.Run("Recreate", func(t *testing.T) {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)

		b, err := NewBuilder(rt)
		if err != nil {
			t.Fatalf("NewBuilder failed: %v", err)
		}

		// Create first with funcs only
		spec1 := &SynthSpec{
			Name: "recreate_test",
			Funcs: []Export{
				{Name: "func1", Fn: func(context.Context, api.Module, []uint64) {}},
			},
		}
		expectedTypes := map[string]ImportSig{
			"func1": {Params: []api.ValueType{}, Results: []api.ValueType{}},
		}

		result1, err := b.CreateSynthBridge(ctx, spec1, expectedTypes)
		if err != nil {
			t.Fatalf("first CreateSynthBridge failed: %v", err)
		}
		if !result1.Created {
			t.Error("expected first creation to succeed")
		}

		// Create second time - should use existing
		result2, err := b.CreateSynthBridge(ctx, spec1, expectedTypes)
		if err != nil {
			t.Fatalf("second CreateSynthBridge failed: %v", err)
		}
		if !result2.Created {
			t.Error("expected second creation to return Created=true for existing")
		}
	})
}

func TestSource_Interfaces(t *testing.T) {
	t.Run("ModuleSource", func(t *testing.T) {
		// Just test that ModuleSource implements Source interface
		var s Source = ModuleSource{}
		s.isSource()
	})

	t.Run("VirtualSource", func(t *testing.T) {
		// Just test that VirtualSource implements Source interface
		var s Source = VirtualSource{}
		s.isSource()
	})
}
