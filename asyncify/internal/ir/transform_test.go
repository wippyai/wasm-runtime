package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestAnalyze_NoAsyncCalls(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	config := &TransformConfig{
		AsyncFuncs: map[uint32]bool{0: true},
	}
	result := Analyze(tree, config)

	if result.NeedsTransform {
		t.Error("expected no transform needed")
	}
	if len(result.CallSites) != 0 {
		t.Errorf("expected 0 call sites, got %d", len(result.CallSites))
	}
}

func TestAnalyze_WithAsyncCall(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	config := &TransformConfig{
		AsyncFuncs: map[uint32]bool{0: true},
	}
	result := Analyze(tree, config)

	if !result.NeedsTransform {
		t.Error("expected transform needed")
	}
	if len(result.CallSites) != 1 {
		t.Errorf("expected 1 call site, got %d", len(result.CallSites))
	}
}

func TestAnalyze_AsyncInBranch(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	config := &TransformConfig{
		AsyncFuncs: map[uint32]bool{0: true},
	}
	result := Analyze(tree, config)

	if !result.NeedsTransform {
		t.Error("expected transform needed")
	}
	if !result.HasAsyncInBranch {
		t.Error("expected HasAsyncInBranch to be true")
	}
}

func TestAnalyze_CallIndirect(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0}},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	config := &TransformConfig{
		AsyncFuncs: map[uint32]bool{},
	}
	result := Analyze(tree, config)

	if !result.NeedsTransform {
		t.Error("expected transform needed for call_indirect")
	}
}

func TestAnalyze_NestedAsync(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	config := &TransformConfig{
		AsyncFuncs: map[uint32]bool{0: true},
	}
	result := Analyze(tree, config)

	if !result.NeedsTransform {
		t.Error("expected transform needed")
	}
	if !result.HasAsyncInBranch {
		t.Error("expected HasAsyncInBranch for nested async")
	}
}

func TestAnalyze_MultipleAsyncCalls(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	config := &TransformConfig{
		AsyncFuncs: map[uint32]bool{0: true},
	}
	result := Analyze(tree, config)

	if !result.NeedsTransform {
		t.Error("expected transform needed")
	}
	if len(result.CallSites) != 2 {
		t.Errorf("expected 2 call sites, got %d", len(result.CallSites))
	}
}
