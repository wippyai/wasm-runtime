package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// TempAllocator manages bounded temporary-local allocation for Asyncify.
// It assigns temporary locals from pools segregated by type and execution domain,
// reusing slots whose values
// are no longer live on the operand stack or pinned in any active control-flow snapshot.
//
// Soundness Invariants:
// 1. Types and execution domains are strictly segregated. Replayed routing
// writes never alias guest temporaries restored for a later continuation.
// 2. A temporary local L is considered IN USE (refCount > 0) if:
//   - L is currently on the simulated operand stack.
//   - L is pinned in one or more active snapshots (e.g. if/else branches).
//   - L was popped during the current instruction and is held until EndInstruction
//     to prevent destination-source aliasing hazards.
//     3. Only locals with refCount == 0 are eligible for allocation/reuse.
//     4. Guest/control locals and scratch locals have indices < firstTempLocal
//     and are never reused here. Async call results are ordinary temporary definitions.
//     5. Simulation records an exact instruction-by-instruction allocation plan so that
//     emission can validate its allocation requests against simulation.
type TempAllocator struct {
	err       error
	storage   *temporaryStorage
	knowledge semantics.LocalKnowledge
	plan      map[int][]semantics.Materialization
	// Branch snapshots and deferred operand releases retain ownership.
	snapshots         [][]uint32
	releasedThisInstr []uint32
	currentInstr      int
	domain            semantics.ExecutionDomain
	noMaterialization bool
}

// NewTempAllocator creates a new allocator starting at firstTempLocal.
func NewTempAllocator(firstTempLocal uint32) *TempAllocator {
	return &TempAllocator{
		storage: newTemporaryStorage(firstTempLocal),
		plan:    make(map[int][]semantics.Materialization),
	}
}

// SetCurrentInstr marks the start of instruction i.
func (a *TempAllocator) SetCurrentInstr(i int) {
	if len(a.releasedThisInstr) != 0 {
		a.failf("instruction changed before pending operands were released")
	}
	a.currentInstr = i
}

// Alloc allocates a temporary local of the specified ValType.
// If an existing slot in the pool has refCount == 0, it is reused.
// Otherwise, a new local index is added to the pool.
func (a *TempAllocator) Alloc(vt wasm.ValType) uint32 {
	if a.noMaterialization {
		a.failf("action forbids temporary allocation")
		return 0
	}
	value := a.storage.defineInDomain(vt, a.domain)
	a.plan[a.currentInstr] = append(a.plan[a.currentInstr], semantics.Materialization{Value: value, Domain: a.domain})
	return value.LocalIdx
}

// ReleaseOnPop marks a popped local index to be released at EndInstruction.
// If the index is below firstTempLocal (guest local, scratch local, etc.), it is ignored.
func (a *TempAllocator) ReleaseOnPop(idx uint32) {
	if idx >= a.storage.firstTempLocal {
		a.releasedThisInstr = append(a.releasedThisInstr, idx)
	}
}

// EndInstruction finishes instruction processing and decrements refCounts of
// all temporaries popped during this instruction.
func (a *TempAllocator) EndInstruction() {
	for _, idx := range a.releasedThisInstr {
		a.release(idx)
	}
	a.releasedThisInstr = a.releasedThisInstr[:0]
}

// PushSnapshot pins all temporary stack entries at an OpIf boundary.
func (a *TempAllocator) PushSnapshot(entries []stackEntry) {
	snap := make([]uint32, 0, len(entries))
	for _, e := range entries {
		if idx, stored := e.LocalIndex(); stored && idx >= a.storage.firstTempLocal {
			a.retain(idx)
			snap = append(snap, idx)
		}
	}
	a.snapshots = append(a.snapshots, snap)
}

// RestoreStack adjusts refCounts when the simulated stack is restored to a snapshot (at OpElse).
func (a *TempAllocator) RestoreStack(oldStack []stackEntry, newStack []stackEntry) {
	for _, e := range oldStack {
		if idx, stored := e.LocalIndex(); stored && idx >= a.storage.firstTempLocal {
			a.release(idx)
		}
	}
	for _, e := range newStack {
		if idx, stored := e.LocalIndex(); stored && idx >= a.storage.firstTempLocal {
			a.retain(idx)
		}
	}
}

// PopSnapshot unpins snapshot entries when closing an OpIf block (at OpEnd).
func (a *TempAllocator) PopSnapshot() {
	if len(a.snapshots) == 0 {
		a.failf("snapshot stack underflow")
		return
	}
	snap := a.snapshots[len(a.snapshots)-1]
	a.snapshots = a.snapshots[:len(a.snapshots)-1]
	for _, idx := range snap {
		a.release(idx)
	}
}

// ClearStack unpins any remaining temporary entries on stack (e.g. at OpReturn).
func (a *TempAllocator) ClearStack(stack []stackEntry) {
	for _, e := range stack {
		if idx, stored := e.LocalIndex(); stored && idx >= a.storage.firstTempLocal {
			a.release(idx)
		}
	}
}

// Plan returns the instruction-indexed allocation plan.
func (a *TempAllocator) Plan() map[int][]semantics.Materialization {
	plan := make(map[int][]semantics.Materialization, len(a.plan))
	for i, entries := range a.plan {
		plan[i] = append([]semantics.Materialization(nil), entries...)
	}
	return plan
}

// AllocatedTypes returns the map of local index to ValType.
func (a *TempAllocator) AllocatedTypes() map[uint32]wasm.ValType {
	types := make(map[uint32]wasm.ValType, len(a.storage.localTypes))
	for index, vt := range a.storage.localTypes {
		types[index] = vt
	}
	return types
}

// MaxLocal returns the maximum local index + 1 allocated by this allocator.
func (a *TempAllocator) MaxLocal() uint32 {
	return a.storage.nextFreshLocal
}

// TotalAllocated returns the number of unique temporary locals allocated.
func (a *TempAllocator) TotalAllocated() int {
	return int(a.storage.nextFreshLocal - a.storage.firstTempLocal)
}

// Err reports an internal ownership inconsistency. The transform must reject
// the allocation plan instead of emitting code from inconsistent liveness.
func (a *TempAllocator) Err() error { return a.err }

func (a *TempAllocator) failf(format string, args ...any) {
	if a.err == nil {
		a.err = fmt.Errorf("asyncify: temporary ownership mismatch at instruction %d: %s", a.currentInstr, fmt.Sprintf(format, args...))
	}
}

func (a *TempAllocator) release(idx uint32) {
	if !a.storage.release(idx) {
		a.failf("release of unowned local %d", idx)
	}
}

func (a *TempAllocator) retain(idx uint32) {
	if !a.storage.retain(idx) {
		a.failf("retain of undefined local %d", idx)
	}
}

// ObserveAction consumes lowering's execution policy and the instruction's
// local knowledge effect. Bytecode annotations cannot choose storage ownership.
func (a *TempAllocator) ObserveAction(instr wasm.Instruction, domain semantics.ExecutionDomain) {
	if !domain.Valid() {
		a.failf("invalid execution domain %d", domain)
		return
	}
	a.domain = domain
	a.noMaterialization = false
	a.knowledge.Observe(semantics.LocalKnowledgeEffect(instr, domain))
}

// SnapshotLocal composes two independent proofs: the cell still denotes this
// value, and storage still contains its exact definition. Knowledge owns no slot.
func (a *TempAllocator) SnapshotLocal(source uint32, vt wasm.ValType) uint32 {
	if a.noMaterialization {
		a.failf("action forbids snapshot materialization")
		return 0
	}
	if value, ok := a.knowledge.Lookup(source); ok && value.Type == vt && a.storage.domains[value.LocalIdx] == a.domain && a.storage.retainValue(value) {
		a.plan[a.currentInstr] = append(a.plan[a.currentInstr], semantics.Materialization{Value: value, Domain: a.domain, SourceLocal: source, LocalRead: true, Reuse: true})
		return value.LocalIdx
	}
	value := a.storage.defineInDomain(vt, a.domain)
	a.plan[a.currentInstr] = append(a.plan[a.currentInstr], semantics.Materialization{Value: value, Domain: a.domain, SourceLocal: source, LocalRead: true})
	a.knowledge.Record(source, value)
	return value.LocalIdx
}

func (a *TempAllocator) InvalidateLocal(source uint32) { a.knowledge.Invalidate(source) }

// ObserveCapture declares a compound action with no temporary definitions. Its
// conditional writes end local-cell knowledge without assigning a fake domain.
func (a *TempAllocator) ObserveCapture() {
	a.noMaterialization = true
	a.knowledge.Observe(semantics.ForgetLocals)
}

// ObserveClosedRegion ends cell knowledge across a copied guest region. The
// region may write locals even though its individual instructions are skipped
// by temporary simulation and allocate no temporary definitions.
func (a *TempAllocator) ObserveClosedRegion() {
	a.noMaterialization = true
	a.knowledge.Observe(semantics.ForgetLocals)
}

// ObserveTransfer declares a source port transfer with no new temporaries.
// Its writes invalidate cell knowledge; the inputs retain storage until the
// action finishes, exactly as for primitive consuming instructions.
func (a *TempAllocator) ObserveTransfer() {
	a.noMaterialization = true
	a.knowledge.Observe(semantics.ForgetLocals)
}

// ObserveReturn owns a source control exit with no temporary definitions.
// Only the active frame suffix is discarded; enclosing validation operands remain.
func (a *TempAllocator) ObserveReturn() {
	a.noMaterialization = true
	a.knowledge.Observe(semantics.ForgetLocals)
}

// ObserveBranch owns existing source operands and selected target writes. It
// creates no storage definitions, including for validation-only fallthrough.
func (a *TempAllocator) ObserveBranch() {
	a.noMaterialization = true
	a.knowledge.Observe(semantics.ForgetLocals)
}

// ObserveTrap ends guest execution without defining temporary storage.
func (a *TempAllocator) ObserveTrap() {
	a.noMaterialization = true
	a.knowledge.Observe(semantics.ForgetLocals)
}
