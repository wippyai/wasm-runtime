package ir

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func valueFixture(t *testing.T, source string) (*ValuePlan, error) {
	t.Helper()
	raw, err := wat.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	functionIndex := uint32(module.NumImportedFuncs())
	signature := module.GetFuncType(functionIndex)
	locals := append([]wasm.ValType(nil), signature.Params...)
	for _, local := range module.Code[0].Locals {
		for range local.Count {
			locals = append(locals, local.ValType)
		}
	}
	calls := semantics.NewCalls(module)
	control, err := Prepare(module.Code[0].Code, module, calls, func(semantics.CallOperation) bool { return true }, signature.Results)
	if err != nil {
		return nil, err
	}
	return PlanValues(control, func(instruction wasm.Instruction) (OperandShape, error) {
		if inputs, outputs, handled := semantics.ScalarSignature(instruction.Opcode); handled {
			return OperandShape{Inputs: inputs, Results: outputs}, nil
		}
		if local, handled, err := semantics.ResolveLocal(instruction, locals); handled {
			if err != nil {
				return OperandShape{}, err
			}
			shape := OperandShape{}
			if local.Effects&semantics.ConsumeOperand != 0 {
				shape.Inputs = []wasm.ValType{local.Type}
			}
			if local.Effects&semantics.ProduceSnapshot != 0 {
				shape.Results = []wasm.ValType{local.Type}
			}
			shape.AliasInput = local.Effects == semantics.ConsumeOperand|semantics.AssignLocal|semantics.ProduceSnapshot
			return shape, nil
		}
		if call, handled, err := calls.Resolve(instruction); handled {
			if err != nil {
				return OperandShape{}, err
			}
			shape := OperandShape{}
			for i := 0; i < call.ParamCount(); i++ {
				shape.Inputs = append(shape.Inputs, call.ParamType(i))
			}
			for i := 0; i < call.ResultCount(); i++ {
				shape.Results = append(shape.Results, call.ResultType(i))
			}
			return shape, nil
		}
		switch instruction.Opcode {
		case wasm.OpI32Const:
			return OperandShape{Results: []wasm.ValType{wasm.ValI32}}, nil
		case wasm.OpI64Const:
			return OperandShape{Results: []wasm.ValType{wasm.ValI64}}, nil
		default:
			return OperandShape{}, fmt.Errorf("fixture needs explicit shape for 0x%x", instruction.Opcode)
		}
	})
}

func TestSourceValuesRetainOperandsAtSuspension(t *testing.T) {
	plan, err := valueFixture(t, `(module (import "env" "yield" (func $yield (param i32) (result i32)))
  (func (result i32) i32.const 7 i32.const 9 call $yield i32.add))`)
	if err != nil {
		t.Fatal(err)
	}
	nodes := plan.control.root.(*SeqNode).Children
	first := plan.operations[nodes[0]].Outputs[0]
	argument := plan.operations[nodes[1]].Outputs[0]
	call := plan.operations[nodes[2]]
	add := plan.operations[nodes[3]]
	retained := plan.continuations[1]
	if len(retained) != 2 || retained[0] != first || retained[1] != argument {
		t.Fatalf("wrong continuation values: %v", retained)
	}
	if len(call.Inputs) != 1 || call.Inputs[0] != argument || len(call.Outputs) != 1 || call.Outputs[0] == argument {
		t.Fatal("call inputs/results lost identity")
	}
	if len(add.Inputs) != 2 || add.Inputs[0] != first || add.Inputs[1] != call.Outputs[0] {
		t.Fatal("following operation lost retained operand")
	}
	for _, id := range add.Inputs {
		if plan.definitions[id-1].Type != wasm.ValI32 {
			t.Fatal("lost value type")
		}
	}
}

func TestSourceValuesLoopPortsAndTeeIdentity(t *testing.T) {
	plan, err := valueFixture(t, `(module (import "env" "yield" (func $yield))
  (func (result i32) (local $counter i32)
   i32.const 2 loop (param i32) (result i32)
    i32.const 1 i32.sub local.tee $counter call $yield
    local.get $counter i32.eqz br_table 0 1 end))`)
	if err != nil {
		t.Fatal(err)
	}
	loop := plan.control.root.(*SeqNode).Children[1].(*BlockNode)
	body := loop.Body.(*SeqNode).Children
	tee := plan.operations[body[2]]
	if len(tee.Inputs) != 1 || len(tee.Outputs) != 1 || tee.Inputs[0] != tee.Outputs[0] {
		t.Fatal("tee must preserve immutable operand identity")
	}
	entries, backedges, exits := 0, 0, 0
	for _, edge := range plan.edges {
		if edge.Target == loop.label && edge.Port == ControlParameter {
			if len(edge.From) != 1 || plan.definitions[edge.From[0]-1].Type != wasm.ValI32 {
				t.Fatal("bad loop argument")
			}
			if edge.Kind == ControlEntry {
				entries++
			}
			if edge.Kind == ControlBranch {
				backedges++
			}
		}
		if edge.Target == 0 && edge.Kind == ControlBranch {
			exits++
		}
	}
	if entries != 1 || backedges != 1 || exits != 1 {
		t.Fatalf("wrong loop/control edges: %d %d %d", entries, backedges, exits)
	}
	foundParam := false
	for _, definition := range plan.definitions {
		if definition.Owner == loop && definition.Port == ControlParameter {
			foundParam = true
			if definition.Position != 0 || definition.Type != wasm.ValI32 {
				t.Fatal("wrong loop parameter definition")
			}
		}
	}
	if !foundParam {
		t.Fatal("loop parameter has no value definition")
	}
}

func TestSourceValueValidationAndPolymorphism(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"valid", `i32.const 7`, true},
		{"unknown-under-unreachable", `unreachable i32.add`, true},
		{"typed-tee-after-unreachable", `(local i32) unreachable local.tee 0 i64.eqz`, false},
		{"typed-branch-fallthrough-after-unreachable", `block (result i32) unreachable br_if 0 i64.eqz end`, false},
		{"typed-tee-valid-after-unreachable", `(local i32) unreachable local.tee 0 i32.eqz`, true},
		{"typed-branch-valid-after-unreachable", `block (result i32) unreachable br_if 0 i32.eqz end`, true},
		{"explicit-wrong-type-after-unreachable", `unreachable i64.const 7 i32.eqz`, false},
		{"extra-results", `i32.const 7 i32.const 9`, false},
		{"missing-operand", `i32.const 7 i32.add`, false},
		{"isolated-block-stack", `i32.const 7 block i32.const 9 i32.add end`, false},
		{"block-params", `i32.const 7 block (param i32) (result i32) i32.const 9 i32.add end`, true},
		{"if-identity-else", `i32.const 7 i32.const 0 if (param i32) (result i32) i32.const 1 i32.add end`, true},
		{"branch-types", `block (result i32) block (result i64) i64.const 7 i32.const 0 br_table 0 1 end drop i32.const 9 end`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `(module (func (result i32) ` + tc.body + `))`
			raw, err := wat.Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			runtime := wazero.NewRuntime(ctx)
			defer runtime.Close(ctx)
			if _, validationErr := runtime.CompileModule(ctx, raw); (validationErr == nil) != tc.valid {
				t.Fatalf("independent validator disagrees with expectation valid=%v: %v", tc.valid, validationErr)
			}
			plan, err := valueFixture(t, source)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if !tc.valid && plan != nil {
				t.Fatal("published partial value plan")
			}
		})
	}
}

func TestSourceValuesRejectMalformedResolverShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape OperandShape
	}{
		{"negative-count", OperandShape{OpaqueInputs: true, PopCount: -1}},
		{"ambiguous-inputs", OperandShape{OpaqueInputs: true, PopCount: 1, Inputs: []wasm.ValType{wasm.ValI32}}},
		{"identity-arity", OperandShape{Inputs: []wasm.ValType{wasm.ValI32}, AliasInput: true}},
		{"identity-type-change", OperandShape{Inputs: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64}, AliasInput: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			constant := &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 7}}}
			operation := &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpI32Eqz}}
			control := &Analysis{root: &SeqNode{Children: []Node{constant, operation}}}
			plan, err := PlanValues(control, func(instruction wasm.Instruction) (OperandShape, error) {
				if instruction.Opcode == wasm.OpI32Const {
					return OperandShape{Results: []wasm.ValType{wasm.ValI32}}, nil
				}
				return tc.shape, nil
			})
			if err == nil || plan != nil {
				t.Fatalf("published malformed shape: plan=%v err=%v", plan, err)
			}
		})
	}
}

func TestSourceValuesOpaqueControlAndResolverOwnership(t *testing.T) {
	// A slice-bearing immediate must not allow a resolver to change the owned
	// source tree. Use an opaque synthetic operation to isolate the contract.
	instruction := &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.MiscImm{SubOpcode: wasm.MiscMemoryInit, Operands: []uint32{7, 0}}}}
	control := &Analysis{root: &SeqNode{Children: []Node{instruction}}}
	plan, err := PlanValues(control, func(copy wasm.Instruction) (OperandShape, error) {
		copy.Imm.(wasm.MiscImm).Operands[0] = 9
		return OperandShape{OpaqueControl: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if instruction.Instr.Imm.(wasm.MiscImm).Operands[0] != 7 {
		t.Fatal("resolver mutated owned source instruction")
	}
	if !plan.opaqueControl || !plan.operations[instruction].OpaqueControl {
		t.Fatal("unrepresented control transfers were lost")
	}
	allocations := 0
	lowered, err := Linearize(plan, &LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 {
		allocations++
		return uint32(allocations - 1)
	}})
	if lowered != nil || err == nil || !strings.Contains(err.Error(), "unsupported unmodeled control transfer") {
		t.Fatalf("production lowering accepted opaque control: lowered=%v err=%v", lowered, err)
	}
	if allocations != 0 {
		t.Fatalf("opaque control allocated %d carriers before rejection", allocations)
	}
}
