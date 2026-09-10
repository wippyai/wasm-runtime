package ir

import "fmt"

// InputStackOperandCount is the number of source inputs actually present in
// the validation stack before this operation consumed them. Present inputs are
// the suffix of InputValue order. An absent input has ValueID zero, but a
// present input may also have ValueID zero when it is a polymorphic unknown.
// Outputs are always present source stack entries, including outputs whose
// identity is the polymorphic unknown.
func (s SourceOperation) InputStackOperandCount() int {
	return s.plan.operations[s.node].inputStackOperands
}

// InputPresent reports whether the input at index was present before the
// operation's pop. It returns false for an out-of-range index.
func (s SourceOperation) InputPresent(index int) bool {
	op := s.plan.operations[s.node]
	if index < 0 || index >= len(op.Inputs) {
		return false
	}
	return index >= len(op.Inputs)-op.inputStackOperands
}

// SelectorPresence reports whether an operation has a selector and whether
// that selector was a present validation-stack operand. It returns false,
// false for operations without a selector.
func (s SourceOperation) SelectorPresence() (present, hasSelector bool) {
	op := s.plan.operations[s.node]
	if !op.HasSelector {
		return false, false
	}
	return len(op.Inputs)-1 >= len(op.Inputs)-op.inputStackOperands, true
}

// EntryStackOperandCount is the number of declared scope parameters actually
// present before entry consumed them. It excludes an if selector; use Selector
// for that operation input. Present entry values are the suffix of
// EntryValue order, and may have ValueID zero.
func (s ControlScope) EntryStackOperandCount() int {
	return s.plan.scopes[s.label].entryStackOperands
}

func (s ControlScope) EntryValueCount() int { return len(s.plan.scopes[s.label].params) }
func (s ControlScope) EntryValue(index int) ValueID {
	return s.plan.operations[s.plan.scopes[s.label].owner].Inputs[index]
}

// EntryValuePresent reports whether a declared entry value was present before
// the scope consumed it. It returns false for an out-of-range index.
func (s ControlScope) EntryValuePresent(index int) bool {
	scope := s.plan.scopes[s.label]
	if index < 0 || index >= len(scope.params) {
		return false
	}
	return index >= len(scope.params)-scope.entryStackOperands
}

// SelectorPresence reports the owning if operation's selector presence. It
// returns false, false for scopes without a selector, including blocks and
// loops. Pair it with Selector when the selector identity is needed.
func (s ControlScope) SelectorPresence() (present, hasSelector bool) {
	op := s.plan.operations[s.plan.scopes[s.label].owner]
	if !op.HasSelector {
		return false, false
	}
	return len(op.Inputs)-1 >= len(op.Inputs)-op.inputStackOperands, true
}

// verifyOperationInputPresence validates bounded, source-local facts only.
// It establishes no independent execution model: it neither reconstructs a
// control-flow path nor claims that a validation-reachable operation runs at
// runtime.
func (p *ValuePlan) verifyOperationInputPresence(operation valueOperation) error {
	if operation.inputStackOperands < 0 || operation.inputStackOperands > len(operation.Inputs) {
		return fmt.Errorf("invalid source operation input presence")
	}
	if operation.ValidationReachable && operation.inputStackOperands != len(operation.Inputs) {
		return fmt.Errorf("reachable source operation has absent input")
	}
	firstPresent := len(operation.Inputs) - operation.inputStackOperands
	for index, id := range operation.Inputs {
		if index < firstPresent {
			if id != 0 {
				return fmt.Errorf("absent source operation input has a value")
			}
			continue
		}
		if id != 0 {
			if _, defined := p.ValueInfo(id); !defined {
				return fmt.Errorf("present source operation input is undefined")
			}
		}
	}
	for _, id := range operation.Outputs {
		if id != 0 {
			if _, defined := p.ValueInfo(id); !defined {
				return fmt.Errorf("source operation output is undefined")
			}
		}
	}
	return nil
}
