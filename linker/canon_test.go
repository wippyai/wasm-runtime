package linker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestLiftOwn_ShouldNotCallDestructor(t *testing.T) {
	// LiftOwn transfers ownership - destructor should NOT be called
	// The destructor is only called when the resource is actually destroyed
	dtorCalled := false
	dtor := func(rep uint32) { dtorCalled = true }

	store := NewResourceStore()
	table := store.TableWithDtor(1, dtor)

	h := table.New(42)

	ctx := &LiftContext{
		Ctx:   context.Background(),
		Store: store,
	}

	rep, err := LiftOwn(ctx, 1, h)
	if err != nil {
		t.Fatalf("LiftOwn failed: %v", err)
	}
	if rep != 42 {
		t.Errorf("rep = %d, want 42", rep)
	}

	// The destructor should NOT have been called - we're just transferring ownership
	if dtorCalled {
		t.Error("LiftOwn should NOT call destructor - ownership is being transferred, not destroyed")
	}
}

func TestLiftOwn_InvalidHandle(t *testing.T) {
	store := NewResourceStore()
	ctx := &LiftContext{
		Ctx:   context.Background(),
		Store: store,
	}

	_, err := LiftOwn(ctx, 1, Handle(999))
	if err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestLiftBorrow_InvalidHandle(t *testing.T) {
	store := NewResourceStore()
	ctx := &LiftContext{
		Ctx:   context.Background(),
		Store: store,
	}

	_, err := LiftBorrow(ctx, 1, Handle(999))
	// Should return error, not silently return 0
	if err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestLiftBorrow_ValidHandle(t *testing.T) {
	store := NewResourceStore()
	table := store.Table(1)
	h := table.New(42)

	ctx := &LiftContext{
		Ctx:   context.Background(),
		Store: store,
	}

	rep, err := LiftBorrow(ctx, 1, h)
	if err != nil {
		t.Fatalf("LiftBorrow failed: %v", err)
	}
	if rep != 42 {
		t.Errorf("rep = %d, want 42", rep)
	}
}

func TestLowerOwn(t *testing.T) {
	store := NewResourceStore()
	ctx := &LowerContext{
		Ctx:   context.Background(),
		Store: store,
	}

	h := LowerOwn(ctx, 1, 42)

	// Verify the handle works
	table := store.Table(1)
	rep, ok := table.Rep(h)
	if !ok {
		t.Fatal("Rep failed for lowered handle")
	}
	if rep != 42 {
		t.Errorf("rep = %d, want 42", rep)
	}
}

func TestLowerBorrow(t *testing.T) {
	store := NewResourceStore()
	table := store.Table(1)
	h := table.New(42)

	ctx := &LowerContext{
		Ctx:   context.Background(),
		Store: store,
	}

	borrowed, err := LowerBorrow(ctx, 1, h)
	if err != nil {
		t.Fatalf("LowerBorrow failed: %v", err)
	}
	if borrowed != h {
		t.Errorf("borrowed = %d, want %d", borrowed, h)
	}

	// Verify borrow count increased
	err = table.EndBorrow(h)
	if err != nil {
		t.Error("EndBorrow should succeed after LowerBorrow")
	}
}

func TestLowerBorrow_InvalidHandle(t *testing.T) {
	store := NewResourceStore()
	ctx := &LowerContext{
		Ctx:   context.Background(),
		Store: store,
	}

	_, err := LowerBorrow(ctx, 1, Handle(999))
	if err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestEndLowerBorrow(t *testing.T) {
	store := NewResourceStore()
	table := store.Table(1)
	h := table.New(42)

	ctx := &LowerContext{
		Ctx:   context.Background(),
		Store: store,
	}

	// Borrow
	_, err := LowerBorrow(ctx, 1, h)
	if err != nil {
		t.Fatalf("LowerBorrow failed: %v", err)
	}

	// End borrow
	err = EndLowerBorrow(ctx, 1, h)
	if err != nil {
		t.Fatalf("EndLowerBorrow failed: %v", err)
	}

	// Ending again should fail (no active borrow)
	err = EndLowerBorrow(ctx, 1, h)
	if err == nil {
		t.Error("EndLowerBorrow should fail when no active borrow")
	}
}

func TestNewLiftContext(t *testing.T) {
	store := NewResourceStore()
	opts := CanonicalOptions{Encoding: StringEncodingUTF8}

	ctx := NewLiftContext(context.Background(), opts, store)

	if ctx.Ctx == nil {
		t.Error("Ctx is nil")
	}
	if ctx.Store != store {
		t.Error("Store mismatch")
	}
	if ctx.Options.Encoding != StringEncodingUTF8 {
		t.Error("Options not set")
	}
}

func TestNewLowerContext(t *testing.T) {
	store := NewResourceStore()
	opts := CanonicalOptions{Encoding: StringEncodingUTF16}

	ctx := NewLowerContext(context.Background(), opts, store)

	if ctx.Ctx == nil {
		t.Error("Ctx is nil")
	}
	if ctx.Store != store {
		t.Error("Store mismatch")
	}
	if ctx.Options.Encoding != StringEncodingUTF16 {
		t.Error("Options not set")
	}
}

func TestLiftString_NilMemory(t *testing.T) {
	ctx := &LiftContext{
		Ctx:     context.Background(),
		Options: CanonicalOptions{Memory: nil},
	}

	_, err := LiftString(ctx, 0, 10)
	// Should return error when memory is nil
	if err == nil {
		t.Error("expected error for nil memory")
	}
}

func TestLowerString_NilMemory(t *testing.T) {
	ctx := &LowerContext{
		Ctx:     context.Background(),
		Options: CanonicalOptions{Memory: nil},
	}

	_, _, err := LowerString(ctx, "test")
	// Should return error when memory is nil
	if err == nil {
		t.Error("expected error for nil memory")
	}
}

func TestLiftList_NilMemory(t *testing.T) {
	ctx := &LiftContext{
		Ctx:     context.Background(),
		Options: CanonicalOptions{Memory: nil},
	}

	_, err := LiftList(ctx, 0, 10, 4, func(b []byte) (any, error) { return nil, nil })
	// Should return error when memory is nil
	if err == nil {
		t.Error("expected error for nil memory")
	}
}

// Test that string encoding constants are correct
func TestStringEncodingConstants(t *testing.T) {
	if StringEncodingUTF8 != 0 {
		t.Errorf("StringEncodingUTF8 = %d, want 0", StringEncodingUTF8)
	}
	if StringEncodingUTF16 != 1 {
		t.Errorf("StringEncodingUTF16 = %d, want 1", StringEncodingUTF16)
	}
	if StringEncodingLatin1 != 2 {
		t.Errorf("StringEncodingLatin1 = %d, want 2", StringEncodingLatin1)
	}
}

func TestLowerList_NilMemory(t *testing.T) {
	ctx := &LowerContext{
		Ctx:     context.Background(),
		Options: CanonicalOptions{Memory: nil},
	}

	_, _, err := LowerList(ctx, 4, [][]byte{{1, 2, 3, 4}})
	if !errors.Is(err, ErrNilMemory) {
		t.Errorf("expected ErrNilMemory, got %v", err)
	}
}

func TestLowerList_EmptyList(t *testing.T) {
	ctx := &LowerContext{
		Ctx:     context.Background(),
		Options: CanonicalOptions{Memory: nil}, // nil is OK for empty list
	}

	ptr, length, err := LowerList(ctx, 4, [][]byte{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if ptr != 0 || length != 0 {
		t.Errorf("expected (0, 0), got (%d, %d)", ptr, length)
	}
}

func TestCanonicalHandler_Fields(t *testing.T) {
	ch := CanonicalHandler{
		FuncType: 42,
	}
	if ch.FuncType != 42 {
		t.Errorf("FuncType = %d, want 42", ch.FuncType)
	}
	if ch.Lift != nil {
		t.Error("Lift should be nil by default")
	}
	if ch.Lower != nil {
		t.Error("Lower should be nil by default")
	}
}

func TestCanonicalOptions_Fields(t *testing.T) {
	opts := CanonicalOptions{
		Encoding: StringEncodingLatin1,
	}
	if opts.Encoding != StringEncodingLatin1 {
		t.Errorf("Encoding = %d, want %d", opts.Encoding, StringEncodingLatin1)
	}
}

func TestLiftString_WithRealMemory(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create module with memory
	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	mem := mod.Memory()
	if mem == nil {
		t.Fatal("memory is nil")
	}

	// Write a string to memory
	testStr := "hello"
	if !mem.Write(100, []byte(testStr)) {
		t.Fatal("memory write failed")
	}

	liftCtx := &LiftContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mem},
	}

	result, err := LiftString(liftCtx, 100, uint32(len(testStr)))
	if err != nil {
		t.Fatalf("LiftString: %v", err)
	}
	if result != testStr {
		t.Errorf("LiftString = %q, want %q", result, testStr)
	}
}

func TestLiftString_ReadFails(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create module with 1 page of memory (64KB)
	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	liftCtx := &LiftContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mod.Memory()},
	}

	// Try to read beyond memory bounds
	_, err = LiftString(liftCtx, 65535, 100)
	if err == nil {
		t.Error("expected error for out-of-bounds read")
	}
}

func TestLowerString_WithRealMemory(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create module with memory and realloc
	wasmBytes, err := wat.Compile(`
		(module
			(memory (export "memory") 1)
			(global $bump (mut i32) (i32.const 0))
			(func (export "realloc") (param i32 i32 i32 i32) (result i32)
				(local $ptr i32)
				global.get $bump
				local.set $ptr
				global.get $bump
				local.get 3
				i32.add
				global.set $bump
				local.get $ptr
			)
		)
	`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	lowerCtx := &LowerContext{
		Ctx: ctx,
		Options: CanonicalOptions{
			Memory:  mod.Memory(),
			Realloc: mod.ExportedFunction("realloc"),
		},
	}

	testStr := "hello world"
	ptr, length, err := LowerString(lowerCtx, testStr)
	if err != nil {
		t.Fatalf("LowerString: %v", err)
	}
	if length != uint32(len(testStr)) {
		t.Errorf("length = %d, want %d", length, len(testStr))
	}

	// Read back from memory
	data, ok := mod.Memory().Read(ptr, length)
	if !ok {
		t.Fatal("memory read failed")
	}
	if string(data) != testStr {
		t.Errorf("read back %q, want %q", string(data), testStr)
	}
}

func TestLowerString_NilRealloc(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	lowerCtx := &LowerContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mod.Memory(), Realloc: nil},
	}

	_, _, err = LowerString(lowerCtx, "test")
	if !errors.Is(err, ErrNilRealloc) {
		t.Errorf("expected ErrNilRealloc, got %v", err)
	}
}

func TestLiftList_WithRealMemory(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	mem := mod.Memory()
	// Write 3 i32 values at offset 0
	mem.WriteUint32Le(0, 10)
	mem.WriteUint32Le(4, 20)
	mem.WriteUint32Le(8, 30)

	liftCtx := &LiftContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mem},
	}

	liftElem := func(b []byte) (any, error) {
		val := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
		return val, nil
	}

	result, err := LiftList(liftCtx, 0, 3, 4, liftElem)
	if err != nil {
		t.Fatalf("LiftList: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d elements, want 3", len(result))
	}
	if result[0].(uint32) != 10 || result[1].(uint32) != 20 || result[2].(uint32) != 30 {
		t.Errorf("values mismatch: %v", result)
	}
}

func TestLiftList_ElementError(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	mem := mod.Memory()
	mem.WriteUint32Le(0, 10)

	liftCtx := &LiftContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mem},
	}

	liftErr := fmt.Errorf("element error")
	liftElem := func(b []byte) (any, error) {
		return nil, liftErr
	}

	_, err = LiftList(liftCtx, 0, 1, 4, liftElem)
	if err == nil {
		t.Error("expected error from liftElem")
	}
}

func TestLiftList_ReadFails(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	liftCtx := &LiftContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mod.Memory()},
	}

	// Try to read beyond memory bounds (1 page = 64KB)
	_, err = LiftList(liftCtx, 100000, 1, 4, func(b []byte) (any, error) { return nil, nil })
	if err == nil {
		t.Error("expected error for out of bounds read")
	}
}

func TestLowerList_WithRealMemory(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`
		(module
			(memory (export "memory") 1)
			(global $bump (mut i32) (i32.const 0))
			(func (export "realloc") (param i32 i32 i32 i32) (result i32)
				(local $ptr i32)
				global.get $bump
				local.set $ptr
				global.get $bump
				local.get 3
				i32.add
				global.set $bump
				local.get $ptr
			)
		)
	`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	lowerCtx := &LowerContext{
		Ctx: ctx,
		Options: CanonicalOptions{
			Memory:  mod.Memory(),
			Realloc: mod.ExportedFunction("realloc"),
		},
	}

	// Lower 2 elements, each 4 bytes
	elems := [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}}
	ptr, length, err := LowerList(lowerCtx, 4, elems)
	if err != nil {
		t.Fatalf("LowerList: %v", err)
	}
	if length != 2 {
		t.Errorf("length = %d, want 2", length)
	}

	// Read back
	data, ok := mod.Memory().Read(ptr, 8)
	if !ok {
		t.Fatal("memory read failed")
	}
	expected := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	for i, b := range data {
		if b != expected[i] {
			t.Errorf("byte %d: got %d, want %d", i, b, expected[i])
		}
	}
}

func TestLowerList_NilRealloc(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	lowerCtx := &LowerContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mod.Memory(), Realloc: nil},
	}

	_, _, err = LowerList(lowerCtx, 4, [][]byte{{1, 2, 3, 4}})
	if !errors.Is(err, ErrNilRealloc) {
		t.Errorf("expected ErrNilRealloc, got %v", err)
	}
}

func TestLiftBorrow_IncrementsLendCount(t *testing.T) {
	store := NewResourceStore()
	table := store.Table(1)
	h := table.New(42)

	ctx := &LiftContext{
		Ctx:   context.Background(),
		Store: store,
	}

	// LiftBorrow should increment lend count
	rep, err := LiftBorrow(ctx, 1, h)
	if err != nil {
		t.Fatalf("LiftBorrow: %v", err)
	}
	if rep != 42 {
		t.Errorf("rep = %d, want 42", rep)
	}

	// Should not be able to drop while borrowed
	_, _, err = table.Drop(h)
	if err == nil {
		t.Error("should not be able to drop while borrowed")
	}

	// EndLiftBorrow should allow drop
	err = EndLiftBorrow(ctx, 1, h)
	if err != nil {
		t.Fatalf("EndLiftBorrow: %v", err)
	}

	// Now drop should work
	_, needsDtor, err := table.Drop(h)
	if err != nil {
		t.Fatalf("Drop after EndLiftBorrow: %v", err)
	}
	if needsDtor {
		t.Error("needsDtor should be false (no dtor set)")
	}
}

func TestResourceNew(t *testing.T) {
	store := NewResourceStore()
	h := ResourceNew(store, 1, 42)

	// Should be able to get rep
	rep, err := ResourceRep(store, 1, h)
	if err != nil {
		t.Fatalf("ResourceRep: %v", err)
	}
	if rep != 42 {
		t.Errorf("rep = %d, want 42", rep)
	}
}

func TestResourceDrop(t *testing.T) {
	dtorCalled := false
	store := NewResourceStore()
	store.TableWithDtor(1, func(rep uint32) {
		dtorCalled = true
		if rep != 42 {
			t.Errorf("dtor got rep=%d, want 42", rep)
		}
	})

	h := ResourceNew(store, 1, 42)
	err := ResourceDrop(store, 1, h)
	if err != nil {
		t.Fatalf("ResourceDrop: %v", err)
	}

	if !dtorCalled {
		t.Error("destructor should have been called")
	}

	// Rep should fail after drop
	_, err = ResourceRep(store, 1, h)
	if err == nil {
		t.Error("ResourceRep should fail after drop")
	}
}

func TestResourceDrop_InvalidHandle(t *testing.T) {
	store := NewResourceStore()
	err := ResourceDrop(store, 1, Handle(999))
	if err == nil {
		t.Error("ResourceDrop should fail for invalid handle")
	}
}

func TestResourceRep_InvalidHandle(t *testing.T) {
	store := NewResourceStore()
	_, err := ResourceRep(store, 1, Handle(999))
	if err == nil {
		t.Error("ResourceRep should fail for invalid handle")
	}
}

func TestLiftString_UnsupportedEncoding(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	liftCtx := &LiftContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mod.Memory(), Encoding: StringEncodingUTF16},
	}

	_, err = LiftString(liftCtx, 0, 10)
	if !errors.Is(err, ErrUnsupportedEncoding) {
		t.Errorf("expected ErrUnsupportedEncoding, got %v", err)
	}
}

func TestLowerString_UnsupportedEncoding(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	lowerCtx := &LowerContext{
		Ctx:     ctx,
		Options: CanonicalOptions{Memory: mod.Memory(), Encoding: StringEncodingLatin1},
	}

	_, _, err = LowerString(lowerCtx, "test")
	if !errors.Is(err, ErrUnsupportedEncoding) {
		t.Errorf("expected ErrUnsupportedEncoding, got %v", err)
	}
}
