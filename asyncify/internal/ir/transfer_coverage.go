package ir

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// verifyTransferCoverage compares edges to source facts, not to another index
// reconstructed from those edges. Scope exit stacks are captured by source
// validation before edge construction. Source branch targets and validation-frame reachability
// determine every required alternative, including repeated table labels.
func (p *ValuePlan) verifyTransferCoverage() error {
	expectedCounts := make(map[Node]int)
	check := func(expected valueEdge) error {
		position := expectedCounts[expected.Source]
		indexes := p.transfers[expected.Source]
		if position >= len(indexes) {
			return fmt.Errorf("asyncify: required source transfer is missing")
		}
		actual := p.edges[indexes[position]] // Index bounds/ownership checked first.
		if actual.Kind != expected.Kind || actual.Target != expected.Target || actual.Port != expected.Port || actual.Mode != expected.Mode || actual.Arm != expected.Arm || actual.TargetOrdinal != expected.TargetOrdinal || !slices.Equal(actual.From, expected.From) || !slices.Equal(actual.To, expected.To) {
			return fmt.Errorf("asyncify: transfer disagrees with source occurrence %d", position)
		}
		expectedCounts[expected.Source] = position + 1
		return nil
	}
	for label, scope := range p.scopes {
		expectedCounts[scope.owner] = 0
		if scope.kind != FunctionScope && scope.entryValidationReachable {
			inputs := p.operations[scope.owner].Inputs
			if len(inputs) < len(scope.params) {
				return fmt.Errorf("asyncify: source entry input arity mismatch")
			}
			if err := check(valueEdge{Source: scope.owner, Kind: ControlEntry, Target: label, Port: ControlParameter, Mode: MoveValues, From: inputs[:len(scope.params)], To: scope.params}); err != nil {
				return err
			}
		}
		for _, exit := range scope.exits {
			if !exit.validationReachable {
				continue
			}
			if err := check(valueEdge{Source: scope.owner, Kind: ControlExit, Target: label, Port: ControlResult, Mode: MoveValues, Arm: exit.arm, From: exit.from, To: scope.results}); err != nil {
				return err
			}
		}
	}
	for owner, operation := range p.operations {
		node, isInstruction := owner.(*InstrNode)
		if !isInstruction {
			continue
		}
		expectedCounts[owner] = 0
		if !operation.ValidationReachable {
			continue
		}
		switch node.Instr.Opcode {
		case wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable, wasm.OpReturn:
		default:
			continue
		}
		targets := node.branchTargets
		if node.Instr.Opcode == wasm.OpReturn {
			targets = []LabelID{0}
		}
		inputs := operation.Inputs
		if operation.HasSelector {
			if len(inputs) == 0 {
				return fmt.Errorf("asyncify: missing branch selector")
			}
			inputs = inputs[:len(inputs)-1]
		}
		mode := MoveValues
		if node.Instr.Opcode == wasm.OpBrIf {
			mode = CopyValues
		}
		for ordinal, target := range targets {
			scope, ok := p.scopes[target]
			if !ok {
				return fmt.Errorf("asyncify: source branch target has no scope")
			}
			port, to := ControlResult, scope.results
			if scope.kind == LoopScope {
				port, to = ControlParameter, scope.params
			}
			if err := check(valueEdge{Source: owner, Kind: ControlBranch, Target: target, Port: port, Mode: mode, From: inputs, To: to, TargetOrdinal: ordinal}); err != nil {
				return err
			}
		}
	}
	for owner, indexes := range p.transfers {
		count, known := expectedCounts[owner]
		if !known || count != len(indexes) {
			return fmt.Errorf("asyncify: unexpected source transfer occurrence")
		}
	}
	return nil
}
