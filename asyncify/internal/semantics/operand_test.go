package semantics

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestOperandZeroValueIsAbsentRatherThanLocalOrLiteralZero(t *testing.T) {
	var absent Operand
	if absent.Type() != 0 {
		t.Fatalf("absent operand type = %v, want zero", absent.Type())
	}
	if index, ok := absent.LocalIndex(); ok || index != 0 {
		t.Fatalf("absent operand reported local index (%d, %t)", index, ok)
	}
	if literal, ok := absent.Literal(); ok || literal.Valid() {
		t.Fatalf("absent operand reported literal (%v, %t)", literal, ok)
	}

	storedZero := StoredOperand(0, wasm.ValI32)
	if storedZero.Type() != wasm.ValI32 {
		t.Fatalf("stored local zero type = %v, want i32", storedZero.Type())
	}
	if index, ok := storedZero.LocalIndex(); !ok || index != 0 {
		t.Fatalf("stored local zero index = (%d, %t), want (0, true)", index, ok)
	}
	if _, ok := storedZero.Literal(); ok {
		t.Fatal("stored local zero was classified as a literal")
	}

	literal, handled, err := ResolveLiteral(wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}})
	if err != nil || !handled {
		t.Fatalf("resolve literal zero = (%v, %t)", err, handled)
	}
	literalZero := LiteralOperand(literal)
	if literalZero.Type() != wasm.ValI32 {
		t.Fatalf("literal zero type = %v, want i32", literalZero.Type())
	}
	if _, ok := literalZero.LocalIndex(); ok {
		t.Fatal("literal zero was classified as a stored local")
	}
	got, ok := literalZero.Literal()
	if !ok || !got.Valid() || got.Bits() != literal.Bits() {
		t.Fatalf("literal zero was not preserved: (%v, %t)", got, ok)
	}
}

func TestLiteralOperandRejectsAbsentLiteral(t *testing.T) {
	if operand := LiteralOperand(Literal{}); operand.Type() != 0 {
		t.Fatalf("absent literal produced operand type %v", operand.Type())
	} else if _, ok := operand.LocalIndex(); ok {
		t.Fatal("absent literal became a local operand")
	} else if _, ok := operand.Literal(); ok {
		t.Fatal("absent literal became a literal operand")
	}
}
