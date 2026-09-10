package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func trapFixture(t *testing.T, source string) (*ValuePlan, *LoweredControl, int) {
	t.Helper()
	values, lowered := lowerValueFixture(t, source)
	for index, action := range lowered.actions {
		if action.kind == SourceTrap {
			return values, lowered, index
		}
	}
	t.Fatal("fixture emitted no source trap action")
	return nil, nil, 0
}

func TestSourceTrapBindsExactEnclosingFramePrefix(t *testing.T) {
	values, lowered, index := trapFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    i64.const 77
    block (result i32)
      i32.const 123
      unreachable
    end
    drop
    drop
    call $yield))`)
	action := lowered.actions[index]
	operation, ok := action.Trap()
	if !ok || !operation.ValidationReachable() || operation.PrefixCount() != 1 {
		t.Fatal("trap did not expose its source operation")
	}
	source, ok := lowered.SourceOperation(index)
	if !ok || source.InputCount() != 0 || source.OutputCount() != 0 {
		t.Fatal("source trap lost its source operation")
	}
	node := action.origin.source
	planned := values.operations[node]
	prefix, ok := operation.PrefixOperand(0)
	if !ok || prefix.Value() != planned.controlPrefix[0] || prefix.Type() != wasm.ValI64 {
		t.Fatal("trap did not preserve its enclosing-frame prefix")
	}
	if _, ok := action.Primitive(); ok {
		t.Fatal("source trap exposed a primitive instruction")
	}
	if _, ok := action.Domain(); ok {
		t.Fatal("source trap claimed an execution domain")
	}
	projection, err := action.CopyInstructions()
	if err != nil || len(projection) != 1 || projection[0].Opcode != wasm.OpUnreachable {
		t.Fatalf("trap projection=%#v err=%v", projection, err)
	}
}

func TestSourceTrapRejectsCorruptedSourceContract(t *testing.T) {
	fixture := `(module
  (func
    i64.const 77
    block
      unreachable
	    end
	    drop))`
	for _, mutate := range []struct {
		edit func(*Action)
		name string
	}{
		{name: "wrong-prefix-value", edit: func(action *Action) { action.trap.prefix[0].value = 0 }},
		{name: "wrong-prefix-type", edit: func(action *Action) { action.trap.prefix[0].valueType = wasm.ValI32 }},
		{name: "wrong-reachability", edit: func(action *Action) { action.trap.validationReachable = false }},
		{name: "wrong-source", edit: func(action *Action) {
			action.trap.owner = &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpUnreachable}}
		}},
		{name: "inactive-return-payload", edit: func(action *Action) {
			action.returnOp = ReturnOperation{owner: &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpReturn}}}
		}},
		{name: "generic-source-trap", edit: func(action *Action) {
			action.kind = SourceInstruction
			action.instruction = wasm.Instruction{Opcode: wasm.OpUnreachable}
			action.trap = TrapOperation{}
			action.domain = semantics.GuestExecution
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			_, lowered, index := trapFixture(t, fixture)
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].trap = corrupt[index].trap.copy()
			mutate.edit(&corrupt[index])
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("CopyActions accepted corrupted source trap")
			}
		})
	}
}

func TestSourceTrapCoverageRejectsMissingDuplicateAndReorderedOccurrences(t *testing.T) {
	fixture := `(module (func unreachable unreachable))`
	for _, scenario := range []string{"missing", "duplicate", "reordered"} {
		t.Run(scenario, func(t *testing.T) {
			_, lowered := lowerValueFixture(t, fixture)
			var indices []int
			for index, action := range lowered.actions {
				if action.kind == SourceTrap {
					indices = append(indices, index)
				}
			}
			if len(indices) != 2 {
				t.Fatalf("traps=%d, want 2", len(indices))
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
				t.Fatal("CopyActions accepted invalid source trap coverage")
			}
		})
	}
}

func TestSourceTrapCopyDoesNotExposePrefixStorage(t *testing.T) {
	_, lowered, index := trapFixture(t, `(module (func i64.const 1 block unreachable end drop))`)
	actions, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	actions[index].trap.prefix[0].value = 0
	again, err := lowered.CopyActions()
	if err != nil || again[index].trap.prefix[0].value == 0 {
		t.Fatal("trap action copy mutated retained prefix storage")
	}
}
