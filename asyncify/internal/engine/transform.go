package engine

import (
	"fmt"
	"math"

	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/wasm"
)

// maxAsyncifyFrameSize is the maximum frame size that can be safely cast to int32.
const maxAsyncifyFrameSize = math.MaxInt32

// Block structure constants for asyncify.
//
// Transformed functions use this nested block layout:
//
//	block (result i32)     ; outer - captures call index on unwind
//	  block                ; middle - normal return path
//	    block              ; inner - entry point, contains transformed code
//	      [user ifs]       ; linearizer-added conditional blocks
//	        if             ; call condition check
//	          if           ; unwind check
//	            br depth   ; break to outer with call index
//
// The base branch depth (asyncifyBranchDepthBase) is the number of blocks
// from the unwind-if to the outer block: unwinding-if(0) -> call-if(1) ->
// inner(2) -> middle(3) -> outer(4).
const asyncifyBranchDepthBase = 4

// Scratch local layout constants (offsets from scratchStart).
// These are allocated after original locals and used by the transform.
const (
	scratchCallIndexSave   = 0 // i32: call site index for save path
	scratchCallIndexRewind = 1 // i32: call site index loaded during rewind
	scratchStackPtr        = 2 // i32: asyncify stack pointer
	// 3-4: i64 scratch, 5-6: f32 scratch, 7-8: f64 scratch, 9: extra i32
	scratchLocalCount = 10 // total scratch locals allocated
)

// CallSite describes an async call within a function.
type CallSite struct {
	CalleeType *wasm.FuncType
	LiveLocals []uint32
	InstrIdx   int
}

// FunctionTransformer transforms individual functions to support asyncify.
type FunctionTransformer struct {
	registry       *handler.Registry
	module         *wasm.Module
	globals        GlobalIndices
	memoryIndex    uint32
	ignoreIndirect bool
}

// NewFunctionTransformer creates a transformer for the given module.
func NewFunctionTransformer(registry *handler.Registry, m *wasm.Module, globals GlobalIndices, memoryIndex uint32, ignoreIndirect bool) *FunctionTransformer {
	return &FunctionTransformer{
		registry:       registry,
		module:         m,
		globals:        globals,
		memoryIndex:    memoryIndex,
		ignoreIndirect: ignoreIndirect,
	}
}

// Transform transforms a function to support async operations.
func (ft *FunctionTransformer) Transform(funcIdx uint32, body *wasm.FuncBody, asyncFuncs map[uint32]bool) error {
	funcType := ft.module.GetFuncType(funcIdx)
	if funcType == nil {
		return nil
	}

	instrs, err := wasm.DecodeInstructions(body.Code)
	if err != nil {
		return err
	}

	// Count original locals before any transformations
	numParams := len(funcType.Params)
	numOriginalLocals := numParams
	for _, le := range body.Locals {
		numOriginalLocals += int(le.Count)
	}

	// Parse to IR tree and check for async calls
	tree := ir.Parse(instrs, ft.module)
	config := &ir.TransformConfig{
		StateGlobal: ft.globals.StateGlobal,
		DataGlobal:  ft.globals.DataGlobal,
		AsyncFuncs:  asyncFuncs,
		Module:      ft.module,
	}
	analysis := ir.Analyze(tree, config)

	if !analysis.NeedsTransform {
		return nil
	}

	// Validate that no reference types are used in parameters or locals
	// Only check for functions that actually need transformation
	if err := ValidateLocalsForAsyncify(funcType.Params, body.Locals); err != nil {
		return err
	}

	// Linearize control flow for asyncify - transforms result-bearing
	// blocks and if/else to handle rewind correctly
	allocLocal := func(vt wasm.ValType) uint32 {
		idx := uint32(numOriginalLocals)
		numOriginalLocals++
		body.Locals = append(body.Locals, wasm.LocalEntry{Count: 1, ValType: vt})
		return idx
	}

	linearConfig := &ir.LinearizeConfig{
		StateGlobal:    ft.globals.StateGlobal,
		StateRewinding: StateRewinding,
		AsyncFuncs:     asyncFuncs,
		Module:         ft.module,
		AllocLocal:     allocLocal,
	}
	instrs = ir.Linearize(tree, linearConfig)
	// Add trailing End instruction
	instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpEnd})

	// Find async call sites in the (possibly transformed) instructions
	var callSites []CallSite
	var asyncCallIndices []int
	for i, instr := range instrs {
		if target, ok := instr.GetCallTarget(); ok && asyncFuncs[target] {
			callSites = append(callSites, CallSite{
				InstrIdx:   i,
				CalleeType: ft.module.GetFuncType(target),
			})
			asyncCallIndices = append(asyncCallIndices, i)
		}
		if instr.IsIndirectCall() && !ft.ignoreIndirect {
			var calleeType *wasm.FuncType
			if imm, ok := instr.Imm.(wasm.CallIndirectImm); ok && int(imm.TypeIdx) < len(ft.module.Types) {
				calleeType = &ft.module.Types[imm.TypeIdx]
			}
			callSites = append(callSites, CallSite{
				InstrIdx:   i,
				CalleeType: calleeType,
			})
			asyncCallIndices = append(asyncCallIndices, i)
		}
		if instr.Opcode == wasm.OpCallRef && !ft.ignoreIndirect {
			var calleeType *wasm.FuncType
			if imm, ok := instr.Imm.(wasm.CallRefImm); ok && int(imm.TypeIdx) < len(ft.module.Types) {
				calleeType = &ft.module.Types[imm.TypeIdx]
			}
			callSites = append(callSites, CallSite{
				InstrIdx:   i,
				CalleeType: calleeType,
			})
			asyncCallIndices = append(asyncCallIndices, i)
		}
	}

	if len(callSites) == 0 {
		return nil
	}

	// Compute liveness for each call site
	la := NewLivenessAnalyzer(numParams, numOriginalLocals-numParams)
	livenessInfo := la.ComputeForCallSites(instrs, asyncCallIndices)
	for i := range callSites {
		callSites[i].LiveLocals = livenessInfo[callSites[i].InstrIdx]
	}

	scratchStart := uint32(numOriginalLocals)

	// Build local type map BEFORE adding scratch locals
	localTypes := make([]wasm.ValType, 0, numOriginalLocals)
	localTypes = append(localTypes, funcType.Params...)
	for _, le := range body.Locals {
		for i := uint32(0); i < le.Count; i++ {
			localTypes = append(localTypes, le.ValType)
		}
	}

	// Add scratch locals with different types
	scratchTypes := []wasm.ValType{
		wasm.ValI32, wasm.ValI32, wasm.ValI32, // callIndexSave, callIndexRewind, stackPtr
		wasm.ValI64, wasm.ValI64,
		wasm.ValF32, wasm.ValF32,
		wasm.ValF64, wasm.ValF64,
		wasm.ValI32, // extra scratch
	}
	for _, vt := range scratchTypes {
		body.Locals = append(body.Locals, wasm.LocalEntry{Count: 1, ValType: vt})
		localTypes = append(localTypes, vt)
	}

	code, err := ft.transformLinear(instrs, callSites, funcType, scratchStart, localTypes, body)
	if err != nil {
		return err
	}
	body.Code = code
	return nil
}

// computeLiveUnion computes the union of all live locals across all call sites.
// This is a conservative approximation - we save any local that is live at ANY call site.
func computeLiveUnion(callSites []CallSite) map[uint32]bool {
	union := make(map[uint32]bool)
	for _, site := range callSites {
		for _, local := range site.LiveLocals {
			union[local] = true
		}
	}
	return union
}

// simulateStackForCallSites does a dry run of instruction processing to track
// which temporaries are on the simulated stack at each async call site.
// Returns: set of local indices that need saving, type map for allocated temps, max local index, error.
// Returns error if reference types are on the stack at an async call site (cannot be saved to memory).
func (ft *FunctionTransformer) simulateStackForCallSites(
	instrs []wasm.Instruction,
	callSites []CallSite,
	scratchStart uint32,
	localTypes []wasm.ValType,
	body *wasm.FuncBody,
	nextResultLocal uint32,
	callSiteResultLocals map[int][]uint32,
) (map[uint32]bool, map[uint32]wasm.ValType, uint32, error) {
	result := make(map[uint32]bool)
	allocatedTypes := make(map[uint32]wasm.ValType)

	// Build call site map
	siteMap := make(map[int]int)
	for i, site := range callSites {
		siteMap[site.InstrIdx] = i
	}

	// Simulate stack - track indices and types
	// The actual locals will be added by handlers during transformLinear
	simNextLocal := nextResultLocal
	allocTemp := func(vt wasm.ValType) uint32 {
		idx := simNextLocal
		allocatedTypes[idx] = vt
		simNextLocal++
		return idx
	}

	// Simple stack simulation
	var stack []stackEntry

	for i, instr := range instrs {
		// Skip trailing End
		if i == len(instrs)-1 && instr.Opcode == wasm.OpEnd {
			continue
		}

		// Check if this is an async call site
		callSiteIdx, isAsync := siteMap[i]
		if isAsync {
			// Check for reference types on stack - these cannot be saved to memory
			for _, entry := range stack {
				if IsReferenceType(entry.Type) {
					return nil, nil, 0, fmt.Errorf("reference type %s on stack at async call site (instruction %d); reference types cannot be saved to linear memory", entry.Type, i)
				}
				result[entry.LocalIdx] = true
			}
		}

		// For async calls, use preAllocatedResults instead of normal simulation
		// This ensures simulation matches handler behavior
		if isAsync {
			site := callSites[callSiteIdx]
			// Pop params from stack (simulation handles this normally)
			if site.CalleeType != nil {
				// Pop params
				for range site.CalleeType.Params {
					if len(stack) > 0 {
						stack = stack[:len(stack)-1]
					}
				}
			}
			// Pop extra operand for call_indirect/call_ref
			if instr.IsIndirectCall() || instr.Opcode == wasm.OpCallRef {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
			// Push preAllocatedResults
			preAllocated := callSiteResultLocals[callSiteIdx]
			if site.CalleeType != nil {
				for i, rt := range site.CalleeType.Results {
					stack = append(stack, stackEntry{LocalIdx: preAllocated[i], Type: rt})
				}
			}
			continue
		}

		// Simulate stack effects for non-async instructions
		ft.simulateInstrStack(&stack, instr, allocTemp, localTypes)
	}

	return result, allocatedTypes, simNextLocal, nil
}

// stackEntry is an alias for handler.StackEntry used in stack simulation.
type stackEntry = handler.StackEntry

// simulateInstrStack simulates the stack effect of an instruction.
func (ft *FunctionTransformer) simulateInstrStack(stack *[]stackEntry, instr wasm.Instruction, allocTemp func(wasm.ValType) uint32, localTypes []wasm.ValType) {
	pop := func() stackEntry {
		if len(*stack) == 0 {
			return stackEntry{LocalIdx: 0, Type: wasm.ValI32}
		}
		last := (*stack)[len(*stack)-1]
		*stack = (*stack)[:len(*stack)-1]
		return last
	}
	push := func(vt wasm.ValType) {
		*stack = append(*stack, stackEntry{LocalIdx: allocTemp(vt), Type: vt})
	}

	// Query handlers first (they implement StackEffecter), then fall back to static table
	if effect := GetStackEffectFromRegistry(ft.registry, instr.Opcode, instr, ft.module); effect != nil {
		for i := 0; i < effect.Pops; i++ {
			pop()
		}
		for _, vt := range effect.Pushes {
			push(vt)
		}
		return
	}

	// Handle instructions with dynamic/complex stack effects
	switch instr.Opcode {
	// Local operations (type depends on local)
	case wasm.OpLocalGet:
		imm := instr.Imm.(wasm.LocalImm)
		vt := wasm.ValI32
		if int(imm.LocalIdx) < len(localTypes) {
			vt = localTypes[imm.LocalIdx]
		}
		push(vt)
	case wasm.OpLocalSet:
		pop()
	case wasm.OpLocalTee:
		imm := instr.Imm.(wasm.LocalImm)
		vt := wasm.ValI32
		if int(imm.LocalIdx) < len(localTypes) {
			vt = localTypes[imm.LocalIdx]
		}
		pop()
		push(vt)

	// Global operations (type depends on global)
	case wasm.OpGlobalGet:
		imm := instr.Imm.(wasm.GlobalImm)
		vt := ft.getGlobalType(imm.GlobalIdx)
		push(vt)
	case wasm.OpGlobalSet:
		pop()

	// Reference types with dynamic type
	case wasm.OpRefNull:
		imm := instr.Imm.(wasm.RefNullImm)
		if imm.HeapType == wasm.HeapTypeFunc {
			push(wasm.ValFuncRef)
		} else {
			push(wasm.ValExtern)
		}
	case wasm.OpRefAsNonNull:
		entry := pop()
		push(entry.Type)
	case wasm.OpBrOnNull:
		entry := pop()
		push(entry.Type)
	case wasm.OpBrOnNonNull:
		pop()

	// Select (type depends on operands)
	case wasm.OpSelect:
		pop()
		falseVal := pop()
		pop()
		push(falseVal.Type)
	case wasm.OpSelectType:
		pop()
		pop()
		pop()
		if imm, ok := instr.Imm.(wasm.SelectTypeImm); ok && len(imm.Types) > 0 {
			push(imm.Types[0])
		} else {
			push(wasm.ValI32)
		}

	// Calls (type depends on callee)
	case wasm.OpCall:
		imm := instr.Imm.(wasm.CallImm)
		if funcType := ft.module.GetFuncType(imm.FuncIdx); funcType != nil {
			for range funcType.Params {
				pop()
			}
			for _, rt := range funcType.Results {
				push(rt)
			}
		}
	case wasm.OpCallIndirect:
		pop()
		imm := instr.Imm.(wasm.CallIndirectImm)
		if int(imm.TypeIdx) < len(ft.module.Types) {
			funcType := &ft.module.Types[imm.TypeIdx]
			for range funcType.Params {
				pop()
			}
			for _, rt := range funcType.Results {
				push(rt)
			}
		}
	case wasm.OpCallRef:
		pop()
		imm := instr.Imm.(wasm.CallRefImm)
		if int(imm.TypeIdx) < len(ft.module.Types) {
			funcType := &ft.module.Types[imm.TypeIdx]
			for range funcType.Params {
				pop()
			}
			for _, rt := range funcType.Results {
				push(rt)
			}
		}

	// Control flow
	case wasm.OpIf:
		pop()
	case wasm.OpBrIf:
		pop()
	case wasm.OpBrTable:
		pop()
	case wasm.OpReturn:
		*stack = (*stack)[:0]
	}
}

// getGlobalType returns the type of a global variable.
// Callers guarantee globalIdx is within bounds.
func (ft *FunctionTransformer) getGlobalType(globalIdx uint32) wasm.ValType {
	numImportedGlobals := uint32(0)
	for _, imp := range ft.module.Imports {
		if imp.Desc.Kind == wasm.KindGlobal {
			if globalIdx == numImportedGlobals {
				return imp.Desc.Global.ValType
			}
			numImportedGlobals++
		}
	}
	localIdx := globalIdx - numImportedGlobals
	return ft.module.Globals[localIdx].Type.ValType
}

func (ft *FunctionTransformer) transformLinear(
	instrs []wasm.Instruction,
	callSites []CallSite,
	funcType *wasm.FuncType,
	scratchStart uint32,
	localTypes []wasm.ValType,
	body *wasm.FuncBody,
) ([]byte, error) {
	// Pre-size emitter: transformed code is typically 3-5x larger
	estimatedSize := len(instrs) * 12
	em := codegen.GetEmitterWithCapacity(estimatedSize)
	defer codegen.PutEmitter(em)

	// Scratch locals are allocated at scratchStart with layout defined by scratch* constants.
	localCallIndexSave := scratchStart + scratchCallIndexSave
	localCallIndexRewind := scratchStart + scratchCallIndexRewind
	localStackPtr := scratchStart + scratchStackPtr

	// Compute live locals union for prelude
	liveUnion := computeLiveUnion(callSites)

	// Pre-allocate result locals for each call site
	// Map from call site index to its pre-allocated result locals
	callSiteResultLocals := make(map[int][]uint32)
	nextResultLocal := scratchStart + scratchLocalCount

	for i, site := range callSites {
		if site.CalleeType != nil && len(site.CalleeType.Results) > 0 {
			var resultLocals []uint32
			for _, rt := range site.CalleeType.Results {
				localIdx := nextResultLocal
				nextResultLocal++
				body.Locals = append(body.Locals, wasm.LocalEntry{Count: 1, ValType: rt})
				localTypes = append(localTypes, rt)
				resultLocals = append(resultLocals, localIdx)
				// Add to liveUnion so it gets saved/restored
				liveUnion[localIdx] = true
			}
			callSiteResultLocals[i] = resultLocals
		}
	}

	// Simulation pass: track simulated stack to find temporaries that need saving
	// These are locals holding operand values at async call sites
	// The simulation returns local indices, their types, and max local index
	stackLocalsAtCallSites, allocatedTypes, maxSimLocal, err := ft.simulateStackForCallSites(instrs, callSites, scratchStart, localTypes, body, nextResultLocal, callSiteResultLocals)
	if err != nil {
		return nil, err
	}

	// Pre-declare locals up to maxSimLocal with correct types
	// The handlers will allocate additional locals starting from maxSimLocal
	for localIdx := uint32(len(localTypes)); localIdx < maxSimLocal; localIdx++ {
		vt := wasm.ValI32
		if t, ok := allocatedTypes[localIdx]; ok {
			vt = t
		}
		body.Locals = append(body.Locals, wasm.LocalEntry{Count: 1, ValType: vt})
		localTypes = append(localTypes, vt)
	}

	for localIdx := range stackLocalsAtCallSites {
		liveUnion[localIdx] = true
	}

	// Prelude: restore live locals (including pre-allocated result locals)
	if err := ft.emitPrelude(em, localTypes, liveUnion); err != nil {
		return nil, err
	}

	// Main structure: 3 nested blocks
	// Outer block is ALWAYS i32 to capture the call index for save path
	em.Block(codegen.BlockI32).Block(codegen.BlockVoid).Block(codegen.BlockVoid)

	// Restore call index if rewinding
	// The prelude already positioned stack_ptr at frame base
	// Layout: [call_index at offset 0][locals at offset 4+]
	if len(callSites) > 0 {
		em.StateCheck(ft.globals.StateGlobal, StateRewinding).If(codegen.BlockVoid)
		em.GlobalGet(ft.globals.DataGlobal).
			I32Load(2, 0). // load stack_ptr (already at frame base)
			I32Load(2, 0). // load call_index from offset 0
			LocalSet(localCallIndexRewind).
			End()
	}

	// Build call site map
	siteMap := make(map[int]int)
	for i, site := range callSites {
		siteMap[site.InstrIdx] = i
	}

	// Stack tracking
	// Handlers start allocating from nextResultLocal (same as simulation)
	// This ensures handler-allocated locals match simulation-tracked locals
	// so that locals live at async call sites are correctly saved/restored
	stack := handler.NewStack(localCallIndexSave) // fallback for empty stack pops (unreachable paths)
	locals := handler.NewLocals(nextResultLocal, body, localTypes)
	ctx := handler.NewContext(em, stack, locals, ft.globals.StateGlobal, ft.globals.DataGlobal)
	ctx.Module = ft.module

	// Track control depth from linearizer-added ifs
	// Base depth is 0 at start (we're inside the inner block)
	controlDepth := 0

	// Track whether we have an open "normal state" guard block
	// Non-control-flow code before async calls must be wrapped in if(state==Normal)
	// to skip re-execution during rewind
	inNormalGuard := false

	// Helper to close the normal guard if open
	closeNormalGuard := func() {
		if inNormalGuard {
			em.End()
			inNormalGuard = false
		}
	}

	// Helper to ensure we're in a normal guard
	ensureNormalGuard := func() {
		if !inNormalGuard {
			em.StateCheck(ft.globals.StateGlobal, StateNormal).If(codegen.BlockVoid)
			inNormalGuard = true
		}
	}

	// Process instructions incrementally, preserving control flow structure
	for i, instr := range instrs {
		// Skip trailing End
		if i == len(instrs)-1 && instr.Opcode == wasm.OpEnd {
			continue
		}

		// Track control flow depth for br targeting
		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
			controlDepth++
		case wasm.OpEnd:
			if controlDepth > 0 {
				controlDepth--
			}
		}

		if callSiteIdx, isAsync := siteMap[i]; isAsync {
			// Close any open normal guard before the async call site
			closeNormalGuard()

			site := callSites[callSiteIdx]
			preAllocatedResults := callSiteResultLocals[callSiteIdx]
			ft.emitAsyncCallSite(ctx, &instr, site, callSiteIdx, localCallIndexRewind, funcType, controlDepth, preAllocatedResults)
			continue
		}

		// Check if this instruction is control flow (should not be guarded)
		if isControlFlowInstruction(instr.Opcode) {
			// Close normal guard before control flow instructions
			closeNormalGuard()
			// Emit control flow instruction directly
			if err := ft.emitSingleInstruction(ctx, instr); err != nil {
				return nil, err
			}
		} else {
			// Non-control-flow instruction: wrap in normal guard
			ensureNormalGuard()
			if err := ft.emitSingleInstruction(ctx, instr); err != nil {
				return nil, err
			}
		}
	}

	// Close any remaining normal guard
	closeNormalGuard()

	// Close inner block
	em.End()

	// Return based on function result type
	if len(funcType.Results) > 0 {
		// Pop all results from simulated stack (in reverse order)
		var resultLocals []uint32
		for i := 0; i < len(funcType.Results); i++ {
			resultLocals = append(resultLocals, stack.Pop())
		}
		// Push all results onto real stack (reverse to get correct order)
		for i := len(resultLocals) - 1; i >= 0; i-- {
			em.LocalGet(resultLocals[i])
		}
		em.Return()
	} else {
		em.Return()
	}

	// Close middle block
	em.End()
	em.Unreachable()

	// Close outer block - captures call index
	em.End()
	em.LocalSet(localCallIndexSave)

	// Save path - save live locals (including pre-allocated result locals)
	if err := ft.emitSavePath(em, localStackPtr, localCallIndexSave, localTypes, liveUnion); err != nil {
		return nil, err
	}

	// Function end - emit dummy return value of correct type
	for _, rt := range funcType.Results {
		switch rt {
		case wasm.ValI32:
			em.I32Const(0)
		case wasm.ValI64:
			em.I64Const(0)
		case wasm.ValF32:
			em.F32Const(0)
		case wasm.ValF64:
			em.F64Const(0)
		case wasm.ValV128:
			em.EmitV128Const([16]byte{})
		case wasm.ValFuncRef:
			em.RefNullFunc()
		case wasm.ValExtern:
			em.RefNullExtern()
		}
	}
	em.End()

	return em.Copy(), nil
}

// getLocalType returns the type of a local.
// Callers guarantee localIdx is within bounds.
func getLocalType(localTypes []wasm.ValType, localIdx uint32) wasm.ValType {
	return localTypes[localIdx]
}

func (ft *FunctionTransformer) emitPrelude(em *codegen.Emitter, localTypes []wasm.ValType, liveLocals map[uint32]bool) error {
	em.StateCheck(ft.globals.StateGlobal, StateRewinding).If(codegen.BlockVoid)

	// Build sorted list of live locals (ascending order - same as save)
	liveList := sortedLocals(liveLocals, true)

	// Compute total frame size for decrement
	frameSize := uint32(4) // call index
	for _, localIdx := range liveList {
		frameSize += uint32(ValTypeSize(getLocalType(localTypes, localIdx)))
	}

	// Validate frame size fits in int32 to prevent overflow
	if frameSize > maxAsyncifyFrameSize {
		return fmt.Errorf("asyncify: frame size %d exceeds maximum %d", frameSize, maxAsyncifyFrameSize)
	}

	// Decrement stack_ptr once to point to start of frame
	// stack_ptr = stack_ptr - frameSize
	em.GlobalGet(ft.globals.DataGlobal).
		GlobalGet(ft.globals.DataGlobal).
		I32Load(2, 0).
		I32Const(int32(frameSize)).
		I32Sub().
		I32Store(2, 0)

	// Restore all locals at computed offsets
	// Layout: [call_index (4 bytes)][local0][local1]...[localN]
	offset := uint32(4) // Start after call index
	for _, localIdx := range liveList {
		vt := getLocalType(localTypes, localIdx)
		size := ValTypeSize(vt)

		// Load local from base + offset
		em.GlobalGet(ft.globals.DataGlobal).I32Load(2, 0)
		if IsV128Type(vt) {
			em.EmitInstr(MakeV128Load(4, offset))
		} else {
			loadOp, loadAlign := ValTypeLoadOp(vt)
			em.EmitInstr(wasm.Instruction{Opcode: loadOp, Imm: wasm.MemoryImm{Align: loadAlign, Offset: uint64(offset)}})
		}
		em.LocalSet(localIdx)
		offset += uint32(size)
	}

	em.End()
	return nil
}

// sortedLocals returns a sorted slice of local indices from the map.
// If ascending is true, sorts low to high; otherwise high to low.
func sortedLocals(locals map[uint32]bool, ascending bool) []uint32 {
	result := make([]uint32, 0, len(locals))
	for local := range locals {
		result = append(result, local)
	}
	// Simple insertion sort (locals count is typically small)
	for i := 1; i < len(result); i++ {
		for j := i; j > 0; j-- {
			swap := false
			if ascending {
				swap = result[j] < result[j-1]
			} else {
				swap = result[j] > result[j-1]
			}
			if swap {
				result[j], result[j-1] = result[j-1], result[j]
			} else {
				break
			}
		}
	}
	return result
}

func (ft *FunctionTransformer) emitSavePath(em *codegen.Emitter, localStackPtr, localCallIndexSave uint32, localTypes []wasm.ValType, liveLocals map[uint32]bool) error {
	em.StateCheck(ft.globals.StateGlobal, StateUnwinding).If(codegen.BlockVoid)

	// Build sorted list of live locals (ascending order for save)
	liveList := sortedLocals(liveLocals, true)

	// Compute total frame size first for bounds check
	// Layout: [call_index (4 bytes)][local0][local1]...[localN]
	frameSize := uint32(4) // call index
	for _, localIdx := range liveList {
		frameSize += uint32(ValTypeSize(getLocalType(localTypes, localIdx)))
	}

	// Validate frame size fits in int32 to prevent overflow
	if frameSize > maxAsyncifyFrameSize {
		return fmt.Errorf("asyncify: frame size %d exceeds maximum %d", frameSize, maxAsyncifyFrameSize)
	}

	// Load base pointer into local once
	em.GlobalGet(ft.globals.DataGlobal).I32Load(2, 0).LocalSet(localStackPtr)

	// Bounds check BEFORE writing: verify new_stack_ptr <= stack_end
	em.LocalGet(localStackPtr).
		I32Const(int32(frameSize)).
		I32Add().
		GlobalGet(ft.globals.DataGlobal).I32Load(2, 4). // stack_end
		I32GtU().                                       // (stack_ptr + frameSize) > stack_end
		If(codegen.BlockVoid).Unreachable().End()

	// Save call index at offset 0
	em.LocalGet(localStackPtr).LocalGet(localCallIndexSave).I32Store(2, 0)

	// Save all locals at computed offsets
	offset := uint32(4) // Start after call index
	for _, localIdx := range liveList {
		vt := getLocalType(localTypes, localIdx)
		size := ValTypeSize(vt)

		// Store local at base + offset
		em.LocalGet(localStackPtr).LocalGet(localIdx)
		if IsV128Type(vt) {
			em.EmitInstr(MakeV128Store(4, offset))
		} else {
			storeOp, storeAlign := ValTypeStoreOp(vt)
			em.EmitInstr(wasm.Instruction{Opcode: storeOp, Imm: wasm.MemoryImm{Align: storeAlign, Offset: uint64(offset)}})
		}
		offset += uint32(size)
	}

	// Update stack_ptr once with total size
	em.GlobalGet(ft.globals.DataGlobal).
		LocalGet(localStackPtr).
		I32Const(int32(offset)).
		I32Add().
		I32Store(2, 0)

	em.End()
	return nil
}

// needsSingleStackValue returns true if the opcode consumes exactly one value from stack
// for control flow purposes (condition check). Does NOT include drop since that's handled
// by its own handler which just pops from simulated stack without emitting code.
func needsSingleStackValue(op byte) bool {
	switch op {
	case wasm.OpIf, wasm.OpBrIf, wasm.OpBrTable, wasm.OpBrOnNull, wasm.OpBrOnNonNull:
		return true
	}
	return false
}

// isControlFlowInstruction returns true if the opcode is a control flow instruction
// that should not be wrapped in a normal-state guard block.
// Control flow instructions structure the code and must be emitted unconditionally.
func isControlFlowInstruction(op byte) bool {
	switch op {
	case wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpElse, wasm.OpEnd,
		wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable, wasm.OpReturn,
		wasm.OpBrOnNull, wasm.OpBrOnNonNull:
		return true
	}
	return false
}

// emitSingleInstruction emits a single non-async instruction, preserving control flow.
func (ft *FunctionTransformer) emitSingleInstruction(ctx *handler.Context, instr wasm.Instruction) error {
	// Handle non-async calls
	if instr.Opcode == wasm.OpCall || instr.Opcode == wasm.OpCallIndirect || instr.Opcode == wasm.OpCallRef {
		ft.emitNonAsyncCall(ctx, instr)
		return nil
	}

	// Handle return
	if instr.Opcode == wasm.OpReturn {
		ft.emitReturn(ctx)
		return nil
	}

	// For control flow that needs single stack value, reload from simulated stack
	if needsSingleStackValue(instr.Opcode) && ctx.Stack.Len() > 0 {
		entry := ctx.Stack.PopTyped()
		ctx.Emit.LocalGet(entry.LocalIdx)
	}

	// Use handler if available, otherwise emit raw instruction
	h := ft.registry.Get(instr.Opcode)
	if h != nil {
		if err := h.Handle(ctx, instr); err != nil {
			return fmt.Errorf("handler for opcode 0x%02X: %w", instr.Opcode, err)
		}
	} else {
		ctx.Emit.EmitInstr(instr)
	}
	return nil
}

// emitReturn handles return instruction by popping all result values from simulated stack.
func (ft *FunctionTransformer) emitReturn(ctx *handler.Context) {
	// Pop all values from simulated stack and push to real stack (in reverse order)
	var resultLocals []uint32
	for ctx.Stack.Len() > 0 {
		resultLocals = append(resultLocals, ctx.Stack.Pop())
	}
	// Push in reverse order so first result ends up at bottom of stack
	for i := len(resultLocals) - 1; i >= 0; i-- {
		ctx.Emit.LocalGet(resultLocals[i])
	}
	ctx.Emit.Return()
}

// emitNonAsyncCall handles non-async calls by popping params from simulated stack.
func (ft *FunctionTransformer) emitNonAsyncCall(ctx *handler.Context, instr wasm.Instruction) {
	var funcType *wasm.FuncType
	isIndirect := instr.Opcode == wasm.OpCallIndirect
	isCallRef := instr.Opcode == wasm.OpCallRef

	switch instr.Opcode {
	case wasm.OpCallIndirect:
		imm := instr.Imm.(wasm.CallIndirectImm)
		if int(imm.TypeIdx) < len(ctx.Module.Types) {
			funcType = &ctx.Module.Types[imm.TypeIdx]
		}
	case wasm.OpCallRef:
		imm := instr.Imm.(wasm.CallRefImm)
		if int(imm.TypeIdx) < len(ctx.Module.Types) {
			funcType = &ctx.Module.Types[imm.TypeIdx]
		}
	default:
		imm := instr.Imm.(wasm.CallImm)
		funcType = ctx.Module.GetFuncType(imm.FuncIdx)
	}

	// Pop table index for call_indirect or func ref for call_ref
	var extraOperandLocal uint32
	if (isIndirect || isCallRef) && ctx.Stack.Len() > 0 {
		extraOperandLocal = ctx.Stack.Pop()
	}

	// Pop params from simulated stack (reverse order - last param is on top)
	var paramLocals []uint32
	if funcType != nil {
		for i := 0; i < len(funcType.Params) && ctx.Stack.Len() > 0; i++ {
			paramLocals = append(paramLocals, ctx.Stack.Pop())
		}
		// Reverse to get correct order
		for i, j := 0, len(paramLocals)-1; i < j; i, j = i+1, j-1 {
			paramLocals[i], paramLocals[j] = paramLocals[j], paramLocals[i]
		}
	}

	// Push params onto real stack
	for _, local := range paramLocals {
		ctx.Emit.LocalGet(local)
	}

	// Push table index for call_indirect or func ref for call_ref
	if isIndirect || isCallRef {
		ctx.Emit.LocalGet(extraOperandLocal)
	}

	// Emit the call
	ctx.Emit.EmitInstr(instr)

	// Push results onto simulated stack
	if funcType != nil && len(funcType.Results) > 0 {
		for i := len(funcType.Results) - 1; i >= 0; i-- {
			resultType := funcType.Results[i]
			tmpLocal := ctx.Locals.Alloc(resultType)
			ctx.Emit.LocalSet(tmpLocal)
			ctx.Stack.Push(tmpLocal, resultType)
		}
	}
}

func (ft *FunctionTransformer) emitAsyncCallSite(
	ctx *handler.Context,
	instr *wasm.Instruction,
	site CallSite,
	callSiteIdx int,
	localCallIndexRewind uint32,
	funcType *wasm.FuncType,
	controlDepth int,
	preAllocatedResults []uint32,
) {
	em := ctx.Emit
	stack := ctx.Stack

	// For call_indirect/call_ref, there's an extra operand (table index / func ref) not in the type
	var extraOperandLocal uint32
	isIndirect := instr.IsIndirectCall()
	isCallRef := instr.Opcode == wasm.OpCallRef
	if isIndirect || isCallRef {
		extraOperandLocal = stack.Pop()
	}

	var paramLocals []uint32
	if site.CalleeType != nil {
		for range site.CalleeType.Params {
			paramLocals = append(paramLocals, stack.Pop())
		}
		for j, k := 0, len(paramLocals)-1; j < k; j, k = j+1, k-1 {
			paramLocals[j], paramLocals[k] = paramLocals[k], paramLocals[j]
		}
	}

	// if (normal || (rewinding && call_index == site_idx))
	em.StateCheck(ft.globals.StateGlobal, StateNormal).
		LocalGet(localCallIndexRewind).
		I32Const(int32(callSiteIdx)).
		I32Eq().
		StateCheck(ft.globals.StateGlobal, StateRewinding).
		I32And().
		I32Or().
		If(codegen.BlockVoid)

	// Push parameters and call
	for _, local := range paramLocals {
		em.LocalGet(local)
	}
	// For call_indirect/call_ref, push the extra operand after params
	if isIndirect || isCallRef {
		em.LocalGet(extraOperandLocal)
	}
	em.EmitInstr(*instr)

	// Handle results - use pre-allocated locals for multi-value returns
	// Pre-allocated locals are already in liveUnion for save/restore
	if site.CalleeType != nil && len(site.CalleeType.Results) > 0 {
		// Store results in reverse order (last result is on top of stack)
		for i := len(site.CalleeType.Results) - 1; i >= 0; i-- {
			em.LocalSet(preAllocatedResults[i])
		}
	}

	// if (unwinding) break to outer block with call index
	// Add controlDepth for linearizer-added ifs between inner block and call-condition
	brDepth := uint32(asyncifyBranchDepthBase + controlDepth)
	em.StateCheck(ft.globals.StateGlobal, StateUnwinding).
		If(codegen.BlockVoid).
		I32Const(int32(callSiteIdx)).
		Br(brDepth).
		End()

	em.End()

	// Push results onto simulated stack (in order)
	if site.CalleeType != nil && len(preAllocatedResults) > 0 {
		for i, resultLocal := range preAllocatedResults {
			stack.Push(resultLocal, site.CalleeType.Results[i])
		}
	}
}
