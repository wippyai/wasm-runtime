package ir

import "fmt"

// newScopeEntryTransfer binds incoming source parameters to the scope's
// parameter ports. The if selector is consumed separately; async if entries
// already own both selector and parameters through EntryCapture.
func (l *linearizer) newScopeEntryTransfer(label LabelID, locals []uint32) (ScopeTransfer, error) {
	if l.values == nil {
		return ScopeTransfer{}, fmt.Errorf("asyncify: scope entry requires source values")
	}
	scope, exists := l.values.scopes[label]
	if !exists || len(scope.params) == 0 || len(scope.params) != len(locals) {
		return ScopeTransfer{}, fmt.Errorf("asyncify: scope entry parameter arity mismatch")
	}
	source, err := scopeEntrySource(l.values, scope)
	if err != nil {
		return ScopeTransfer{}, err
	}
	transfer := ScopeTransfer{owner: scope.owner, scope: label, mode: MoveValues, operands: make([]ScopeTransferOperand, len(scope.params))}
	for position, port := range scope.params {
		info, defined := l.values.ValueInfo(port)
		local, assigned := l.storage.portLocals[port]
		if !defined || !assigned || local != locals[position] {
			return ScopeTransfer{}, fmt.Errorf("asyncify: scope entry parameter assignment mismatch")
		}
		transfer.operands[position] = ScopeTransferOperand{port: port, value: source.Inputs[position], local: local, valueType: info.Type, present: position >= len(scope.params)-scope.entryStackOperands}
	}
	if err := verifyScopeTransferShape(transfer); err != nil {
		return ScopeTransfer{}, err
	}
	return transfer, nil
}

func (l *LoweredControl) verifyScopeEntryTransfer(transfer ScopeTransfer, owner Node) error {
	if l.values == nil || transfer.owner != owner || transfer.arm != NoArm || transfer.mode != MoveValues {
		return fmt.Errorf("asyncify: scope entry ownership mismatch")
	}
	scope, exists := l.values.scopes[transfer.scope]
	if !exists || scope.owner != owner || len(scope.params) == 0 || len(scope.params) != len(transfer.operands) {
		return fmt.Errorf("asyncify: scope entry source scope mismatch")
	}
	source, err := scopeEntrySource(l.values, scope)
	if err != nil {
		return err
	}
	for position, port := range scope.params {
		operand := transfer.operands[position]
		info, defined := l.values.ValueInfo(port)
		local, assigned := l.portLocals[port]
		present := position >= len(scope.params)-scope.entryStackOperands
		if !defined || !assigned || operand.port != port || operand.value != source.Inputs[position] || operand.local != local || operand.valueType != info.Type || operand.present != present {
			return fmt.Errorf("asyncify: scope entry operand %d source binding mismatch", position)
		}
	}
	return nil
}

func (l *linearizer) emitScopeEntry(label LabelID, locals []uint32) {
	if len(locals) == 0 {
		return
	}
	if l.values == nil {
		l.emitStructuralScopeEntry(locals)
		return
	}
	transfer, err := l.newScopeEntryTransfer(label, locals)
	if err != nil {
		l.err = err
		return
	}
	l.output.scopeEntryTransfer(transfer)
}

// scopeEntrySource checks the source parameter suffix independently of the
// assigned carriers. If selectors occupy the final source input position.
func scopeEntrySource(values *ValuePlan, scope valueScope) (valueOperation, error) {
	source, exists := values.operations[scope.owner]
	width := len(scope.params)
	selector := scope.kind == IfScope
	if selector {
		width++
	}
	if !exists || len(source.Inputs) != width || source.HasSelector != selector || source.inputStackOperands < 0 || source.inputStackOperands > width || scope.entryStackOperands < 0 || scope.entryStackOperands > len(scope.params) {
		return valueOperation{}, fmt.Errorf("asyncify: scope entry source input shape mismatch")
	}
	if selector && source.Selector != source.Inputs[width-1] {
		return valueOperation{}, fmt.Errorf("asyncify: scope entry source selector mismatch")
	}
	for position := range scope.params {
		present := position >= len(scope.params)-scope.entryStackOperands
		if present != (position >= width-source.inputStackOperands) {
			return valueOperation{}, fmt.Errorf("asyncify: scope entry source presence mismatch")
		}
	}
	return source, nil
}
