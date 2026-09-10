package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestVerifyActionRejectsOwnerlessInactivePayloadFields(t *testing.T) {
	node := &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpNop}}
	base := Action{
		origin:      instructionOrigin{owner: node, source: node},
		instruction: node.Instr,
		kind:        SourceInstruction,
		domain:      semantics.GuestExecution,
	}
	for _, test := range []struct {
		edit func(*Action)
		name string
	}{
		{func(action *Action) { action.portRead.local = 1 }, "port-read-local"},
		{func(action *Action) { action.selector.local = 1 }, "selector-local"},
		{func(action *Action) { action.capture.stateGlobal = 1 }, "capture-state"},
		{func(action *Action) { action.transfer.scope = 1 }, "transfer-scope"},
		{func(action *Action) { action.returnOp.target = 1 }, "return-target"},
		{func(action *Action) { action.branch.opcode = wasm.OpBr }, "branch-opcode"},
		{func(action *Action) { action.trap.validationReachable = true }, "trap-reachability"},
	} {
		t.Run(test.name, func(t *testing.T) {
			action := base
			test.edit(&action)
			if err := verifyAction(action); err == nil {
				t.Fatal("accepted an inactive payload field without its owner")
			}
		})
	}
}

func TestVerifyActionRejectsPrimitivePayloadOnCapture(t *testing.T) {
	capture := EntryCapture{
		owner: &IfNode{},
		operands: []EntryCaptureOperand{{
			role:      CaptureSelector,
			valueType: wasm.ValI32,
		}},
	}
	action := Action{
		origin:      instructionOrigin{owner: capture.owner},
		instruction: wasm.Instruction{Opcode: wasm.OpNop},
		capture:     capture,
		kind:        AsyncIfEntryCapture,
	}
	if err := verifyAction(action); err == nil {
		t.Fatal("capture accepted an inactive primitive payload")
	}
}
