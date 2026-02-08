package abi

import (
	"math"
	"testing"
)

func TestSafeMulU32(t *testing.T) {
	tests := []struct {
		name   string
		a, b   uint32
		want   uint32
		wantOK bool
	}{
		{"zero * zero", 0, 0, 0, true},
		{"zero * max", 0, math.MaxUint32, 0, true},
		{"max * zero", math.MaxUint32, 0, 0, true},
		{"one * one", 1, 1, 1, true},
		{"small * small", 100, 200, 20000, true},
		{"max * one", math.MaxUint32, 1, math.MaxUint32, true},
		{"one * max", 1, math.MaxUint32, math.MaxUint32, true},
		{"half * two", math.MaxUint32 / 2, 2, (math.MaxUint32 / 2) * 2, true},
		{"overflow", math.MaxUint32, 2, 0, false},
		{"overflow symmetric", 2, math.MaxUint32, 0, false},
		{"large overflow", 100000, 100000, 0, false},
		{"edge case ok", 65536, 65535, 65536 * 65535, true},
		{"edge case overflow", 65536, 65537, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SafeMulU32(tt.a, tt.b)
			if ok != tt.wantOK {
				t.Errorf("SafeMulU32(%d, %d) ok = %v, want %v", tt.a, tt.b, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("SafeMulU32(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestTypeName(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"nil", nil, "nil"},
		{"int", 42, "int"},
		{"string", "hello", "string"},
		{"float64", 3.14, "float64"},
		{"bool", true, "bool"},
		{"uint32", uint32(1), "uint32"},
		{"slice", []int{1, 2, 3}, "[]int"},
		{"map", map[string]int{}, "map[string]int"},
		{"struct", struct{ X int }{}, "struct { X int }"},
		{"pointer", new(int), "*int"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TypeName(tt.input)
			if got != tt.want {
				t.Errorf("TypeName(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAlignTo(t *testing.T) {
	tests := []struct {
		name   string
		offset uint32
		align  uint32
		want   uint32
	}{
		// Align 0 (edge case)
		{"align 0", 5, 0, 5},

		// Align 1
		{"offset 0 align 1", 0, 1, 0},
		{"offset 5 align 1", 5, 1, 5},
		{"offset max align 1", math.MaxUint32, 1, math.MaxUint32},

		// Align 2
		{"offset 0 align 2", 0, 2, 0},
		{"offset 1 align 2", 1, 2, 2},
		{"offset 2 align 2", 2, 2, 2},
		{"offset 3 align 2", 3, 2, 4},

		// Align 4
		{"offset 0 align 4", 0, 4, 0},
		{"offset 1 align 4", 1, 4, 4},
		{"offset 3 align 4", 3, 4, 4},
		{"offset 4 align 4", 4, 4, 4},
		{"offset 5 align 4", 5, 4, 8},
		{"offset 7 align 4", 7, 4, 8},

		// Align 8
		{"offset 0 align 8", 0, 8, 0},
		{"offset 1 align 8", 1, 8, 8},
		{"offset 7 align 8", 7, 8, 8},
		{"offset 8 align 8", 8, 8, 8},
		{"offset 9 align 8", 9, 8, 16},
		{"offset 15 align 8", 15, 8, 16},

		// Align 16
		{"offset 0 align 16", 0, 16, 0},
		{"offset 1 align 16", 1, 16, 16},
		{"offset 15 align 16", 15, 16, 16},
		{"offset 16 align 16", 16, 16, 16},
		{"offset 17 align 16", 17, 16, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AlignTo(tt.offset, tt.align)
			if got != tt.want {
				t.Errorf("AlignTo(%d, %d) = %d, want %d", tt.offset, tt.align, got, tt.want)
			}
		})
	}
}

func TestSafeAddU32(t *testing.T) {
	tests := []struct {
		name   string
		a, b   uint32
		want   uint32
		wantOK bool
	}{
		{"zero + zero", 0, 0, 0, true},
		{"zero + max", 0, math.MaxUint32, math.MaxUint32, true},
		{"max + zero", math.MaxUint32, 0, math.MaxUint32, true},
		{"one + one", 1, 1, 2, true},
		{"small + small", 100, 200, 300, true},
		{"max + one", math.MaxUint32, 1, 0, false},
		{"one + max", 1, math.MaxUint32, 0, false},
		{"half + half", math.MaxUint32 / 2, math.MaxUint32 / 2, math.MaxUint32 - 1, true},
		{"half + half + 1", math.MaxUint32/2 + 1, math.MaxUint32 / 2, math.MaxUint32, true},
		{"half + half + 2", math.MaxUint32/2 + 1, math.MaxUint32/2 + 1, 0, false},
		{"large address + size ok", 0xFFFF0000, 0x0000FFFF, 0xFFFFFFFF, true},
		{"large address + size overflow", 0xFFFF0000, 0x00010000, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SafeAddU32(tt.a, tt.b)
			if ok != tt.wantOK {
				t.Errorf("SafeAddU32(%d, %d) ok = %v, want %v", tt.a, tt.b, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("SafeAddU32(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCanonicalizeF32(t *testing.T) {
	tests := []struct {
		name string
		bits uint32
		want uint32
	}{
		{"zero", 0, 0},
		{"one", 0x3f800000, 0x3f800000},
		{"negative zero", 0x80000000, 0x80000000},
		{"infinity", 0x7f800000, 0x7f800000},
		{"negative infinity", 0xff800000, 0xff800000},
		{"canonical NaN", CanonicalNaN32, CanonicalNaN32},
		{"quiet NaN 1", 0x7fc00001, CanonicalNaN32},
		{"quiet NaN 2", 0x7fc12345, CanonicalNaN32},
		{"signaling NaN", 0x7f800001, CanonicalNaN32},
		{"negative NaN", 0xffc00000, CanonicalNaN32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeF32(tt.bits)
			if got != tt.want {
				t.Errorf("CanonicalizeF32(0x%08x) = 0x%08x, want 0x%08x", tt.bits, got, tt.want)
			}
		})
	}
}

func TestCanonicalizeF64(t *testing.T) {
	tests := []struct {
		name string
		bits uint64
		want uint64
	}{
		{"zero", 0, 0},
		{"one", 0x3ff0000000000000, 0x3ff0000000000000},
		{"negative zero", 0x8000000000000000, 0x8000000000000000},
		{"infinity", 0x7ff0000000000000, 0x7ff0000000000000},
		{"negative infinity", 0xfff0000000000000, 0xfff0000000000000},
		{"canonical NaN", CanonicalNaN64, CanonicalNaN64},
		{"quiet NaN 1", 0x7ff8000000000001, CanonicalNaN64},
		{"quiet NaN 2", 0x7ff8123456789abc, CanonicalNaN64},
		{"signaling NaN", 0x7ff0000000000001, CanonicalNaN64},
		{"negative NaN", 0xfff8000000000000, CanonicalNaN64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeF64(tt.bits)
			if got != tt.want {
				t.Errorf("CanonicalizeF64(0x%016x) = 0x%016x, want 0x%016x", tt.bits, got, tt.want)
			}
		})
	}
}

func TestValidateChar(t *testing.T) {
	tests := []struct {
		name  string
		r     rune
		valid bool
	}{
		{"null", 0, true},
		{"ASCII A", 'A', true},
		{"ASCII z", 'z', true},
		{"space", ' ', true},
		{"newline", '\n', true},
		{"max ASCII", 0x7F, true},
		{"Latin-1", 0xFF, true},
		{"Greek alpha", 'α', true},
		{"emoji", '😀', true},
		{"max BMP", 0xFFFF, true},
		{"first surrogate", 0xD800, false},
		{"middle surrogate", 0xDB00, false},
		{"last surrogate", 0xDFFF, false},
		{"just before surrogate", 0xD7FF, true},
		{"just after surrogate", 0xE000, true},
		{"max valid codepoint", 0x10FFFF, true},
		{"first invalid codepoint", 0x110000, false},
		{"large invalid", 0x200000, false},
		{"negative (as rune)", rune(-1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateChar(tt.r)
			if got != tt.valid {
				t.Errorf("ValidateChar(0x%X) = %v, want %v", tt.r, got, tt.valid)
			}
		})
	}
}
