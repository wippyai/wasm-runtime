package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func stackContractAt(t *testing.T, lowered *LoweredControl, index int) StackContract {
	t.Helper()
	contract, err := lowered.StackContract(index)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func requireStackValues(t *testing.T, label string, count int, value func(int) ValueID, want []ValueID) {
	t.Helper()
	if count != len(want) {
		t.Fatalf("%s count=%d, want %d", label, count, len(want))
	}
	for index, expected := range want {
		if actual := value(index); actual != expected {
			t.Fatalf("%s[%d]=%d, want %d", label, index, actual, expected)
		}
	}
}

func TestStackContractSourceInstructionUsesSourceValuesIncludingZeroArity(t *testing.T) {
	values, lowered := lowerValueFixture(t, `(module
  (func (result i32) nop i32.const 7))`)
	for index, action := range lowered.actions {
		if action.kind != SourceInstruction {
			continue
		}
		contract := stackContractAt(t, lowered, index)
		operation := values.operations[action.origin.source]
		if !contract.Checked() || contract.TruncatesToPrefix() {
			t.Fatalf("source action %d did not retain its checked non-truncating contract", index)
		}
		requireStackValues(t, "inputs", contract.InputCount(), contract.InputValue, operation.Inputs)
		requireStackValues(t, "outputs", contract.OutputCount(), contract.OutputValue, operation.Outputs)
	}
}

func TestStackContractPortReadAndSelectors(t *testing.T) {
	values, lowered := lowerValueFixture(t, `(module (import "env" "yield" (func $yield))
 (func (result i32)
  i32.const 40 i32.const 1 if (param i32) (result i32) i32.const 1 i32.add end
  i32.const 1 if (param i32) (result i32) call $yield i32.const 1 i32.add end))`)

	seenRead, seenStore, seenLoad, seenRoute := false, false, false, false
	for index, action := range lowered.actions {
		contract := stackContractAt(t, lowered, index)
		switch action.kind {
		case SourcePortRead:
			seenRead = true
			if !contract.Checked() || contract.InputCount() != 0 || contract.OutputCount() != 1 || contract.OutputValue(0) != action.portRead.Port() {
				t.Fatal("port read contract lost its source port")
			}
		case SourceSelectorStore:
			seenStore = true
			if !contract.Checked() || contract.InputCount() != 1 || contract.InputValue(0) != action.selector.Value() || contract.OutputCount() != 0 {
				t.Fatal("selector store contract is wrong")
			}
		case SourceSelectorLoad:
			seenLoad = true
			if !contract.Checked() || contract.OutputCount() != 1 || contract.OutputValue(0) != action.selector.Value() || contract.InputCount() != 0 {
				t.Fatal("selector load contract is wrong")
			}
		case SourceSelectorRoute:
			seenRoute = true
			if contract.Checked() {
				t.Fatal("selector routing claimed a source stack contract")
			}
		}
	}
	if !seenRead || !seenStore || !seenLoad || !seenRoute {
		t.Fatalf("reads=%v store=%v load=%v route=%v", seenRead, seenStore, seenLoad, seenRoute)
	}

	// The non-async if remains guest structure and consumes exactly its source
	// selector. The async if is lowered through routing and has no guest if.
	for index, action := range lowered.actions {
		if action.kind != GuestStructureInstruction || action.instruction.Opcode != wasm.OpIf {
			continue
		}
		owner, ok := action.origin.owner.(*IfNode)
		if !ok {
			t.Fatal("guest if has no if owner")
		}
		contract := stackContractAt(t, lowered, index)
		op := values.operations[owner]
		if !contract.Checked() || contract.InputCount() != 1 || contract.InputValue(0) != op.Selector {
			t.Fatal("plain guest if did not consume its source selector")
		}
	}
}

func TestStackContractCaptureAndTransfersFilterAbsentOperands(t *testing.T) {
	_, captureLowered, captureIndex := captureFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func
    i32.const 7 i64.const 9 i32.const 1
    if (param i32 i64)
      drop drop call $yield
    else
      drop drop
    end))`)
	capture := captureLowered.actions[captureIndex].capture
	captureContract := stackContractAt(t, captureLowered, captureIndex)
	wantCapture := make([]ValueID, 0, capture.OperandCount())
	for i := 0; i < capture.OperandCount(); i++ {
		operand, _ := capture.Operand(i)
		if operand.Present() {
			wantCapture = append(wantCapture, operand.Value())
		}
	}
	requireStackValues(t, "capture inputs", captureContract.InputCount(), captureContract.InputValue, wantCapture)

	_, transferLowered, transferIndex := transferFixture(t, `(module
  (import "env" "yield" (func $yield))
  (func block (result i64) call $yield unreachable end drop))`)
	transfer := transferLowered.actions[transferIndex].transfer
	transferContract := stackContractAt(t, transferLowered, transferIndex)
	wantTransfer := make([]ValueID, 0, transfer.OperandCount())
	for i := 0; i < transfer.OperandCount(); i++ {
		operand, _ := transfer.Operand(i)
		if operand.Present() {
			wantTransfer = append(wantTransfer, operand.Value())
		}
	}
	requireStackValues(t, "transfer inputs", transferContract.InputCount(), transferContract.InputValue, wantTransfer)
}

func TestStackContractBranchReturnAndTrap(t *testing.T) {
	_, branchLowered, branchIndex := branchFixture(t, `(module (import "env" "yield" (func $yield))
  (func block (result i32) call $yield i32.const 7 i32.const 1 br_if 0 end drop))`)
	branch := branchLowered.actions[branchIndex].branch
	branchContract := stackContractAt(t, branchLowered, branchIndex)
	if !branchContract.Checked() || branchContract.TruncatesToPrefix() {
		t.Fatal("br_if must retain its fallthrough stack")
	}
	wantBranchInputs := make([]ValueID, 0, branch.InputCount())
	for i := 0; i < branch.InputCount(); i++ {
		operand, _ := branch.Input(i)
		if operand.Present() {
			wantBranchInputs = append(wantBranchInputs, operand.Value())
		}
	}
	wantFallthrough := make([]ValueID, branch.FallthroughCount())
	for i := range wantFallthrough {
		operand, _ := branch.Fallthrough(i)
		wantFallthrough[i] = operand.Value()
	}
	requireStackValues(t, "branch inputs", branchContract.InputCount(), branchContract.InputValue, wantBranchInputs)
	requireStackValues(t, "branch outputs", branchContract.OutputCount(), branchContract.OutputValue, wantFallthrough)

	_, directLowered, directIndex := branchFixture(t, `(module (import "env" "yield" (func $yield))
  (func block (result i32) call $yield i32.const 7 br 0 end drop))`)
	directContract := stackContractAt(t, directLowered, directIndex)
	if !directContract.Checked() || !directContract.TruncatesToPrefix() || directContract.OutputCount() != 0 {
		t.Fatal("br must truncate to its prefix without a fallthrough result")
	}

	_, tableLowered, tableIndex := branchFixture(t, `(module (import "env" "yield" (func $yield))
  (func block (result i32) call $yield i32.const 7 i32.const 0 br_table 0 0 end drop))`)
	tableContract := stackContractAt(t, tableLowered, tableIndex)
	if !tableContract.Checked() || !tableContract.TruncatesToPrefix() || tableContract.OutputCount() != 0 {
		t.Fatal("br_table must truncate to its prefix without a fallthrough result")
	}

	_, returnLowered, returnIndex := returnFixture(t, `(module (import "env" "yield" (func $yield))
	  (func (result i32) i64.const 77 block i32.const 9 return end drop i32.const 0))`)
	returnOp := returnLowered.actions[returnIndex].returnOp
	returnContract := stackContractAt(t, returnLowered, returnIndex)
	if !returnContract.Checked() || !returnContract.TruncatesToPrefix() {
		t.Fatal("return must truncate to its source prefix")
	}
	if returnContract.PrefixCount() != returnOp.PrefixCount() {
		t.Fatal("return prefix arity changed")
	}
	for i := 0; i < returnOp.PrefixCount(); i++ {
		operand, _ := returnOp.PrefixOperand(i)
		if returnContract.PrefixValue(i) != operand.Value() {
			t.Fatal("return prefix identity changed")
		}
	}

	_, trapLowered, trapIndex := trapFixture(t, `(module
	  (func i64.const 77 block i32.const 123 unreachable end drop))`)
	trap := trapLowered.actions[trapIndex].trap
	trapContract := stackContractAt(t, trapLowered, trapIndex)
	if !trapContract.Checked() || !trapContract.TruncatesToPrefix() || trapContract.InputCount() != 0 || trapContract.OutputCount() != 0 {
		t.Fatal("trap did not retain its truncating prefix-only contract")
	}
	if trapContract.PrefixCount() != trap.PrefixCount() {
		t.Fatal("trap prefix arity changed")
	}
}

func TestStackContractRejectsUnboundCarrierAndHandlesBounds(t *testing.T) {
	_, lowered := lowerValueFixture(t, `(module (func nop))`)
	lowered.actions = append(lowered.actions, Action{
		kind:        GuestCarrierInstruction,
		domain:      semantics.GuestExecution,
		instruction: wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		origin:      instructionOrigin{owner: &SeqNode{}},
	})
	if _, err := lowered.StackContract(len(lowered.actions) - 1); err == nil {
		t.Fatal("source-backed raw carrier received a stack contract")
	}
	if _, err := lowered.StackContract(-1); err == nil {
		t.Fatal("negative action index did not fail")
	}
	if _, err := lowered.StackContract(len(lowered.actions)); err == nil {
		t.Fatal("past-end action index did not fail")
	}

	contract := checkedStackContract([]ValueID{1}, []ValueID{2}, []ValueID{3}, false)
	if contract.InputValue(-1) != 0 || contract.InputValue(1) != 0 || contract.OutputValue(-1) != 0 || contract.OutputValue(1) != 0 || contract.PrefixValue(-1) != 0 || contract.PrefixValue(1) != 0 {
		t.Fatal("contract getters panic or expose values outside their bounds")
	}
}

func TestStackContractRejectsGenericEncodingOfSourceControl(t *testing.T) {
	_, lowered, index := returnFixture(t, `(module (func (result i32) i32.const 9 return))`)
	action := lowered.actions[index]
	action.kind = SourceInstruction
	action.instruction = action.returnOp.owner.Instr
	action.returnOp = ReturnOperation{}
	action.domain = semantics.GuestExecution
	lowered.actions[index] = action
	if _, err := lowered.StackContract(index); err == nil {
		t.Fatal("generic source return received an ordinary instruction contract")
	}

	legacy := &LoweredControl{actions: []Action{{
		kind:        GuestCarrierInstruction,
		domain:      semantics.GuestExecution,
		instruction: wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		origin:      instructionOrigin{owner: &SeqNode{}},
	}}}
	contract, err := legacy.StackContract(0)
	if err != nil || contract.Checked() {
		t.Fatalf("legacy carrier contract=%#v err=%v", contract, err)
	}
}
