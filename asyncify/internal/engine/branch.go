package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// branchOperands binds the present source suffix once. Missing or unknown
// polymorphic inputs are validation-only, never fabricated local reads.
func branchOperands(op ir.BranchOperation, stack []stackEntry) ([]stackEntry, []stackEntry, error) {
	if _, err := retainedPrefix(op.PrefixCount(), op.PrefixOperand, stack); err != nil {
		return nil, nil, err
	}
	present, err := transferInputs(op.InputCount(), op.Input, stack[op.PrefixCount():])
	if err != nil {
		return nil, nil, err
	}
	inputs := make([]stackEntry, op.InputCount())
	index := 0
	for i := range inputs {
		input, ok := op.Input(i)
		if !ok {
			return nil, nil, fmt.Errorf("missing branch input %d", i)
		}
		if input.Present() {
			inputs[i] = present[index]
			index++
		}
		if !input.Present() || input.Value() == 0 {
			if op.ValidationReachable() {
				return nil, nil, fmt.Errorf("reachable branch input %d has no value", i)
			}
			inputs[i] = semantics.ValidationOperand(input.Type())
		}
	}
	if op.Opcode() != wasm.OpBrIf {
		return inputs, stack[:op.PrefixCount()], nil
	}
	if op.FallthroughCount()+1 != len(inputs) {
		return nil, nil, fmt.Errorf("branch fallthrough arity mismatch")
	}
	after := append([]stackEntry(nil), stack[:len(stack)-len(present)]...)
	for i := 0; i < op.FallthroughCount(); i++ {
		output, ok := op.Fallthrough(i)
		if !ok || output.Type() != inputs[i].Type() {
			return nil, nil, fmt.Errorf("branch fallthrough %d type mismatch", i)
		}
		after = append(after, inputs[i])
	}
	return inputs, after, nil
}

// emitBranchTarget performs a parallel transfer through the Wasm operand
// stack: every input is read before any target cell is overwritten. This does
// not depend on the allocator keeping source snapshots and target cells apart.
func emitBranchTarget(ctx *handler.Context, target ir.BranchTarget, inputs []stackEntry, extraDepth uint32) error {
	if target.PortCount() > len(inputs) {
		return fmt.Errorf("branch target exceeds inputs")
	}
	for i := 0; i < target.PortCount(); i++ {
		port, ok := target.Port(i)
		if !ok || port.Type() != inputs[i].Type() {
			return fmt.Errorf("branch target port %d mismatch", i)
		}
		ctx.Emit.Operand(inputs[i])
	}
	for i := target.PortCount() - 1; i >= 0; i-- {
		port, _ := target.Port(i)
		ctx.Emit.LocalSet(port.Local())
	}
	ctx.Emit.Br(target.Depth() + extraDepth)
	return ctx.Emit.Err()
}

// emitSourceBranch owns all private dispatch blocks and normal/rewind guards.
// Outer control tracking sees no private labels. Source depths are adjusted only
// by the exact number of private labels still open at each selected edge.
func emitSourceBranch(ctx *handler.Context, op ir.BranchOperation, guard bool, stateGlobal uint32) error {
	inputs, after, err := branchOperands(op, ctx.Stack.Snapshot())
	if err != nil {
		return err
	}
	// Void direct/conditional branches need no private transfer block. Keep
	// their existing compact state check when rewind can still reach them.
	if guard && op.ValidationReachable() && (op.Opcode() == wasm.OpBr || op.Opcode() == wasm.OpBrIf) {
		target, ok := op.Target(0)
		if !ok {
			return fmt.Errorf("branch has no target")
		}
		if target.PortCount() == 0 {
			if op.Opcode() == wasm.OpBrIf {
				ctx.Emit.Operand(inputs[len(inputs)-1]).I32Eqz().I32Eqz()
			}
			ctx.Emit.StateCheck(stateGlobal, StateNormal)
			if op.Opcode() == wasm.OpBrIf {
				ctx.Emit.I32And()
			}
			ctx.Emit.BrIf(target.Depth())
			ctx.Stack.Restore(after)
			return ctx.Emit.Err()
		}
	}
	extraDepth := uint32(0)
	if guard {
		ctx.Emit.StateCheck(stateGlobal, StateNormal).If(codegen.BlockVoid)
		extraDepth++
	}
	if !op.ValidationReachable() {
		ctx.Emit.Unreachable()
	} else {
		switch op.Opcode() {
		case wasm.OpBr:
			target, ok := op.Target(0)
			if !ok {
				return fmt.Errorf("branch has no target")
			}
			if err := emitBranchTarget(ctx, target, inputs, extraDepth); err != nil {
				return err
			}
		case wasm.OpBrIf:
			target, ok := op.Target(0)
			if !ok {
				return fmt.Errorf("conditional branch has no target")
			}
			selector := inputs[len(inputs)-1]
			if target.PortCount() == 0 {
				ctx.Emit.Operand(selector).BrIf(target.Depth() + extraDepth)
			} else {
				ctx.Emit.Operand(selector).If(codegen.BlockVoid)
				if err := emitBranchTarget(ctx, target, inputs, extraDepth+1); err != nil {
					return err
				}
				ctx.Emit.End()
			}
		case wasm.OpBrTable:
			if err := emitBranchTable(ctx, op, inputs, extraDepth); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported typed branch opcode %x", op.Opcode())
		}
	}
	if guard {
		ctx.Emit.End()
	}
	ctx.Stack.Restore(after)
	return ctx.Emit.Err()
}

func emitBranchTable(ctx *handler.Context, op ir.BranchOperation, inputs []stackEntry, extraDepth uint32) error {
	if op.TargetCount() == 0 || len(inputs) == 0 {
		return fmt.Errorf("branch table is incomplete")
	}
	first, _ := op.Target(0)
	if first.PortCount() == 0 {
		depths := make([]uint32, op.TargetCount())
		for i := range depths {
			target, ok := op.Target(i)
			if !ok {
				return fmt.Errorf("missing table target")
			}
			depths[i] = target.Depth() + extraDepth
		}
		ctx.Emit.Operand(inputs[len(inputs)-1]).BrTable(depths[:len(depths)-1], depths[len(depths)-1])
		return ctx.Emit.Err()
	}
	// Repeated ordinals share one dispatcher arm, while the table retains every
	// original ordinal and its unsigned out-of-range default semantics.
	var targets []ir.BranchTarget
	byLabel := make(map[ir.LabelID]uint32)
	labels := make([]uint32, op.TargetCount())
	for i := range labels {
		target, ok := op.Target(i)
		if !ok {
			return fmt.Errorf("missing table target %d", i)
		}
		index, exists := byLabel[target.Label()]
		if !exists {
			index = uint32(len(targets))
			byLabel[target.Label()] = index
			targets = append(targets, target)
		}
		labels[i] = index
	}
	for range targets {
		ctx.Emit.Block(codegen.BlockVoid)
	}
	ctx.Emit.Operand(inputs[len(inputs)-1]).BrTable(labels[:len(labels)-1], labels[len(labels)-1])
	for index, target := range targets {
		ctx.Emit.End()
		remaining := uint32(len(targets) - index - 1)
		if err := emitBranchTarget(ctx, target, inputs, extraDepth+remaining); err != nil {
			return err
		}
	}
	return ctx.Emit.Err()
}
