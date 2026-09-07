// Package semantics resolves instruction behavior shared by Asyncify analysis
// and lowering. It describes values and effects, not emitted byte sequences.
package semantics

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// LocalEffects describes the value flow of a local instruction. A produced
// operand is a value snapshot, never an alias to the mutable guest local.
type LocalEffects uint8

const (
	ConsumeOperand LocalEffects = 1 << iota
	AssignLocal
	ProduceSnapshot
)

// LocalOperation is a resolved, typed instruction description. It is passed by
// value and owns no analysis slices. Consumers must not reinterpret its opcode
// independently: use its effects to determine operand and result behavior.
type LocalOperation struct {
	Index   uint32
	Type    wasm.ValType
	Effects LocalEffects
}

// ResolveLocal resolves standard local operations. The boolean distinguishes
// other instruction families from malformed local operations. Type environments
// must describe the instruction's IR revision, including generated locals.
func ResolveLocal(instr wasm.Instruction, localTypes []wasm.ValType) (LocalOperation, bool, error) {
	var effects LocalEffects
	switch instr.Opcode {
	case wasm.OpLocalGet:
		effects = ProduceSnapshot
	case wasm.OpLocalSet:
		effects = ConsumeOperand | AssignLocal
	case wasm.OpLocalTee:
		effects = ConsumeOperand | AssignLocal | ProduceSnapshot
	default:
		return LocalOperation{}, false, nil
	}
	imm, ok := instr.Imm.(wasm.LocalImm)
	if !ok {
		return LocalOperation{}, true, fmt.Errorf("asyncify: local instruction has invalid immediate %T", instr.Imm)
	}
	if uint64(imm.LocalIdx) >= uint64(len(localTypes)) {
		return LocalOperation{}, true, fmt.Errorf("asyncify: local index %d out of range", imm.LocalIdx)
	}
	return LocalOperation{Index: imm.LocalIdx, Type: localTypes[imm.LocalIdx], Effects: effects}, true, nil
}
