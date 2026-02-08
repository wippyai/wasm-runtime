package ir

import "github.com/wippyai/wasm-runtime/wasm"

// TransformConfig holds configuration for the asyncify tree transform.
type TransformConfig struct {
	AsyncFuncs  map[uint32]bool
	Module      *wasm.Module
	StateGlobal uint32
	DataGlobal  uint32
}

// TransformResult contains the output of tree transformation.
type TransformResult struct {
	CallSites        []int
	NeedsTransform   bool
	HasAsyncInBranch bool
}

// Analyze checks if the tree needs asyncify transformation.
func Analyze(tree Node, config *TransformConfig) *TransformResult {
	a := &analyzer{config: config}
	a.analyze(tree, 0)
	return &TransformResult{
		NeedsTransform:   len(a.callSites) > 0,
		CallSites:        a.callSites,
		HasAsyncInBranch: a.hasAsyncInBranch,
	}
}

type analyzer struct {
	config           *TransformConfig
	callSites        []int
	instrIndex       int
	hasAsyncInBranch bool
}

func (a *analyzer) analyze(node Node, depth int) {
	switch n := node.(type) {
	case *SeqNode:
		for _, child := range n.Children {
			a.analyze(child, depth)
		}
	case *BlockNode:
		a.analyze(n.Body, depth+1)
	case *IfNode:
		thenHasAsync := a.branchHasAsync(n.Then)
		elseHasAsync := n.Else != nil && a.branchHasAsync(n.Else)
		if thenHasAsync || elseHasAsync {
			a.hasAsyncInBranch = true
		}
		a.analyze(n.Then, depth+1)
		if n.Else != nil {
			a.analyze(n.Else, depth+1)
		}
	case *InstrNode:
		if a.isAsyncCall(n.Instr) {
			a.callSites = append(a.callSites, a.instrIndex)
		}
		a.instrIndex++
	}
}

func (a *analyzer) branchHasAsync(node Node) bool {
	return branchHasAsync(node, a.config.AsyncFuncs)
}

func (a *analyzer) isAsyncCall(instr wasm.Instruction) bool {
	if instr.Opcode == wasm.OpCall {
		if imm, ok := instr.Imm.(wasm.CallImm); ok {
			return a.config.AsyncFuncs[imm.FuncIdx]
		}
	}
	if instr.Opcode == wasm.OpCallIndirect {
		return true // indirect calls are async by default
	}
	if instr.Opcode == wasm.OpCallRef {
		return true // call_ref is async (dynamic dispatch)
	}
	return false
}
