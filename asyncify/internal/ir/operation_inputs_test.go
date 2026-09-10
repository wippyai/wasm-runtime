package ir

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func requireStockWazeroSource(t *testing.T, source string) {
	t.Helper()
	raw, err := wat.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)
	module, err := runtime.CompileModule(ctx, raw)
	if err != nil {
		t.Fatalf("stock Wazero rejected source fixture: %v", err)
	}
	defer module.Close(ctx)
}

func requireInputFacts(t *testing.T, operation SourceOperation, present int, values ...ValueID) {
	t.Helper()
	if operation.InputCount() != len(values) || operation.InputStackOperandCount() != present {
		t.Fatalf("inputs=%d present=%d, want inputs=%d present=%d", operation.InputCount(), operation.InputStackOperandCount(), len(values), present)
	}
	for index, want := range values {
		if operation.InputValue(index) != want {
			t.Fatalf("input %d = %d, want %d", index, operation.InputValue(index), want)
		}
		if got := operation.InputPresent(index); got != (index >= len(values)-present) {
			t.Fatalf("input %d present=%t, want %t", index, got, index >= len(values)-present)
		}
	}
}

func TestOperationInputsDistinguishAbsentAndPresentUnknown(t *testing.T) {
	const source = `(module (func unreachable select select drop))`
	requireStockWazeroSource(t, source)
	_, lowered := lowerValueFixture(t, source)
	seen := 0
	for index, action := range lowered.actions {
		origin := action.origin
		if origin.source == nil || origin.source.Instr.Opcode != wasm.OpSelect {
			continue
		}
		operation, ok := lowered.SourceOperation(index)
		if !ok {
			t.Fatal("select lost source operation")
		}
		switch seen {
		case 0:
			requireInputFacts(t, operation, 0, 0, 0, 0)
			if operation.OutputCount() != 1 || operation.OutputValue(0) != 0 {
				t.Fatal("unknown select did not produce a present unknown output")
			}
		case 1:
			requireInputFacts(t, operation, 1, 0, 0, 0)
			if operation.InputPresent(0) || operation.InputPresent(1) || !operation.InputPresent(2) || operation.InputValue(2) != 0 {
				t.Fatal("present unknown was confused with absent polymorphic pops")
			}
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("select operations=%d, want 2", seen)
	}
}

func TestOperationInputsRetainPartialBranchArgumentsAndSelector(t *testing.T) {
	const source = `(module (func
  block (result i32 i32)
    unreachable
    i32.const 7
    i32.const 1
    br_if 0
  end
	  drop drop))`
	requireStockWazeroSource(t, source)
	plan, lowered := lowerValueFixture(t, source)
	branch := sourceOperationForOpcode(t, lowered, wasm.OpBrIf)
	requireInputFacts(t, branch, 2, 0, branch.InputValue(1), branch.InputValue(2))
	if branch.InputValue(1) == 0 || branch.InputValue(2) == 0 {
		t.Fatal("partial branch fixture did not retain concrete argument and selector")
	}
	selector, hasSelector := branch.Selector()
	if !hasSelector || selector != branch.InputValue(2) || !branch.InputPresent(2) {
		t.Fatal("branch selector presence disagrees with source inputs")
	}
	scope := scopeOfKind(t, plan, BlockScope)
	if scope.EntryValueCount() != 0 || scope.EntryStackOperandCount() != 0 {
		t.Fatal("result-only block unexpectedly reports entry arguments")
	}
}

func TestControlScopeEntryInputsRetainPresenceSeparately(t *testing.T) {
	const source = `(module (func
  unreachable
  i32.const 1
  if (param i32) (result i32)
    i32.const 7
	    i32.add
  else
    i32.const 9
	    i32.add
  end
	  drop))`
	requireStockWazeroSource(t, source)
	plan, err := valueFixture(t, source)
	if err != nil {
		t.Fatal(err)
	}
	scope := scopeOfKind(t, plan, IfScope)
	if scope.EntryValueCount() != 1 || scope.EntryStackOperandCount() != 0 || scope.EntryValue(0) != 0 || scope.EntryValuePresent(0) {
		t.Fatal("if parameter entry did not preserve its absent source input")
	}
	if present, hasSelector := scope.SelectorPresence(); !hasSelector || !present {
		t.Fatal("if scope did not expose its present selector")
	}
	op := plan.operations[plan.scopes[scope.Label()].owner]
	if len(op.Inputs) != 2 || op.inputStackOperands != 1 || op.Inputs[0] != 0 || op.Inputs[1] == 0 {
		t.Fatal("if operation did not retain partial inputs before its pops")
	}
}

func TestOperationInputPresenceUsesNestedFreshValidationFrame(t *testing.T) {
	const source = `(module (func
  unreachable
  block (param i32) (result i32)
    i32.const 1
    i32.add
  end
	  drop))`
	requireStockWazeroSource(t, source)
	plan, lowered := lowerValueFixture(t, source)
	scope := scopeOfKind(t, plan, BlockScope)
	if scope.EntryStackOperandCount() != 0 || scope.EntryValue(0) != 0 || scope.EntryValuePresent(0) {
		t.Fatal("outer polymorphism did not leave an absent nested block argument")
	}
	add := sourceOperationForOpcode(t, lowered, wasm.OpI32Add)
	requireInputFacts(t, add, 2, add.InputValue(0), add.InputValue(1))
	if add.InputValue(0) == 0 || add.InputValue(1) == 0 {
		t.Fatal("nested frame did not create present internal operands")
	}
}

func TestTypedAliasOutputRemainsPresentAfterPolymorphicPop(t *testing.T) {
	const source = `(module (func (result i32) (local i32)
  unreachable
  local.tee 0
	  i32.eqz))`
	requireStockWazeroSource(t, source)
	_, lowered := lowerValueFixture(t, source)
	tee := sourceOperationForOpcode(t, lowered, wasm.OpLocalTee)
	requireInputFacts(t, tee, 0, 0)
	if tee.OutputCount() != 1 || tee.OutputValue(0) == 0 {
		t.Fatal("typed alias did not create a present typed output from an absent pop")
	}
	eqz := sourceOperationForOpcode(t, lowered, wasm.OpI32Eqz)
	requireInputFacts(t, eqz, 1, tee.OutputValue(0))
}

func TestOperationInputPresenceVerifierRejectsBoundViolations(t *testing.T) {
	for _, scenario := range []string{"negative-width", "excess-width", "reachable-absence", "value-in-absent-prefix"} {
		t.Run(scenario, func(t *testing.T) {
			const partialSource = `(module (func unreachable i32.const 7 i32.add drop))`
			requireStockWazeroSource(t, partialSource)
			plan, err := valueFixture(t, partialSource)
			if err != nil {
				t.Fatal(err)
			}
			var add *InstrNode
			for _, node := range plan.control.instructions {
				if node.Instr.Opcode == wasm.OpI32Add {
					add = node
					break
				}
			}
			if add == nil {
				t.Fatal("fixture did not contain add")
			}
			op := plan.operations[add]
			switch scenario {
			case "negative-width":
				op.inputStackOperands = -1
			case "excess-width":
				op.inputStackOperands = len(op.Inputs) + 1
			case "value-in-absent-prefix":
				if op.inputStackOperands != 1 || op.Inputs[0] != 0 || op.Inputs[1] == 0 {
					t.Fatal("fixture did not produce one absent and one present input")
				}
				op.Inputs[0] = op.Inputs[1]
			case "reachable-absence":
				const reachableSource = `(module (func i32.const 7 drop))`
				requireStockWazeroSource(t, reachableSource)
				plan, err = valueFixture(t, reachableSource)
				if err != nil {
					t.Fatal(err)
				}
				for _, node := range plan.control.instructions {
					if node.Instr.Opcode == wasm.OpDrop {
						add = node
						break
					}
				}
				op = plan.operations[add]
				op.inputStackOperands = 0
			}
			plan.operations[add] = op
			if err := plan.verifyTransfers(); err == nil {
				t.Fatal("accepted invalid bounded source input facts")
			}
		})
	}
}

func TestInputPresenceViewsRejectInvalidIndexesAndNoSelector(t *testing.T) {
	const source = `(module (func
  i32.const 1
  drop
  block
	  end))`
	requireStockWazeroSource(t, source)
	plan, lowered := lowerValueFixture(t, source)
	drop := sourceOperationForOpcode(t, lowered, wasm.OpDrop)
	if drop.InputPresent(-1) || drop.InputPresent(drop.InputCount()) {
		t.Fatal("source operation accepted invalid input presence index")
	}
	if present, hasSelector := drop.SelectorPresence(); present || hasSelector {
		t.Fatal("non-selector instruction reported selector presence")
	}
	scope := scopeOfKind(t, plan, BlockScope)
	if scope.EntryValuePresent(-1) || scope.EntryValuePresent(scope.EntryValueCount()) {
		t.Fatal("scope accepted invalid entry presence index")
	}
	if present, hasSelector := scope.SelectorPresence(); present || hasSelector {
		t.Fatal("plain block reported selector presence")
	}
}
