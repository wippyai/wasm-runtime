package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// ScopeExit describes a validation-frame exit, including exits with no control
// edge. Stack operands occupy the suffix of the result signature. Missing
// operands are polymorphic pops at an unreachable frame base; a present operand
// can itself have unknown type (for example, unreachable followed by select).
// Neither an unknown ValueID nor validation unreachability implies absence.
type ScopeExit struct {
	plan  *ValuePlan
	label LabelID
	index int
}

func (s ControlScope) ExitCount() int { return len(s.plan.scopes[s.label].exits) }
func (s ControlScope) Exit(index int) (ScopeExit, bool) {
	if index < 0 || index >= s.ExitCount() {
		return ScopeExit{}, false
	}
	return ScopeExit{plan: s.plan, label: s.label, index: index}, true
}
func (e ScopeExit) Arm() ControlArm { return e.plan.scopes[e.label].exits[e.index].arm }
func (e ScopeExit) ValidationReachable() bool {
	return e.plan.scopes[e.label].exits[e.index].validationReachable
}
func (e ScopeExit) StackOperandCount() int {
	return e.plan.scopes[e.label].exits[e.index].stackOperands
}
func (e ScopeExit) ValueCount() int { return len(e.plan.scopes[e.label].exits[e.index].from) }
func (e ScopeExit) Value(index int) ValueID {
	return e.plan.scopes[e.label].exits[e.index].from[index]
}

func (p *ValuePlan) verifyExitOperands(scope valueScope, exit scopeExit) error {
	if len(exit.from) != len(scope.results) || exit.stackOperands < 0 || exit.stackOperands > len(exit.from) {
		return fmt.Errorf("invalid exit operand arity")
	}
	if exit.validationReachable && exit.stackOperands != len(exit.from) {
		return fmt.Errorf("reachable exit has absent operands")
	}
	for index, id := range exit.from {
		if index < len(exit.from)-exit.stackOperands && id != 0 {
			return fmt.Errorf("absent exit operand has a source value")
		}
		if id == 0 {
			continue
		}
		info, ok := p.ValueInfo(id)
		target, targetOK := p.ValueInfo(scope.results[index])
		if !ok || !targetOK || info.Type != target.Type {
			return fmt.Errorf("invalid exit operand type")
		}
	}
	return nil
}

// Production consumes source validation facts, never the last emitted opcode.
// The control-only structural test primitive has no operand plan and retains its
// legacy structural behavior; it is not an alternative production pipeline.
func (l *linearizer) exitOperandCount(label LabelID, arm ControlArm, body []Action, resultCount int) int {
	if l.values == nil {
		if actionsEndWithUnconditionalTerminator(body) {
			return 0
		}
		return resultCount
	}
	scope, ok := l.values.scopes[label]
	if ok {
		for _, exit := range scope.exits {
			if exit.arm == arm && len(exit.from) == resultCount {
				return exit.stackOperands
			}
		}
	}
	if l.err == nil {
		l.err = fmt.Errorf("asyncify: missing source exit operands at label %d arm %d", label, arm)
	}
	return 0
}

func actionsEndWithUnconditionalTerminator(actions []Action) bool {
	if len(actions) == 0 {
		return false
	}
	return endsWithUnconditionalTerminator([]wasm.Instruction{actions[len(actions)-1].instruction})
}
