package wasm

import (
	"fmt"
	"slices"
)

// CloneInstruction copies a decoded instruction, including owned storage for
// every supported pointer or slice immediate. Unknown immediate representations
// fail explicitly so extending the decoder requires defining its ownership here.
func CloneInstruction(instruction Instruction) (Instruction, error) {
	switch imm := instruction.Imm.(type) {
	case nil, BlockImm, BranchImm, CallImm, CallIndirectImm, LocalImm, GlobalImm,
		MemoryImm, MemoryIdxImm, I32Imm, I64Imm, F32Imm, F64Imm, TableImm,
		RefNullImm, RefFuncImm, GCImm, ThrowImm, CallRefImm:
		// All fields in these immediates are values.
	case BrTableImm:
		imm.Labels = slices.Clone(imm.Labels)
		instruction.Imm = imm
	case MiscImm:
		imm.Operands = slices.Clone(imm.Operands)
		instruction.Imm = imm
	case SelectTypeImm:
		imm.Types = slices.Clone(imm.Types)
		imm.ExtTypes = slices.Clone(imm.ExtTypes)
		instruction.Imm = imm
	case SIMDImm:
		imm.V128Bytes = slices.Clone(imm.V128Bytes)
		if imm.MemArg != nil {
			value := *imm.MemArg
			imm.MemArg = &value
		}
		if imm.LaneIdx != nil {
			value := *imm.LaneIdx
			imm.LaneIdx = &value
		}
		instruction.Imm = imm
	case AtomicImm:
		if imm.MemArg != nil {
			value := *imm.MemArg
			imm.MemArg = &value
		}
		instruction.Imm = imm
	case TryTableImm:
		imm.Catches = slices.Clone(imm.Catches)
		instruction.Imm = imm
	default:
		return Instruction{}, fmt.Errorf("cannot clone unknown instruction immediate %T", instruction.Imm)
	}
	return instruction, nil
}
