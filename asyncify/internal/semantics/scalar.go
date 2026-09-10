package semantics

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// ScalarSignature returns the input and output operand types for base scalar
// numeric arithmetic, comparison, unary, conversion, and sign-extension instructions.
// It returns handled=false for all other instructions (including constants, SIMD,
// prefixed saturation, parametric, variable, memory, reference, and control flow).
//
// Input types are returned in bottom-to-top operand stack order (lhs, then rhs).
// The returned slices are freshly allocated and do not alias mutable global state.
func ScalarSignature(opcode byte) (inputs []wasm.ValType, outputs []wasm.ValType, handled bool) {
	switch opcode {
	// i32 unary bit counting: [i32] -> [i32]
	case wasm.OpI32Clz,
		wasm.OpI32Ctz,
		wasm.OpI32Popcnt:
		return unary(wasm.ValI32, wasm.ValI32)

	// i32 testop (comparison with zero): [i32] -> [i32]
	case wasm.OpI32Eqz:
		return unary(wasm.ValI32, wasm.ValI32)

	// i32 sign-extension: [i32] -> [i32]
	case wasm.OpI32Extend8S,
		wasm.OpI32Extend16S:
		return unary(wasm.ValI32, wasm.ValI32)

	// i32 binary arithmetic: [i32, i32] -> [i32]
	case wasm.OpI32Add,
		wasm.OpI32Sub,
		wasm.OpI32Mul,
		wasm.OpI32DivS,
		wasm.OpI32DivU,
		wasm.OpI32RemS,
		wasm.OpI32RemU:
		return binary(wasm.ValI32, wasm.ValI32, wasm.ValI32)

	// i32 bitwise and shift/rotate operations: [i32, i32] -> [i32]
	case wasm.OpI32And,
		wasm.OpI32Or,
		wasm.OpI32Xor,
		wasm.OpI32Shl,
		wasm.OpI32ShrS,
		wasm.OpI32ShrU,
		wasm.OpI32Rotl,
		wasm.OpI32Rotr:
		return binary(wasm.ValI32, wasm.ValI32, wasm.ValI32)

	// i32 binary comparisons (relops): [i32, i32] -> [i32]
	case wasm.OpI32Eq,
		wasm.OpI32Ne,
		wasm.OpI32LtS,
		wasm.OpI32LtU,
		wasm.OpI32GtS,
		wasm.OpI32GtU,
		wasm.OpI32LeS,
		wasm.OpI32LeU,
		wasm.OpI32GeS,
		wasm.OpI32GeU:
		return binary(wasm.ValI32, wasm.ValI32, wasm.ValI32)

	// i64 unary bit counting: [i64] -> [i64]
	case wasm.OpI64Clz,
		wasm.OpI64Ctz,
		wasm.OpI64Popcnt:
		return unary(wasm.ValI64, wasm.ValI64)

	// i64 testop (comparison with zero): [i64] -> [i32]
	case wasm.OpI64Eqz:
		return unary(wasm.ValI64, wasm.ValI32)

	// i64 sign-extension: [i64] -> [i64]
	case wasm.OpI64Extend8S,
		wasm.OpI64Extend16S,
		wasm.OpI64Extend32S:
		return unary(wasm.ValI64, wasm.ValI64)

	// i64 binary arithmetic: [i64, i64] -> [i64]
	case wasm.OpI64Add,
		wasm.OpI64Sub,
		wasm.OpI64Mul,
		wasm.OpI64DivS,
		wasm.OpI64DivU,
		wasm.OpI64RemS,
		wasm.OpI64RemU:
		return binary(wasm.ValI64, wasm.ValI64, wasm.ValI64)

	// i64 bitwise and shift/rotate operations: [i64, i64] -> [i64]
	case wasm.OpI64And,
		wasm.OpI64Or,
		wasm.OpI64Xor,
		wasm.OpI64Shl,
		wasm.OpI64ShrS,
		wasm.OpI64ShrU,
		wasm.OpI64Rotl,
		wasm.OpI64Rotr:
		return binary(wasm.ValI64, wasm.ValI64, wasm.ValI64)

	// i64 binary comparisons (relops): [i64, i64] -> [i32]
	case wasm.OpI64Eq,
		wasm.OpI64Ne,
		wasm.OpI64LtS,
		wasm.OpI64LtU,
		wasm.OpI64GtS,
		wasm.OpI64GtU,
		wasm.OpI64LeS,
		wasm.OpI64LeU,
		wasm.OpI64GeS,
		wasm.OpI64GeU:
		return binary(wasm.ValI64, wasm.ValI64, wasm.ValI32)

	// f32 unary operations: [f32] -> [f32]
	case wasm.OpF32Abs,
		wasm.OpF32Neg,
		wasm.OpF32Ceil,
		wasm.OpF32Floor,
		wasm.OpF32Trunc,
		wasm.OpF32Nearest,
		wasm.OpF32Sqrt:
		return unary(wasm.ValF32, wasm.ValF32)

	// f32 binary arithmetic: [f32, f32] -> [f32]
	case wasm.OpF32Add,
		wasm.OpF32Sub,
		wasm.OpF32Mul,
		wasm.OpF32Div,
		wasm.OpF32Min,
		wasm.OpF32Max,
		wasm.OpF32Copysign:
		return binary(wasm.ValF32, wasm.ValF32, wasm.ValF32)

	// f32 binary comparisons (relops): [f32, f32] -> [i32]
	case wasm.OpF32Eq,
		wasm.OpF32Ne,
		wasm.OpF32Lt,
		wasm.OpF32Gt,
		wasm.OpF32Le,
		wasm.OpF32Ge:
		return binary(wasm.ValF32, wasm.ValF32, wasm.ValI32)

	// f64 unary operations: [f64] -> [f64]
	case wasm.OpF64Abs,
		wasm.OpF64Neg,
		wasm.OpF64Ceil,
		wasm.OpF64Floor,
		wasm.OpF64Trunc,
		wasm.OpF64Nearest,
		wasm.OpF64Sqrt:
		return unary(wasm.ValF64, wasm.ValF64)

	// f64 binary arithmetic: [f64, f64] -> [f64]
	case wasm.OpF64Add,
		wasm.OpF64Sub,
		wasm.OpF64Mul,
		wasm.OpF64Div,
		wasm.OpF64Min,
		wasm.OpF64Max,
		wasm.OpF64Copysign:
		return binary(wasm.ValF64, wasm.ValF64, wasm.ValF64)

	// f64 binary comparisons (relops): [f64, f64] -> [i32]
	case wasm.OpF64Eq,
		wasm.OpF64Ne,
		wasm.OpF64Lt,
		wasm.OpF64Gt,
		wasm.OpF64Le,
		wasm.OpF64Ge:
		return binary(wasm.ValF64, wasm.ValF64, wasm.ValI32)

	// Conversions: i64 -> i32
	case wasm.OpI32WrapI64:
		return unary(wasm.ValI64, wasm.ValI32)

	// Conversions: f32 -> i32
	case wasm.OpI32TruncF32S,
		wasm.OpI32TruncF32U,
		wasm.OpI32ReinterpretF32:
		return unary(wasm.ValF32, wasm.ValI32)

	// Conversions: f64 -> i32
	case wasm.OpI32TruncF64S,
		wasm.OpI32TruncF64U:
		return unary(wasm.ValF64, wasm.ValI32)

	// Conversions: i32 -> i64
	case wasm.OpI64ExtendI32S,
		wasm.OpI64ExtendI32U:
		return unary(wasm.ValI32, wasm.ValI64)

	// Conversions: f32 -> i64
	case wasm.OpI64TruncF32S,
		wasm.OpI64TruncF32U:
		return unary(wasm.ValF32, wasm.ValI64)

	// Conversions: f64 -> i64
	case wasm.OpI64TruncF64S,
		wasm.OpI64TruncF64U,
		wasm.OpI64ReinterpretF64:
		return unary(wasm.ValF64, wasm.ValI64)

	// Conversions: i32 -> f32
	case wasm.OpF32ConvertI32S,
		wasm.OpF32ConvertI32U,
		wasm.OpF32ReinterpretI32:
		return unary(wasm.ValI32, wasm.ValF32)

	// Conversions: i64 -> f32
	case wasm.OpF32ConvertI64S,
		wasm.OpF32ConvertI64U:
		return unary(wasm.ValI64, wasm.ValF32)

	// Conversions: f64 -> f32
	case wasm.OpF32DemoteF64:
		return unary(wasm.ValF64, wasm.ValF32)

	// Conversions: i32 -> f64
	case wasm.OpF64ConvertI32S,
		wasm.OpF64ConvertI32U:
		return unary(wasm.ValI32, wasm.ValF64)

	// Conversions: i64 -> f64
	case wasm.OpF64ConvertI64S,
		wasm.OpF64ConvertI64U,
		wasm.OpF64ReinterpretI64:
		return unary(wasm.ValI64, wasm.ValF64)

	// Conversions: f32 -> f64
	case wasm.OpF64PromoteF32:
		return unary(wasm.ValF32, wasm.ValF64)

	default:
		return nil, nil, false
	}
}

func unary(in, out wasm.ValType) ([]wasm.ValType, []wasm.ValType, bool) {
	return []wasm.ValType{in}, []wasm.ValType{out}, true
}

func binary(lhs, rhs, out wasm.ValType) ([]wasm.ValType, []wasm.ValType, bool) {
	return []wasm.ValType{lhs, rhs}, []wasm.ValType{out}, true
}
