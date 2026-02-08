package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// LocalGetHandler reads a local variable and pushes it onto the simulated stack.
//
// In standard WebAssembly, local.get pushes a value directly onto the operand
// stack. In asyncify's flattened representation, we instead copy the local's
// value into a fresh temporary local, then record that temporary in our
// simulated stack. This indirection is essential because the original local
// might be modified before an async call site, and we need a snapshot of
// the value at the point where local.get was executed.
//
// The emitted code does: local.get $original -> local.set $temp
// The simulated stack then tracks $temp as holding the value.
//
// When saving state during unwind, values in the simulated stack get written
// to the asyncify data buffer. When rewinding, they get restored. By using
// temporaries, we ensure the correct values are saved even if the original
// locals change between the local.get and the async call.
type LocalGetHandler struct{}

func (h LocalGetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.LocalImm)
	localType := ctx.TypeOf(imm.LocalIdx)
	tmp := ctx.AllocTemp(localType)

	ctx.Emit.LocalGet(imm.LocalIdx).LocalSet(tmp)
	ctx.Stack.Push(tmp, localType)

	return nil
}

// LocalSetHandler writes a value from the simulated stack into a local variable.
//
// The simulated stack tracks which temporary local holds the value we want
// to store. We pop that temporary's index, emit code to load from it, and
// store into the target local. This maintains the invariant that all stack
// operations go through our tracking system.
//
// Emitted code: local.get $temp -> local.set $target
//
// After this, the simulated stack no longer tracks the consumed value.
// The target local now holds the value, which will be saved/restored through
// the normal local save path during asyncify state transitions.
type LocalSetHandler struct{}

func (h LocalSetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.LocalImm)
	src := ctx.Stack.Pop()

	ctx.Emit.LocalGet(src).LocalSet(imm.LocalIdx)

	return nil
}

// LocalTeeHandler copies the top stack value to a local without consuming it.
//
// WebAssembly's local.tee is equivalent to: dup -> local.set -> (value remains)
// In our flattened model, we pop the source temporary, emit the store to the
// target local, then push the target local index onto the simulated stack.
// This way the "remaining" value is now tracked as being in the target local.
//
// This is a slight semantic change from the original local.tee behavior where
// the stack value stays on the operand stack. In flattened form, the target
// local itself becomes the new "stack slot". This works correctly because
// subsequent operations will read from that local.
type LocalTeeHandler struct{}

func (h LocalTeeHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.LocalImm)
	src := ctx.Stack.Pop()

	ctx.Emit.LocalGet(src).LocalSet(imm.LocalIdx)
	ctx.Stack.Push(imm.LocalIdx, ctx.TypeOf(imm.LocalIdx))

	return nil
}

// GlobalGetHandler reads a global variable and pushes it onto the simulated stack.
//
// Globals work similarly to locals in the flattened model. We allocate a
// temporary local to hold the global's value, emit the global.get and store,
// then track the temporary in our simulated stack. This captures the global's
// value at this point in execution.
//
// The handler looks up the global's type from module metadata, handling both
// imported globals and module-defined globals. If the module is not available
// in the context, it falls back to i32 (the most common case for asyncify
// globals).
type GlobalGetHandler struct{}

func (h GlobalGetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.GlobalImm)

	globalType := h.lookupGlobalType(ctx, imm.GlobalIdx)
	tmp := ctx.AllocTemp(globalType)

	ctx.Emit.GlobalGet(imm.GlobalIdx).LocalSet(tmp)
	ctx.Stack.Push(tmp, globalType)

	return nil
}

func (h GlobalGetHandler) lookupGlobalType(ctx *Context, globalIdx uint32) wasm.ValType {
	if ctx.Module == nil {
		return wasm.ValI32
	}

	// Count imported globals first
	numImportedGlobals := uint32(0)
	for _, imp := range ctx.Module.Imports {
		if imp.Desc.Kind == wasm.KindGlobal {
			if globalIdx == numImportedGlobals {
				if imp.Desc.Global != nil {
					return imp.Desc.Global.ValType
				}
				return wasm.ValI32
			}
			numImportedGlobals++
		}
	}

	// Then check module-defined globals
	localIdx := globalIdx - numImportedGlobals
	if int(localIdx) < len(ctx.Module.Globals) {
		return ctx.Module.Globals[localIdx].Type.ValType
	}

	return wasm.ValI32
}

// GlobalSetHandler writes a value from the simulated stack into a global variable.
//
// We pop the source temporary from our simulated stack, load its value,
// and store it into the target global. The global's new value will persist
// across async operations since globals are module-level state that doesn't
// need special asyncify handling.
type GlobalSetHandler struct{}

func (h GlobalSetHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.GlobalImm)
	src := ctx.Stack.Pop()

	ctx.Emit.LocalGet(src).GlobalSet(imm.GlobalIdx)

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
