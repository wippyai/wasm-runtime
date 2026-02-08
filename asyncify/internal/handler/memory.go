package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// LoadHandler reads values from WebAssembly linear memory.
//
// Load instructions take an address (i32) from the stack, add an immediate
// offset, and read a value from that memory location. The loaded value is
// pushed onto the stack. Different load variants read different sizes and
// types (i32.load, i64.load, i32.load8_s, etc).
//
// In the flattened representation, we pop the address local, emit the load
// instruction with its alignment and offset, and store the result in a new
// temporary. The temporary's type matches the load instruction's result type.
//
// Memory loads are important for asyncify because the asyncify data buffer
// itself is in linear memory. During rewinding, the engine reads saved
// locals from this buffer using load instructions. The handlers here process
// user loads, not asyncify's internal memory operations.
//
// The MemoryImm immediate contains alignment (as log2 of bytes) and offset.
// We preserve these unchanged since they affect performance but not semantics.
type LoadHandler struct {
	Opcode     byte
	ResultType wasm.ValType
}

func (h LoadHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.MemoryImm)
	addr := ctx.Stack.Pop()
	tmp := ctx.AllocTemp(h.ResultType)

	ctx.Emit.LocalGet(addr).EmitInstr(wasm.Instruction{
		Opcode: h.Opcode,
		Imm:    imm,
	}).LocalSet(tmp)
	ctx.Stack.Push(tmp, h.ResultType)

	return nil
}

// StoreHandler writes values to WebAssembly linear memory.
//
// Store instructions take an address (i32) and a value from the stack,
// then write the value to memory at address+offset. Nothing is pushed
// back onto the stack.
//
// In the flattened model, we pop both the value and address locals,
// emit the store with both values loaded, and our simulated stack
// shrinks by two entries with no new entry pushed.
//
// Stores have similar alignment and offset immediates as loads.
type StoreHandler struct {
	Opcode byte
}

func (h StoreHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm := instr.Imm.(wasm.MemoryImm)
	value := ctx.Stack.Pop()
	addr := ctx.Stack.Pop()

	ctx.Emit.LocalGet(addr).LocalGet(value).EmitInstr(wasm.Instruction{
		Opcode: h.Opcode,
		Imm:    imm,
	})

	return nil
}

// MemorySizeHandler queries the current size of linear memory.
//
// The memory.size instruction takes no arguments and returns the current
// memory size in pages (each page is 64KB). This is useful for bounds
// checking or determining how much memory is available.
//
// We allocate a temporary to hold the result and push it onto the
// simulated stack. The instruction itself has no immediates.
type MemorySizeHandler struct{}

func (h MemorySizeHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	tmp := ctx.AllocTemp(wasm.ValI32)

	ctx.Emit.MemorySize().LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValI32)

	return nil
}

// MemoryGrowHandler attempts to grow linear memory.
//
// The memory.grow instruction takes a delta (number of pages to add) and
// returns the previous memory size, or -1 if the growth failed. Growing
// memory can fail if the runtime limits are exceeded or the system is
// out of memory.
//
// Like other operations, we pop the delta, emit the grow, and push the
// result to a new temporary. The old size (or -1) becomes available in
// that local.
type MemoryGrowHandler struct{}

func (h MemoryGrowHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	delta := ctx.Stack.Pop()
	tmp := ctx.AllocTemp(wasm.ValI32)

	ctx.Emit.LocalGet(delta).MemoryGrow().LocalSet(tmp)
	ctx.Stack.Push(tmp, wasm.ValI32)

	return nil
}

// RegisterMemoryHandlers adds handlers for all memory access operations.
// These cover loads and stores for all value types and sizes, plus the
// memory.size and memory.grow instructions for dynamic memory management.
func RegisterMemoryHandlers(r *Registry) {
	// i32 loads: full word and partial with sign/zero extension
	r.Register(wasm.OpI32Load, LoadHandler{wasm.OpI32Load, wasm.ValI32}, "i32.load")
	r.Register(wasm.OpI32Load8S, LoadHandler{wasm.OpI32Load8S, wasm.ValI32}, "i32.load8_s")
	r.Register(wasm.OpI32Load8U, LoadHandler{wasm.OpI32Load8U, wasm.ValI32}, "i32.load8_u")
	r.Register(wasm.OpI32Load16S, LoadHandler{wasm.OpI32Load16S, wasm.ValI32}, "i32.load16_s")
	r.Register(wasm.OpI32Load16U, LoadHandler{wasm.OpI32Load16U, wasm.ValI32}, "i32.load16_u")

	// i64 loads: full word and partial with sign/zero extension
	r.Register(wasm.OpI64Load, LoadHandler{wasm.OpI64Load, wasm.ValI64}, "i64.load")
	r.Register(wasm.OpI64Load8S, LoadHandler{wasm.OpI64Load8S, wasm.ValI64}, "i64.load8_s")
	r.Register(wasm.OpI64Load8U, LoadHandler{wasm.OpI64Load8U, wasm.ValI64}, "i64.load8_u")
	r.Register(wasm.OpI64Load16S, LoadHandler{wasm.OpI64Load16S, wasm.ValI64}, "i64.load16_s")
	r.Register(wasm.OpI64Load16U, LoadHandler{wasm.OpI64Load16U, wasm.ValI64}, "i64.load16_u")
	r.Register(wasm.OpI64Load32S, LoadHandler{wasm.OpI64Load32S, wasm.ValI64}, "i64.load32_s")
	r.Register(wasm.OpI64Load32U, LoadHandler{wasm.OpI64Load32U, wasm.ValI64}, "i64.load32_u")

	// Float loads: always full width
	r.Register(wasm.OpF32Load, LoadHandler{wasm.OpF32Load, wasm.ValF32}, "f32.load")
	r.Register(wasm.OpF64Load, LoadHandler{wasm.OpF64Load, wasm.ValF64}, "f64.load")

	// i32 stores: full word and partial
	r.Register(wasm.OpI32Store, StoreHandler{wasm.OpI32Store}, "i32.store")
	r.Register(wasm.OpI32Store8, StoreHandler{wasm.OpI32Store8}, "i32.store8")
	r.Register(wasm.OpI32Store16, StoreHandler{wasm.OpI32Store16}, "i32.store16")

	// i64 stores: full word and partial
	r.Register(wasm.OpI64Store, StoreHandler{wasm.OpI64Store}, "i64.store")
	r.Register(wasm.OpI64Store8, StoreHandler{wasm.OpI64Store8}, "i64.store8")
	r.Register(wasm.OpI64Store16, StoreHandler{wasm.OpI64Store16}, "i64.store16")
	r.Register(wasm.OpI64Store32, StoreHandler{wasm.OpI64Store32}, "i64.store32")

	// Float stores: always full width
	r.Register(wasm.OpF32Store, StoreHandler{wasm.OpF32Store}, "f32.store")
	r.Register(wasm.OpF64Store, StoreHandler{wasm.OpF64Store}, "f64.store")

	// Memory management
	r.Register(wasm.OpMemorySize, MemorySizeHandler{}, "memory.size")
	r.Register(wasm.OpMemoryGrow, MemoryGrowHandler{}, "memory.grow")

	// Bulk memory operations (0xFC prefix)
	r.Register(wasm.OpPrefixMisc, BulkMemoryHandler{}, "bulk_memory")
}

// BulkMemoryHandler handles bulk memory operations (0xFC prefix).
// This includes memory.copy, memory.fill, memory.init, and data.drop.
type BulkMemoryHandler struct{}

// StackEffectWith implements StackEffectWith for Misc/bulk memory instructions.
func (h BulkMemoryHandler) StackEffectWith(instr wasm.Instruction) *StackEffect {
	imm, ok := instr.Imm.(wasm.MiscImm)
	if !ok {
		return nil
	}

	switch imm.SubOpcode {
	// Saturating trunc: f32/f64 -> i32
	case wasm.MiscI32TruncSatF32S, wasm.MiscI32TruncSatF32U,
		wasm.MiscI32TruncSatF64S, wasm.MiscI32TruncSatF64U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	// Saturating trunc: f32/f64 -> i64
	case wasm.MiscI64TruncSatF32S, wasm.MiscI64TruncSatF32U,
		wasm.MiscI64TruncSatF64S, wasm.MiscI64TruncSatF64U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI64}}

	// Bulk memory ops: (d, s, n) -> ()
	case wasm.MiscMemoryInit, wasm.MiscMemoryCopy, wasm.MiscMemoryFill,
		wasm.MiscTableInit, wasm.MiscTableCopy, wasm.MiscTableFill:
		return &StackEffect{Pops: 3, Pushes: nil}

	// Drop ops: () -> ()
	case wasm.MiscDataDrop, wasm.MiscElemDrop:
		return &StackEffect{Pops: 0, Pushes: nil}

	// table.grow: (init, n) -> i32
	case wasm.MiscTableGrow:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValI32}}

	// table.size: () -> i32
	case wasm.MiscTableSize:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValI32}}

	default:
		return nil
	}
}

func (h BulkMemoryHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm, ok := instr.Imm.(wasm.MiscImm)
	if !ok {
		// Passthrough if not a misc immediate
		ctx.Emit.EmitInstr(instr)
		return nil
	}

	switch imm.SubOpcode {
	case wasm.MiscMemoryCopy:
		// memory.copy: (d, s, n) -> ()
		n := ctx.Stack.Pop()
		s := ctx.Stack.Pop()
		d := ctx.Stack.Pop()
		ctx.Emit.LocalGet(d).LocalGet(s).LocalGet(n).EmitInstr(instr)

	case wasm.MiscMemoryFill:
		// memory.fill: (d, v, n) -> ()
		n := ctx.Stack.Pop()
		v := ctx.Stack.Pop()
		d := ctx.Stack.Pop()
		ctx.Emit.LocalGet(d).LocalGet(v).LocalGet(n).EmitInstr(instr)

	case wasm.MiscMemoryInit:
		// memory.init: (d, s, n) -> ()
		n := ctx.Stack.Pop()
		s := ctx.Stack.Pop()
		d := ctx.Stack.Pop()
		ctx.Emit.LocalGet(d).LocalGet(s).LocalGet(n).EmitInstr(instr)

	case wasm.MiscDataDrop:
		// data.drop: () -> ()
		ctx.Emit.EmitInstr(instr)

	case wasm.MiscTableInit:
		// table.init: (d, s, n) -> ()
		n := ctx.Stack.Pop()
		s := ctx.Stack.Pop()
		d := ctx.Stack.Pop()
		ctx.Emit.LocalGet(d).LocalGet(s).LocalGet(n).EmitInstr(instr)

	case wasm.MiscElemDrop:
		// elem.drop: () -> ()
		ctx.Emit.EmitInstr(instr)

	case wasm.MiscTableCopy:
		// table.copy: (d, s, n) -> ()
		n := ctx.Stack.Pop()
		s := ctx.Stack.Pop()
		d := ctx.Stack.Pop()
		ctx.Emit.LocalGet(d).LocalGet(s).LocalGet(n).EmitInstr(instr)

	case wasm.MiscTableGrow:
		// table.grow: (init, n) -> (i32)
		n := ctx.Stack.Pop()
		init := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(init).LocalGet(n).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	case wasm.MiscTableSize:
		// table.size: () -> (i32)
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	case wasm.MiscTableFill:
		// table.fill: (d, v, n) -> ()
		n := ctx.Stack.Pop()
		v := ctx.Stack.Pop()
		d := ctx.Stack.Pop()
		ctx.Emit.LocalGet(d).LocalGet(v).LocalGet(n).EmitInstr(instr)

	// Saturating truncation operations: (f32/f64) -> (i32/i64)
	case wasm.MiscI32TruncSatF32S, wasm.MiscI32TruncSatF32U,
		wasm.MiscI32TruncSatF64S, wasm.MiscI32TruncSatF64U:
		operand := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(operand).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	case wasm.MiscI64TruncSatF32S, wasm.MiscI64TruncSatF32U,
		wasm.MiscI64TruncSatF64S, wasm.MiscI64TruncSatF64U:
		operand := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI64)
		ctx.Emit.LocalGet(operand).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI64)

	default:
		// Passthrough for any unhandled misc opcodes
		ctx.Emit.EmitInstr(instr)
	}

	return nil
}
