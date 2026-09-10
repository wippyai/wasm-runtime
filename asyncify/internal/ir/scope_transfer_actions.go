package ir

import "fmt"

// newScopeResultTransfer derives one result transfer from the retained source
// exit fact. It never inspects emitted body instructions or terminal opcodes.
func (l *linearizer) newScopeResultTransfer(owner Node, label LabelID, arm ControlArm, resultLocals []uint32) (ScopeTransfer, error) {
	if l.values == nil {
		return ScopeTransfer{}, fmt.Errorf("asyncify: scope transfer requires source values")
	}
	scope, ok := l.values.scopes[label]
	if !ok || scope.owner != owner || len(scope.results) != len(resultLocals) || len(scope.results) == 0 {
		return ScopeTransfer{}, fmt.Errorf("asyncify: scope transfer has inconsistent source scope")
	}
	var exit *scopeExit
	for index := range scope.exits {
		if scope.exits[index].arm == arm {
			if exit != nil {
				return ScopeTransfer{}, fmt.Errorf("asyncify: scope transfer has repeated exit arm")
			}
			exit = &scope.exits[index]
		}
	}
	if exit == nil || len(exit.from) != len(scope.results) || exit.stackOperands < 0 || exit.stackOperands > len(exit.from) {
		return ScopeTransfer{}, fmt.Errorf("asyncify: scope transfer has no source exit")
	}
	transfer := ScopeTransfer{owner: owner, scope: label, arm: arm, mode: MoveValues, operands: make([]ScopeTransferOperand, len(scope.results))}
	for index, port := range scope.results {
		info, defined := l.values.ValueInfo(port)
		if !defined {
			return ScopeTransfer{}, fmt.Errorf("asyncify: scope transfer result port is undefined")
		}
		transfer.operands[index] = ScopeTransferOperand{
			port: port, value: exit.from[index], local: resultLocals[index], valueType: info.Type,
			present: index >= len(exit.from)-exit.stackOperands,
		}
	}
	if err := verifyScopeTransferShape(transfer); err != nil {
		return ScopeTransfer{}, err
	}
	return transfer, nil
}

func (l *LoweredControl) verifyScopeTransfer(transfer ScopeTransfer, owner Node) error {
	if l.values == nil || transfer.owner == nil || owner != transfer.owner {
		return fmt.Errorf("asyncify: scope transfer has no retained source owner")
	}
	scope, ok := l.values.scopes[transfer.scope]
	if !ok || scope.owner != transfer.owner || len(scope.results) == 0 || transfer.mode != MoveValues || len(transfer.operands) != len(scope.results) {
		return fmt.Errorf("asyncify: scope transfer source scope mismatch")
	}
	var exit *scopeExit
	for index := range scope.exits {
		if scope.exits[index].arm == transfer.arm {
			if exit != nil {
				return fmt.Errorf("asyncify: scope transfer has repeated source exit arm")
			}
			exit = &scope.exits[index]
		}
	}
	if exit == nil || len(exit.from) != len(scope.results) || exit.stackOperands < 0 || exit.stackOperands > len(exit.from) {
		return fmt.Errorf("asyncify: scope transfer source exit mismatch")
	}
	for index, port := range scope.results {
		operand := transfer.operands[index]
		info, defined := l.values.ValueInfo(port)
		local, assigned := l.portLocals[port]
		present := index >= len(exit.from)-exit.stackOperands
		if !defined || !assigned || operand.port != port || operand.value != exit.from[index] || operand.present != present || operand.valueType != info.Type || operand.local != local {
			return fmt.Errorf("asyncify: scope transfer result %d does not match source exit", index)
		}
	}
	return nil
}
