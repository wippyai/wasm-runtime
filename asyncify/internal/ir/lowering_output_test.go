package ir

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func lowerValueFixture(t *testing.T, text string) (*ValuePlan, *LoweredControl) {
	t.Helper()
	values, err := valueFixture(t, text)
	if err != nil {
		t.Fatal(err)
	}
	next := uint32(20)
	lowered, err := Linearize(values, &LinearizeConfig{StateGlobal: 2, StateRewinding: 2, AllocLocal: func(wasm.ValType) uint32 { local := next; next++; return local }})
	if err != nil {
		t.Fatal(err)
	}
	return values, lowered
}

func TestLoweredOperationsRetainSourceValues(t *testing.T) {
	values, lowered := lowerValueFixture(t, `(module
  (import "env" "yield" (func $yield (param i32)))
  (func (param i32) (result i32)
   i32.const 7 local.tee 0 local.get 0
   if (param i32) (result i32)
    local.tee 0 call $yield local.get 0
   else
   end))`)
	sources, generated, tees := 0, 0, 0
	for index, action := range lowered.actions {
		origin := action.origin
		operation, ok := lowered.SourceOperation(index)
		if origin.source == nil {
			if ok {
				t.Fatal("generated instruction impersonated source operation")
			}
			generated++
			continue
		}
		if !ok {
			t.Fatal("source instruction lost value contract")
		}
		sources++
		expected := values.operations[origin.source]
		if operation.InputCount() != len(expected.Inputs) || operation.OutputCount() != len(expected.Outputs) {
			t.Fatal("source arity changed")
		}
		for i, id := range expected.Inputs {
			if operation.InputValue(i) != id {
				t.Fatal("source input identity changed")
			}
		}
		for i, id := range expected.Outputs {
			if operation.OutputValue(i) != id {
				t.Fatal("source output identity changed")
			}
		}
		if origin.source.Instr.Opcode == wasm.OpLocalTee {
			tees++
			if operation.InputValue(0) != operation.OutputValue(0) {
				t.Fatal("tee lost source alias")
			}
		}
	}
	if sources != len(values.control.instructions) || generated == 0 || tees != 2 {
		t.Fatalf("incomplete fixture sources=%d generated=%d tees=%d", sources, generated, tees)
	}
	if _, ok := lowered.SourceOperation(-1); ok {
		t.Fatal("negative source position accepted")
	}
	if _, ok := lowered.SourceOperation(len(lowered.actions)); ok {
		t.Fatal("out-of-range source position accepted")
	}
}

func TestLoweringOriginsRejectSameTypedDivergence(t *testing.T) {
	fixture := `(module (func (param i32) (result i32) local.get 0 drop i32.const 7 local.set 0 local.get 0))`
	for _, scenario := range []string{"missing-origin", "missing-occurrence", "duplicate", "reordered-identical-reads", "changed-immediate", "foreign-owner", "foreign-analysis"} {
		t.Run(scenario, func(t *testing.T) {
			values, lowered := lowerValueFixture(t, fixture)
			switch scenario {
			case "missing-origin":
				lowered.actions[len(lowered.actions)-1].origin = instructionOrigin{}
			case "missing-occurrence":
				lowered.actions = lowered.actions[1:]
			case "duplicate":
				lowered.actions[len(lowered.actions)-1].origin = lowered.actions[0].origin
			case "reordered-identical-reads":
				lowered.actions[0].origin, lowered.actions[len(lowered.actions)-1].origin = lowered.actions[len(lowered.actions)-1].origin, lowered.actions[0].origin
			case "changed-immediate":
				lowered.actions[2].instruction.Imm = wasm.I32Imm{Value: 8}
			case "foreign-owner":
				lowered.actions[0].origin.owner = &SeqNode{}
			case "foreign-analysis":
				values, _ = lowerValueFixture(t, fixture)
			}
			if err := values.control.verifyOrigins(lowered); err == nil {
				t.Fatal("accepted inconsistent ownership")
			}
		})
	}
}

func TestLoweredCopiesDoNotExposeVerifiedStorage(t *testing.T) {
	_, lowered := lowerValueFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func (param i32) (result i32)
   block (result i32)
    call $yield i32.const 7 local.get 0 br_table 0 0
   end))`)
	expected, err := lowered.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	expectedBytes := wasm.EncodeInstructions(expected)
	copied, err := lowered.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	branches := 0
	for index, instruction := range copied {
		if table, ok := instruction.Imm.(wasm.BrTableImm); ok {
			table.Labels[0] = 99
			branches++
		}
		copied[index].Opcode = wasm.OpNop
	}
	again, err := lowered.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	if branches == 0 || !bytes.Equal(expectedBytes, wasm.EncodeInstructions(again)) {
		t.Fatal("output mutation reached verified instruction storage")
	}
	sites := lowered.SuspensionSites()
	if len(sites) != 1 || len(sites[0].ControlLocals) == 0 {
		t.Fatal("fixture has no saved carrier")
	}
	original := sites[0].ControlLocals[0]
	sites[0].ControlLocals[0] = 99
	sites[0].ActionIndex = 99
	repeated := lowered.SuspensionSites()
	if repeated[0].ControlLocals[0] != original || repeated[0].ActionIndex == 99 {
		t.Fatal("site mutation reached retained contract")
	}
}

func TestActionsOwnExplicitExecutionAndRejectInvalidContracts(t *testing.T) {
	_, lowered := lowerValueFixture(t, `(module
	  (import "env" "yield" (func $yield))
	  (func (result i32) block i32.const 0 drop end i32.const 1 if (result i32) call $yield i32.const 7 else i32.const 8 end))`)
	actions, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[ActionKind]bool{}
	for _, action := range actions {
		seen[action.Kind()] = true
		if action.Kind() == AsyncIfEntryCapture {
			if _, ok := action.Domain(); ok {
				t.Fatal("capture claimed an execution domain")
			}
			if _, ok := action.Primitive(); ok {
				t.Fatal("capture exposed a primitive instruction")
			}
			if _, ok := action.Capture(); !ok {
				t.Fatal("capture payload was not exposed")
			}
		} else if action.Kind() == ScopeResultTransfer {
			if _, ok := action.Domain(); ok {
				t.Fatal("transfer claimed an execution domain")
			}
			if _, ok := action.Primitive(); ok {
				t.Fatal("transfer exposed a primitive instruction")
			}
			if _, ok := action.Transfer(); !ok {
				t.Fatal("transfer payload was not exposed")
			}
		} else if action.Kind() == RoutingInstruction || action.Kind() == SourceSelectorRoute {
			domain, ok := action.Domain()
			instruction, primitive := action.Primitive()
			if !ok || domain != semantics.RewindRouting || !primitive || !instruction.Synthetic {
				t.Fatal("routing action lost explicit routing ownership")
			}
		} else {
			domain, ok := action.Domain()
			instruction, primitive := action.Primitive()
			if !ok || domain != semantics.GuestExecution || !primitive || instruction.Synthetic {
				t.Fatal("guest action has invalid execution ownership")
			}
		}
	}
	if !seen[SourceInstruction] || !seen[SourcePortRead] || !seen[GuestStructureInstruction] || !seen[RoutingInstruction] || !seen[AsyncIfEntryCapture] || !seen[ScopeResultTransfer] {
		t.Fatalf("incomplete action coverage: %#v", seen)
	}
	actions[0].instruction.Opcode = wasm.OpNop
	again, err := lowered.CopyActions()
	if err != nil || again[0].instruction.Opcode == wasm.OpNop {
		t.Fatal("action copy mutated retained action storage")
	}
	for _, replacement := range []struct {
		from ActionKind
		to   ActionKind
	}{
		{SourcePortRead, GuestCarrierInstruction},
		{GuestStructureInstruction, GuestCarrierInstruction},
	} {
		for index := range lowered.actions {
			if lowered.actions[index].kind != replacement.from {
				continue
			}
			corrupt := append([]Action(nil), lowered.actions...)
			corrupt[index].kind = replacement.to
			lowered.actions = corrupt
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatalf("CopyActions accepted %v as %v", replacement.from, replacement.to)
			}
			lowered.actions = again
			break
		}
	}
	for _, invalid := range []Action{
		{},
		{kind: SourceInstruction, domain: semantics.GuestExecution},
		{kind: RoutingInstruction, domain: semantics.GuestExecution, instruction: wasm.Instruction{Synthetic: true}, origin: instructionOrigin{owner: &SeqNode{}}},
	} {
		lowered.actions = []Action{invalid}
		if _, err := lowered.CopyActions(); err == nil {
			t.Fatal("CopyActions accepted invalid action")
		}
	}
}

func TestLoweringActionConstructionRejectsMarkerMismatch(t *testing.T) {
	output := loweringOutput{owner: &SeqNode{}}
	output.routing(wasm.Instruction{Opcode: wasm.OpI32Eq})
	if output.err != nil || len(output.actions) != 1 || !output.actions[0].instruction.Synthetic {
		t.Fatal("routing marker was not derived from action kind")
	}
	output = loweringOutput{owner: &SeqNode{}}
	output.guestCarrier(wasm.Instruction{Opcode: wasm.OpLocalGet, Synthetic: true})
	if output.err != nil || len(output.actions) != 1 || output.actions[0].instruction.Synthetic {
		t.Fatal("guest marker was not derived from action kind")
	}
	output = loweringOutput{owner: &SeqNode{}}
	output.guestCarrier(wasm.Instruction{Opcode: wasm.OpNop})
	if output.err == nil || len(output.actions) != 0 {
		t.Fatal("guest carrier construction accepted a non-carrier opcode")
	}
	output = loweringOutput{}
	output.append(GuestStructureInstruction, wasm.Instruction{Opcode: wasm.OpBlock}, instructionOrigin{})
	if output.err == nil || len(output.actions) != 0 {
		t.Fatal("lowering construction accepted invalid provenance")
	}
}

func TestActionsRejectMismatchedGeneratedPayloads(t *testing.T) {
	for _, tc := range []struct {
		name        string
		instruction wasm.Instruction
		kind        ActionKind
	}{
		{"carrier-wrong-immediate", wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.I32Imm{Value: 1}}, GuestCarrierInstruction},
		{"routing-if-with-result", wasm.Instruction{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}, Synthetic: true}, RoutingInstruction},
		{"routing-predicate-with-immediate", wasm.Instruction{Opcode: wasm.OpI32Eq, Imm: wasm.I32Imm{}, Synthetic: true}, RoutingInstruction},
		{"structure-missing-type", wasm.Instruction{Opcode: wasm.OpBlock}, GuestStructureInstruction},
	} {
		t.Run(tc.name, func(t *testing.T) {
			domain, _ := expectedActionDomain(tc.kind)
			lowered := &LoweredControl{actions: []Action{{kind: tc.kind, domain: domain, instruction: tc.instruction, origin: instructionOrigin{owner: &SeqNode{}}}}}
			if _, err := lowered.CopyActions(); err == nil {
				t.Fatal("published malformed generated payload")
			}
		})
	}
}
