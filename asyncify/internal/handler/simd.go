package handler

import (
	"github.com/wippyai/wasm-runtime/wasm"
)

// SIMDHandler handles SIMD (128-bit vector) operations.
// SIMD instructions use the 0xFD prefix followed by a LEB128 sub-opcode.
// Sub-opcodes are defined in wasm.Simd* constants.
type SIMDHandler struct{}

// StackEffectWith implements StackEffectWith for SIMD instructions.
func (h SIMDHandler) StackEffectWith(instr wasm.Instruction) *StackEffect {
	imm, ok := instr.Imm.(wasm.SIMDImm)
	if !ok {
		return nil
	}

	subOp := imm.SubOpcode

	switch {
	// Loads: i32 -> v128
	case subOp == wasm.SimdV128Load || (subOp >= wasm.SimdV128Load8x8S && subOp <= wasm.SimdV128Load64Splat) ||
		subOp == wasm.SimdV128Load32Zero || subOp == wasm.SimdV128Load64Zero:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValV128}}

	// Store: (i32, v128) -> ()
	case subOp == wasm.SimdV128Store:
		return &StackEffect{Pops: 2, Pushes: nil}

	// v128.const: () -> v128
	case subOp == wasm.SimdV128Const:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValV128}}

	// Shuffle/Swizzle: (v128, v128) -> v128
	case subOp == wasm.SimdI8x16Shuffle || subOp == wasm.SimdI8x16Swizzle:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValV128}}

	// Splat: scalar -> v128
	case subOp >= wasm.SimdI8x16Splat && subOp <= wasm.SimdF64x2Splat:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValV128}}

	// Extract lane to i32
	case subOp == wasm.SimdI8x16ExtractLaneS || subOp == wasm.SimdI8x16ExtractLaneU ||
		subOp == wasm.SimdI16x8ExtractLaneS || subOp == wasm.SimdI16x8ExtractLaneU ||
		subOp == wasm.SimdI32x4ExtractLane:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	case subOp == wasm.SimdI64x2ExtractLane:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI64}}

	case subOp == wasm.SimdF32x4ExtractLane:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF32}}

	case subOp == wasm.SimdF64x2ExtractLane:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF64}}

	// Replace lane: (v128, scalar) -> v128
	case subOp == wasm.SimdI8x16ReplaceLane || subOp == wasm.SimdI16x8ReplaceLane ||
		subOp == wasm.SimdI32x4ReplaceLane || subOp == wasm.SimdI64x2ReplaceLane ||
		subOp == wasm.SimdF32x4ReplaceLane || subOp == wasm.SimdF64x2ReplaceLane:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValV128}}

	// Lane load: (i32, v128) -> v128
	case subOp >= wasm.SimdV128Load8Lane && subOp <= wasm.SimdV128Load64Lane:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValV128}}

	// Lane store: (i32, v128) -> ()
	case subOp >= wasm.SimdV128Store8Lane && subOp <= wasm.SimdV128Store64Lane:
		return &StackEffect{Pops: 2, Pushes: nil}

	// Bitmask/all_true: v128 -> i32
	case subOp == wasm.SimdV128AnyTrue ||
		subOp == wasm.SimdI8x16AllTrue || subOp == wasm.SimdI16x8AllTrue ||
		subOp == wasm.SimdI32x4AllTrue || subOp == wasm.SimdI64x2AllTrue ||
		subOp == wasm.SimdI8x16Bitmask || subOp == wasm.SimdI16x8Bitmask ||
		subOp == wasm.SimdI32x4Bitmask || subOp == wasm.SimdI64x2Bitmask:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	// Binary v128 ops: (v128, v128) -> v128
	case isBinaryV128Op(subOp):
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValV128}}

	// Unary v128 ops: v128 -> v128
	case isUnaryV128Op(subOp):
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValV128}}

	// Ternary (bitselect): (v128, v128, v128) -> v128
	case subOp == wasm.SimdV128Bitselect:
		return &StackEffect{Pops: 3, Pushes: []wasm.ValType{wasm.ValV128}}

	default:
		// Unknown - assume binary (v128, v128) -> v128
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValV128}}
	}
}

func (h SIMDHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm, ok := instr.Imm.(wasm.SIMDImm)
	if !ok {
		ctx.Emit.EmitInstr(instr)
		return nil
	}

	subOp := imm.SubOpcode

	switch {
	// Loads that produce v128 from address
	case subOp == wasm.SimdV128Load || (subOp >= wasm.SimdV128Load8x8S && subOp <= wasm.SimdV128Load64Splat) ||
		subOp == wasm.SimdV128Load32Zero || subOp == wasm.SimdV128Load64Zero:
		addr := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(addr).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Store (address, v128) -> ()
	case subOp == wasm.SimdV128Store:
		val := ctx.Stack.Pop()
		addr := ctx.Stack.Pop()
		ctx.Emit.LocalGet(addr).LocalGet(val).EmitInstr(instr)

	// v128.const pushes constant
	case subOp == wasm.SimdV128Const:
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Shuffle: (v128, v128) -> v128
	case subOp == wasm.SimdI8x16Shuffle:
		b := ctx.Stack.Pop()
		a := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(a).LocalGet(b).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Swizzle: (v128, v128) -> v128
	case subOp == wasm.SimdI8x16Swizzle:
		b := ctx.Stack.Pop()
		a := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(a).LocalGet(b).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Splat: scalar -> v128
	case subOp >= wasm.SimdI8x16Splat && subOp <= wasm.SimdF64x2Splat:
		scalar := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(scalar).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Extract lane: v128 -> scalar
	case subOp == wasm.SimdI8x16ExtractLaneS || subOp == wasm.SimdI8x16ExtractLaneU ||
		subOp == wasm.SimdI16x8ExtractLaneS || subOp == wasm.SimdI16x8ExtractLaneU ||
		subOp == wasm.SimdI32x4ExtractLane:
		vec := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(vec).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	case subOp == wasm.SimdI64x2ExtractLane:
		vec := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI64)
		ctx.Emit.LocalGet(vec).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI64)

	case subOp == wasm.SimdF32x4ExtractLane:
		vec := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValF32)
		ctx.Emit.LocalGet(vec).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValF32)

	case subOp == wasm.SimdF64x2ExtractLane:
		vec := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValF64)
		ctx.Emit.LocalGet(vec).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValF64)

	// Replace lane: (v128, scalar) -> v128
	case subOp == wasm.SimdI8x16ReplaceLane || subOp == wasm.SimdI16x8ReplaceLane ||
		subOp == wasm.SimdI32x4ReplaceLane || subOp == wasm.SimdI64x2ReplaceLane ||
		subOp == wasm.SimdF32x4ReplaceLane || subOp == wasm.SimdF64x2ReplaceLane:
		scalar := ctx.Stack.Pop()
		vec := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(vec).LocalGet(scalar).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Lane load: (address, v128) -> v128
	case subOp >= wasm.SimdV128Load8Lane && subOp <= wasm.SimdV128Load64Lane:
		vec := ctx.Stack.Pop()
		addr := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(addr).LocalGet(vec).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Lane store: (address, v128) -> ()
	case subOp >= wasm.SimdV128Store8Lane && subOp <= wasm.SimdV128Store64Lane:
		vec := ctx.Stack.Pop()
		addr := ctx.Stack.Pop()
		ctx.Emit.LocalGet(addr).LocalGet(vec).EmitInstr(instr)

	// Bitmask and all_true: v128 -> i32
	case subOp == wasm.SimdV128AnyTrue ||
		subOp == wasm.SimdI8x16AllTrue || subOp == wasm.SimdI16x8AllTrue ||
		subOp == wasm.SimdI32x4AllTrue || subOp == wasm.SimdI64x2AllTrue ||
		subOp == wasm.SimdI8x16Bitmask || subOp == wasm.SimdI16x8Bitmask ||
		subOp == wasm.SimdI32x4Bitmask || subOp == wasm.SimdI64x2Bitmask:
		vec := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValI32)
		ctx.Emit.LocalGet(vec).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValI32)

	// Binary v128 operations: (v128, v128) -> v128
	// This covers comparisons, arithmetic, bitwise, etc.
	case isBinaryV128Op(subOp):
		b := ctx.Stack.Pop()
		a := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(a).LocalGet(b).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Unary v128 operations: v128 -> v128
	case isUnaryV128Op(subOp):
		a := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(a).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	// Ternary v128 operations (bitselect): (v128, v128, v128) -> v128
	case subOp == wasm.SimdV128Bitselect:
		c := ctx.Stack.Pop()
		b := ctx.Stack.Pop()
		a := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(a).LocalGet(b).LocalGet(c).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)

	default:
		// Unhandled SIMD ops - assume binary (v128, v128) -> v128 for consistency with simulation
		b := ctx.Stack.Pop()
		a := ctx.Stack.Pop()
		tmp := ctx.AllocTemp(wasm.ValV128)
		ctx.Emit.LocalGet(a).LocalGet(b).EmitInstr(instr).LocalSet(tmp)
		ctx.Stack.Push(tmp, wasm.ValV128)
	}

	return nil
}

// isBinaryV128Op returns true if the sub-opcode is a binary v128 -> v128 operation.
// SIMD binary operations take two v128 values and produce one v128 result.
// Ranges based on WASM SIMD spec (https://webassembly.github.io/simd/core/binary/instructions.html)
func isBinaryV128Op(subOp uint32) bool {
	// Comparisons (eq, ne, lt, gt, le, ge for various types)
	// i8x16 comparisons: 0x23-0x28
	// i16x8 comparisons: 0x2D-0x34
	// i32x4 comparisons: 0x37-0x40
	// f32x4 comparisons: 0x41-0x46
	// f64x2 comparisons: 0x47-0x4C
	if (subOp >= 0x23 && subOp <= 0x28) ||
		(subOp >= 0x2D && subOp <= 0x34) ||
		(subOp >= 0x37 && subOp <= 0x40) ||
		(subOp >= 0x41 && subOp <= 0x46) ||
		(subOp >= 0x47 && subOp <= 0x4C) {
		return true
	}

	// v128 bitwise: 0x4E (and), 0x4F (andnot), 0x50 (or), 0x51 (xor)
	if subOp >= 0x4E && subOp <= 0x51 {
		return true
	}

	// i8x16 arithmetic: add/sub/min/max/avgr etc (0x6E-0x73)
	// Note: 0x74-0x77 are f64x2 rounding (unary), handled in isUnaryV128Op
	if subOp >= wasm.SimdI8x16Add && subOp <= 0x73 {
		return true
	}

	// i16x8 arithmetic: 0x8D-0x9B
	// Note: 0x9C-0x9F are i64x2 extend (unary), handled in isUnaryV128Op
	if subOp >= 0x8D && subOp <= 0x9B {
		return true
	}

	// i32x4 arithmetic: 0xAB-0xBB
	// Note: 0xBC-0xBF are convert ops (unary), handled in isUnaryV128Op
	if subOp >= 0xAB && subOp <= 0xBB {
		return true
	}

	// i64x2 arithmetic: 0xC5-0xD6
	if subOp >= 0xC5 && subOp <= 0xD6 {
		return true
	}

	// f32x4 arithmetic: 0xE4-0xEB
	// Note: 0xEC-0xEF are f64x2 abs/neg/sqrt (unary), handled in isUnaryV128Op
	if subOp >= 0xE4 && subOp <= 0xEB {
		return true
	}

	// f64x2 arithmetic: 0xF0-0xFD
	if subOp >= 0xF0 && subOp <= 0xFD {
		return true
	}

	return false
}

// isUnaryV128Op returns true if the sub-opcode is a unary v128 -> v128 operation.
// SIMD unary operations take one v128 value and produce one v128 result.
func isUnaryV128Op(subOp uint32) bool {
	// v128.not = 0x4D
	if subOp == 0x4D {
		return true
	}

	// i8x16 unary: abs (0x60), neg (0x61), popcnt (0x62)
	if subOp >= wasm.SimdI8x16Abs && subOp <= wasm.SimdI8x16Popcnt {
		return true
	}

	// i16x8 unary: abs (0x80), neg (0x81)
	if subOp >= 0x80 && subOp <= 0x81 {
		return true
	}

	// i32x4 unary: abs (0xA0), neg (0xA1)
	if subOp >= 0xA0 && subOp <= 0xA1 {
		return true
	}

	// i64x2 unary: abs (0xC0), neg (0xC1)
	if subOp >= 0xC0 && subOp <= 0xC1 {
		return true
	}

	// f32x4 rounding: ceil, floor, trunc, nearest (0xE0-0xE3)
	if subOp >= wasm.SimdF32x4Ceil && subOp <= wasm.SimdF32x4Nearest {
		return true
	}

	// f64x2 rounding: ceil, floor, trunc, nearest (0x74-0x77)
	if subOp >= wasm.SimdF64x2Ceil && subOp <= wasm.SimdF64x2Nearest {
		return true
	}

	// f32x4 unary: abs (0x67), neg (0x68), sqrt (0x69)
	if subOp == wasm.SimdF32x4Abs || subOp == wasm.SimdF32x4Neg || subOp == wasm.SimdF32x4Sqrt {
		return true
	}

	// f64x2 unary: abs (0xEC), neg (0xED), sqrt (0xEF)
	if subOp == wasm.SimdF64x2Abs || subOp == wasm.SimdF64x2Neg || subOp == wasm.SimdF64x2Sqrt {
		return true
	}

	// Extend/narrow operations (unary)
	// i16x8 extend low/high: 0x5E-0x5F
	// i32x4 extend: 0x7C-0x7F
	// i64x2 extend: 0x9C-0x9F
	// f32x4/f64x2 convert: 0xBC-0xBF
	if (subOp >= 0x5E && subOp <= 0x5F) ||
		(subOp >= wasm.SimdI16x8ExtAddPairwiseI8x16S && subOp <= wasm.SimdI16x8ExtAddPairwiseI8x16U) ||
		(subOp >= 0x9C && subOp <= 0x9F) ||
		(subOp >= 0xBC && subOp <= 0xBF) {
		return true
	}

	return false
}

// RegisterSIMDHandlers adds the SIMD handler for all 0xFD prefix operations.
func RegisterSIMDHandlers(r *Registry) {
	r.Register(wasm.OpPrefixSIMD, SIMDHandler{}, "simd")
}
