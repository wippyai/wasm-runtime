package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/wasm"
)

// Asyncify state constants.
const (
	StateNormal    int32 = 0
	StateUnwinding int32 = 1
	StateRewinding int32 = 2
)

// ImportMatcher determines which imports should trigger asyncify transformation.
//
// When an import matches, all functions that can transitively call it
// will be transformed to support stack switching.
type ImportMatcher interface {
	Match(module, name string) bool
}

// FunctionMatcher determines if a function should be included or excluded.
// Used for addlist/removelist/onlylist configuration.
type FunctionMatcher interface {
	MatchFunction(name string) bool
}

// GlobalIndices holds the indices of asyncify globals added to the module.
type GlobalIndices struct {
	StateGlobal uint32 // asyncify state (0=normal, 1=unwinding, 2=rewinding)
	DataGlobal  uint32 // pointer to asyncify data structure
}

// Config configures the transformation engine.
type Config struct {
	OnlyList             FunctionMatcher
	Matcher              ImportMatcher
	AddList              FunctionMatcher
	RemoveList           FunctionMatcher
	Registry             *handler.Registry
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

// Engine orchestrates the asyncify transformation pipeline.
//
// The engine is stateless between Transform calls. Each Transform
// operates on an independent module.
type Engine struct {
	onlyList             FunctionMatcher
	matcher              ImportMatcher
	addList              FunctionMatcher
	removeList           FunctionMatcher
	registry             *handler.Registry
	memoryIndex          uint32
	secondaryMemoryPages uint32
	ignoreImports        bool
	ignoreIndirect       bool
	asserts              bool
	propagateAddList     bool
	useSecondaryMemory   bool
	importGlobals        bool
	exportGlobals        bool
	wasm64               bool
}

// New creates a new transformation engine with the given config.
func New(cfg Config) *Engine {
	reg := cfg.Registry
	if reg == nil {
		reg = DefaultRegistry()
	}
	secondaryPages := cfg.SecondaryMemoryPages
	if secondaryPages == 0 {
		secondaryPages = 1 // default 1 page
	}
	return &Engine{
		matcher:              cfg.Matcher,
		ignoreImports:        cfg.IgnoreImports,
		registry:             reg,
		addList:              cfg.AddList,
		removeList:           cfg.RemoveList,
		onlyList:             cfg.OnlyList,
		memoryIndex:          cfg.MemoryIndex,
		ignoreIndirect:       cfg.IgnoreIndirect,
		asserts:              cfg.Asserts,
		propagateAddList:     cfg.PropagateAddList,
		useSecondaryMemory:   cfg.UseSecondaryMemory,
		secondaryMemoryPages: secondaryPages,
		importGlobals:        cfg.ImportGlobals,
		exportGlobals:        cfg.ExportGlobals,
		wasm64:               cfg.Wasm64,
	}
}

// DefaultRegistry creates a registry with all standard handlers.
func DefaultRegistry() *handler.Registry {
	r := handler.NewRegistry()
	handler.RegisterPassthroughHandlers(r)
	handler.RegisterVariableHandlers(r)
	handler.RegisterConstantHandlers(r)
	handler.RegisterArithmeticHandlers(r)
	handler.RegisterConversionHandlers(r)
	handler.RegisterMemoryHandlers(r)
	handler.RegisterSIMDHandlers(r)
	handler.RegisterReferenceHandlers(r)
	handler.RegisterGCHandlers(r)
	return r
}

// Transform applies asyncify transformation to a WASM module.
//
// The transformation:
//  1. Parses the input WASM binary
//  2. Adds asyncify globals (state, data pointer)
//  3. Identifies functions that need transformation
//  4. Transforms each function to support stack switching
//  5. Adds asyncify export functions
//  6. Encodes and returns the result
func (e *Engine) Transform(wasmData []byte) ([]byte, error) {
	m, err := wasm.ParseModule(wasmData)
	if err != nil {
		return nil, fmt.Errorf("parse module: %w", err)
	}

	// Check if module is already asyncified (has asyncify exports with function bodies)
	if e.isPreAsyncified(m) {
		return nil, fmt.Errorf("module is already asyncified (has asyncify exports); re-asyncifying is not supported")
	}

	// Check for conflicting asyncify imports and remove them
	if err := e.removeAsyncifyConflicts(m); err != nil {
		return nil, err
	}

	globals, err := e.addGlobals(m)
	if err != nil {
		return nil, err
	}
	e.ensureMemory(m)

	// Validate memory index
	if err := e.validateMemoryIndex(m); err != nil {
		return nil, err
	}

	asyncFuncs, err := e.findAsyncFuncs(m)
	if err != nil {
		return nil, err
	}
	if len(asyncFuncs) == 0 {
		e.addExports(m, globals)
		return m.Encode(), nil
	}

	// Validate only functions that will be transformed for unsupported opcodes
	numImported := uint32(m.NumImportedFuncs())
	if err := e.validateAsyncFuncs(m, asyncFuncs, numImported); err != nil {
		return nil, err
	}

	transformer := NewFunctionTransformer(e.registry, m, globals, e.memoryIndex, e.ignoreIndirect)

	for funcIdx := range asyncFuncs {
		if funcIdx >= numImported {
			localIdx := funcIdx - numImported
			if int(localIdx) < len(m.Code) {
				if err := transformer.Transform(funcIdx, &m.Code[localIdx], asyncFuncs); err != nil {
					return nil, fmt.Errorf("transform func %d: %w", funcIdx, err)
				}
			}
		}
	}

	// Add assertions to non-instrumented functions
	if e.asserts {
		if err := e.addAssertions(m, globals, asyncFuncs, numImported); err != nil {
			return nil, err
		}
	}

	e.addExports(m, globals)
	return m.Encode(), nil
}

// addAssertions adds runtime assertions to non-instrumented functions.
// These assertions check that asyncify state is normal (0) at function entry.
func (e *Engine) addAssertions(m *wasm.Module, globals GlobalIndices, asyncFuncs map[uint32]bool, numImported uint32) error {
	for i := range m.Code {
		funcIdx := numImported + uint32(i)
		if asyncFuncs[funcIdx] {
			continue // skip instrumented functions
		}

		body := &m.Code[i]
		instrs, err := wasm.DecodeInstructions(body.Code)
		if err != nil {
			return fmt.Errorf("decode func %d: %w", funcIdx, err)
		}
		if len(instrs) == 0 {
			continue // empty function body is valid
		}

		// Build assertion prefix: if (state != 0) unreachable
		// global.get $state
		// i32.const 0
		// i32.ne
		// if
		//   unreachable
		// end
		assertCode := []byte{
			wasm.OpGlobalGet,
		}
		assertCode = append(assertCode, wasm.EncodeLEB128u(globals.StateGlobal)...)
		assertCode = append(assertCode,
			wasm.OpI32Const, 0, // i32.const 0
			wasm.OpI32Ne,    // i32.ne
			wasm.OpIf, 0x40, // if (empty block type)
			wasm.OpUnreachable, // unreachable
			wasm.OpEnd,         // end if
		)

		// Prepend assertion to existing code
		newCode := make([]byte, 0, len(assertCode)+len(body.Code))
		newCode = append(newCode, assertCode...)
		newCode = append(newCode, body.Code...)
		body.Code = newCode
	}
	return nil
}

// addGlobals adds or imports the asyncify state and data pointer globals.
func (e *Engine) addGlobals(m *wasm.Module) (GlobalIndices, error) {
	if e.importGlobals {
		return e.importAsyncifyGlobals(m)
	}
	return e.createAsyncifyGlobals(m), nil
}

// importAsyncifyGlobals imports globals from "env" module.
func (e *Engine) importAsyncifyGlobals(m *wasm.Module) (GlobalIndices, error) {
	// Add global imports at the beginning of imports list
	newImports := []wasm.Import{
		{
			Module: "env",
			Name:   "asyncify_state",
			Desc:   wasm.ImportDesc{Kind: wasm.KindGlobal, Global: &wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}},
		},
		{
			Module: "env",
			Name:   "asyncify_data",
			Desc:   wasm.ImportDesc{Kind: wasm.KindGlobal, Global: &wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}},
		},
	}
	m.Imports = append(newImports, m.Imports...)

	// Adjust all existing global references in code by +2
	if err := e.adjustGlobalReferences(m, 2); err != nil {
		return GlobalIndices{}, fmt.Errorf("adjust global refs: %w", err)
	}

	// New globals are at indices 0 and 1 (prepended first)
	return GlobalIndices{
		StateGlobal: 0,
		DataGlobal:  1,
	}, nil
}

// createAsyncifyGlobals creates local globals.
func (e *Engine) createAsyncifyGlobals(m *wasm.Module) GlobalIndices {
	baseIdx := uint32(len(m.Globals))

	for _, imp := range m.Imports {
		if imp.Desc.Kind == wasm.KindGlobal {
			baseIdx++
		}
	}

	m.Globals = append(m.Globals, wasm.Global{
		Type: wasm.GlobalType{
			ValType: wasm.ValI32,
			Mutable: true,
		},
		Init: wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
			{Opcode: wasm.OpEnd},
		}),
	})

	m.Globals = append(m.Globals, wasm.Global{
		Type: wasm.GlobalType{
			ValType: wasm.ValI32,
			Mutable: true,
		},
		Init: wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
			{Opcode: wasm.OpEnd},
		}),
	})

	return GlobalIndices{
		StateGlobal: baseIdx,
		DataGlobal:  baseIdx + 1,
	}
}

// ensureMemory adds a default memory if none exists, or adds secondary memory.
func (e *Engine) ensureMemory(m *wasm.Module) {
	if e.useSecondaryMemory {
		e.addSecondaryMemory(m)
		return
	}

	for _, imp := range m.Imports {
		if imp.Desc.Kind == wasm.KindMemory {
			return
		}
	}
	if len(m.Memories) > 0 {
		return
	}
	m.Memories = append(m.Memories, wasm.MemoryType{
		Limits: wasm.Limits{Min: 1, Max: nil},
	})
}

// addSecondaryMemory adds a separate memory for asyncify state.
func (e *Engine) addSecondaryMemory(m *wasm.Module) {
	// Add the secondary memory
	m.Memories = append(m.Memories, wasm.MemoryType{
		Limits: wasm.Limits{Min: uint64(e.secondaryMemoryPages), Max: nil},
	})

	// Count imported memories
	importedMemories := uint32(0)
	for _, imp := range m.Imports {
		if imp.Desc.Kind == wasm.KindMemory {
			importedMemories++
		}
	}

	// The secondary memory index is the last one
	secondaryMemIdx := importedMemories + uint32(len(m.Memories)) - 1

	// Update memory index to use secondary memory
	e.memoryIndex = secondaryMemIdx

	// Export the secondary memory
	m.Exports = append(m.Exports, wasm.Export{
		Name: "asyncify_memory",
		Kind: wasm.KindMemory,
		Idx:  secondaryMemIdx,
	})
}

// adjustGlobalReferences updates all global.get and global.set indices by delta.
// Called when prepending global imports to adjust existing code references.
func (e *Engine) adjustGlobalReferences(m *wasm.Module, delta uint32) error {
	numImported := uint32(m.NumImportedFuncs())
	for i := range m.Code {
		funcIdx := numImported + uint32(i)
		instrs, err := wasm.DecodeInstructions(m.Code[i].Code)
		if err != nil {
			return fmt.Errorf("decode func %d: %w", funcIdx, err)
		}

		modified := false
		for j := range instrs {
			switch instrs[j].Opcode {
			case wasm.OpGlobalGet, wasm.OpGlobalSet:
				if imm, ok := instrs[j].Imm.(wasm.GlobalImm); ok {
					instrs[j].Imm = wasm.GlobalImm{GlobalIdx: imm.GlobalIdx + delta}
					modified = true
				}
			}
		}

		if modified {
			m.Code[i].Code = wasm.EncodeInstructions(instrs)
		}
	}

	// Also adjust global references in global initializers
	for i := range m.Globals {
		instrs, err := wasm.DecodeInstructions(m.Globals[i].Init)
		if err != nil {
			return fmt.Errorf("decode global %d init: %w", i, err)
		}

		modified := false
		for j := range instrs {
			if instrs[j].Opcode == wasm.OpGlobalGet {
				if imm, ok := instrs[j].Imm.(wasm.GlobalImm); ok {
					instrs[j].Imm = wasm.GlobalImm{GlobalIdx: imm.GlobalIdx + delta}
					modified = true
				}
			}
		}

		if modified {
			m.Globals[i].Init = wasm.EncodeInstructions(instrs)
		}
	}

	// Adjust element segment initializers that reference globals
	for i := range m.Elements {
		if m.Elements[i].Offset != nil {
			instrs, err := wasm.DecodeInstructions(m.Elements[i].Offset)
			if err != nil {
				return fmt.Errorf("decode element %d offset: %w", i, err)
			}

			modified := false
			for j := range instrs {
				if instrs[j].Opcode == wasm.OpGlobalGet {
					if imm, ok := instrs[j].Imm.(wasm.GlobalImm); ok {
						instrs[j].Imm = wasm.GlobalImm{GlobalIdx: imm.GlobalIdx + delta}
						modified = true
					}
				}
			}

			if modified {
				m.Elements[i].Offset = wasm.EncodeInstructions(instrs)
			}
		}
	}

	// Adjust data segment initializers that reference globals
	for i := range m.Data {
		if m.Data[i].Offset != nil {
			instrs, err := wasm.DecodeInstructions(m.Data[i].Offset)
			if err != nil {
				return fmt.Errorf("decode data %d offset: %w", i, err)
			}

			modified := false
			for j := range instrs {
				if instrs[j].Opcode == wasm.OpGlobalGet {
					if imm, ok := instrs[j].Imm.(wasm.GlobalImm); ok {
						instrs[j].Imm = wasm.GlobalImm{GlobalIdx: imm.GlobalIdx + delta}
						modified = true
					}
				}
			}

			if modified {
				m.Data[i].Offset = wasm.EncodeInstructions(instrs)
			}
		}
	}
	return nil
}

func (e *Engine) validateMemoryIndex(m *wasm.Module) error {
	// Count imported memories
	importedMemories := uint32(0)
	for _, imp := range m.Imports {
		if imp.Desc.Kind == wasm.KindMemory {
			importedMemories++
		}
	}

	totalMemories := importedMemories + uint32(len(m.Memories))
	if e.memoryIndex >= totalMemories {
		return fmt.Errorf("asyncify memory index %d out of range (module has %d memories)", e.memoryIndex, totalMemories)
	}

	return nil
}

// Unsupported opcodes that cannot be asyncified
const (
	opReturnCall         byte = 0x12 // tail call
	opReturnCallIndirect byte = 0x13 // indirect tail call
	opCallRef            byte = 0x14 // typed function references - call via ref
	opReturnCallRef      byte = 0x15 // typed function references - tail call via ref
	opTry                byte = 0x06 // exception handling
	opCatch              byte = 0x07
	opThrow              byte = 0x08
	opRethrow            byte = 0x09
	opDelegate           byte = 0x18
	opCatchAll           byte = 0x19
	opAtomicPrefix       byte = 0xFE // atomic/threads
)

// validateAsyncFuncs checks only functions that will be transformed for unsupported opcodes.
// This allows modules with unsupported features in non-async paths to still be transformed.
func (e *Engine) validateAsyncFuncs(m *wasm.Module, asyncFuncs map[uint32]bool, numImported uint32) error {
	for funcIdx := range asyncFuncs {
		if funcIdx < numImported {
			continue // imports don't have bodies to validate
		}
		localIdx := funcIdx - numImported
		if int(localIdx) >= len(m.Code) {
			continue
		}
		body := &m.Code[localIdx]
		instrs, err := wasm.DecodeInstructions(body.Code)
		if err != nil {
			return fmt.Errorf("func %d: %w", funcIdx, err)
		}
		for _, instr := range instrs {
			switch instr.Opcode {
			case opReturnCall:
				return fmt.Errorf("func %d: asyncify does not support tail calls (return_call) - tail calls eliminate stack frames needed for suspend/resume", funcIdx)
			case opReturnCallIndirect:
				return fmt.Errorf("func %d: asyncify does not support tail calls (return_call_indirect) - tail calls eliminate stack frames needed for suspend/resume", funcIdx)
			case opReturnCallRef:
				return fmt.Errorf("func %d: asyncify does not support tail calls (return_call_ref) - tail calls eliminate stack frames needed for suspend/resume", funcIdx)
			case opTry:
				return fmt.Errorf("func %d: asyncify does not support exception handling (try) - exception unwinding conflicts with asyncify stack", funcIdx)
			case opCatch, opCatchAll:
				return fmt.Errorf("func %d: asyncify does not support exception handling (catch)", funcIdx)
			case opThrow:
				return fmt.Errorf("func %d: asyncify does not support exception handling (throw)", funcIdx)
			case opRethrow:
				return fmt.Errorf("func %d: asyncify does not support exception handling (rethrow)", funcIdx)
			case opDelegate:
				return fmt.Errorf("func %d: asyncify does not support exception handling (delegate)", funcIdx)
			case opAtomicPrefix:
				return fmt.Errorf("func %d: asyncify does not support atomic/thread operations - use asyncify for cooperative single-threaded async instead", funcIdx)
			}
			// Note: GC prefix (0xFB) and typed function references (ref.as_non_null, ref.eq,
			// br_on_null, br_on_non_null) are allowed. They work with reference types which
			// are supported as long as they're not live on the stack at async call sites.
		}
	}
	return nil
}

// isPreAsyncified checks if the module was already asyncified.
// A pre-asyncified module has asyncify function exports pointing to local functions.
func (e *Engine) isPreAsyncified(m *wasm.Module) bool {
	asyncifyNames := map[string]bool{
		"asyncify_start_unwind": true,
		"asyncify_stop_unwind":  true,
		"asyncify_start_rewind": true,
		"asyncify_stop_rewind":  true,
		"asyncify_get_state":    true,
	}

	numImported := uint32(m.NumImportedFuncs())
	asyncifyExports := 0
	for _, exp := range m.Exports {
		if exp.Kind == wasm.KindFunc && asyncifyNames[exp.Name] {
			// Check if it's a local function (not import)
			if exp.Idx >= numImported {
				asyncifyExports++
			}
		}
	}

	// If we have all 5 asyncify exports as local functions, module is pre-asyncified
	return asyncifyExports >= 5
}

// removeAsyncifyConflicts checks for and removes existing asyncify imports.
// This prevents conflicts when adding our own asyncify exports.
func (e *Engine) removeAsyncifyConflicts(m *wasm.Module) error {
	asyncifyFuncs := map[string]bool{
		"asyncify_start_unwind": true,
		"asyncify_stop_unwind":  true,
		"asyncify_start_rewind": true,
		"asyncify_stop_rewind":  true,
		"asyncify_get_state":    true,
	}

	// Check for imports from "asyncify" module or with asyncify function names
	// Track which function indices are being removed
	var toRemove []int
	var funcIndicesToRemove []uint32
	funcIdx := uint32(0)
	for i, imp := range m.Imports {
		if imp.Desc.Kind == wasm.KindFunc {
			if imp.Module == "asyncify" || asyncifyFuncs[imp.Name] {
				toRemove = append(toRemove, i)
				funcIndicesToRemove = append(funcIndicesToRemove, funcIdx)
			}
			funcIdx++
		}
	}

	// Remove conflicting imports if any (in reverse order to preserve indices)
	if len(toRemove) > 0 {
		for i := len(toRemove) - 1; i >= 0; i-- {
			idx := toRemove[i]
			m.Imports = append(m.Imports[:idx], m.Imports[idx+1:]...)
		}

		// Build reindex map: old function index -> new function index
		reindexFunc := func(oldIdx uint32) uint32 {
			offset := uint32(0)
			for _, removedIdx := range funcIndicesToRemove {
				if oldIdx > removedIdx {
					offset++
				} else if oldIdx == removedIdx {
					return ^uint32(0) // should not happen for valid references
				}
			}
			return oldIdx - offset
		}

		// Update function references in exports
		for i := range m.Exports {
			if m.Exports[i].Kind == wasm.KindFunc {
				m.Exports[i].Idx = reindexFunc(m.Exports[i].Idx)
			}
		}

		// Update start function if present
		if m.Start != nil {
			newStart := reindexFunc(*m.Start)
			m.Start = &newStart
		}

		// Update table element segments
		for i := range m.Elements {
			for j := range m.Elements[i].FuncIdxs {
				m.Elements[i].FuncIdxs[j] = reindexFunc(m.Elements[i].FuncIdxs[j])
			}
		}

		// Update call instructions in all function bodies
		numImported := uint32(m.NumImportedFuncs())
		for i := range m.Code {
			funcIdx := numImported + uint32(i)
			instrs, err := wasm.DecodeInstructions(m.Code[i].Code)
			if err != nil {
				return fmt.Errorf("decode func %d: %w", funcIdx, err)
			}
			modified := false
			for j := range instrs {
				switch instrs[j].Opcode {
				case wasm.OpCall:
					if imm, ok := instrs[j].Imm.(wasm.CallImm); ok {
						newIdx := reindexFunc(imm.FuncIdx)
						if newIdx != imm.FuncIdx {
							instrs[j].Imm = wasm.CallImm{FuncIdx: newIdx}
							modified = true
						}
					}
				case wasm.OpRefFunc:
					if imm, ok := instrs[j].Imm.(wasm.RefFuncImm); ok {
						newIdx := reindexFunc(imm.FuncIdx)
						if newIdx != imm.FuncIdx {
							instrs[j].Imm = wasm.RefFuncImm{FuncIdx: newIdx}
							modified = true
						}
					}
				}
			}
			if modified {
				m.Code[i].Code = wasm.EncodeInstructions(instrs)
			}
		}
	}

	// Check for existing exports with asyncify names (always check, even if no imports removed)
	var exportRemove []int
	for i, exp := range m.Exports {
		if asyncifyFuncs[exp.Name] {
			exportRemove = append(exportRemove, i)
		}
	}

	// Remove conflicting exports (in reverse order)
	for i := len(exportRemove) - 1; i >= 0; i-- {
		idx := exportRemove[i]
		m.Exports = append(m.Exports[:idx], m.Exports[idx+1:]...)
	}

	return nil
}

// findAsyncFuncs identifies functions that need transformation.
func (e *Engine) findAsyncFuncs(m *wasm.Module) (map[uint32]bool, error) {
	// Build export name map: funcIdx -> name
	exportNames := make(map[uint32]string)
	for _, exp := range m.Exports {
		if exp.Kind == wasm.KindFunc {
			exportNames[exp.Idx] = exp.Name
		}
	}

	cg, err := BuildCallGraph(m)
	if err != nil {
		return nil, fmt.Errorf("build call graph: %w", err)
	}

	// Find async imports (skip if ignoreImports is set)
	asyncImports := make(map[uint32]bool)
	if e.matcher != nil && !e.ignoreImports {
		importIdx := uint32(0)
		for _, imp := range m.Imports {
			if imp.Desc.Kind == wasm.KindFunc {
				if e.matcher.Match(imp.Module, imp.Name) {
					asyncImports[importIdx] = true
				}
				importIdx++
			}
		}
	}

	// Get transitive callers of async imports
	var result map[uint32]bool
	if len(asyncImports) > 0 {
		result = cg.TransitiveCallers(asyncImports)
	} else {
		result = make(map[uint32]bool)
	}

	// Include functions with call_indirect (unless ignoreIndirect is set)
	if !e.ignoreIndirect {
		numImported := uint32(m.NumImportedFuncs())
		for i, body := range m.Code {
			funcIdx := numImported + uint32(i)
			instrs, err := wasm.DecodeInstructions(body.Code)
			if err != nil {
				return nil, fmt.Errorf("decode func %d: %w", funcIdx, err)
			}
			for _, instr := range instrs {
				if instr.IsIndirectCall() || instr.Opcode == wasm.OpCallRef {
					result[funcIdx] = true
					break
				}
			}
		}
	}

	// Apply addList - force these functions to be transformed
	if e.addList != nil {
		addedFuncs := make(map[uint32]bool)
		for funcIdx, name := range exportNames {
			if e.addList.MatchFunction(name) {
				result[funcIdx] = true
				addedFuncs[funcIdx] = true
			}
		}

		// PropagateAddList - also instrument callers of addList functions
		if e.propagateAddList && len(addedFuncs) > 0 {
			callers := cg.TransitiveCallers(addedFuncs)
			for funcIdx := range callers {
				result[funcIdx] = true
			}
		}
	}

	// Apply onlyList - restrict to only these functions and their callees
	if e.onlyList != nil {
		filtered := make(map[uint32]bool)
		onlyFuncs := make(map[uint32]bool)

		// Find functions matching onlyList
		for funcIdx, name := range exportNames {
			if e.onlyList.MatchFunction(name) {
				onlyFuncs[funcIdx] = true
			}
		}

		// Include matched functions and their transitive callees
		callees := cg.TransitiveCallees(onlyFuncs)
		for funcIdx := range result {
			if onlyFuncs[funcIdx] || callees[funcIdx] {
				filtered[funcIdx] = true
			}
		}
		result = filtered
	}

	// Apply removeList - exclude these functions (highest priority)
	if e.removeList != nil {
		for funcIdx, name := range exportNames {
			if e.removeList.MatchFunction(name) {
				delete(result, funcIdx)
			}
		}
	}

	if len(result) == 0 {
		return nil, nil
	}

	return result, nil
}

// addExports adds asyncify helper function exports.
func (e *Engine) addExports(m *wasm.Module, globals GlobalIndices) {
	var em *ExportManager
	if e.wasm64 {
		em = NewExportManager64(m, globals)
	} else {
		em = NewExportManager(m, globals)
	}
	em.AddAsyncifyExports()

	if e.exportGlobals {
		m.Exports = append(m.Exports, wasm.Export{
			Name: "asyncify_state",
			Kind: wasm.KindGlobal,
			Idx:  globals.StateGlobal,
		})
		m.Exports = append(m.Exports, wasm.Export{
			Name: "asyncify_data",
			Kind: wasm.KindGlobal,
			Idx:  globals.DataGlobal,
		})
	}
}
