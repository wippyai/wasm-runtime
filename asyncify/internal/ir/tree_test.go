package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestParse_SimpleSequence(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	if tree == nil {
		t.Fatal("tree is nil")
	}

	seq, ok := tree.(*SeqNode)
	if !ok {
		t.Fatalf("expected SeqNode, got %T", tree)
	}
	if len(seq.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(seq.Children))
	}
}

func TestParse_Block(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	seq := tree.(*SeqNode)
	if len(seq.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(seq.Children))
	}

	block, ok := seq.Children[0].(*BlockNode)
	if !ok {
		t.Fatalf("expected BlockNode, got %T", seq.Children[0])
	}
	if block.Opcode != wasm.OpBlock {
		t.Fatalf("expected OpBlock, got %d", block.Opcode)
	}
	if len(block.ResultTypes) != 1 || block.ResultTypes[0] != wasm.ValI32 {
		t.Fatalf("expected i32 result, got %v", block.ResultTypes)
	}
}

func TestParse_Loop(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	seq := tree.(*SeqNode)
	if len(seq.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(seq.Children))
	}

	loop, ok := seq.Children[0].(*BlockNode)
	if !ok {
		t.Fatalf("expected BlockNode, got %T", seq.Children[0])
	}
	if loop.Opcode != wasm.OpLoop {
		t.Fatalf("expected OpLoop, got %d", loop.Opcode)
	}
}

func TestParse_IfElse(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 3}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	seq := tree.(*SeqNode)
	if len(seq.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(seq.Children))
	}

	ifNode, ok := seq.Children[1].(*IfNode)
	if !ok {
		t.Fatalf("expected IfNode, got %T", seq.Children[1])
	}
	if len(ifNode.ResultTypes) != 1 {
		t.Fatalf("expected 1 result type, got %d", len(ifNode.ResultTypes))
	}
	if ifNode.Then == nil {
		t.Fatal("then branch is nil")
	}
	if ifNode.Else == nil {
		t.Fatal("else branch is nil")
	}
}

func TestParse_IfWithoutElse(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpNop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	seq := tree.(*SeqNode)
	if len(seq.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(seq.Children))
	}

	ifNode := seq.Children[1].(*IfNode)
	if ifNode.Then == nil {
		t.Fatal("then branch is nil")
	}
	if ifNode.Else != nil {
		t.Fatal("else branch should be nil")
	}
}

func TestParse_NestedBlocks(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	seq := tree.(*SeqNode)
	if len(seq.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(seq.Children))
	}

	outer := seq.Children[0].(*BlockNode)
	innerSeq := outer.Body.(*SeqNode)
	if len(innerSeq.Children) != 1 {
		t.Fatalf("expected 1 inner child, got %d", len(innerSeq.Children))
	}

	inner := innerSeq.Children[0].(*BlockNode)
	if inner.Body == nil {
		t.Fatal("inner body is nil")
	}
}

func TestParse_BranchInstructions(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs)
	seq := tree.(*SeqNode)
	block := seq.Children[0].(*BlockNode)
	bodySeq := block.Body.(*SeqNode)

	if len(bodySeq.Children) != 1 {
		t.Fatalf("expected 1 child in body, got %d", len(bodySeq.Children))
	}

	br, ok := bodySeq.Children[0].(*InstrNode)
	if !ok {
		t.Fatalf("expected InstrNode, got %T", bodySeq.Children[0])
	}
	if br.Instr.Opcode != wasm.OpBr {
		t.Fatalf("expected OpBr, got %d", br.Instr.Opcode)
	}
}

func TestNode_IsControlFlow(t *testing.T) {
	blockNode := &BlockNode{Opcode: wasm.OpBlock}
	if !blockNode.IsControlFlow() {
		t.Error("BlockNode should be control flow")
	}

	ifNode := &IfNode{}
	if !ifNode.IsControlFlow() {
		t.Error("IfNode should be control flow")
	}

	instrNode := &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpI32Add}}
	if instrNode.IsControlFlow() {
		t.Error("InstrNode should not be control flow")
	}

	seqNode := &SeqNode{}
	if seqNode.IsControlFlow() {
		t.Error("SeqNode should not be control flow")
	}
}

func TestNode_ResultTypes(t *testing.T) {
	block := &BlockNode{
		Opcode:      wasm.OpBlock,
		ResultTypes: []wasm.ValType{wasm.ValI32, wasm.ValI64},
	}
	results := block.Results()
	if len(results) != 2 || results[0] != wasm.ValI32 || results[1] != wasm.ValI64 {
		t.Errorf("block results wrong: %v", results)
	}

	ifNode := &IfNode{
		ResultTypes: []wasm.ValType{wasm.ValF32},
	}
	results = ifNode.Results()
	if len(results) != 1 || results[0] != wasm.ValF32 {
		t.Errorf("if results wrong: %v", results)
	}

	seq := &SeqNode{}
	if seq.Results() != nil {
		t.Error("seq results should be nil")
	}

	instr := &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpNop}}
	if instr.Results() != nil {
		t.Error("instr results should be nil")
	}
}

func TestBlockTypeToResults_AllTypes(t *testing.T) {
	tests := []struct {
		expected  []wasm.ValType
		blockType int32
	}{
		{[]wasm.ValType{wasm.ValI32}, -1},
		{[]wasm.ValType{wasm.ValI64}, -2},
		{[]wasm.ValType{wasm.ValF32}, -3},
		{[]wasm.ValType{wasm.ValF64}, -4},
		{[]wasm.ValType{wasm.ValV128}, -5},
		{[]wasm.ValType{wasm.ValFuncRef}, -16},
		{[]wasm.ValType{wasm.ValExtern}, -17},
		{nil, -64},
	}

	for _, tc := range tests {
		got := blockTypeToResults(tc.blockType, nil)
		if len(got) != len(tc.expected) {
			t.Errorf("blockType %d: got %v, want %v", tc.blockType, got, tc.expected)
			continue
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Errorf("blockType %d[%d]: got %v, want %v", tc.blockType, i, got[i], tc.expected[i])
			}
		}
	}
}

func TestBlockTypeToParamsAndResults_TypeIndex(t *testing.T) {
	// Test type index path (blockType >= 0 with module)
	module := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32, wasm.ValI64}, Results: []wasm.ValType{wasm.ValF32}},
			{Params: nil, Results: []wasm.ValType{wasm.ValI32, wasm.ValI64}},
		},
	}

	// Type index 0
	params, results := blockTypeToParamsAndResults(0, module)
	if len(params) != 2 || params[0] != wasm.ValI32 || params[1] != wasm.ValI64 {
		t.Errorf("type 0 params: got %v, want [i32, i64]", params)
	}
	if len(results) != 1 || results[0] != wasm.ValF32 {
		t.Errorf("type 0 results: got %v, want [f32]", results)
	}

	// Type index 1
	params, results = blockTypeToParamsAndResults(1, module)
	if len(params) != 0 {
		t.Errorf("type 1 params: got %v, want []", params)
	}
	if len(results) != 2 {
		t.Errorf("type 1 results: got %v, want [i32, i64]", results)
	}

	// Out of bounds type index
	params, results = blockTypeToParamsAndResults(99, module)
	if params != nil || results != nil {
		t.Errorf("out of bounds: got params=%v results=%v, want nil", params, results)
	}

	// Nil module with type index
	params, results = blockTypeToParamsAndResults(0, nil)
	if params != nil || results != nil {
		t.Errorf("nil module: got params=%v results=%v, want nil", params, results)
	}
}

func TestParse_BlockWithTypeIndex(t *testing.T) {
	// Block using type index for multi-value
	module := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64, wasm.ValF32}},
		},
	}

	instrs := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: 0}}, // type index 0
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	tree := Parse(instrs, module)
	seq := tree.(*SeqNode)
	block := seq.Children[0].(*BlockNode)

	if len(block.ParamTypes) != 1 || block.ParamTypes[0] != wasm.ValI32 {
		t.Errorf("block params: got %v, want [i32]", block.ParamTypes)
	}
	if len(block.ResultTypes) != 2 {
		t.Errorf("block results: got %v, want [i64, f32]", block.ResultTypes)
	}
}
