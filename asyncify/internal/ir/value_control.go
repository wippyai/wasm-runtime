package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// Control traversal creates explicit parameter/result ports and source edges.
// It follows Wasm validation reachability, not a complete CFG reachability pass.
func (b *valueBuilder) visit(node Node) error {
	switch n := node.(type) {
	case *SeqNode:
		for _, child := range n.Children {
			if err := b.visit(child); err != nil {
				return err
			}
		}
	case *BlockNode:
		validationReachable := !b.frames[len(b.frames)-1].unreachable
		entryStackOperands := b.inputStackOperandCount(len(n.ParamTypes))
		inputs, err := b.popTypes(n.ParamTypes)
		if err != nil {
			return err
		}
		f := b.frame(n, n.label, n.ParamTypes, n.ResultTypes, n.Opcode == wasm.OpLoop)
		b.setEntryStackOperands(f.label, entryStackOperands)
		b.edge(n, ControlEntry, f, inputs, ControlParameter, MoveValues, NoArm, 0)
		b.frames = append(b.frames, f)
		b.stack = append(b.stack, f.params...)
		if err := b.visit(n.Body); err != nil {
			return err
		}
		if err := b.finish(n, f, NoArm); err != nil {
			return err
		}
		b.frames = b.frames[:len(b.frames)-1]
		b.stack = append(b.stack, f.results...)
		b.plan.operations[node] = valueOperation{Inputs: inputs, Outputs: append([]ValueID(nil), f.results...), inputStackOperands: entryStackOperands, ValidationReachable: validationReachable}
	case *IfNode:
		validationReachable := !b.frames[len(b.frames)-1].unreachable
		inputStackOperands := b.inputStackOperandCount(len(n.ParamTypes) + 1)
		condition, err := b.pop(wasm.ValI32)
		if err != nil {
			return err
		}
		entryStackOperands := b.inputStackOperandCount(len(n.ParamTypes))
		inputs, err := b.popTypes(n.ParamTypes)
		if err != nil {
			return err
		}
		f := b.frame(n, n.label, n.ParamTypes, n.ResultTypes, false)
		b.setEntryStackOperands(f.label, entryStackOperands)
		b.edge(n, ControlEntry, f, inputs, ControlParameter, MoveValues, NoArm, 0)
		b.frames = append(b.frames, f)
		for armIndex, arm := range []Node{n.Then, n.Else} {
			f.unreachable = false
			b.stack = append(b.stack[:f.base], f.params...)
			if arm != nil {
				if err := b.visit(arm); err != nil {
					return err
				}
			}
			armKind := ThenArm
			if armIndex == 1 {
				armKind = ElseArm
			}
			if err := b.finish(n, f, armKind); err != nil {
				return err
			}
		}
		b.frames = b.frames[:len(b.frames)-1]
		b.stack = append(b.stack[:f.base], f.results...)
		b.plan.operations[node] = valueOperation{Selector: condition, HasSelector: true, Inputs: append(inputs, condition), Outputs: append([]ValueID(nil), f.results...), inputStackOperands: inputStackOperands, ValidationReachable: validationReachable}
	case *InstrNode:
		if err := b.instruction(n); err != nil {
			return fmt.Errorf("asyncify: source opcode 0x%x: %w", n.Instr.Opcode, err)
		}
	default:
		return fmt.Errorf("asyncify: unknown source value node %T", node)
	}
	return nil
}
