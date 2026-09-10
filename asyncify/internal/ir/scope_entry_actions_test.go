package ir

import (
	"testing"
)

func scopeEntryFixture(t *testing.T) (*ValuePlan, *LoweredControl, int) {
	t.Helper()
	values, lowered := lowerValueFixture(t, `(module (import "env" "yield" (func $yield))
 (func (result i32)
 i32.const 10 i32.const 20 block (param i32 i32) (result i32 i32) call $yield end i32.add))`)
	for index, action := range lowered.actions {
		if action.kind == ScopeEntryTransfer {
			return values, lowered, index
		}
	}
	t.Fatal("missing entry transfer")
	return nil, nil, 0
}

func TestScopeEntryTransferOwnsIncomingValuesAndParameterPorts(t *testing.T) {
	values, lowered, index := scopeEntryFixture(t)
	transfer, ok := lowered.actions[index].Transfer()
	if !ok || transfer.Arm() != NoArm || transfer.Mode() != MoveValues || transfer.OperandCount() != 2 {
		t.Fatal("invalid entry transfer")
	}
	scope := values.scopes[transfer.Scope()]
	op := values.operations[scope.owner]
	for i := 0; i < transfer.OperandCount(); i++ {
		operand, _ := transfer.Operand(i)
		local, assigned := lowered.PortStorage(scope.params[i])
		if operand.Value() != op.Inputs[i] || operand.Port() != scope.params[i] || !operand.Present() || !assigned || operand.Local() != local {
			t.Fatal("entry source binding lost")
		}
	}
}

func TestScopeEntryTransferRejectsRetargetingAndCoverageChanges(t *testing.T) {
	for _, mutation := range []string{"value", "port", "cell", "presence", "arm", "kind", "drop", "duplicate", "source-arity", "source-presence"} {
		t.Run(mutation, func(t *testing.T) {
			values, lowered, index := scopeEntryFixture(t)
			transfer := &lowered.actions[index].transfer
			switch mutation {
			case "value":
				transfer.operands[0].value = transfer.operands[1].value
			case "port":
				transfer.operands[0].port = transfer.operands[1].port
			case "cell":
				transfer.operands[0].local = transfer.operands[1].local
			case "presence":
				transfer.operands[0].present = false
			case "arm":
				transfer.arm = ThenArm
			case "kind":
				lowered.actions[index].kind = ScopeResultTransfer
			case "drop":
				lowered.actions = append(lowered.actions[:index], lowered.actions[index+1:]...)
			case "source-arity":
				source := values.operations[transfer.owner]
				source.Inputs = append(source.Inputs, source.Inputs[0])
				values.operations[transfer.owner] = source
			case "source-presence":
				source := values.operations[transfer.owner]
				source.inputStackOperands--
				values.operations[transfer.owner] = source
			case "duplicate":
				lowered.actions = append(lowered.actions, lowered.actions[index])
			}
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("accepted corrupted entry transfer")
			}
		})
	}
}

func TestScopeEntryTransferDistinguishesAsyncCaptureAndPlainIf(t *testing.T) {
	_, lowered := lowerValueFixture(t, `(module (import "env" "yield" (func $yield))
 (func (result i32)
 i32.const 40 i32.const 1 if (param i32) (result i32) i32.const 1 i32.add end
 i32.const 1 if (param i32) (result i32) call $yield i32.const 1 i32.add end))`)
	entries, captures := 0, 0
	for _, action := range lowered.actions {
		if action.kind == ScopeEntryTransfer {
			entries++
		}
		if action.kind == AsyncIfEntryCapture {
			captures++
		}
	}
	if entries != 1 || captures != 1 {
		t.Fatalf("entries=%d captures=%d", entries, captures)
	}
	if _, err := lowered.CopyActions(); err != nil {
		t.Fatal(err)
	}
}
