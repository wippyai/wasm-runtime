package engine

import (
	"fmt"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func addStackContract(t *testing.T) ir.StackContract {
	t.Helper()
	return outputStackContract(t, `(module (func (result i32) i32.const 11 i32.const 22 i32.add))`, 2)
}

func outputStackContract(t *testing.T, source string, action int) ir.StackContract {
	t.Helper()
	raw, err := wat.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	control, err := ir.Prepare(module.Code[0].Code, module, semantics.NewCalls(module), func(semantics.CallOperation) bool { return false }, []wasm.ValType{wasm.ValI32})
	if err != nil {
		t.Fatal(err)
	}
	values, err := ir.PlanValues(control, func(instruction wasm.Instruction) (ir.OperandShape, error) {
		switch instruction.Opcode {
		case wasm.OpI32Const:
			return ir.OperandShape{Results: []wasm.ValType{wasm.ValI32}}, nil
		case wasm.OpLocalTee:
			return ir.OperandShape{Inputs: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}, AliasInput: true}, nil
		case wasm.OpI32Add:
			return ir.OperandShape{Inputs: []wasm.ValType{wasm.ValI32, wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}}, nil
		default:
			return ir.OperandShape{}, fmt.Errorf("unexpected fixture opcode %x", instruction.Opcode)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := ir.Linearize(values, &ir.LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { t.Fatal("unexpected control storage"); return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lowered.StackContract(action)
	if err != nil {
		t.Fatal(err)
	}
	if contract.OutputCount() != 1 {
		t.Fatal("expected single output contract")
	}
	return contract
}

func TestStackBindingRejectsSameTypeSubstitution(t *testing.T) {
	contract := addStackContract(t)
	for _, mutation := range []string{"none", "swap", "duplicate", "unbound", "missing"} {
		t.Run(mutation, func(t *testing.T) {
			stack := handler.NewStack(0)
			first := semantics.StoredOperand(0, wasm.ValI32).WithBinding(uint64(contract.InputValue(0)))
			second := semantics.StoredOperand(1, wasm.ValI32).WithBinding(uint64(contract.InputValue(1)))
			switch mutation {
			case "swap":
				first, second = second, first
			case "duplicate":
				second = first
			case "unbound":
				second = semantics.StoredOperand(1, wasm.ValI32)
			}
			stack.PushOperand(first)
			if mutation != "missing" {
				stack.PushOperand(second)
			}
			var step stackBindingStep
			err := step.begin(contract, 2, stack.Len(), stack.At)
			if (err == nil) != (mutation == "none") {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}

func TestStackBindingChecksEffectAndPreservesSnapshotIdentity(t *testing.T) {
	contract := addStackContract(t)
	stack := handler.NewStack(0)
	for i := 0; i < contract.InputCount(); i++ {
		stack.PushOperand(semantics.StoredOperand(uint32(i), wasm.ValI32).WithBinding(uint64(contract.InputValue(i))))
	}
	snapshot := stack.Snapshot()
	var step stackBindingStep
	if err := step.begin(contract, 2, stack.Len(), stack.At); err != nil {
		t.Fatal(err)
	}
	if err := step.finish(stack.Len(), stack.At, stack.BindAt); err == nil {
		t.Fatal("accepted missing arithmetic stack effect")
	}
	stack.Pop()
	stack.Pop()
	stack.Push(2, wasm.ValI32)
	if err := step.finish(stack.Len(), stack.At, stack.BindAt); err != nil {
		t.Fatal(err)
	}
	if err := matchStackValue(contract.OutputValue(0), 0, stack.At); err != nil {
		t.Fatal(err)
	}
	stack.Restore(snapshot)
	if err := step.begin(contract, 2, stack.Len(), stack.At); err != nil {
		t.Fatalf("snapshot lost identity: %v", err)
	}
	if err := stack.BindAt(0, uint64(contract.InputValue(1))); err != nil {
		t.Fatal(err)
	}
	stack.Restore(snapshot)
	if err := step.begin(contract, 2, stack.Len(), stack.At); err != nil {
		t.Fatalf("binding mutated saved snapshot: %v", err)
	}
}

func TestStackOutputRejectsRelabeling(t *testing.T) {
	for _, fixture := range []struct {
		name, source string
		action       int
		forwarded    bool
	}{
		{"fresh", `(module (func (result i32) i32.const 11 i32.const 22 i32.add))`, 2, false},
		{"forwarded", `(module (func (result i32) (local i32) i32.const 11 local.tee 0))`, 1, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			contract := outputStackContract(t, fixture.source, fixture.action)
			output, ok := contract.Output(0)
			if !ok {
				t.Fatal("missing output provenance")
			}
			for _, scenario := range []string{"materialized", "same-token", "unrelated-token", "wrong-type", "absent", "validation"} {
				t.Run(scenario, func(t *testing.T) {
					entry := semantics.StoredOperand(4, wasm.ValI32)
					switch scenario {
					case "same-token":
						entry = entry.WithBinding(uint64(contract.OutputValue(0)))
					case "unrelated-token":
						entry = entry.WithBinding(uint64(contract.OutputValue(0)) + 100)
					case "wrong-type":
						entry = semantics.StoredOperand(4, wasm.ValI64)
					case "absent":
						entry = semantics.Operand{}
					case "validation":
						entry = semantics.ValidationOperand(wasm.ValI32)
					}
					valid := (scenario == "materialized" && !fixture.forwarded) || (scenario == "same-token" && fixture.forwarded)
					if err := verifyStackOutput(output, contract.OutputValue(0), entry); (err == nil) != valid {
						t.Fatalf("valid=%t err=%v", valid, err)
					}
				})
			}
		})
	}
}

func TestStackOutputValidationHasNoRuntimeStorage(t *testing.T) {
	contract := outputStackContract(t, `(module (func (result i32) (local i32) unreachable local.tee 0))`, 1)
	output, ok := contract.Output(0)
	if !ok {
		t.Fatal("missing validation provenance")
	}
	if err := verifyStackOutput(output, contract.OutputValue(0), semantics.ValidationOperand(wasm.ValI32)); err != nil {
		t.Fatal(err)
	}
	if err := verifyStackOutput(output, contract.OutputValue(0), semantics.StoredOperand(4, wasm.ValI32)); err == nil {
		t.Fatal("validation continuation accepted runtime storage")
	}
}

func TestStackOutputFailureDoesNotAssignIdentity(t *testing.T) {
	contract := addStackContract(t)
	stack := handler.NewStack(0)
	for i := 0; i < contract.InputCount(); i++ {
		stack.PushOperand(semantics.StoredOperand(uint32(i), wasm.ValI32).WithBinding(uint64(contract.InputValue(i))))
	}
	var step stackBindingStep
	if err := step.begin(contract, 2, stack.Len(), stack.At); err != nil {
		t.Fatal(err)
	}
	stack.Pop()
	stack.Pop()
	stack.PushOperand(semantics.StoredOperand(4, wasm.ValI32).WithBinding(uint64(contract.InputValue(0))))
	assigned := false
	err := step.finish(stack.Len(), stack.At, func(int, uint64) error { assigned = true; return nil })
	if err == nil || assigned {
		t.Fatalf("invalid result was relabeled: err=%v assigned=%t", err, assigned)
	}
}
