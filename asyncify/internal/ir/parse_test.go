package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestParseRejectsMalformedControl(t *testing.T) {
	block := wasm.Instruction{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}}
	conditional := wasm.Instruction{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}}
	end := wasm.Instruction{Opcode: wasm.OpEnd}
	otherwise := wasm.Instruction{Opcode: wasm.OpElse}
	for _, tc := range []struct {
		name         string
		instructions []wasm.Instruction
	}{
		{"empty", nil},
		{"no-function-end", []wasm.Instruction{{Opcode: wasm.OpNop}}},
		{"unclosed-block", []wasm.Instruction{block, end}},
		{"unclosed-else", []wasm.Instruction{conditional, otherwise}},
		{"root-else", []wasm.Instruction{otherwise, end}},
		{"block-else", []wasm.Instruction{block, otherwise, end, end}},
		{"duplicate-else", []wasm.Instruction{conditional, otherwise, otherwise, end, end}},
		{"after-end", []wasm.Instruction{end, {Opcode: wasm.OpNop}}},
		{"extra-end", []wasm.Instruction{end, end}},
		{"block-immediate", []wasm.Instruction{{Opcode: wasm.OpBlock}, end, end}},
		{"block-type", []wasm.Instruction{{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: 0}}, end, end}},
		{"branch-immediate", []wasm.Instruction{{Opcode: wasm.OpBr}, end}},
		{"branch-depth", []wasm.Instruction{{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 1}}, end}},
		{"table-immediate", []wasm.Instruction{{Opcode: wasm.OpBrTable}, end}},
		{"table-depth", []wasm.Instruction{{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{1}}}, end}},
		{"table-default", []wasm.Instruction{{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Default: 1}}, end}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := Parse(tc.instructions)
			if err == nil || tree != nil {
				t.Fatalf("published malformed control tree: %v %v", tree, err)
			}
		})
	}
}

func TestParseOwnsBranchTableAndSignatures(t *testing.T) {
	module := &wasm.Module{Types: []wasm.FuncType{{Results: []wasm.ValType{wasm.ValI64}}}}
	labels := []uint32{0, 1}
	instructions := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: 0}},
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: labels, Default: 1}},
		{Opcode: wasm.OpEnd}, {Opcode: wasm.OpEnd},
	}
	tree, err := Parse(instructions, module)
	if err != nil {
		t.Fatal(err)
	}
	block := tree.(*SeqNode).Children[0].(*BlockNode)
	table := block.Body.(*SeqNode).Children[0].(*InstrNode).Instr.Imm.(wasm.BrTableImm)
	labels[0] = 999
	module.Types[0].Results[0] = wasm.ValI32
	if table.Labels[0] != 0 || block.ResultTypes[0] != wasm.ValI64 {
		t.Fatal("source mutation changed tree")
	}
}

func TestParseDeepControlAndFunctionLabel(t *testing.T) {
	const depth = 8192
	instructions := make([]wasm.Instruction, 0, 2*depth+2)
	for range depth {
		instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}})
	}
	instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: depth}})
	for range depth + 1 {
		instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpEnd})
	}
	if _, err := Parse(instructions); err != nil {
		t.Fatal(err)
	}
}
