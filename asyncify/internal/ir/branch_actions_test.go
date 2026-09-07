package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func branchFixture(t *testing.T, source string) (*ValuePlan, *LoweredControl, int) {
	t.Helper()
	values, lowered := lowerValueFixture(t, source)
	for index, action := range lowered.actions {
		if action.kind == SourceBranch {
			return values, lowered, index
		}
	}
	t.Fatal("fixture emitted no source branch action")
	return nil, nil, 0
}

func TestSourceBranchBindsTargetPortsInputsAndDepth(t *testing.T) {
	values, lowered, index := branchFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func block (result i32 i64)
    call $yield i32.const 7 i64.const 9 br 0
  end drop drop))`)
	action := lowered.actions[index]
	branch, ok := action.Branch()
	if !ok || branch.Opcode() != wasm.OpBr || branch.Mode() != MoveValues || !branch.ValidationReachable() || branch.InputCount() != 2 || branch.TargetCount() != 1 || branch.PrefixCount() != 0 {
		t.Fatal("branch did not expose its source operation")
	}
	target, _ := branch.Target(0)
	if target.Ordinal() != 0 || target.PortCount() != 2 {
		t.Fatal("branch did not expose target result ports")
	}
	for i := 0; i < target.PortCount(); i++ {
		input, _ := branch.Input(i)
		port, _ := target.Port(i)
		if !input.Present() || input.Value() != port.Value() || input.Type() != port.Type() {
			t.Fatalf("branch input %d lost its source binding", i)
		}
		local, assigned := lowered.PortStorage(port.Port())
		if !assigned || port.Local() != local {
			t.Fatalf("branch port %d lost its carrier", i)
		}
	}
	if _, ok := action.Primitive(); ok {
		t.Fatal("source branch exposed a primitive")
	}
	if _, ok := action.Domain(); ok {
		t.Fatal("source branch claimed a domain")
	}
	if _, ok := values.Scope(target.Label()); !ok {
		t.Fatal("branch target has no source scope")
	}
}

func TestSourceBranchRejectsTargetAndInputCorruption(t *testing.T) {
	fixture := `(module (import "env" "yield" (func $yield))
  (func block (result i32 i32) call $yield i32.const 7 i32.const 8 br 0 end drop drop))`
	for _, mutate := range []struct {
		edit func(*BranchOperation)
		name string
	}{
		{name: "same-type-input-swap", edit: func(b *BranchOperation) { b.inputs[0].value, b.inputs[1].value = b.inputs[1].value, b.inputs[0].value }},
		{name: "wrong-input-type", edit: func(b *BranchOperation) { b.inputs[0].valueType = wasm.ValI64 }},
		{name: "absent-reachable-input", edit: func(b *BranchOperation) { b.inputs[0].present = false; b.inputs[0].value = 0 }},
		{name: "wrong-target-port", edit: func(b *BranchOperation) { b.targets[0].ports[0].port = b.targets[0].ports[1].port }},
		{name: "wrong-target-cell", edit: func(b *BranchOperation) { b.targets[0].ports[0].local++ }},
		{name: "wrong-depth", edit: func(b *BranchOperation) { b.targets[0].depth++ }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			_, lowered, index := branchFixture(t, fixture)
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].branch = corrupt[index].branch.copy()
			mutate.edit(&corrupt[index].branch)
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted corrupted source branch")
			}
		})
	}
}

func TestSourceBranchRejectsCarrierAllocationCorruption(t *testing.T) {
	selectorFixture := `(module (import "env" "yield" (func $yield))
  (func block (result i32) call $yield i32.const 7 i32.const 1 br_if 0 end drop))`
	tableFixture := `(module (import "env" "yield" (func $yield))
  (func block (result i32 i64)
    call $yield i32.const 7 i64.const 9 i32.const 0 br_table 0 0
  end drop drop))`
	directFixture := `(module (import "env" "yield" (func $yield))
  (func block (result i32) call $yield i32.const 7 br 0 end drop))`
	for _, mutate := range []struct {
		edit    func(*BranchOperation)
		name    string
		fixture string
	}{
		{
			name:    "selector-uses-target-cell",
			fixture: selectorFixture,
			edit: func(b *BranchOperation) {
				b.selectorLocal = b.targets[0].ports[0].local
			},
		},
		{
			name:    "selector-presence",
			fixture: selectorFixture,
			edit:    func(b *BranchOperation) { b.hasSelectorLocal = false },
		},
		{
			name:    "table-scratch-arity",
			fixture: tableFixture,
			edit:    func(b *BranchOperation) { b.branchValues = b.branchValues[:1] },
		},
		{
			name:    "table-scratch-order",
			fixture: tableFixture,
			edit: func(b *BranchOperation) {
				b.branchValues[0], b.branchValues[1] = b.branchValues[1], b.branchValues[0]
			},
		},
		{
			name:    "table-scratch-uses-target-cell",
			fixture: tableFixture,
			edit: func(b *BranchOperation) {
				b.branchValues[0] = b.targets[0].ports[0].local
			},
		},
		{
			name:    "inactive-selector",
			fixture: directFixture,
			edit: func(b *BranchOperation) {
				b.hasSelectorLocal = true
				b.selectorLocal = b.targets[0].ports[0].local
			},
		},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			_, lowered, index := branchFixture(t, mutate.fixture)
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].branch = corrupt[index].branch.copy()
			mutate.edit(&corrupt[index].branch)
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted corrupted source branch carrier allocation")
			}
		})
	}
}

func TestSourceBranchVoidProjectionIsClosedLoopCopyable(t *testing.T) {
	_, lowered, index := branchFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func block call $yield br 0 end))`)
	branch, _ := lowered.actions[index].Branch()
	if !branch.CopyableInClosedRegion() {
		t.Fatal("void branch was not eligible for one-step closed-loop projection")
	}
	projection, err := branch.CopyInstructions()
	if err != nil || len(projection) != 1 || projection[0].Opcode != wasm.OpBr {
		t.Fatalf("void branch projection=%#v err=%v", projection, err)
	}
}
