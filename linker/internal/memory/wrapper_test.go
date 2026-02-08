package memory

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
)

// memoryWASM is a minimal WASM module with 1 page of memory exported as "memory"
var memoryWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x05, 0x03, 0x01, 0x00, 0x01, // memory section: 1 page, no max
	0x07, 0x0a, 0x01, // export section: 10 bytes, 1 export
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, // name: "memory" (6 bytes + string)
	0x02, 0x00, // kind: memory, index 0
}

func TestWrapMemory_Nil(t *testing.T) {
	mem := WrapMemory(nil)
	if mem != nil {
		t.Error("expected nil for nil memory")
	}
}

func TestWrapAllocator_Nil(t *testing.T) {
	alloc := WrapAllocator(context.Background(), nil)
	if alloc != nil {
		t.Error("expected nil for nil function")
	}
}

func TestWrapper_ReadWrite(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, memoryWASM)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}
	defer mod.Close(ctx)

	mem := WrapMemory(mod.ExportedMemory("memory"))
	if mem == nil {
		t.Fatal("expected non-nil wrapped memory")
	}

	// Test Write
	data := []byte{1, 2, 3, 4}
	err = mem.Write(0, data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Test Read
	read, err := mem.Read(0, 4)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	for i, b := range read {
		if b != data[i] {
			t.Errorf("byte %d: expected %d, got %d", i, data[i], b)
		}
	}
}

func TestWrapper_ReadOutOfBounds(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, memoryWASM)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}
	defer mod.Close(ctx)

	mem := WrapMemory(mod.ExportedMemory("memory"))

	// Try to read beyond memory bounds
	_, err = mem.Read(65536, 1) // exactly at boundary
	if err == nil {
		t.Error("expected error for out of bounds read")
	}
}

func TestWrapper_WriteOutOfBounds(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, memoryWASM)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}
	defer mod.Close(ctx)

	mem := WrapMemory(mod.ExportedMemory("memory"))

	// Try to write beyond memory bounds
	err = mem.Write(65536, []byte{1})
	if err == nil {
		t.Error("expected error for out of bounds write")
	}
}

func TestWrapper_IntegerReadWrite(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, memoryWASM)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}
	defer mod.Close(ctx)

	mem := WrapMemory(mod.ExportedMemory("memory"))

	// Test U8
	if err := mem.WriteU8(0, 42); err != nil {
		t.Fatalf("WriteU8 failed: %v", err)
	}
	v8, err := mem.ReadU8(0)
	if err != nil {
		t.Fatalf("ReadU8 failed: %v", err)
	}
	if v8 != 42 {
		t.Errorf("ReadU8: expected 42, got %d", v8)
	}

	// Test U16
	if err := mem.WriteU16(0, 0x1234); err != nil {
		t.Fatalf("WriteU16 failed: %v", err)
	}
	v16, err := mem.ReadU16(0)
	if err != nil {
		t.Fatalf("ReadU16 failed: %v", err)
	}
	if v16 != 0x1234 {
		t.Errorf("ReadU16: expected 0x1234, got 0x%x", v16)
	}

	// Test U32
	if err := mem.WriteU32(0, 0x12345678); err != nil {
		t.Fatalf("WriteU32 failed: %v", err)
	}
	v32, err := mem.ReadU32(0)
	if err != nil {
		t.Fatalf("ReadU32 failed: %v", err)
	}
	if v32 != 0x12345678 {
		t.Errorf("ReadU32: expected 0x12345678, got 0x%x", v32)
	}

	// Test U64
	if err := mem.WriteU64(0, 0x123456789ABCDEF0); err != nil {
		t.Fatalf("WriteU64 failed: %v", err)
	}
	v64, err := mem.ReadU64(0)
	if err != nil {
		t.Fatalf("ReadU64 failed: %v", err)
	}
	if v64 != 0x123456789ABCDEF0 {
		t.Errorf("ReadU64: expected 0x123456789ABCDEF0, got 0x%x", v64)
	}
}
