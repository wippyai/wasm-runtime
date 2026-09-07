package semantics

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

type operandKind uint8

const (
	absentOperand operandKind = iota
	storedOperand
	literalOperand
	validationOperand
)

// Operand is a typed snapshot, exact literal, or explicit validation-only value.
// Its discriminant and payload are private. No local index doubles as a tag.
// The zero value is invalid, distinct from stored local zero and literal zero.
type Operand struct {
	binding   uint64
	local     uint32
	literal   Literal
	valueType wasm.ValType
	kind      operandKind
	bound     bool
}

func StoredOperand(local uint32, valueType wasm.ValType) Operand {
	return Operand{local: local, valueType: valueType, kind: storedOperand}
}
func LiteralOperand(literal Literal) Operand {
	if !literal.Valid() {
		return Operand{}
	}
	return Operand{literal: literal, valueType: literal.Type(), kind: literalOperand}
}

// ValidationOperand represents a declared type pushed by polymorphic Wasm
// validation. It has no runtime value or physical storage. Only source-proved
// unreachable operations may create it; attempting to execute a read traps.
func ValidationOperand(valueType wasm.ValType) Operand {
	switch valueType {
	case wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64, wasm.ValV128, wasm.ValFuncRef, wasm.ValExtern:
		return Operand{valueType: valueType, kind: validationOperand}
	default:
		return Operand{}
	}
}
func (o Operand) ValidationOnly() bool { return o.kind == validationOperand }

func (o Operand) Type() wasm.ValType         { return o.valueType }
func (o Operand) LocalIndex() (uint32, bool) { return o.local, o.kind == storedOperand }
func (o Operand) Literal() (Literal, bool)   { return o.literal, o.kind == literalOperand }

// WithBinding attaches an opaque source-definition token without changing the
// storage snapshot or literal. Only the source-aware action dispatcher assigns
// tokens; storage indices and equal literal bits do not imply identity.
func (o Operand) WithBinding(token uint64) Operand { o.binding = token; o.bound = true; return o }
func (o Operand) Binding() (uint64, bool)          { return o.binding, o.bound }

// MaterializedAt describes a copy of this runtime operand into new storage.
// The operation performing the copy owns the bytecode; source identity and type
// come exclusively from the consumed operand, never from the expected output.
func (o Operand) MaterializedAt(local uint32) (Operand, error) {
	if o.kind != storedOperand && o.kind != literalOperand {
		return Operand{}, fmt.Errorf("asyncify: cannot materialize absent or validation-only operand")
	}
	result := StoredOperand(local, o.valueType)
	result.binding, result.bound = o.binding, o.bound
	return result, nil
}
