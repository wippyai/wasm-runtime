package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// ConversionHandler transforms values from one WebAssembly type to another.
//
// WebAssembly has many conversion operations between its four value types
// (i32, i64, f32, f64). These include truncations (removing higher bits),
// extensions (adding bits), float/int conversions, and bit reinterpretations.
//
// All conversions follow the same flattening pattern: pop the source value's
// local index, emit the load-convert-store sequence, push the result's local
// index. The key difference from arithmetic operations is that input and
// output types differ.
//
// Some conversions can trap. For example, i32.trunc_f32_s will trap if the
// float value is too large to represent as a signed 32-bit integer, or if
// it's NaN. The asyncify transformation preserves this behavior since we
// emit the conversion opcode unchanged.
//
// Reinterpret operations (like i32.reinterpret_f32) are special: they don't
// change the bit pattern at all, just the type interpretation. A float's
// bits become an integer's bits. These never trap.
type ConversionHandler struct {
	Opcode     byte
	ResultType wasm.ValType
}

func (h ConversionHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	operand := ctx.Stack.Pop()
	tmp := ctx.AllocTemp(h.ResultType)

	ctx.Emit.LocalGet(operand).EmitRawOpcode(h.Opcode).LocalSet(tmp)
	ctx.Stack.Push(tmp, h.ResultType)

	return nil
}

// RegisterConversionHandlers adds handlers for all type conversion operations.
// These bridge between WebAssembly's type system, enabling operations like
// converting floats to integers, widening 32-bit values to 64-bit, and
// reinterpreting bit patterns as different types.
func RegisterConversionHandlers(r *Registry) {
	// Integer width conversions: wrap (narrow) and extend (widen)
	r.Register(wasm.OpI32WrapI64, ConversionHandler{wasm.OpI32WrapI64, wasm.ValI32}, "i32.wrap_i64")
	r.Register(wasm.OpI64ExtendI32S, ConversionHandler{wasm.OpI64ExtendI32S, wasm.ValI64}, "i64.extend_i32_s")
	r.Register(wasm.OpI64ExtendI32U, ConversionHandler{wasm.OpI64ExtendI32U, wasm.ValI64}, "i64.extend_i32_u")

	// Float to i32: truncate toward zero, can trap on overflow/NaN
	r.Register(wasm.OpI32TruncF32S, ConversionHandler{wasm.OpI32TruncF32S, wasm.ValI32}, "i32.trunc_f32_s")
	r.Register(wasm.OpI32TruncF32U, ConversionHandler{wasm.OpI32TruncF32U, wasm.ValI32}, "i32.trunc_f32_u")
	r.Register(wasm.OpI32TruncF64S, ConversionHandler{wasm.OpI32TruncF64S, wasm.ValI32}, "i32.trunc_f64_s")
	r.Register(wasm.OpI32TruncF64U, ConversionHandler{wasm.OpI32TruncF64U, wasm.ValI32}, "i32.trunc_f64_u")

	// Float to i64: same as above but for 64-bit integers
	r.Register(wasm.OpI64TruncF32S, ConversionHandler{wasm.OpI64TruncF32S, wasm.ValI64}, "i64.trunc_f32_s")
	r.Register(wasm.OpI64TruncF32U, ConversionHandler{wasm.OpI64TruncF32U, wasm.ValI64}, "i64.trunc_f32_u")
	r.Register(wasm.OpI64TruncF64S, ConversionHandler{wasm.OpI64TruncF64S, wasm.ValI64}, "i64.trunc_f64_s")
	r.Register(wasm.OpI64TruncF64U, ConversionHandler{wasm.OpI64TruncF64U, wasm.ValI64}, "i64.trunc_f64_u")

	// Integer to f32: may lose precision for large values
	r.Register(wasm.OpF32ConvertI32S, ConversionHandler{wasm.OpF32ConvertI32S, wasm.ValF32}, "f32.convert_i32_s")
	r.Register(wasm.OpF32ConvertI32U, ConversionHandler{wasm.OpF32ConvertI32U, wasm.ValF32}, "f32.convert_i32_u")
	r.Register(wasm.OpF32ConvertI64S, ConversionHandler{wasm.OpF32ConvertI64S, wasm.ValF32}, "f32.convert_i64_s")
	r.Register(wasm.OpF32ConvertI64U, ConversionHandler{wasm.OpF32ConvertI64U, wasm.ValF32}, "f32.convert_i64_u")

	// Integer to f64: f64 can exactly represent all i32 values
	r.Register(wasm.OpF64ConvertI32S, ConversionHandler{wasm.OpF64ConvertI32S, wasm.ValF64}, "f64.convert_i32_s")
	r.Register(wasm.OpF64ConvertI32U, ConversionHandler{wasm.OpF64ConvertI32U, wasm.ValF64}, "f64.convert_i32_u")
	r.Register(wasm.OpF64ConvertI64S, ConversionHandler{wasm.OpF64ConvertI64S, wasm.ValF64}, "f64.convert_i64_s")
	r.Register(wasm.OpF64ConvertI64U, ConversionHandler{wasm.OpF64ConvertI64U, wasm.ValF64}, "f64.convert_i64_u")

	// Float width conversions: demote (narrow) and promote (widen)
	r.Register(wasm.OpF32DemoteF64, ConversionHandler{wasm.OpF32DemoteF64, wasm.ValF32}, "f32.demote_f64")
	r.Register(wasm.OpF64PromoteF32, ConversionHandler{wasm.OpF64PromoteF32, wasm.ValF64}, "f64.promote_f32")

	// Bit reinterpretation: same bits, different type (no conversion)
	r.Register(wasm.OpI32ReinterpretF32, ConversionHandler{wasm.OpI32ReinterpretF32, wasm.ValI32}, "i32.reinterpret_f32")
	r.Register(wasm.OpI64ReinterpretF64, ConversionHandler{wasm.OpI64ReinterpretF64, wasm.ValI64}, "i64.reinterpret_f64")
	r.Register(wasm.OpF32ReinterpretI32, ConversionHandler{wasm.OpF32ReinterpretI32, wasm.ValF32}, "f32.reinterpret_i32")
	r.Register(wasm.OpF64ReinterpretI64, ConversionHandler{wasm.OpF64ReinterpretI64, wasm.ValF64}, "f64.reinterpret_i64")

	// Sign extension: extend lower bits with sign replication
	r.Register(wasm.OpI32Extend8S, ConversionHandler{wasm.OpI32Extend8S, wasm.ValI32}, "i32.extend8_s")
	r.Register(wasm.OpI32Extend16S, ConversionHandler{wasm.OpI32Extend16S, wasm.ValI32}, "i32.extend16_s")
	r.Register(wasm.OpI64Extend8S, ConversionHandler{wasm.OpI64Extend8S, wasm.ValI64}, "i64.extend8_s")
	r.Register(wasm.OpI64Extend16S, ConversionHandler{wasm.OpI64Extend16S, wasm.ValI64}, "i64.extend16_s")
	r.Register(wasm.OpI64Extend32S, ConversionHandler{wasm.OpI64Extend32S, wasm.ValI64}, "i64.extend32_s")
}
