package linker

import (
	"context"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

// nonComparableModule is legal at the api.Module boundary because it embeds a
// real wazero module, while its slice deliberately makes the concrete value
// non-comparable. The cache must decline this representation without panicking.
type nonComparableModule struct {
	api.Module
	values []string
}

func TestCanonicalBoundModuleCache_IdentityAndDelegation(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	wasmBytes, err := wat.Compile(`(module
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) i32.const 4 i32.load8_u)
		(func (export "alternate_allocator") (result i32) i32.const 6 i32.load8_u)
		(func (export "delegated") (result i32) i32.const 5 i32.load8_u)
	)`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	callerA, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("caller-a"))
	if err != nil {
		t.Fatal(err)
	}
	defer callerA.Close(ctx)
	callerB, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("caller-b"))
	if err != nil {
		t.Fatal(err)
	}
	defer callerB.Close(ctx)
	if !callerB.Memory().Write(4, []byte{0xB2}) {
		t.Fatal("write caller-b marker")
	}
	if !callerA.Memory().Write(5, []byte{0xA1}) {
		t.Fatal("write caller-a delegated marker")
	}
	if !callerB.Memory().Write(5, []byte{0xB1}) {
		t.Fatal("write caller-b delegated marker")
	}
	if !callerA.Memory().Write(6, []byte{0xA6}) {
		t.Fatal("write caller-a alternate allocator marker")
	}

	cache := newCanonicalBoundModuleCache(callerB.Memory(), callerB.ExportedFunction("cabi_realloc"))
	first := cache.wrap(callerA)
	again := cache.wrap(callerA)
	other := cache.wrap(callerB)
	if first != again {
		t.Fatal("same pointer caller did not reuse the immutable wrapper")
	}
	if first == other {
		t.Fatal("different pointer caller reused the cached wrapper")
	}
	if first.Memory() != callerB.Memory() {
		t.Fatal("cached wrapper lost its bound memory")
	}
	got, err := first.ExportedFunction("cabi_realloc").Call(ctx, 0, 0, 0, 0)
	if err != nil || len(got) != 1 || got[0] != 0xB2 {
		t.Fatalf("cached wrapper lost its bound allocator: result=%v err=%v", got, err)
	}
	if first.Name() != callerA.Name() {
		t.Fatalf("Name() = %q, want delegated caller name %q", first.Name(), callerA.Name())
	}
	delegated, err := first.ExportedFunction("delegated").Call(ctx)
	if err != nil || len(delegated) != 1 || delegated[0] != 0xA1 {
		t.Fatalf("non-allocator export did not delegate to caller-a: result=%v err=%v", delegated, err)
	}
	if first.Memory() != callerB.Memory() {
		t.Fatal("later different caller changed retained wrapper memory")
	}

	// A nested module wrapper is itself the caller identity. The canonical
	// wrapper must retain it verbatim: callers can delegate exports through a
	// nested wrapper with its own binding rules.
	nested := &boundModuleWrapper{
		Module:     callerA,
		boundMem:   callerA.Memory(),
		boundAlloc: callerA.ExportedFunction("alternate_allocator"),
		allocName:  "alternate_allocator",
	}
	nestedCache := newCanonicalBoundModuleCache(callerB.Memory(), callerB.ExportedFunction("cabi_realloc"))
	nestedResult := nestedCache.wrap(nested)
	if nestedResult.Module != nested {
		t.Fatal("canonical cache unwrapped the caller module")
	}
	if nestedCache.wrap(nested) != nestedResult {
		t.Fatal("nested pointer caller did not reuse its retained wrapper")
	}
	delegatedAlloc, err := nestedResult.ExportedFunction("alternate_allocator").Call(ctx)
	if err != nil || len(delegatedAlloc) != 1 || delegatedAlloc[0] != 0xA6 {
		t.Fatalf("nested allocator delegation result=%v err=%v", delegatedAlloc, err)
	}

	nonComparable := nonComparableModule{Module: callerA, values: []string{"not", "comparable"}}
	fallbackOne := cache.wrap(nonComparable)
	fallbackTwo := cache.wrap(nonComparable)
	if fallbackOne == fallbackTwo {
		t.Fatal("non-pointer caller must not enter the cache")
	}
}

func TestCanonicalBoundModuleCache_ConcurrentFirstCaller(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	wasmBytes, err := wat.Compile(`(module (memory (export "memory") 1) (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) i32.const 0))`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	caller, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("concurrent-caller"))
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close(ctx)

	cache := newCanonicalBoundModuleCache(caller.Memory(), caller.ExportedFunction("cabi_realloc"))
	const goroutines = 32
	wrappers := make(chan *boundModuleWrapper, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			wrappers <- cache.wrap(caller)
		}()
	}
	wg.Wait()
	close(wrappers)
	var first *boundModuleWrapper
	for wrapper := range wrappers {
		if first == nil {
			first = wrapper
			continue
		}
		if wrapper != first {
			t.Fatal("concurrent first calls published more than one cached wrapper")
		}
	}
}
