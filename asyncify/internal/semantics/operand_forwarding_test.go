package semantics

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestMaterializedOperandRetainsSource(t *testing.T) {
	literal, _, err := ResolveLiteral(wasm.Instruction{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: -7}})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []Operand{StoredOperand(1, wasm.ValI64), LiteralOperand(literal)} {
		source = source.WithBinding(19)
		result, err := source.MaterializedAt(5)
		if err != nil {
			t.Fatal(err)
		}
		token, bound := result.Binding()
		local, stored := result.LocalIndex()
		if !bound || token != 19 || !stored || local != 5 || result.Type() != wasm.ValI64 {
			t.Fatalf("incorrect snapshot: %+v", result)
		}
		if _, remainsLiteral := result.Literal(); remainsLiteral {
			t.Fatal("storage copy still claims literal representation")
		}
	}
	for _, source := range []Operand{{}, ValidationOperand(wasm.ValI64)} {
		if _, err := source.WithBinding(19).MaterializedAt(5); err == nil {
			t.Fatal("non-runtime input was materialized")
		}
	}
}
