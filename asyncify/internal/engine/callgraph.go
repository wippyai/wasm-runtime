package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

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
			if instr.Opcode == wasm.OpCall {
				if imm, ok := instr.Imm.(wasm.CallImm); ok {
					cg[callerIdx] = appendUnique(cg[callerIdx], imm.FuncIdx)
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
	result := make(map[uint32]bool)

	// Copy initial targets
	for t := range targets {
		result[t] = true
	}

	// Fixed-point iteration: keep expanding until no changes
	changed := true
	for changed {
		changed = false
		for caller, callees := range cg {
			if result[caller] {
				continue // already marked
			}
			for _, callee := range callees {
				if result[callee] {
					result[caller] = true
					changed = true
					break
				}
			}
		}
	}

	return result
}

// TransitiveCallees finds all functions that are transitively called by any of the sources.
//
// Starting from a set of source functions, this walks the call graph forward
// to find all functions that could be reached from them.
func (cg CallGraph) TransitiveCallees(sources map[uint32]bool) map[uint32]bool {
	result := make(map[uint32]bool)

	// Copy initial sources
	for s := range sources {
		result[s] = true
	}

	// Fixed-point iteration: keep expanding until no changes
	changed := true
	for changed {
		changed = false
		for caller := range result {
			for _, callee := range cg[caller] {
				if !result[callee] {
					result[callee] = true
					changed = true
				}
			}
		}
	}

	return result
}

func appendUnique(slice []uint32, val uint32) []uint32 {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}
