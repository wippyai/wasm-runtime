package abi

import (
	"math"
	"testing"
)

func TestCoerceToUint32(t *testing.T) {
	tests := []struct {
		input  any
		name   string
		want   uint32
		wantOK bool
	}{
		// Direct uint32
		{uint32(0), "uint32 zero", 0, true},
		{uint32(math.MaxUint32), "uint32 max", math.MaxUint32, true},
		{uint32(12345), "uint32 mid", 12345, true},

		// float64 (JSON numbers)
		{float64(0), "float64 zero", 0, true},
		{float64(42), "float64 positive", 42, true},
		{float64(1000000), "float64 large", 1000000, true},
		{float64(math.MaxUint32), "float64 max uint32", math.MaxUint32, true},
		{float64(-1), "float64 negative", 0, false},
		{float64(math.MaxUint32 + 1), "float64 too large", 0, false},
		{float64(3.14), "float64 fractional", 0, false},

		// float32
		{float32(0), "float32 zero", 0, true},
		{float32(100), "float32 positive", 100, true},
		{float32(-1), "float32 negative", 0, false},

		// int
		{int(0), "int zero", 0, true},
		{int(1000), "int positive", 1000, true},
		{int(-1), "int negative", 0, false},

		// int64
		{int64(0), "int64 zero", 0, true},
		{int64(999), "int64 positive", 999, true},
		{int64(-5), "int64 negative", 0, false},
		{int64(math.MaxUint32 + 1), "int64 too large", 0, false},

		// uint
		{uint(0), "uint zero", 0, true},
		{uint(500), "uint positive", 500, true},

		// uint64
		{uint64(0), "uint64 zero", 0, true},
		{uint64(12345), "uint64 in range", 12345, true},
		{uint64(math.MaxUint32 + 1), "uint64 too large", 0, false},

		// int32
		{int32(0), "int32 zero", 0, true},
		{int32(777), "int32 positive", 777, true},
		{int32(-1), "int32 negative", 0, false},

		// uint8
		{uint8(0), "uint8 zero", 0, true},
		{uint8(255), "uint8 max", 255, true},

		// int8
		{int8(0), "int8 zero", 0, true},
		{int8(127), "int8 positive", 127, true},
		{int8(-1), "int8 negative", 0, false},

		// uint16
		{uint16(0), "uint16 zero", 0, true},
		{uint16(65535), "uint16 max", 65535, true},

		// int16
		{int16(0), "int16 zero", 0, true},
		{int16(32767), "int16 positive", 32767, true},
		{int16(-1), "int16 negative", 0, false},

		// Invalid types
		{"hello", "string", 0, false},
		{nil, "nil", 0, false},
		{true, "bool", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CoerceToUint32(tt.input)
			if ok != tt.wantOK {
				t.Errorf("CoerceToUint32(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("CoerceToUint32(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCoerceToInt32(t *testing.T) {
	tests := []struct {
		input  any
		name   string
		want   int32
		wantOK bool
	}{
		// Direct int32
		{int32(0), "int32 zero", 0, true},
		{int32(42), "int32 positive", 42, true},
		{int32(-100), "int32 negative", -100, true},
		{int32(math.MaxInt32), "int32 max", math.MaxInt32, true},
		{int32(math.MinInt32), "int32 min", math.MinInt32, true},

		// float64
		{float64(0), "float64 zero", 0, true},
		{float64(123), "float64 positive", 123, true},
		{float64(-456), "float64 negative", -456, true},
		{float64(math.MaxInt32 + 1), "float64 too large", 0, false},
		{float64(math.MinInt32 - 1), "float64 too small", 0, false},
		{float64(1.5), "float64 fractional", 0, false},

		// float32
		{float32(0), "float32 zero", 0, true},
		{float32(99), "float32 positive", 99, true},
		{float32(-99), "float32 negative", -99, true},

		// int
		{int(0), "int zero", 0, true},
		{int(1000), "int positive", 1000, true},
		{int(-1000), "int negative", -1000, true},

		// int64
		{int64(500), "int64 in range", 500, true},
		{int64(-500), "int64 negative", -500, true},
		{int64(math.MaxInt32 + 1), "int64 too large", 0, false},
		{int64(math.MinInt32 - 1), "int64 too small", 0, false},

		// uint32
		{uint32(1000), "uint32 in range", 1000, true},
		{uint32(math.MaxInt32), "uint32 max int32", math.MaxInt32, true},
		{uint32(math.MaxInt32 + 1), "uint32 too large", 0, false},

		// uint8
		{uint8(0), "uint8 zero", 0, true},
		{uint8(255), "uint8 max", 255, true},

		// int8
		{int8(0), "int8 zero", 0, true},
		{int8(127), "int8 positive", 127, true},
		{int8(-100), "int8 negative", -100, true},

		// uint16
		{uint16(0), "uint16 zero", 0, true},
		{uint16(65535), "uint16 max", 65535, true},

		// int16
		{int16(0), "int16 zero", 0, true},
		{int16(32767), "int16 positive", 32767, true},
		{int16(-32768), "int16 negative", -32768, true},

		// uint (needs range check)
		{uint(1000), "uint in range", 1000, true},

		// Invalid
		{"test", "string", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CoerceToInt32(tt.input)
			if ok != tt.wantOK {
				t.Errorf("CoerceToInt32(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("CoerceToInt32(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCoerceToUint64(t *testing.T) {
	tests := []struct {
		input  any
		name   string
		want   uint64
		wantOK bool
	}{
		// Direct uint64
		{uint64(0), "uint64 zero", 0, true},
		{uint64(1 << 50), "uint64 large", 1 << 50, true},

		// float64
		{float64(0), "float64 zero", 0, true},
		{float64(1000), "float64 positive", 1000, true},
		{float64(-1), "float64 negative", 0, false},

		// int
		{int(100), "int positive", 100, true},
		{int(-1), "int negative", 0, false},

		// int64
		{int64(999), "int64 positive", 999, true},
		{int64(-1), "int64 negative", 0, false},

		// uint
		{uint(12345), "uint", 12345, true},

		// uint32
		{uint32(math.MaxUint32), "uint32", math.MaxUint32, true},

		// uint8
		{uint8(0), "uint8 zero", 0, true},
		{uint8(255), "uint8 max", 255, true},

		// int8
		{int8(0), "int8 zero", 0, true},
		{int8(127), "int8 positive", 127, true},
		{int8(-1), "int8 negative", 0, false},

		// uint16
		{uint16(0), "uint16 zero", 0, true},
		{uint16(65535), "uint16 max", 65535, true},

		// int16
		{int16(0), "int16 zero", 0, true},
		{int16(32767), "int16 positive", 32767, true},
		{int16(-1), "int16 negative", 0, false},

		// int32
		{int32(1000), "int32 positive", 1000, true},
		{int32(-1), "int32 negative", 0, false},

		// float32
		{float32(100), "float32 positive", 100, true},
		{float32(-1), "float32 negative", 0, false},

		// Invalid
		{"x", "string", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CoerceToUint64(tt.input)
			if ok != tt.wantOK {
				t.Errorf("CoerceToUint64(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("CoerceToUint64(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCoerceToInt64(t *testing.T) {
	tests := []struct {
		input  any
		name   string
		want   int64
		wantOK bool
	}{
		// Direct int64
		{int64(0), "int64 zero", 0, true},
		{int64(1 << 40), "int64 positive", 1 << 40, true},
		{int64(-1 << 40), "int64 negative", -1 << 40, true},

		// float64
		{float64(0), "float64 zero", 0, true},
		{float64(1000), "float64 positive", 1000, true},
		{float64(-1000), "float64 negative", -1000, true},
		{float64(1.5), "float64 fractional", 0, false},

		// int
		{int(12345), "int", 12345, true},

		// int32
		{int32(100), "int32 positive", 100, true},
		{int32(-100), "int32 negative", -100, true},

		// uint32
		{uint32(math.MaxUint32), "uint32", math.MaxUint32, true},

		// uint64
		{uint64(1000), "uint64 in range", 1000, true},

		// uint
		{uint(12345), "uint positive", 12345, true},

		// uint8
		{uint8(0), "uint8 zero", 0, true},
		{uint8(255), "uint8 max", 255, true},

		// int8
		{int8(0), "int8 zero", 0, true},
		{int8(127), "int8 positive", 127, true},
		{int8(-100), "int8 negative", -100, true},

		// uint16
		{uint16(0), "uint16 zero", 0, true},
		{uint16(65535), "uint16 max", 65535, true},

		// int16
		{int16(0), "int16 zero", 0, true},
		{int16(32767), "int16 positive", 32767, true},
		{int16(-32768), "int16 negative", -32768, true},

		// float32
		{float32(1000), "float32 positive", 1000, true},
		{float32(-1000), "float32 negative", -1000, true},

		// Invalid
		{"y", "string", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CoerceToInt64(tt.input)
			if ok != tt.wantOK {
				t.Errorf("CoerceToInt64(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("CoerceToInt64(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
