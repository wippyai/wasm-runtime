package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLivenessBranchBypassesRedefinition(t *testing.T) {
	instructions := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	// The taken branch skips the local.set, so the pre-suspension value is live.
	got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instructions, 2)
	if !equalUint32Sets(got, []uint32{0}) {
		t.Fatalf("live=%v; branch bypasses redefinition, so local 0 must survive suspension", got)
	}
}
