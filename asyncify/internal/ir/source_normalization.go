package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// SourceElisionReason explains why a source instruction is absent from the
// executable view. The original node and its ordinal remain retained by the
// validated ValuePlan.
type SourceElisionReason uint8

const (
	NotElided SourceElisionReason = iota
	ElidedAfterTerminator
)

// SourceProvenance identifies a source instruction in its original decoded
// order. CallID is zero for non-call instructions and is never renumbered by
// normalization, so active calls may have gaps.
type SourceProvenance struct {
	InstructionOrdinal int
	CallID             uint64
}

// NormalizedSource is the executable view of a fully validated source plan.
// It owns reachability, elision, and active-subtree suspension facts without
// modifying the raw source tree or its ValuePlan identities.
type NormalizedSource struct {
	values             *ValuePlan
	active             map[Node]bool
	elided             map[Node]SourceElisionReason
	suspends           map[Node]bool
	ordinals           map[*InstrNode]int
	lexicalExits       map[normalizedExit]bool
	instructions       []*InstrNode
	suspensionIDs      []uint64
	hasFunctionBranch  bool
	hasAsyncInBranch   bool
	rootMayFallthrough bool
}

type normalizedExit struct {
	owner Node
	arm   ControlArm
}

// NormalizeSource derives structured runtime reachability after PlanValues has
// validated every source instruction. It intentionally does not perform a
// pure-expression DCE pass: only paths that structured control proves cannot
// execute are elided.
func NormalizeSource(values *ValuePlan) (*NormalizedSource, error) {
	if values == nil || values.control == nil {
		return nil, fmt.Errorf("asyncify: missing validated source plan")
	}
	if err := values.verifyTransfers(); err != nil {
		return nil, fmt.Errorf("asyncify: source normalization requires a valid value plan: %w", err)
	}
	result := &NormalizedSource{
		values:       values,
		active:       make(map[Node]bool),
		elided:       make(map[Node]SourceElisionReason),
		suspends:     make(map[Node]bool),
		ordinals:     make(map[*InstrNode]int, len(values.control.instructions)),
		lexicalExits: make(map[normalizedExit]bool),
	}
	for ordinal, node := range values.control.instructions {
		result.ordinals[node] = ordinal
	}
	flow, err := result.visit(values.control.root)
	if err != nil {
		return nil, err
	}
	for label := range flow.escapes {
		if label != 0 {
			return nil, fmt.Errorf("asyncify: normalized source has unconsumed branch target %d", label)
		}
	}
	result.rootMayFallthrough = flow.mayFallthrough
	if err := result.verifyValueClosure(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *NormalizedSource) NeedsTransform() bool {
	return s != nil && s.suspends[s.values.control.root]
}
func (s *NormalizedSource) SuspensionCount() int {
	if s == nil {
		return 0
	}
	return len(s.suspensionIDs)
}
func (s *NormalizedSource) HasAsyncInBranch() bool { return s != nil && s.hasAsyncInBranch }
func (s *NormalizedSource) HasFunctionBranch() bool {
	return s != nil && s.hasFunctionBranch
}
func (s *NormalizedSource) InstructionCount() int {
	if s == nil {
		return 0
	}
	return len(s.instructions)
}

// Instruction returns an owned source instruction and its immutable original
// provenance. It never exposes action or generated instruction storage.
func (s *NormalizedSource) Instruction(index int) (wasm.Instruction, SourceProvenance, bool) {
	if s == nil || index < 0 || index >= len(s.instructions) {
		return wasm.Instruction{}, SourceProvenance{}, false
	}
	node := s.instructions[index]
	instruction, err := wasm.CloneInstruction(node.Instr)
	if err != nil {
		return wasm.Instruction{}, SourceProvenance{}, false
	}
	return instruction, SourceProvenance{InstructionOrdinal: s.ordinals[node], CallID: node.callID}, true
}

func (s *NormalizedSource) SuspensionID(index int) (uint64, bool) {
	if s == nil || index < 0 || index >= len(s.suspensionIDs) {
		return 0, false
	}
	return s.suspensionIDs[index], true
}

// ElisionAt reports the reason an original source instruction is absent. It
// returns false for active and out-of-range instructions.
func (s *NormalizedSource) ElisionAt(ordinal int) (SourceElisionReason, bool) {
	if s == nil || ordinal < 0 || ordinal >= len(s.values.control.instructions) {
		return NotElided, false
	}
	reason, elided := s.elided[s.values.control.instructions[ordinal]]
	return reason, elided
}

// Active reports whether this retained source node belongs to the executable
// view. Future lowering must use this fact rather than ValidationReachable.
func (s *NormalizedSource) Active(node Node) bool { return s != nil && s.active[node] }

// NodeSuspends reports the normalized subtree suspension fact for an active
// source node.
func (s *NormalizedSource) NodeSuspends(node Node) bool {
	if s == nil {
		return false
	}
	return s.suspends[node]
}

// LexicalExit reports whether normal execution can reach a source scope's
// declared end on the named arm. A branch to that scope's label is deliberately
// excluded: its typed branch action owns the target-port write instead.
func (s *NormalizedSource) LexicalExit(owner Node, arm ControlArm) bool {
	return s != nil && s.lexicalExits[normalizedExit{owner: owner, arm: arm}]
}

func (s *NormalizedSource) RootMayFallthrough() bool {
	return s != nil && s.rootMayFallthrough
}

type sourceFlow struct {
	escapes        map[LabelID]struct{}
	mayFallthrough bool
}

func mayFallthroughFlow() sourceFlow { return sourceFlow{mayFallthrough: true} }

func branchFlow(targets []LabelID, mayFallthrough bool) sourceFlow {
	result := sourceFlow{mayFallthrough: mayFallthrough}
	if len(targets) != 0 {
		result.escapes = make(map[LabelID]struct{}, len(targets))
		for _, target := range targets {
			result.escapes[target] = struct{}{}
		}
	}
	return result
}

func (s *NormalizedSource) visit(node Node) (sourceFlow, error) {
	s.active[node] = true
	switch n := node.(type) {
	case *SeqNode:
		flow := mayFallthroughFlow()
		for _, child := range n.Children {
			if !flow.mayFallthrough {
				s.elide(child, ElidedAfterTerminator)
				continue
			}
			childFlow, err := s.visit(child)
			if err != nil {
				return sourceFlow{}, err
			}
			flow.escapes = mergeEscapes(flow.escapes, childFlow.escapes)
			flow.mayFallthrough = childFlow.mayFallthrough
		}
		s.suspends[node] = s.childrenSuspend(n.Children)
		return flow, nil
	case *BlockNode:
		body, err := s.visit(n.Body)
		if err != nil {
			return sourceFlow{}, err
		}
		s.lexicalExits[normalizedExit{owner: n, arm: NoArm}] = body.mayFallthrough
		flow := consumeLabel(body, n.label, n.Opcode == wasm.OpLoop)
		s.suspends[node] = s.suspends[n.Body]
		return flow, nil
	case *IfNode:
		thenFlow, err := s.visit(n.Then)
		if err != nil {
			return sourceFlow{}, err
		}
		elseFlow := mayFallthroughFlow()
		if n.Else != nil {
			elseFlow, err = s.visit(n.Else)
			if err != nil {
				return sourceFlow{}, err
			}
		}
		s.lexicalExits[normalizedExit{owner: n, arm: ThenArm}] = thenFlow.mayFallthrough
		s.lexicalExits[normalizedExit{owner: n, arm: ElseArm}] = elseFlow.mayFallthrough
		flow := sourceFlow{
			mayFallthrough: thenFlow.mayFallthrough || elseFlow.mayFallthrough,
			escapes:        mergeEscapes(thenFlow.escapes, elseFlow.escapes),
		}
		flow = consumeLabel(flow, n.label, false)
		s.suspends[node] = s.suspends[n.Then] || (n.Else != nil && s.suspends[n.Else])
		s.hasAsyncInBranch = s.hasAsyncInBranch || s.suspends[node]
		return flow, nil
	case *InstrNode:
		op, exists := s.values.operations[n]
		if !exists {
			return sourceFlow{}, fmt.Errorf("asyncify: normalized source has no operation")
		}
		if op.OpaqueControl {
			return sourceFlow{}, fmt.Errorf("asyncify: unsupported unmodeled control transfer in active source")
		}
		s.instructions = append(s.instructions, n)
		if n.callID != 0 {
			if n.callID > uint64(len(s.values.control.calls)) {
				return sourceFlow{}, fmt.Errorf("asyncify: normalized source has unknown call identity")
			}
			call := s.values.control.calls[n.callID-1]
			s.suspends[n] = call.suspends
			if call.suspends {
				s.suspensionIDs = append(s.suspensionIDs, n.callID)
			}
		}
		switch op.control {
		case controlFallsThrough:
			return mayFallthroughFlow(), nil
		case controlTraps, controlReturn:
			return sourceFlow{}, nil
		case controlConditionalBranch:
			if hasLabel(op.branchTargets, 0) {
				s.hasFunctionBranch = true
			}
			return branchFlow(op.branchTargets, true), nil
		case controlBranch:
			if hasLabel(op.branchTargets, 0) {
				s.hasFunctionBranch = true
			}
			return branchFlow(op.branchTargets, false), nil
		default:
			return sourceFlow{}, fmt.Errorf("asyncify: source operation has unknown control effect")
		}
	default:
		return sourceFlow{}, fmt.Errorf("asyncify: unknown normalized source node %T", node)
	}
}

func (s *NormalizedSource) elide(node Node, reason SourceElisionReason) {
	if s.active[node] || s.elided[node] != NotElided {
		return
	}
	s.elided[node] = reason
	switch n := node.(type) {
	case *SeqNode:
		for _, child := range n.Children {
			s.elide(child, reason)
		}
	case *BlockNode:
		s.elide(n.Body, reason)
	case *IfNode:
		s.elide(n.Then, reason)
		if n.Else != nil {
			s.elide(n.Else, reason)
		}
	}
}

func (s *NormalizedSource) childrenSuspend(children []Node) bool {
	for _, child := range children {
		if s.suspends[child] {
			return true
		}
	}
	return false
}

func mergeEscapes(left, right map[LabelID]struct{}) map[LabelID]struct{} {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	result := make(map[LabelID]struct{}, len(left)+len(right))
	for label := range left {
		result[label] = struct{}{}
	}
	for label := range right {
		result[label] = struct{}{}
	}
	return result
}

func consumeLabel(flow sourceFlow, label LabelID, loop bool) sourceFlow {
	if _, exits := flow.escapes[label]; !exits {
		return flow
	}
	delete(flow.escapes, label)
	if !loop {
		flow.mayFallthrough = true
	}
	return flow
}

func hasLabel(labels []LabelID, wanted LabelID) bool {
	for _, label := range labels {
		if label == wanted {
			return true
		}
	}
	return false
}
