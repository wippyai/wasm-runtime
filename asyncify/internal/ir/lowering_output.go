package ir

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// ActionKind says why an instruction exists and which execution phase owns it.
// It deliberately does not derive ownership from Instruction.Synthetic.
type ActionKind uint8

const (
	InvalidAction ActionKind = iota
	SourceInstruction
	GuestCarrierInstruction
	GuestStructureInstruction
	RoutingInstruction
	AsyncIfEntryCapture
	ScopeResultTransfer
	SourceReturn
	SourceBranch
	SourceTrap
	SourcePortRead
	ScopeEntryTransfer
	SourceSelectorStore
	SourceSelectorLoad
	SourceSelectorRoute
)

// Action owns one tagged lowering payload and its source provenance. Actions
// are returned only by CopyActions, which gives the caller independent payload
// storage. Kind is the sole payload discriminant.
type Action struct {
	selector    SelectorOperation
	portRead    PortReadOperation
	origin      instructionOrigin
	instruction wasm.Instruction
	branch      BranchOperation
	returnOp    ReturnOperation
	trap        TrapOperation
	transfer    ScopeTransfer
	capture     EntryCapture
	kind        ActionKind
	domain      semantics.ExecutionDomain
}

func (a Action) Kind() ActionKind { return a.kind }
func (a Action) Domain() (semantics.ExecutionDomain, bool) {
	domain, ok := expectedActionDomain(a.kind)
	return domain, ok
}
func (a Action) Primitive() (wasm.Instruction, bool) {
	if isSelectorAction(a.kind) {
		return a.selector.instruction(a.kind), true
	}
	if a.kind == SourcePortRead {
		return a.portRead.instruction(), true
	}
	_, ok := expectedActionDomain(a.kind)
	return a.instruction, ok
}
func (a Action) Selector() (SelectorOperation, bool) { return a.selector, isSelectorAction(a.kind) }

func (a Action) ReadPort() (PortReadOperation, bool) { return a.portRead, a.kind == SourcePortRead }

func (a Action) Capture() (EntryCapture, bool) { return a.capture, a.kind == AsyncIfEntryCapture }
func (a Action) Transfer() (ScopeTransfer, bool) {
	return a.transfer, a.kind == ScopeResultTransfer || a.kind == ScopeEntryTransfer
}
func (a Action) Return() (ReturnOperation, bool) { return a.returnOp, a.kind == SourceReturn }
func (a Action) Branch() (BranchOperation, bool) { return a.branch, a.kind == SourceBranch }
func (a Action) Trap() (TrapOperation, bool)     { return a.trap, a.kind == SourceTrap }

func expectedActionDomain(kind ActionKind) (semantics.ExecutionDomain, bool) {
	switch kind {
	case SourceInstruction, GuestCarrierInstruction, GuestStructureInstruction, SourcePortRead, SourceSelectorStore, SourceSelectorLoad:
		return semantics.GuestExecution, true
	case RoutingInstruction, SourceSelectorRoute:
		return semantics.RewindRouting, true
	default:
		return 0, false
	}
}

// actionPayloadKind is intentionally distinct from ActionKind. An action kind
// selects its one active payload, while this enum lets verification reject all
// inactive payloads in one place before it validates the selected shape.
// Adding a payload therefore requires one explicit empty-shape predicate and
// one entry here; individual action branches cannot silently forget it.
type actionPayloadKind uint8

const (
	primitiveActionPayload actionPayloadKind = iota
	captureActionPayload
	transferActionPayload
	returnActionPayload
	branchActionPayload
	trapActionPayload
	portReadActionPayload
	selectorActionPayload
)

func payloadForActionKind(kind ActionKind) (actionPayloadKind, bool) {
	switch kind {
	case SourceSelectorStore, SourceSelectorLoad, SourceSelectorRoute:
		return selectorActionPayload, true
	case SourceInstruction, GuestCarrierInstruction, GuestStructureInstruction, RoutingInstruction:
		return primitiveActionPayload, true
	case AsyncIfEntryCapture:
		return captureActionPayload, true
	case ScopeResultTransfer, ScopeEntryTransfer:
		return transferActionPayload, true
	case SourceReturn:
		return returnActionPayload, true
	case SourceBranch:
		return branchActionPayload, true
	case SourceTrap:
		return trapActionPayload, true
	case SourcePortRead:
		return portReadActionPayload, true
	default:
		return 0, false
	}
}

func emptyActionInstruction(instruction wasm.Instruction) bool {
	return instruction.Opcode == 0 && instruction.Imm == nil && !instruction.Synthetic
}

func emptyEntryCapture(capture EntryCapture) bool {
	return capture.owner == nil && len(capture.operands) == 0 && capture.scope == 0 && capture.stateGlobal == 0 && capture.stateRewinding == 0
}

func emptyScopeTransfer(transfer ScopeTransfer) bool {
	return transfer.owner == nil && len(transfer.operands) == 0 && transfer.scope == 0 && transfer.arm == 0 && transfer.mode == 0
}

func emptyReturnOperation(operation ReturnOperation) bool {
	return operation.owner == nil && len(operation.operands) == 0 && len(operation.prefix) == 0 && operation.target == 0 && operation.mode == 0 && !operation.validationReachable
}

func emptyBranchOperation(operation BranchOperation) bool {
	return operation.owner == nil && len(operation.inputs) == 0 && len(operation.prefix) == 0 && len(operation.targets) == 0 && len(operation.fallthroughs) == 0 && len(operation.branchValues) == 0 && operation.selectorLocal == 0 && operation.opcode == 0 && operation.mode == 0 && !operation.validationReachable && !operation.hasSelectorLocal
}

// verifyInactiveActionPayloads is the single exclusion protocol for the
// tagged Action union. The predicates preserve every zero-value field in the
// old per-kind checks, including fields that are nonzero without an owner.
func verifyInactiveActionPayloads(action Action, active actionPayloadKind) error {
	if active != selectorActionPayload && action.selector != (SelectorOperation{}) {
		return fmt.Errorf("selector")
	}
	if active != portReadActionPayload && action.portRead != (PortReadOperation{}) {
		return fmt.Errorf("port read")
	}
	if active != primitiveActionPayload && !emptyActionInstruction(action.instruction) {
		return fmt.Errorf("primitive")
	}
	if active != captureActionPayload && !emptyEntryCapture(action.capture) {
		return fmt.Errorf("capture")
	}
	if active != transferActionPayload && !emptyScopeTransfer(action.transfer) {
		return fmt.Errorf("transfer")
	}
	if active != returnActionPayload && !emptyReturnOperation(action.returnOp) {
		return fmt.Errorf("return")
	}
	if active != branchActionPayload && !emptyBranchOperation(action.branch) {
		return fmt.Errorf("branch")
	}
	if active != trapActionPayload && !emptyTrapOperation(action.trap) {
		return fmt.Errorf("trap")
	}
	return nil
}

func verifyAction(action Action) error {
	payload, ok := payloadForActionKind(action.kind)
	if !ok {
		return fmt.Errorf("invalid lowered action kind")
	}
	if err := verifyInactiveActionPayloads(action, payload); err != nil {
		switch action.kind {
		case AsyncIfEntryCapture:
			return fmt.Errorf("invalid async if entry capture action: %w", err)
		case ScopeResultTransfer:
			return fmt.Errorf("invalid scope result transfer action: %w", err)
		case SourceReturn:
			return fmt.Errorf("invalid source return action: %w", err)
		case SourceBranch:
			return fmt.Errorf("invalid source branch action: %w", err)
		case SourceTrap:
			return fmt.Errorf("invalid source trap action: %w", err)
		default:
			return fmt.Errorf("primitive action has capture payload: %w", err)
		}
	}
	switch payload {
	case selectorActionPayload:
		expected, _ := expectedActionDomain(action.kind)
		if action.origin.owner != action.selector.owner || action.origin.source != nil || action.domain != expected {
			return fmt.Errorf("asyncify: invalid selector ownership or domain")
		}
		return verifySelectorShape(action.selector, action.kind)
	case portReadActionPayload:
		if action.origin.owner == nil || action.origin.owner != action.portRead.owner || action.origin.source != nil || action.domain != semantics.GuestExecution {
			return fmt.Errorf("invalid source port read action")
		}
		return verifyPortReadShape(action.portRead)
	case captureActionPayload:
		if action.origin.owner == nil || action.origin.source != nil || action.domain != 0 {
			return fmt.Errorf("invalid async if entry capture action")
		}
		return verifyEntryCaptureShape(action.capture)
	case transferActionPayload:
		if action.origin.owner == nil || action.origin.source != nil || action.domain != 0 {
			return fmt.Errorf("invalid scope result transfer action")
		}
		return verifyScopeTransferShape(action.transfer)
	case returnActionPayload:
		if action.origin.owner == nil || action.origin.source == nil || action.domain != 0 || action.origin.owner != action.origin.source || action.returnOp.owner != action.origin.source {
			return fmt.Errorf("invalid source return action")
		}
		return verifyReturnOperationShape(action.returnOp)
	case branchActionPayload:
		if action.origin.owner == nil || action.origin.source == nil || action.domain != 0 || action.origin.owner != action.origin.source || action.branch.owner != action.origin.source {
			return fmt.Errorf("invalid source branch action")
		}
		return verifyBranchOperationShape(action.branch)
	case trapActionPayload:
		if action.origin.owner == nil || action.origin.source == nil || action.domain != 0 || action.origin.owner != action.origin.source || action.trap.owner != action.origin.source {
			return fmt.Errorf("invalid source trap action")
		}
		return verifyTrapOperationShape(action.trap)
	}
	expected, _ := expectedActionDomain(action.kind)
	if action.domain != expected {
		return fmt.Errorf("lowered action domain does not match kind")
	}
	if (action.kind == RoutingInstruction) != action.instruction.Synthetic {
		return fmt.Errorf("lowered action synthetic marker does not match routing ownership")
	}
	if action.origin.owner == nil {
		return fmt.Errorf("lowered action has no source owner")
	}
	if action.kind == SourceInstruction && action.origin.source == nil {
		return fmt.Errorf("source action has no source occurrence")
	}
	if action.kind != SourceInstruction && action.origin.source != nil {
		return fmt.Errorf("generated action impersonates source occurrence")
	}
	if !actionOpcodeMatchesKind(action.kind, action.instruction.Opcode) {
		return fmt.Errorf("lowered action opcode does not match kind")
	}
	if err := verifyGeneratedActionPayload(action); err != nil {
		return err
	}
	if _, err := wasm.CloneInstruction(action.instruction); err != nil {
		return fmt.Errorf("invalid lowered action instruction: %w", err)
	}
	return nil
}

func actionOpcodeMatchesKind(kind ActionKind, opcode byte) bool {
	switch kind {
	case SourceInstruction:
		return true
	case GuestCarrierInstruction:
		return opcode == wasm.OpLocalGet || opcode == wasm.OpLocalSet
	case GuestStructureInstruction:
		switch opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpElse, wasm.OpEnd:
			return true
		}
	case RoutingInstruction:
		switch opcode {
		case wasm.OpGlobalGet, wasm.OpI32Const, wasm.OpI32Eq, wasm.OpI32Ne,
			wasm.OpIf, wasm.OpDrop, wasm.OpElse, wasm.OpLocalSet, wasm.OpLocalGet,
			wasm.OpI32Or, wasm.OpI32And, wasm.OpI32Eqz, wasm.OpEnd:
			return true
		}
	}
	return false
}

// loweringOutput is the only action sink for control lowering. Ownership is
// recorded when emitted, never inferred later from opcode shape.
type loweringOutput struct {
	err          error
	owner        Node
	branchDepths map[*InstrNode][]uint32
	actions      []Action
}

type instructionOrigin struct {
	owner  Node
	source *InstrNode
}

func (o *loweringOutput) append(kind ActionKind, instruction wasm.Instruction, origin instructionOrigin) {
	if o.err != nil {
		return
	}
	domain, ok := expectedActionDomain(kind)
	if !ok {
		o.err = fmt.Errorf("asyncify: lowering emitted invalid action kind")
		return
	}
	if !actionOpcodeMatchesKind(kind, instruction.Opcode) {
		o.err = fmt.Errorf("asyncify: lowering action opcode does not match kind")
		return
	}
	if origin.owner == nil || (kind == SourceInstruction && origin.source == nil) || (kind != SourceInstruction && origin.source != nil) {
		o.err = fmt.Errorf("asyncify: lowering action has inconsistent source provenance")
		return
	}
	// Synthetic is a redundant byte projection for legacy diagnostics. Action
	// ownership is declared exactly once by kind, so callers cannot create a
	// second, inconsistent routing declaration.
	instruction.Synthetic = kind == RoutingInstruction
	o.actions = append(o.actions, Action{instruction: instruction, origin: origin, kind: kind, domain: domain})
}

func (o *loweringOutput) source(node *InstrNode, instruction wasm.Instruction) {
	o.append(SourceInstruction, instruction, instructionOrigin{owner: node, source: node})
}
func (o *loweringOutput) guestCarrier(instruction wasm.Instruction) {
	o.append(GuestCarrierInstruction, instruction, instructionOrigin{owner: o.owner})
}
func (o *loweringOutput) guestStructure(instruction wasm.Instruction) {
	o.append(GuestStructureInstruction, instruction, instructionOrigin{owner: o.owner})
}
func (o *loweringOutput) routing(instruction wasm.Instruction) {
	o.append(RoutingInstruction, instruction, instructionOrigin{owner: o.owner})
}
func (o *loweringOutput) routingBatch(instructions []wasm.Instruction) {
	for _, instruction := range instructions {
		o.routing(instruction)
	}
}

func (o *loweringOutput) asyncIfEntryCapture(capture EntryCapture) {
	if o.err != nil {
		return
	}
	if err := verifyEntryCaptureShape(capture); err != nil {
		o.err = err
		return
	}
	if o.owner == nil || o.owner != capture.owner {
		o.err = fmt.Errorf("asyncify: capture owner does not match lowering owner")
		return
	}
	o.actions = append(o.actions, Action{origin: instructionOrigin{owner: o.owner}, capture: capture, kind: AsyncIfEntryCapture})
}

func (o *loweringOutput) scopeResultTransfer(transfer ScopeTransfer) {
	if o.err != nil {
		return
	}
	if err := verifyScopeTransferShape(transfer); err != nil {
		o.err = err
		return
	}
	if o.owner == nil || o.owner != transfer.owner {
		o.err = fmt.Errorf("asyncify: transfer owner does not match lowering owner")
		return
	}
	o.actions = append(o.actions, Action{origin: instructionOrigin{owner: o.owner}, transfer: transfer, kind: ScopeResultTransfer})
}

func (o *loweringOutput) sourceReturn(operation ReturnOperation) {
	if o.err != nil {
		return
	}
	if err := verifyReturnOperationShape(operation); err != nil {
		o.err = err
		return
	}
	if o.owner == nil || o.owner != operation.owner {
		o.err = fmt.Errorf("asyncify: source return owner does not match lowering owner")
		return
	}
	o.actions = append(o.actions, Action{origin: instructionOrigin{owner: o.owner, source: operation.owner}, returnOp: operation, kind: SourceReturn})
}

func (o *loweringOutput) sourceBranch(operation BranchOperation) {
	if o.err != nil {
		return
	}
	if err := verifyBranchOperationShape(operation); err != nil {
		o.err = err
		return
	}
	if o.owner == nil || o.owner != operation.owner {
		o.err = fmt.Errorf("asyncify: source branch owner does not match lowering owner")
		return
	}
	if o.branchDepths == nil {
		o.branchDepths = make(map[*InstrNode][]uint32)
	}
	depths := make([]uint32, len(operation.targets))
	for index, target := range operation.targets {
		depths[index] = target.depth
	}
	o.branchDepths[operation.owner] = depths
	o.actions = append(o.actions, Action{origin: instructionOrigin{owner: o.owner, source: operation.owner}, branch: operation, kind: SourceBranch})
}

func (o *loweringOutput) sourceTrap(operation TrapOperation) {
	if o.err != nil {
		return
	}
	if err := verifyTrapOperationShape(operation); err != nil {
		o.err = err
		return
	}
	if o.owner == nil || o.owner != operation.owner {
		o.err = fmt.Errorf("asyncify: source trap owner does not match lowering owner")
		return
	}
	o.actions = append(o.actions, Action{origin: instructionOrigin{owner: o.owner, source: operation.owner}, trap: operation, kind: SourceTrap})
}

// verifyOrigins rejects missing, duplicated or fabricated source occurrences.
// It additionally makes domain/kind ownership part of the checked contract.
func (a *Analysis) verifyOrigins(lowered *LoweredControl) error {
	instructions := a.instructions
	if lowered.source != nil {
		instructions = lowered.source.instructions
	}
	position := 0
	var sourceBytes, outputBytes bytes.Buffer
	for index, action := range lowered.actions {
		if err := lowered.verifyAction(action); err != nil {
			return fmt.Errorf("asyncify: instruction %d: %w", index, err)
		}
		origin := action.origin
		if _, exists := a.suspends[origin.owner]; !exists || (lowered.source != nil && !lowered.source.Active(origin.owner)) {
			return fmt.Errorf("asyncify: unknown lowering owner at instruction %d", index)
		}
		if origin.source == nil {
			if _, primitive := action.Primitive(); primitive && semantics.IsCallInstruction(action.instruction.Opcode) {
				return fmt.Errorf("asyncify: generated call has no source occurrence at instruction %d", index)
			}
			continue
		}
		node := origin.source
		if (action.kind != SourceInstruction && action.kind != SourceReturn && action.kind != SourceBranch && action.kind != SourceTrap) || origin.owner != node || position >= len(instructions) || instructions[position] != node {
			return fmt.Errorf("asyncify: inconsistent source occurrence at instruction %d", index)
		}
		position++
		if action.kind == SourceReturn {
			if node.Instr.Opcode != wasm.OpReturn {
				return fmt.Errorf("asyncify: source return action changed source occurrence at instruction %d", index)
			}
			continue
		}
		if action.kind == SourceBranch {
			if node.Instr.Opcode != wasm.OpBr && node.Instr.Opcode != wasm.OpBrIf && node.Instr.Opcode != wasm.OpBrTable {
				return fmt.Errorf("asyncify: source branch action changed source occurrence at instruction %d", index)
			}
			continue
		}
		if action.kind == SourceTrap {
			if node.Instr.Opcode != wasm.OpUnreachable {
				return fmt.Errorf("asyncify: source trap action changed source occurrence at instruction %d", index)
			}
			continue
		}
		instruction := action.instruction
		if instruction.Opcode != node.Instr.Opcode || instruction.Synthetic != node.Instr.Synthetic || reflect.TypeOf(instruction.Imm) != reflect.TypeOf(node.Instr.Imm) {
			return fmt.Errorf("asyncify: source opcode changed at instruction %d", index)
		}
		switch node.Instr.Opcode {
		case wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable:
			// Control-frame mapping owns rewritten target depths.
		default:
			sourceBytes.Reset()
			outputBytes.Reset()
			wasm.EncodeInstructionTo(&sourceBytes, &node.Instr)
			wasm.EncodeInstructionTo(&outputBytes, &instruction)
			if !bytes.Equal(sourceBytes.Bytes(), outputBytes.Bytes()) {
				return fmt.Errorf("asyncify: source instruction changed at instruction %d", index)
			}
		}
	}
	if position != len(instructions) {
		return fmt.Errorf("asyncify: source instruction omitted during lowering")
	}
	return a.verifyActionCoverage(lowered)
}

// SourceOperation provides immutable identities for one original instruction.
// Generated actions never impersonate source operations.
type SourceOperation struct {
	plan *ValuePlan
	node *InstrNode
}

func (s SourceOperation) InputCount() int  { return len(s.plan.operations[s.node].Inputs) }
func (s SourceOperation) OutputCount() int { return len(s.plan.operations[s.node].Outputs) }
func (s SourceOperation) InputValue(index int) ValueID {
	return s.plan.operations[s.node].Inputs[index]
}
func (s SourceOperation) OutputValue(index int) ValueID {
	return s.plan.operations[s.node].Outputs[index]
}

func (l *LoweredControl) SourceOperation(index int) (SourceOperation, bool) {
	if l.values == nil || index < 0 || index >= len(l.actions) || l.actions[index].kind == AsyncIfEntryCapture || l.actions[index].kind == ScopeResultTransfer || l.actions[index].origin.source == nil {
		return SourceOperation{}, false
	}
	return SourceOperation{plan: l.values, node: l.actions[index].origin.source}, true
}

func (l *LoweredControl) verifyAction(action Action) error {
	if err := verifyAction(action); err != nil {
		return err
	}
	if isSelectorAction(action.kind) {
		return l.verifySelector(action.selector, action.origin.owner, action.kind)
	}
	if action.kind == SourcePortRead {
		return l.verifyPortRead(action.portRead, action.origin.owner)
	}
	if action.kind == AsyncIfEntryCapture {
		return l.verifyEntryCapture(action.capture, action.origin.owner)
	}
	if action.kind == ScopeEntryTransfer {
		return l.verifyScopeEntryTransfer(action.transfer, action.origin.owner)
	}
	if action.kind == ScopeResultTransfer {
		return l.verifyScopeTransfer(action.transfer, action.origin.owner)
	}
	if action.kind == SourceReturn {
		return l.verifySourceReturn(action.returnOp, action.origin.owner, action.origin.source)
	}
	if action.kind == SourceBranch {
		return l.verifySourceBranch(action.branch, action.origin.owner, action.origin.source)
	}
	if action.kind == SourceTrap {
		return l.verifySourceTrap(action.trap, action.origin.owner, action.origin.source)
	}
	return nil
}

// CopyActions validates then deep-copies the authoritative action stream.
func (l *LoweredControl) CopyActions() ([]Action, error) {
	if l.values != nil {
		if err := l.values.control.verifyActionCoverage(l); err != nil {
			return nil, err
		}
	}
	result := make([]Action, len(l.actions))
	for index, action := range l.actions {
		if err := l.verifyAction(action); err != nil {
			return nil, fmt.Errorf("asyncify: action %d: %w", index, err)
		}
		instruction, err := wasm.CloneInstruction(action.instruction)
		if err != nil {
			return nil, err
		}
		result[index] = action
		result[index].instruction = instruction
		result[index].capture = action.capture.copy()
		result[index].transfer = action.transfer.copy()
		result[index].returnOp = action.returnOp.copy()
		result[index].branch = action.branch.copy()
		result[index].trap = action.trap.copy()
	}
	return result, nil
}

// CopyInstructions is a diagnostic byte projection; actions are authoritative.
func (l *LoweredControl) CopyInstructions() ([]wasm.Instruction, error) {
	actions, err := l.CopyActions()
	if err != nil {
		return nil, err
	}
	var result []wasm.Instruction
	for _, action := range actions {
		instructions, err := action.CopyInstructions()
		if err != nil {
			return nil, err
		}
		result = append(result, instructions...)
	}
	return result, nil
}

// CopyInstructions returns an owned byte projection of this one action.
func (a Action) CopyInstructions() ([]wasm.Instruction, error) {
	if err := verifyAction(a); err != nil {
		return nil, err
	}
	if isSelectorAction(a.kind) {
		return []wasm.Instruction{a.selector.instruction(a.kind)}, nil
	}
	if a.kind == SourcePortRead {
		return []wasm.Instruction{a.portRead.instruction()}, nil
	}
	if a.kind == AsyncIfEntryCapture {
		return a.capture.CopyInstructions()
	}
	if a.kind == ScopeResultTransfer || a.kind == ScopeEntryTransfer {
		return a.transfer.CopyInstructions()
	}
	if a.kind == SourceReturn {
		return a.returnOp.CopyInstructions()
	}
	if a.kind == SourceBranch {
		return a.branch.CopyInstructions()
	}
	if a.kind == SourceTrap {
		return a.trap.CopyInstructions()
	}
	instruction, err := wasm.CloneInstruction(a.instruction)
	if err != nil {
		return nil, err
	}
	return []wasm.Instruction{instruction}, nil
}

func (l *LoweredControl) SuspensionSites() []SuspensionSite {
	sites := make([]SuspensionSite, len(l.suspensions))
	copy(sites, l.suspensions)
	for index := range sites {
		sites[index].ControlLocals = append([]uint32(nil), sites[index].ControlLocals...)
	}
	return sites
}

func (o *loweringOutput) sourcePortRead(read PortReadOperation) {
	if o.err != nil {
		return
	}
	action := Action{origin: instructionOrigin{owner: read.owner}, portRead: read, kind: SourcePortRead, domain: semantics.GuestExecution}
	if err := verifyAction(action); err != nil {
		o.err = err
		return
	}
	o.actions = append(o.actions, action)
}

func (o *loweringOutput) scopeEntryTransfer(transfer ScopeTransfer) {
	if o.err != nil {
		return
	}
	action := Action{origin: instructionOrigin{owner: transfer.owner}, transfer: transfer, kind: ScopeEntryTransfer}
	if err := verifyAction(action); err != nil {
		o.err = err
		return
	}
	o.actions = append(o.actions, action)
}

func (o *loweringOutput) selectorAccess(operation SelectorOperation, kind ActionKind) {
	if o.err != nil {
		return
	}
	domain, _ := expectedActionDomain(kind)
	action := Action{origin: instructionOrigin{owner: operation.owner}, selector: operation, kind: kind, domain: domain}
	if err := verifyAction(action); err != nil {
		o.err = err
		return
	}
	o.actions = append(o.actions, action)
}
