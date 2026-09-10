package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// ReturnOperand moves one declared function result from an explicit return's
// source stack position to the corresponding implicit function result port.
// Value zero is a polymorphic unknown only when Present is true.
type ReturnOperand struct {
	port      ValueID
	value     ValueID
	valueType wasm.ValType
	present   bool
}

func (o ReturnOperand) Port() ValueID      { return o.port }
func (o ReturnOperand) Value() ValueID     { return o.value }
func (o ReturnOperand) Present() bool      { return o.present }
func (o ReturnOperand) Type() wasm.ValType { return o.valueType }

// ReturnPrefixOperand is an enclosing validation operand. It survives lexical
// validation after return; it is never an additional runtime return value.
type ReturnPrefixOperand struct {
	value     ValueID
	valueType wasm.ValType
}

func (o ReturnPrefixOperand) Value() ValueID     { return o.value }
func (o ReturnPrefixOperand) Type() wasm.ValType { return o.valueType }

// ReturnOperation is the source-owned semantic operation for one original
// return instruction. It is not a generated scope transfer: it terminates the
// current function through its implicit target label zero.
type ReturnOperation struct {
	owner               *InstrNode
	operands            []ReturnOperand
	prefix              []ReturnPrefixOperand
	target              LabelID
	mode                TransferMode
	validationReachable bool
}

func (r ReturnOperation) PrefixCount() int { return len(r.prefix) }
func (r ReturnOperation) PrefixOperand(index int) (ReturnPrefixOperand, bool) {
	if index < 0 || index >= len(r.prefix) {
		return ReturnPrefixOperand{}, false
	}
	return r.prefix[index], true
}

func (r ReturnOperation) Target() LabelID           { return r.target }
func (r ReturnOperation) Mode() TransferMode        { return r.mode }
func (r ReturnOperation) ValidationReachable() bool { return r.validationReachable }
func (r ReturnOperation) OperandCount() int         { return len(r.operands) }
func (r ReturnOperation) Operand(index int) (ReturnOperand, bool) {
	if index < 0 || index >= len(r.operands) {
		return ReturnOperand{}, false
	}
	return r.operands[index], true
}

func (r ReturnOperation) copy() ReturnOperation {
	r.operands = append([]ReturnOperand(nil), r.operands...)
	r.prefix = append([]ReturnPrefixOperand(nil), r.prefix...)
	return r
}

// CopyInstructions is a diagnostic projection. Runtime execution consumes the
// ReturnOperation directly, so the generic primitive path never observes it.
func (r ReturnOperation) CopyInstructions() ([]wasm.Instruction, error) {
	if err := verifyReturnOperationShape(r); err != nil {
		return nil, err
	}
	instruction, err := wasm.CloneInstruction(r.owner.Instr)
	if err != nil {
		return nil, err
	}
	return []wasm.Instruction{instruction}, nil
}

func verifyReturnOperationShape(r ReturnOperation) error {
	if r.owner == nil || r.owner.Instr.Opcode != wasm.OpReturn || r.target != 0 || r.mode != MoveValues {
		return fmt.Errorf("asyncify: incomplete source return operation")
	}
	for _, operand := range r.operands {
		if operand.port == 0 {
			return fmt.Errorf("asyncify: source return operand has no root port")
		}
		if !operand.present && operand.value != 0 {
			return fmt.Errorf("asyncify: absent source return operand has a value")
		}
	}
	return nil
}

// newSourceReturn derives an explicit return from its immutable source
// operation. It never infers operands from emitted bytes or the last opcode.
func (l *linearizer) newSourceReturn(node *InstrNode) (ReturnOperation, error) {
	if l.values == nil || node == nil || node.Instr.Opcode != wasm.OpReturn {
		return ReturnOperation{}, fmt.Errorf("asyncify: source return requires source values")
	}
	root, ok := l.values.scopes[0]
	operation, operationOK := l.values.operations[node]
	if !ok || root.kind != FunctionScope || root.owner != l.values.control.root || !operationOK || operation.HasSelector || len(operation.Outputs) != 0 || len(operation.Inputs) != len(root.results) || operation.inputStackOperands < 0 || operation.inputStackOperands > len(operation.Inputs) {
		return ReturnOperation{}, fmt.Errorf("asyncify: source return has inconsistent source operation")
	}
	result := ReturnOperation{
		owner:               node,
		target:              0,
		mode:                MoveValues,
		validationReachable: operation.ValidationReachable,
		operands:            make([]ReturnOperand, len(root.results)),
	}
	for _, value := range operation.controlPrefix {
		info, defined := l.values.ValueInfo(value)
		if value != 0 && !defined {
			return ReturnOperation{}, fmt.Errorf("asyncify: undefined control validation prefix")
		}
		result.prefix = append(result.prefix, ReturnPrefixOperand{value: value, valueType: info.Type})
	}
	for index, port := range root.results {
		info, defined := l.values.ValueInfo(port)
		present := index >= len(operation.Inputs)-operation.inputStackOperands
		value := operation.Inputs[index]
		if !defined || (!present && value != 0) || (operation.ValidationReachable && (!present || value == 0)) {
			return ReturnOperation{}, fmt.Errorf("asyncify: source return result %d is not a complete known operand", index)
		}
		result.operands[index] = ReturnOperand{port: port, value: value, valueType: info.Type, present: present}
	}
	if err := verifyReturnOperationShape(result); err != nil {
		return ReturnOperation{}, err
	}
	return result, nil
}

func (l *LoweredControl) verifySourceReturn(operation ReturnOperation, owner Node, source *InstrNode) error {
	if l.values == nil || operation.owner == nil || owner != operation.owner || source != operation.owner || source.Instr.Opcode != wasm.OpReturn {
		return fmt.Errorf("asyncify: source return has no retained source owner")
	}
	root, ok := l.values.scopes[0]
	expected, operationOK := l.values.operations[source]
	if !ok || root.kind != FunctionScope || root.owner != l.values.control.root || !operationOK || expected.HasSelector || len(expected.Outputs) != 0 || len(expected.Inputs) != len(root.results) || expected.inputStackOperands < 0 || expected.inputStackOperands > len(expected.Inputs) || operation.target != 0 || operation.mode != MoveValues || operation.validationReachable != expected.ValidationReachable || len(operation.operands) != len(root.results) || len(operation.prefix) != len(expected.controlPrefix) {
		return fmt.Errorf("asyncify: source return source operation mismatch")
	}
	for index, value := range expected.controlPrefix {
		info, defined := l.values.ValueInfo(value)
		operand := operation.prefix[index]
		if (value != 0 && !defined) || operand.value != value || operand.valueType != info.Type {
			return fmt.Errorf("asyncify: control validation prefix %d mismatch", index)
		}
	}
	for index, port := range root.results {
		operand := operation.operands[index]
		info, defined := l.values.ValueInfo(port)
		present := index >= len(expected.Inputs)-expected.inputStackOperands
		if !defined || operand.port != port || operand.value != expected.Inputs[index] || operand.present != present || operand.valueType != info.Type || (expected.ValidationReachable && (!operand.present || operand.value == 0)) {
			return fmt.Errorf("asyncify: source return operand %d does not match source operation", index)
		}
	}
	return nil
}
