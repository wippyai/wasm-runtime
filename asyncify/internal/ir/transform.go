package ir

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// SuspensionPolicy decides whether a resolved call can suspend. It is evaluated
// only during Prepare; neither the callback nor its mutable inputs are retained.
type SuspensionPolicy func(semantics.CallOperation) bool

// Analysis owns a decoded source tree and its suspension facts. Its tree is not
// exposed: lowering cannot accidentally combine facts with a different revision.
// Rewrites require preparing a new analysis. No generated routing is analyzed.
type Analysis struct {
	instructions      []*InstrNode
	root              Node
	suspends          map[Node]bool
	functionResults   []wasm.ValType
	calls             []sourceCall
	suspensionCount   int
	hasFunctionBranch bool
	hasAsyncInBranch  bool
}

// Prepare decodes its own instruction storage and resolves suspension on the
// checked source tree. It retains no mutable module or caller instruction data.
func Prepare(code []byte, module *wasm.Module, calls *semantics.Calls, policy SuspensionPolicy, functionResults []wasm.ValType) (*Analysis, error) {
	instructions, err := wasm.DecodeInstructions(code)
	if err != nil {
		return nil, err
	}
	root, err := Parse(instructions, module)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, fmt.Errorf("asyncify: missing suspension policy")
	}
	analysis := &Analysis{root: root, suspends: make(map[Node]bool), functionResults: slices.Clone(functionResults)}
	if _, err := analysis.visit(root, calls, policy); err != nil {
		return nil, err
	}
	return analysis, nil
}

func (a *Analysis) NeedsTransform() bool   { return a.suspends[a.root] }
func (a *Analysis) SuspensionCount() int   { return a.suspensionCount }
func (a *Analysis) HasAsyncInBranch() bool { return a.hasAsyncInBranch }

// visit computes each subtree's fact exactly once. All branch decisions in
// lowering consume these facts instead of rescanning instructions or opcodes.
func (a *Analysis) visit(node Node, calls *semantics.Calls, policy SuspensionPolicy) (bool, error) {
	var suspends bool
	switch n := node.(type) {
	case *SeqNode:
		for _, child := range n.Children {
			childSuspends, err := a.visit(child, calls, policy)
			if err != nil {
				return false, err
			}
			suspends = suspends || childSuspends
		}
	case *BlockNode:
		var err error
		suspends, err = a.visit(n.Body, calls, policy)
		if err != nil {
			return false, err
		}
	case *IfNode:
		thenSuspends, err := a.visit(n.Then, calls, policy)
		if err != nil {
			return false, err
		}
		var elseSuspends bool
		if n.Else != nil {
			elseSuspends, err = a.visit(n.Else, calls, policy)
			if err != nil {
				return false, err
			}
		}
		suspends = thenSuspends || elseSuspends
		a.hasAsyncInBranch = a.hasAsyncInBranch || suspends
	case *InstrNode:
		a.instructions = append(a.instructions, n)
		for _, target := range n.branchTargets {
			if target == 0 {
				a.hasFunctionBranch = true
			}
		}
		call, handled, err := calls.Resolve(n.Instr)
		if err != nil {
			return false, err
		}
		if handled {
			suspends = policy(call)
			n.callID = uint64(len(a.calls)) + 1
			a.calls = append(a.calls, sourceCall{operation: call, suspends: suspends})
			if suspends {
				a.suspensionCount++
			}
		}
	default:
		return false, fmt.Errorf("asyncify: unknown source node %T", node)
	}
	a.suspends[node] = suspends
	return suspends, nil
}
