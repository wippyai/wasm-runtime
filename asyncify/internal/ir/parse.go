package ir

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// Parse constructs a control tree from exactly one complete function expression.
// It checks structure, block signatures and branch depths before publishing the
// tree. It does not validate operand-stack types or unreachable polymorphism.
// Explicit frames make nesting independent of the Go call stack.
func Parse(instrs []wasm.Instruction, modules ...*wasm.Module) (Node, error) {
	var module *wasm.Module
	if len(modules) > 0 {
		module = modules[0]
	}
	root := &SeqNode{}
	frames := []parseFrame{{sequence: root}}
	for index, instr := range instrs {
		if len(frames) == 0 {
			return nil, parseError(index, "instructions after function end")
		}
		current := &frames[len(frames)-1]
		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
			imm, ok := instr.Imm.(wasm.BlockImm)
			if !ok {
				return nil, parseError(index, "invalid block immediate %T", instr.Imm)
			}
			signature, err := semantics.ResolveBlockType(imm.Type, module)
			if err != nil {
				return nil, parseError(index, "%s", err)
			}
			body := &SeqNode{}
			next := parseFrame{sequence: body, opcode: instr.Opcode, label: LabelID(index + 1)}
			if instr.Opcode == wasm.OpIf {
				node := &IfNode{label: next.label, Then: body, ParamTypes: signature.Params, ResultTypes: signature.Results, Imm: imm}
				next.conditional = node
				current.sequence.Children = append(current.sequence.Children, node)
			} else {
				node := &BlockNode{label: next.label, Body: body, ParamTypes: signature.Params, ResultTypes: signature.Results, Imm: imm, Opcode: instr.Opcode}
				current.sequence.Children = append(current.sequence.Children, node)
			}
			frames = append(frames, next)
		case wasm.OpElse:
			if current.opcode != wasm.OpIf || current.conditional == nil || current.conditional.Else != nil {
				return nil, parseError(index, "else without an unmatched if")
			}
			body := &SeqNode{}
			current.conditional.Else, current.sequence = body, body
		case wasm.OpEnd:
			frames = frames[:len(frames)-1]
		case wasm.OpBr, wasm.OpBrIf:
			imm, ok := instr.Imm.(wasm.BranchImm)
			if !ok {
				return nil, parseError(index, "invalid branch immediate %T", instr.Imm)
			}
			if uint64(imm.LabelIdx) >= uint64(len(frames)) {
				return nil, parseError(index, "branch depth %d out of range", imm.LabelIdx)
			}
			current.sequence.Children = append(current.sequence.Children, &InstrNode{Instr: instr, branchTargets: []LabelID{frames[len(frames)-1-int(imm.LabelIdx)].label}})
		case wasm.OpBrTable:
			imm, ok := instr.Imm.(wasm.BrTableImm)
			if !ok {
				return nil, parseError(index, "invalid branch table immediate %T", instr.Imm)
			}
			if uint64(imm.Default) >= uint64(len(frames)) {
				return nil, parseError(index, "default branch depth %d out of range", imm.Default)
			}
			for _, depth := range imm.Labels {
				if uint64(depth) >= uint64(len(frames)) {
					return nil, parseError(index, "branch table depth %d out of range", depth)
				}
			}
			targets := make([]LabelID, 0, len(imm.Labels)+1)
			for _, depth := range imm.Labels {
				targets = append(targets, frames[len(frames)-1-int(depth)].label)
			}
			targets = append(targets, frames[len(frames)-1-int(imm.Default)].label)
			// The tree owns branch lists; later rewrites cannot change the source.
			imm.Labels = slices.Clone(imm.Labels)
			instr.Imm = imm
			current.sequence.Children = append(current.sequence.Children, &InstrNode{Instr: instr, branchTargets: targets})
		case wasm.OpTry, wasm.OpTryTable, wasm.OpCatch, wasm.OpCatchAll, wasm.OpDelegate:
			return nil, parseError(index, "exception control opcode 0x%x is not supported by Asyncify control lowering", instr.Opcode)
		default:
			current.sequence.Children = append(current.sequence.Children, &InstrNode{Instr: instr})
		}
	}
	if len(frames) != 0 {
		return nil, parseError(len(instrs), "missing end for %d control frames", len(frames))
	}
	return root, nil
}

type parseFrame struct {
	sequence    *SeqNode
	conditional *IfNode
	label       LabelID
	opcode      byte
}

func parseError(index int, format string, args ...any) error {
	return fmt.Errorf("asyncify: control input at instruction %d: %s", index, fmt.Sprintf(format, args...))
}
