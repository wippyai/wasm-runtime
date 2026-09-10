package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

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
	scratchLocalCount      = 3 // the three i32 control slots above
)

// CallSite describes an async call within a function.
type CallSite struct {
	SourceOperands *ir.Continuation
	ControlLocals  []uint32
	Call           semantics.CallOperation
	LiveLocals     []uint32
	ActionIndex    int
}

// FunctionTransformer transforms individual functions to support asyncify.
type FunctionTransformer struct {
	calls          *semantics.Calls
	registry       *handler.Registry
	module         *wasm.Module
	globals        GlobalIndices
	memoryIndex    uint32
	ignoreIndirect bool
}

// NewFunctionTransformer creates a transformer for the given module.
func NewFunctionTransformer(registry *handler.Registry, m *wasm.Module, globals GlobalIndices, memoryIndex uint32, ignoreIndirect bool) *FunctionTransformer {
	return &FunctionTransformer{
		calls:          semantics.NewCalls(m),
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

	// Count original locals before any transformations
	numParams := len(funcType.Params)
	numOriginalLocals := numParams
	for _, le := range body.Locals {
		numOriginalLocals += int(le.Count)
	}

	// Resolve suspension once on an owned source tree, before routing is added.
	suspensionPolicy := func(call semantics.CallOperation) bool {
		if call.Kind == semantics.DirectCall {
			return asyncFuncs[call.TargetIndex]
		}
		return !ft.ignoreIndirect
	}
	analysis, err := ir.Prepare(body.Code, ft.module, ft.calls, suspensionPolicy, funcType.Results)
	if err != nil {
		return err
	}
	if !analysis.NeedsTransform() {
		return nil
	}

	// Resolve original local identities before generated locals can alias them.
	localTypes := make([]wasm.ValType, 0, numOriginalLocals)
	localTypes = append(localTypes, funcType.Params...)
	for _, le := range body.Locals {
		for i := uint32(0); i < le.Count; i++ {
			localTypes = append(localTypes, le.ValType)
		}
	}

	values, err := ir.PlanValues(analysis, func(instr wasm.Instruction) (ir.OperandShape, error) {
		return ft.sourceOperandShape(instr, localTypes)
	})
	if err != nil {
		return err
	}

	// Validate every original source operation before eliding non-executing
	// code. Runtime reachability and Wasm's local-frame validation state differ.
	source, err := ir.NormalizeSource(values)
	if err != nil {
		return err
	}
	if !source.NeedsTransform() {
		return nil
	}
	if err := ValidateLocalsForAsyncify(funcType.Params, body.Locals); err != nil {
		return err
	}

	// Linearize control flow for asyncify - transforms result-bearing
	// blocks and if/else to handle rewind correctly
	allocLocal := func(vt wasm.ValType) uint32 {
		idx := uint32(numOriginalLocals)
		numOriginalLocals++
		body.Locals = append(body.Locals, wasm.LocalEntry{Count: 1, ValType: vt})
		localTypes = append(localTypes, vt)
		return idx
	}

	linearConfig := &ir.LinearizeConfig{
		StateGlobal:    ft.globals.StateGlobal,
		StateRewinding: StateRewinding,
		AllocLocal:     allocLocal,
	}
	lowered, err := ir.LinearizeNormalized(source, linearConfig)
	if err != nil {
		return err
	}
	// Consume the checked action program; raw instructions are a projection
	// for byte-level liveness analysis, not execution ownership.
	actions, err := lowered.CopyActions()
	if err != nil {
		return err
	}
	plan, err := newExecutionPlan(actions, lowered)
	if err != nil {
		return err
	}
	instrs := plan.steps

	sites := lowered.SuspensionSites()

	// Consume the source analysis's verified call identities and suspension
	// decisions. Routing may move positions, but cannot invent or retarget calls.
	callSites := make([]CallSite, 0, len(sites))
	asyncCallIndices := make([]int, 0, len(sites))
	for _, site := range sites {
		callSites = append(callSites, CallSite{ActionIndex: site.ActionIndex, Call: site.Call, SourceOperands: site.Operands, ControlLocals: site.ControlLocals})
		step, err := plan.sourceStep(site.ActionIndex)
		if err != nil {
			return err
		}
		asyncCallIndices = append(asyncCallIndices, step)
	}

	if len(callSites) == 0 {
		return nil
	}

	// Compute liveness for each call site
	la := NewLivenessAnalyzer(numParams, numOriginalLocals-numParams)
	livenessInfo := la.ComputeForCallSites(instrs, asyncCallIndices)
	for i := range callSites {
		callSites[i].LiveLocals = livenessInfo[asyncCallIndices[i]]
	}

	scratchStart := uint32(numOriginalLocals)

	// Reserve exactly the control slots consumed by emission. Typed operand
	// temporaries and call results are allocated separately from this region.
	for range scratchLocalCount {
		body.Locals = append(body.Locals, wasm.LocalEntry{Count: 1, ValType: wasm.ValI32})
		localTypes = append(localTypes, wasm.ValI32)
	}

	completion, err := lowered.Completion()
	if err != nil {
		return err
	}
	code, err := ft.transformLinear(plan, callSites, funcType, scratchStart, localTypes, body, completion)
	if err != nil {
		return err
	}
	body.Code = code
	return nil
}

// simulateStackForCallSites does a dry run of instruction processing to track
// which temporaries are on the simulated stack at each async call site.
// Returns frozen per-continuation operand storage and the temporary allocation plan.
// Returns error if reference types are on the stack at an async call site (cannot be saved to memory).
func (ft *FunctionTransformer) simulateStackForCallSites(
	actions []ir.Action,
	callSites []CallSite,
	scratchStart uint32,
	localTypes []wasm.ValType,
	body *wasm.FuncBody,
	firstTempLocal uint32,
	closedRegions map[int]int,
	source ...*ir.LoweredControl,
) (*continuationStorage, *TempAllocator, error) {
	result, err := newContinuationStorage(callSites)
	if err != nil {
		return nil, nil, err
	}
	allocator := NewTempAllocator(firstTempLocal)

	// Build call site map
	siteMap := make(map[int]int)
	for i, site := range callSites {
		siteMap[site.ActionIndex] = i
	}

	// Simple stack simulation
	var stack []stackEntry
	var ifSimSnapshots [][]stackEntry
	var ctrlSimStack []byte

	var binding stackBindingStep
	var routing routingBoundary
	at := func(index int) (stackEntry, bool) {
		if index < 0 || index >= len(stack) {
			return stackEntry{}, false
		}
		return stack[index], true
	}
	bind := func(index int, token uint64) error { stack[index] = stack[index].WithBinding(token); return nil }
	for i := 0; i < len(actions); i++ {
		if err := binding.finish(len(stack), at, bind); err != nil {
			return nil, nil, err
		}
		action := actions[i]
		if len(source) > 0 && source[0] != nil {
			domain, ok := action.Domain()
			if err := routing.advance(ok && domain == semantics.RewindRouting, len(stack), at); err != nil {
				return nil, nil, err
			}
		}
		if end, ok := closedRegions[i]; ok {
			allocator.ObserveClosedRegion()
			i = end
			continue
		}

		if len(source) > 0 && source[0] != nil {
			contract, err := source[0].StackContract(i)
			if err != nil {
				return nil, nil, err
			}
			if err := binding.begin(contract, i, len(stack), at); err != nil {
				return nil, nil, err
			}
		}
		allocator.SetCurrentInstr(i)
		if capture, ok := action.Capture(); ok {
			allocator.ObserveCapture()
			inputs, err := transferInputs(capture.OperandCount(), capture.Operand, stack)
			if err != nil {
				return nil, nil, fmt.Errorf("asyncify: capture action %d: %w", i, err)
			}
			for _, input := range inputs {
				if local, stored := input.LocalIndex(); stored {
					allocator.ReleaseOnPop(local)
				}
			}
			stack = stack[:len(stack)-len(inputs)]
			allocator.EndInstruction()
			continue
		}
		if transfer, ok := action.Transfer(); ok {
			allocator.ObserveTransfer()
			inputs, err := transferInputs(transfer.OperandCount(), transfer.Operand, stack)
			if err != nil {
				return nil, nil, fmt.Errorf("asyncify: scope transfer action %d: %w", i, err)
			}
			for _, input := range inputs {
				if local, stored := input.LocalIndex(); stored {
					allocator.ReleaseOnPop(local)
				}
			}
			stack = stack[:len(stack)-len(inputs)]
			allocator.EndInstruction()
			continue
		}
		if operation, ok := action.Branch(); ok {
			allocator.ObserveBranch()
			_, after, err := branchOperands(operation, stack)
			if err != nil {
				return nil, nil, fmt.Errorf("asyncify: branch action %d: %w", i, err)
			}
			allocator.RestoreStack(stack, after)
			stack = after
			allocator.EndInstruction()
			continue
		}
		if operation, ok := action.Trap(); ok {
			allocator.ObserveTrap()
			prefix, err := retainedPrefix(operation.PrefixCount(), operation.PrefixOperand, stack)
			if err != nil {
				return nil, nil, fmt.Errorf("asyncify: trap action %d: %w", i, err)
			}
			allocator.ClearStack(stack[len(prefix):])
			stack = prefix
			allocator.EndInstruction()
			continue
		}
		if operation, ok := action.Return(); ok {
			allocator.ObserveReturn()
			prefix, err := returnPrefix(operation, stack)
			if err != nil {
				return nil, nil, fmt.Errorf("asyncify: return action %d: %w", i, err)
			}
			if operation.ValidationReachable() {
				if _, err := transferInputs(operation.OperandCount(), operation.Operand, stack[len(prefix):]); err != nil {
					return nil, nil, fmt.Errorf("asyncify: return action %d: %w", i, err)
				}
			}
			allocator.ClearStack(stack[len(prefix):])
			stack = prefix
			allocator.EndInstruction()
			continue
		}
		instr, primitive := action.Primitive()
		domain, hasDomain := action.Domain()
		if !primitive || !hasDomain {
			return nil, nil, fmt.Errorf("asyncify: action %d has no execution policy", i)
		}
		allocator.ObserveAction(instr, domain)

		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
			ctrlSimStack = append(ctrlSimStack, instr.Opcode)
		case wasm.OpElse:
			if len(ifSimSnapshots) > 0 {
				oldStack := stack
				stack = append([]stackEntry(nil), ifSimSnapshots[len(ifSimSnapshots)-1]...)
				allocator.RestoreStack(oldStack, stack)
			}
		case wasm.OpEnd:
			if len(ctrlSimStack) > 0 {
				k := ctrlSimStack[len(ctrlSimStack)-1]
				ctrlSimStack = ctrlSimStack[:len(ctrlSimStack)-1]
				if k == wasm.OpIf && len(ifSimSnapshots) > 0 {
					ifSimSnapshots = ifSimSnapshots[:len(ifSimSnapshots)-1]
					allocator.PopSnapshot()
				}
			}
		}

		// Check if this is an async call site
		callSiteIdx, isAsync := siteMap[i]
		if isAsync {
			if err := verifyContinuationOperands(callSites[callSiteIdx].SourceOperands, len(stack), at, allocator); err != nil {
				return nil, nil, fmt.Errorf("asyncify: lowered continuation at instruction %d: %w", i, err)
			}
			// Check for reference types on stack - these cannot be saved to memory
			for _, entry := range stack {
				if IsReferenceType(entry.Type()) {
					return nil, nil, fmt.Errorf("reference type %s on stack at async call site (instruction %d); reference types cannot be saved to linear memory", entry.Type(), i)
				}
			}
			if err := result.capture(i, stack); err != nil {
				return nil, nil, err
			}
		}

		// Call results are ordinary guest definitions with operand lifetimes.
		// Inputs remain owned until EndInstruction, preventing a dummy unwind
		// result from overwriting parameters needed when this call resumes.
		if isAsync {
			site := callSites[callSiteIdx]
			// Pop params from stack (simulation handles this normally)

			for range site.Call.ParamCount() {
				if len(stack) > 0 {
					last := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if local, stored := last.LocalIndex(); stored {
						allocator.ReleaseOnPop(local)
					}
				}
			}

			// Pop extra operand for call_indirect/call_ref
			if site.Call.HasTargetOperand() {
				if len(stack) > 0 {
					last := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if local, stored := last.LocalIndex(); stored {
						allocator.ReleaseOnPop(local)
					}
				}
			}
			for j := range site.Call.ResultCount() {
				rt := site.Call.ResultType(j)
				local := allocator.Alloc(rt)
				stack = append(stack, semantics.StoredOperand(local, rt))
			}

			allocator.EndInstruction()
			continue
		}

		// Simulate stack effects for non-async instructions
		ft.simulateInstrStack(&stack, instr, allocator, localTypes)

		if instr.Opcode == wasm.OpIf {
			snapshot := append([]stackEntry(nil), stack...)
			ifSimSnapshots = append(ifSimSnapshots, snapshot)
			allocator.PushSnapshot(snapshot)
		}

		allocator.EndInstruction()
	}

	if err := binding.finish(len(stack), at, bind); err != nil {
		return nil, nil, err
	}
	if err := routing.advance(false, len(stack), at); err != nil {
		return nil, nil, err
	}
	if err := allocator.Err(); err != nil {
		return nil, nil, err
	}
	if err := result.seal(); err != nil {
		return nil, nil, err
	}
	return result, allocator, nil
}

// stackEntry is an alias for handler.StackEntry used in stack simulation.
type stackEntry = handler.StackEntry

// simulateInstrStack simulates the stack effect of an instruction.
func (ft *FunctionTransformer) simulateInstrStack(stack *[]stackEntry, instr wasm.Instruction, allocator *TempAllocator, localTypes []wasm.ValType) {
	pop := func() stackEntry {
		if len(*stack) == 0 {
			return semantics.StoredOperand(0, wasm.ValI32)
		}
		last := (*stack)[len(*stack)-1]
		*stack = (*stack)[:len(*stack)-1]
		if local, stored := last.LocalIndex(); stored {
			allocator.ReleaseOnPop(local)
		}
		return last
	}
	push := func(vt wasm.ValType) {
		idx := allocator.Alloc(vt)
		*stack = append(*stack, semantics.StoredOperand(idx, vt))
	}

	// Exact literal definitions use the same resolver as LiteralHandler.
	// They own no physical storage; consumers reconstruct their original bits.
	if literal, handled, err := semantics.ResolveLiteral(instr); handled {
		if err != nil {
			allocator.failf("%v", err)
			return
		}
		*stack = append(*stack, semantics.LiteralOperand(literal))
		return
	}

	forward := func(entry stackEntry) {
		result, err := entry.MaterializedAt(allocator.Alloc(entry.Type()))
		if err != nil {
			allocator.failf("%v", err)
			return
		}
		*stack = append(*stack, result)
	}

	// Resolve local value flow through the same semantic description used by
	// emission. Do not fabricate i32 types for invalid local identities.
	if op, handled, err := semantics.ResolveLocal(instr, localTypes); handled {
		if err != nil {
			allocator.failf("%v", err)
			return
		}
		if op.Effects&semantics.AssignLocal != 0 {
			allocator.InvalidateLocal(op.Index)
		}
		if op.Effects == semantics.ProduceSnapshot {
			idx := allocator.SnapshotLocal(op.Index, op.Type)
			*stack = append(*stack, semantics.StoredOperand(idx, op.Type))
			return
		}
		var consumed stackEntry
		if op.Effects&semantics.ConsumeOperand != 0 {
			consumed = pop()
		}
		if op.Effects&semantics.ProduceSnapshot != 0 {
			forward(consumed)
		}
		return
	}

	// Resolve globals before generic effects so invalid metadata is never
	// accepted by a static pop count or independently inferred emission type.
	if op, handled, err := semantics.ResolveGlobal(instr, ft.module); handled {
		if err != nil {
			allocator.failf("%v", err)
			return
		}
		if op.Write {
			pop()
		} else {
			push(op.Type)
		}
		return
	}

	if call, handled, err := ft.calls.Resolve(instr); handled {
		if err != nil {
			allocator.failf("%v", err)
			return
		}
		if call.HasTargetOperand() {
			pop()
		}
		for range call.ParamCount() {
			pop()
		}
		for i := range call.ResultCount() {
			push(call.ResultType(i))
		}
		return
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
	// Reference types with dynamic type
	case wasm.OpRefNull:
		imm := instr.Imm.(wasm.RefNullImm)
		if imm.HeapType == wasm.HeapTypeFunc {
			push(wasm.ValFuncRef)
		} else {
			push(wasm.ValExtern)
		}
	case wasm.OpRefAsNonNull:
		forward(pop())
	case wasm.OpBrOnNull:
		entry := pop()
		push(entry.Type())
	case wasm.OpBrOnNonNull:
		pop()

	// Select (type depends on operands)
	case wasm.OpSelect:
		pop()
		falseVal := pop()
		pop()
		push(falseVal.Type())
	case wasm.OpSelectType:
		pop()
		pop()
		pop()
		if imm, ok := instr.Imm.(wasm.SelectTypeImm); ok && len(imm.Types) > 0 {
			push(imm.Types[0])
		} else {
			push(wasm.ValI32)
		}

	// Control flow
	case wasm.OpIf:
		pop()
	case wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable:
		allocator.failf("source branch requires a typed action")
	case wasm.OpReturn:
		allocator.failf("source return requires a typed action")
	}
}

func (ft *FunctionTransformer) transformLinear(
	program *executionPlan,
	callSites []CallSite,
	funcType *wasm.FuncType,
	scratchStart uint32,
	localTypes []wasm.ValType,
	body *wasm.FuncBody,
	completion ir.FunctionCompletion,
) ([]byte, error) {
	actions, instrs := program.actions, program.steps
	// Pre-size emitter: transformed code is typically 3-5x larger
	estimatedSize := len(instrs) * 12
	em := codegen.GetEmitterWithCapacity(estimatedSize)
	defer codegen.PutEmitter(em)

	// Scratch locals are allocated at scratchStart with layout defined by scratch* constants.
	localCallIndexSave := scratchStart + scratchCallIndexSave
	localCallIndexRewind := scratchStart + scratchCallIndexRewind
	localStackPtr := scratchStart + scratchStackPtr

	// All operand definitions, including async call results, share the checked
	// lifetime allocator. Scratch and source control locals precede this range.
	firstTempLocal := scratchStart + scratchLocalCount

	excludedActions := make(map[int]bool, len(callSites))
	for _, site := range callSites {
		excludedActions[site.ActionIndex] = true
	}
	// Routing actions cannot be copied into a guest-only loop span. This policy
	// comes from action ownership even if an instruction annotation disagrees.
	for index, action := range actions {
		if domain, ok := action.Domain(); ok && domain == semantics.RewindRouting {
			excludedActions[index] = true
		}
	}
	// Planning and emission skip these spans with the same map.
	closedRegions, err := ft.closedActionRegions(program, excludedActions)
	if err != nil {
		return nil, err
	}

	// Simulation pass: track simulated stack to find temporaries that need saving
	// These are locals holding operand values at async call sites
	// The simulation returns local indices, their types, and max local index
	continuations, allocator, err := ft.simulateStackForCallSites(actions, callSites, scratchStart, localTypes, body, firstTempLocal, closedRegions, program.source)
	if err != nil {
		return nil, err
	}
	allocatedTypes := allocator.AllocatedTypes()
	maxSimLocal := allocator.MaxLocal()

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

	liveUnion, err := continuations.savedLocals()
	if err != nil {
		return nil, err
	}

	// Build frame plan once immediately before prelude emission
	plan, err := newFramePlan(localTypes, liveUnion)
	if err != nil {
		return nil, err
	}

	// Prelude: restore the selected live values.
	ft.emitPrelude(em, plan)

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
			LocalSet(localCallIndexRewind)
		// Guard elision after the final call assumes rewind selects a real site.
		// Reject malformed frame indices before dispatch can reach guest effects.
		em.LocalGet(localCallIndexRewind).I32Const(int32(len(callSites))).I32GeU().
			If(codegen.BlockVoid).Unreachable().End()
		em.End()
	}

	// Build call site map
	siteMap := make(map[int]int)
	for i, site := range callSites {
		siteMap[site.ActionIndex] = i
	}

	// Stack tracking
	// Handlers start allocating from firstTempLocal (same as simulation)
	// This ensures handler-allocated locals match simulation-tracked locals
	// so that locals live at async call sites are correctly saved/restored
	stack := handler.NewStack(localCallIndexSave) // fallback for empty stack pops (unreachable paths)
	locals := handler.NewLocals(firstTempLocal, body, localTypes)
	locals.SetAllocationPlan(allocator.Plan())
	ctx := handler.NewContext(em, stack, locals, ft.globals.StateGlobal, ft.globals.DataGlobal)
	ctx.Module = ft.module

	// Track control depth from linearizer-added ifs
	// Base depth is 0 at start (we're inside the inner block)
	controlDepth := 0

	// Track whether we have an open "normal state" guard block
	// Non-control-flow code before async calls must be wrapped in if(state==Normal)
	// to skip re-execution during rewind
	inNormalGuard := false

	var ifSnapshots [][]handler.StackEntry
	var ctrlStack []byte

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

	lastAsyncSiteIdx := -1
	if len(callSites) > 0 {
		lastAsyncSiteIdx = callSites[len(callSites)-1].ActionIndex
	}

	// Dispatch the action program; the final function end belongs to this
	// emitter's enclosing continuation structure, not to a source action.
	var binding stackBindingStep
	var routing routingBoundary
	for i := 0; i < len(actions); i++ {
		if err := binding.finish(stack.Len(), stack.At, stack.BindAt); err != nil {
			return nil, err
		}
		action := actions[i]
		if program.source != nil {
			domain, ok := action.Domain()
			if err := routing.advance(ok && domain == semantics.RewindRouting, stack.Len(), stack.At); err != nil {
				return nil, err
			}
		}
		afterLastAsync := lastAsyncSiteIdx >= 0 && i > lastAsyncSiteIdx

		if end, ok := closedRegions[i]; ok {
			if !afterLastAsync {
				ensureNormalGuard()
			}
			ctx.Locals.BeginClosedRegion(i)
			em.EmitInstrs(instrs[program.ranges[i].start:program.ranges[end].end])
			i = end
			continue
		}
		if program.source != nil {
			contract, err := program.source.StackContract(i)
			if err != nil {
				return nil, err
			}
			if err := binding.begin(contract, i, stack.Len(), stack.At); err != nil {
				return nil, err
			}
		}
		if capture, ok := action.Capture(); ok {
			closeNormalGuard()
			ctx.Locals.BeginCapture(i)
			if err := emitEntryCapture(ctx, capture); err != nil {
				return nil, fmt.Errorf("asyncify: capture action %d: %w", i, err)
			}
			continue
		}
		if transfer, ok := action.Transfer(); ok {
			ctx.Locals.BeginTransfer(i)
			if !afterLastAsync {
				ensureNormalGuard()
			}
			if err := emitScopeTransfer(ctx, transfer); err != nil {
				return nil, fmt.Errorf("asyncify: scope transfer action %d: %w", i, err)
			}
			continue
		}
		if operation, ok := action.Branch(); ok {
			closeNormalGuard()
			ctx.Locals.BeginBranch(i)
			if err := emitSourceBranch(ctx, operation, !afterLastAsync, ft.globals.StateGlobal); err != nil {
				return nil, fmt.Errorf("asyncify: branch action %d: %w", i, err)
			}
			continue
		}
		if operation, ok := action.Trap(); ok {
			closeNormalGuard()
			ctx.Locals.BeginTrap(i)
			if !afterLastAsync {
				em.StateCheck(ft.globals.StateGlobal, StateNormal).If(codegen.BlockVoid)
			}
			if err := emitSourceTrap(ctx, operation); err != nil {
				return nil, fmt.Errorf("asyncify: trap action %d: %w", i, err)
			}
			if !afterLastAsync {
				em.End()
			}
			continue
		}
		if operation, ok := action.Return(); ok {
			closeNormalGuard()
			ctx.Locals.BeginReturn(i)
			if !afterLastAsync {
				em.StateCheck(ft.globals.StateGlobal, StateNormal).If(codegen.BlockVoid)
			}
			if err := emitSourceReturn(ctx, operation); err != nil {
				return nil, fmt.Errorf("asyncify: return action %d: %w", i, err)
			}
			if !afterLastAsync {
				em.End()
			}
			continue
		}
		instr, primitive := action.Primitive()
		domain, hasDomain := action.Domain()
		if !primitive || !hasDomain {
			return nil, fmt.Errorf("asyncify: action %d has no execution policy", i)
		}
		ctx.Locals.BeginAction(i, domain, semantics.LocalKnowledgeEffect(instr, domain))

		// Track control flow depth for br targeting
		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
			controlDepth++
			ctrlStack = append(ctrlStack, instr.Opcode)
		case wasm.OpElse:
			if len(ifSnapshots) > 0 {
				ctx.Stack.Restore(ifSnapshots[len(ifSnapshots)-1])
			}
		case wasm.OpEnd:
			if controlDepth > 0 {
				controlDepth--
			}
			if len(ctrlStack) > 0 {
				k := ctrlStack[len(ctrlStack)-1]
				ctrlStack = ctrlStack[:len(ctrlStack)-1]
				if k == wasm.OpIf && len(ifSnapshots) > 0 {
					ifSnapshots = ifSnapshots[:len(ifSnapshots)-1]
				}
			}
		}

		if callSiteIdx, isAsync := siteMap[i]; isAsync {
			// Close any open normal guard before the async call site
			closeNormalGuard()

			site := callSites[callSiteIdx]
			if err := verifyContinuationOperands(site.SourceOperands, stack.Len(), stack.At, allocator); err != nil {
				return nil, fmt.Errorf("asyncify: emitted continuation at action %d: %w", i, err)
			}
			if err := continuations.verify(i, stack.Len(), stack.At); err != nil {
				return nil, err
			}
			ft.emitAsyncCallSite(ctx, &instr, site, callSiteIdx, localCallIndexRewind, funcType, controlDepth)
			continue
		}

		if domain, ok := action.Domain(); ok && domain == semantics.RewindRouting {
			// Routing actions execute across rewind and normal.
			closeNormalGuard()
			if err := ft.emitSingleInstruction(ctx, instr); err != nil {
				return nil, err
			}
			if instr.Opcode == wasm.OpIf {
				ifSnapshots = append(ifSnapshots, ctx.Stack.Snapshot())
			}
			continue
		}

		if action.Kind() == ir.GuestStructureInstruction {
			// Structural control flow: open/close blocks unconditionally
			closeNormalGuard()
			if err := ft.emitSingleInstruction(ctx, instr); err != nil {
				return nil, err
			}
			if instr.Opcode == wasm.OpIf {
				ifSnapshots = append(ifSnapshots, ctx.Stack.Snapshot())
			}
		} else if isReferenceBranch(instr.Opcode) {
			// Guest branch: must only execute during normal execution, not rewinding
			closeNormalGuard()
			if afterLastAsync {
				if err := ft.emitUnconditionalReferenceBranch(ctx, instr); err != nil {
					return nil, err
				}
			} else {
				if err := ft.emitReferenceBranch(ctx, instr); err != nil {
					return nil, err
				}
			}
		} else {
			// Non-control-flow instruction: wrap in normal guard only when rewinding could reach it
			if !afterLastAsync {
				ensureNormalGuard()
			}
			if err := ft.emitSingleInstruction(ctx, instr); err != nil {
				return nil, err
			}
		}
	}

	if err := binding.finish(stack.Len(), stack.At, stack.BindAt); err != nil {
		return nil, err
	}
	if err := routing.advance(false, stack.Len(), stack.At); err != nil {
		return nil, err
	}
	// Close any remaining normal guard
	closeNormalGuard()

	// Close inner block
	em.End()

	// Never manufacture a value from the empty-stack fallback local: it is
	// an i32 scratch slot, not a polymorphic WASM result.
	if completion.Kind() == ir.UnreachableCompletion {
		em.Unreachable()
	} else if stack.Len() < len(funcType.Results) {
		return nil, fmt.Errorf("asyncify: missing function results on fallthrough: have %d, want %d", stack.Len(), len(funcType.Results))
	} else if len(funcType.Results) > 0 {
		if program.source != nil {
			for position := range funcType.Results {
				value, ok := completion.ResultValue(position)
				if !ok {
					return nil, fmt.Errorf("asyncify: missing source completion identity")
				}
				if err := matchStackValue(value, stack.Len()-len(funcType.Results)+position, stack.At); err != nil {
					return nil, err
				}
			}
		}
		// Pop all results from simulated stack (in reverse order)
		var resultLocals []semantics.Operand
		for i := 0; i < len(funcType.Results); i++ {
			resultLocals = append(resultLocals, stack.Pop())
		}
		// Push all results onto real stack (reverse to get correct order)
		for i := len(resultLocals) - 1; i >= 0; i-- {
			em.Operand(resultLocals[i])
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

	// Save path uses the same selected-value frame as the restore prelude.
	ft.emitSavePath(em, localStackPtr, localCallIndexSave, plan)

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

	if err := locals.FinishPlan(); err != nil {
		return nil, err
	}
	if err := em.Err(); err != nil {
		return nil, err
	}
	return em.Copy(), nil
}

func (ft *FunctionTransformer) emitPrelude(em *codegen.Emitter, plan *framePlan) {
	em.StateCheck(ft.globals.StateGlobal, StateRewinding).If(codegen.BlockVoid)

	// Decrement stack_ptr once to point to start of frame
	// stack_ptr = stack_ptr - frameSize
	em.GlobalGet(ft.globals.DataGlobal).
		GlobalGet(ft.globals.DataGlobal).
		I32Load(2, 0).
		I32Const(int32(plan.frameSize)).
		I32Sub().
		I32Store(2, 0)

	// Restore all locals at computed offsets
	// Layout: [call_index (4 bytes)][local0][local1]...[localN]
	for _, slot := range plan.slots {
		em.GlobalGet(ft.globals.DataGlobal).I32Load(2, 0)
		if IsV128Type(slot.valType) {
			em.EmitInstr(MakeV128Load(4, slot.offset))
		} else {
			op, align := ValTypeLoadOp(slot.valType)
			em.EmitInstr(wasm.Instruction{Opcode: op, Imm: wasm.MemoryImm{Align: align, Offset: uint64(slot.offset)}})
		}
		em.LocalSet(slot.localIdx)
	}

	em.End()
}

func (ft *FunctionTransformer) emitSavePath(em *codegen.Emitter, localStackPtr, localCallIndexSave uint32, plan *framePlan) {
	em.StateCheck(ft.globals.StateGlobal, StateUnwinding).If(codegen.BlockVoid)

	// Load base pointer into local once
	em.GlobalGet(ft.globals.DataGlobal).I32Load(2, 0).LocalSet(localStackPtr)

	// Bounds check BEFORE writing: verify new_stack_ptr <= stack_end
	em.LocalGet(localStackPtr).
		I32Const(int32(plan.frameSize)).
		I32Add().
		GlobalGet(ft.globals.DataGlobal).I32Load(2, 4). // stack_end
		I32GtU().                                       // (stack_ptr + frameSize) > stack_end
		If(codegen.BlockVoid).Unreachable().End()

	// Save call index at offset 0
	em.LocalGet(localStackPtr).LocalGet(localCallIndexSave).I32Store(2, 0)

	// Save all locals at computed offsets
	for _, slot := range plan.slots {
		em.LocalGet(localStackPtr).LocalGet(slot.localIdx)
		if IsV128Type(slot.valType) {
			em.EmitInstr(MakeV128Store(4, slot.offset))
		} else {
			op, align := ValTypeStoreOp(slot.valType)
			em.EmitInstr(wasm.Instruction{Opcode: op, Imm: wasm.MemoryImm{Align: align, Offset: uint64(slot.offset)}})
		}
	}

	// Update stack_ptr once with total size
	em.GlobalGet(ft.globals.DataGlobal).
		LocalGet(localStackPtr).
		I32Const(int32(plan.frameSize)).
		I32Add().
		I32Store(2, 0)

	em.End()
}

// needsSingleStackValue returns true if the opcode consumes exactly one value from stack
// for control flow purposes (condition check).
func needsSingleStackValue(op byte) bool {
	return op == wasm.OpIf
}

// isReferenceBranch identifies reference branches pending typed transfer support.
// These must not execute during rewind.
func isReferenceBranch(op byte) bool {
	switch op {
	case wasm.OpBrOnNull, wasm.OpBrOnNonNull:
		return true
	}
	return false
}

type closedRegionFrame struct {
	startHeight int
	opcode      byte
	unreachable bool
}

func matchControlEnd(instrs []wasm.Instruction, start int) (int, bool) {
	depth := 0
	for i := start; i < len(instrs); i++ {
		switch instrs[i].Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpTry, wasm.OpTryTable:
			depth++
		case wasm.OpEnd:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func popHeight(height *int, n int) bool {
	if *height < n {
		return false
	}
	*height -= n
	return true
}

func (ft *FunctionTransformer) voidControlType(instr wasm.Instruction) bool {
	switch imm := instr.Imm.(type) {
	case wasm.BlockImm:
		return ft.voidBlockType(imm.Type)
	case wasm.TryTableImm:
		return ft.voidBlockType(imm.BlockType)
	default:
		return false
	}
}

func (ft *FunctionTransformer) voidBlockType(blockType int32) bool {
	switch blockType {
	case wasm.BlockTypeVoid:
		return true
	default:
		if blockType >= 0 && ft.module != nil && int(blockType) < len(ft.module.Types) {
			sig := ft.module.Types[blockType]
			return len(sig.Params) == 0 && len(sig.Results) == 0
		}
		return false
	}
}

func (ft *FunctionTransformer) applyCallHeight(height *int, instr wasm.Instruction) bool {
	call, handled, err := ft.calls.Resolve(instr)
	if err != nil || !handled {
		return false
	}
	pops := call.ParamCount()
	if call.HasTargetOperand() {
		pops++
	}
	if !popHeight(height, pops) {
		return false
	}
	*height += call.ResultCount()
	return true
}

func (ft *FunctionTransformer) applyClosedRegionHeight(height *int, instr wasm.Instruction) bool {
	switch instr.Opcode {
	case wasm.OpNop:
		return true
	case wasm.OpLocalGet, wasm.OpGlobalGet, wasm.OpRefNull, wasm.OpRefFunc, wasm.OpMemorySize:
		*height++
		return true
	case wasm.OpLocalSet, wasm.OpGlobalSet:
		return popHeight(height, 1)
	case wasm.OpLocalTee, wasm.OpRefAsNonNull:
		return *height >= 1
	case wasm.OpSelect, wasm.OpSelectType:
		if !popHeight(height, 3) {
			return false
		}
		*height++
		return true
	case wasm.OpCall:
		return ft.applyCallHeight(height, instr)
	}
	if effect := GetStackEffectFromRegistry(ft.registry, instr.Opcode, instr, ft.module); effect != nil {
		if !popHeight(height, effect.Pops) {
			return false
		}
		*height += len(effect.Pushes)
		return true
	}
	return false
}

// closedVoidRegionSpans maps maximal closed block/loop starts to their end.
// An eligible region has zero params/results on every construct, with
// balanced stack and only in-region branches. One normal-state guard encloses
// the original instructions, so guest branch depths stay unchanged.
func (ft *FunctionTransformer) closedVoidRegionSpans(instrs []wasm.Instruction, excludedActions map[int]bool) map[int]int {
	spans := make(map[int]int)
	for i := 0; i < len(instrs); i++ {
		if instrs[i].Opcode != wasm.OpLoop && instrs[i].Opcode != wasm.OpBlock {
			continue
		}
		end, ok := matchControlEnd(instrs, i)
		if !ok {
			continue
		}
		if ft.closedVoidRegionEligible(instrs, i, end, excludedActions) {
			spans[i] = end
			i = end
		}
	}
	return spans
}

func (ft *FunctionTransformer) closedVoidRegionEligible(instrs []wasm.Instruction, start, end int, excludedActions map[int]bool) bool {
	if start < 0 || end >= len(instrs) || start >= end {
		return false
	}
	if (instrs[start].Opcode != wasm.OpLoop && instrs[start].Opcode != wasm.OpBlock) || instrs[end].Opcode != wasm.OpEnd {
		return false
	}
	if !ft.voidControlType(instrs[start]) {
		return false
	}

	height := 0
	var frames []closedRegionFrame

	for i := start; i <= end; i++ {
		instr := instrs[i]
		if excludedActions[i] {
			return false
		}
		switch instr.Opcode {
		case wasm.OpCallIndirect, wasm.OpCallRef,
			wasm.OpReturn, wasm.OpReturnCall, wasm.OpReturnCallIndirect, wasm.OpReturnCallRef,
			wasm.OpThrow, wasm.OpThrowRef, wasm.OpRethrow,
			wasm.OpTry, wasm.OpCatch, wasm.OpCatchAll, wasm.OpDelegate, wasm.OpTryTable,
			wasm.OpBrOnNull, wasm.OpBrOnNonNull:
			return false
		}

		unreachable := len(frames) > 0 && frames[len(frames)-1].unreachable
		if unreachable {
			switch instr.Opcode {
			case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
				if !ft.voidControlType(instr) {
					return false
				}
				frames = append(frames, closedRegionFrame{opcode: instr.Opcode, startHeight: height, unreachable: true})
			case wasm.OpElse:
				if len(frames) == 0 || frames[len(frames)-1].opcode != wasm.OpIf {
					return false
				}
				frames[len(frames)-1].unreachable = false
				height = frames[len(frames)-1].startHeight
			case wasm.OpEnd:
				if len(frames) == 0 {
					return false
				}
				height = frames[len(frames)-1].startHeight
				frames = frames[:len(frames)-1]
				if i == end {
					return len(frames) == 0 && height == 0
				}
			}
			continue
		}

		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpLoop:
			if !ft.voidControlType(instr) {
				return false
			}
			frames = append(frames, closedRegionFrame{opcode: instr.Opcode, startHeight: height})
		case wasm.OpIf:
			if !ft.voidControlType(instr) {
				return false
			}
			if !popHeight(&height, 1) {
				return false
			}
			frames = append(frames, closedRegionFrame{opcode: wasm.OpIf, startHeight: height})
		case wasm.OpElse:
			if len(frames) == 0 || frames[len(frames)-1].opcode != wasm.OpIf {
				return false
			}
			if height != frames[len(frames)-1].startHeight {
				return false
			}
		case wasm.OpEnd:
			if len(frames) == 0 {
				return false
			}
			if height != frames[len(frames)-1].startHeight {
				return false
			}
			frames = frames[:len(frames)-1]
			if i == end {
				return len(frames) == 0 && height == 0
			}
		case wasm.OpBr:
			imm, ok := instr.Imm.(wasm.BranchImm)
			if !ok || int(imm.LabelIdx) >= len(frames) {
				return false
			}
			target := frames[len(frames)-1-int(imm.LabelIdx)]
			if height != target.startHeight {
				return false
			}
			frames[len(frames)-1].unreachable = true
		case wasm.OpBrIf:
			imm, ok := instr.Imm.(wasm.BranchImm)
			if !ok || int(imm.LabelIdx) >= len(frames) {
				return false
			}
			if !popHeight(&height, 1) {
				return false
			}
			target := frames[len(frames)-1-int(imm.LabelIdx)]
			if height != target.startHeight {
				return false
			}
		case wasm.OpBrTable:
			imm, ok := instr.Imm.(wasm.BrTableImm)
			if !ok {
				return false
			}
			if !popHeight(&height, 1) {
				return false
			}
			if int(imm.Default) >= len(frames) {
				return false
			}
			if height != frames[len(frames)-1-int(imm.Default)].startHeight {
				return false
			}
			for _, label := range imm.Labels {
				if int(label) >= len(frames) {
					return false
				}
				if height != frames[len(frames)-1-int(label)].startHeight {
					return false
				}
			}
			frames[len(frames)-1].unreachable = true
		case wasm.OpUnreachable:
			if len(frames) == 0 {
				return false
			}
			frames[len(frames)-1].unreachable = true
		default:
			if !ft.applyClosedRegionHeight(&height, instr) {
				return false
			}
		}
	}
	return false
}

func (ft *FunctionTransformer) emitReferenceBranch(ctx *handler.Context, instr wasm.Instruction) error {
	switch instr.Opcode {
	case wasm.OpBrOnNull:
		imm := instr.Imm.(wasm.BranchImm)
		var refLocal semantics.Operand
		refType := wasm.ValFuncRef
		if ctx.Stack.Len() > 0 {
			entry := ctx.Stack.Pop()
			refLocal = entry
			refType = entry.Type()
		}
		tmp := ctx.AllocTemp(refType)
		ctx.Emit.StateCheck(ft.globals.StateGlobal, StateNormal).If(codegen.BlockVoid)
		ctx.Emit.Operand(refLocal)
		ctx.Emit.EmitInstr(wasm.Instruction{
			Opcode: wasm.OpBrOnNull,
			Imm:    wasm.BranchImm{LabelIdx: imm.LabelIdx + 1},
		})
		ctx.Emit.LocalSet(tmp)
		ctx.Emit.End()
		ctx.Stack.Push(tmp, refType)
	case wasm.OpBrOnNonNull:
		imm := instr.Imm.(wasm.BranchImm)
		var refLocal semantics.Operand
		if ctx.Stack.Len() > 0 {
			refLocal = ctx.Stack.Pop()
		}
		ctx.Emit.StateCheck(ft.globals.StateGlobal, StateNormal).If(codegen.BlockVoid)
		ctx.Emit.Operand(refLocal)
		ctx.Emit.EmitInstr(wasm.Instruction{
			Opcode: wasm.OpBrOnNonNull,
			Imm:    wasm.BranchImm{LabelIdx: imm.LabelIdx + 1},
		})
		ctx.Emit.End()
	}
	return nil
}

// emitUnconditionalBranch emits guest branch instructions directly without
// state checks. This is sound for code following the last async call site, where execution
// is guaranteed to be in StateNormal (rewind never reaches past the resumed call site).
func (ft *FunctionTransformer) emitUnconditionalReferenceBranch(ctx *handler.Context, instr wasm.Instruction) error {
	switch instr.Opcode {
	case wasm.OpBrOnNull:
		imm := instr.Imm.(wasm.BranchImm)
		var refLocal semantics.Operand
		refType := wasm.ValFuncRef
		if ctx.Stack.Len() > 0 {
			entry := ctx.Stack.Pop()
			refLocal = entry
			refType = entry.Type()
		}
		tmp := ctx.AllocTemp(refType)
		ctx.Emit.Operand(refLocal)
		ctx.Emit.EmitInstr(wasm.Instruction{
			Opcode: wasm.OpBrOnNull,
			Imm:    wasm.BranchImm{LabelIdx: imm.LabelIdx},
		})
		ctx.Emit.LocalSet(tmp)
		ctx.Stack.Push(tmp, refType)
	case wasm.OpBrOnNonNull:
		imm := instr.Imm.(wasm.BranchImm)
		var refLocal semantics.Operand
		if ctx.Stack.Len() > 0 {
			refLocal = ctx.Stack.Pop()
		}
		ctx.Emit.Operand(refLocal)
		ctx.Emit.EmitInstr(wasm.Instruction{
			Opcode: wasm.OpBrOnNonNull,
			Imm:    wasm.BranchImm{LabelIdx: imm.LabelIdx},
		})
	}
	return nil
}

// emitSingleInstruction emits a single non-async instruction, preserving control flow.
func (ft *FunctionTransformer) emitSingleInstruction(ctx *handler.Context, instr wasm.Instruction) error {
	// Handle non-async calls
	if instr.Opcode == wasm.OpCall || instr.Opcode == wasm.OpCallIndirect || instr.Opcode == wasm.OpCallRef {
		return ft.emitNonAsyncCall(ctx, instr)
	}

	if instr.Opcode == wasm.OpBr || instr.Opcode == wasm.OpBrIf || instr.Opcode == wasm.OpBrTable {
		return fmt.Errorf("asyncify: source branch requires a typed action")
	}
	if instr.Opcode == wasm.OpReturn {
		return fmt.Errorf("asyncify: source return requires a typed action")
	}

	// For control flow that needs single stack value, reload from simulated stack
	if needsSingleStackValue(instr.Opcode) && ctx.Stack.Len() > 0 {
		entry := ctx.Stack.Pop()
		ctx.Emit.Operand(entry)
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

// emitNonAsyncCall handles non-async calls by popping params from simulated stack.
func (ft *FunctionTransformer) emitNonAsyncCall(ctx *handler.Context, instr wasm.Instruction) error {
	call, handled, err := ft.calls.Resolve(instr)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("asyncify: non-call opcode %#x in call emitter", instr.Opcode)
	}
	// Pop table index for call_indirect or func ref for call_ref
	var extraOperandLocal semantics.Operand
	if call.HasTargetOperand() && ctx.Stack.Len() > 0 {
		extraOperandLocal = ctx.Stack.Pop()
	}

	// Pop params from simulated stack (reverse order - last param is on top)
	var paramLocals []semantics.Operand

	for i := 0; i < call.ParamCount() && ctx.Stack.Len() > 0; i++ {
		paramLocals = append(paramLocals, ctx.Stack.Pop())
	}
	// Reverse to get correct order
	for i, j := 0, len(paramLocals)-1; i < j; i, j = i+1, j-1 {
		paramLocals[i], paramLocals[j] = paramLocals[j], paramLocals[i]
	}

	// Push params onto real stack
	for _, local := range paramLocals {
		ctx.Emit.Operand(local)
	}

	// Push table index for call_indirect or func ref for call_ref
	if call.HasTargetOperand() {
		ctx.Emit.Operand(extraOperandLocal)
	}

	// Emit the call
	ctx.Emit.EmitInstr(instr)

	// Push results onto simulated stack
	if call.ResultCount() > 0 {
		resLocals := make([]uint32, call.ResultCount())
		for i := range call.ResultCount() {
			rt := call.ResultType(i)
			resLocals[i] = ctx.Locals.Alloc(rt)
		}
		for i := call.ResultCount() - 1; i >= 0; i-- {
			ctx.Emit.LocalSet(resLocals[i])
		}
		for i := range call.ResultCount() {
			rt := call.ResultType(i)
			ctx.Stack.Push(resLocals[i], rt)
		}
	}
	return nil
}

func (ft *FunctionTransformer) emitAsyncCallSite(
	ctx *handler.Context,
	instr *wasm.Instruction,
	site CallSite,
	callSiteIdx int,
	localCallIndexRewind uint32,
	funcType *wasm.FuncType,
	controlDepth int,
) {
	em := ctx.Emit
	stack := ctx.Stack

	// For call_indirect/call_ref, there's an extra operand (table index / func ref) not in the type
	var extraOperandLocal semantics.Operand
	if site.Call.HasTargetOperand() {
		extraOperandLocal = stack.Pop()
	}

	var paramLocals []semantics.Operand

	for range site.Call.ParamCount() {
		paramLocals = append(paramLocals, stack.Pop())
	}
	for j, k := 0, len(paramLocals)-1; j < k; j, k = j+1, k-1 {
		paramLocals[j], paramLocals[k] = paramLocals[k], paramLocals[j]
	}

	// Consume the exact result definitions planned by operand simulation.
	// The plan prevents these writes from aliasing still-owned input operands.
	results := make([]uint32, site.Call.ResultCount())
	for i := range results {
		results[i] = ctx.AllocTemp(site.Call.ResultType(i))
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
		em.Operand(local)
	}
	// For call_indirect/call_ref, push the extra operand after params
	if site.Call.HasTargetOperand() {
		em.Operand(extraOperandLocal)
	}
	em.EmitInstr(*instr)

	// Results are written only when the call actually executes. A result is
	// saved only if it is an operand of a later suspended continuation.
	if site.Call.ResultCount() > 0 {
		// Store results in reverse order (last result is on top of stack)
		for i := site.Call.ResultCount() - 1; i >= 0; i-- {
			em.LocalSet(results[i])
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
	if len(results) > 0 {
		for i, resultLocal := range results {
			stack.Push(resultLocal, site.Call.ResultType(i))
		}
	}
}
