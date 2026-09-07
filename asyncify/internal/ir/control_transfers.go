package ir

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TransferMode describes source ownership on one selected control edge.
// br_if copies to the target while retaining its inputs on fallthrough.
// Edges of a branch table are alternatives, not simultaneous moves.
type TransferMode uint8

const (
	MoveValues TransferMode = iota
	CopyValues
)

type ControlArm uint8

const (
	NoArm ControlArm = iota
	ThenArm
	ElseArm
)

type ScopeKind uint8

const (
	FunctionScope ScopeKind = iota
	BlockScope
	LoopScope
	IfScope
)

// Validation reachability is local to a Wasm control frame. A nested frame
// starts fresh even when its parent is polymorphic; these facts never authorize
// pruning based on runtime executability.
type scopeExit struct {
	from                []ValueID
	stackOperands       int
	arm                 ControlArm
	validationReachable bool
}

type valueScope struct {
	owner                    Node
	exits                    []scopeExit
	params                   []ValueID
	results                  []ValueID
	kind                     ScopeKind
	entryValidationReachable bool
	// entryStackOperands describes the parameter suffix actually present
	// before the scope entry consumed its declared parameters. It does not
	// include an if selector, which belongs to the owning operation instead.
	entryStackOperands int
}

// ValueInfo is a copy of immutable source definition metadata. Value zero is
// the explicit polymorphic unknown, never a concrete source definition.
type ValueInfo struct {
	Type     wasm.ValType
	Kind     ValueKind
	Position int
}

func (p *ValuePlan) ValueInfo(id ValueID) (ValueInfo, bool) {
	if id == 0 || uint64(id) > uint64(len(p.definitions)) {
		return ValueInfo{}, false
	}
	value := p.definitions[id-1]
	return ValueInfo{Type: value.Type, Kind: value.Port, Position: value.Position}, true
}

type ControlScope struct {
	plan  *ValuePlan
	label LabelID
}

func (p *ValuePlan) Scope(label LabelID) (ControlScope, bool) {
	_, exists := p.scopes[label]
	return ControlScope{plan: p, label: label}, exists
}
func (s ControlScope) Label() LabelID                   { return s.label }
func (s ControlScope) Kind() ScopeKind                  { return s.plan.scopes[s.label].kind }
func (s ControlScope) ParameterCount() int              { return len(s.plan.scopes[s.label].params) }
func (s ControlScope) ResultCount() int                 { return len(s.plan.scopes[s.label].results) }
func (s ControlScope) ParameterValue(index int) ValueID { return s.plan.scopes[s.label].params[index] }
func (s ControlScope) ResultValue(index int) ValueID    { return s.plan.scopes[s.label].results[index] }
func (s ControlScope) Selector() (ValueID, bool) {
	operation := s.plan.operations[s.plan.scopes[s.label].owner]
	return operation.Selector, operation.HasSelector
}

// ControlEdge is a read-only view of an explicit source-to-port value mapping.
// Arms distinguish if exits; branch ordinals preserve each table alternative,
// including repeated targets and its final default target.
type ControlEdge struct {
	plan  *ValuePlan
	index int
}

func (p *ValuePlan) EdgeCount() int { return len(p.edges) }
func (p *ValuePlan) Edge(index int) (ControlEdge, bool) {
	if index < 0 || index >= len(p.edges) {
		return ControlEdge{}, false
	}
	return ControlEdge{plan: p, index: index}, true
}
func (e ControlEdge) Kind() EdgeKind         { return e.plan.edges[e.index].Kind }
func (e ControlEdge) Target() LabelID        { return e.plan.edges[e.index].Target }
func (e ControlEdge) Port() ValueKind        { return e.plan.edges[e.index].Port }
func (e ControlEdge) Mode() TransferMode     { return e.plan.edges[e.index].Mode }
func (e ControlEdge) Arm() ControlArm        { return e.plan.edges[e.index].Arm }
func (e ControlEdge) ValueCount() int        { return len(e.plan.edges[e.index].From) }
func (e ControlEdge) From(index int) ValueID { return e.plan.edges[e.index].From[index] }
func (e ControlEdge) To(index int) ValueID   { return e.plan.edges[e.index].To[index] }
func (e ControlEdge) BranchOrdinal() (int, bool) {
	edge := e.plan.edges[e.index]
	return edge.TargetOrdinal, edge.Kind == ControlBranch
}
func (s SourceOperation) Selector() (ValueID, bool) {
	operation := s.plan.operations[s.node]
	return operation.Selector, operation.HasSelector
}

// verifyTransfers checks port ownership and the source graph's transfer shape.
// It does not claim complete CFG reachability: source validation can retain
// structurally valid edges from dynamically unreachable code.
func (p *ValuePlan) verifyTransfers() error {
	if err := p.verifySourceBindings(); err != nil {
		return err
	}
	root, ok := p.scopes[0]
	if !ok || root.kind != FunctionScope || root.owner != p.control.root {
		return fmt.Errorf("asyncify: missing source function scope")
	}
	for label, scope := range p.scopes {
		exitCount := 1
		if scope.kind == IfScope {
			exitCount = 2
		}
		if len(scope.exits) != exitCount {
			return fmt.Errorf("asyncify: source scope %d has incomplete exit facts", label)
		}
		for index, exit := range scope.exits {
			expectedArm := NoArm
			if scope.kind == IfScope {
				expectedArm = ThenArm
				if index == 1 {
					expectedArm = ElseArm
				}
			}
			if err := p.verifyExitOperands(scope, exit); err != nil {
				return fmt.Errorf("asyncify: source scope %d: %w", label, err)
			}
			if exit.arm != expectedArm {
				return fmt.Errorf("asyncify: source scope %d has inconsistent exit arms", label)
			}
		}
		for _, group := range []struct {
			ids  []ValueID
			kind ValueKind
		}{{ids: scope.params, kind: ControlParameter}, {ids: scope.results, kind: ControlResult}} {
			for index, id := range group.ids {
				info, exists := p.ValueInfo(id)
				if !exists || info.Kind != group.kind || info.Position != index || p.definitions[id-1].Owner != scope.owner {
					return fmt.Errorf("asyncify: invalid source port at scope %d", label)
				}
			}
		}
	}
	for node, operation := range p.operations {
		needsSelector := false
		switch source := node.(type) {
		case *IfNode:
			needsSelector = true
		case *InstrNode:
			needsSelector = source.Instr.Opcode == wasm.OpBrIf || source.Instr.Opcode == wasm.OpBrTable
		}
		if operation.HasSelector != needsSelector {
			return fmt.Errorf("asyncify: source selector presence mismatch")
		}
		if needsSelector {
			if len(operation.Inputs) == 0 || operation.Selector != operation.Inputs[len(operation.Inputs)-1] {
				return fmt.Errorf("asyncify: source selector identity mismatch")
			}
			if operation.Selector != 0 {
				info, defined := p.ValueInfo(operation.Selector)
				if !defined || info.Type != wasm.ValI32 {
					return fmt.Errorf("asyncify: source selector type mismatch")
				}
			}
		}
	}
	indexed := make([]bool, len(p.edges))
	for source, transfers := range p.transfers {
		for _, index := range transfers {
			if index < 0 || index >= len(p.edges) || indexed[index] || p.edges[index].Source != source {
				return fmt.Errorf("asyncify: inconsistent source transfer index")
			}
			indexed[index] = true
		}
	}
	for index, edge := range p.edges {
		if !indexed[index] {
			return fmt.Errorf("asyncify: edge %d has no source transfer index", index)
		}

		scope, exists := p.scopes[edge.Target]
		if !exists {
			return fmt.Errorf("asyncify: edge %d has unknown target scope", index)
		}
		expected := scope.results
		switch edge.Port {
		case ControlParameter:
			expected = scope.params
		case ControlResult:
		default:
			return fmt.Errorf("asyncify: edge %d targets an instruction value", index)
		}
		if len(edge.From) != len(expected) || !slices.Equal(edge.To, expected) {
			return fmt.Errorf("asyncify: edge %d disagrees with destination ports", index)
		}
		for i, from := range edge.From {
			if from == 0 {
				continue
			} // Wasm's explicit polymorphic unknown.
			source, defined := p.ValueInfo(from)
			target, _ := p.ValueInfo(edge.To[i])
			if !defined || source.Type != target.Type {
				return fmt.Errorf("asyncify: edge %d has invalid input type", index)
			}
		}
		operation := p.operations[edge.Source]
		switch edge.Kind {
		case ControlEntry:
			if edge.Source != scope.owner || edge.Port != ControlParameter || edge.Mode != MoveValues || edge.Arm != NoArm {
				return fmt.Errorf("asyncify: edge %d has invalid scope entry", index)
			}
			if len(operation.Inputs) < len(edge.From) || !slices.Equal(edge.From, operation.Inputs[:len(edge.From)]) {
				return fmt.Errorf("asyncify: edge %d changes source entry values", index)
			}
		case ControlExit:
			if edge.Source != scope.owner || edge.Port != ControlResult || edge.Mode != MoveValues {
				return fmt.Errorf("asyncify: edge %d has invalid scope exit", index)
			}
			if (scope.kind == IfScope && edge.Arm != ThenArm && edge.Arm != ElseArm) || (scope.kind != IfScope && edge.Arm != NoArm) {
				return fmt.Errorf("asyncify: edge %d has invalid exit arm", index)
			}
		case ControlBranch:
			node, isInstruction := edge.Source.(*InstrNode)
			if !isInstruction {
				return fmt.Errorf("asyncify: edge %d has no source branch", index)
			}
			targets := node.branchTargets
			if node.Instr.Opcode == wasm.OpReturn {
				targets = []LabelID{0}
			}
			if edge.TargetOrdinal < 0 || edge.TargetOrdinal >= len(targets) || targets[edge.TargetOrdinal] != edge.Target {
				return fmt.Errorf("asyncify: edge %d changes source branch target", index)
			}
			mode := MoveValues
			if node.Instr.Opcode == wasm.OpBrIf {
				mode = CopyValues
			}
			if mode == CopyValues {
				if len(operation.Outputs) != len(edge.From) {
					return fmt.Errorf("asyncify: conditional branch fallthrough arity mismatch")
				}
				for i, from := range edge.From {
					if from != 0 && operation.Outputs[i] != from {
						return fmt.Errorf("asyncify: conditional branch changes fallthrough identity")
					}
				}
			}
			port := ControlResult
			if scope.kind == LoopScope {
				port = ControlParameter
			}
			if edge.Mode != mode || edge.Port != port || edge.Arm != NoArm {
				return fmt.Errorf("asyncify: edge %d has invalid branch transfer", index)
			}
			if len(operation.Inputs) < len(edge.From) || !slices.Equal(edge.From, operation.Inputs[:len(edge.From)]) {
				return fmt.Errorf("asyncify: edge %d changes source branch values", index)
			}
		default:
			return fmt.Errorf("asyncify: edge %d has unknown kind", index)
		}
	}
	return p.verifyTransferCoverage()
}

// Transfers are indexed by their source occurrence during planning. Lowering
// can retrieve a source operation's transfers without scanning emitted opcodes
// or repeatedly searching the whole function graph.
func (s SourceOperation) TransferCount() int { return len(s.plan.transfers[s.node]) }
func (s SourceOperation) Transfer(index int) (ControlEdge, bool) {
	return s.plan.sourceTransfer(s.node, index)
}
func (s ControlScope) TransferCount() int { return len(s.plan.transfers[s.plan.scopes[s.label].owner]) }
func (s ControlScope) Transfer(index int) (ControlEdge, bool) {
	return s.plan.sourceTransfer(s.plan.scopes[s.label].owner, index)
}
func (p *ValuePlan) sourceTransfer(owner Node, index int) (ControlEdge, bool) {
	transfers := p.transfers[owner]
	if index < 0 || index >= len(transfers) {
		return ControlEdge{}, false
	}
	return p.Edge(transfers[index])
}
