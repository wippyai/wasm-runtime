package asyncify

import (
	"bytes"

	"github.com/wippyai/wasm-runtime/asyncify/internal/engine"
)

// asyncifyExports are the functions added by asyncify transformation.
var asyncifyExports = [][]byte{
	[]byte("asyncify_start_unwind"),
	[]byte("asyncify_stop_unwind"),
	[]byte("asyncify_start_rewind"),
	[]byte("asyncify_stop_rewind"),
}

// IsAsyncified checks if a WASM module has been asyncified.
func IsAsyncified(wasmBytes []byte) bool {
	for _, exp := range asyncifyExports {
		if bytes.Contains(wasmBytes, exp) {
			return true
		}
	}
	return false
}

// ImportMatcher determines if an import should be treated as async/blocking.
//
// When an import matches, all functions that can transitively call it
// will be transformed to support stack switching.
type ImportMatcher = engine.ImportMatcher

// Config configures the asyncify transformation.
type Config struct {
	OnlyList             FunctionMatcher
	Matcher              ImportMatcher
	AddList              FunctionMatcher
	RemoveList           FunctionMatcher
	AsyncImports         []string
	MemoryIndex          uint32
	SecondaryMemoryPages uint32
	IgnoreImports        bool
	IgnoreIndirect       bool
	Asserts              bool
	PropagateAddList     bool
	UseSecondaryMemory   bool
	ImportGlobals        bool
	ExportGlobals        bool
	Wasm64               bool
}

// Transform applies the asyncify transformation to a WASM module.
//
// The transformation enables cooperative multitasking by instrumenting
// functions that can reach async imports. When an async import triggers
// a suspend, the call stack is serialized to linear memory, allowing
// the host to resume execution later.
//
// The transformation:
//   - Adds asyncify globals (state, data pointer)
//   - Identifies functions that need transformation via call graph analysis
//   - Transforms each function to support stack save/restore
//   - Adds asyncify export functions for host control
//
// Returns the transformed WASM binary or an error.
func Transform(wasmData []byte, cfg Config) ([]byte, error) {
	// Build matcher from AsyncImports if provided
	matcher := cfg.Matcher
	if len(cfg.AsyncImports) > 0 {
		matcher = &asyncImportMatcher{
			patterns: cfg.AsyncImports,
			fallback: cfg.Matcher,
		}
	}

	eng := engine.New(engine.Config{
		Matcher:              matcher,
		IgnoreImports:        cfg.IgnoreImports,
		AddList:              cfg.AddList,
		RemoveList:           cfg.RemoveList,
		OnlyList:             cfg.OnlyList,
		MemoryIndex:          cfg.MemoryIndex,
		IgnoreIndirect:       cfg.IgnoreIndirect,
		Asserts:              cfg.Asserts,
		PropagateAddList:     cfg.PropagateAddList,
		UseSecondaryMemory:   cfg.UseSecondaryMemory,
		SecondaryMemoryPages: cfg.SecondaryMemoryPages,
		ImportGlobals:        cfg.ImportGlobals,
		ExportGlobals:        cfg.ExportGlobals,
		Wasm64:               cfg.Wasm64,
	})
	return eng.Transform(wasmData)
}

// asyncImportMatcher matches imports from a list of patterns.
type asyncImportMatcher struct {
	fallback ImportMatcher
	patterns []string
}

func (m *asyncImportMatcher) Match(module, name string) bool {
	// Support both legacy "module.name" and canonical WIT "module#name".
	fullDot := module + "." + name
	fullHash := module + "#" + name
	for _, p := range m.patterns {
		if p == fullDot || p == fullHash || p == name {
			return true
		}
	}
	// Fall back to provided matcher
	if m.fallback != nil {
		return m.fallback.Match(module, name)
	}
	return false
}
