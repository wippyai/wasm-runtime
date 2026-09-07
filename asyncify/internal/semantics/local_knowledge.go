package semantics

import "github.com/wippyai/wasm-runtime/wasm"

// KnowledgeEffect describes whether a single instruction preserves knowledge
// of guest local cells. The zero value ends the region. This is deliberately
// incomplete: adding an instruction requires an explicit effect contract.
type KnowledgeEffect uint8

const (
	ForgetLocals KnowledgeEffect = iota
	PreserveLocals
)

// LocalKnowledgeEffect certifies only this initial straight-line vocabulary.
// Local writes are separately invalidated using ResolveLocal's AssignLocal
// effect. Routing actions and every unlisted opcode end the region, including
// calls, control joins, prefixed instructions and reference operations.
func LocalKnowledgeEffect(instr wasm.Instruction, domain ExecutionDomain) KnowledgeEffect {
	if domain != GuestExecution {
		return ForgetLocals
	}
	switch instr.Opcode {
	case wasm.OpLocalGet, wasm.OpLocalSet, wasm.OpLocalTee,
		wasm.OpI32Const, wasm.OpI64Const, wasm.OpF32Const, wasm.OpF64Const,
		wasm.OpI32Add, wasm.OpI32Sub, wasm.OpI32Mul, wasm.OpI32Eqz,
		wasm.OpI64Add, wasm.OpI64Sub, wasm.OpI64Mul, wasm.OpI64Eqz,
		wasm.OpF32Add, wasm.OpF32Sub, wasm.OpF32Mul,
		wasm.OpF64Add, wasm.OpF64Sub, wasm.OpF64Mul:
		return PreserveLocals
	default:
		return ForgetLocals
	}
}

// LocalKnowledge owns only cell-to-value facts, never storage or references.
// A lookup is not a proof that the value remains resident in its former slot.
type LocalKnowledge struct {
	cells   map[uint32]Value
	enabled bool
}

func (k *LocalKnowledge) Observe(effect KnowledgeEffect) {
	k.enabled = effect == PreserveLocals
	if !k.enabled {
		clear(k.cells)
	}
}

func (k *LocalKnowledge) Lookup(source uint32) (Value, bool) {
	value, ok := k.cells[source]
	return value, ok && k.enabled
}

func (k *LocalKnowledge) Record(source uint32, value Value) {
	if !k.enabled {
		return
	}
	if k.cells == nil {
		k.cells = make(map[uint32]Value)
	}
	k.cells[source] = value
}

func (k *LocalKnowledge) Invalidate(source uint32) { delete(k.cells, source) }
