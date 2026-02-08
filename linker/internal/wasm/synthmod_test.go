package wasm

import (
	"bytes"
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func TestNewSynthModuleBuilder(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
	if b.hostModuleName != "test" {
		t.Errorf("expected host module name 'test', got '%s'", b.hostModuleName)
	}
	if b.tableSize != 2 {
		t.Errorf("expected default table size 2, got %d", b.tableSize)
	}
}

func TestSynthModuleBuilder_EmptyBuild(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	result := b.Build()
	if result != nil {
		t.Error("expected nil for empty builder")
	}
}

func TestSynthModuleBuilder_AddFunc(t *testing.T) {
	b := NewSynthModuleBuilder("host")
	b.AddFunc("add", []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})

	if len(b.funcs) != 1 {
		t.Fatalf("expected 1 func, got %d", len(b.funcs))
	}
	if b.funcs[0].name != "add" {
		t.Errorf("expected name 'add', got '%s'", b.funcs[0].name)
	}
}

func TestSynthModuleBuilder_SetTableSize(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	b.SetTableSize(10)
	if b.tableSize != 10 {
		t.Errorf("expected table size 10, got %d", b.tableSize)
	}
}

func TestSynthModuleBuilder_SetTableImport(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	b.SetTableImport("env", "$imports", "table")

	if !b.HasTableImport() {
		t.Error("expected HasTableImport to return true")
	}
	if b.tableImportMod != "env" {
		t.Errorf("expected mod 'env', got '%s'", b.tableImportMod)
	}
}

func TestSynthModuleBuilder_SetMemoryImport(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	b.SetMemoryImport("env", "memory", "mem")

	if !b.HasMemoryImport() {
		t.Error("expected HasMemoryImport to return true")
	}
	if b.memoryImportMod != "env" {
		t.Errorf("expected mod 'env', got '%s'", b.memoryImportMod)
	}
}

func TestSynthModuleBuilder_AddGlobalImport(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	b.AddGlobalImport("env", "heap_base", "__heap_base", api.ValueTypeI32, false)

	if len(b.globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(b.globals))
	}
	if b.globals[0].moduleName != "env" {
		t.Error("expected module name 'env'")
	}
	if b.globals[0].isLocal {
		t.Error("expected imported global, not local")
	}
}

func TestSynthModuleBuilder_AddLocalGlobal(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	b.AddLocalGlobal("counter", api.ValueTypeI32, true, 42)

	if len(b.globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(b.globals))
	}
	if !b.globals[0].isLocal {
		t.Error("expected local global")
	}
	if b.globals[0].initValue != 42 {
		t.Errorf("expected init value 42, got %d", b.globals[0].initValue)
	}
}

func TestSynthModuleBuilder_BuildWithFunc(t *testing.T) {
	b := NewSynthModuleBuilder("host")
	b.AddFunc("noop", nil, nil)

	wasm := b.Build()
	if wasm == nil {
		t.Fatal("expected non-nil wasm")
	}

	// Check magic and version
	if !bytes.HasPrefix(wasm, testMagicVersion) {
		t.Error("expected valid WASM header")
	}

	// Verify it's valid WASM by trying to compile
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// First register the host module
	_, err := rt.NewHostModuleBuilder("host").
		NewFunctionBuilder().WithFunc(func() {}).Export("noop").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("failed to create host module: %v", err)
	}

	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatalf("failed to compile synthetic module: %v", err)
	}
	defer compiled.Close(ctx)
}

func TestSynthModuleBuilder_BuildWithLocalGlobal(t *testing.T) {
	b := NewSynthModuleBuilder("host")
	b.AddLocalGlobal("counter", api.ValueTypeI32, true, 100)

	wasm := b.Build()
	if wasm == nil {
		t.Fatal("expected non-nil wasm")
	}

	// Verify it's valid WASM
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("synth"))
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}
	defer mod.Close(ctx)

	// Check exported global
	g := mod.ExportedGlobal("counter")
	if g == nil {
		t.Fatal("expected counter global to be exported")
	}

	val := g.Get()
	if val != 100 {
		t.Errorf("expected global value 100, got %d", val)
	}
}

func TestSynthModuleBuilder_BuildWithMemoryImport(t *testing.T) {
	b := NewSynthModuleBuilder("host")
	b.SetMemoryImport("env", "memory", "mem")

	wasm := b.Build()
	if wasm == nil {
		t.Fatal("expected non-nil wasm")
	}

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create env module with memory
	envMod, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func() {}).Export("_dummy").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("failed to create env module: %v", err)
	}
	_ = envMod

	// The synth module imports memory from env - this will fail if memory doesn't exist
	// but the module structure itself should be valid
	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	compiled.Close(ctx)
}

func TestSynthModuleBuilder_CountGlobals(t *testing.T) {
	b := NewSynthModuleBuilder("test")
	b.AddGlobalImport("env", "g1", "g1", api.ValueTypeI32, false)
	b.AddGlobalImport("env", "g2", "g2", api.ValueTypeI64, true)
	b.AddLocalGlobal("local1", api.ValueTypeF32, false, 0)

	if b.countImportedGlobals() != 2 {
		t.Errorf("expected 2 imported globals, got %d", b.countImportedGlobals())
	}
	if b.countLocalGlobals() != 1 {
		t.Errorf("expected 1 local global, got %d", b.countLocalGlobals())
	}
}

func TestValTypeToWasm_AllTypes(t *testing.T) {
	tests := []struct {
		input    api.ValueType
		expected byte
	}{
		{api.ValueTypeI32, 0x7f},
		{api.ValueTypeI64, 0x7e},
		{api.ValueTypeF32, 0x7d},
		{api.ValueTypeF64, 0x7c},
	}

	for _, tc := range tests {
		result := ValTypeToWasm(tc.input)
		if result != tc.expected {
			t.Errorf("ValTypeToWasm(%v) = 0x%02x, want 0x%02x", tc.input, result, tc.expected)
		}
	}
}
