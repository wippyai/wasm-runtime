package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// IsReferenceType returns true if the type is a reference type (funcref, externref).
// Reference types cannot be stored to linear memory and are not compatible with asyncify.
func IsReferenceType(vt wasm.ValType) bool {
	return vt == wasm.ValFuncRef || vt == wasm.ValExtern
}

// CanStoreToMemory returns true if the type can be saved/restored via linear memory.
// Reference types cannot be stored to memory - they would need table operations.
func CanStoreToMemory(vt wasm.ValType) bool {
	switch vt {
	case wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64, wasm.ValV128:
		return true
	default:
		return false
	}
}

// ErrReferenceType is returned when attempting memory operations on reference types.
var ErrReferenceType = fmt.Errorf("reference types cannot be stored to linear memory for asyncify")

// ValTypeSize returns the byte size of a WebAssembly value type.
// Returns -1 for reference types as they cannot be stored to linear memory.
func ValTypeSize(vt wasm.ValType) int {
	switch vt {
	case wasm.ValI32, wasm.ValF32:
		return 4
	case wasm.ValI64, wasm.ValF64:
		return 8
	case wasm.ValV128:
		return 16
	case wasm.ValFuncRef, wasm.ValExtern:
		return -1 // reference types cannot be stored
	default:
		return 4
	}
}

// ValTypeLoadOp returns the appropriate load opcode and alignment for a type.
// For v128, returns OpPrefixSIMD - use EmitV128Load helper for proper emission.
// Returns (0, 0) for reference types as they cannot be loaded from linear memory.
func ValTypeLoadOp(vt wasm.ValType) (byte, uint32) {
	switch vt {
	case wasm.ValI32:
		return wasm.OpI32Load, 2
	case wasm.ValI64:
		return wasm.OpI64Load, 3
	case wasm.ValF32:
		return wasm.OpF32Load, 2
	case wasm.ValF64:
		return wasm.OpF64Load, 3
	case wasm.ValV128:
		return wasm.OpPrefixSIMD, 4
	case wasm.ValFuncRef, wasm.ValExtern:
		return 0, 0 // reference types cannot be loaded
	default:
		return wasm.OpI32Load, 2
	}
}

// IsV128Type returns true if the type is v128 (SIMD).
func IsV128Type(vt wasm.ValType) bool {
	return vt == wasm.ValV128
}

// MakeV128Load creates a v128.load instruction with the given alignment and offset.
func MakeV128Load(align, offset uint32) wasm.Instruction {
	return wasm.Instruction{
		Opcode: wasm.OpPrefixSIMD,
		Imm:    wasm.SIMDImm{SubOpcode: 0x00, MemArg: &wasm.MemoryImm{Align: align, Offset: uint64(offset)}},
	}
}

// MakeV128Store creates a v128.store instruction with the given alignment and offset.
func MakeV128Store(align, offset uint32) wasm.Instruction {
	return wasm.Instruction{
		Opcode: wasm.OpPrefixSIMD,
		Imm:    wasm.SIMDImm{SubOpcode: 0x0B, MemArg: &wasm.MemoryImm{Align: align, Offset: uint64(offset)}},
	}
}

// ValTypeStoreOp returns the appropriate store opcode and alignment for a type.
// For v128, returns the SIMD prefix (0xFD) - caller must emit the full SIMD instruction.
// Returns (0, 0) for reference types as they cannot be stored to linear memory.
func ValTypeStoreOp(vt wasm.ValType) (byte, uint32) {
	switch vt {
	case wasm.ValI32:
		return wasm.OpI32Store, 2
	case wasm.ValI64:
		return wasm.OpI64Store, 3
	case wasm.ValF32:
		return wasm.OpF32Store, 2
	case wasm.ValF64:
		return wasm.OpF64Store, 3
	case wasm.ValV128:
		return wasm.OpPrefixSIMD, 4 // v128.store is 0xFD 0x0B
	case wasm.ValFuncRef, wasm.ValExtern:
		return 0, 0 // reference types cannot be stored
	default:
		return wasm.OpI32Store, 2
	}
}

// ValidateLocalsForAsyncify checks if all locals can be saved/restored by asyncify.
// Returns an error if any local uses a reference type.
func ValidateLocalsForAsyncify(params []wasm.ValType, locals []wasm.LocalEntry) error {
	// Check parameters
	for i, p := range params {
		if IsReferenceType(p) {
			return fmt.Errorf("parameter %d has reference type %s which cannot be saved by asyncify", i, p)
		}
	}

	// Check locals
	localIdx := len(params)
	for _, entry := range locals {
		if IsReferenceType(entry.ValType) {
			return fmt.Errorf("local %d has reference type %s which cannot be saved by asyncify", localIdx, entry.ValType)
		}
		localIdx += int(entry.Count)
	}

	return nil
}
