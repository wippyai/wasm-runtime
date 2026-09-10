package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TrapPrefixOperand is one enclosing validation-stack value that survives
// truncating the active frame for an unreachable instruction.
type TrapPrefixOperand struct {
	value     ValueID
	valueType wasm.ValType
}

func (o TrapPrefixOperand) Value() ValueID     { return o.value }
func (o TrapPrefixOperand) Type() wasm.ValType { return o.valueType }

// TrapOperation owns an original unreachable instruction. Its prefix is part
// of execution semantics: before emitting unreachable, the runtime restores
// that exact enclosing-frame stack prefix.
type TrapOperation struct {
	owner               *InstrNode
	prefix              []TrapPrefixOperand
	validationReachable bool
}

func (t TrapOperation) PrefixCount() int { return len(t.prefix) }
func (t TrapOperation) PrefixOperand(index int) (TrapPrefixOperand, bool) {
	if index < 0 || index >= len(t.prefix) {
		return TrapPrefixOperand{}, false
	}
	return t.prefix[index], true
}
func (t TrapOperation) ValidationReachable() bool { return t.validationReachable }

func (t TrapOperation) copy() TrapOperation {
	t.prefix = append([]TrapPrefixOperand(nil), t.prefix...)
	return t
}

func emptyTrapOperation(t TrapOperation) bool {
	return t.owner == nil && len(t.prefix) == 0 && !t.validationReachable
}

// CopyInstructions is a diagnostic projection. Runtime execution consumes the
// semantic operation so it can restore the active control-frame prefix first.
func (t TrapOperation) CopyInstructions() ([]wasm.Instruction, error) {
	if err := verifyTrapOperationShape(t); err != nil {
		return nil, err
	}
	instruction, err := wasm.CloneInstruction(t.owner.Instr)
	if err != nil {
		return nil, err
	}
	return []wasm.Instruction{instruction}, nil
}

func verifyTrapOperationShape(t TrapOperation) error {
	if t.owner == nil || t.owner.Instr.Opcode != wasm.OpUnreachable {
		return fmt.Errorf("asyncify: incomplete source trap operation")
	}
	return nil
}

// newSourceTrap derives the source-owned trap operation from the immutable
// value plan. It does not reconstruct stack state from emitted instructions.
func (l *linearizer) newSourceTrap(node *InstrNode) (TrapOperation, error) {
	if l.values == nil || node == nil || node.Instr.Opcode != wasm.OpUnreachable {
		return TrapOperation{}, fmt.Errorf("asyncify: source trap requires source values")
	}
	op, ok := l.values.operations[node]
	if !ok || op.control != controlTraps || op.OpaqueControl || len(op.Inputs) != 0 || len(op.Outputs) != 0 || len(op.branchTargets) != 0 || op.inputStackOperands != 0 || op.HasSelector || op.Selector != 0 {
		return TrapOperation{}, fmt.Errorf("asyncify: source trap has inconsistent source operation")
	}
	result := TrapOperation{owner: node, validationReachable: op.ValidationReachable}
	for _, value := range op.controlPrefix {
		info, defined := l.values.ValueInfo(value)
		if value != 0 && !defined {
			return TrapOperation{}, fmt.Errorf("asyncify: undefined trap validation prefix")
		}
		result.prefix = append(result.prefix, TrapPrefixOperand{value: value, valueType: info.Type})
	}
	if err := verifyTrapOperationShape(result); err != nil {
		return TrapOperation{}, err
	}
	return result, nil
}

func (l *LoweredControl) verifySourceTrap(operation TrapOperation, owner Node, source *InstrNode) error {
	if l.values == nil || operation.owner == nil || owner != operation.owner || source != operation.owner || source.Instr.Opcode != wasm.OpUnreachable {
		return fmt.Errorf("asyncify: source trap has no retained source owner")
	}
	if err := verifyTrapOperationShape(operation); err != nil {
		return err
	}
	expected, ok := l.values.operations[source]
	if !ok || expected.control != controlTraps || expected.OpaqueControl || len(expected.Inputs) != 0 || len(expected.Outputs) != 0 || len(expected.branchTargets) != 0 || expected.inputStackOperands != 0 || expected.HasSelector || expected.Selector != 0 || operation.validationReachable != expected.ValidationReachable || len(operation.prefix) != len(expected.controlPrefix) {
		return fmt.Errorf("asyncify: source trap operation mismatch")
	}
	for index, value := range expected.controlPrefix {
		info, defined := l.values.ValueInfo(value)
		operand := operation.prefix[index]
		if (value != 0 && !defined) || operand.value != value || operand.valueType != info.Type {
			return fmt.Errorf("asyncify: source trap prefix %d mismatch", index)
		}
	}
	return nil
}
