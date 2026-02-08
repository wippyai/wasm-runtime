package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// TableGetHandler handles table.get instruction.
// Takes an index (i32) from stack, returns reference type.
type TableGetHandler struct{}

func (h TableGetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.TableImm)
	idx := ctx.Stack.Pop()

	// Determine the element type of the table
	elemType := wasm.ValFuncRef
	if ctx.Module != nil && int(imm.TableIdx) < len(ctx.Module.Tables) {
		if wasm.ValType(ctx.Module.Tables[imm.TableIdx].ElemType) == wasm.ValExtern {
			elemType = wasm.ValExtern
		}
	}

	tmp := ctx.AllocTemp(elemType)
	ctx.Emit.LocalGet(idx).EmitInstr(wasm.Instruction{
		Opcode: wasm.OpTableGet,
		Imm:    imm,
	}).LocalSet(tmp)
	ctx.Stack.Push(tmp, elemType)

	return nil
}

// TableSetHandler handles table.set instruction.
// Takes (index i32, value ref) from stack.
type TableSetHandler struct{}

func (h TableSetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.TableImm)
	val := ctx.Stack.Pop()
	idx := ctx.Stack.Pop()

	ctx.Emit.LocalGet(idx).LocalGet(val).EmitInstr(wasm.Instruction{
		Opcode: wasm.OpTableSet,
		Imm:    imm,
	})

	return nil
}

// RefNullHandler handles ref.null instruction.
// Pushes a null reference of the specified type.
type RefNullHandler struct{}

func (h RefNullHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.RefNullImm)
	var refType wasm.ValType
	if imm.HeapType == wasm.HeapTypeFunc {
		refType = wasm.ValFuncRef
	} else {
		refType = wasm.ValExtern
	}

	tmp := ctx.AllocTemp(refType)
	ctx.Emit.EmitInstr(instr).LocalSet(tmp)
	ctx.Stack.Push(tmp, refType)

	return nil
}

// RefIsNullHandler handles ref.is_null instruction.
// Takes a reference and returns i32 (0 or 1).
type RefIsNullHandler struct{}

func (h RefIsNullHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	ref := ctx.Stack.Pop()
	tmp := ctx.AllocTemp(wasm.ValI32)

	ctx.Emit.LocalGet(ref).EmitInstr(wasm.Instruction{Opcode: wasm.OpRefIsNull}).LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValI32)

	return nil
}

// RefFuncHandler handles ref.func instruction.
// Pushes a function reference constant.
type RefFuncHandler struct{}

func (h RefFuncHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	tmp := ctx.AllocTemp(wasm.ValFuncRef)
	ctx.Emit.EmitInstr(instr).LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValFuncRef)

	return nil
}

// RefAsNonNullHandler handles ref.as_non_null instruction (typed function references).
// Takes a nullable reference and returns non-null (or traps if null).
type RefAsNonNullHandler struct{}

func (h RefAsNonNullHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	entry := ctx.Stack.PopTyped()
	tmp := ctx.AllocTemp(entry.Type)
	ctx.Emit.LocalGet(entry.LocalIdx).EmitInstr(wasm.Instruction{Opcode: wasm.OpRefAsNonNull}).LocalSet(tmp)
	ctx.Stack.Push(tmp, entry.Type)
	return nil
}

// RefEqHandler handles ref.eq instruction (GC proposal).
// Compares two references and returns i32 (0 or 1).
type RefEqHandler struct{}

func (h RefEqHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	ref2 := ctx.Stack.Pop()
	ref1 := ctx.Stack.Pop()
	tmp := ctx.AllocTemp(wasm.ValI32)
	ctx.Emit.LocalGet(ref1).LocalGet(ref2).EmitInstr(wasm.Instruction{Opcode: wasm.OpRefEq}).LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValI32)
	return nil
}

// RegisterReferenceHandlers adds handlers for reference type operations.
func RegisterReferenceHandlers(r *Registry) {
	r.Register(wasm.OpTableGet, TableGetHandler{}, "table.get")
	r.Register(wasm.OpTableSet, TableSetHandler{}, "table.set")
	r.Register(wasm.OpRefNull, RefNullHandler{}, "ref.null")
	r.Register(wasm.OpRefIsNull, RefIsNullHandler{}, "ref.is_null")
	r.Register(wasm.OpRefFunc, RefFuncHandler{}, "ref.func")
	r.Register(wasm.OpRefAsNonNull, RefAsNonNullHandler{}, "ref.as_non_null")
	r.Register(wasm.OpRefEq, RefEqHandler{}, "ref.eq")
	// Note: br_on_null and br_on_non_null are control flow instructions
	// handled directly in transform.go like br_if
}
