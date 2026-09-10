package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestContinuationOwnsSourceOperandContract(t *testing.T) {
	values, err := valueFixture(t, `(module (import "env" "yield" (func $yield (param i32)))
  (func (result i64) i64.const 7 i32.const 9 call $yield))`)
	if err != nil {
		t.Fatal(err)
	}
	next := uint32(0)
	lowered, err := Linearize(values, &LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { local := next; next++; return local }})
	if err != nil {
		t.Fatal(err)
	}
	source := lowered.suspensions[0].Operands
	if source.OperandCount() != 2 || source.OperandType(0) != wasm.ValI64 || source.OperandType(1) != wasm.ValI32 {
		t.Fatal("lost retained tail or call argument types")
	}
	first, second := source.OperandValue(0), source.OperandValue(1)
	if first == 0 || second == 0 || first == second {
		t.Fatal("lost source identities")
	}
	values.continuations[1][0] = 0
	values.definitions[first-1].Type = wasm.ValF64
	if source.OperandValue(0) != first || source.OperandType(0) != wasm.ValI64 {
		t.Fatal("exported continuation aliases mutable plan storage")
	}
}

func TestContinuationRejectsInconsistentBindings(t *testing.T) {
	for _, name := range []string{"missing", "unknown", "repeated", "undefined-value"} {
		t.Run(name, func(t *testing.T) {
			values, err := valueFixture(t, `(module (import "env" "yield" (func $yield)) (func i32.const 7 call $yield drop call $yield))`)
			if err != nil {
				t.Fatal(err)
			}
			lowered := &LoweredControl{suspensions: []SuspensionSite{{SourceCallID: 1}, {SourceCallID: 2}}}
			switch name {
			case "missing":
				lowered.suspensions = lowered.suspensions[:1]
			case "unknown":
				lowered.suspensions[0].SourceCallID = 99
			case "repeated":
				lowered.suspensions[1].SourceCallID = 1
			case "undefined-value":
				values.continuations[1][0] = 999
			}
			if err := values.bindContinuations(lowered); err == nil {
				t.Fatal("accepted inconsistent continuation contract")
			}
		})
	}
}

func TestContinuationLiteralProvenanceIsExact(t *testing.T) {
	values, err := valueFixture(t, `(module (import "env" "yield" (func $yield))
  (func (result i32 i32 i32) (local $x i32)
   i32.const 7 local.tee $x local.get $x i32.const 1 i32.const 2 i32.add call $yield))`)
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := Linearize(values, &LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { t.Fatal("fixture needs no carriers"); return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	continuation := lowered.suspensions[0].Operands
	literal, ok := continuation.OperandLiteral(0)
	if !ok || literal.Type() != wasm.ValI32 || literal.Bits()[0] != 7 {
		t.Fatal("tee lost exact literal identity")
	}
	for _, index := range []int{1, 2} {
		if _, ok := continuation.OperandLiteral(index); ok {
			t.Fatal("inferred literal from mutable cell or arithmetic")
		}
	}
	delete(values.literals, continuation.OperandValue(0))
	if retained, ok := continuation.OperandLiteral(0); !ok || retained != literal {
		t.Fatal("bound literal aliases source fact map")
	}
}

func TestSourceLiteralRejectsWrongResolverType(t *testing.T) {
	constant := &InstrNode{Instr: wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 7}}}
	source := &Analysis{root: &SeqNode{Children: []Node{constant}}, functionResults: []wasm.ValType{wasm.ValI64}}
	plan, err := PlanValues(source, func(wasm.Instruction) (OperandShape, error) {
		return OperandShape{Results: []wasm.ValType{wasm.ValI64}}, nil
	})
	if err == nil || plan != nil {
		t.Fatal("resolver changed a literal's type")
	}
}
