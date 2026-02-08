package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// I32ConstHandler pushes a 32-bit integer constant onto the simulated stack.
//
// In normal WebAssembly, i32.const pushes the immediate value directly onto
// the operand stack. In asyncify's flattened representation, we allocate a
// temporary local, emit code to store the constant into it, and track that
// local in our simulated stack.
//
// This might seem wasteful compared to just using the constant directly,
// but the flattening is necessary for consistency. When we reach an async
// call site, we need all "stack" values to be in locals so they can be
// saved to the asyncify data buffer. Having constants already in locals
// means the save code doesn't need special cases.
//
// The emitted code: i32.const <value> -> local.set $temp
// The simulated stack records $temp as holding an i32.
type I32ConstHandler struct{}

func (h I32ConstHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.I32Imm)
	tmp := ctx.AllocTemp(wasm.ValI32)

	ctx.Emit.I32Const(imm.Value).LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValI32)

	return nil
}

// I64ConstHandler pushes a 64-bit integer constant onto the simulated stack.
//
// Works identically to I32ConstHandler but for 64-bit values. The temporary
// local is typed as i64, and the save/restore code will use i64.load/store
// operations when persisting this value to the asyncify data buffer.
type I64ConstHandler struct{}

func (h I64ConstHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.I64Imm)
	tmp := ctx.AllocTemp(wasm.ValI64)

	ctx.Emit.I64Const(imm.Value).LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValI64)

	return nil
}

// F32ConstHandler pushes a 32-bit float constant onto the simulated stack.
//
// Floating point constants follow the same pattern as integers. The value
// is stored to a temporary local of type f32. When saving state, the
// f32.store instruction preserves the exact bit pattern including NaN
// payloads and signed zeros.
type F32ConstHandler struct{}

func (h F32ConstHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.F32Imm)
	tmp := ctx.AllocTemp(wasm.ValF32)

	ctx.Emit.F32Const(imm.Value).LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValF32)

	return nil
}

// F64ConstHandler pushes a 64-bit float constant onto the simulated stack.
//
// Same pattern as other constants but for 64-bit floats. The temporary
// uses 8 bytes in the asyncify data buffer when saved, matching the
// f64's memory representation.
type F64ConstHandler struct{}

func (h F64ConstHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.F64Imm)
	tmp := ctx.AllocTemp(wasm.ValF64)

	ctx.Emit.F64Const(imm.Value).LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValF64)

	return nil
}

// RegisterConstantHandlers adds handlers for all constant-pushing instructions.
// These are the starting point for most computations in WebAssembly, loading
// immediate values that then get used by other operations.
func RegisterConstantHandlers(r *Registry) {
	r.Register(wasm.OpI32Const, I32ConstHandler{}, "i32.const")
	r.Register(wasm.OpI64Const, I64ConstHandler{}, "i64.const")
	r.Register(wasm.OpF32Const, F32ConstHandler{}, "f32.const")
	r.Register(wasm.OpF64Const, F64ConstHandler{}, "f64.const")
}
