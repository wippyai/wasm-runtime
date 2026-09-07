package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func transferFixture(t *testing.T, source string) (*ValuePlan, *LoweredControl, int) {
	t.Helper()
	values, lowered := lowerValueFixture(t, source)
	for index, action := range lowered.actions {
		if action.kind == ScopeResultTransfer {
			return values, lowered, index
		}
	}
	t.Fatal("fixture emitted no scope result transfer")
	return nil, nil, 0
}

func TestScopeResultTransferBindsSourceExitPortsAndStorage(t *testing.T) {
	values, lowered, index := transferFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (result i32)
    block (result i32 i32)
      i32.const 7 i32.const 8 call $yield
    end
    drop))`)
	transfer, ok := lowered.actions[index].Transfer()
	if !ok || transfer.Mode() != MoveValues || transfer.Arm() != NoArm || transfer.OperandCount() != 2 {
		t.Fatal("transfer did not expose the source block exit")
	}
	scope, ok := values.Scope(transfer.Scope())
	if !ok || scope.Kind() != BlockScope {
		t.Fatal("transfer did not name its source scope")
	}
	exit, ok := scope.Exit(0)
	if !ok {
		t.Fatal("scope has no exit")
	}
	for i := 0; i < transfer.OperandCount(); i++ {
		operand, ok := transfer.Operand(i)
		if !ok || operand.Port() != scope.ResultValue(i) || operand.Value() != exit.Value(i) || operand.Present() != (i >= exit.ValueCount()-exit.StackOperandCount()) {
			t.Fatalf("transfer operand %d lost its source exit fact", i)
		}
		local, assigned := lowered.PortStorage(operand.Port())
		if !assigned || operand.Local() != local {
			t.Fatalf("transfer operand %d lost its assigned port storage", i)
		}
	}
}

func TestScopeResultTransferRejectsSwappedValuesAndWrongStorage(t *testing.T) {
	fixture := `(module
  (import "env" "yield" (func $yield))
  (func (result i32)
    block (result i32 i32)
      i32.const 7 i32.const 8 call $yield
    end
    drop))`
	for _, mutate := range []struct {
		edit func(*ScopeTransfer)
		name string
	}{
		{name: "same-type-swap", edit: func(t *ScopeTransfer) {
			t.operands[0].value, t.operands[1].value = t.operands[1].value, t.operands[0].value
		}},
		{name: "wrong-port", edit: func(t *ScopeTransfer) { t.operands[0].port = t.operands[1].port }},
		{name: "wrong-cell", edit: func(t *ScopeTransfer) { t.operands[0].local++ }},
		{name: "wrong-type", edit: func(t *ScopeTransfer) { t.operands[0].valueType = wasm.ValI64 }},
		{name: "wrong-scope", edit: func(t *ScopeTransfer) { t.scope = 0 }},
		{name: "wrong-arm", edit: func(t *ScopeTransfer) { t.arm = ThenArm }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			_, lowered, index := transferFixture(t, fixture)
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].transfer = corrupt[index].transfer.copy()
			mutate.edit(&corrupt[index].transfer)
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted corrupted scope transfer")
			}
		})
	}
}

func TestScopeResultTransferKeepsAbsentAndPresentUnknownDistinct(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		present    bool
	}{
		{"absent", "unreachable", false},
		{"present-unknown", "unreachable select", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, lowered, index := transferFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func block (result i64) call $yield `+tc.body+` end drop))`)
			transfer, _ := lowered.actions[index].Transfer()
			operand, ok := transfer.Operand(0)
			if !ok || operand.Present() != tc.present || operand.Value() != 0 {
				t.Fatal("transfer confused absence with a present polymorphic value")
			}
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].transfer = corrupt[index].transfer.copy()
			corrupt[index].transfer.operands[0].present = !tc.present
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted changed transfer presence")
			}
		})
	}
}

func TestScopeResultTransferCopyDoesNotExposeOperandStorage(t *testing.T) {
	_, lowered, index := transferFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (result i32) block (result i32) call $yield i32.const 7 end))`)
	actions, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	actions[index].transfer.operands[0].local = 99
	transfer, _ := actions[index].Transfer()
	transfer.operands[0].local = 88
	again, err := lowered.CopyActions()
	if err != nil || again[index].transfer.operands[0].local == 99 || again[index].transfer.operands[0].local == 88 {
		t.Fatal("transfer copy mutation reached retained action")
	}
}

func TestScopeResultTransferUsesCanonicalMaterializedRootOwner(t *testing.T) {
	values, lowered, index := transferFixture(t, `(module (func (result i32) i32.const 7 br 0 nop))`)
	transfer, _ := lowered.actions[index].Transfer()
	if !lowered.rootMaterialized || transfer.Scope() != 0 || transfer.Arm() != NoArm || transfer.owner != values.control.root || lowered.actions[index].origin.owner != values.control.root {
		t.Fatal("root wrapper transfer did not retain the canonical source function owner")
	}
	operand, _ := transfer.Operand(0)
	if operand.Present() || operand.Value() != 0 {
		t.Fatal("root branch fallthrough transfer fabricated an operand")
	}
	lowered.rootMaterialized = false
	if _, err := lowered.CopyActions(); err == nil {
		t.Fatal("CopyActions accepted corrupted root materialization coverage")
	}
}
