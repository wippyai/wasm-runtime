package handler

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// LiteralHandler records an exact immutable operand. Consumers emit its bits;
// literal definitions need neither a temporary local nor a continuation slot.
type LiteralHandler struct{}

func (h LiteralHandler) Handle(ctx *Context, instruction wasm.Instruction) error {
	literal, handled, err := semantics.ResolveLiteral(instruction)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("literal handler received nonliteral opcode 0x%x", instruction.Opcode)
	}
	ctx.Stack.PushOperand(semantics.LiteralOperand(literal))
	return nil
}

type I32ConstHandler = LiteralHandler
type I64ConstHandler = LiteralHandler
type F32ConstHandler = LiteralHandler
type F64ConstHandler = LiteralHandler

func RegisterConstantHandlers(r *Registry) {
	r.Register(wasm.OpI32Const, LiteralHandler{}, "i32.const")
	r.Register(wasm.OpI64Const, LiteralHandler{}, "i64.const")
	r.Register(wasm.OpF32Const, LiteralHandler{}, "f32.const")
	r.Register(wasm.OpF64Const, LiteralHandler{}, "f64.const")
}
