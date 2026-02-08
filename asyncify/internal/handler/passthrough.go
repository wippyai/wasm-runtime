package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// PassthroughHandler emits instructions unchanged without stack simulation.
//
// During asyncify transformation, most code gets "flattened" - values that
// normally live on the WebAssembly operand stack are instead stored in
// local variables. This allows the transformed code to be wrapped in
// conditional blocks that check asyncify state. However, some instructions
// don't need this treatment because they either don't interact with the
// stack in ways that matter for asyncify, or they're being emitted in a
// context where flattening has already happened.
//
// The passthrough handler simply re-emits the instruction byte-for-byte.
// This is used when processing instructions that appear inside an
// "if state == normal" block where we've already set up the flattened
// local variables and just need to execute the actual operation.
type PassthroughHandler struct{}

func (h PassthroughHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	ctx.Emit.EmitInstr(instr)
	return nil
}

// DropHandler removes the top value from the simulated operand stack.
//
// In flattened asyncify code, values are stored in local variables rather
// than on the operand stack. When we see a drop, we simply pop from our
// simulated stack (forgetting the local that held the value). No actual
// code emission is needed because the value is already in a local - we
// just stop tracking it.
//
// This is correct because:
// 1. The real operand stack is empty in flattened mode (all values in locals)
// 2. Dropping means "discard this value" - achieved by removing from tracking
// 3. The local can be reused later by the allocator if needed
type DropHandler struct{}

func (h DropHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	ctx.Stack.Pop()
	// No code emission - value is in a local, we just stop tracking it
	return nil
}

// NopHandler emits the no-operation instruction.
//
// Nop does nothing and has no stack effect. We emit it unchanged. Some
// toolchains insert nops for alignment or as placeholders, and preserving
// them maintains compatibility with debugging tools that expect specific
// instruction offsets.
type NopHandler struct{}

func (h NopHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	ctx.Emit.Nop()
	return nil
}

// UnreachableHandler emits the unreachable trap instruction.
//
// Unreachable indicates code that should never execute. The WebAssembly
// runtime will trap if it reaches this instruction. We emit it unchanged
// since it has no stack effect and represents a hard stop in control flow.
// During asyncify transformation, unreachable often appears after br
// instructions that exit to the save path, marking dead code.
type UnreachableHandler struct{}

func (h UnreachableHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	ctx.Emit.Unreachable()
	return nil
}

// RegisterPassthroughHandlers adds handlers for instructions that need
// minimal transformation. These opcodes either have no stack effect or
// are handled specially by the transformation engine in other ways.
func RegisterPassthroughHandlers(r *Registry) {
	r.Register(wasm.OpNop, NopHandler{}, "nop")
	r.Register(wasm.OpUnreachable, UnreachableHandler{}, "unreachable")
	r.Register(wasm.OpDrop, DropHandler{}, "drop")
}
