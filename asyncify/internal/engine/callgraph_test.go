package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TestBuildCallGraph_Empty tests empty module.
func TestBuildCallGraph_Empty(t *testing.T) {
	m := &wasm.Module{}
	cg, err := BuildCallGraph(m)
	if err != nil {
		t.Fatalf("BuildCallGraph: %v", err)
	}

	if len(cg) != 0 {
		t.Errorf("BuildCallGraph(empty) = %d entries, want 0", len(cg))
	}
}

// TestBuildCallGraph_SingleCall tests module with one function calling another.
func TestBuildCallGraph_SingleCall(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "imported", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // call imported
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	cg, err := BuildCallGraph(m)
	if err != nil {
		t.Fatalf("BuildCallGraph: %v", err)
	}

	// Function 1 (first local func) should call function 0 (imported)
	if callees := cg[1]; len(callees) != 1 || callees[0] != 0 {
		t.Errorf("BuildCallGraph: func 1 callees = %v, want [0]", callees)
	}
}

// TestBuildCallGraph_MultipleCallees tests function calling multiple functions.
func TestBuildCallGraph_MultipleCallees(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "a", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "b", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // duplicate
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	cg, err := BuildCallGraph(m)
	if err != nil {
		t.Fatalf("BuildCallGraph: %v", err)
	}

	// Function 2 should call 0 and 1 (no duplicates)
	callees := cg[2]
	if len(callees) != 2 {
		t.Errorf("BuildCallGraph: func 2 has %d callees, want 2", len(callees))
	}
}

// TestBuildCallGraph_CallChain tests A -> B -> C chain.
func TestBuildCallGraph_CallChain(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "c", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0, 0}, // B and A
		Code: []wasm.FuncBody{
			{ // B calls C (func 0)
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
			{ // A calls B (func 1)
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	cg, err := BuildCallGraph(m)
	if err != nil {
		t.Fatalf("BuildCallGraph: %v", err)
	}

	// B (func 1) calls C (func 0)
	if callees := cg[1]; len(callees) != 1 || callees[0] != 0 {
		t.Errorf("B callees = %v, want [0]", callees)
	}

	// A (func 2) calls B (func 1)
	if callees := cg[2]; len(callees) != 1 || callees[0] != 1 {
		t.Errorf("A callees = %v, want [1]", callees)
	}
}

// TestTransitiveCallers_Direct tests direct caller identification.
func TestTransitiveCallers_Direct(t *testing.T) {
	cg := CallGraph{
		1: {0}, // func 1 calls func 0
	}

	targets := map[uint32]bool{0: true}
	result := cg.TransitiveCallers(targets)

	if !result[0] {
		t.Error("target 0 should be in result")
	}
	if !result[1] {
		t.Error("direct caller 1 should be in result")
	}
}

// TestTransitiveCallers_Chain tests transitive caller identification.
func TestTransitiveCallers_Chain(t *testing.T) {
	// A (2) -> B (1) -> C (0)
	cg := CallGraph{
		1: {0}, // B calls C
		2: {1}, // A calls B
	}

	targets := map[uint32]bool{0: true}
	result := cg.TransitiveCallers(targets)

	if !result[0] {
		t.Error("target 0 should be in result")
	}
	if !result[1] {
		t.Error("B (1) should be in result (direct caller)")
	}
	if !result[2] {
		t.Error("A (2) should be in result (transitive caller)")
	}
}

// TestTransitiveCallers_Diamond tests diamond call pattern.
func TestTransitiveCallers_Diamond(t *testing.T) {
	//     D (3)
	//    /   \
	//   B(1)  C(2)
	//    \   /
	//     A (0)
	cg := CallGraph{
		1: {0},    // B calls A
		2: {0},    // C calls A
		3: {1, 2}, // D calls B and C
	}

	targets := map[uint32]bool{0: true}
	result := cg.TransitiveCallers(targets)

	for i := uint32(0); i <= 3; i++ {
		if !result[i] {
			t.Errorf("func %d should be in result", i)
		}
	}
}

// TestTransitiveCallers_Isolated tests isolated functions.
func TestTransitiveCallers_Isolated(t *testing.T) {
	cg := CallGraph{
		1: {0}, // func 1 calls func 0
		2: {3}, // func 2 calls func 3 (not target)
	}

	targets := map[uint32]bool{0: true}
	result := cg.TransitiveCallers(targets)

	if !result[0] {
		t.Error("target 0 should be in result")
	}
	if !result[1] {
		t.Error("caller 1 should be in result")
	}
	if result[2] {
		t.Error("isolated func 2 should NOT be in result")
	}
	if result[3] {
		t.Error("func 3 should NOT be in result")
	}
}

// TestTransitiveCallers_Cycle tests cyclic call graph.
func TestTransitiveCallers_Cycle(t *testing.T) {
	// A (1) <-> B (2), B calls C (0)
	cg := CallGraph{
		1: {2},    // A calls B
		2: {0, 1}, // B calls C and A (cycle)
	}

	targets := map[uint32]bool{0: true}
	result := cg.TransitiveCallers(targets)

	if !result[0] {
		t.Error("target 0 should be in result")
	}
	if !result[1] {
		t.Error("A (1) should be in result")
	}
	if !result[2] {
		t.Error("B (2) should be in result")
	}
}

// TestAppendUnique tests the helper function.
func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name  string
		slice []uint32
		val   uint32
		want  int // expected length
	}{
		{"empty", nil, 1, 1},
		{"add new", []uint32{1, 2}, 3, 3},
		{"duplicate", []uint32{1, 2, 3}, 2, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUnique(tt.slice, tt.val)
			if len(got) != tt.want {
				t.Errorf("appendUnique() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

// TestTransitiveCallees_Direct tests direct callee identification.
func TestTransitiveCallees_Direct(t *testing.T) {
	cg := CallGraph{
		1: {0, 2}, // func 1 calls func 0 and 2
	}

	sources := map[uint32]bool{1: true}
	result := cg.TransitiveCallees(sources)

	if !result[1] {
		t.Error("source 1 should be in result")
	}
	if !result[0] {
		t.Error("direct callee 0 should be in result")
	}
	if !result[2] {
		t.Error("direct callee 2 should be in result")
	}
}

// TestTransitiveCallees_Chain tests transitive callee identification.
func TestTransitiveCallees_Chain(t *testing.T) {
	// A (2) -> B (1) -> C (0)
	cg := CallGraph{
		2: {1}, // A calls B
		1: {0}, // B calls C
	}

	sources := map[uint32]bool{2: true}
	result := cg.TransitiveCallees(sources)

	if !result[2] {
		t.Error("source A (2) should be in result")
	}
	if !result[1] {
		t.Error("B (1) should be in result (direct callee)")
	}
	if !result[0] {
		t.Error("C (0) should be in result (transitive callee)")
	}
}

// TestTransitiveCallees_Isolated tests isolated functions.
func TestTransitiveCallees_Isolated(t *testing.T) {
	cg := CallGraph{
		1: {0}, // func 1 calls func 0
		3: {4}, // func 3 calls func 4 (not reachable from source)
	}

	sources := map[uint32]bool{1: true}
	result := cg.TransitiveCallees(sources)

	if !result[1] {
		t.Error("source 1 should be in result")
	}
	if !result[0] {
		t.Error("callee 0 should be in result")
	}
	if result[3] {
		t.Error("unreachable func 3 should NOT be in result")
	}
	if result[4] {
		t.Error("unreachable func 4 should NOT be in result")
	}
}
