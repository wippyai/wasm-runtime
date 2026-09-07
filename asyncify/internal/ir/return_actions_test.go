package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func returnFixture(t *testing.T, source string) (*ValuePlan, *LoweredControl, int) {
	t.Helper()
	values, lowered := lowerValueFixture(t, source)
	for index, action := range lowered.actions {
		if action.kind == SourceReturn {
			return values, lowered, index
		}
	}
	t.Fatal("fixture emitted no source return action")
	return nil, nil, 0
}

func TestSourceReturnBindsRootPortsAndSourceInputs(t *testing.T) {
	values, lowered, index := returnFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (result i32 i64)
    call $yield
    i32.const 7 i64.const 9
    return))`)
	action := lowered.actions[index]
	operation, ok := action.Return()
	if !ok || operation.Target() != 0 || operation.Mode() != MoveValues || !operation.ValidationReachable() || operation.OperandCount() != 2 {
		t.Fatal("return did not expose its source operation")
	}
	root, ok := values.Scope(0)
	if !ok || root.Kind() != FunctionScope {
		t.Fatal("return did not bind the function scope")
	}
	source, ok := lowered.SourceOperation(index)
	if !ok || source.InputCount() != operation.OperandCount() {
		t.Fatal("source return lost its source operation")
	}
	for i := 0; i < operation.OperandCount(); i++ {
		operand, ok := operation.Operand(i)
		if !ok || operand.Port() != root.ResultValue(i) || operand.Value() != source.InputValue(i) || !operand.Present() {
			t.Fatalf("return operand %d lost its source result binding", i)
		}
		info, defined := values.ValueInfo(operand.Port())
		if !defined || operand.Type() != info.Type {
			t.Fatalf("return operand %d lost its root result type", i)
		}
	}
	if _, ok := action.Primitive(); ok {
		t.Fatal("source return exposed a primitive instruction")
	}
	if _, ok := action.Domain(); ok {
		t.Fatal("source return claimed an execution domain")
	}
	projection, err := action.CopyInstructions()
	if err != nil || len(projection) != 1 || projection[0].Opcode != wasm.OpReturn {
		t.Fatalf("return projection=%#v err=%v", projection, err)
	}
}

func TestSourceReturnPreservesPresentUnknownAndUnreachableState(t *testing.T) {
	_, lowered, index := returnFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (result i64)
    call $yield
    unreachable select
    return))`)
	operation, _ := lowered.actions[index].Return()
	operand, ok := operation.Operand(0)
	if !ok || !operand.Present() || operand.Value() != 0 || operand.Type() != wasm.ValI64 || operation.ValidationReachable() {
		t.Fatal("unreachable return lost its present polymorphic operand contract")
	}
}

func TestSourceReturnRejectsCorruptedSourceContract(t *testing.T) {
	fixture := `(module
  (import "env" "yield" (func $yield))
  (func (result i32 i32)
    call $yield
    i32.const 7 i32.const 8
    return))`
	for _, mutate := range []struct {
		edit func(*Action)
		name string
	}{
		{name: "same-type-swap", edit: func(action *Action) {
			action.returnOp.operands[0].value, action.returnOp.operands[1].value = action.returnOp.operands[1].value, action.returnOp.operands[0].value
		}},
		{name: "wrong-root-port", edit: func(action *Action) { action.returnOp.operands[0].port = action.returnOp.operands[1].port }},
		{name: "wrong-type", edit: func(action *Action) { action.returnOp.operands[0].valueType = wasm.ValI64 }},
		{name: "wrong-presence", edit: func(action *Action) { action.returnOp.operands[0].present = false }},
		{name: "wrong-reachability", edit: func(action *Action) { action.returnOp.validationReachable = false }},
		{name: "wrong-target", edit: func(action *Action) { action.returnOp.target = 1 }},
		{name: "wrong-mode", edit: func(action *Action) { action.returnOp.mode = CopyValues }},
		{name: "wrong-source", edit: func(action *Action) {
			action.returnOp.owner = &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpReturn}}
		}},
		{name: "generic-source-return", edit: func(action *Action) {
			action.kind = SourceInstruction
			action.instruction = wasm.Instruction{Opcode: wasm.OpReturn}
			action.returnOp = ReturnOperation{}
			action.domain = semantics.GuestExecution
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			_, lowered, index := returnFixture(t, fixture)
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].returnOp = corrupt[index].returnOp.copy()
			mutate.edit(&corrupt[index])
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted corrupted source return")
			}
		})
	}
}

func TestSourceReturnCoverageRejectsMissingDuplicateAndReorderedOccurrences(t *testing.T) {
	fixture := `(module
  (import "env" "yield" (func $yield))
  (func (result i64)
    call $yield i64.const 1 return
    i64.const 2 return))`
	for _, scenario := range []string{"missing", "duplicate", "reordered"} {
		t.Run(scenario, func(t *testing.T) {
			_, lowered := lowerValueFixture(t, fixture)
			var indices []int
			for index, action := range lowered.actions {
				if action.kind == SourceReturn {
					indices = append(indices, index)
				}
			}
			if len(indices) != 2 {
				t.Fatalf("returns=%d, want 2", len(indices))
			}
			actions := append([]Action(nil), lowered.actions...)
			switch scenario {
			case "missing":
				actions = append(actions[:indices[0]], actions[indices[0]+1:]...)
			case "duplicate":
				actions = append(actions, actions[indices[0]])
			case "reordered":
				actions[indices[0]], actions[indices[1]] = actions[indices[1]], actions[indices[0]]
			}
			lowered.actions = actions
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted invalid source return coverage")
			}
		})
	}
}

func TestSourceReturnCopyDoesNotExposeOperandStorage(t *testing.T) {
	_, lowered, index := returnFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (result i32) call $yield i32.const 7 return))`)
	actions, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	actions[index].returnOp.operands[0].value = 99
	operation, _ := actions[index].Return()
	operation.operands[0].value = 88
	again, err := lowered.CopyActions()
	if err != nil || again[index].returnOp.operands[0].value == 99 || again[index].returnOp.operands[0].value == 88 {
		t.Fatal("return copy mutation reached retained action")
	}
}

func TestSourceReturnSupportsVoidFunction(t *testing.T) {
	_, lowered, index := returnFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func call $yield return))`)
	operation, _ := lowered.actions[index].Return()
	if operation.OperandCount() != 0 || !operation.ValidationReachable() || operation.Target() != 0 || operation.Mode() != MoveValues {
		t.Fatal("void source return acquired a fabricated result contract")
	}
}

func TestSourceReturnDoesNotDisruptSuspensionCallBinding(t *testing.T) {
	_, lowered := lowerValueFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    call $yield return
    call $yield unreachable))`)
	if len(lowered.SuspensionSites()) != 2 {
		t.Fatalf("suspension calls after source return were lost: %d", len(lowered.SuspensionSites()))
	}
}

func TestSourceReturnRetainsValidationPrefix(t *testing.T) {
	fixture := `(module (import "env" "yield" (func $yield))
 (func (result i64)
  i32.const 7 i32.const 8
  block
   call $yield i64.const 42 return
  end
  drop drop i64.const 99))`
	values, lowered, index := returnFixture(t, fixture)
	op, _ := lowered.actions[index].Return()
	if op.PrefixCount() != 2 {
		t.Fatalf("prefix count %d", op.PrefixCount())
	}
	first, _ := op.PrefixOperand(0)
	second, _ := op.PrefixOperand(1)
	if first.Value() == 0 || first.Value() == second.Value() || first.Type() != wasm.ValI32 || second.Type() != wasm.ValI32 {
		t.Fatal("lost prefix identities/types")
	}
	expected := values.operations[op.owner].controlPrefix
	if first.Value() != expected[0] || second.Value() != expected[1] {
		t.Fatal("prefix differs from source")
	}
	copied, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	copied[index].returnOp.prefix[0].value = 0
	if lowered.actions[index].returnOp.prefix[0].value != first.Value() {
		t.Fatal("copy aliases prefix")
	}
	for _, scenario := range []string{"swap", "type", "missing", "inactive"} {
		t.Run(scenario, func(t *testing.T) {
			_, lowered, index := returnFixture(t, fixture)
			switch scenario {
			case "swap":
				p := lowered.actions[index].returnOp.prefix
				p[0], p[1] = p[1], p[0]
			case "type":
				lowered.actions[index].returnOp.prefix[0].valueType = wasm.ValI64
			case "missing":
				lowered.actions[index].returnOp.prefix = nil
			case "inactive":
				for i := range lowered.actions {
					if lowered.actions[i].kind == SourceInstruction {
						lowered.actions[i].returnOp.prefix = lowered.actions[index].returnOp.prefix
						break
					}
				}
			}
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("accepted corrupt prefix")
			}
		})
	}
}
