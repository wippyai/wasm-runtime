package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// StackContract is the source-identity stack effect of one lowered action.
//
// Prefix is the portion of the source stack below the action's operands. A
// truncating action replaces its active stack with Prefix. Inputs are consumed
// above Prefix and Outputs are produced above it. The slices are deliberately
// private: a contract returned by StackContract cannot mutate lowering state.
// ValueID zero is retained because it can name a present polymorphic source
// value in diagnostic contracts.
type StackContract struct {
	inputs            []ValueID
	outputProvenance  []StackOutput
	outputs           []ValueID
	prefix            []ValueID
	truncatesToPrefix bool
	checked           bool
}

func (c StackContract) InputCount() int  { return len(c.inputs) }
func (c StackContract) OutputCount() int { return len(c.outputs) }
func (c StackContract) PrefixCount() int { return len(c.prefix) }

// InputValue returns zero outside the contract's input range. Zero is also a
// valid polymorphic source identity, so callers that need bounds information
// must compare their index with InputCount first.
func (c StackContract) InputValue(index int) ValueID { return stackContractValue(c.inputs, index) }

// OutputValue returns zero outside the contract's output range. Zero is also
// a valid polymorphic source identity; use OutputCount to validate an index.
func (c StackContract) OutputValue(index int) ValueID { return stackContractValue(c.outputs, index) }

// PrefixValue returns zero outside the contract's prefix range. Zero is also
// a valid polymorphic source identity; use PrefixCount to validate an index.
func (c StackContract) PrefixValue(index int) ValueID { return stackContractValue(c.prefix, index) }

func (c StackContract) TruncatesToPrefix() bool { return c.truncatesToPrefix }
func (c StackContract) Checked() bool           { return c.checked }

func stackContractValue(values []ValueID, index int) ValueID {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func checkedStackContract(inputs, outputs, prefix []ValueID, truncates bool) StackContract {
	return StackContract{
		inputs:            append([]ValueID(nil), inputs...),
		outputs:           append([]ValueID(nil), outputs...),
		prefix:            append([]ValueID(nil), prefix...),
		truncatesToPrefix: truncates,
		checked:           true,
	}
}

// StackContract returns the bounded source-stack contract for one authoritative
// lowered action. Legacy lowering has no ValuePlan and is explicitly unbound:
// its actions return an unchecked contract. Source-backed lowering is checked
// before a contract is exposed so malformed semantic actions cannot silently
// become a physical-local stack model.
func (l *LoweredControl) rawStackContract(index int) (StackContract, error) {
	if l == nil || index < 0 || index >= len(l.actions) {
		return StackContract{}, fmt.Errorf("asyncify: action index %d outside lowered control", index)
	}
	if l.values == nil {
		return StackContract{}, nil
	}

	action := l.actions[index]
	if err := l.verifyAction(action); err != nil {
		return StackContract{}, fmt.Errorf("asyncify: action %d has invalid source contract: %w", index, err)
	}

	switch action.kind {
	case SourceInstruction:
		operation, err := l.stackSourceOperation(action)
		if err != nil {
			return StackContract{}, fmt.Errorf("asyncify: action %d: %w", index, err)
		}
		return checkedStackContract(operation.Inputs, operation.Outputs, nil, false), nil

	case SourcePortRead:
		return checkedStackContract(nil, []ValueID{action.portRead.Port()}, nil, false), nil

	case SourceSelectorStore:
		return checkedStackContract([]ValueID{action.selector.Value()}, nil, nil, false), nil

	case SourceSelectorLoad:
		return checkedStackContract(nil, []ValueID{action.selector.Value()}, nil, false), nil

	case SourceSelectorRoute, RoutingInstruction:
		return StackContract{}, nil

	case GuestCarrierInstruction:
		// Source-backed lowering must name every guest stack carrier through a
		// source port read or another semantic action. Raw carriers are retained
		// only by the unbound structural lowering test path.
		return StackContract{}, fmt.Errorf("asyncify: action %d is a raw guest carrier in source-backed lowering", index)

	case GuestStructureInstruction:
		return l.guestStructureStackContract(index, action)

	case AsyncIfEntryCapture:
		return stackCaptureContract(action.capture), nil

	case ScopeEntryTransfer, ScopeResultTransfer:
		return stackTransferContract(action.transfer), nil

	case SourceBranch:
		return stackBranchContract(action.branch), nil

	case SourceReturn:
		return stackReturnContract(action.returnOp), nil

	case SourceTrap:
		return stackTrapContract(action.trap), nil
	default:
		return StackContract{}, fmt.Errorf("asyncify: action %d has unknown kind %d", index, action.kind)
	}
}

func (l *LoweredControl) stackSourceOperation(action Action) (valueOperation, error) {
	if action.origin.owner == nil || action.origin.source == nil || action.origin.owner != action.origin.source {
		return valueOperation{}, fmt.Errorf("source instruction has inconsistent ownership")
	}
	if action.instruction.Opcode != action.origin.source.Instr.Opcode {
		return valueOperation{}, fmt.Errorf("source instruction opcode does not match its source occurrence")
	}
	switch action.origin.source.Instr.Opcode {
	case wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable, wasm.OpReturn, wasm.OpUnreachable:
		// These opcodes have source-owned semantic actions. Treating one as a
		// generic primitive would discard its prefix, fallthrough, or terminal
		// stack semantics.
		return valueOperation{}, fmt.Errorf("source control instruction has no semantic action")
	}
	operation, ok := l.values.operations[action.origin.source]
	if !ok {
		return valueOperation{}, fmt.Errorf("source instruction has no value operation")
	}
	return operation, nil
}

func (l *LoweredControl) guestStructureStackContract(index int, action Action) (StackContract, error) {
	if action.instruction.Opcode != wasm.OpIf {
		return StackContract{}, nil
	}
	owner, ok := action.origin.owner.(*IfNode)
	if !ok || owner == nil {
		return StackContract{}, fmt.Errorf("asyncify: action %d guest if has no source if owner", index)
	}
	operation, exists := l.values.operations[owner]
	if !exists || !operation.HasSelector {
		return StackContract{}, fmt.Errorf("asyncify: action %d guest if has no source selector", index)
	}
	if l.nodeSuspends(owner) {
		return StackContract{}, fmt.Errorf("asyncify: action %d guest if is not a plain-if action", index)
	}
	return checkedStackContract([]ValueID{operation.Selector}, nil, nil, false), nil
}

func (l *LoweredControl) nodeSuspends(node Node) bool {
	if l.source != nil {
		return l.source.NodeSuspends(node)
	}
	return l.values.control.suspends[node]
}

func stackCaptureContract(capture EntryCapture) StackContract {
	inputs := make([]ValueID, 0, capture.OperandCount())
	for index := 0; index < capture.OperandCount(); index++ {
		operand, _ := capture.Operand(index)
		if operand.Present() {
			inputs = append(inputs, operand.Value())
		}
	}
	return checkedStackContract(inputs, nil, nil, false)
}

func stackTransferContract(transfer ScopeTransfer) StackContract {
	inputs := make([]ValueID, 0, transfer.OperandCount())
	for index := 0; index < transfer.OperandCount(); index++ {
		operand, _ := transfer.Operand(index)
		if operand.Present() {
			inputs = append(inputs, operand.Value())
		}
	}
	return checkedStackContract(inputs, nil, nil, false)
}

func stackBranchContract(branch BranchOperation) StackContract {
	prefix := make([]ValueID, 0, branch.PrefixCount())
	for index := 0; index < branch.PrefixCount(); index++ {
		operand, _ := branch.PrefixOperand(index)
		prefix = append(prefix, operand.Value())
	}
	inputs := make([]ValueID, 0, branch.InputCount())
	for index := 0; index < branch.InputCount(); index++ {
		input, _ := branch.Input(index)
		if input.Present() {
			inputs = append(inputs, input.Value())
		}
	}
	var outputs []ValueID
	if branch.Opcode() == wasm.OpBrIf {
		outputs = make([]ValueID, 0, branch.FallthroughCount())
		for index := 0; index < branch.FallthroughCount(); index++ {
			output, _ := branch.Fallthrough(index)
			outputs = append(outputs, output.Value())
		}
	}
	return checkedStackContract(inputs, outputs, prefix, branch.Opcode() != wasm.OpBrIf)
}

func stackReturnContract(operation ReturnOperation) StackContract {
	prefix := make([]ValueID, 0, operation.PrefixCount())
	for index := 0; index < operation.PrefixCount(); index++ {
		operand, _ := operation.PrefixOperand(index)
		prefix = append(prefix, operand.Value())
	}
	inputs := make([]ValueID, 0, operation.OperandCount())
	for index := 0; index < operation.OperandCount(); index++ {
		operand, _ := operation.Operand(index)
		if operand.Present() {
			inputs = append(inputs, operand.Value())
		}
	}
	return checkedStackContract(inputs, nil, prefix, true)
}

func stackTrapContract(operation TrapOperation) StackContract {
	prefix := make([]ValueID, 0, operation.PrefixCount())
	for index := 0; index < operation.PrefixCount(); index++ {
		operand, _ := operation.PrefixOperand(index)
		prefix = append(prefix, operand.Value())
	}
	return checkedStackContract(nil, nil, prefix, true)
}
