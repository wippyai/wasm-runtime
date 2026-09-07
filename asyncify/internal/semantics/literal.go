package semantics

import (
	encodingbinary "encoding/binary"
	"fmt"
	"math"

	"github.com/wippyai/wasm-runtime/wasm"
)

// Literal is a typed, bit-exact immutable value, independent of any storage
// location. It describes only Wasm literal instructions, not folded expressions.
// Zero Literal is absent. Bits above the type's width are always zero.
type Literal struct {
	bits      [16]byte
	valueType wasm.ValType
}

func (l Literal) Valid() bool        { return l.valueType != 0 }
func (l Literal) Type() wasm.ValType { return l.valueType }
func (l Literal) Bits() [16]byte     { return l.bits }

// ResolveLiteral owns all input bits. Unsupported instructions are unhandled;
// malformed literal encodings are handled errors, never guessed zero constants.
func ResolveLiteral(instruction wasm.Instruction) (Literal, bool, error) {
	var l Literal
	switch instruction.Opcode {
	case wasm.OpI32Const:
		immediate, ok := instruction.Imm.(wasm.I32Imm)
		if !ok {
			return l, true, fmt.Errorf("invalid i32 literal immediate")
		}
		l.valueType = wasm.ValI32
		encodingbinary.LittleEndian.PutUint32(l.bits[:], uint32(immediate.Value))
	case wasm.OpI64Const:
		immediate, ok := instruction.Imm.(wasm.I64Imm)
		if !ok {
			return l, true, fmt.Errorf("invalid i64 literal immediate")
		}
		l.valueType = wasm.ValI64
		encodingbinary.LittleEndian.PutUint64(l.bits[:], uint64(immediate.Value))
	case wasm.OpF32Const:
		immediate, ok := instruction.Imm.(wasm.F32Imm)
		if !ok {
			return l, true, fmt.Errorf("invalid f32 literal immediate")
		}
		l.valueType = wasm.ValF32
		encodingbinary.LittleEndian.PutUint32(l.bits[:], math.Float32bits(immediate.Value))
	case wasm.OpF64Const:
		immediate, ok := instruction.Imm.(wasm.F64Imm)
		if !ok {
			return l, true, fmt.Errorf("invalid f64 literal immediate")
		}
		l.valueType = wasm.ValF64
		encodingbinary.LittleEndian.PutUint64(l.bits[:], math.Float64bits(immediate.Value))
	case wasm.OpPrefixSIMD:
		immediate, ok := instruction.Imm.(wasm.SIMDImm)
		if !ok {
			return l, true, fmt.Errorf("invalid SIMD immediate")
		}
		if immediate.SubOpcode != wasm.SimdV128Const {
			return l, false, nil
		}
		if len(immediate.V128Bytes) != 16 {
			return l, true, fmt.Errorf("v128 literal must contain 16 bytes")
		}
		l.valueType = wasm.ValV128
		copy(l.bits[:], immediate.V128Bytes)
	default:
		return l, false, nil
	}
	return l, true, nil
}

// Instruction returns independent storage and preserves NaN payloads, signed
// zeros and vector lane bits. No numeric conversion or host floating arithmetic
// is involved. The routing domain belongs to the consumer, not to the value.
func (l Literal) Instruction() (wasm.Instruction, error) {
	switch l.valueType {
	case wasm.ValI32:
		return wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: int32(encodingbinary.LittleEndian.Uint32(l.bits[:]))}}, nil
	case wasm.ValI64:
		return wasm.Instruction{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: int64(encodingbinary.LittleEndian.Uint64(l.bits[:]))}}, nil
	case wasm.ValF32:
		return wasm.Instruction{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: math.Float32frombits(encodingbinary.LittleEndian.Uint32(l.bits[:]))}}, nil
	case wasm.ValF64:
		return wasm.Instruction{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: math.Float64frombits(encodingbinary.LittleEndian.Uint64(l.bits[:]))}}, nil
	case wasm.ValV128:
		return wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: append([]byte(nil), l.bits[:]...)}}, nil
	default:
		return wasm.Instruction{}, fmt.Errorf("absent literal has no instruction")
	}
}
