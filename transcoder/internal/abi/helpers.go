package abi

import (
	"math"
	"reflect"
)

func SafeMulU32(a, b uint32) (uint32, bool) {
	if b != 0 && a > math.MaxUint32/b {
		return 0, false
	}
	return a * b, true
}

func SafeAddU32(a, b uint32) (uint32, bool) {
	if a > math.MaxUint32-b {
		return 0, false
	}
	return a + b, true
}

// TypeName returns "nil" for nil values, avoiding reflect.TypeOf(nil) panic.
func TypeName(value any) string {
	if value == nil {
		return "nil"
	}
	return reflect.TypeOf(value).String()
}

func AlignTo(offset, align uint32) uint32 {
	if align == 0 {
		return offset
	}
	return (offset + align - 1) &^ (align - 1)
}

const (
	CanonicalNaN32 = 0x7fc00000
	CanonicalNaN64 = 0x7ff8000000000000
)

const (
	MaxStringSize = 1 << 30 // 1 GB max string size
	MaxListLength = 1 << 27 // 128M max elements
	MaxAlloc      = 1 << 30 // 1 GB max single allocation
)

// CanonicalizeF32 returns canonical NaN for any NaN input per Canonical ABI spec.
func CanonicalizeF32(bits uint32) uint32 {
	f := math.Float32frombits(bits)
	if f != f { // NaN check
		return CanonicalNaN32
	}
	return bits
}

// CanonicalizeF64 returns canonical NaN for any NaN input per Canonical ABI spec.
func CanonicalizeF64(bits uint64) uint64 {
	f := math.Float64frombits(bits)
	if f != f { // NaN check
		return CanonicalNaN64
	}
	return bits
}

// ValidateChar rejects surrogates (0xD800-0xDFFF) and values >= 0x110000 per spec.
func ValidateChar(r rune) bool {
	if r >= 0xD800 && r <= 0xDFFF {
		return false
	}
	if r < 0 || r >= 0x110000 {
		return false
	}
	return true
}
