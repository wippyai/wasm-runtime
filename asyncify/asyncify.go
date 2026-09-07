package asyncify

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/engine"
	"github.com/wippyai/wasm-runtime/wasm"
)

// Required Asyncify helper function export names.
const (
	ExportStartUnwind = "asyncify_start_unwind"
	ExportStopUnwind  = "asyncify_stop_unwind"
	ExportStartRewind = "asyncify_start_rewind"
	ExportStopRewind  = "asyncify_stop_rewind"
	ExportGetState    = "asyncify_get_state"
)

// IsAsyncified checks if a WASM module conforms to the Asyncify ABI protocol shape.
//
// Structural Export Validation:
// It performs structural inspection of the module's export, type, function, and import
// metadata using sound WebAssembly binary parsing (without decoding or copying code
// and data section payloads). A module is identified as asyncified if and only if it
// defines and exports the complete suite of five standard Asyncify runtime helpers:
//
//   - asyncify_start_unwind: (i32) -> () [or (i64) -> () for wasm64]
//   - asyncify_stop_unwind:  () -> ()
//   - asyncify_start_rewind: (i32) -> () [or (i64) -> () for wasm64, matching start_unwind]
//   - asyncify_stop_rewind:  () -> ()
//   - asyncify_get_state:    () -> (i32) [or (i64)]
//
// All five helpers must be exported as functions (wasm.KindFunc), must be locally defined
// within the module (not imported functions or re-exports), and must match expected type
// signatures.
//
// Limitation & Protocol Shape Notice:
// Validating export names, kinds, and type signatures verifies compliance with the
// external Asyncify protocol shape expected by the host runtime. However, static export
// and signature inspection cannot prove behavioral correctness of the guest code — it
// cannot verify whether the guest function bodies correctly manipulate the asyncify stack,
// manage unwind/rewind state transitions, or preserve locals. Behavioral correctness
// requires runtime execution testing.
//
// This structural check ensures that modules with passive data, custom sections,
// import names, or function names containing helper strings are not mistakenly skipped
// during transformation, while preserving accurate detection for valid pretransformed
// Binaryen and Wippy modules.
func IsAsyncified(wasmBytes []byte) bool {
	if len(wasmBytes) < 8 {
		return false
	}
	m, err := wasm.ParseModuleMetadata(wasmBytes)
	if err != nil {
		return false
	}
	return HasAsyncifyProtocolExports(m)
}

// HasAsyncifyProtocolExports checks whether a parsed WASM module structurally exports
// the complete set of required Asyncify helper functions with valid signatures.
func HasAsyncifyProtocolExports(m *wasm.Module) bool {
	if m == nil {
		return false
	}

	numImported := uint32(m.NumImportedFuncs())
	numDefined := uint32(len(m.Funcs))

	required := map[string]struct{}{
		ExportStartUnwind: {},
		ExportStopUnwind:  {},
		ExportStartRewind: {},
		ExportStopRewind:  {},
		ExportGetState:    {},
	}

	helperExports := make(map[string]*wasm.Export, len(required))
	seenNames := make(map[string]struct{}, len(m.Exports))

	for i := range m.Exports {
		exp := &m.Exports[i]
		if _, exists := seenNames[exp.Name]; exists {
			// Spec violation: duplicate export name
			return false
		}
		seenNames[exp.Name] = struct{}{}

		if _, isHelper := required[exp.Name]; isHelper {
			helperExports[exp.Name] = exp
		}
	}

	// All 5 helpers must be present (partial exports are rejected)
	if len(helperExports) != len(required) {
		return false
	}

	// All helpers must be exported functions locally defined in the module
	for _, exp := range helperExports {
		if exp.Kind != wasm.KindFunc {
			return false
		}
		if exp.Idx < numImported || exp.Idx >= numImported+numDefined {
			return false
		}
	}

	// Validate asyncify_start_unwind: (i32|i64) -> ()
	startUnwindFT := m.GetFuncType(helperExports[ExportStartUnwind].Idx)
	if startUnwindFT == nil || len(startUnwindFT.Results) != 0 || len(startUnwindFT.Params) != 1 {
		return false
	}
	ptrType := startUnwindFT.Params[0]
	if ptrType != wasm.ValI32 && ptrType != wasm.ValI64 {
		return false
	}

	// Validate asyncify_stop_unwind: () -> ()
	stopUnwindFT := m.GetFuncType(helperExports[ExportStopUnwind].Idx)
	if stopUnwindFT == nil || len(stopUnwindFT.Results) != 0 || len(stopUnwindFT.Params) != 0 {
		return false
	}

	// Validate asyncify_start_rewind: (i32|i64) -> (), parameter type must match start_unwind
	startRewindFT := m.GetFuncType(helperExports[ExportStartRewind].Idx)
	if startRewindFT == nil || len(startRewindFT.Results) != 0 || len(startRewindFT.Params) != 1 || startRewindFT.Params[0] != ptrType {
		return false
	}

	// Validate asyncify_stop_rewind: () -> ()
	stopRewindFT := m.GetFuncType(helperExports[ExportStopRewind].Idx)
	if stopRewindFT == nil || len(stopRewindFT.Results) != 0 || len(stopRewindFT.Params) != 0 {
		return false
	}

	// Validate asyncify_get_state: () -> (i32|i64)
	getStateFT := m.GetFuncType(helperExports[ExportGetState].Idx)
	if getStateFT == nil || len(getStateFT.Params) != 0 || len(getStateFT.Results) != 1 {
		return false
	}
	if getStateFT.Results[0] != wasm.ValI32 && getStateFT.Results[0] != wasm.ValI64 {
		return false
	}

	return true
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
