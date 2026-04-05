package engine

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/wat"
	"go.bytecodealliance.org/wit"
)

// WAT module that exports cabi_realloc + a string echo function.
// cabi_realloc: bump allocator using a global pointer.
// echo: copies (ptr, len) input to a return struct at a fixed address.
const echoWAT = `(module
	(memory (export "memory") 4)

	;; bump pointer starts after the first 1024 bytes (reserved for return values)
	(global $bump (mut i32) (i32.const 1024))

	;; cabi_realloc(old_ptr, old_size, align, new_size) -> new_ptr
	(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
		(local $ptr i32)
		(local.set $ptr (global.get $bump))
		(global.set $bump (i32.add (global.get $bump) (local.get 3)))
		(local.get $ptr)
	)

	;; echo(ptr, len) -> retptr
	;; writes {ptr, len} at address 0 and returns 0
	(func (export "echo") (param $ptr i32) (param $len i32) (result i32)
		(i32.store (i32.const 0) (local.get $ptr))
		(i32.store (i32.const 4) (local.get $len))
		(i32.const 0)
	)
)`

// TestAllocatorLookup_AnonymousModule verifies that cabi_realloc is found
// when the module is instantiated with an empty name (anonymous module).
// This is the core bug: FunctionDefinition.Name() returns "" for anonymous
// modules, so ExportedFunction(def.Name()) returned nil.
func TestAllocatorLookup_AnonymousModule(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := wat.Compile(echoWAT)
	if err != nil {
		t.Fatalf("compile WAT: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	// Instantiate with empty name (anonymous) — this is how wippy creates instances
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{Name: ""})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	if inst.allocFn == nil {
		t.Fatal("allocFn is nil — cabi_realloc was not found for anonymous module")
	}

	if inst.memory == nil {
		t.Fatal("memory is nil")
	}
}

// TestAllocatorLookup_NamedModule verifies that cabi_realloc is found
// for a named module (control case).
func TestAllocatorLookup_NamedModule(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := wat.Compile(echoWAT)
	if err != nil {
		t.Fatalf("compile WAT: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{Name: "test-module"})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	if inst.allocFn == nil {
		t.Fatal("allocFn is nil — cabi_realloc was not found for named module")
	}
}

// TestCallWithTypes_StringParam_AnonymousModule verifies that string parameters
// can be encoded and passed to a WASM function in an anonymous module.
// This is the end-to-end test for the bug: without the fix, this fails with
// "failed to allocate N bytes for string data" because allocFn is nil.
func TestCallWithTypes_StringParam_AnonymousModule(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := wat.Compile(echoWAT)
	if err != nil {
		t.Fatalf("compile WAT: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{Name: ""})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Call echo with a string parameter via CallWithTypes
	paramTypes := []wit.Type{wit.String{}}
	resultTypes := []wit.Type{wit.String{}}

	result, err := inst.CallWithTypes(ctx, "echo", paramTypes, resultTypes, "hello world")
	if err != nil {
		t.Fatalf("CallWithTypes failed: %v", err)
	}

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T: %v", result, result)
	}
	if s != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", s)
	}
}

// TestAllocator_NullPointerCheck verifies that the allocator returns an error
// when cabi_realloc returns 0 for a non-zero allocation.
func TestAllocator_NullPointerCheck(t *testing.T) {
	ctx := context.Background()

	// Module where cabi_realloc always returns 0 (simulates allocation failure)
	nullAllocWAT := `(module
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			(i32.const 0)
		)
	)`

	wasmBytes, err := wat.Compile(nullAllocWAT)
	if err != nil {
		t.Fatalf("compile WAT: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{Name: ""})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	if inst.allocFn == nil {
		t.Fatal("allocFn should not be nil")
	}

	// Alloc with size > 0 should fail when cabi_realloc returns 0
	inst.alloc.setContext(ctx)
	_, err = inst.alloc.Alloc(16, 1)
	if err == nil {
		t.Fatal("expected error for null pointer allocation")
	}

	// Alloc with size == 0 should succeed (ptr=0 is valid for zero-size)
	ptr, err := inst.alloc.Alloc(0, 1)
	if err != nil {
		t.Fatalf("zero-size alloc should succeed: %v", err)
	}
	if ptr != 0 {
		t.Errorf("expected ptr=0 for zero-size alloc, got %d", ptr)
	}
}

// TestAllocatorLookup_MultipleInstances verifies that allocFn is correctly
// found for each new instance created from the same module.
func TestAllocatorLookup_MultipleInstances(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := wat.Compile(echoWAT)
	if err != nil {
		t.Fatalf("compile WAT: %v", err)
	}

	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	defer eng.Close(ctx)

	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	for i := 0; i < 5; i++ {
		inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{Name: ""})
		if err != nil {
			t.Fatalf("Instantiate #%d: %v", i, err)
		}

		if inst.allocFn == nil {
			t.Fatalf("allocFn is nil on instance #%d", i)
		}

		inst.alloc.setContext(ctx)
		ptr, err := inst.alloc.Alloc(64, 1)
		if err != nil {
			t.Fatalf("Alloc on instance #%d: %v", i, err)
		}
		if ptr == 0 {
			t.Fatalf("expected non-zero ptr on instance #%d", i)
		}

		inst.Close(ctx)
	}
}
