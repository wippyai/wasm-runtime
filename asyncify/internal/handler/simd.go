package handler

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// SIMDHandler handles SIMD (128-bit vector) operations.
// SIMD instructions use the 0xFD prefix followed by a LEB128 sub-opcode.
// Sub-opcodes are defined in wasm.Simd* constants.
type SIMDHandler struct{}

// simdSignature returns the operand count and result type of a SIMD sub-opcode,
// as listed in the WebAssembly 2.0 vector instruction encoding. A zero result
// type means the instruction pushes nothing. Unknown sub-opcodes return false.
func simdSignature(subOp uint32) (pops int, result wasm.ValType, ok bool) {
	v128 := wasm.ValV128
	switch {
	case subOp <= wasm.SimdV128Load64Splat, subOp == wasm.SimdV128Load32Zero, subOp == wasm.SimdV128Load64Zero:
		return 1, v128, true
	case subOp == wasm.SimdV128Store:
		return 2, 0, true
	case subOp == wasm.SimdV128Const:
		return 0, v128, true
	case subOp == wasm.SimdI8x16Shuffle, subOp == wasm.SimdI8x16Swizzle:
		return 2, v128, true
	case subOp >= wasm.SimdI8x16Splat && subOp <= wasm.SimdF64x2Splat:
		return 1, v128, true
	case subOp == wasm.SimdI8x16ExtractLaneS, subOp == wasm.SimdI8x16ExtractLaneU,
		subOp == wasm.SimdI16x8ExtractLaneS, subOp == wasm.SimdI16x8ExtractLaneU,
		subOp == wasm.SimdI32x4ExtractLane:
		return 1, wasm.ValI32, true
	case subOp == wasm.SimdI64x2ExtractLane:
		return 1, wasm.ValI64, true
	case subOp == wasm.SimdF32x4ExtractLane:
		return 1, wasm.ValF32, true
	case subOp == wasm.SimdF64x2ExtractLane:
		return 1, wasm.ValF64, true
	case subOp == wasm.SimdI8x16ReplaceLane, subOp == wasm.SimdI16x8ReplaceLane,
		subOp == wasm.SimdI32x4ReplaceLane, subOp == wasm.SimdI64x2ReplaceLane,
		subOp == wasm.SimdF32x4ReplaceLane, subOp == wasm.SimdF64x2ReplaceLane:
		return 2, v128, true
	case subOp >= 0x23 && subOp <= 0x4C: // comparisons
		return 2, v128, true
	case subOp == wasm.SimdV128Not:
		return 1, v128, true
	case subOp >= wasm.SimdV128And && subOp <= wasm.SimdV128Xor:
		return 2, v128, true
	case subOp == wasm.SimdV128Bitselect:
		return 3, v128, true
	case subOp >= wasm.SimdV128Load8Lane && subOp <= wasm.SimdV128Load64Lane:
		return 2, v128, true
	case subOp >= wasm.SimdV128Store8Lane && subOp <= wasm.SimdV128Store64Lane:
		return 2, 0, true
	case subOp == wasm.SimdV128AnyTrue,
		subOp == wasm.SimdI8x16AllTrue, subOp == wasm.SimdI8x16Bitmask,
		subOp == wasm.SimdI16x8AllTrue, subOp == wasm.SimdI16x8Bitmask,
		subOp == wasm.SimdI32x4AllTrue, subOp == wasm.SimdI32x4Bitmask,
		subOp == wasm.SimdI64x2AllTrue, subOp == wasm.SimdI64x2Bitmask:
		return 1, wasm.ValI32, true
	}
	if simdUnary(subOp) {
		return 1, v128, true
	}
	if simdBinary(subOp) {
		return 2, v128, true
	}
	return 0, 0, false
}

// simdUnary reports v128 -> v128 operations: abs, neg, popcnt, sqrt, rounding,
// extend, extadd_pairwise, demote, promote, and conversions.
func simdUnary(subOp uint32) bool {
	switch {
	case subOp == 0x5E || subOp == 0x5F: // f32x4.demote_f64x2_zero, f64x2.promote_low_f32x4
		return true
	case subOp >= wasm.SimdI8x16Abs && subOp <= wasm.SimdI8x16Popcnt:
		return true
	case subOp >= wasm.SimdF32x4Ceil && subOp <= wasm.SimdF32x4Nearest:
		return true
	case subOp == wasm.SimdF64x2Ceil, subOp == wasm.SimdF64x2Floor,
		subOp == wasm.SimdF64x2Trunc, subOp == wasm.SimdF64x2Nearest:
		return true
	case subOp >= wasm.SimdI16x8ExtAddPairwiseI8x16S && subOp <= 0x7F: // extadd_pairwise
		return true
	case subOp == 0x80 || subOp == 0x81: // i16x8.abs, neg
		return true
	case subOp >= 0x87 && subOp <= 0x8A: // i16x8.extend_*
		return true
	case subOp == 0xA0 || subOp == 0xA1: // i32x4.abs, neg
		return true
	case subOp >= 0xA7 && subOp <= 0xAA: // i32x4.extend_*
		return true
	case subOp == 0xC0 || subOp == 0xC1: // i64x2.abs, neg
		return true
	case subOp >= 0xC7 && subOp <= 0xCA: // i64x2.extend_*
		return true
	case subOp == wasm.SimdF32x4Abs, subOp == wasm.SimdF32x4Neg, subOp == wasm.SimdF32x4Sqrt,
		subOp == wasm.SimdF64x2Abs, subOp == wasm.SimdF64x2Neg, subOp == wasm.SimdF64x2Sqrt:
		return true
	case subOp >= 0xF8 && subOp <= 0xFF: // trunc_sat and convert
		return true
	}
	return false
}

// simdBinary reports (v128, v128) -> v128 and (v128, i32) -> v128 shift
// operations: narrow, shifts, arithmetic, min/max, avgr, q15mulr, dot, extmul.
func simdBinary(subOp uint32) bool {
	switch {
	case subOp == wasm.SimdI8x16NarrowI16x8S, subOp == wasm.SimdI8x16NarrowI16x8U:
		return true
	case subOp >= wasm.SimdI8x16Shl && subOp <= wasm.SimdI8x16SubSatU:
		return true
	case subOp >= 0x76 && subOp <= 0x79, subOp == 0x7B: // i8x16.min/max, avgr_u
		return true
	case subOp >= 0x82 && subOp <= 0x86 && subOp != wasm.SimdI16x8AllTrue && subOp != wasm.SimdI16x8Bitmask:
		return true // i16x8.q15mulr_sat_s, narrow_i32x4_*
	case subOp >= 0x8B && subOp <= 0x93: // i16x8 shifts, add, sub
		return true
	case subOp >= 0x95 && subOp <= 0x99, subOp >= 0x9B && subOp <= 0x9F: // i16x8 mul, min/max, avgr_u, extmul
		return true
	case subOp >= 0xAB && subOp <= 0xAE, subOp == 0xB1: // i32x4 shifts, add, sub
		return true
	case subOp >= 0xB5 && subOp <= 0xBA, subOp >= 0xBC && subOp <= 0xBF: // i32x4 mul, min/max, dot, extmul
		return true
	case subOp >= 0xCB && subOp <= 0xCE, subOp == 0xD1: // i64x2 shifts, add, sub
		return true
	case subOp >= 0xD5 && subOp <= 0xDF: // i64x2 mul, comparisons, extmul
		return true
	case subOp >= 0xE4 && subOp <= 0xEB: // f32x4 arithmetic, min/max, pmin/pmax
		return true
	case subOp >= 0xF0 && subOp <= 0xF7: // f64x2 arithmetic, min/max, pmin/pmax
		return true
	}
	return false
}

// StackEffectWith implements StackEffectWith for SIMD instructions.
func (h SIMDHandler) StackEffectWith(instr wasm.Instruction) *StackEffect {
	imm, ok := instr.Imm.(wasm.SIMDImm)
	if !ok {
		return nil
	}
	pops, result, ok := simdSignature(imm.SubOpcode)
	if !ok {
		return nil
	}
	effect := &StackEffect{Pops: pops}
	if result != 0 {
		effect.Pushes = []wasm.ValType{result}
	}
	return effect
}

func (h SIMDHandler) Handle(ctx *Context, instr wasm.Instruction) error {
	imm, ok := instr.Imm.(wasm.SIMDImm)
	if !ok {
		ctx.Emit.EmitInstr(instr)
		return nil
	}
	// Vector constants share the same bit-exact literal semantics as scalars.
	if imm.SubOpcode == wasm.SimdV128Const {
		return (LiteralHandler{}).Handle(ctx, instr)
	}
	pops, result, ok := simdSignature(imm.SubOpcode)
	if !ok {
		return fmt.Errorf("asyncify: unsupported SIMD sub-opcode 0x%x", imm.SubOpcode)
	}
	operands := make([]StackEntry, pops)
	for i := pops - 1; i >= 0; i-- {
		operands[i] = ctx.Stack.Pop()
	}
	for _, operand := range operands {
		ctx.Emit.Operand(operand)
	}
	ctx.Emit.EmitInstr(instr)
	if result != 0 {
		tmp := ctx.AllocTemp(result)
		ctx.Emit.LocalSet(tmp)
		ctx.Stack.Push(tmp, result)
	}
	return nil
}

// RegisterSIMDHandlers adds the SIMD handler for all 0xFD prefix operations.
func RegisterSIMDHandlers(r *Registry) {
	r.Register(wasm.OpPrefixSIMD, SIMDHandler{}, "simd")
}
