package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func scopeOfKind(t *testing.T, plan *ValuePlan, kind ScopeKind) ControlScope {
	t.Helper()
	label, ok := labelOfKind(plan, kind)
	if !ok {
		t.Fatalf("scope kind %d is not unique", kind)
	}
	scope, ok := plan.Scope(label)
	if !ok {
		t.Fatal("published scope is not available through its label")
	}
	return scope
}

func labelOfKind(plan *ValuePlan, kind ScopeKind) (LabelID, bool) {
	var label LabelID
	found := false
	for candidate, scope := range plan.scopes {
		if scope.kind == kind {
			if found {
				return 0, false
			}
			label, found = candidate, true
		}
	}
	return label, found
}

func transferEdges(plan *ValuePlan, match func(ControlEdge) bool) []ControlEdge {
	var found []ControlEdge
	for index := 0; index < plan.EdgeCount(); index++ {
		edge, ok := plan.Edge(index)
		if ok && match(edge) {
			found = append(found, edge)
		}
	}
	return found
}

func sourceOperationForOpcode(t *testing.T, lowered *LoweredControl, opcode byte) SourceOperation {
	t.Helper()
	for index, action := range lowered.actions {
		origin := action.origin
		if origin.source != nil && origin.source.Instr.Opcode == opcode {
			operation, ok := lowered.SourceOperation(index)
			if !ok {
				t.Fatal("lowered source lost its operation contract")
			}
			return operation
		}
	}
	t.Fatalf("source opcode 0x%x not found", opcode)
	return SourceOperation{}
}

func TestControlTransfersLoopEntryBackedgeAndRootExit(t *testing.T) {
	plan, err := valueFixture(t, `(module (func (result i32)
  i32.const 7
  loop (param i32) (result i32)
    br 0
  end))`)
	if err != nil {
		t.Fatal(err)
	}
	loop := scopeOfKind(t, plan, LoopScope)
	root := scopeOfKind(t, plan, FunctionScope)
	entries := transferEdges(plan, func(edge ControlEdge) bool {
		return edge.Kind() == ControlEntry && edge.Target() == loop.Label()
	})
	if len(entries) != 1 {
		t.Fatalf("loop entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Port() != ControlParameter || entry.Mode() != MoveValues || entry.Arm() != NoArm || entry.ValueCount() != 1 || entry.To(0) != loop.ParameterValue(0) {
		t.Fatal("loop entry did not move its source argument to the parameter port")
	}
	backedges := transferEdges(plan, func(edge ControlEdge) bool {
		return edge.Kind() == ControlBranch && edge.Target() == loop.Label()
	})
	if len(backedges) != 1 {
		t.Fatalf("loop backedges = %d, want 1", len(backedges))
	}
	backedge := backedges[0]
	ordinal, isBranch := backedge.BranchOrdinal()
	if !isBranch || ordinal != 0 || backedge.Port() != ControlParameter || backedge.Mode() != MoveValues || backedge.From(0) != loop.ParameterValue(0) || backedge.To(0) != loop.ParameterValue(0) {
		t.Fatal("loop backedge did not preserve the loop parameter transfer")
	}
	exits := transferEdges(plan, func(edge ControlEdge) bool {
		return edge.Kind() == ControlExit && edge.Target() == root.Label()
	})
	if len(exits) != 1 {
		t.Fatalf("root exits = %d, want 1", len(exits))
	}
	exit := exits[0]
	if exit.Port() != ControlResult || exit.Mode() != MoveValues || exit.Arm() != NoArm || exit.ValueCount() != 1 || exit.From(0) != loop.ResultValue(0) || exit.To(0) != root.ResultValue(0) {
		t.Fatal("root exit did not map the loop result to the root result port")
	}
}

func TestControlTransfersBrIfCopiesTargetAndRetainsFallthroughValue(t *testing.T) {
	plan, lowered := lowerValueFixture(t, `(module (func (result i32)
  block (result i32)
    i32.const 7
    i32.const 1
    br_if 0
  end))`)
	block := scopeOfKind(t, plan, BlockScope)
	branches := transferEdges(plan, func(edge ControlEdge) bool {
		return edge.Kind() == ControlBranch && edge.Target() == block.Label()
	})
	if len(branches) != 1 {
		t.Fatalf("br_if edges = %d, want 1", len(branches))
	}
	branch := branches[0]
	if branch.Mode() != CopyValues || branch.Port() != ControlResult || branch.ValueCount() != 1 || branch.To(0) != block.ResultValue(0) {
		t.Fatal("br_if did not copy its argument to the target result port")
	}
	operation := sourceOperationForOpcode(t, lowered, wasm.OpBrIf)
	if operation.InputCount() != 2 || operation.OutputCount() != 1 || operation.OutputValue(0) != operation.InputValue(0) || branch.From(0) != operation.InputValue(0) {
		t.Fatal("br_if did not retain its original argument identity on fallthrough")
	}
	selector, hasSelector := operation.Selector()
	if !hasSelector || selector != operation.InputValue(1) {
		t.Fatal("br_if selector is not its source condition")
	}
	if _, branch := block.Selector(); branch {
		t.Fatal("plain block unexpectedly has a selector")
	}
}

func TestControlTransfersBrTablePreservesRepeatedAndDefaultOrdinals(t *testing.T) {
	plan, lowered := lowerValueFixture(t, `(module (func (result i32)
  block (result i32)
    block (result i32)
      i32.const 7
      i32.const 0
      br_table 0 0 1
    end
  end))`)
	branch := sourceOperationForOpcode(t, lowered, wasm.OpBrTable)
	if branch.InputCount() != 2 {
		t.Fatalf("br_table inputs = %d, want argument and selector", branch.InputCount())
	}
	selector, hasSelector := branch.Selector()
	if !hasSelector || selector != branch.InputValue(1) {
		t.Fatal("br_table selector is not its source condition")
	}
	edges := transferEdges(plan, func(edge ControlEdge) bool { return edge.Kind() == ControlBranch })
	if len(edges) != 3 {
		t.Fatalf("br_table edges = %d, want labels plus default", len(edges))
	}
	ordinals := make(map[int]ControlEdge, len(edges))
	for _, edge := range edges {
		ordinal, isBranch := edge.BranchOrdinal()
		if !isBranch || edge.Mode() != MoveValues || edge.Port() != ControlResult || edge.ValueCount() != 1 || edge.From(0) != branch.InputValue(0) {
			t.Fatal("invalid br_table branch transfer")
		}
		if _, duplicate := ordinals[ordinal]; duplicate {
			t.Fatalf("duplicate branch ordinal %d", ordinal)
		}
		ordinals[ordinal] = edge
	}
	first, firstOK := ordinals[0]
	second, secondOK := ordinals[1]
	defaultEdge, defaultOK := ordinals[2]
	if !firstOK || !secondOK || !defaultOK || first.Target() != second.Target() || first.Target() == defaultEdge.Target() {
		t.Fatal("br_table did not preserve repeated labels and the distinct default")
	}
	if first.To(0) != second.To(0) || first.To(0) == defaultEdge.To(0) {
		t.Fatal("br_table target ports do not follow each ordinal's source target")
	}
}

func TestControlTransfersIfArmsSelectorsAndValueInfoAreImmutable(t *testing.T) {
	plan, err := valueFixture(t, `(module (func (result i32)
  i32.const 7
  i32.const 1
  if (param i32) (result i32)
    i32.const 1
    i32.add
  else
    i32.const 2
    i32.add
  end))`)
	if err != nil {
		t.Fatal(err)
	}
	ifScope := scopeOfKind(t, plan, IfScope)
	selector, hasSelector := ifScope.Selector()
	if !hasSelector {
		t.Fatal("if scope lost its source condition")
	}
	info, ok := plan.ValueInfo(selector)
	if !ok || info.Type != wasm.ValI32 || info.Kind != InstructionResult {
		t.Fatal("if selector has the wrong immutable definition metadata")
	}
	info.Type = wasm.ValI64
	info.Kind = ControlResult
	info.Position = 99
	again, ok := plan.ValueInfo(selector)
	if !ok || again.Type != wasm.ValI32 || again.Kind != InstructionResult || again.Position == 99 {
		t.Fatal("ValueInfo exposed mutable source definition metadata")
	}
	if _, ok := plan.ValueInfo(0); ok {
		t.Fatal("polymorphic unknown was exposed as a concrete source value")
	}
	if _, ok := plan.ValueInfo(ValueID(len(plan.definitions) + 1)); ok {
		t.Fatal("out-of-range source value was accepted")
	}
	exits := transferEdges(plan, func(edge ControlEdge) bool {
		return edge.Kind() == ControlExit && edge.Target() == ifScope.Label()
	})
	if len(exits) != 2 {
		t.Fatalf("if exits = %d, want 2", len(exits))
	}
	var thenExit, elseExit ControlEdge
	for _, edge := range exits {
		if edge.Port() != ControlResult || edge.Mode() != MoveValues || edge.ValueCount() != 1 || edge.To(0) != ifScope.ResultValue(0) {
			t.Fatal("if arm did not move to its result port")
		}
		switch edge.Arm() {
		case ThenArm:
			thenExit = edge
		case ElseArm:
			elseExit = edge
		default:
			t.Fatal("if exit has no arm identity")
		}
	}
	if thenExit.From(0) == elseExit.From(0) {
		t.Fatal("if arms lost their distinct source result identities")
	}
}

func TestVerifyTransfersRejectsMutatedSourceContracts(t *testing.T) {
	const tableFixture = `(module (func (result i32)
  block (result i32)
    block (result i32)
      i32.const 7
      i32.const 0
      br_table 0 0 1
    end
  end))`
	for _, test := range []struct {
		mutate  func(*ValuePlan)
		name    string
		fixture string
	}{
		{
			name: "wrong-destination-with-same-type",
			fixture: `(module (func (result i32)
  i32.const 7
  loop (param i32) (result i32) br 0 end))`,
			mutate: func(plan *ValuePlan) {
				loop, ok := labelOfKind(plan, LoopScope)
				if !ok {
					return
				}
				root, ok := labelOfKind(plan, FunctionScope)
				if !ok {
					return
				}
				for index := range plan.edges {
					edge := &plan.edges[index]
					if edge.Kind == ControlEntry && edge.Target == loop {
						edge.To[0] = plan.scopes[root].results[0]
						return
					}
				}
			},
		},
		{
			name:    "entry-move-changed-to-copy",
			fixture: `(module (func (result i32) i32.const 7 block (param i32) (result i32) end))`,
			mutate: func(plan *ValuePlan) {
				for index := range plan.edges {
					if plan.edges[index].Kind == ControlEntry {
						plan.edges[index].Mode = CopyValues
						return
					}
				}
			},
		},
		{
			name:    "br-if-copy-changed-to-move",
			fixture: `(module (func (result i32) block (result i32) i32.const 7 i32.const 1 br_if 0 end))`,
			mutate: func(plan *ValuePlan) {
				for index := range plan.edges {
					if plan.edges[index].Kind == ControlBranch {
						plan.edges[index].Mode = MoveValues
						return
					}
				}
			},
		},
		{
			name:    "wrong-branch-ordinal",
			fixture: tableFixture,
			mutate: func(plan *ValuePlan) {
				for index := range plan.edges {
					if plan.edges[index].Kind == ControlBranch && plan.edges[index].TargetOrdinal == 0 {
						plan.edges[index].TargetOrdinal = 2
						return
					}
				}
			},
		},
		{
			name:    "wrong-branch-target",
			fixture: tableFixture,
			mutate: func(plan *ValuePlan) {
				for index := range plan.edges {
					if plan.edges[index].Kind == ControlBranch && plan.edges[index].TargetOrdinal == 0 {
						plan.edges[index].Target = plan.edges[index+2].Target
						return
					}
				}
			},
		},
		{
			name: "wrong-branch-source-input",
			fixture: `(module (func (result i32)
  block (result i32) i32.const 7 i32.const 1 br_if 0 end))`,
			mutate: func(plan *ValuePlan) {
				for index := range plan.edges {
					if plan.edges[index].Kind == ControlBranch {
						plan.edges[index].From[0] = plan.operations[plan.control.root.(*SeqNode).Children[0]].Outputs[0]
						return
					}
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, err := valueFixture(t, test.fixture)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(plan)
			if err := plan.verifyTransfers(); err == nil {
				t.Fatal("accepted a mutated source transfer contract")
			}
		})
	}
}

func TestTransferSelectorAndFallthroughIdentityAreChecked(t *testing.T) {
	for _, scenario := range []string{"selector-presence", "selector-identity", "fallthrough-identity"} {
		t.Run(scenario, func(t *testing.T) {
			plan, err := valueFixture(t, `(module (func (result i32) block (result i32) i32.const 7 i32.const 1 br_if 0 end))`)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			for node, operation := range plan.operations {
				instruction, ok := node.(*InstrNode)
				if !ok || instruction.Instr.Opcode != wasm.OpBrIf {
					continue
				}
				switch scenario {
				case "selector-presence":
					operation.HasSelector = false
				case "selector-identity":
					operation.Selector = operation.Inputs[0]
				case "fallthrough-identity":
					operation.Outputs[0] = operation.Selector
				}
				plan.operations[node] = operation
				changed = true
			}
			if !changed {
				t.Fatal("fixture did not reach source branch")
			}
			if err := plan.verifyTransfers(); err == nil {
				t.Fatal("accepted corrupted conditional branch identities")
			}
		})
	}
}

func TestSourceTransferIndexesRemainOwned(t *testing.T) {
	const fixture = `(module (func (result i32) block (result i32) i32.const 7 i32.const 1 br_if 0 end))`
	plan, lowered := lowerValueFixture(t, fixture)
	branch := sourceOperationForOpcode(t, lowered, wasm.OpBrIf)
	if branch.TransferCount() != 1 {
		t.Fatal("branch lost indexed transfer")
	}
	edge, ok := branch.Transfer(0)
	if !ok || edge.Kind() != ControlBranch || edge.Mode() != CopyValues {
		t.Fatal("branch retrieved another owner's transfer")
	}
	scope := scopeOfKind(t, plan, BlockScope)
	if scope.TransferCount() != 2 {
		t.Fatal("scope lost entry/exit transfers")
	}
	if _, ok := scope.Transfer(-1); ok {
		t.Fatal("negative transfer index accepted")
	}
	if _, ok := branch.Transfer(1); ok {
		t.Fatal("out-of-range transfer index accepted")
	}
	for _, scenario := range []string{"missing", "duplicate", "foreign-owner"} {
		t.Run(scenario, func(t *testing.T) {
			values, err := valueFixture(t, fixture)
			if err != nil {
				t.Fatal(err)
			}
			var owner Node
			for node := range values.transfers {
				if instruction, ok := node.(*InstrNode); ok && instruction.Instr.Opcode == wasm.OpBrIf {
					owner = node
					break
				}
			}
			if owner == nil {
				t.Fatal("missing fixture branch")
			}
			indexes := values.transfers[owner]
			switch scenario {
			case "missing":
				delete(values.transfers, owner)
			case "duplicate":
				values.transfers[owner] = append(indexes, indexes[0])
			case "foreign-owner":
				delete(values.transfers, owner)
				values.transfers[&SeqNode{}] = indexes
			}
			if err := values.verifyTransfers(); err == nil {
				t.Fatal("accepted inconsistent transfer ownership index")
			}
		})
	}
}
