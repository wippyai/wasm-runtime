package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// ScopeTransferOperand moves one declared scope result from an exit stack
// position into its assigned source result port. Value zero is a validation-only
// polymorphic operand when Present is true; it never selects a fallback local.
type ScopeTransferOperand struct {
	port      ValueID
	value     ValueID
	local     uint32
	valueType wasm.ValType
	present   bool
}

func (o ScopeTransferOperand) Value() ValueID     { return o.value }
func (o ScopeTransferOperand) Present() bool      { return o.present }
func (o ScopeTransferOperand) Type() wasm.ValType { return o.valueType }
func (o ScopeTransferOperand) Local() uint32      { return o.local }
func (o ScopeTransferOperand) Port() ValueID      { return o.port }

// ScopeTransfer owns a source scope's ordered parameter or result transfer.
// The action kind selects its source entry or exit contract. The same list
// defines virtual consumption and the diagnostic byte projection; normal
// writes cannot diverge from that source transfer contract.
type ScopeTransfer struct {
	owner    Node
	operands []ScopeTransferOperand
	scope    LabelID
	arm      ControlArm
	mode     TransferMode
}

func (t ScopeTransfer) Scope() LabelID     { return t.scope }
func (t ScopeTransfer) Arm() ControlArm    { return t.arm }
func (t ScopeTransfer) Mode() TransferMode { return t.mode }
func (t ScopeTransfer) OperandCount() int  { return len(t.operands) }
func (t ScopeTransfer) Operand(index int) (ScopeTransferOperand, bool) {
	if index < 0 || index >= len(t.operands) {
		return ScopeTransferOperand{}, false
	}
	return t.operands[index], true
}

func (t ScopeTransfer) copy() ScopeTransfer {
	t.operands = append([]ScopeTransferOperand(nil), t.operands...)
	return t
}

// CopyInstructions is a diagnostic byte projection. Execution consumes the
// transfer action directly. Known values store in reverse stack order; present
// polymorphic values consume a validation position without a typed write.
func (t ScopeTransfer) CopyInstructions() ([]wasm.Instruction, error) {
	if err := verifyScopeTransferShape(t); err != nil {
		return nil, err
	}
	var instructions []wasm.Instruction
	for index := len(t.operands) - 1; index >= 0; index-- {
		operand := t.operands[index]
		if !operand.present {
			continue
		}
		if operand.value == 0 {
			instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpDrop})
			continue
		}
		instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: operand.local}})
	}
	return instructions, nil
}

func verifyScopeTransferShape(t ScopeTransfer) error {
	if t.owner == nil || t.mode != MoveValues || len(t.operands) == 0 {
		return fmt.Errorf("asyncify: incomplete scope value transfer")
	}
	for _, operand := range t.operands {
		if operand.port == 0 {
			return fmt.Errorf("asyncify: scope transfer has no target port")
		}
		if !operand.present && operand.value != 0 {
			return fmt.Errorf("asyncify: absent scope transfer operand has a value")
		}
	}
	return nil
}
