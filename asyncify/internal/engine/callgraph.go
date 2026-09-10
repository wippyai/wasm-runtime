package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// GroupNodeBase is the threshold above which node IDs in CallGraph are treated
// as synthetic intermediate group nodes (e.g. for (table, signature) indirect call clusters).
// Group nodes are traversed during BFS reachability analysis but excluded from final
// function caller/callee sets.
const GroupNodeBase uint32 = 0x80000000

// CallGraph represents the function call relationships in a module.
// Maps each function index to the list of functions it directly calls.
type CallGraph map[uint32][]uint32

// BuildCallGraph analyzes a module and constructs a call graph.
//
// The resulting graph maps function indices to the functions they call.
// This is used to find which functions transitively call async imports.
func BuildCallGraph(m *wasm.Module) (CallGraph, error) {
	cg := make(CallGraph)
	numImported := uint32(m.NumImportedFuncs())

	for i, body := range m.Code {
		callerIdx := numImported + uint32(i)
		instrs, err := wasm.DecodeInstructions(body.Code)
		if err != nil {
			return nil, fmt.Errorf("decode func %d: %w", callerIdx, err)
		}

		for _, instr := range instrs {
			if instr.Opcode == wasm.OpCall || instr.Opcode == wasm.OpReturnCall {
				if imm, ok := instr.Imm.(wasm.CallImm); ok {
					cg.AddEdge(callerIdx, imm.FuncIdx)
				}
			}
		}
	}

	return cg, nil
}

// TransitiveCallers finds all functions that transitively call any of the targets.
//
// Starting from a set of target functions (typically async imports), this
// walks the call graph backwards to find all callers that could reach them.
func (cg CallGraph) TransitiveCallers(targets map[uint32]bool) map[uint32]bool {
	// Reverse each edge once, then visit each reachable caller once. A fixed
	// point scan can revisit the entire graph for every level of a deep chain.
	callers := make(CallGraph)
	for caller, callees := range cg {
		for _, callee := range callees {
			callers[callee] = append(callers[callee], caller)
		}
	}
	return callers.TransitiveCallees(targets)
}

// TransitiveCallees finds all functions that are transitively called by any of the sources.
//
// Starting from a set of source functions, this walks the call graph forward
// to find all functions that could be reached from them.
func (cg CallGraph) TransitiveCallees(sources map[uint32]bool) map[uint32]bool {
	result := make(map[uint32]bool, len(sources))
	visited := make(map[uint32]bool, len(sources))
	queue := make([]uint32, 0, len(sources))
	for source := range sources {
		visited[source] = true
		if source < GroupNodeBase {
			result[source] = true
		}
		queue = append(queue, source)
	}
	for head := 0; head < len(queue); head++ {
		for _, callee := range cg[queue[head]] {
			if !visited[callee] {
				visited[callee] = true
				if callee < GroupNodeBase {
					result[callee] = true
				}
				queue = append(queue, callee)
			}
		}
	}
	return result
}

// AddEdge adds a directed edge caller -> callee if not already present.
func (cg CallGraph) AddEdge(caller, callee uint32) {
	cg[caller] = appendUnique(cg[caller], callee)
}

func appendUnique(slice []uint32, val uint32) []uint32 {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}
