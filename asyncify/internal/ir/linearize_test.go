package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLinearize_NoAsync(t *testing.T) {
	// Simple function with no async calls - should emit as-is
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should have same structure (minus the trailing End which is part of SeqNode parse)
	if len(result) != 3 {
		t.Errorf("expected 3 instructions, got %d", len(result))
	}
}

func TestLinearize_AsyncInThenBranch(t *testing.T) {
	// if with async call in then branch
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}},     // result i32
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},  // async call
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 99}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(10)
	config := &LinearizeConfig{
		StateGlobal:    5,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Verify structure:
	// - i32.const 1
	// - local.set $cond
	// - (rewinding check) || local.get $cond
	// - if (void)
	// - call
	// - local.set $result
	// - end
	// - (!rewinding) && !local.get $cond
	// - if (void)
	// - i32.const 99
	// - local.set $result
	// - end
	// - local.get $result

	// Check that we have the expected pattern
	foundCondSet := false
	foundRewindingCheck := false
	for _, instr := range result {
		if instr.Opcode == wasm.OpLocalSet {
			if imm, ok := instr.Imm.(wasm.LocalImm); ok && imm.LocalIdx == 10 {
				foundCondSet = true
			}
		}
		if instr.Opcode == wasm.OpGlobalGet {
			if imm, ok := instr.Imm.(wasm.GlobalImm); ok && imm.GlobalIdx == 5 {
				foundRewindingCheck = true
			}
		}
	}

	if !foundCondSet {
		t.Error("expected condition local.set")
	}
	if !foundRewindingCheck {
		t.Error("expected rewinding state check")
	}
}

func TestLinearize_AsyncInElseBranch(t *testing.T) {
	// if with async call in else branch
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}},     // result i32
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call in else
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should have three if blocks:
	// 1. condition-save: if (rewinding) { drop } else { local.set $cond }
	// 2. then branch: if (thenCondition) { ... }
	// 3. else branch: if (elseCondition) { ... }
	ifCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpIf {
			ifCount++
		}
	}

	if ifCount != 3 {
		t.Errorf("expected 3 if blocks (condition-save + then + else), got %d", ifCount)
	}
}

func TestLinearize_AsyncInBothBranches(t *testing.T) {
	// if with async calls in both branches
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}},     // result i32
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},  // async call in then
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}}, // async call in else
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true, 1: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// When BOTH branches have async, we use just the saved condition (no rewinding || cond).
	// This ensures only the originally-taken branch executes during rewind.
	// Should have 0 i32.or (no "rewinding || cond" patterns)
	orCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpI32Or {
			orCount++
		}
	}

	if orCount != 0 {
		t.Errorf("expected 0 i32.or (both branches use saved condition), got %d", orCount)
	}

	// Still should have 3 if blocks: condition-save + then + else
	ifCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpIf {
			ifCount++
		}
	}

	if ifCount != 3 {
		t.Errorf("expected 3 if blocks, got %d", ifCount)
	}
}

func TestLinearize_NestedIfWithAsync(t *testing.T) {
	// Nested if with async in inner branch
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // outer condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}},     // outer result i32
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // inner condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}},     // inner result i32
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},  // async call
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 99}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 88}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should have multiple linearized if blocks
	ifCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpIf {
			ifCount++
		}
	}

	// Each linearized if now has:
	// - 1 condition-save if: if (rewinding) { drop } else { local.set $cond }
	// - 1-2 branch ifs depending on else presence
	// Outer if: 1 (cond-save) + 2 (then + else) = 3
	// Inner if: 1 (cond-save) + 2 (then + else) = 3
	// Total: 6 ifs
	if ifCount != 6 {
		t.Errorf("expected 6 if blocks, got %d", ifCount)
	}
}

func TestLinearize_BlockNoAsyncNoResult(t *testing.T) {
	// Block with no async call, no result, no params - should emit as-is
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}}, // void block (no result)
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{}, // no async funcs
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should have: block, i32.const, drop, end
	foundBlock := false
	foundEnd := false
	for _, instr := range result {
		if instr.Opcode == wasm.OpBlock {
			foundBlock = true
		}
		if instr.Opcode == wasm.OpEnd {
			foundEnd = true
		}
	}

	if !foundBlock || !foundEnd {
		t.Error("expected block and end instructions to be preserved")
	}
}

func TestLinearize_IfNoAsyncNoResult(t *testing.T) {
	// If with no async, no result, no params - should emit as-is
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},    // void if
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	foundIf := false
	foundEnd := false
	for _, instr := range result {
		if instr.Opcode == wasm.OpIf {
			foundIf = true
		}
		if instr.Opcode == wasm.OpEnd {
			foundEnd = true
		}
	}

	if !foundIf || !foundEnd {
		t.Error("expected if and end to be preserved")
	}
}

func TestLinearize_IfNoAsyncNoResultWithElse(t *testing.T) {
	// If/else with no async, no result, no params - should emit as-is
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	foundIf := false
	foundElse := false
	foundEnd := false
	for _, instr := range result {
		if instr.Opcode == wasm.OpIf {
			foundIf = true
		}
		if instr.Opcode == wasm.OpElse {
			foundElse = true
		}
		if instr.Opcode == wasm.OpEnd {
			foundEnd = true
		}
	}

	if !foundIf || !foundElse || !foundEnd {
		t.Error("expected if, else, and end to be preserved")
	}
}

func TestLinearize_BlockWithResult(t *testing.T) {
	// Block with result
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -1}}, // result i32
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should have local.set and local.get for result
	foundSet := false
	foundGet := false
	for _, instr := range result {
		if instr.Opcode == wasm.OpLocalSet {
			foundSet = true
		}
		if instr.Opcode == wasm.OpLocalGet {
			foundGet = true
		}
	}

	if !foundSet || !foundGet {
		t.Error("expected local.set and local.get for block result")
	}
}

func TestLinearize_LoopWithResult(t *testing.T) {
	// Loop with result (rare but valid)
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -1}}, // result i32
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should emit as void loop with result locals
	loopFound := false
	for _, instr := range result {
		if instr.Opcode == wasm.OpLoop {
			if imm, ok := instr.Imm.(wasm.BlockImm); ok {
				if imm.Type == -64 { // void
					loopFound = true
				}
			}
		}
	}

	if !loopFound {
		t.Error("expected void loop after linearization")
	}
}

func TestLinearize_CallIndirectTreatedAsAsync(t *testing.T) {
	// call_indirect should be treated as async
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},                // condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}},                    // result i32
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},                // table index
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0}}, // indirect call
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 99}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{}, // no explicit async funcs
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should detect call_indirect as async and linearize
	// 1 condition-save if + 2 branch ifs = 3 total
	ifCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpIf {
			ifCount++
		}
	}

	if ifCount != 3 {
		t.Errorf("expected 3 if blocks for call_indirect branch, got %d", ifCount)
	}
}

// Tests for transformBranches - critical control flow code

func TestTransformBranches_OpBr(t *testing.T) {
	// br targeting a result-bearing block that was flattened to void
	// Original: (block (result i32) ... (br 0) ...)
	// After linearization, block is void but br still carries value
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}}, // targets our block
	}

	resultLocals := []uint32{10} // one i32 result local
	nextLocal := uint32(20)
	allocLocal := func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l }

	result := transformBranches(instrs, 0, resultLocals, allocLocal)

	// Should insert local.set before br
	// Expected: i32.const 42, local.set 10, br 0
	if len(result) != 3 {
		t.Fatalf("expected 3 instructions, got %d: %v", len(result), result)
	}

	if result[0].Opcode != wasm.OpI32Const {
		t.Errorf("result[0] should be i32.const, got %#x", result[0].Opcode)
	}
	if result[1].Opcode != wasm.OpLocalSet {
		t.Errorf("result[1] should be local.set, got %#x", result[1].Opcode)
	}
	if imm, ok := result[1].Imm.(wasm.LocalImm); !ok || imm.LocalIdx != 10 {
		t.Errorf("result[1] should set local 10, got %v", result[1].Imm)
	}
	if result[2].Opcode != wasm.OpBr {
		t.Errorf("result[2] should be br, got %#x", result[2].Opcode)
	}
}

func TestTransformBranches_OpBrIf(t *testing.T) {
	// br_if targeting a result-bearing block
	// Stack: [value, condition] -> if taken, value becomes result
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},  // value
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},   // condition
		{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}}, // targets our block
	}

	resultLocals := []uint32{10}
	nextLocal := uint32(20)
	allocLocal := func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l }

	result := transformBranches(instrs, 0, resultLocals, allocLocal)

	// Should transform to:
	// i32.const 42 (value)
	// i32.const 1 (condition)
	// local.set $cond (save condition)
	// local.set 10 (save value to result local)
	// local.get $cond (reload condition)
	// br_if 0
	// local.get 10 (reload value for fallthrough)

	if len(result) != 7 {
		t.Fatalf("expected 7 instructions, got %d: %v", len(result), result)
	}

	// Check key instructions
	if result[2].Opcode != wasm.OpLocalSet {
		t.Errorf("result[2] should be local.set (save cond), got %#x", result[2].Opcode)
	}
	if result[3].Opcode != wasm.OpLocalSet {
		t.Errorf("result[3] should be local.set (save value), got %#x", result[3].Opcode)
	}
	if result[4].Opcode != wasm.OpLocalGet {
		t.Errorf("result[4] should be local.get (reload cond), got %#x", result[4].Opcode)
	}
	if result[5].Opcode != wasm.OpBrIf {
		t.Errorf("result[5] should be br_if, got %#x", result[5].Opcode)
	}
	if result[6].Opcode != wasm.OpLocalGet {
		t.Errorf("result[6] should be local.get (reload value), got %#x", result[6].Opcode)
	}
}

func TestTransformBranches_OpBrTable(t *testing.T) {
	// br_table targeting a result-bearing block
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}}, // value
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},  // index
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0, 1}, Default: 0}},
	}

	resultLocals := []uint32{10}
	nextLocal := uint32(20)
	allocLocal := func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l }

	result := transformBranches(instrs, 0, resultLocals, allocLocal)

	// Should transform to:
	// i32.const 42 (value)
	// i32.const 0 (index)
	// local.set $idx (save index)
	// local.set 10 (save value)
	// local.get $idx (reload index)
	// br_table

	if len(result) != 6 {
		t.Fatalf("expected 6 instructions, got %d: %v", len(result), result)
	}

	if result[2].Opcode != wasm.OpLocalSet {
		t.Errorf("result[2] should be local.set (save idx), got %#x", result[2].Opcode)
	}
	if result[3].Opcode != wasm.OpLocalSet {
		t.Errorf("result[3] should be local.set (save value), got %#x", result[3].Opcode)
	}
	if result[4].Opcode != wasm.OpLocalGet {
		t.Errorf("result[4] should be local.get (reload idx), got %#x", result[4].Opcode)
	}
	if result[5].Opcode != wasm.OpBrTable {
		t.Errorf("result[5] should be br_table, got %#x", result[5].Opcode)
	}
}

func TestTransformBranches_MultiValue(t *testing.T) {
	// br targeting block with multiple results
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
	}

	resultLocals := []uint32{10, 11} // two results
	nextLocal := uint32(20)
	allocLocal := func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l }

	result := transformBranches(instrs, 0, resultLocals, allocLocal)

	// Should insert two local.sets before br (reverse order for stack)
	// Expected: const 1, const 2, local.set 11, local.set 10, br
	if len(result) != 5 {
		t.Fatalf("expected 5 instructions, got %d", len(result))
	}

	// Check local.sets are in reverse order (last result first)
	if imm, ok := result[2].Imm.(wasm.LocalImm); !ok || imm.LocalIdx != 11 {
		t.Errorf("first local.set should target local 11, got %v", result[2].Imm)
	}
	if imm, ok := result[3].Imm.(wasm.LocalImm); !ok || imm.LocalIdx != 10 {
		t.Errorf("second local.set should target local 10, got %v", result[3].Imm)
	}
}

func TestTransformBranches_NestedBlocks(t *testing.T) {
	// br inside nested block targeting outer block
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}}, // inner block (void)
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 1}}, // targets outer (depth=1)
		{Opcode: wasm.OpEnd},
	}

	resultLocals := []uint32{10}
	nextLocal := uint32(20)
	allocLocal := func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l }

	// targetDepth=0 means we're transforming for the outermost block
	// The br targets LabelIdx=1, which at depth=1 (inside inner block) means outer
	result := transformBranches(instrs, 0, resultLocals, allocLocal)

	// br 1 inside the inner block (at depth 1) should match targetDepth 0
	// depth + targetDepth = 1 + 0 = 1, matches LabelIdx=1
	// So local.set should be inserted
	setCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpLocalSet {
			setCount++
		}
	}

	if setCount != 1 {
		t.Errorf("expected 1 local.set for br targeting outer block, got %d", setCount)
	}
}

func TestTransformBranches_NoResultLocals(t *testing.T) {
	// When no result locals, should return input unchanged
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
	}

	result := transformBranches(instrs, 0, nil, nil)

	if len(result) != len(instrs) {
		t.Errorf("with no result locals, should return unchanged: got %d, want %d", len(result), len(instrs))
	}
}

func TestLinearize_BlockWithParams(t *testing.T) {
	// Block with params (multi-value WASM feature)
	// When a block has params, they're consumed from parent stack and need to be
	// saved to locals before the block body
	module := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
	}

	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}}, // param value on stack
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: 0}},    // type index 0: (param i32)(result i32)
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},   // async call
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs, module)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should have local.set for param and local.get to reload it
	setCount := 0
	getCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpLocalSet {
			setCount++
		}
		if instr.Opcode == wasm.OpLocalGet {
			getCount++
		}
	}

	if setCount == 0 {
		t.Error("expected local.set for block param")
	}
	if getCount == 0 {
		t.Error("expected local.get to reload param")
	}
}

func TestLinearize_IfWithParams(t *testing.T) {
	// If with params (multi-value WASM feature)
	module := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
	}

	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 10}}, // param value
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},  // condition
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: 0}},       // type index with param
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},   // async
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 99}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs, module)
	nextLocal := uint32(0)
	config := &LinearizeConfig{
		StateGlobal:    0,
		StateRewinding: 2,
		AsyncFuncs:     map[uint32]bool{0: true},
		AllocLocal:     func(vt wasm.ValType) uint32 { l := nextLocal; nextLocal++; return l },
	}

	result := Linearize(tree, config)

	// Should linearize with param handling
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}

	// Count local operations - should have sets for both param and condition
	setCount := 0
	for _, instr := range result {
		if instr.Opcode == wasm.OpLocalSet {
			setCount++
		}
	}

	if setCount < 2 {
		t.Errorf("expected at least 2 local.set (param + condition), got %d", setCount)
	}
}

func TestBranchHasAsync_NestedBlocks(t *testing.T) {
	// Test branchHasAsync with nested blocks containing async call
	asyncFuncs := map[uint32]bool{0: true}

	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	if !branchHasAsync(tree, asyncFuncs) {
		t.Error("should detect async in nested block")
	}
}

func TestBranchHasAsync_IfBranches(t *testing.T) {
	// Test branchHasAsync with if/else containing async
	asyncFuncs := map[uint32]bool{0: true}

	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	if !branchHasAsync(tree, asyncFuncs) {
		t.Error("should detect async in if then branch")
	}
}

func TestBranchHasAsync_ElseBranch(t *testing.T) {
	// Test branchHasAsync with async in else branch only
	asyncFuncs := map[uint32]bool{0: true}

	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpNop}, // then branch - no async
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async in else
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	if !branchHasAsync(tree, asyncFuncs) {
		t.Error("should detect async in else branch")
	}
}

func TestBranchHasAsync(t *testing.T) {
	asyncFuncs := map[uint32]bool{0: true}

	tests := []struct {
		name   string
		instrs []wasm.Instruction
		want   bool
	}{
		{
			name:   "no calls",
			instrs: []wasm.Instruction{{Opcode: wasm.OpI32Const}, {Opcode: wasm.OpEnd}},
			want:   false,
		},
		{
			name:   "async call",
			instrs: []wasm.Instruction{{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, {Opcode: wasm.OpEnd}},
			want:   true,
		},
		{
			name:   "non-async call",
			instrs: []wasm.Instruction{{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}}, {Opcode: wasm.OpEnd}},
			want:   false,
		},
		{
			name:   "call_indirect",
			instrs: []wasm.Instruction{{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0}}, {Opcode: wasm.OpEnd}},
			want:   true,
		},
		{
			name:   "call_ref",
			instrs: []wasm.Instruction{{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 0}}, {Opcode: wasm.OpEnd}},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := Parse(tt.instrs)
			got := branchHasAsync(tree, asyncFuncs)
			if got != tt.want {
				t.Errorf("branchHasAsync() = %v, want %v", got, tt.want)
			}
		})
	}
}
