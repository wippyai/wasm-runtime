// Liveness analysis for asyncify optimization.
//
// Reference: Binaryen asyncify pass
// https://github.com/WebAssembly/binaryen/blob/main/src/passes/Asyncify.cpp
//
// Liveness analysis determines which locals are "live" at each program point.
// A local is live if its value may be used before being redefined.
// At async call sites, we only need to save live locals instead of all locals.
package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TestLivenessAnalysis_Basic tests basic liveness computation.
func TestLivenessAnalysis_Basic(t *testing.T) {
	tests := []struct {
		name         string
		instrs       []wasm.Instruction
		wantLive     []uint32
		numParams    int
		numLocals    int
		asyncCallIdx int
	}{
		{
			name: "local used after call",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}}, // use param 0
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}}, // define local 1
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},       // async call
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}}, // use local 1 after
				{Opcode: wasm.OpEnd},
			},
			numParams:    1,
			numLocals:    1,
			asyncCallIdx: 2,
			wantLive:     []uint32{1}, // only local 1 is live (used after call)
		},
		{
			name: "local not used after call",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}}, // used before call
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},       // async call
				{Opcode: wasm.OpEnd},
			},
			numParams:    1,
			numLocals:    1,
			asyncCallIdx: 3,
			wantLive:     []uint32{}, // local 1 not used after call
		},
		{
			name: "multiple locals some live",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 3}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 2}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 2}},
				{Opcode: wasm.OpI32Add},
				{Opcode: wasm.OpEnd},
			},
			numParams:    0,
			numLocals:    3,
			asyncCallIdx: 6,
			wantLive:     []uint32{0, 2}, // 0 and 2 live, 1 not used after
		},
		{
			name: "local redefined after call",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}}, // redefined
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}}, // use of new value
				{Opcode: wasm.OpEnd},
			},
			numParams:    0,
			numLocals:    1,
			asyncCallIdx: 2,
			wantLive:     []uint32{}, // local 0 redefined before use
		},
		{
			name: "param used after call",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
				{Opcode: wasm.OpI32Add},
				{Opcode: wasm.OpEnd},
			},
			numParams:    2,
			numLocals:    0,
			asyncCallIdx: 0,
			wantLive:     []uint32{0, 1}, // both params live
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			la := NewLivenessAnalyzer(tt.numParams, tt.numLocals)
			got := la.LiveAtInstruction(tt.instrs, tt.asyncCallIdx)

			if !equalUint32Sets(got, tt.wantLive) {
				t.Errorf("LiveAtInstruction() = %v, want %v", got, tt.wantLive)
			}
		})
	}
}

// TestLivenessAnalysis_LocalTee tests local.tee handling.
func TestLivenessAnalysis_LocalTee(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}}, // defines 0, keeps value
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpEnd},
	}

	la := NewLivenessAnalyzer(0, 1)
	got := la.LiveAtInstruction(instrs, 3)

	want := []uint32{0}
	if !equalUint32Sets(got, want) {
		t.Errorf("LiveAtInstruction() = %v, want %v", got, want)
	}
}

// TestLivenessAnalysis_MultipleAsyncCalls tests with multiple async points.
func TestLivenessAnalysis_MultipleAsyncCalls(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // first async call
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 2}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // second async call
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 2}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpEnd},
	}

	la := NewLivenessAnalyzer(0, 3)

	// At first call (idx 4): local 0 used after, local 1 used after second call
	got1 := la.LiveAtInstruction(instrs, 4)
	want1 := []uint32{0, 1}
	if !equalUint32Sets(got1, want1) {
		t.Errorf("first call: LiveAtInstruction() = %v, want %v", got1, want1)
	}

	// At second call (idx 7): local 1 and 2 used after
	got2 := la.LiveAtInstruction(instrs, 7)
	want2 := []uint32{1, 2}
	if !equalUint32Sets(got2, want2) {
		t.Errorf("second call: LiveAtInstruction() = %v, want %v", got2, want2)
	}
}

// TestLivenessAnalysis_ControlFlow tests basic if/else handling.
func TestLivenessAnalysis_ControlFlow(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},         // void block
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}}, // use in if branch
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpElse},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}}, // use in else branch
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	la := NewLivenessAnalyzer(0, 2)
	got := la.LiveAtInstruction(instrs, 4)

	// Both locals are live because they might be used in either branch
	want := []uint32{0, 1}
	if !equalUint32Sets(got, want) {
		t.Errorf("LiveAtInstruction() = %v, want %v", got, want)
	}
}

// TestLivenessAnalysis_Loop tests loop handling.
func TestLivenessAnalysis_Loop(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 10}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}}, // counter
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},       // void block
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},       // async call in loop
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Sub},
		{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}}, // br_if back to loop
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}

	la := NewLivenessAnalyzer(0, 1)
	got := la.LiveAtInstruction(instrs, 3)

	// Local 0 is live - used after the call and potentially in loop iterations
	want := []uint32{0}
	if !equalUint32Sets(got, want) {
		t.Errorf("LiveAtInstruction() = %v, want %v", got, want)
	}
}

// TestLivenessInfo_AllCallSites tests computing liveness for all async calls.
func TestLivenessInfo_AllCallSites(t *testing.T) {
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // call 1
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // call 2
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpEnd},
	}

	asyncCalls := []int{4, 7}
	la := NewLivenessAnalyzer(0, 2)
	info := la.ComputeForCallSites(instrs, asyncCalls)

	// Call at idx 4: local 0 is live (used immediately after), local 1 live (used after call 2)
	if got, want := info[4], []uint32{0, 1}; !equalUint32Sets(got, want) {
		t.Errorf("call 4: live = %v, want %v", got, want)
	}

	// Call at idx 7: only local 1 is live
	if got, want := info[7], []uint32{1}; !equalUint32Sets(got, want) {
		t.Errorf("call 7: live = %v, want %v", got, want)
	}
}

func equalUint32Sets(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	setA := make(map[uint32]bool)
	for _, v := range a {
		setA[v] = true
	}
	for _, v := range b {
		if !setA[v] {
			return false
		}
	}
	return true
}
