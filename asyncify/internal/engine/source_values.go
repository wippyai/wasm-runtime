package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// sourceOperandShape connects the source graph to the existing instruction
// semantics. Families with exact input signatures certify types; legacy
// registry effects explicitly certify counts/results only. Those operations
// remain opaque to transformations requiring complete operand/effect semantics.
func (ft *FunctionTransformer) sourceOperandShape(instr wasm.Instruction, locals []wasm.ValType) (ir.OperandShape, error) {
	if literal, handled, err := semantics.ResolveLiteral(instr); handled {
		if err != nil {
			return ir.OperandShape{}, err
		}
		return ir.OperandShape{Results: []wasm.ValType{literal.Type()}}, nil
	}
	if inputs, outputs, ok := semantics.ScalarSignature(instr.Opcode); ok {
		return ir.OperandShape{Inputs: inputs, Results: outputs}, nil
	}
	if op, handled, err := semantics.ResolveLocal(instr, locals); handled {
		if err != nil {
			return ir.OperandShape{}, err
		}
		shape := ir.OperandShape{}
		if op.Effects&semantics.ConsumeOperand != 0 {
			shape.Inputs = []wasm.ValType{op.Type}
		}
		if op.Effects&semantics.ProduceSnapshot != 0 {
			shape.Results = []wasm.ValType{op.Type}
		}
		shape.AliasInput = op.Effects&semantics.ConsumeOperand != 0 && op.Effects&semantics.ProduceSnapshot != 0
		return shape, nil
	}
	if op, handled, err := semantics.ResolveGlobal(instr, ft.module); handled {
		if err != nil {
			return ir.OperandShape{}, err
		}
		if op.Write {
			return ir.OperandShape{Inputs: []wasm.ValType{op.Type}}, nil
		}
		return ir.OperandShape{Results: []wasm.ValType{op.Type}}, nil
	}
	if call, handled, err := ft.calls.Resolve(instr); handled {
		if err != nil {
			return ir.OperandShape{}, err
		}
		shape := ir.OperandShape{}
		for i := 0; i < call.ParamCount(); i++ {
			shape.Inputs = append(shape.Inputs, call.ParamType(i))
		}
		if call.HasTargetOperand() {
			targetType := wasm.ValI32
			if call.Kind == semantics.ReferenceCall {
				targetType = wasm.ValFuncRef
			}
			shape.Inputs = append(shape.Inputs, targetType)
		}
		for i := 0; i < call.ResultCount(); i++ {
			shape.Results = append(shape.Results, call.ResultType(i))
		}
		return shape, nil
	}
	switch instr.Opcode {
	case wasm.OpRefNull:
		imm, ok := instr.Imm.(wasm.RefNullImm)
		if !ok {
			return ir.OperandShape{}, fmt.Errorf("invalid ref.null immediate")
		}
		vt := wasm.ValRefNull
		if imm.HeapType < 0 {
			signature, err := semantics.ResolveBlockType(int32(imm.HeapType), nil)
			if err != nil || len(signature.Results) != 1 {
				return ir.OperandShape{}, fmt.Errorf("unresolved reference heap type %d", imm.HeapType)
			}
			vt = signature.Results[0]
		}
		return ir.OperandShape{Results: []wasm.ValType{vt}}, nil
	case wasm.OpRefAsNonNull:
		return ir.OperandShape{OpaqueInputs: true, PopCount: 1, Results: []wasm.ValType{0}, AliasInput: true}, nil
	case wasm.OpBrOnNull:
		return ir.OperandShape{OpaqueInputs: true, OpaqueControl: true, PopCount: 1, Results: []wasm.ValType{0}, AliasInput: true}, nil
	case wasm.OpBrOnNonNull:
		return ir.OperandShape{OpaqueInputs: true, OpaqueControl: true, PopCount: 1}, nil
	}
	if effect := GetStackEffectFromRegistry(ft.registry, instr.Opcode, instr, ft.module); effect != nil {
		return ir.OperandShape{PopCount: effect.Pops, Results: effect.Pushes, OpaqueInputs: effect.Pops > 0}, nil
	}
	return ir.OperandShape{}, fmt.Errorf("asyncify: no source operand shape for opcode 0x%x", instr.Opcode)
}
