package handler

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// materializationVerifier owns an immutable copy of the plan and declaration
// types, plus its own cell-to-snapshot knowledge. Reuse requires both a current
// source-cell fact and the exact resident definition. It has no allocator or
// emitter access. Rejected decisions cannot update
// its definition ledger. The caller must reject the artifact if finish fails.
type materializationVerifier struct {
	err               error
	knowledge         semantics.LocalKnowledge
	plan              map[int][]semantics.Materialization
	resident          map[uint32]semantics.Materialization
	defined           map[uint64]bool
	visited           map[int]bool
	types             []wasm.ValType
	current, position int
	started           bool
	domain            semantics.ExecutionDomain
	noMaterialization bool
}

func newMaterializationVerifier(plan map[int][]semantics.Materialization, types []wasm.ValType) *materializationVerifier {
	copyPlan := make(map[int][]semantics.Materialization, len(plan))
	for i, entries := range plan {
		copyPlan[i] = append([]semantics.Materialization(nil), entries...)
	}
	return &materializationVerifier{plan: copyPlan, types: append([]wasm.ValType(nil), types...),
		resident: make(map[uint32]semantics.Materialization), defined: make(map[uint64]bool), visited: make(map[int]bool)}
}

func (v *materializationVerifier) beginInDomain(instruction int, domain semantics.ExecutionDomain, effect semantics.KnowledgeEffect) {
	if !domain.Valid() {
		v.failf("invalid execution domain %d", domain)
		return
	}
	if domain != semantics.GuestExecution {
		effect = semantics.ForgetLocals
	}
	v.knowledge.Observe(effect)
	v.domain = domain
	v.noMaterialization = false
	v.beginPosition(instruction)
}

func (v *materializationVerifier) beginPosition(instruction int) {
	v.checkConsumed()
	if v.visited[instruction] {
		v.failf("instruction %d emitted twice", instruction)
	}
	v.visited[instruction] = true
	v.current, v.position, v.started = instruction, 0, true
}

func (v *materializationVerifier) consume(vt wasm.ValType, source *uint32) (semantics.Materialization, bool) {
	if v.noMaterialization {
		v.failf("action forbids temporary materialization")
		return semantics.Materialization{}, false
	}
	entries := v.plan[v.current]
	if !v.started || v.position >= len(entries) {
		v.failf("unexpected allocation of %v", vt)
		return semantics.Materialization{}, false
	}
	entry := entries[v.position]
	v.position++
	if err := v.validate(entry, vt, source); err != nil {
		v.failf("%s", err)
		return semantics.Materialization{}, false
	}
	if !entry.Reuse {
		v.resident[entry.LocalIdx] = entry
		v.defined[entry.ID] = true
	}
	if entry.LocalRead {
		v.knowledge.Record(entry.SourceLocal, entry.Value)
	}
	return entry, true
}

func (v *materializationVerifier) validate(entry semantics.Materialization, vt wasm.ValType, source *uint32) error {
	if entry.Domain != v.domain || (entry.Domain != semantics.GuestExecution && entry.Domain != semantics.RewindRouting) {
		return fmt.Errorf("materialization has inconsistent execution domain")
	}
	if prior, ok := v.resident[entry.LocalIdx]; ok && prior.Domain != entry.Domain {
		return fmt.Errorf("local %d aliases guest and rewind routing storage", entry.LocalIdx)
	}
	if uint64(entry.LocalIdx) >= uint64(len(v.types)) || v.types[entry.LocalIdx] != vt || entry.Type != vt {
		return fmt.Errorf("local %d has invalid index or type for %v", entry.LocalIdx, vt)
	}
	if entry.LocalRead != (source != nil) || (source != nil && entry.SourceLocal != *source) || (!entry.LocalRead && entry.SourceLocal != 0) {
		return fmt.Errorf("local %d has mismatched source", entry.LocalIdx)
	}
	if entry.ID == 0 {
		return fmt.Errorf("zero value identity")
	}
	if !entry.Reuse {
		if v.defined[entry.ID] {
			return fmt.Errorf("value %d defined twice", entry.ID)
		}
		return nil
	}
	known, cellKnown := v.knowledge.Lookup(entry.SourceLocal)
	if !cellKnown || known != entry.Value {
		return fmt.Errorf("value %d no longer denotes source local %d", entry.ID, entry.SourceLocal)
	}
	prior, ok := v.resident[entry.LocalIdx]
	if !entry.LocalRead || !ok || prior.Value != entry.Value || !prior.LocalRead || prior.SourceLocal != entry.SourceLocal {
		return fmt.Errorf("value %d is not a resident snapshot of local %d", entry.ID, entry.SourceLocal)
	}
	return nil
}

func (v *materializationVerifier) finish() error {
	v.checkConsumed()
	for instruction, entries := range v.plan {
		if len(entries) > 0 && !v.visited[instruction] {
			v.failf("planned instruction %d was never emitted", instruction)
		}
	}
	return v.err
}

func (v *materializationVerifier) checkConsumed() {
	if v.started && v.position != len(v.plan[v.current]) {
		v.failf("consumed %d of %d planned allocations", v.position, len(v.plan[v.current]))
	}
}

func (v *materializationVerifier) failf(format string, args ...any) {
	if v.err == nil {
		v.err = fmt.Errorf("asyncify: allocation plan mismatch at instruction %d: %s", v.current, fmt.Sprintf(format, args...))
	}
}

func (v *materializationVerifier) beginNoMaterialization(action int) {
	v.knowledge.Observe(semantics.ForgetLocals)
	v.beginPosition(action)
	v.noMaterialization = true
	if len(v.plan[action]) != 0 {
		v.failf("action forbids planned temporary definitions")
	}
}
