package ir

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func sourceTestModule() *wasm.Module {
	return &wasm.Module{Types: []wasm.FuncType{{}}, Funcs: []uint32{0, 0}, Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}}}
}

func sourceTestLower(t *testing.T, analysis *Analysis) *LoweredControl {
	t.Helper()
	next := uint32(10)
	lowered, err := linearizeControl(analysis, &LinearizeConfig{StateGlobal: 2, StateRewinding: 2, AllocLocal: func(wasm.ValType) uint32 { index := next; next++; return index }})
	if err != nil {
		t.Fatal(err)
	}
	return lowered
}

func TestSourceAnalysisOwnsInputsAndUsesPolicyOnce(t *testing.T) {
	module := sourceTestModule()
	enabled := map[uint32]bool{0: true}
	policyCalls := 0
	instructions := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpEnd},
	}
	raw := wasm.EncodeInstructions(instructions)
	analysis, err := Prepare(raw, module, semantics.NewCalls(module), func(call semantics.CallOperation) bool {
		policyCalls++
		return call.Kind == semantics.DirectCall && enabled[call.TargetIndex]
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if policyCalls != 2 || analysis.SuspensionCount() != 1 || analysis.HasAsyncInBranch() {
		t.Fatalf("wrong source decisions: calls=%d suspensions=%d branch=%v", policyCalls, analysis.SuspensionCount(), analysis.HasAsyncInBranch())
	}
	first := sourceTestLower(t, analysis)
	if len(first.suspensions) != 1 || first.suspensions[0].SourceCallID != 2 {
		t.Fatalf("wrong source call map: %+v", first.suspensions)
	}
	for _, action := range first.actions {
		if action.kind == RoutingInstruction {
			t.Fatal("ignored indirect branch acquired rewind routing")
		}
	}
	clear(raw)
	enabled[0] = false
	module.Types[0].Results = []wasm.ValType{wasm.ValI64}
	second := sourceTestLower(t, analysis)
	firstInstructions, err := first.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	secondInstructions, err := second.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wasm.EncodeInstructions(firstInstructions), wasm.EncodeInstructions(secondInstructions)) || policyCalls != 2 {
		t.Fatal("lowering re-read mutable input or policy")
	}
}

func TestSourceCallPlanRejectsDivergence(t *testing.T) {
	module := sourceTestModule()
	instructions := []wasm.Instruction{{Opcode: wasm.OpCall, Imm: wasm.CallImm{}}, {Opcode: wasm.OpCall, Imm: wasm.CallImm{}}, {Opcode: wasm.OpEnd}}
	analysis, err := Prepare(wasm.EncodeInstructions(instructions), module, semantics.NewCalls(module), func(semantics.CallOperation) bool { return true }, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing", "extra", "generated-call", "retargeted", "reordered-same-target", "repeated-id", "missing-id", "wrong-immediate", "synthetic"} {
		t.Run(scenario, func(t *testing.T) {
			lowered := sourceTestLower(t, analysis)
			output := loweringOutput{actions: append([]Action(nil), lowered.actions...)}
			switch scenario {
			case "missing":
				output.actions = output.actions[1:]
			case "extra":
				output.actions = append(output.actions, output.actions[0])
			case "generated-call":
				action := output.actions[0]
				action.origin = instructionOrigin{owner: analysis.root}
				action.kind = GuestCarrierInstruction
				action.domain = semantics.GuestExecution
				output.actions = append(output.actions, action)
			case "retargeted":
				output.actions[0].instruction.Imm = wasm.CallImm{FuncIdx: 1}
			case "reordered-same-target":
				output.actions[0].origin, output.actions[1].origin = output.actions[1].origin, output.actions[0].origin
			case "repeated-id":
				output.actions[1].origin = output.actions[0].origin
			case "missing-id":
				output.actions = output.actions[:1]
			case "wrong-immediate":
				output.actions[0].instruction.Imm = wasm.BrTableImm{Labels: []uint32{0}}
			case "synthetic":
				output.actions[0].instruction.Synthetic = true
			}
			if result, err := analysis.bindLoweredCalls(&output, nil, nil, nil); err == nil || result != nil {
				t.Fatal("accepted inconsistent source call lowering")
			}
		})
	}
}

func TestSourceLabelBindingsSurviveOutputMutation(t *testing.T) {
	instructions := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0, 1}, Default: 2}},
		{Opcode: wasm.OpEnd}, {Opcode: wasm.OpEnd}, {Opcode: wasm.OpEnd},
	}
	analysis, err := Prepare(wasm.EncodeInstructions(instructions), nil, nil, func(semantics.CallOperation) bool { return false }, nil)
	if err != nil {
		t.Fatal(err)
	}
	block := analysis.root.(*SeqNode).Children[0].(*BlockNode)
	loop := block.Body.(*SeqNode).Children[0].(*BlockNode)
	branch := loop.Body.(*SeqNode).Children[1].(*InstrNode)
	if len(branch.branchTargets) != 3 || branch.branchTargets[0] != loop.label || branch.branchTargets[1] != block.label || branch.branchTargets[2] != 0 {
		t.Fatalf("wrong source labels: %v", branch.branchTargets)
	}
	first := sourceTestLower(t, analysis)
	firstInstructions, err := first.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	expected := wasm.EncodeInstructions(firstInstructions)
	for _, action := range first.actions {
		instruction := action.instruction
		if imm, ok := instruction.Imm.(wasm.BrTableImm); ok {
			imm.Labels[0] = 999
		}
	}
	second := sourceTestLower(t, analysis)
	secondInstructions, err := second.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, wasm.EncodeInstructions(secondInstructions)) {
		t.Fatal("lowered output mutated owned source tree")
	}
	branch.branchTargets[0] = 999 // Simulate an inconsistent internal rewrite.
	next := uint32(0)
	result, err := linearizeControl(analysis, &LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { next++; return next }})
	if err == nil || result != nil {
		t.Fatal("accepted missing source label binding")
	}
}

func TestLoweringRejectsIncompleteBranchBindings(t *testing.T) {
	analysis, err := Prepare(wasm.EncodeInstructions([]wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Default: 0}},
		{Opcode: wasm.OpEnd},
	}), nil, nil, func(semantics.CallOperation) bool { return false }, nil)
	if err != nil {
		t.Fatal(err)
	}
	branch := analysis.root.(*SeqNode).Children[1].(*InstrNode)
	branch.branchTargets = nil
	output, err := linearizeControl(analysis, &LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { return 0 }})
	if err == nil || output != nil {
		t.Fatal("accepted incomplete branch binding")
	}
}
