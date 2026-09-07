package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// Stack operations enforce the innermost source control frame boundary.
// Polymorphic operands exist only at an unreachable frame base; concrete
// values above that base remain subject to type checks.
func (b *valueBuilder) define(owner Node, port ValueKind, position int, vt wasm.ValType) ValueID {
	if vt == 0 {
		return 0
	}
	b.plan.definitions = append(b.plan.definitions, sourceValue{Owner: owner, Port: port, Position: position, Type: vt})
	return ValueID(len(b.plan.definitions))
}

func (b *valueBuilder) valueType(id ValueID) wasm.ValType {
	if id == 0 {
		return 0
	}
	return b.plan.definitions[id-1].Type
}

func (b *valueBuilder) frame(owner Node, label LabelID, params, results []wasm.ValType, loop bool) *valueFrame {
	f := &valueFrame{label: label, base: len(b.stack), loop: loop}
	for i, vt := range params {
		f.params = append(f.params, b.define(owner, ControlParameter, i, vt))
	}
	for i, vt := range results {
		f.results = append(f.results, b.define(owner, ControlResult, i, vt))
	}
	kind := FunctionScope
	if label != 0 {
		kind = BlockScope
		if loop {
			kind = LoopScope
		}
		if _, ok := owner.(*IfNode); ok {
			kind = IfScope
		}
	}
	b.plan.scopes[label] = valueScope{entryValidationReachable: len(b.frames) > 0 && !b.frames[len(b.frames)-1].unreachable, owner: owner, kind: kind, params: append([]ValueID(nil), f.params...), results: append([]ValueID(nil), f.results...)}
	return f
}

func (b *valueBuilder) pop(expected wasm.ValType) (ValueID, error) {
	f := b.frames[len(b.frames)-1]
	if len(b.stack) == f.base {
		if f.unreachable {
			return 0, nil
		}
		return 0, fmt.Errorf("source operand underflow at label %d", f.label)
	}
	id := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	actual := b.valueType(id)
	if actual != 0 && expected != 0 && actual != expected {
		return 0, fmt.Errorf("source operand type %v, expected %v", actual, expected)
	}
	return id, nil
}

func (b *valueBuilder) popTypes(types []wasm.ValType) ([]ValueID, error) {
	ids := make([]ValueID, len(types))
	for i := len(types) - 1; i >= 0; i-- {
		id, err := b.pop(types[i])
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func (b *valueBuilder) types(ids []ValueID) []wasm.ValType {
	types := make([]wasm.ValType, len(ids))
	for i, id := range ids {
		types[i] = b.valueType(id)
	}
	return types
}

func (b *valueBuilder) target(label LabelID) (*valueFrame, error) {
	for i := len(b.frames) - 1; i >= 0; i-- {
		if b.frames[i].label == label {
			return b.frames[i], nil
		}
	}
	return nil, fmt.Errorf("inactive source value label %d", label)
}

func (b *valueBuilder) targetValues(f *valueFrame) []ValueID {
	if f.loop {
		return f.params
	}
	return f.results
}

func (b *valueBuilder) edge(source Node, kind EdgeKind, f *valueFrame, values []ValueID, port ValueKind, mode TransferMode, arm ControlArm, ordinal int) {
	if b.frames[len(b.frames)-1].unreachable {
		return
	}
	target := f.results
	if port == ControlParameter {
		target = f.params
	}
	b.plan.transfers[source] = append(b.plan.transfers[source], len(b.plan.edges))
	b.plan.edges = append(b.plan.edges, valueEdge{Source: source, Kind: kind, Target: f.label, From: append([]ValueID(nil), values...), To: append([]ValueID(nil), target...), Port: port, Mode: mode, Arm: arm, TargetOrdinal: ordinal})
}

func (b *valueBuilder) unreachable() {
	f := b.frames[len(b.frames)-1]
	b.stack = b.stack[:f.base]
	f.unreachable = true
}

func (b *valueBuilder) finish(owner Node, f *valueFrame, arm ControlArm) error {
	stackOperands := len(b.stack) - f.base
	values, err := b.popTypes(b.types(f.results))
	if err != nil {
		return err
	}
	if len(b.stack) != f.base {
		return fmt.Errorf("source label %d leaves %d unexpected operands", f.label, len(b.stack)-f.base)
	}
	scope := b.plan.scopes[f.label]
	scope.exits = append(scope.exits, scopeExit{from: append([]ValueID(nil), values...), stackOperands: stackOperands, arm: arm, validationReachable: !f.unreachable})
	b.plan.scopes[f.label] = scope
	b.edge(owner, ControlExit, f, values, ControlResult, MoveValues, arm, 0)
	return nil
}

// defineValidation retains why an otherwise typed definition has no runtime
// producer. The caller is the source rule that synthesizes a polymorphic
// continuation, rather than a later opcode or reachability classifier.
func (b *valueBuilder) defineValidation(owner Node, position int, vt wasm.ValType) ValueID {
	id := b.define(owner, InstructionResult, position, vt)
	if id != 0 {
		b.plan.definitions[id-1].ValidationOnly = true
	}
	return id
}
