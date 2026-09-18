package engine

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
)

// countingTransformCache wraps a transform cache and records how often the
// transform ran (one Put per miss) and how often a stored result was reused.
type countingTransformCache struct {
	inner *asyncify.MemoryTransformCache
	bytes map[string][]byte
	mu    sync.Mutex
	gets  int
	hits  int
	puts  int
}

func newCountingTransformCache() *countingTransformCache {
	return &countingTransformCache{inner: asyncify.NewMemoryTransformCache(), bytes: make(map[string][]byte)}
}

func (c *countingTransformCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	data, ok := c.inner.Get(key)
	if ok {
		c.hits++
	}
	return data, ok
}

func (c *countingTransformCache) Put(key string, transformed []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.puts++
	c.bytes[key] = append([]byte(nil), transformed...)
	return c.inner.Put(key, transformed)
}

func (c *countingTransformCache) counts() (gets, hits, puts int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets, c.hits, c.puts
}

func (c *countingTransformCache) stored() map[string][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string][]byte, len(c.bytes))
	for key, data := range c.bytes {
		out[key] = append([]byte(nil), data...)
	}
	return out
}

const transformCacheCoreWAT = `(module
	(import "env" "call" (func $call))
	(func (export "run") (call $call))
	(memory (export "memory") 1)
` + ownedAsyncifyTestAllocator + `
)`

func loadAsyncifyCore(t *testing.T, eng *WazeroEngine, raw []byte) *WazeroModule {
	t.Helper()
	ctx := t.Context()
	mod, err := eng.LoadModule(ctx, raw)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}
	err = mod.RegisterHostFuncRaw("env", "call", nil, nil, MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp {
		return &traceOp{name: "call", id: 1}
	}), true)
	if err != nil {
		mod.Close(ctx)
		t.Fatalf("register host func: %v", err)
	}
	if err := mod.Compile(ctx, &CompileConfig{EnableAsyncify: true}); err != nil {
		mod.Close(ctx)
		t.Fatalf("compile: %v", err)
	}
	return mod
}

func loadAsyncifyComponent(t *testing.T, eng *WazeroEngine, raw []byte) *WazeroModule {
	t.Helper()
	ctx := t.Context()
	mod, err := eng.LoadModule(ctx, raw)
	if err != nil {
		t.Fatalf("load module: %v", err)
	}
	err = mod.RegisterHostFuncRaw("test:async/host@0.1.0", "yield",
		[]api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32},
		MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp {
			return &traceOp{name: "yield", id: 1}
		}), true)
	if err != nil {
		mod.Close(ctx)
		t.Fatalf("register host func: %v", err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err != nil {
		mod.Close(ctx)
		t.Fatalf("instantiate: %v", err)
	}
	inst.Close(ctx)
	return mod
}

// TestTransformCacheSingleModuleTransformOnce loads and compiles the same core
// module through two separate WazeroModule objects sharing one cache. The
// transform runs once; the second module reuses the stored output.
func TestTransformCacheSingleModuleTransformOnce(t *testing.T) {
	ctx := context.Background()
	raw, err := wat.Compile(transformCacheCoreWAT)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	uncached, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer uncached.Close(ctx)
	uncachedMod := loadAsyncifyCore(t, uncached, raw)
	defer uncachedMod.Close(ctx)
	want := append([]byte(nil), uncachedMod.rawBytes...)
	if !uncachedMod.IsTransformed() {
		t.Fatal("uncached module was not transformed")
	}

	cache := newCountingTransformCache()
	eng, err := NewWazeroEngineWithConfig(ctx, &Config{TransformCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)

	first := loadAsyncifyCore(t, eng, raw)
	defer first.Close(ctx)
	second := loadAsyncifyCore(t, eng, raw)
	defer second.Close(ctx)

	gets, hits, puts := cache.counts()
	if puts != 1 {
		t.Fatalf("transform ran %d times, want 1", puts)
	}
	if gets != 2 || hits != 1 {
		t.Fatalf("cache gets/hits = %d/%d, want 2/1", gets, hits)
	}
	if !bytes.Equal(first.rawBytes, want) {
		t.Fatal("cached transform output differs from uncached output")
	}
	if !bytes.Equal(second.rawBytes, want) {
		t.Fatal("cache-hit transform output differs from uncached output")
	}
	if !bytes.Equal(first.rawBytes, second.rawBytes) {
		t.Fatal("modules sharing a cache produced different output")
	}
}

// TestTransformCacheMultiModuleTransformOnce exercises the linker path: a
// multi-module component is instantiated twice through two WazeroModule
// objects sharing one cache. The fixture has two distinct core modules, so the
// first module performs two transforms, and the second module reuses both
// stored outputs without transforming again.
func TestTransformCacheMultiModuleTransformOnce(t *testing.T) {
	ctx := context.Background()
	raw := componentFixture(t, multiCoreWrapperWAT)

	cache := newCountingTransformCache()
	eng, err := NewWazeroEngineWithConfig(ctx, &Config{TransformCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)

	first := loadAsyncifyComponent(t, eng, raw)
	defer first.Close(ctx)
	derivedImports, err := first.deriveAsyncifyImports()
	if err != nil {
		t.Fatalf("derive asyncify imports: %v", err)
	}

	comp, err := component.DecodeAndValidate(raw)
	if err != nil {
		t.Fatalf("decode and validate component fixture: %v", err)
	}
	want := make(map[string][]byte)
	for _, modBytes := range comp.Raw.CoreModules {
		if asyncify.IsAsyncified(modBytes) {
			continue
		}
		cfg := asyncify.Config{
			AsyncImports:  derivedImports,
			ExportGlobals: true,
		}
		transformed, err := asyncify.Transform(modBytes, cfg)
		if err != nil {
			t.Fatalf("plain asyncify.Transform: %v", err)
		}
		key, ok := asyncify.TransformCacheKey(modBytes, cfg)
		if !ok {
			t.Fatal("expected cacheable key")
		}
		want[key] = transformed
	}
	if len(want) == 0 {
		t.Fatal("component transform never ran")
	}
	_, _, putsAfterFirst := cache.counts()
	if putsAfterFirst != len(want) {
		t.Fatalf("first module transformed %d core modules, want %d", putsAfterFirst, len(want))
	}

	second := loadAsyncifyComponent(t, eng, raw)
	defer second.Close(ctx)

	gets, hits, puts := cache.counts()
	if puts != putsAfterFirst {
		t.Fatalf("second module re-ran the transform %d times", puts-putsAfterFirst)
	}
	if gets != 2*len(want) || hits != len(want) {
		t.Fatalf("cache gets/hits = %d/%d, want %d/%d", gets, hits, 2*len(want), len(want))
	}

	stored := cache.stored()
	if len(stored) != len(want) {
		t.Fatalf("cached run stored %d entries, want %d", len(stored), len(want))
	}
	for key, wantBytes := range want {
		if gotBytes, ok := stored[key]; !ok {
			t.Fatalf("cached run missing entry %s", key)
		} else if !bytes.Equal(gotBytes, wantBytes) {
			t.Fatalf("cached entry %s differs from plain asyncify.Transform output", key)
		}
	}
}

// TestTransformCacheNilUnchanged confirms a nil cache preserves the plain
// transform path and its output.
func TestTransformCacheNilUnchanged(t *testing.T) {
	ctx := context.Background()
	raw, err := wat.Compile(transformCacheCoreWAT)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	plain, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close(ctx)
	plainMod := loadAsyncifyCore(t, plain, raw)
	defer plainMod.Close(ctx)

	nilCache, err := NewWazeroEngineWithConfig(ctx, &Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer nilCache.Close(ctx)
	nilMod := loadAsyncifyCore(t, nilCache, raw)
	defer nilMod.Close(ctx)

	if !bytes.Equal(plainMod.rawBytes, nilMod.rawBytes) {
		t.Fatal("nil cache changed the transformed output")
	}
}
