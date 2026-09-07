package engine

import (
	"fmt"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestContinuationStorageRejectsDivergence(t *testing.T) {
	raw, err := wat.Compile(`(module (import "env" "yield" (func $yield (param i32))) (func (result i64) i64.const 7 i32.const 9 call $yield))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	control, err := ir.Prepare(module.Code[0].Code, module, semantics.NewCalls(module), func(semantics.CallOperation) bool { return true }, []wasm.ValType{wasm.ValI64})
	if err != nil {
		t.Fatal(err)
	}
	values, err := ir.PlanValues(control, func(instruction wasm.Instruction) (ir.OperandShape, error) {
		switch instruction.Opcode {
		case wasm.OpI32Const:
			return ir.OperandShape{Results: []wasm.ValType{wasm.ValI32}}, nil
		case wasm.OpI64Const:
			return ir.OperandShape{Results: []wasm.ValType{wasm.ValI64}}, nil
		case wasm.OpCall:
			return ir.OperandShape{Inputs: []wasm.ValType{wasm.ValI32}}, nil
		default:
			return ir.OperandShape{}, fmt.Errorf("unexpected fixture opcode")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := ir.Linearize(values, &ir.LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { t.Fatal("fixture needs no control locals"); return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	source := lowered.SuspensionSites()[0].Operands
	for _, name := range []string{"valid", "valid-literal", "wrong-literal", "absent", "missing", "extra", "wrong-type", "routing-storage", "missing-source", "unbound", "wrong-identity"} {
		t.Run(name, func(t *testing.T) {
			allocator := NewTempAllocator(0)
			tail := allocator.Alloc(wasm.ValI64)
			argument := allocator.Alloc(wasm.ValI32)
			stack := []stackEntry{semantics.StoredOperand(tail, wasm.ValI64), semantics.StoredOperand(argument, wasm.ValI32)}
			contract := source
			switch name {
			case "valid-literal", "wrong-literal":
				value := int64(7)
				if name == "wrong-literal" {
					value = 8
				}
				literal, _, err := semantics.ResolveLiteral(wasm.Instruction{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: value}})
				if err != nil {
					t.Fatal(err)
				}
				stack[0] = semantics.LiteralOperand(literal)
			case "absent":
				stack[0] = semantics.Operand{}

			case "missing":
				stack = stack[:1]
			case "extra":
				stack = append(stack, stack[1])
			case "wrong-type":
				stack[0] = semantics.StoredOperand(tail, wasm.ValI32)
			case "routing-storage":
				allocator.ObserveAction(wasm.Instruction{Synthetic: true}, semantics.RewindRouting)
				stack[0] = semantics.StoredOperand(allocator.Alloc(wasm.ValI64), wasm.ValI64)
			case "missing-source":
				contract = nil
			}
			for i := range stack {
				if i < source.OperandCount() {
					stack[i] = stack[i].WithBinding(uint64(source.OperandValue(i)))
				}
			}
			if name == "unbound" {
				stack[0] = semantics.StoredOperand(tail, wasm.ValI64)
			}
			if name == "wrong-identity" {
				stack[0] = stack[0].WithBinding(uint64(source.OperandValue(1)))
			}
			err := verifyContinuationOperands(contract, len(stack), func(index int) (stackEntry, bool) { return stack[index], true }, allocator)
			if (err == nil) != (name == "valid" || name == "valid-literal") {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
