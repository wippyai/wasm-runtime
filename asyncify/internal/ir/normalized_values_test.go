package ir

import (
	"strings"
	"testing"
)

func TestNormalizedValueClosureRejectsElidedOperandAndContinuation(t *testing.T) {
	for _, continuation := range []bool{false, true} {
		values, source := normalizedFixture(t, `(module (import "env" "yield" (func $yield))
   (func block br 0 i32.const 99 drop end i32.const 42 call $yield drop))`)
		var dead ValueID
		var drop *InstrNode
		for _, node := range values.control.instructions {
			op := values.operations[node]
			if !source.Active(node) && len(op.Outputs) == 1 {
				dead = op.Outputs[0]
			}
			if source.Active(node) && len(op.Inputs) == 1 {
				drop = node
			}
		}
		if dead == 0 || drop == nil {
			t.Fatal("missing fixture operands")
		}
		if continuation {
			values.continuations[1] = []ValueID{dead}
		} else {
			op := values.operations[drop]
			op.Inputs = []ValueID{dead}
			values.operations[drop] = op
		}
		if err := source.verifyValueClosure(); err == nil || !strings.Contains(err.Error(), "elided definition") {
			t.Fatalf("closure = %v", err)
		}
	}
}

func TestNormalizedValueClosureRejectsUnknownLiveOperand(t *testing.T) {
	values, source := normalizedFixture(t, `(module (func i32.const 42 drop))`)
	node := values.control.instructions[1]
	op := values.operations[node]
	op.Inputs = []ValueID{0}
	values.operations[node] = op
	if err := source.verifyValueClosure(); err == nil || !strings.Contains(err.Error(), "no concrete definition") {
		t.Fatalf("closure = %v", err)
	}
}

func TestNormalizedValueClosurePreservesImplicitElseInitialization(t *testing.T) {
	_, source := normalizedFixture(t, `(module
  (func (result i32) i32.const 42 i32.const 0
   if (param i32) (result i32) drop unreachable end))`)
	if !source.RootMayFallthrough() {
		t.Fatal("implicit else lost fallthrough")
	}
	if err := source.verifyValueClosure(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizedValueClosureRequiresLoopEntryDespiteBackedge(t *testing.T) {
	values, source := normalizedFixture(t, `(module
  (func i32.const 42 loop (param i32) br 0 end))`)
	if err := source.verifyValueClosure(); err != nil {
		t.Fatal(err)
	}
	// Keep the loop's self-backedge. It supplies membership but cannot initialize
	// the first iteration. This specifically tests initialization, not DAG order.
	var edges []valueEdge
	for _, edge := range values.edges {
		if edge.Kind != ControlEntry {
			edges = append(edges, edge)
		}
	}
	values.edges = edges
	if err := source.verifyValueClosure(); err == nil || !strings.Contains(err.Error(), "no retained initialization") {
		t.Fatalf("closure = %v", err)
	}
}

func TestNormalizedValueClosureRequiresConsumedResultInitialization(t *testing.T) {
	values, source := normalizedFixture(t, `(module (func block (result i32) i32.const 42 end drop))`)
	var retained []valueEdge
	for _, edge := range values.edges {
		if edge.Kind != ControlExit || edge.Target == 0 {
			retained = append(retained, edge)
		}
	}
	values.edges = retained
	if err := source.verifyValueClosure(); err == nil || !strings.Contains(err.Error(), "no retained incoming transfer") {
		t.Fatalf("closure = %v", err)
	}
}
