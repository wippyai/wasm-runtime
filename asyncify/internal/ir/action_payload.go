package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// Generated actions have a deliberately small payload vocabulary. Cloning
// proves ownership of an immediate; it does not prove that the immediate is
// appropriate for its opcode. Source payloads retain their separate exact
// occurrence contract against the decoded source instruction.
func verifyGeneratedActionPayload(action Action) error {
	if action.kind == SourceInstruction {
		return nil
	}
	instruction := action.instruction
	valid := false
	switch instruction.Opcode {
	case wasm.OpLocalGet, wasm.OpLocalSet:
		_, valid = instruction.Imm.(wasm.LocalImm)
	case wasm.OpGlobalGet:
		_, valid = instruction.Imm.(wasm.GlobalImm)
	case wasm.OpI32Const:
		_, valid = instruction.Imm.(wasm.I32Imm)
	case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
		block, ok := instruction.Imm.(wasm.BlockImm)
		valid = ok && (action.kind != RoutingInstruction || block.Type == -64)
	default:
		// Kind/opcode validation has already limited these actions to the
		// immediate-free structure and routing operators.
		valid = instruction.Imm == nil
	}
	if !valid {
		return fmt.Errorf("lowered action immediate does not match its operation")
	}
	return nil
}

// CaptureOperandRole proves which source storage contract owns a capture
// destination. Parameter captures name their destination port; selector
// captures name the scope-owned selector cell and have no port.
type CaptureOperandRole uint8

const (
	InvalidCaptureOperand CaptureOperandRole = iota
	CaptureParameter
	CaptureSelector
)

// EntryCaptureOperand is one bottom-to-top source entry operand. Value zero is
// a present polymorphic unknown only when Present is true; it never means a
// local zero fallback.
type EntryCaptureOperand struct {
	port      ValueID
	value     ValueID
	local     uint32
	valueType wasm.ValType
	role      CaptureOperandRole
	present   bool
}

func (o EntryCaptureOperand) Value() ValueID           { return o.value }
func (o EntryCaptureOperand) Present() bool            { return o.present }
func (o EntryCaptureOperand) Type() wasm.ValType       { return o.valueType }
func (o EntryCaptureOperand) Local() uint32            { return o.local }
func (o EntryCaptureOperand) Role() CaptureOperandRole { return o.role }
func (o EntryCaptureOperand) Port() (ValueID, bool)    { return o.port, o.role == CaptureParameter }

// EntryCapture atomically owns both normal entry stores and rewind entry
// consumption for one async if. It deliberately exposes one operand list, so
// independent normal and rewind orderings cannot be constructed.
type EntryCapture struct {
	owner          *IfNode
	operands       []EntryCaptureOperand
	scope          LabelID
	stateGlobal    uint32
	stateRewinding int32
}

func (c EntryCapture) Scope() LabelID        { return c.scope }
func (c EntryCapture) StateGlobal() uint32   { return c.stateGlobal }
func (c EntryCapture) StateRewinding() int32 { return c.stateRewinding }
func (c EntryCapture) OperandCount() int     { return len(c.operands) }
func (c EntryCapture) Operand(index int) (EntryCaptureOperand, bool) {
	if index < 0 || index >= len(c.operands) {
		return EntryCaptureOperand{}, false
	}
	return c.operands[index], true
}

func (c EntryCapture) copy() EntryCapture {
	c.operands = append([]EntryCaptureOperand(nil), c.operands...)
	return c
}

// CopyInstructions expands the capture only for diagnostics and byte/liveness
// projection. Engine execution consumes EntryCapture directly.
func (c EntryCapture) CopyInstructions() ([]wasm.Instruction, error) {
	if err := verifyEntryCaptureShape(c); err != nil {
		return nil, err
	}
	selector := c.operands[len(c.operands)-1]
	params := c.operands[:len(c.operands)-1]
	var instructions []wasm.Instruction
	appendGuard := func(operands []EntryCaptureOperand) {
		if len(operands) == 0 {
			return
		}
		instructions = append(instructions,
			wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: c.stateGlobal}, Synthetic: true},
			wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: c.stateRewinding}, Synthetic: true},
			wasm.Instruction{Opcode: wasm.OpI32Eq, Synthetic: true},
			wasm.Instruction{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}, Synthetic: true},
		)
		for range operands {
			instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpDrop, Synthetic: true})
		}
		instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpElse, Synthetic: true})
		for index := len(operands) - 1; index >= 0; index-- {
			if operands[index].value == 0 {
				// A present polymorphic input has a validation-stack position but
				// no concrete runtime definition to materialize in a carrier.
				instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpDrop, Synthetic: true})
				continue
			}
			instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: operands[index].local}, Synthetic: true})
		}
		instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpEnd, Synthetic: true})
	}
	if selector.present {
		appendGuard([]EntryCaptureOperand{selector})
	}
	// Validation permits only a present suffix. Omitting absent entries is not
	// an optimization: no runtime operand exists to drop or store.
	firstPresent := len(params)
	for index, operand := range params {
		if operand.present {
			firstPresent = index
			break
		}
	}
	appendGuard(params[firstPresent:])
	return instructions, nil
}

func verifyEntryCaptureShape(c EntryCapture) error {
	if c.owner == nil || len(c.operands) == 0 {
		return fmt.Errorf("asyncify: incomplete if entry capture")
	}
	for index, operand := range c.operands {
		selector := index == len(c.operands)-1
		if selector {
			if operand.role != CaptureSelector || operand.port != 0 || operand.valueType != wasm.ValI32 {
				return fmt.Errorf("asyncify: invalid capture selector")
			}
		} else if operand.role != CaptureParameter || operand.port == 0 {
			return fmt.Errorf("asyncify: invalid capture parameter")
		}
		if !operand.present && operand.value != 0 {
			return fmt.Errorf("asyncify: absent capture operand has a value")
		}
	}
	return nil
}
