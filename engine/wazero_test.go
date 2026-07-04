package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wippyai/wasm-runtime/wat"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := &Config{}
	if cfg.MemoryLimitPages != 0 {
		t.Errorf("expected default MemoryLimitPages 0, got %d", cfg.MemoryLimitPages)
	}
}

func TestConfig_MemoryLimitPages(t *testing.T) {
	cfg := &Config{
		MemoryLimitPages: 256, // 16MB
	}
	if cfg.MemoryLimitPages != 256 {
		t.Errorf("expected MemoryLimitPages 256, got %d", cfg.MemoryLimitPages)
	}
}

func TestNewWazeroEngineWithConfig(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		cfg  *Config
		name string
	}{
		{nil, "nil config"},
		{&Config{}, "default config"},
		{&Config{MemoryLimitPages: 256}, "16MB limit"},
		{&Config{MemoryLimitPages: 1024}, "64MB limit"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := NewWazeroEngineWithConfig(ctx, tc.cfg)
			if err != nil {
				t.Fatalf("NewWazeroEngineWithConfig failed: %v", err)
			}
			defer engine.Close(ctx)

			if engine.runtime == nil {
				t.Error("engine runtime should not be nil")
			}
		})
	}
}

func TestNewWazeroEngine(t *testing.T) {
	ctx := context.Background()

	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine failed: %v", err)
	}
	defer engine.Close(ctx)

	if engine.runtime == nil {
		t.Error("engine runtime should not be nil")
	}
}

func TestWazeroEngine_Close(t *testing.T) {
	ctx := context.Background()

	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine failed: %v", err)
	}

	err = engine.Close(ctx)
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestWazeroEngine_FSMountReadlinkRegularFile(t *testing.T) {
	ctx := context.Background()

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	wasmBytes, err := wat.Compile(`(module
		(import "wasi_snapshot_preview1" "path_readlink"
			(func $path_readlink (param i32 i32 i32 i32 i32 i32) (result i32)))
		(memory (export "memory") 1)
		(data (i32.const 8) "file.txt")
		(func (export "readlink_errno") (result i32)
			(call $path_readlink
				(i32.const 3)
				(i32.const 8)
				(i32.const 8)
				(i32.const 32)
				(i32.const 64)
				(i32.const 4))))`)
	if err != nil {
		t.Fatalf("compile WAT: %v", err)
	}

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		Mounts: []Mount{{
			Guest:    "/data",
			FS:       fstest.MapFS{"file.txt": &fstest.MapFile{Data: []byte("ok")}},
			ReadOnly: true,
		}},
	})
	if err != nil {
		t.Fatalf("InstantiateWithConfig: %v", err)
	}
	defer inst.Close(ctx)

	fn := inst.instance.ExportedFunction("readlink_errno")
	if fn == nil {
		t.Fatal("readlink_errno function not exported")
	}
	results, err := fn.Call(ctx)
	if err != nil {
		t.Fatalf("readlink_errno: %v", err)
	}
	const wasiErrnoInval = uint64(28)
	if got := results[0]; got != wasiErrnoInval {
		t.Fatalf("path_readlink regular file errno = %d, want %d", got, wasiErrnoInval)
	}
}

// TestWazeroEngine_HTTPComponentExports verifies HTTP component loading and export resolution.
func TestWazeroEngine_HTTPComponentExports(t *testing.T) {
	ctx := context.Background()

	// Load the HTTP component - check testbed first, then skip if not available
	data, err := os.ReadFile("../testbed/hello_http.wasm")
	if err != nil {
		t.Skip("hello_http.wasm not found in testbed - this test requires an HTTP component")
	}

	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine failed: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.LoadModule(ctx, data)
	if err != nil {
		t.Fatalf("LoadModule failed: %v", err)
	}

	// Verify canon registry is populated
	if mod.canonRegistry == nil {
		t.Fatal("canonRegistry should not be nil for HTTP component")
	}

	if len(mod.canonRegistry.Lifts) == 0 {
		t.Error("expected at least one canon lift for HTTP component")
	}

	// Verify exports exist
	exports := mod.ExportNames()
	if len(exports) == 0 {
		t.Error("expected at least one export for HTTP component")
	}

	// Verify validated component structure
	if mod.validated == nil {
		t.Fatal("validated component should not be nil")
	}

	comp := mod.validated.Raw
	if len(comp.Exports) == 0 {
		t.Error("expected component exports")
	}

	// Verify HTTP-related lowers exist
	httpLowerCount := 0
	for name := range mod.canonRegistry.Lowers {
		if strings.Contains(name, "http/types") {
			httpLowerCount++
		}
	}
	if httpLowerCount == 0 {
		t.Error("expected HTTP types in canon lowers")
	}
}

func TestWazeroEngine_MemoryLimit(t *testing.T) {
	ctx := context.Background()

	// Create engine with 1 page limit (64KB)
	cfg := &Config{MemoryLimitPages: 1}
	engine, err := NewWazeroEngineWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWazeroEngineWithConfig failed: %v", err)
	}
	defer engine.Close(ctx)

	// Minimal WASM module with 1 page memory
	// (module (memory 1))
	wasmWith1Page := []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version
		0x05, 0x03, 0x01, 0x00, 0x01, // memory section: 1 memory, min=1
	}

	mod, err := engine.LoadModule(ctx, wasmWith1Page)
	if err != nil {
		t.Fatalf("LoadModule failed: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate failed: %v", err)
	}
	defer inst.Close(ctx)
}

func TestWazeroInstance_MemorySize(t *testing.T) {
	ctx := context.Background()

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	t.Run("one_page", func(t *testing.T) {
		wasmBytes := []byte{
			0x00, 0x61, 0x73, 0x6d,
			0x01, 0x00, 0x00, 0x00,
			0x05, 0x03, 0x01, 0x00, 0x01, // memory min=1
		}

		mod, err := eng.LoadModule(ctx, wasmBytes)
		if err != nil {
			t.Fatalf("LoadModule: %v", err)
		}

		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("Instantiate: %v", err)
		}
		defer inst.Close(ctx)

		size := inst.MemorySize()
		if size != 65536 {
			t.Errorf("expected 65536 bytes (1 page), got %d", size)
		}
	})

	t.Run("two_pages", func(t *testing.T) {
		wasmBytes := []byte{
			0x00, 0x61, 0x73, 0x6d,
			0x01, 0x00, 0x00, 0x00,
			0x05, 0x03, 0x01, 0x00, 0x02, // memory min=2
		}

		mod, err := eng.LoadModule(ctx, wasmBytes)
		if err != nil {
			t.Fatalf("LoadModule: %v", err)
		}

		inst, err := mod.Instantiate(ctx)
		if err != nil {
			t.Fatalf("Instantiate: %v", err)
		}
		defer inst.Close(ctx)

		size := inst.MemorySize()
		if size != 131072 {
			t.Errorf("expected 131072 bytes (2 pages), got %d", size)
		}
	})

	t.Run("nil_memory", func(t *testing.T) {
		inst := &WazeroInstance{}
		size := inst.MemorySize()
		if size != 0 {
			t.Errorf("expected 0 for nil memory, got %d", size)
		}
	})
}

func TestWazeroMemory_Size(t *testing.T) {
	ctx := context.Background()

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	wasmBytes := []byte{
		0x00, 0x61, 0x73, 0x6d,
		0x01, 0x00, 0x00, 0x00,
		0x05, 0x03, 0x01, 0x00, 0x03, // memory min=3
	}

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	if inst.memory == nil {
		t.Fatal("expected memory to be set")
	}

	size := inst.memory.Size()
	if size != 196608 {
		t.Errorf("expected 196608 bytes (3 pages), got %d", size)
	}
}

func TestMultiModuleWASIReuse(t *testing.T) {
	ctx := context.Background()

	addWasm, err := wat.Compile(`(module
		(func (export "add") (param i32 i32) (result i32)
			(i32.add (local.get 0) (local.get 1)))
	)`)
	if err != nil {
		t.Fatalf("compile add.wat: %v", err)
	}

	mulWasm, err := wat.Compile(`(module
		(func (export "mul") (param i32 i32) (result i32)
			(i32.mul (local.get 0) (local.get 1)))
	)`)
	if err != nil {
		t.Fatalf("compile mul.wat: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	modAdd, err := eng.LoadModule(ctx, addWasm)
	if err != nil {
		t.Fatalf("LoadModule(add): %v", err)
	}

	modMul, err := eng.LoadModule(ctx, mulWasm)
	if err != nil {
		t.Fatalf("LoadModule(mul): %v", err)
	}

	instAdd, err := modAdd.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate(add): %v", err)
	}
	defer instAdd.Close(ctx)

	instMul, err := modMul.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate(mul): %v", err)
	}
	defer instMul.Close(ctx)

	addFn := instAdd.instance.ExportedFunction("add")
	if addFn == nil {
		t.Fatal("add function not exported")
	}
	results, err := addFn.Call(ctx, 3, 4)
	if err != nil {
		t.Fatalf("add(3,4): %v", err)
	}
	if results[0] != 7 {
		t.Errorf("add(3,4) = %d, want 7", results[0])
	}

	mulFn := instMul.instance.ExportedFunction("mul")
	if mulFn == nil {
		t.Fatal("mul function not exported")
	}
	results, err = mulFn.Call(ctx, 3, 4)
	if err != nil {
		t.Fatalf("mul(3,4): %v", err)
	}
	if results[0] != 12 {
		t.Errorf("mul(3,4) = %d, want 12", results[0])
	}
}

func TestRegisterResourceDropWithoutCanonLower(t *testing.T) {
	ctx := context.Background()

	data, err := os.ReadFile("../testbed/sleep-test.wasm")
	if err != nil {
		t.Skipf("sleep-test.wasm not found in testbed: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, data)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	err = mod.RegisterHostFuncTyped("wasi:io/poll@0.2.8", "[resource-drop]pollable", func(context.Context, uint32) {})
	if err != nil {
		t.Fatalf("RegisterHostFuncTyped(resource-drop) failed: %v", err)
	}
}
