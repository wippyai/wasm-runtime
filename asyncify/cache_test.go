package asyncify

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/wippyai/wasm-runtime/wat"
)

func cacheTestModule(t *testing.T) []byte {
	t.Helper()
	raw, err := wat.Compile(`(module
		(import "env" "yield" (func $yield (param i32) (result i32)))
		(memory 1)
		(func (export "run") (param i32) (result i32) (call $yield (local.get 0))))`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	return raw
}

func TestTransformCacheKeyStable(t *testing.T) {
	raw := cacheTestModule(t)
	base := Config{AsyncImports: []string{"env.b", "env.a"}, ExportGlobals: true}
	reordered := Config{AsyncImports: []string{"env.a", "env.b"}, ExportGlobals: true}

	first, ok := TransformCacheKey(raw, base)
	if !ok {
		t.Fatal("plain config must be cacheable")
	}
	second, ok := TransformCacheKey(raw, reordered)
	if !ok {
		t.Fatal("reordered config must be cacheable")
	}
	if first != second {
		t.Fatalf("AsyncImports order changed key: %s != %s", first, second)
	}
	if again, _ := TransformCacheKey(raw, base); again != first {
		t.Fatalf("key not stable: %s != %s", again, first)
	}
}

func TestTransformCacheKeyDistinguishesInputs(t *testing.T) {
	raw := cacheTestModule(t)
	cfg := Config{AsyncImports: []string{"env.a"}}
	base, _ := TransformCacheKey(raw, cfg)

	other := append([]byte(nil), raw...)
	other[8] ^= 0xff
	if key, _ := TransformCacheKey(other, cfg); key == base {
		t.Fatal("different input bytes produced the same key")
	}

	variants := map[string]Config{
		"memory index":       {AsyncImports: cfg.AsyncImports, MemoryIndex: 1},
		"secondary pages":    {AsyncImports: cfg.AsyncImports, SecondaryMemoryPages: 1},
		"ignore imports":     {AsyncImports: cfg.AsyncImports, IgnoreImports: true},
		"ignore indirect":    {AsyncImports: cfg.AsyncImports, IgnoreIndirect: true},
		"asserts":            {AsyncImports: cfg.AsyncImports, Asserts: true},
		"propagate add list": {AsyncImports: cfg.AsyncImports, PropagateAddList: true},
		"secondary memory":   {AsyncImports: cfg.AsyncImports, UseSecondaryMemory: true},
		"import globals":     {AsyncImports: cfg.AsyncImports, ImportGlobals: true},
		"export globals":     {AsyncImports: cfg.AsyncImports, ExportGlobals: true},
		"wasm64":             {AsyncImports: cfg.AsyncImports, Wasm64: true},
		"async imports":      {AsyncImports: []string{"env.z"}},
	}
	for name, variant := range variants {
		key, ok := TransformCacheKey(raw, variant)
		if !ok {
			t.Fatalf("%s: config must be cacheable", name)
		}
		if key == base {
			t.Fatalf("%s: key did not change", name)
		}
	}
}

func TestTransformCacheKeyVersionTag(t *testing.T) {
	raw := cacheTestModule(t)
	cfg := Config{}
	first, _ := transformCacheKey("asyncify-transform-v1", raw, cfg)
	second, _ := transformCacheKey("asyncify-transform-v2", raw, cfg)
	if first == second {
		t.Fatal("version tag did not change the key")
	}
}

func TestTransformCacheKeyMatchersUncacheable(t *testing.T) {
	raw := cacheTestModule(t)
	configs := map[string]Config{
		"matcher":     {Matcher: NewExactMatcher([]string{"env.yield"})},
		"only list":   {OnlyList: NewFunctionNameMatcher([]string{"run"})},
		"add list":    {AddList: NewFunctionNameMatcher([]string{"run"})},
		"remove list": {RemoveList: NewFunctionNameMatcher([]string{"run"})},
	}
	for name, cfg := range configs {
		if _, ok := TransformCacheKey(raw, cfg); ok {
			t.Fatalf("%s: matcher config must not be cacheable", name)
		}
	}
}

func TestTransformCachedNilCache(t *testing.T) {
	raw := cacheTestModule(t)
	cfg := Config{AsyncImports: []string{"env.yield"}, ExportGlobals: true}
	want, err := Transform(raw, cfg)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got, err := TransformCached(nil, raw, cfg)
	if err != nil {
		t.Fatalf("TransformCached(nil): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("nil cache changed the transform output")
	}
}

func TestTransformCachedReusesMemoryCache(t *testing.T) {
	raw := cacheTestModule(t)
	cfg := Config{AsyncImports: []string{"env.yield"}, ExportGlobals: true}
	cache := NewMemoryTransformCache()

	want, err := Transform(raw, cfg)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	first, err := TransformCached(cache, raw, cfg)
	if err != nil {
		t.Fatalf("TransformCached first: %v", err)
	}
	second, err := TransformCached(cache, raw, cfg)
	if err != nil {
		t.Fatalf("TransformCached second: %v", err)
	}
	if !bytes.Equal(first, want) || !bytes.Equal(second, want) {
		t.Fatal("cached output differs from uncached transform")
	}
}

func TestMemoryTransformCacheConcurrent(t *testing.T) {
	cache := NewMemoryTransformCache()
	keys := []string{"a", "b"}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := keys[i%len(keys)]
			data := []byte(key)
			cache.Put(key, data)
			if got, ok := cache.Get(key); ok && !bytes.Equal(got, data) {
				t.Errorf("key %q returned %q, want %q", key, got, data)
			}
		}(i)
	}
	wg.Wait()
}

func TestDirTransformCacheRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")
	cache, err := NewDirTransformCache(dir)
	if err != nil {
		t.Fatalf("NewDirTransformCache: %v", err)
	}
	key := "0123456789abcdef"
	if _, ok := cache.Get(key); ok {
		t.Fatal("empty cache reported a hit")
	}
	transformed := []byte("transformed bytes")
	cache.Put(key, transformed)
	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("Put then Get reported a miss")
	}
	if !bytes.Equal(got, transformed) {
		t.Fatalf("round trip = %q, want %q", got, transformed)
	}
}

func TestDirTransformCacheConcurrent(t *testing.T) {
	dir := t.TempDir()
	first, err := NewDirTransformCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewDirTransformCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	caches := []*DirTransformCache{first, second}
	shared := bytes.Repeat([]byte("shared"), 4096)
	distinct := map[string][]byte{
		"key-a": bytes.Repeat([]byte("a"), 4096),
		"key-b": bytes.Repeat([]byte("b"), 4096),
	}

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache := caches[i%len(caches)]
			if i%2 == 0 {
				cache.Put("shared", shared)
				return
			}
			for key, want := range distinct {
				cache.Put(key, want)
			}
		}(i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache := caches[i%len(caches)]
			if got, ok := cache.Get("shared"); ok && !bytes.Equal(got, shared) {
				t.Errorf("shared key returned %d bytes, want %d", len(got), len(shared))
			}
			for key, want := range distinct {
				if got, ok := cache.Get(key); ok && !bytes.Equal(got, want) {
					t.Errorf("key %q returned %d bytes, want %d", key, len(got), len(want))
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestDirTransformCacheIgnoresOrphanTemp(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewDirTransformCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := TransformCacheKey(cacheTestModule(t), Config{})
	if !ok {
		t.Fatal("config must be cacheable")
	}
	orphan := filepath.Join(dir, key+".tmp-orphan")
	if err := os.WriteFile(orphan, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit := cache.Get(key); hit {
		t.Fatal("orphan temp file produced a cache hit")
	}
	cache.Put(key, []byte("complete"))
	got, hit := cache.Get(key)
	if !hit || !bytes.Equal(got, []byte("complete")) {
		t.Fatalf("Get after orphan = %q, %v; want complete", got, hit)
	}
}

type failingPutCache struct {
	err error
}

func (f *failingPutCache) Get(key string) ([]byte, bool) {
	return nil, false
}

func (f *failingPutCache) Put(key string, transformed []byte) error {
	return f.err
}

func TestTransformCachedPutFailureReportsError(t *testing.T) {
	raw := cacheTestModule(t)
	cfg := Config{AsyncImports: []string{"env.yield"}, ExportGlobals: true}
	want, err := Transform(raw, cfg)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	core, recorded := observer.New(zapcore.WarnLevel)
	origLogger := Logger()
	SetLogger(zap.New(core))
	defer SetLogger(origLogger)

	putErr := errors.New("write failure")
	cache := &failingPutCache{err: putErr}

	got, err := TransformCached(cache, raw, cfg)
	if err != nil {
		t.Fatalf("TransformCached with failing Put returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("TransformCached did not return transformed bytes on Put failure")
	}

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if !strings.Contains(entries[0].Message, "cache put failed") {
		t.Fatalf("unexpected log message: %s", entries[0].Message)
	}
}

func TestTransformCacheSingleInterface(t *testing.T) {
	var cache TransformCache = NewMemoryTransformCache()
	if err := cache.Put("test-key", []byte("val")); err != nil {
		t.Fatalf("MemoryTransformCache.Put returned error: %v", err)
	}

	dirCache, err := NewDirTransformCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var dirTransCache TransformCache = dirCache
	if err := dirTransCache.Put("test-key", []byte("val")); err != nil {
		t.Fatalf("DirTransformCache.Put returned error: %v", err)
	}
}

func TestDirTransformCacheIntegrityCorruptedEntries(t *testing.T) {
	raw := cacheTestModule(t)
	cfg := Config{AsyncImports: []string{"env.yield"}, ExportGlobals: true}
	want, err := Transform(raw, cfg)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	key, ok := TransformCacheKey(raw, cfg)
	if !ok {
		t.Fatal("config must be cacheable")
	}

	tests := []struct {
		corruptFn func(dir, key string)
		name      string
	}{
		{
			name: "empty file",
			corruptFn: func(dir, key string) {
				entryPath := filepath.Join(dir, key)
				if err := os.WriteFile(entryPath, []byte{}, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "truncated entry",
			corruptFn: func(dir, key string) {
				entryPath := filepath.Join(dir, key)
				// Write fewer bytes than a header, or a partial payload.
				if err := os.WriteFile(entryPath, []byte("short-truncated"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "bit-flipped entry",
			corruptFn: func(dir, key string) {
				cache, err := NewDirTransformCache(dir)
				if err != nil {
					t.Fatal(err)
				}
				if err := cache.Put(key, want); err != nil {
					t.Fatal(err)
				}
				entryPath := filepath.Join(dir, key)
				data, err := os.ReadFile(entryPath)
				if err != nil {
					t.Fatal(err)
				}
				if len(data) == 0 {
					t.Fatal("empty data after put")
				}
				data[len(data)/2] ^= 0xff
				if err := os.WriteFile(entryPath, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cache, err := NewDirTransformCache(dir)
			if err != nil {
				t.Fatal(err)
			}

			tc.corruptFn(dir, key)

			// 1. Missing, short or mismatching entry must be a miss.
			if got, hit := cache.Get(key); hit {
				t.Fatalf("Get on %s reported a hit (got %d bytes), want miss", tc.name, len(got))
			}

			// 2. TransformCached replaces corrupt entry with verified entry.
			got, err := TransformCached(cache, raw, cfg)
			if err != nil {
				t.Fatalf("TransformCached after %s: %v", tc.name, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("TransformCached returned unexpected bytes for %s", tc.name)
			}

			// 3. Next Get must report a hit with verified entry.
			verified, hit := cache.Get(key)
			if !hit {
				t.Fatalf("Get after replacement reported a miss for %s", tc.name)
			}
			if !bytes.Equal(verified, want) {
				t.Fatalf("verified entry differs from want for %s", tc.name)
			}
		})
	}
}

func TestDirTransformCacheDoNotRewriteValidEntry(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewDirTransformCache(dir)
	if err != nil {
		t.Fatal(err)
	}

	key := "0123456789abcdef"
	transformed := []byte("valid transformed bytes")

	if err := cache.Put(key, transformed); err != nil {
		t.Fatalf("initial Put: %v", err)
	}

	entryPath := filepath.Join(dir, key)
	info1, err := os.Stat(entryPath)
	if err != nil {
		t.Fatalf("stat after initial Put: %v", err)
	}

	// Calling Put again with the same valid content must be a no-op:
	// it must not recreate/rename the file and must create no temp files.
	if err := cache.Put(key, transformed); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	info2, err := os.Stat(entryPath)
	if err != nil {
		t.Fatalf("stat after second Put: %v", err)
	}

	if !os.SameFile(info1, info2) {
		t.Fatal("Put rewrote existing valid entry; file identity changed")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in cache dir, got %d", len(entries))
	}
	if entries[0].Name() != key {
		t.Fatalf("unexpected file in cache dir: %s", entries[0].Name())
	}
}

func TestTransformCacheKeyAccountsForAllConfigFields(t *testing.T) {
	raw := cacheTestModule(t)
	base := Config{}
	baseKey, baseOk := TransformCacheKey(raw, base)
	if !baseOk {
		t.Fatal("base zero-config must be cacheable")
	}

	matcherFields := map[string]func() any{
		"OnlyList":   func() any { return NewFunctionNameMatcher([]string{"fn"}) },
		"Matcher":    func() any { return NewExactMatcher([]string{"env.mod"}) },
		"AddList":    func() any { return NewFunctionNameMatcher([]string{"fn"}) },
		"RemoveList": func() any { return NewFunctionNameMatcher([]string{"fn"}) },
	}

	configType := reflect.TypeOf(Config{})
	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		t.Run(field.Name, func(t *testing.T) {
			if makeMatcher, isMatcher := matcherFields[field.Name]; isMatcher {
				var cfg Config
				val := reflect.ValueOf(&cfg).Elem()
				val.FieldByName(field.Name).Set(reflect.ValueOf(makeMatcher()))
				if _, ok := TransformCacheKey(raw, cfg); ok {
					t.Fatalf("declared matcher field %s must be uncacheable when non-nil", field.Name)
				}
				return
			}

			var modified Config
			val := reflect.ValueOf(&modified).Elem()
			fieldVal := val.FieldByName(field.Name)

			switch fieldVal.Kind() {
			case reflect.Bool:
				fieldVal.SetBool(true)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				fieldVal.SetUint(1)
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				fieldVal.SetInt(1)
			case reflect.String:
				fieldVal.SetString("modified-field-value")
			case reflect.Slice:
				if fieldVal.Type().Elem().Kind() == reflect.String {
					fieldVal.Set(reflect.ValueOf([]string{"modified-slice-value"}))
				} else {
					t.Fatalf("unhandled slice element type for %s: %v", field.Name, fieldVal.Type().Elem())
				}
			default:
				t.Fatalf("unhandled field type for %s: %v", field.Name, field.Type)
			}

			key, ok := TransformCacheKey(raw, modified)
			if !ok {
				t.Fatalf("field %s: modified config should be cacheable", field.Name)
			}
			if key == baseKey {
				t.Fatalf("field %s did not change the cache key", field.Name)
			}
		})
	}
}
