package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestCFG_BranchBypassesRedefinition(t *testing.T) {
	instrs := []wasm.Instruction{
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
	got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 2)
	if !equalUint32Sets(got, []uint32{0}) {
		t.Fatalf("live=%v; branch bypasses redefinition, so local 0 must survive suspension", got)
	}
}

func TestCFG_MutuallyExclusiveIfArms(t *testing.T) {
	t.Run("uses in opposite arms", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpDrop},
			{Opcode: wasm.OpElse},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
			{Opcode: wasm.OpDrop},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 2).LiveAtInstruction(instrs, 0)
		if !equalUint32Sets(got, []uint32{0, 1}) {
			t.Fatalf("live=%v, want [0 1]", got)
		}
	})

	t.Run("def in both arms kills merge use", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpElse},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 0)
		if !equalUint32Sets(got, nil) {
			t.Fatalf("live=%v, want [] (redefined on every path)", got)
		}
	})

	t.Run("def in then only keeps pre-call value live", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpElse},
			{Opcode: wasm.OpNop},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 0)
		if !equalUint32Sets(got, []uint32{0}) {
			t.Fatalf("live=%v, want [0]", got)
		}
	})

	t.Run("unused sibling local stays dead", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 2).LiveAtInstruction(instrs, 4)
		if !equalUint32Sets(got, []uint32{0}) {
			t.Fatalf("live=%v, want [0] (local 1 unused after call)", got)
		}
	})
}

func TestCFG_IfWithoutElseSkipsDef(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 0)
	if !equalUint32Sets(got, []uint32{0}) {
		t.Fatalf("live=%v, want [0] (false if skips the set)", got)
	}
}

func TestCFG_LoopBackedge(t *testing.T) {
	t.Run("use after call and on next iteration", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 10}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpI32Sub},
			{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 3)
		if !equalUint32Sets(got, []uint32{0}) {
			t.Fatalf("live=%v, want [0]", got)
		}
	})

	t.Run("def after call on every iteration is dead at call", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 1)
		if !equalUint32Sets(got, nil) {
			t.Fatalf("live=%v, want [] (set dominates the use, including backedge)", got)
		}
	})

	t.Run("use after loop keeps local live across backedge", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
			{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
			{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 2).LiveAtInstruction(instrs, 5)
		if !equalUint32Sets(got, []uint32{0, 1}) {
			t.Fatalf("live=%v, want [0 1]", got)
		}
	})
}

func TestCFG_NestedBrTable(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}}, // label 1
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}}, // label 0
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0, 1}, Default: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	got := NewLivenessAnalyzer(0, 2).LiveAtInstruction(instrs, 2)
	if !equalUint32Sets(got, []uint32{0, 1}) {
		t.Fatalf("live=%v, want [0 1] (br_table skips the set and reaches both uses)", got)
	}
}

func TestCFG_ReturnPaths(t *testing.T) {
	t.Run("return in then still unions else use", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpDrop},
			{Opcode: wasm.OpReturn},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 2).LiveAtInstruction(instrs, 0)
		if !equalUint32Sets(got, []uint32{0, 1}) {
			t.Fatalf("live=%v, want [0 1]", got)
		}
	})

	t.Run("code after return is dead", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpDrop},
			{Opcode: wasm.OpReturn},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 2).LiveAtInstruction(instrs, 0)
		if !equalUint32Sets(got, []uint32{0}) {
			t.Fatalf("live=%v, want [0] (local 1 is after return)", got)
		}
	})

	t.Run("immediate return saves nothing", func(t *testing.T) {
		instrs := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpReturn},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			{Opcode: wasm.OpEnd},
		}
		got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 0)
		if !equalUint32Sets(got, nil) {
			t.Fatalf("live=%v, want []", got)
		}
	})
}

func TestCFG_UnreachableStopsFallthrough(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 0)
	if !equalUint32Sets(got, []uint32{0}) {
		t.Fatalf("live=%v, want [0] (unreachable set must not kill)", got)
	}
}

func TestCFG_LocalTeeIsDefinition(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 2)
	if !equalUint32Sets(got, nil) {
		t.Fatalf("live=%v, want [] (tee after call defines the value the get uses)", got)
	}
}

func TestCFG_TryCatchSkipsDef(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpTry, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpCatch, Imm: wasm.ThrowImm{TagIdx: 0}},
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	got := NewLivenessAnalyzer(0, 1).LiveAtInstruction(instrs, 2)
	if !equalUint32Sets(got, []uint32{0}) {
		t.Fatalf("live=%v, want [0] (throw path skips the set)", got)
	}
}

func TestCFG_InvalidIndicesNoPanic(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 99}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 99}},
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{7, 8}, Default: 9}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	la := NewLivenessAnalyzer(0, 1)
	got := la.ComputeForCallSites(instrs, []int{-1, 0, 100, 2})
	if _, ok := got[0]; !ok {
		t.Fatal("in-range call site 0 missing from result")
	}
	if _, ok := got[-1]; ok {
		t.Fatal("negative index must not appear")
	}
	if _, ok := got[100]; ok {
		t.Fatal("out-of-range index must not appear")
	}
	_ = got[0]
}

func TestCFG_EmptyAndNilCallSites(t *testing.T) {
	if NewLivenessAnalyzer(0, 1).ComputeForCallSites(nil, nil) != nil {
		t.Fatal("empty callSites must return nil")
	}
	got := NewLivenessAnalyzer(0, 1).ComputeForCallSites(nil, []int{0})
	if got == nil || len(got) != 0 {
		t.Fatalf("no instructions: got %#v", got)
	}
}

func TestCFG_MultipleCallSites(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	info := NewLivenessAnalyzer(0, 1).ComputeForCallSites(instrs, []int{2, 9})
	if !equalUint32Sets(info[2], []uint32{0}) {
		t.Fatalf("first call live=%v, want [0]", info[2])
	}
	if !equalUint32Sets(info[9], []uint32{0}) {
		t.Fatalf("second call live=%v, want [0]", info[9])
	}
}
