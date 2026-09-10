package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// SelectorOperation names the saved selector belonging to one source if.
// The action kind owns access direction and execution domain. Value zero is
// retained only for validation-polymorphic source in structural diagnostics.
type SelectorOperation struct {
	owner *IfNode
	scope LabelID
	value ValueID
	local uint32
	arm   ControlArm
}

func (s SelectorOperation) Scope() LabelID  { return s.scope }
func (s SelectorOperation) Value() ValueID  { return s.value }
func (s SelectorOperation) Local() uint32   { return s.local }
func (s SelectorOperation) Arm() ControlArm { return s.arm }

func isSelectorAction(kind ActionKind) bool {
	return kind == SourceSelectorStore || kind == SourceSelectorLoad || kind == SourceSelectorRoute
}
func (s SelectorOperation) instruction(kind ActionKind) wasm.Instruction {
	opcode := wasm.OpLocalGet
	if kind == SourceSelectorStore {
		opcode = wasm.OpLocalSet
	}
	return wasm.Instruction{Opcode: opcode, Imm: wasm.LocalImm{LocalIdx: s.local}, Synthetic: kind == SourceSelectorRoute}
}
func verifySelectorShape(s SelectorOperation, kind ActionKind) error {
	if s.owner == nil || !isSelectorAction(kind) {
		return fmt.Errorf("asyncify: invalid selector action")
	}
	if kind == SourceSelectorRoute {
		if s.arm != ThenArm && s.arm != ElseArm {
			return fmt.Errorf("asyncify: selector route has no arm")
		}
	} else if s.arm != NoArm {
		return fmt.Errorf("asyncify: guest selector access has a routing arm")
	}
	return nil
}

func (l *linearizer) emitSelector(node *IfNode, kind ActionKind, arm ControlArm) {
	carriers := l.carriers(node)
	if l.err != nil {
		return
	}
	if l.values == nil {
		instruction := (SelectorOperation{local: carriers.selector}).instruction(kind)
		if kind == SourceSelectorRoute {
			l.output.routing(instruction)
		} else {
			l.output.guestCarrier(instruction)
		}
		return
	}
	source, exists := l.values.operations[node]
	if !exists || !source.HasSelector || !carriers.hasSelector {
		l.err = fmt.Errorf("asyncify: selector access has no source assignment")
		return
	}
	operation := SelectorOperation{owner: node, scope: node.label, value: source.Selector, local: carriers.selector, arm: arm}
	l.output.selectorAccess(operation, kind)
}

func (l *LoweredControl) verifySelector(operation SelectorOperation, owner Node, kind ActionKind) error {
	if l.values == nil || operation.owner != owner {
		return fmt.Errorf("asyncify: selector source owner mismatch")
	}
	scope, exists := l.values.scopes[operation.scope]
	source, hasOperation := l.values.operations[owner]
	local, assigned := l.selectorLocals[operation.scope]
	if !exists || scope.kind != IfScope || scope.owner != owner || !hasOperation || !source.HasSelector || source.Selector != operation.value || !assigned || operation.local != local {
		return fmt.Errorf("asyncify: selector source assignment mismatch")
	}
	suspends := l.values.control.suspends[owner]
	if l.source != nil {
		suspends = l.source.NodeSuspends(owner)
	}
	if kind == SourceSelectorRoute {
		if !suspends || (operation.arm == ElseArm && operation.owner.Else == nil && !needIdentityElse(operation.owner)) {
			return fmt.Errorf("asyncify: selector routing path mismatch")
		}
	} else if suspends || len(scope.params) == 0 {
		return fmt.Errorf("asyncify: guest selector access does not match source entry")
	}
	return nil
}
