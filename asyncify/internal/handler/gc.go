package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// GCHandler handles GC prefix (0xFB) operations.
// GC operations work with struct/array/ref types which are reference types.
// These cannot be saved to linear memory, but we track them on the simulated
// stack so reference type detection works correctly at async call sites.
type GCHandler struct{}

// StackEffectWith implements StackEffectWith for GC instructions.
func (h GCHandler) StackEffectWith(instr wasm.Instruction) *StackEffect {
	imm, ok := instr.Imm.(wasm.GCImm)
	if !ok {
		return nil
	}

	// GC types are reference types - use funcref as representative
	refType := wasm.ValFuncRef

	switch imm.SubOpcode {
	// struct.new: pops field values, pushes structref
	case wasm.GCStructNew:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{refType}}

	case wasm.GCStructNewDefault:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{refType}}

	// struct.get: pop structref, push field value
	case wasm.GCStructGet, wasm.GCStructGetS, wasm.GCStructGetU:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	// struct.set: pop value, pop structref
	case wasm.GCStructSet:
		return &StackEffect{Pops: 2, Pushes: nil}

	// array.new: pop init, pop length, push arrayref
	case wasm.GCArrayNew:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{refType}}

	case wasm.GCArrayNewDefault:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{refType}}

	case wasm.GCArrayNewFixed:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{refType}}

	case wasm.GCArrayNewData, wasm.GCArrayNewElem:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{refType}}

	// array.get: pop index, pop arrayref, push element
	case wasm.GCArrayGet, wasm.GCArrayGetS, wasm.GCArrayGetU:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValI32}}

	// array.set: pop value, pop index, pop arrayref
	case wasm.GCArraySet:
		return &StackEffect{Pops: 3, Pushes: nil}

	// array.len: pop arrayref, push i32
	case wasm.GCArrayLen:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	// array.fill: pop n, pop value, pop offset, pop arrayref
	case wasm.GCArrayFill:
		return &StackEffect{Pops: 4, Pushes: nil}

	// array.copy: pop n, pop src_offset, pop src, pop dst_offset, pop dst
	case wasm.GCArrayCopy:
		return &StackEffect{Pops: 5, Pushes: nil}

	// array.init_data/elem: pop n, pop src_offset, pop dst_offset, pop arrayref
	case wasm.GCArrayInitData, wasm.GCArrayInitElem:
		return &StackEffect{Pops: 4, Pushes: nil}

	// ref.test: pop ref, push i32
	case wasm.GCRefTest, wasm.GCRefTestNull:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	// ref.cast: pop ref, push ref
	case wasm.GCRefCast, wasm.GCRefCastNull:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{refType}}

	// br_on_cast variants: keep ref on stack
	case wasm.GCBrOnCast, wasm.GCBrOnCastFail:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{refType}}

	// any.convert_extern, extern.convert_any: ref -> ref
	case wasm.GCAnyConvertExtern, wasm.GCExternConvertAny:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{refType}}

	// ref.i31: i32 -> i31ref
	case wasm.GCRefI31:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{refType}}

	// i31.get_s/u: i31ref -> i32
	case wasm.GCI31GetS, wasm.GCI31GetU:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	default:
		return nil
	}
}

func (h GCHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm, ok := instr.Imm.(wasm.GCImm)
	if !ok {
		ctx.Emit.EmitInstr(instr)
		return nil
	}

	// Helper to push a reference type
	pushRef := func() {
		tmp := ctx.AllocTemp(wasm.ValFuncRef)
		ctx.Emit.EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValFuncRef)
	}

	switch imm.SubOpcode {
	// struct.new: pops field values, pushes structref
	case wasm.GCStructNew:
		// For struct.new, we need type info to know field count
		// The instruction will consume values from real stack
		// We just emit and track the result
		pushRef()

	case wasm.GCStructNewDefault:
		pushRef()

	// struct.get: pop structref, push field value
	case wasm.GCStructGet, wasm.GCStructGetS, wasm.GCStructGetU:
		ref := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32) // type unknown, use i32
		ctx.Emit.LocalGet(ref).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	// struct.set: pop value, pop structref
	case wasm.GCStructSet:
		val := ctx.Stack.Pop()
		ref := ctx.Stack.Pop()
		ctx.Emit.LocalGet(ref).LocalGet(val).EmitInstr(instr)

	// array.new: pop init, pop length, push arrayref
	case wasm.GCArrayNew:
		length := ctx.Stack.Pop()
		init := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValFuncRef)
		ctx.Emit.LocalGet(init).LocalGet(length).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValFuncRef)

	case wasm.GCArrayNewDefault:
		length := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValFuncRef)
		ctx.Emit.LocalGet(length).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValFuncRef)

	case wasm.GCArrayNewFixed:
		// Pops N elements, push arrayref
		// Without type info, just emit and push ref
		pushRef()

	case wasm.GCArrayNewData, wasm.GCArrayNewElem:
		length := ctx.Stack.Pop()
		offset := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValFuncRef)
		ctx.Emit.LocalGet(offset).LocalGet(length).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValFuncRef)

	// array.get: pop index, pop arrayref, push element
	case wasm.GCArrayGet, wasm.GCArrayGetS, wasm.GCArrayGetU:
		idx := ctx.Stack.Pop()
		arr := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(arr).LocalGet(idx).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	// array.set: pop value, pop index, pop arrayref
	case wasm.GCArraySet:
		val := ctx.Stack.Pop()
		idx := ctx.Stack.Pop()
		arr := ctx.Stack.Pop()
		ctx.Emit.LocalGet(arr).LocalGet(idx).LocalGet(val).EmitInstr(instr)

	// array.len: pop arrayref, push i32
	case wasm.GCArrayLen:
		arr := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(arr).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	// array.fill: pop n, pop value, pop offset, pop arrayref
	case wasm.GCArrayFill:
		n := ctx.Stack.Pop()
		val := ctx.Stack.Pop()
		offset := ctx.Stack.Pop()
		arr := ctx.Stack.Pop()
		ctx.Emit.LocalGet(arr).LocalGet(offset).LocalGet(val).LocalGet(n).EmitInstr(instr)

	// array.copy: pop n, pop src_offset, pop src, pop dst_offset, pop dst
	case wasm.GCArrayCopy:
		n := ctx.Stack.Pop()
		srcOff := ctx.Stack.Pop()
		src := ctx.Stack.Pop()
		dstOff := ctx.Stack.Pop()
		dst := ctx.Stack.Pop()
		ctx.Emit.LocalGet(dst).LocalGet(dstOff).LocalGet(src).LocalGet(srcOff).LocalGet(n).EmitInstr(instr)

	// array.init_data/elem: pop n, pop src_offset, pop dst_offset, pop arrayref
	case wasm.GCArrayInitData, wasm.GCArrayInitElem:
		n := ctx.Stack.Pop()
		srcOff := ctx.Stack.Pop()
		dstOff := ctx.Stack.Pop()
		arr := ctx.Stack.Pop()
		ctx.Emit.LocalGet(arr).LocalGet(dstOff).LocalGet(srcOff).LocalGet(n).EmitInstr(instr)

	// ref.test: pop ref, push i32
	case wasm.GCRefTest, wasm.GCRefTestNull:
		ref := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(ref).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	// ref.cast: pop ref, push ref
	case wasm.GCRefCast, wasm.GCRefCastNull:
		ref := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValFuncRef)
		ctx.Emit.LocalGet(ref).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValFuncRef)

	// br_on_cast variants: control flow with ref on stack
	case wasm.GCBrOnCast, wasm.GCBrOnCastFail:
		entry := ctx.Stack.PopTyped()
		tmp := ctx.AllocTemp(entry.Type)
		ctx.Emit.LocalGet(entry.LocalIdx).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, entry.Type)

	// any.convert_extern, extern.convert_any: ref -> ref
	case wasm.GCAnyConvertExtern, wasm.GCExternConvertAny:
		ref := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValFuncRef)
		ctx.Emit.LocalGet(ref).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValFuncRef)

	// ref.i31: i32 -> i31ref
	case wasm.GCRefI31:
		val := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValFuncRef)
		ctx.Emit.LocalGet(val).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValFuncRef)

	// i31.get_s/u: i31ref -> i32
	case wasm.GCI31GetS, wasm.GCI31GetU:
		ref := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(ref).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	default:
		// Unknown GC opcode - passthrough
		ctx.Emit.EmitInstr(instr)
	}

	return nil
}

// RegisterGCHandlers adds handlers for GC prefix operations.
func RegisterGCHandlers(r *Registry) {
	r.Register(wasm.OpPrefixGC, GCHandler{}, "gc")
}
