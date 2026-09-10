// Liveness analysis for asyncify local save optimization.
//
// Reference: Binaryen asyncify pass
// https://github.com/WebAssembly/binaryen/blob/main/src/passes/Asyncify.cpp
//
// A local is LIVE at a program point if there exists a path from that point
// to a use of the local that does not pass through a definition of that local.
//
// Algorithm: backward dataflow on a basic-block CFG.
// liveOut[B] = ⋃ liveIn[S] for successors S of B
// liveIn[B]  = use[B] ∪ (liveOut[B] − def[B])
// Fixed-point worklist for loops and other join/back edges.
// Call-site answers are live-after-call.
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
// Returns a map from instruction index to locals live immediately after that instruction.
func (la *LivenessAnalyzer) ComputeForCallSites(instrs []wasm.Instruction, callSites []int) map[int][]uint32 {
	if len(callSites) == 0 {
		return nil
	}

	n := len(instrs)
	maxSite := 0
	valid := 0
	for _, idx := range callSites {
		if idx < 0 || idx >= n {
			continue
		}
		valid++
		if idx > maxSite {
			maxSite = idx
		}
	}
	result := make(map[int][]uint32, len(callSites))
	if n == 0 || valid == 0 {
		return result
	}

	siteSet := NewBitSet(maxSite)
	for _, idx := range callSites {
		if idx >= 0 && idx < n {
			siteSet.Set(uint32(idx))
		}
	}

	if !needsCFG(instrs) {
		la.straightLine(instrs, siteSet, result)
		return result
	}

	la.cfgLiveness(instrs, siteSet, result)
	return result
}

func (la *LivenessAnalyzer) straightLine(instrs []wasm.Instruction, siteSet *BitSet, result map[int][]uint32) {
	live := NewBitSet(la.numLocals - 1)
	for i := len(instrs) - 1; i >= 0; i-- {
		if siteSet.Has(uint32(i)) {
			result[i] = live.ToSlice()
		}
		la.applyTransfer(instrs[i], live)
	}
}

func (la *LivenessAnalyzer) cfgLiveness(instrs []wasm.Instruction, siteSet *BitSet, result map[int][]uint32) {
	frames, inner, frameByStart := parseControl(instrs)
	blocks := buildBlocks(instrs, frames, inner, frameByStart)
	if len(blocks) == 0 {
		return
	}
	la.initBlockSets(blocks)
	la.scanUseDef(instrs, blocks)
	la.solve(blocks)
	la.collectSites(instrs, blocks, siteSet, result)
}

func (la *LivenessAnalyzer) applyTransfer(instr wasm.Instruction, live *BitSet) {
	switch instr.Opcode {
	case wasm.OpLocalGet:
		if idx, ok := localIndex(instr, la.numLocals); ok {
			live.Set(idx)
		}
	case wasm.OpLocalSet, wasm.OpLocalTee:
		if idx, ok := localIndex(instr, la.numLocals); ok {
			live.Clear(idx)
		}
	}
}

// Allocate analysis sets together; individual blocks borrow disjoint slices.
// This avoids eight heap allocations per block for bitsets and their backing.
func (la *LivenessAnalyzer) initBlockSets(blocks []liveBlock) {
	words := (la.numLocals + 63) / 64
	sets := make([]BitSet, len(blocks)*4)
	storage := make([]uint64, len(sets)*words)
	for i := range sets {
		start, end := i*words, (i+1)*words
		sets[i].bits = storage[start:end:end]
	}
	for i := range blocks {
		blocks[i].gen = &sets[4*i]
		blocks[i].kill = &sets[4*i+1]
		blocks[i].liveIn = &sets[4*i+2]
		blocks[i].liveOut = &sets[4*i+3]
	}
}

func (la *LivenessAnalyzer) scanUseDef(instrs []wasm.Instruction, blocks []liveBlock) {
	for i := range blocks {
		b := &blocks[i]
		for pc := b.start; pc <= b.end; pc++ {
			switch instrs[pc].Opcode {
			case wasm.OpLocalGet:
				if idx, ok := localIndex(instrs[pc], la.numLocals); ok && !b.kill.Has(idx) {
					b.gen.Set(idx)
				}
			case wasm.OpLocalSet, wasm.OpLocalTee:
				if idx, ok := localIndex(instrs[pc], la.numLocals); ok {
					b.kill.Set(idx)
				}
			}
		}
	}
}

func (la *LivenessAnalyzer) solve(blocks []liveBlock) {
	nblocks := len(blocks)

	queue := make([]int, 0, nblocks)
	inQ := make([]bool, nblocks)
	for i := 0; i < nblocks; i++ {
		queue = append(queue, i)
		inQ[i] = true
	}

	newOut := NewBitSet(la.numLocals - 1)
	newIn := NewBitSet(la.numLocals - 1)
	for len(queue) > 0 {
		bi := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		inQ[bi] = false
		b := &blocks[bi]

		newOut.Reset()
		for _, s := range b.succs {
			newOut.Union(blocks[s].liveIn)
		}
		bitsetAssign(b.liveOut, newOut)

		bitsetAssign(newIn, newOut)
		bitsetSubtract(newIn, b.kill)
		newIn.Union(b.gen)
		if bitsetEqual(b.liveIn, newIn) {
			continue
		}
		bitsetAssign(b.liveIn, newIn)
		for _, p := range b.preds {
			if !inQ[p] {
				inQ[p] = true
				queue = append(queue, p)
			}
		}
	}
}

func (la *LivenessAnalyzer) collectSites(instrs []wasm.Instruction, blocks []liveBlock, siteSet *BitSet, result map[int][]uint32) {
	live := NewBitSet(la.numLocals - 1)
	for i := range blocks {
		b := &blocks[i]
		bitsetAssign(live, b.liveOut)
		for pc := b.end; pc >= b.start; pc-- {
			if siteSet.Has(uint32(pc)) {
				result[pc] = live.ToSlice()
			}
			la.applyTransfer(instrs[pc], live)
		}
	}
}

func localIndex(instr wasm.Instruction, numLocals int) (uint32, bool) {
	imm, ok := instr.Imm.(wasm.LocalImm)
	if !ok || numLocals <= 0 || imm.LocalIdx >= uint32(numLocals) {
		return 0, false
	}
	return imm.LocalIdx, true
}

func needsCFG(instrs []wasm.Instruction) bool {
	last := len(instrs) - 1
	for i, instr := range instrs {
		switch instr.Opcode {
		case wasm.OpEnd:
			if i != last {
				return true
			}
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpElse,
			wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable, wasm.OpReturn, wasm.OpUnreachable,
			wasm.OpTry, wasm.OpCatch, wasm.OpCatchAll, wasm.OpThrow, wasm.OpThrowRef,
			wasm.OpRethrow, wasm.OpDelegate, wasm.OpTryTable,
			wasm.OpBrOnNull, wasm.OpBrOnNonNull,
			wasm.OpReturnCall, wasm.OpReturnCallIndirect, wasm.OpReturnCallRef:
			return true
		case wasm.OpPrefixGC:
			if isBrOnCast(instr) {
				return true
			}
		}
	}
	return false
}

const (
	frameBlock uint8 = iota
	frameLoop
	frameIf
	frameTry
	frameTryTable
)

type liveFrame struct {
	catches []int
	kind    uint8
	start   int
	elseIdx int
	endIdx  int
	parent  int
}

func (f liveFrame) branchTarget() int {
	if f.kind == frameLoop {
		return f.start
	}
	return f.endIdx
}

type liveBlock struct {
	gen, kill       *BitSet
	liveIn, liveOut *BitSet
	succs, preds    []int
	start, end      int
}

func parseControl(instrs []wasm.Instruction) (frames []liveFrame, inner []int, frameByStart []int) {
	n := len(instrs)
	inner = make([]int, n)
	frameByStart = make([]int, n)
	for i := 0; i < n; i++ {
		inner[i] = -1
		frameByStart[i] = -1
	}
	stack := make([]int, 0, 8)

	push := func(kind uint8, start int) {
		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		frames = append(frames, liveFrame{
			kind:    kind,
			start:   start,
			elseIdx: -1,
			endIdx:  -1,
			parent:  parent,
		})
		id := len(frames) - 1
		frameByStart[start] = id
		stack = append(stack, id)
	}

	for i, instr := range instrs {
		if len(stack) > 0 {
			inner[i] = stack[len(stack)-1]
		}
		switch instr.Opcode {
		case wasm.OpBlock:
			push(frameBlock, i)
		case wasm.OpLoop:
			push(frameLoop, i)
		case wasm.OpIf:
			push(frameIf, i)
		case wasm.OpTry:
			push(frameTry, i)
		case wasm.OpTryTable:
			push(frameTryTable, i)
		case wasm.OpElse:
			if len(stack) > 0 {
				f := &frames[stack[len(stack)-1]]
				if f.kind == frameIf && f.elseIdx < 0 {
					f.elseIdx = i
				}
			}
		case wasm.OpCatch, wasm.OpCatchAll:
			if len(stack) > 0 {
				f := &frames[stack[len(stack)-1]]
				if f.kind == frameTry {
					f.catches = append(f.catches, i)
				}
			}
		case wasm.OpEnd, wasm.OpDelegate:
			if len(stack) > 0 {
				fi := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				frames[fi].endIdx = i
			}
		}
	}
	return frames, inner, frameByStart
}

func buildBlocks(instrs []wasm.Instruction, frames []liveFrame, inner, frameByStart []int) []liveBlock {
	n := len(instrs)
	if n == 0 {
		return nil
	}
	leaders := markLeaders(instrs, frames)
	blocks := splitBlocks(leaders)
	blockOf := make([]int, n)
	for i := range blockOf {
		blockOf[i] = -1
	}
	for id := range blocks {
		for pc := blocks[id].start; pc <= blocks[id].end; pc++ {
			blockOf[pc] = id
		}
	}

	addEdge := func(from, toInstr int) {
		if from < 0 || toInstr < 0 || toInstr >= n {
			return
		}
		to := blockOf[toInstr]
		if to < 0 {
			return
		}
		if !containsInt(blocks[from].succs, to) {
			blocks[from].succs = append(blocks[from].succs, to)
			blocks[to].preds = append(blocks[to].preds, from)
		}
	}

	resolve := func(fromFrame int, label uint32) int {
		return resolveLabel(frames, fromFrame, label)
	}

	addFallthrough := func(from, i int) {
		next := i + 1
		if next >= n {
			return
		}
		switch instrs[next].Opcode {
		case wasm.OpElse, wasm.OpCatch, wasm.OpCatchAll:
			f := inner[next]
			if f >= 0 {
				addEdge(from, frames[f].endIdx)
			}
			return
		}
		addEdge(from, next)
	}

	addExceptions := func(from, i int) {
		for f := inner[i]; f >= 0; f = frames[f].parent {
			switch frames[f].kind {
			case frameTry:
				for _, c := range frames[f].catches {
					addEdge(from, c)
				}
			case frameTryTable:
				start := frames[f].start
				if start < 0 || start >= n {
					continue
				}
				imm, ok := instrs[start].Imm.(wasm.TryTableImm)
				if !ok {
					continue
				}
				for _, c := range imm.Catches {
					addEdge(from, resolve(f, c.LabelIdx))
				}
			}
		}
	}

	for id := range blocks {
		i := blocks[id].end
		instr := instrs[i]
		switch instr.Opcode {
		case wasm.OpUnreachable, wasm.OpReturn,
			wasm.OpReturnCall, wasm.OpReturnCallIndirect, wasm.OpReturnCallRef:
			// no successors
		case wasm.OpBr:
			if imm, ok := instr.Imm.(wasm.BranchImm); ok {
				addEdge(id, resolve(inner[i], imm.LabelIdx))
			}
		case wasm.OpBrIf, wasm.OpBrOnNull, wasm.OpBrOnNonNull:
			if imm, ok := instr.Imm.(wasm.BranchImm); ok {
				addEdge(id, resolve(inner[i], imm.LabelIdx))
			}
			addFallthrough(id, i)
		case wasm.OpBrTable:
			if imm, ok := instr.Imm.(wasm.BrTableImm); ok {
				for _, label := range imm.Labels {
					addEdge(id, resolve(inner[i], label))
				}
				addEdge(id, resolve(inner[i], imm.Default))
			}
		case wasm.OpIf:
			fidx := frameByStart[i]
			if fidx < 0 {
				addFallthrough(id, i)
				break
			}
			f := frames[fidx]
			thenIdx := i + 1
			if f.elseIdx >= 0 {
				if thenIdx != f.elseIdx {
					addEdge(id, thenIdx)
				} else {
					addEdge(id, f.endIdx)
				}
				addEdge(id, f.elseIdx)
			} else {
				addEdge(id, thenIdx)
				addEdge(id, f.endIdx)
			}
		case wasm.OpThrow, wasm.OpThrowRef, wasm.OpRethrow:
			addExceptions(id, i)
		case wasm.OpCall, wasm.OpCallIndirect, wasm.OpCallRef:
			addFallthrough(id, i)
			addExceptions(id, i)
		case wasm.OpPrefixGC:
			if isBrOnCast(instr) {
				if imm, ok := instr.Imm.(wasm.GCImm); ok {
					addEdge(id, resolve(inner[i], imm.LabelIdx))
				}
				addFallthrough(id, i)
			} else {
				addFallthrough(id, i)
			}
		default:
			addFallthrough(id, i)
		}
	}
	return blocks
}

func markLeaders(instrs []wasm.Instruction, frames []liveFrame) []bool {
	n := len(instrs)
	leaders := make([]bool, n)
	leaders[0] = true
	mark := func(idx int) {
		if idx >= 0 && idx < n {
			leaders[idx] = true
		}
	}
	for i, instr := range instrs {
		switch instr.Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpElse, wasm.OpEnd,
			wasm.OpTry, wasm.OpTryTable, wasm.OpCatch, wasm.OpCatchAll, wasm.OpDelegate,
			wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable, wasm.OpReturn, wasm.OpUnreachable,
			wasm.OpThrow, wasm.OpThrowRef, wasm.OpRethrow,
			wasm.OpReturnCall, wasm.OpReturnCallIndirect, wasm.OpReturnCallRef,
			wasm.OpBrOnNull, wasm.OpBrOnNonNull,
			wasm.OpCall, wasm.OpCallIndirect, wasm.OpCallRef:
			mark(i)
			mark(i + 1)
		case wasm.OpPrefixGC:
			if isBrOnCast(instr) {
				mark(i)
				mark(i + 1)
			}
		}
	}
	for _, f := range frames {
		mark(f.start)
		mark(f.start + 1)
		mark(f.elseIdx)
		mark(f.endIdx)
		mark(f.endIdx + 1)
		for _, c := range f.catches {
			mark(c)
		}
	}
	return leaders
}

func splitBlocks(leaders []bool) []liveBlock {
	n := len(leaders)
	count := 0
	for _, leader := range leaders {
		if leader {
			count++
		}
	}
	blocks := make([]liveBlock, 0, count)
	start := 0
	for i := 1; i <= n; i++ {
		if i == n || leaders[i] {
			blocks = append(blocks, liveBlock{start: start, end: i - 1})
			start = i
		}
	}
	return blocks
}

func resolveLabel(frames []liveFrame, from int, label uint32) int {
	f := from
	for label > 0 {
		if f < 0 || f >= len(frames) {
			return -1
		}
		f = frames[f].parent
		label--
	}
	if f < 0 || f >= len(frames) {
		return -1
	}
	return frames[f].branchTarget()
}

func isBrOnCast(instr wasm.Instruction) bool {
	imm, ok := instr.Imm.(wasm.GCImm)
	if !ok {
		return false
	}
	return imm.SubOpcode == wasm.GCBrOnCast || imm.SubOpcode == wasm.GCBrOnCastFail
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func bitsetAssign(dst, src *BitSet) {
	if len(dst.bits) < len(src.bits) {
		dst.bits = make([]uint64, len(src.bits))
	}
	copy(dst.bits, src.bits)
	for i := len(src.bits); i < len(dst.bits); i++ {
		dst.bits[i] = 0
	}
}

func bitsetEqual(a, b *BitSet) bool {
	n, m := len(a.bits), len(b.bits)
	max := n
	if m > max {
		max = m
	}
	for i := 0; i < max; i++ {
		var aw, bw uint64
		if i < n {
			aw = a.bits[i]
		}
		if i < m {
			bw = b.bits[i]
		}
		if aw != bw {
			return false
		}
	}
	return true
}

func bitsetSubtract(dst, kill *BitSet) {
	n := len(dst.bits)
	if len(kill.bits) < n {
		n = len(kill.bits)
	}
	for i := 0; i < n; i++ {
		dst.bits[i] &^= kill.bits[i]
	}
}
