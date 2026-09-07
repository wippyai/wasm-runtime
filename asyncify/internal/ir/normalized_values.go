package ir

import "fmt"

// verifyValueClosure checks that the executable view is closed over retained
// source definitions. A consumed control port must have a retained incoming
// transfer. This is not a dominance proof or a physical carrier lifetime proof:
// source validation owns operand order, and later lowering owns storage.
func (s *NormalizedSource) verifyValueClosure() error {
	type portInput struct{ retained, entry bool }
	incoming := make([]portInput, len(s.values.definitions))
	for _, edge := range s.values.edges {
		if !s.retainsEdge(edge) {
			continue
		}
		for _, id := range edge.To {
			if id == 0 || uint64(id) > uint64(len(incoming)) {
				return fmt.Errorf("asyncify: normalized transfer has undefined target %d", id)
			}
			incoming[id-1].retained = true
			if edge.Kind == ControlEntry {
				incoming[id-1].entry = true
			}
		}
	}
	check := func(id ValueID) error {
		if id == 0 || uint64(id) > uint64(len(s.values.definitions)) {
			return fmt.Errorf("asyncify: normalized input has no concrete definition: %d", id)
		}
		definition := s.values.definitions[id-1]
		if !s.Active(definition.Owner) {
			return fmt.Errorf("asyncify: normalized input %d references an elided definition", id)
		}
		if definition.Port != InstructionResult && !incoming[id-1].retained {
			return fmt.Errorf("asyncify: normalized input port %d has no retained incoming transfer", id)
		}
		if definition.Port == ControlParameter && !incoming[id-1].entry {
			return fmt.Errorf("asyncify: normalized parameter %d has no retained initialization", id)
		}
		return nil
	}
	checkValues := func(ids []ValueID) error {
		for _, id := range ids {
			if err := check(id); err != nil {
				return err
			}
		}
		return nil
	}
	// Edge order is original source order. Elided lexical exits are excluded
	// even when the raw validator retained known operands after a terminator.
	for _, edge := range s.values.edges {
		if s.retainsEdge(edge) {
			if err := checkValues(edge.From); err != nil {
				return err
			}
		}
	}
	var visit func(Node) error
	visit = func(node Node) error {
		if !s.Active(node) {
			return nil
		}
		if operation, exists := s.values.operations[node]; exists {
			if err := checkValues(operation.Inputs); err != nil {
				return err
			}
			if operation.HasSelector {
				if err := check(operation.Selector); err != nil {
					return err
				}
			}
			if err := checkValues(operation.controlPrefix); err != nil {
				return err
			}
		}
		switch n := node.(type) {
		case *SeqNode:
			for _, child := range n.Children {
				if err := visit(child); err != nil {
					return err
				}
			}
		case *BlockNode:
			return visit(n.Body)
		case *IfNode:
			if err := visit(n.Then); err != nil {
				return err
			}
			if n.Else != nil {
				return visit(n.Else)
			}
		case *InstrNode:
			if s.NodeSuspends(n) {
				operands, exists := s.values.continuations[n.callID]
				if !exists {
					return fmt.Errorf("asyncify: normalized call %d has no continuation", n.callID)
				}
				return checkValues(operands)
			}
		}
		return nil
	}
	return visit(s.values.control.root)
}

func (s *NormalizedSource) retainsEdge(edge valueEdge) bool {
	if !s.Active(edge.Source) {
		return false
	}
	if edge.Kind != ControlExit {
		return true
	}
	if edge.Source == s.values.control.root {
		return s.RootMayFallthrough()
	}
	return s.LexicalExit(edge.Source, edge.Arm)
}
