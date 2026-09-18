package asyncify

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// TransformCacheVersion tags the transform's output format and semantics.
// Maintainers bump it whenever the transform's output changes, so stale
// entries from an older transform are not reused.
const TransformCacheVersion = "asyncify-transform-v1"

// TransformCache stores transformed module bytes keyed by their content
// address. Implementations must be safe for concurrent use: the same cache is
// shared by every runtime and module that loads a given input.
//
// Get reports a hit only when the stored bytes were read in full. Put publishes
// bytes that Get may return; arrays are copied on read so callers can mutate
// the result.
type TransformCache interface {
	Get(key string) ([]byte, bool)
	Put(key string, transformed []byte) error
}

// TransformCacheKey computes the content address of a transform request.
//
// It hashes the transform version tag, the input module bytes, and a canonical
// encoding of every Config field that affects output. AsyncImports are sorted,
// so their order never changes the key.
//
// A Config carrying any non-nil matcher (OnlyList, Matcher, AddList or
// RemoveList) is not cacheable: the matcher may close over state the cache
// cannot observe. The second result reports cacheability.
func TransformCacheKey(wasmData []byte, cfg Config) (string, bool) {
	return transformCacheKey(TransformCacheVersion, wasmData, cfg)
}

func transformCacheKey(version string, wasmData []byte, cfg Config) (string, bool) {
	if cfg.OnlyList != nil || cfg.Matcher != nil || cfg.AddList != nil || cfg.RemoveList != nil {
		return "", false
	}

	digest := sha256.New()
	hashString(digest, version)
	hashBytes(digest, wasmData)

	imports := append([]string(nil), cfg.AsyncImports...)
	sort.Strings(imports)
	hashUint32(digest, uint32(len(imports)))
	for _, pattern := range imports {
		hashString(digest, pattern)
	}

	hashUint32(digest, cfg.MemoryIndex)
	hashUint32(digest, cfg.SecondaryMemoryPages)
	hashBool(digest, cfg.IgnoreImports)
	hashBool(digest, cfg.IgnoreIndirect)
	hashBool(digest, cfg.Asserts)
	hashBool(digest, cfg.PropagateAddList)
	hashBool(digest, cfg.UseSecondaryMemory)
	hashBool(digest, cfg.ImportGlobals)
	hashBool(digest, cfg.ExportGlobals)
	hashBool(digest, cfg.Wasm64)

	return hex.EncodeToString(digest.Sum(nil)), true
}

func hashString(digest hash.Hash, value string) {
	hashUint32(digest, uint32(len(value)))
	digest.Write([]byte(value))
}

func hashBytes(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	digest.Write(length[:])
	digest.Write(value)
}

func hashUint32(digest hash.Hash, value uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	digest.Write(buf[:])
}

func hashBool(digest hash.Hash, value bool) {
	if value {
		digest.Write([]byte{1})
		return
	}
	digest.Write([]byte{0})
}

// TransformCached applies Transform and caches the result by content address.
//
// A nil cache or an uncacheable Config falls back to Transform unchanged. A
// cache hit returns the stored bytes without running the transform. A cache
// write failure is non-fatal: the transformed bytes are still returned, and
// the failure is reported through the package logger.
func TransformCached(cache TransformCache, wasmData []byte, cfg Config) ([]byte, error) {
	if cache == nil {
		return Transform(wasmData, cfg)
	}
	key, cacheable := TransformCacheKey(wasmData, cfg)
	if !cacheable {
		return Transform(wasmData, cfg)
	}
	if cached, hit := cache.Get(key); hit {
		return cached, nil
	}
	transformed, err := Transform(wasmData, cfg)
	if err != nil {
		return nil, err
	}
	if err := cache.Put(key, transformed); err != nil {
		Logger().Warn("asyncify transform cache put failed", zap.String("key", key), zap.Error(err))
	}
	return transformed, nil
}

// MemoryTransformCache is an in-memory TransformCache safe for concurrent use.
// The zero value is ready to use.
type MemoryTransformCache struct {
	entries map[string][]byte
	mu      sync.RWMutex
}

// NewMemoryTransformCache creates an empty in-memory transform cache.
func NewMemoryTransformCache() *MemoryTransformCache {
	return &MemoryTransformCache{}
}

// Get returns a copy of the stored bytes.
func (c *MemoryTransformCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	stored, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return append([]byte(nil), stored...), true
}

// Put stores a copy of transformed.
func (c *MemoryTransformCache) Put(key string, transformed []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string][]byte)
	}
	if _, ok := c.entries[key]; ok {
		return nil
	}
	c.entries[key] = append([]byte(nil), transformed...)
	return nil
}

// DirTransformCache is a directory-backed TransformCache safe for concurrent
// use by multiple processes sharing the directory.
//
// Entries are published atomically: bytes go to a temporary file in the same
// directory, are synced, then renamed onto the key's path. Readers therefore
// observe either a complete entry or nothing.
type DirTransformCache struct {
	dir string
}

// NewDirTransformCache creates a directory-backed cache, creating dir when
// missing.
func NewDirTransformCache(dir string) (*DirTransformCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DirTransformCache{dir: dir}, nil
}

// Get reads a stored entry. Read errors, corrupted entries, and keys outside
// the cache directory are a miss.
func (c *DirTransformCache) Get(key string) ([]byte, bool) {
	if !validTransformCacheKey(key) {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, key))
	if err != nil {
		return nil, false
	}
	if len(data) < sha256.Size {
		return nil, false
	}
	expectedHash := data[:sha256.Size]
	transformed := data[sha256.Size:]
	actualHash := sha256.Sum256(transformed)
	if !bytes.Equal(actualHash[:], expectedHash) {
		return nil, false
	}
	return transformed, true
}

// Put stores transformed, publishing it atomically with an integrity checksum.
// It returns the first IO error encountered. An existing valid entry that
// verifies is not rewritten.
func (c *DirTransformCache) Put(key string, transformed []byte) error {
	if !validTransformCacheKey(key) {
		return os.ErrInvalid
	}
	if existing, ok := c.Get(key); ok && bytes.Equal(existing, transformed) {
		return nil
	}
	tmp, err := os.CreateTemp(c.dir, key+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	header := sha256.Sum256(transformed)
	if _, err := tmp.Write(header[:]); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(transformed); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(c.dir, key)); err != nil {
		_ = os.Remove(tmpName)
		if existing, ok := c.Get(key); ok && bytes.Equal(existing, transformed) {
			return nil
		}
		return err
	}
	return nil
}

// validTransformCacheKey rejects keys that would escape the cache directory.
// Keys produced by TransformCacheKey are lowercase hex.
func validTransformCacheKey(key string) bool {
	if key == "" || key == "." || key == ".." {
		return false
	}
	return !strings.ContainsAny(key, `/\`)
}
