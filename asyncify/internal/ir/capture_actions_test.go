package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func captureFixture(t *testing.T, source string) (*ValuePlan, *LoweredControl, int) {
	t.Helper()
	values, lowered := lowerValueFixture(t, source)
	for index, action := range lowered.actions {
		if action.kind == AsyncIfEntryCapture {
			return values, lowered, index
		}
	}
	t.Fatal("fixture emitted no async if entry capture")
	return nil, nil, 0
}

func TestAsyncIfEntryCaptureBindsSourceValuesPortsAndSelector(t *testing.T) {
	values, lowered, index := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (result i64)
    i32.const 7 i64.const 9 i32.const 1
    if (param i32 i64) (result i64)
      drop drop call $yield i64.const 11
    else
      drop drop i64.const 12
    end))`)
	capture, ok := lowered.actions[index].Capture()
	if !ok || capture.OperandCount() != 3 {
		t.Fatal("capture did not expose ordered params and selector")
	}
	ifNode := capture.owner
	source := values.operations[ifNode]
	scope := values.scopes[ifNode.label]
	for operandIndex := 0; operandIndex < 2; operandIndex++ {
		operand, ok := capture.Operand(operandIndex)
		if !ok || operand.Role() != CaptureParameter || operand.Value() != source.Inputs[operandIndex] || !operand.Present() {
			t.Fatalf("parameter %d lost source entry binding", operandIndex)
		}
		port, portOK := operand.Port()
		local, localOK := lowered.PortStorage(scope.params[operandIndex])
		if !portOK || !localOK || port != scope.params[operandIndex] || operand.Local() != local {
			t.Fatalf("parameter %d lost source port assignment", operandIndex)
		}
	}
	selector, ok := capture.Operand(2)
	if !ok || selector.Role() != CaptureSelector || selector.Value() != source.Selector || !selector.Present() || selector.Type() != wasm.ValI32 {
		t.Fatal("selector lost source binding")
	}
	if _, portOK := selector.Port(); portOK {
		t.Fatal("selector claimed a parameter port")
	}
	if _, ok := lowered.actions[index].Primitive(); ok {
		t.Fatal("capture exposed primitive bytecode")
	}
	if _, ok := lowered.actions[index].Domain(); ok {
		t.Fatal("capture claimed a single execution domain")
	}
	projection, err := lowered.actions[index].CopyInstructions()
	if err != nil || len(projection) != 18 {
		t.Fatalf("capture projection=%d err=%v, want 18 instructions", len(projection), err)
	}
}

func TestAsyncIfEntryCaptureRejectsSwappedValuesAndWrongStorage(t *testing.T) {
	_, lowered, index := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    i32.const 7 i32.const 9 i32.const 1
    if (param i32 i32)
      drop drop call $yield
    else
      drop drop
    end))`)
	for _, mutate := range []struct {
		edit func(*EntryCapture)
		name string
	}{
		{name: "same-type-swap", edit: func(c *EntryCapture) {
			c.operands[0].value, c.operands[1].value = c.operands[1].value, c.operands[0].value
		}},
		{name: "wrong-port", edit: func(c *EntryCapture) { c.operands[0].port = c.operands[1].port }},
		{name: "wrong-cell", edit: func(c *EntryCapture) { c.operands[0].local++ }},
		{name: "selector-role", edit: func(c *EntryCapture) { c.operands[2].role = CaptureParameter; c.operands[2].port = 1 }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].capture = corrupt[index].capture.copy()
			mutate.edit(&corrupt[index].capture)
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted corrupted entry capture")
			}
		})
		// Rebuild rather than restoring a potentially aliased corruption.
		_, lowered, index = captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    i32.const 7 i32.const 9 i32.const 1
    if (param i32 i32)
      drop drop call $yield
    else
      drop drop
    end))`)
	}
}

func TestAsyncIfEntryCaptureRejectsInactivePrimitivePayload(t *testing.T) {
	_, lowered, index := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func i32.const 1 if call $yield else end))`)
	corrupt := append([]Action(nil), lowered.actions...)
	corrupt[index].instruction = wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{}}
	lowered.actions = corrupt
	if _, err := lowered.CopyActions(); err == nil {
		t.Fatal("CopyActions accepted a primitive payload on capture action")
	}
}

func TestAsyncIfEntryCaptureKeepsAbsentAndPresentUnknownDistinct(t *testing.T) {
	_, absent, absentIndex := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    unreachable i32.const 1
    if (param i32)
      drop call $yield
    else
      drop
    end))`)
	absentCapture, _ := absent.actions[absentIndex].Capture()
	parameter, _ := absentCapture.Operand(0)
	selector, _ := absentCapture.Operand(1)
	if parameter.Present() || parameter.Value() != 0 || !selector.Present() || selector.Value() == 0 {
		t.Fatal("absent parameter was confused with the present selector")
	}
	absentProjection, err := absentCapture.CopyInstructions()
	if err != nil || len(absentProjection) != 8 {
		t.Fatalf("absent capture projection=%d err=%v, want selector guard only", len(absentProjection), err)
	}

	_, unknown, unknownIndex := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    unreachable select
    if
      call $yield
    else
    end))`)
	unknownCapture, _ := unknown.actions[unknownIndex].Capture()
	unknownSelector, _ := unknownCapture.Operand(0)
	if !unknownSelector.Present() || unknownSelector.Value() != 0 || unknownSelector.Type() != wasm.ValI32 {
		t.Fatal("present polymorphic selector was confused with absence")
	}
	unknownProjection, err := unknownCapture.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	sets := 0
	for _, instruction := range unknownProjection {
		if instruction.Opcode == wasm.OpLocalSet {
			sets++
		}
	}
	if sets != 0 {
		t.Fatal("present polymorphic selector fabricated a carrier write")
	}
}

func TestAsyncIfEntryCaptureProjectsAllAbsentInputsToNoSteps(t *testing.T) {
	_, lowered, index := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    unreachable
    if
      call $yield
    else
    end))`)
	capture, _ := lowered.actions[index].Capture()
	selector, _ := capture.Operand(0)
	if selector.Present() || selector.Value() != 0 {
		t.Fatal("all-absent capture acquired a source operand")
	}
	steps, err := capture.CopyInstructions()
	if err != nil || len(steps) != 0 {
		t.Fatalf("all-absent capture steps=%d err=%v, want zero", len(steps), err)
	}
}

func TestEntryCaptureCopyDoesNotExposeOperandStorage(t *testing.T) {
	_, lowered, index := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func i32.const 1 if call $yield else end))`)
	actions, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	actions[index].capture.operands[0].local = 99
	capture, _ := actions[index].Capture()
	capture.operands[0].local = 88
	again, err := lowered.CopyActions()
	if err != nil || again[index].capture.operands[0].local == 99 || again[index].capture.operands[0].local == 88 {
		t.Fatal("capture copy mutation reached retained action")
	}
}
