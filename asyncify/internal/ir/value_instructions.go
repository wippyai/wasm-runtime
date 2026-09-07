package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func (b *valueBuilder) instruction(n *InstrNode) error {
	op := valueOperation{ValidationReachable: !b.frames[len(b.frames)-1].unreachable, control: controlFallsThrough}
	switch n.Instr.Opcode {
	case wasm.OpNop:
	case wasm.OpUnreachable:
		op.control = controlTraps
		// Preserve the enclosing validation stack before unreachable truncates
		// the active frame. The runtime trap action restores exactly this prefix
		// before it emits the source trap.
		op.controlPrefix = append([]ValueID(nil), b.stack[:b.frames[len(b.frames)-1].base]...)
		b.unreachable()
	case wasm.OpDrop:
		op.inputStackOperands = b.inputStackOperandCount(1)
		id, err := b.pop(0)
		if err != nil {
			return err
		}
		op.Inputs = []ValueID{id}
	case wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable, wasm.OpReturn:
		targets := n.branchTargets
		if n.Instr.Opcode == wasm.OpBr || n.Instr.Opcode == wasm.OpBrIf || n.Instr.Opcode == wasm.OpBrTable || n.Instr.Opcode == wasm.OpReturn {
			op.controlPrefix = append([]ValueID(nil), b.stack[:b.frames[len(b.frames)-1].base]...)
		}
		switch n.Instr.Opcode {
		case wasm.OpReturn:
			targets = []LabelID{0}
			op.control = controlReturn
		case wasm.OpBrIf:
			op.control = controlConditionalBranch
		default:
			op.control = controlBranch
		}
		if len(targets) == 0 {
			return fmt.Errorf("branch without source targets")
		}
		op.branchTargets = append([]LabelID(nil), targets...)
		first, err := b.target(targets[0])
		if err != nil {
			return err
		}
		types := b.types(b.targetValues(first))
		hasSelector := n.Instr.Opcode == wasm.OpBrIf || n.Instr.Opcode == wasm.OpBrTable
		inputWidth := len(types)
		if hasSelector {
			inputWidth++
		}
		op.inputStackOperands = b.inputStackOperandCount(inputWidth)
		var selector ValueID
		if hasSelector {
			selector, err = b.pop(wasm.ValI32)
			if err != nil {
				return err
			}
		}
		op.Selector = selector
		op.HasSelector = hasSelector
		values, err := b.popTypes(types)
		if err != nil {
			return err
		}
		for ordinal, target := range targets {
			f, err := b.target(target)
			if err != nil {
				return err
			}
			expected := b.types(b.targetValues(f))
			if len(expected) != len(types) {
				return fmt.Errorf("branch target arity mismatch")
			}
			for i, vt := range expected {
				if vt != types[i] {
					return fmt.Errorf("branch target type mismatch")
				}
			}
			port := ControlResult
			if f.loop {
				port = ControlParameter
			}
			mode := MoveValues
			if n.Instr.Opcode == wasm.OpBrIf {
				mode = CopyValues
			}
			b.edge(n, ControlBranch, f, values, port, mode, NoArm, ordinal)
		}
		op.Inputs = append([]ValueID(nil), values...)
		if n.Instr.Opcode == wasm.OpBrIf {
			op.Inputs = append(op.Inputs, selector)
			for i, value := range values {
				// Validation pushes the label's declared types on fallthrough,
				// even if the consumed value was polymorphic in dead code.
				if value == 0 {
					value = b.defineValidation(n, i, types[i])
				}
				op.Outputs = append(op.Outputs, value)
			}
			b.stack = append(b.stack, op.Outputs...)
		} else {
			if n.Instr.Opcode == wasm.OpBrTable {
				op.Inputs = append(op.Inputs, selector)
			}
			b.unreachable()
		}
	case wasm.OpSelect, wasm.OpSelectType:
		op.inputStackOperands = b.inputStackOperandCount(3)
		condition, err := b.pop(wasm.ValI32)
		if err != nil {
			return err
		}
		var expected wasm.ValType
		if n.Instr.Opcode == wasm.OpSelectType {
			imm, ok := n.Instr.Imm.(wasm.SelectTypeImm)
			if !ok || len(imm.Types) != 1 {
				return fmt.Errorf("typed select must name one result type")
			}
			expected = imm.Types[0]
		}
		right, err := b.pop(expected)
		if err != nil {
			return err
		}
		if expected == 0 {
			expected = b.valueType(right)
		}
		left, err := b.pop(expected)
		if err != nil {
			return err
		}
		if expected == 0 {
			expected = b.valueType(left)
		}
		if n.Instr.Opcode == wasm.OpSelect && expected != 0 && expected != wasm.ValI32 && expected != wasm.ValI64 && expected != wasm.ValF32 && expected != wasm.ValF64 && expected != wasm.ValV128 {
			return fmt.Errorf("untyped select requires numeric or vector operands")
		}
		output := b.define(n, InstructionResult, 0, expected)
		op.Inputs = []ValueID{left, right, condition}
		op.Outputs = []ValueID{output}
		b.stack = append(b.stack, output)
	default:
		if n.callID != 0 {
			call := b.plan.control.calls[n.callID-1]
			if call.suspends {
				b.plan.continuations[n.callID] = append([]ValueID(nil), b.stack...)
			}
		}
		// The resolver may inspect or retain immediates. Give it independent
		// storage so it cannot mutate the source analysis owned by the plan.
		instruction, err := wasm.CloneInstruction(n.Instr)
		if err != nil {
			return err
		}
		shape, err := b.resolve(instruction)
		if err != nil {
			return err
		}
		if shape.PopCount < 0 {
			return fmt.Errorf("negative operand pop count")
		}
		if shape.OpaqueInputs && len(shape.Inputs) != 0 {
			return fmt.Errorf("opaque operand shape also specifies input types")
		}
		inputs := shape.Inputs
		if shape.OpaqueInputs {
			inputs = make([]wasm.ValType, shape.PopCount)
		}
		op.inputStackOperands = b.inputStackOperandCount(len(inputs))
		values, err := b.popTypes(inputs)
		if err != nil {
			return err
		}
		op.Inputs, op.OpaqueInputs = values, shape.OpaqueInputs
		op.OpaqueControl = shape.OpaqueControl
		b.plan.opaqueControl = b.plan.opaqueControl || shape.OpaqueControl
		if shape.AliasInput {
			if len(values) != 1 || len(shape.Results) != 1 {
				return fmt.Errorf("invalid identity value effect")
			}
			actual, expected := b.valueType(values[0]), shape.Results[0]
			if actual != 0 && expected != 0 && actual != expected {
				return fmt.Errorf("identity value changes type from %v to %v", actual, expected)
			}
			output := values[0]
			if output == 0 && expected != 0 {
				// A typed identity operation still pushes its declared type
				// when its input came from the polymorphic stack base.
				output = b.defineValidation(n, 0, expected)
			}
			op.Outputs = []ValueID{output}
		} else {
			for i, vt := range shape.Results {
				op.Outputs = append(op.Outputs, b.define(n, InstructionResult, i, vt))
			}
		}
		if literal, handled, err := semantics.ResolveLiteral(n.Instr); handled {
			if err != nil {
				return err
			}
			if len(op.Inputs) != 0 || len(op.Outputs) != 1 || b.valueType(op.Outputs[0]) != literal.Type() || shape.AliasInput || shape.OpaqueInputs || shape.OpaqueControl {
				return fmt.Errorf("source literal shape disagrees with exact value")
			}
			b.plan.literals[op.Outputs[0]] = literal
		}
		b.stack = append(b.stack, op.Outputs...)
	}
	b.plan.operations[n] = op
	return nil
}
