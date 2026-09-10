package ir

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// verifySourceBindings establishes that the graph covers exactly the owned
// source tree. Iterating only graph maps cannot detect a missing zero-input
// operation (notably an if whose selector contract would silently disappear).
func (p *ValuePlan) verifySourceBindings() error {
	operations := make(map[Node]bool)
	scopes := make(map[LabelID]bool)
	scope := func(owner Node, label LabelID, kind ScopeKind, params, results []wasm.ValType) error {
		fact, ok := p.scopes[label]
		if !ok || scopes[label] || fact.owner != owner || fact.kind != kind {
			return fmt.Errorf("asyncify: source scope binding mismatch")
		}
		scopes[label] = true
		for _, group := range []struct {
			ids   []ValueID
			types []wasm.ValType
		}{{fact.params, params}, {fact.results, results}} {
			if len(group.ids) != len(group.types) {
				return fmt.Errorf("asyncify: source scope port arity mismatch")
			}
			for index, id := range group.ids {
				info, defined := p.ValueInfo(id)
				if !defined || info.Type != group.types[index] {
					return fmt.Errorf("asyncify: source scope port type mismatch")
				}
			}
		}
		if fact.entryStackOperands < 0 || fact.entryStackOperands > len(params) {
			return fmt.Errorf("asyncify: source scope entry presence out of bounds")
		}
		if fact.entryValidationReachable && fact.entryStackOperands != len(params) {
			return fmt.Errorf("asyncify: reachable source scope has absent entry parameter")
		}
		return nil
	}
	if err := scope(p.control.root, 0, FunctionScope, nil, p.control.functionResults); err != nil {
		return err
	}
	operation := func(node Node) error {
		source, exists := p.operations[node]
		if !exists || operations[node] {
			return fmt.Errorf("asyncify: missing or repeated source operation binding")
		}
		if err := p.verifyOperationInputPresence(source); err != nil {
			return fmt.Errorf("asyncify: source operation input facts: %w", err)
		}
		operations[node] = true
		return nil
	}
	controlOperation := func(node Node, label LabelID, condition bool) error {
		if err := operation(node); err != nil {
			return err
		}
		source := p.operations[node]
		ports := p.scopes[label]
		inputs := len(ports.params)
		if condition {
			inputs++
		}
		if len(source.Inputs) != inputs || !slices.Equal(source.Outputs, ports.results) {
			return fmt.Errorf("asyncify: source control operation port mismatch")
		}
		entryStackOperands := ports.entryStackOperands
		for index, id := range source.Inputs[:len(ports.params)] {
			inputPresent := index >= len(ports.params)-entryStackOperands
			if inputPresent != (index >= len(source.Inputs)-source.inputStackOperands) {
				return fmt.Errorf("asyncify: source scope entry presence disagrees with operation")
			}
			if !inputPresent && id != 0 {
				return fmt.Errorf("asyncify: absent source scope entry has a value")
			}
		}
		if condition {
			selectorPresent := len(source.Inputs)-1 >= len(source.Inputs)-source.inputStackOperands
			if !source.HasSelector || selectorPresent != (source.inputStackOperands > entryStackOperands) {
				return fmt.Errorf("asyncify: source if selector presence mismatch")
			}
		} else if source.inputStackOperands != entryStackOperands {
			return fmt.Errorf("asyncify: source scope entry presence disagrees with operation")
		}
		return nil
	}
	var visit func(Node) error
	visit = func(node Node) error {
		switch source := node.(type) {
		case *SeqNode:
			for _, child := range source.Children {
				if err := visit(child); err != nil {
					return err
				}
			}
		case *InstrNode:
			return operation(node)
		case *BlockNode:
			kind := BlockScope
			if source.Opcode == wasm.OpLoop {
				kind = LoopScope
			}
			if err := scope(node, source.label, kind, source.ParamTypes, source.ResultTypes); err != nil {
				return err
			}
			if err := controlOperation(node, source.label, false); err != nil {
				return err
			}
			return visit(source.Body)
		case *IfNode:
			if err := scope(node, source.label, IfScope, source.ParamTypes, source.ResultTypes); err != nil {
				return err
			}
			if err := controlOperation(node, source.label, true); err != nil {
				return err
			}
			if err := visit(source.Then); err != nil {
				return err
			}
			if source.Else != nil {
				return visit(source.Else)
			}
		default:
			return fmt.Errorf("asyncify: unknown source binding owner")
		}
		return nil
	}
	if err := visit(p.control.root); err != nil {
		return err
	}
	if len(operations) != len(p.operations) || len(scopes) != len(p.scopes) {
		return fmt.Errorf("asyncify: graph contains foreign source bindings")
	}
	return nil
}
