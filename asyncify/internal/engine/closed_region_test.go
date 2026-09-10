package engine

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestClosedVoidBlockProjectionAndBoundaries(t *testing.T) {
	region := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpEnd},
	}
	body := append([]wasm.Instruction{}, region...)
	body = append(body, wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}})
	output := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)
	if !bytes.Contains(wasm.EncodeInstructions(output), wasm.EncodeInstructions(region)) {
		t.Fatal("closed block was not preserved")
	}
	ft := NewFunctionTransformer(DefaultRegistry(), &wasm.Module{}, GlobalIndices{}, 0, false)
	escape := []wasm.Instruction{{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}}, {Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 1}}, {Opcode: wasm.OpEnd}}
	if got := ft.closedVoidRegionSpans(escape, nil); len(got) != 0 {
		t.Fatalf("copied escaping block: %v", got)
	}
	if got := ft.closedVoidRegionSpans(region, map[int]bool{2: true}); len(got) != 0 {
		t.Fatalf("copied excluded action: %v", got)
	}
}
