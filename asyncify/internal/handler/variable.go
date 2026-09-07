package handler

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// Local handlers share the same resolved effects as stack planning. The
// emitter chooses bytecode for those effects; it does not define new stack or
// snapshot semantics for each opcode.
type LocalGetHandler struct{}
type LocalSetHandler struct{}
type LocalTeeHandler struct{}

func (LocalGetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	return emitLocalOperation(ctx, instr)
}
func (LocalSetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	return emitLocalOperation(ctx, instr)
}
func (LocalTeeHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	return emitLocalOperation(ctx, instr)
}

func emitLocalOperation(ctx *Context, instr wasm.Instruction) error {
	op, handled, err := semantics.ResolveLocal(instr, ctx.Locals.types)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("asyncify: non-local opcode %#x in local handler", instr.Opcode)
	}
	if op.Effects&semantics.AssignLocal != 0 {
		ctx.Locals.InvalidateLocal(op.Index)
	}
	if op.Effects == semantics.ProduceSnapshot {
		tmp, reuse := ctx.Locals.SnapshotLocal(op.Index, op.Type)
		if !reuse {
			ctx.Emit.LocalGet(op.Index).LocalSet(tmp)
		}
		ctx.Stack.Push(tmp, op.Type)
		return nil
	}
	src := ctx.Stack.Pop()
	if op.Effects&semantics.ProduceSnapshot == 0 {
		ctx.Emit.Operand(src).LocalSet(op.Index)
		return nil
	}
	tmp := ctx.AllocTemp(op.Type)
	forwarded, err := src.MaterializedAt(tmp)
	if err != nil {
		return err
	}
	ctx.Emit.Operand(src)
	if op.Effects&semantics.AssignLocal != 0 {
		ctx.Emit.LocalTee(op.Index)
	}
	ctx.Emit.LocalSet(tmp)
	ctx.Stack.PushOperand(forwarded)
	return nil
}

// Global handlers consume the same checked identity and type as planning.
// Reads produce snapshots; writes modify the global cell.
type GlobalGetHandler struct{}
type GlobalSetHandler struct{}

func (GlobalGetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	return emitGlobalOperation(ctx, instr)
}

func (GlobalSetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	return emitGlobalOperation(ctx, instr)
}

func emitGlobalOperation(ctx *Context, instr wasm.Instruction) error {
	op, handled, err := semantics.ResolveGlobal(instr, ctx.Module)
	if err != nil {
		return err
	}
	if !handled {
		return fmt.Errorf("asyncify: non-global opcode %#x in global handler", instr.Opcode)
	}
	if op.Write {
		src := ctx.Stack.Pop()
		ctx.Emit.Operand(src).GlobalSet(op.Index)
	} else {
		tmp := ctx.AllocTemp(op.Type)
		ctx.Emit.GlobalGet(op.Index).LocalSet(tmp)
		ctx.Stack.Push(tmp, op.Type)
	}
	return nil
}

// RegisterVariableHandlers adds handlers for all local and global variable
// access instructions. These form the foundation of the flattening transform
// since they bridge between WebAssembly's operand stack and our local-based
// representation.
func RegisterVariableHandlers(r *Registry) {
	r.Register(wasm.OpLocalGet, LocalGetHandler{}, "local.get")
	r.Register(wasm.OpLocalSet, LocalSetHandler{}, "local.set")
	r.Register(wasm.OpLocalTee, LocalTeeHandler{}, "local.tee")
	r.Register(wasm.OpGlobalGet, GlobalGetHandler{}, "global.get")
	r.Register(wasm.OpGlobalSet, GlobalSetHandler{}, "global.set")
}
