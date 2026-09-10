package ir

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// BranchInputRole distinguishes branch result operands from the final selector
// consumed by br_if and br_table.
type BranchInputRole uint8

const (
	BranchValue BranchInputRole = iota
	BranchSelector
)

// BranchInput is an ordered source operand. Present is independent from Value:
// a present zero is the validation-only polymorphic unknown.
type BranchInput struct {
	value     ValueID
	valueType wasm.ValType
	role      BranchInputRole
	present   bool
}

func (i BranchInput) Value() ValueID        { return i.value }
func (i BranchInput) Type() wasm.ValType    { return i.valueType }
func (i BranchInput) Role() BranchInputRole { return i.role }
func (i BranchInput) Present() bool         { return i.present }

// BranchPrefixOperand is the validation stack retained below the currently
// active source frame after a taken unconditional branch/table branch.
type BranchPrefixOperand struct {
	value     ValueID
	valueType wasm.ValType
}

func (o BranchPrefixOperand) Value() ValueID     { return o.value }
func (o BranchPrefixOperand) Type() wasm.ValType { return o.valueType }

// BranchTargetPort binds one source branch value to a target port and its fixed
// lowered carrier. It says nothing about which table alternative is selected.
type BranchTargetPort struct {
	port      ValueID
	value     ValueID
	local     uint32
	valueType wasm.ValType
}

func (p BranchTargetPort) Port() ValueID      { return p.port }
func (p BranchTargetPort) Value() ValueID     { return p.value }
func (p BranchTargetPort) Local() uint32      { return p.local }
func (p BranchTargetPort) Type() wasm.ValType { return p.valueType }

// BranchTarget is one source branch alternative. Ordinal preserves every
// br_table label and default occurrence, including repeated labels. Depth is
// the target depth in the lowered source-control frame, before diagnostic-only
// dispatcher blocks are added.
type BranchTarget struct {
	ports   []BranchTargetPort
	label   LabelID
	depth   uint32
	ordinal int
}

func (t BranchTarget) Label() LabelID { return t.label }
func (t BranchTarget) Depth() uint32  { return t.depth }
func (t BranchTarget) Ordinal() int   { return t.ordinal }
func (t BranchTarget) PortCount() int { return len(t.ports) }
func (t BranchTarget) Port(index int) (BranchTargetPort, bool) {
	if index < 0 || index >= len(t.ports) {
		return BranchTargetPort{}, false
	}
	return t.ports[index], true
}

// BranchFallthrough is a br_if result on its not-taken validation path. Value
// can be a fresh typed validation definition when Concrete is false; it must
// not be read as a runtime local in that case.
type BranchFallthrough struct {
	value     ValueID
	valueType wasm.ValType
	present   bool
	concrete  bool
}

func (o BranchFallthrough) Value() ValueID     { return o.value }
func (o BranchFallthrough) Type() wasm.ValType { return o.valueType }
func (o BranchFallthrough) Present() bool      { return o.present }
func (o BranchFallthrough) Concrete() bool     { return o.concrete }

// BranchOperation atomically owns an original br, br_if, or br_table and every
// source fact needed to execute it. Targets are alternatives, never a batch of
// simultaneous carrier writes.
type BranchOperation struct {
	owner               *InstrNode
	inputs              []BranchInput
	prefix              []BranchPrefixOperand
	targets             []BranchTarget
	fallthroughs        []BranchFallthrough
	branchValues        []uint32
	selectorLocal       uint32
	opcode              byte
	mode                TransferMode
	validationReachable bool
	hasSelectorLocal    bool
}

func (b BranchOperation) Opcode() byte              { return b.opcode }
func (b BranchOperation) Mode() TransferMode        { return b.mode }
func (b BranchOperation) ValidationReachable() bool { return b.validationReachable }
func (b BranchOperation) InputCount() int           { return len(b.inputs) }
func (b BranchOperation) PrefixCount() int          { return len(b.prefix) }
func (b BranchOperation) TargetCount() int          { return len(b.targets) }
func (b BranchOperation) FallthroughCount() int     { return len(b.fallthroughs) }
func (b BranchOperation) Input(index int) (BranchInput, bool) {
	if index < 0 || index >= len(b.inputs) {
		return BranchInput{}, false
	}
	return b.inputs[index], true
}
func (b BranchOperation) PrefixOperand(index int) (BranchPrefixOperand, bool) {
	if index < 0 || index >= len(b.prefix) {
		return BranchPrefixOperand{}, false
	}
	return b.prefix[index], true
}
func (b BranchOperation) Target(index int) (BranchTarget, bool) {
	if index < 0 || index >= len(b.targets) {
		return BranchTarget{}, false
	}
	return b.targets[index], true
}
func (b BranchOperation) Fallthrough(index int) (BranchFallthrough, bool) {
	if index < 0 || index >= len(b.fallthroughs) {
		return BranchFallthrough{}, false
	}
	return b.fallthroughs[index], true
}

func (b BranchOperation) copy() BranchOperation {
	b.inputs = append([]BranchInput(nil), b.inputs...)
	b.prefix = append([]BranchPrefixOperand(nil), b.prefix...)
	b.targets = append([]BranchTarget(nil), b.targets...)
	for index := range b.targets {
		b.targets[index].ports = append([]BranchTargetPort(nil), b.targets[index].ports...)
	}
	b.fallthroughs = append([]BranchFallthrough(nil), b.fallthroughs...)
	b.branchValues = append([]uint32(nil), b.branchValues...)
	return b
}

func branchMode(opcode byte) TransferMode {
	if opcode == wasm.OpBrIf {
		return CopyValues
	}
	return MoveValues
}

func verifyBranchOperationShape(b BranchOperation) error {
	if b.owner == nil || (b.opcode != wasm.OpBr && b.opcode != wasm.OpBrIf && b.opcode != wasm.OpBrTable) || b.owner.Instr.Opcode != b.opcode || b.mode != branchMode(b.opcode) || len(b.targets) == 0 {
		return fmt.Errorf("asyncify: incomplete source branch operation")
	}
	selectorCount := 0
	for index, input := range b.inputs {
		selector := input.role == BranchSelector
		if selector {
			selectorCount++
			if index != len(b.inputs)-1 || input.valueType != wasm.ValI32 {
				return fmt.Errorf("asyncify: invalid branch selector")
			}
		} else if input.role != BranchValue {
			return fmt.Errorf("asyncify: invalid branch input role")
		}
		if !input.present && input.value != 0 {
			return fmt.Errorf("asyncify: absent branch input has a value")
		}
	}
	expectsSelector := b.opcode == wasm.OpBrIf || b.opcode == wasm.OpBrTable
	if expectsSelector != (selectorCount == 1) || (!expectsSelector && selectorCount != 0) || (b.opcode != wasm.OpBrIf && len(b.fallthroughs) != 0) {
		return fmt.Errorf("asyncify: inconsistent branch input shape")
	}
	for index, target := range b.targets {
		if target.ordinal != index {
			return fmt.Errorf("asyncify: branch target ordinal mismatch")
		}
		for _, port := range target.ports {
			if port.port == 0 {
				return fmt.Errorf("asyncify: branch target has no port")
			}
		}
	}
	if len(b.branchValues) != 0 && b.opcode != wasm.OpBrTable {
		return fmt.Errorf("asyncify: non-table branch owns table scratch")
	}
	return nil
}

// newSourceBranch derives the complete branch action from source operation and
// edge facts plus the already-active lowered source frames. No emitted carrier
// instruction or rewritten depth is used as its source of truth.
func (l *linearizer) newSourceBranch(node *InstrNode) (BranchOperation, error) {
	if l.values == nil || node == nil || (node.Instr.Opcode != wasm.OpBr && node.Instr.Opcode != wasm.OpBrIf && node.Instr.Opcode != wasm.OpBrTable) {
		return BranchOperation{}, fmt.Errorf("asyncify: source branch requires source values")
	}
	operation, ok := l.values.operations[node]
	if !ok || operation.OpaqueControl || operation.inputStackOperands < 0 || operation.inputStackOperands > len(operation.Inputs) || len(node.branchTargets) == 0 {
		return BranchOperation{}, fmt.Errorf("asyncify: source branch has inconsistent source operation")
	}
	result := BranchOperation{owner: node, opcode: node.Instr.Opcode, mode: branchMode(node.Instr.Opcode), validationReachable: operation.ValidationReachable}
	for _, value := range operation.controlPrefix {
		info, defined := l.values.ValueInfo(value)
		if value != 0 && !defined {
			return BranchOperation{}, fmt.Errorf("asyncify: undefined branch validation prefix")
		}
		result.prefix = append(result.prefix, BranchPrefixOperand{value: value, valueType: info.Type})
	}
	transfers := l.values.transfers[node]
	if operation.ValidationReachable && len(transfers) != len(node.branchTargets) {
		return BranchOperation{}, fmt.Errorf("asyncify: source branch has incomplete target edges")
	}
	if !operation.ValidationReachable && len(transfers) != 0 {
		return BranchOperation{}, fmt.Errorf("asyncify: unreachable source branch has target edges")
	}
	for ordinal, label := range node.branchTargets {
		var targetPorts []ValueID
		var from []ValueID
		if operation.ValidationReachable {
			edgeIndex := transfers[ordinal]
			if edgeIndex < 0 || edgeIndex >= len(l.values.edges) {
				return BranchOperation{}, fmt.Errorf("asyncify: source branch has invalid target edge")
			}
			edge := l.values.edges[edgeIndex]
			if edge.Source != node || edge.Kind != ControlBranch || edge.Target != label || edge.TargetOrdinal != ordinal || edge.Mode != result.mode || edge.Arm != NoArm {
				return BranchOperation{}, fmt.Errorf("asyncify: source branch target edge mismatch")
			}
			targetPorts, from = edge.To, edge.From
		} else {
			scope, exists := l.values.scopes[label]
			if !exists {
				return BranchOperation{}, fmt.Errorf("asyncify: unreachable source branch has unknown target")
			}
			targetPorts = scope.results
			if scope.kind == LoopScope {
				targetPorts = scope.params
			}
		}
		frameIndex := l.targetFrame(node, ordinal)
		if frameIndex < 0 {
			return BranchOperation{}, fmt.Errorf("asyncify: source branch has no lowered target frame")
		}
		target := BranchTarget{label: label, ordinal: ordinal, depth: uint32(len(l.frames) - 1 - frameIndex), ports: make([]BranchTargetPort, len(targetPorts))}
		frame := l.frames[frameIndex]
		if len(frame.targetLocals) != len(targetPorts) || len(frame.targetTypes) != len(targetPorts) {
			return BranchOperation{}, fmt.Errorf("asyncify: source branch target frame mismatch")
		}
		for index, port := range targetPorts {
			info, defined := l.values.ValueInfo(port)
			local, assigned := l.storage.portLocals[port]
			if !defined || !assigned || info.Type != frame.targetTypes[index] || local != frame.targetLocals[index] {
				return BranchOperation{}, fmt.Errorf("asyncify: source branch target port mismatch")
			}
			value := ValueID(0)
			if operation.ValidationReachable {
				value = from[index]
			}
			target.ports[index] = BranchTargetPort{port: port, value: value, local: local, valueType: info.Type}
		}
		result.targets = append(result.targets, target)
	}
	valueCount := len(result.targets[0].ports)
	if len(operation.Inputs) != valueCount || node.Instr.Opcode == wasm.OpBrIf || node.Instr.Opcode == wasm.OpBrTable {
		// br_if and br_table append exactly one selector after their value suffix.
		if len(operation.Inputs) != valueCount+1 || (node.Instr.Opcode != wasm.OpBrIf && node.Instr.Opcode != wasm.OpBrTable) {
			return BranchOperation{}, fmt.Errorf("asyncify: source branch input arity mismatch")
		}
	}
	for index, value := range operation.Inputs {
		role := BranchValue
		var valueType wasm.ValType
		if index < valueCount {
			valueType = result.targets[0].ports[index].valueType
		} else {
			role, valueType = BranchSelector, wasm.ValI32
		}
		for _, target := range result.targets {
			if index < valueCount && !operation.ValidationReachable {
				target.ports[index].value = value
			}
			if index < valueCount && (len(target.ports) != valueCount || target.ports[index].valueType != valueType || target.ports[index].value != value) {
				return BranchOperation{}, fmt.Errorf("asyncify: source branch alternatives disagree")
			}
		}
		present := index >= len(operation.Inputs)-operation.inputStackOperands
		result.inputs = append(result.inputs, BranchInput{value: value, valueType: valueType, role: role, present: present})
	}
	if node.Instr.Opcode == wasm.OpBrIf {
		if len(operation.Outputs) != valueCount {
			return BranchOperation{}, fmt.Errorf("asyncify: source conditional branch fallthrough mismatch")
		}
		for index, value := range operation.Outputs {
			info, defined := l.values.ValueInfo(value)
			input := result.inputs[index]
			if !defined || info.Type != input.valueType {
				return BranchOperation{}, fmt.Errorf("asyncify: source conditional branch fallthrough type mismatch")
			}
			result.fallthroughs = append(result.fallthroughs, BranchFallthrough{value: value, valueType: info.Type, present: input.present, concrete: input.value != 0})
		}
	}
	carriers := l.carriers(node)
	if valueCount != 0 && (node.Instr.Opcode == wasm.OpBrIf || node.Instr.Opcode == wasm.OpBrTable) {
		if !carriers.hasSelector {
			return BranchOperation{}, fmt.Errorf("asyncify: source branch has no selector carrier")
		}
		result.selectorLocal, result.hasSelectorLocal = carriers.selector, true
	}
	if node.Instr.Opcode == wasm.OpBrTable && valueCount != 0 {
		if len(carriers.branchValues) != valueCount {
			return BranchOperation{}, fmt.Errorf("asyncify: source table branch has no value carriers")
		}
		result.branchValues = append([]uint32(nil), carriers.branchValues...)
	}
	if err := verifyBranchOperationShape(result); err != nil {
		return BranchOperation{}, err
	}
	return result, nil
}

func (l *LoweredControl) verifySourceBranch(operation BranchOperation, owner Node, source *InstrNode) error {
	if l.values == nil || operation.owner == nil || owner != operation.owner || source != operation.owner {
		return fmt.Errorf("asyncify: source branch has no retained source owner")
	}
	// Rebuild from immutable source facts and the fixed storage/frame lowering
	// contract is not available after emission. Verify the complete static
	// source portion here; target locals/depths were checked during construction
	// and are immutable action payloads validated by their source ports below.
	if err := verifyBranchOperationShape(operation); err != nil {
		return err
	}
	carriers, retained := l.branchCarriers[source]
	if !retained || operation.hasSelectorLocal != carriers.hasSelector || operation.selectorLocal != carriers.selector || !slices.Equal(operation.branchValues, carriers.branchValues) {
		return fmt.Errorf("asyncify: source branch carrier allocation mismatch")
	}
	values := l.values
	op, exists := values.operations[source]
	if !exists || source.Instr.Opcode != operation.opcode || operation.validationReachable != op.ValidationReachable || operation.mode != branchMode(operation.opcode) || len(operation.inputs) != len(op.Inputs) || len(operation.prefix) != len(op.controlPrefix) || len(operation.targets) != len(source.branchTargets) {
		return fmt.Errorf("asyncify: source branch operation mismatch")
	}
	for index, value := range op.controlPrefix {
		info, defined := values.ValueInfo(value)
		prefix := operation.prefix[index]
		if (value != 0 && !defined) || prefix.value != value || prefix.valueType != info.Type {
			return fmt.Errorf("asyncify: source branch prefix %d mismatch", index)
		}
	}
	for index, input := range operation.inputs {
		present := index >= len(op.Inputs)-op.inputStackOperands
		selector := index == len(op.Inputs)-1 && (operation.opcode == wasm.OpBrIf || operation.opcode == wasm.OpBrTable)
		if input.value != op.Inputs[index] || input.present != present || (op.ValidationReachable && (!input.present || input.value == 0)) || (selector && (input.role != BranchSelector || input.valueType != wasm.ValI32)) || (!selector && (input.role != BranchValue || len(operation.targets) == 0 || index >= len(operation.targets[0].ports) || input.valueType != operation.targets[0].ports[index].valueType)) {
			return fmt.Errorf("asyncify: source branch input %d mismatch", index)
		}
	}
	transfers := values.transfers[source]
	if op.ValidationReachable && len(transfers) != len(operation.targets) {
		return fmt.Errorf("asyncify: source branch target transfer mismatch")
	}
	if !op.ValidationReachable && len(transfers) != 0 {
		return fmt.Errorf("asyncify: unreachable source branch has target transfer")
	}
	for ordinal, target := range operation.targets {
		depths, recorded := l.branchDepths[source]
		if !recorded || len(depths) != len(operation.targets) || target.depth != depths[ordinal] || target.ordinal != ordinal || target.label != source.branchTargets[ordinal] {
			return fmt.Errorf("asyncify: source branch target %d depth mismatch", ordinal)
		}
		ports := []ValueID(nil)
		from := []ValueID(nil)
		if op.ValidationReachable {
			edge := values.edges[transfers[ordinal]]
			if edge.Source != source || edge.Kind != ControlBranch || edge.Target != target.label || edge.TargetOrdinal != target.ordinal || edge.Mode != operation.mode {
				return fmt.Errorf("asyncify: source branch target %d mismatch", ordinal)
			}
			ports, from = edge.To, edge.From
		} else {
			scope, ok := values.scopes[target.label]
			if !ok {
				return fmt.Errorf("asyncify: unreachable source branch has unknown target")
			}
			ports = scope.results
			if scope.kind == LoopScope {
				ports = scope.params
			}
			from = op.Inputs[:len(ports)]
		}
		if len(target.ports) != len(ports) {
			return fmt.Errorf("asyncify: source branch target %d port count mismatch", ordinal)
		}
		for index, port := range target.ports {
			info, defined := values.ValueInfo(ports[index])
			local, assigned := l.portLocals[ports[index]]
			if !defined || !assigned || port.port != ports[index] || port.value != from[index] || port.valueType != info.Type || port.local != local {
				return fmt.Errorf("asyncify: source branch target %d port %d mismatch", ordinal, index)
			}
		}
	}
	if operation.opcode == wasm.OpBrIf {
		if len(operation.fallthroughs) != len(op.Outputs) {
			return fmt.Errorf("asyncify: source conditional branch fallthrough mismatch")
		}
		for index, output := range operation.fallthroughs {
			info, defined := values.ValueInfo(op.Outputs[index])
			input := operation.inputs[index]
			if !defined || output.value != op.Outputs[index] || output.valueType != info.Type || output.present != input.present || output.concrete != (input.value != 0) {
				return fmt.Errorf("asyncify: source conditional branch fallthrough %d mismatch", index)
			}
		}
	}
	return nil
}

// CopyableInClosedRegion permits the engine's whole-region byte projection only
// when the semantic branch has no carrier writes or captures. Its projection is
// then exactly its original one-instruction branch form.
func (b BranchOperation) CopyableInClosedRegion() bool {
	if err := verifyBranchOperationShape(b); err != nil {
		return false
	}
	if b.hasSelectorLocal || len(b.branchValues) != 0 {
		return false
	}
	for _, target := range b.targets {
		if len(target.ports) != 0 {
			return false
		}
	}
	return true
}

func (b BranchOperation) sourceInstruction() (wasm.Instruction, error) {
	instruction, err := wasm.CloneInstruction(b.owner.Instr)
	if err != nil {
		return wasm.Instruction{}, err
	}
	switch b.opcode {
	case wasm.OpBr, wasm.OpBrIf:
		instruction.Imm = wasm.BranchImm{LabelIdx: b.targets[0].depth}
	case wasm.OpBrTable:
		imm, ok := instruction.Imm.(wasm.BrTableImm)
		if !ok || len(imm.Labels)+1 != len(b.targets) {
			return wasm.Instruction{}, fmt.Errorf("asyncify: source table branch immediate mismatch")
		}
		for index := range imm.Labels {
			imm.Labels[index] = b.targets[index].depth
		}
		imm.Default = b.targets[len(imm.Labels)].depth
		instruction.Imm = imm
	}
	return instruction, nil
}

func appendBranchStores(instructions []wasm.Instruction, target BranchTarget, values []uint32) []wasm.Instruction {
	for index := len(target.ports) - 1; index >= 0; index-- {
		port := target.ports[index]
		if port.value == 0 {
			instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpDrop})
			continue
		}
		if values != nil {
			instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: values[index]}})
		}
		instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: port.local}})
	}
	return instructions
}

// CopyInstructions is the diagnostic/liveness byte projection. Runtime branch
// execution consumes BranchOperation directly. Table writes are routed through
// a dispatcher so each alternative writes only its selected target ports.
func (b BranchOperation) CopyInstructions() ([]wasm.Instruction, error) {
	if err := verifyBranchOperationShape(b); err != nil {
		return nil, err
	}
	instruction, err := b.sourceInstruction()
	if err != nil {
		return nil, err
	}
	if b.CopyableInClosedRegion() {
		return []wasm.Instruction{instruction}, nil
	}
	switch b.opcode {
	case wasm.OpBr:
		result := appendBranchStores(nil, b.targets[0], nil)
		return append(result, instruction), nil
	case wasm.OpBrIf:
		if !b.hasSelectorLocal {
			return nil, fmt.Errorf("asyncify: conditional branch projection lacks selector carrier")
		}
		result := []wasm.Instruction{
			{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: b.selectorLocal}},
			{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: b.selectorLocal}},
			{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		}
		result = appendBranchStores(result, b.targets[0], nil)
		result = append(result, wasm.Instruction{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: b.targets[0].depth + 1}}, wasm.Instruction{Opcode: wasm.OpEnd})
		return result, nil
	case wasm.OpBrTable:
		if !b.hasSelectorLocal || len(b.branchValues) == 0 {
			return nil, fmt.Errorf("asyncify: table branch projection lacks carriers")
		}
		result := []wasm.Instruction{{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: b.selectorLocal}}}
		for index := len(b.branchValues) - 1; index >= 0; index-- {
			result = append(result, wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: b.branchValues[index]}})
		}
		// Canonicalize repeated labels into one dispatcher continuation while
		// retaining every ordinal in BranchOperation itself.
		type targetKey struct {
			label LabelID
			depth uint32
		}
		unique := make([]BranchTarget, 0, len(b.targets))
		which := make([]int, len(b.targets))
		uniqueIndex := make(map[targetKey]int, len(b.targets))
		for index, target := range b.targets {
			key := targetKey{label: target.label, depth: target.depth}
			found, exists := uniqueIndex[key]
			if !exists {
				found = len(unique)
				unique = append(unique, target)
				uniqueIndex[key] = found
			}
			which[index] = found
		}
		for range unique {
			result = append(result, wasm.Instruction{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}})
		}
		result = append(result, wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: b.selectorLocal}})
		imm := wasm.BrTableImm{Labels: make([]uint32, len(b.targets)-1)}
		for index := range imm.Labels {
			imm.Labels[index] = uint32(len(unique) - 1 - which[index])
		}
		imm.Default = uint32(len(unique) - 1 - which[len(b.targets)-1])
		result = append(result, wasm.Instruction{Opcode: wasm.OpBrTable, Imm: imm})
		for index := len(unique) - 1; index >= 0; index-- {
			result = append(result, wasm.Instruction{Opcode: wasm.OpEnd})
			result = appendBranchStores(result, unique[index], b.branchValues)
			result = append(result, wasm.Instruction{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: unique[index].depth + uint32(index)}})
		}
		return result, nil
	}
	return nil, fmt.Errorf("asyncify: unsupported source branch projection")
}
