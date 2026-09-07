package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// newAsyncIfEntryCapture binds one async if's source entry values to its
// already-planned parameter and selector carriers. It is production-only: the
// structural lowering primitive intentionally has no source value contracts.
func (l *linearizer) newAsyncIfEntryCapture(node *IfNode, carriers controlCarriers) (EntryCapture, error) {
	if l.values == nil {
		return EntryCapture{}, fmt.Errorf("asyncify: async if capture requires source values")
	}
	scope, ok := l.values.scopes[node.label]
	if !ok || scope.owner != node || scope.kind != IfScope {
		return EntryCapture{}, fmt.Errorf("asyncify: async if capture has no source scope")
	}
	source, ok := l.values.operations[node]
	if !ok || !source.HasSelector || len(source.Inputs) != len(scope.params)+1 {
		return EntryCapture{}, fmt.Errorf("asyncify: async if capture has no source entry operation")
	}
	if len(carriers.params) != len(scope.params) || !carriers.hasSelector {
		return EntryCapture{}, fmt.Errorf("asyncify: async if capture carrier assignment mismatch")
	}
	if source.inputStackOperands < 0 || source.inputStackOperands > len(source.Inputs) || scope.entryStackOperands < 0 || scope.entryStackOperands > len(scope.params) {
		return EntryCapture{}, fmt.Errorf("asyncify: async if capture input presence mismatch")
	}
	capture := EntryCapture{owner: node, scope: node.label, stateGlobal: l.config.StateGlobal, stateRewinding: l.config.StateRewinding, operands: make([]EntryCaptureOperand, 0, len(source.Inputs))}
	for index, port := range scope.params {
		info, defined := l.values.ValueInfo(port)
		if !defined {
			return EntryCapture{}, fmt.Errorf("asyncify: async if capture parameter port is undefined")
		}
		present := index >= len(scope.params)-scope.entryStackOperands
		capture.operands = append(capture.operands, EntryCaptureOperand{
			value: source.Inputs[index], present: present, valueType: info.Type,
			local: carriers.params[index], role: CaptureParameter, port: port,
		})
	}
	selectorPresent := source.inputStackOperands > scope.entryStackOperands
	capture.operands = append(capture.operands, EntryCaptureOperand{
		value: source.Selector, present: selectorPresent, valueType: wasm.ValI32,
		local: carriers.selector, role: CaptureSelector,
	})
	if err := verifyEntryCaptureShape(capture); err != nil {
		return EntryCapture{}, err
	}
	return capture, nil
}

func (l *LoweredControl) verifyEntryCapture(capture EntryCapture, owner Node) error {
	if l.values == nil || capture.owner == nil || owner != capture.owner {
		return fmt.Errorf("asyncify: capture has no retained source owner")
	}
	scope, ok := l.values.scopes[capture.scope]
	if !ok || scope.owner != capture.owner || scope.kind != IfScope {
		return fmt.Errorf("asyncify: capture scope does not match source if")
	}
	source, ok := l.values.operations[capture.owner]
	if !ok || !source.HasSelector || len(source.Inputs) != len(scope.params)+1 || source.Selector != source.Inputs[len(scope.params)] {
		return fmt.Errorf("asyncify: capture source selector does not match if operation")
	}
	if capture.stateGlobal != l.stateGlobal || capture.stateRewinding != l.stateRewinding || len(capture.operands) != len(source.Inputs) {
		return fmt.Errorf("asyncify: capture configuration or arity mismatch")
	}
	if source.inputStackOperands < 0 || source.inputStackOperands > len(source.Inputs) || scope.entryStackOperands < 0 || scope.entryStackOperands > len(scope.params) {
		return fmt.Errorf("asyncify: capture source presence is out of bounds")
	}
	for index, port := range scope.params {
		operand := capture.operands[index]
		info, defined := l.values.ValueInfo(port)
		local, assigned := l.portLocals[port]
		present := index >= len(scope.params)-scope.entryStackOperands
		if present != (index >= len(source.Inputs)-source.inputStackOperands) {
			return fmt.Errorf("asyncify: capture parameter %d presence disagrees with source operation", index)
		}
		if !defined || !assigned || operand.role != CaptureParameter || operand.port != port || operand.value != source.Inputs[index] || operand.present != present || operand.valueType != info.Type || operand.local != local {
			return fmt.Errorf("asyncify: capture parameter %d does not match source port", index)
		}
	}
	selector := capture.operands[len(scope.params)]
	selectorLocal, assigned := l.selectorLocals[capture.scope]
	selectorPresent := source.inputStackOperands > scope.entryStackOperands
	if selectorPresent != (len(source.Inputs)-1 >= len(source.Inputs)-source.inputStackOperands) {
		return fmt.Errorf("asyncify: capture selector presence disagrees with source operation")
	}
	if !assigned || selector.role != CaptureSelector || selector.port != 0 || selector.value != source.Selector || selector.present != selectorPresent || selector.valueType != wasm.ValI32 || selector.local != selectorLocal {
		return fmt.Errorf("asyncify: capture selector does not match source if")
	}
	return nil
}
