// Liveness analysis for asyncify local save optimization.
//
// Reference: Binaryen asyncify pass
// https://github.com/WebAssembly/binaryen/blob/main/src/passes/Asyncify.cpp
//
// Binaryen computes liveness to optimize which locals are saved at each unwind
// point. Without this, all locals are saved/restored at every async call,
// which wastes memory and CPU cycles.
//
// A local is LIVE at a program point if there exists a path from that point
// to a use of the local that doesn't pass through a definition of that local.
//
// Algorithm (backward dataflow analysis):
// 1. Start from each use of a local, mark it live
// 2. Propagate backward until we hit a definition of that local
// 3. At control flow merges, union the live sets from all successors
//
// Optimization: O(n) single-pass algorithm
// Build CFG once, compute liveness at all points in single backward pass,
// then return results for requested call sites.
package engine

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// LivenessAnalyzer computes live locals at program points.
type LivenessAnalyzer struct {
	numLocals int
}

// NewLivenessAnalyzer creates an analyzer for a function with given local count.
// numParams and numLocals together give total locals (params are locals 0..numParams-1).
func NewLivenessAnalyzer(numParams, numLocals int) *LivenessAnalyzer {
	return &LivenessAnalyzer{
		numLocals: numParams + numLocals,
	}
}

// LiveAtInstruction computes which locals are live immediately after the given instruction.
// Wrapper for single call site queries.
func (la *LivenessAnalyzer) LiveAtInstruction(instrs []wasm.Instruction, instrIdx int) []uint32 {
	result := la.ComputeForCallSites(instrs, []int{instrIdx})
	return result[instrIdx]
}

// ComputeForCallSites computes liveness for multiple async call sites at once.
// Returns a map from instruction index to live locals at that point.
// O(n) algorithm: builds CFG once, computes liveness at all points in single pass.
func (la *LivenessAnalyzer) ComputeForCallSites(instrs []wasm.Instruction, callSites []int) map[int][]uint32 {
	if len(callSites) == 0 {
		return nil
	}

	// Build call site lookup set using bitset for O(1) lookup
	maxSite := 0
	for _, idx := range callSites {
		if idx > maxSite {
			maxSite = idx
		}
	}
	siteSet := NewBitSet(maxSite + 1)
	for _, idx := range callSites {
		siteSet.Set(uint32(idx))
	}

	// Build CFG once
	cfg := la.buildCFG(instrs)

	// Single backward pass computing liveness at each call site
	result := make(map[int][]uint32, len(callSites))
	live := NewBitSet(la.numLocals)

	// First pass: collect all locals used in loops (conservative for back edges)
	loopLocals := la.collectLoopLocalsBitset(instrs, cfg)

	// Backward pass from end to start
	for i := len(instrs) - 1; i >= 0; i-- {
		instr := instrs[i]

		// Apply transfer function
		la.applyTransferBitset(instr, live)

		// At loop headers, add all loop locals
		if loopSet, isLoopHeader := loopLocals[i]; isLoopHeader {
			live.Union(loopSet)
		}

		// Record liveness at call sites (before the call)
		if siteSet.Has(uint32(i)) {
			result[i] = live.ToSlice()
		}
	}

	return result
}

// cfgInfo holds control flow graph information.
type cfgInfo struct {
	// blockEnds maps block start index to end index
	blockEnds map[int]int
	// loopBodies maps loop start to (start, end) range
	loopBodies map[int][2]int
}

// buildCFG builds basic control flow information.
func (la *LivenessAnalyzer) buildCFG(instrs []wasm.Instruction) *cfgInfo {
	cfg := &cfgInfo{
		blockEnds:  make(map[int]int),
		loopBodies: make(map[int][2]int),
	}

	// Stack of block start indices
	var blockStack []struct {
		start  int
		isLoop bool
	}

	for i, instr := range instrs {
		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpIf:
			blockStack = append(blockStack, struct {
				start  int
				isLoop bool
			}{i, false})

		case wasm.OpLoop:
			blockStack = append(blockStack, struct {
				start  int
				isLoop bool
			}{i, true})

		case wasm.OpElse:
			// else is in the middle of if block, don't pop

		case wasm.OpEnd:
			if len(blockStack) > 0 {
				block := blockStack[len(blockStack)-1]
				blockStack = blockStack[:len(blockStack)-1]
				cfg.blockEnds[block.start] = i
				if block.isLoop {
					cfg.loopBodies[block.start] = [2]int{block.start + 1, i}
				}
			}
		}
	}

	return cfg
}

// applyTransferBitset applies the transfer function for liveness at an instruction.
// For backward analysis: uses add to live set, defs remove from live set.
func (la *LivenessAnalyzer) applyTransferBitset(instr wasm.Instruction, live *BitSet) {
	switch instr.Opcode {
	case wasm.OpLocalGet:
		if imm, ok := instr.Imm.(wasm.LocalImm); ok {
			live.Set(imm.LocalIdx)
		}
	case wasm.OpLocalSet:
		if imm, ok := instr.Imm.(wasm.LocalImm); ok {
			live.Clear(imm.LocalIdx)
		}
		// Note: OpLocalTee intentionally omitted - it writes to local but value
		// flows through stack, so the local's previous liveness state should persist
	}
}

// collectLoopLocalsBitset collects all locals referenced within each loop.
// For loops with async calls, all locals used or defined in the loop must be live
// at the loop header due to potential back edges.
func (la *LivenessAnalyzer) collectLoopLocalsBitset(instrs []wasm.Instruction, cfg *cfgInfo) map[int]*BitSet {
	result := make(map[int]*BitSet)

	for loopStart, bounds := range cfg.loopBodies {
		locals := NewBitSet(la.numLocals)
		for i := bounds[0]; i < bounds[1]; i++ {
			switch instrs[i].Opcode {
			case wasm.OpLocalGet, wasm.OpLocalSet, wasm.OpLocalTee:
				if imm, ok := instrs[i].Imm.(wasm.LocalImm); ok {
					locals.Set(imm.LocalIdx)
				}
			}
		}
		result[loopStart] = locals
	}

	return result
}
