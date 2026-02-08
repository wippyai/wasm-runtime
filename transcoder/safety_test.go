package transcoder

import (
	"math"
	"testing"
)

func TestSafeMulU32(t *testing.T) {
	tests := []struct {
		a, b   uint32
		result uint32
		ok     bool
	}{
		{0, 0, 0, true},
		{1, 0, 0, true},
		{0, 1, 0, true},
		{1, 1, 1, true},
		{100, 100, 10000, true},
		{1 << 16, 1 << 16, 0, false}, // overflow
		{math.MaxUint32, 2, 0, false},
		{math.MaxUint32, 1, math.MaxUint32, true},
		{1 << 20, 1 << 12, 0, false}, // overflow
		{1 << 10, 1 << 10, 1 << 20, true},
	}

	for _, tc := range tests {
		result, ok := safeMulU32(tc.a, tc.b)
		if ok != tc.ok {
			t.Errorf("safeMulU32(%d, %d): got ok=%v, want %v", tc.a, tc.b, ok, tc.ok)
		}
		if ok && result != tc.result {
			t.Errorf("safeMulU32(%d, %d): got %d, want %d", tc.a, tc.b, result, tc.result)
		}
	}
}

func TestMaxAlloc(t *testing.T) {
	if MaxAlloc < MaxListLength {
		t.Errorf("MaxAlloc (%d) should be >= MaxListLength (%d)", MaxAlloc, MaxListLength)
	}
	if MaxAlloc < MaxStringSize {
		t.Errorf("MaxAlloc (%d) should be >= MaxStringSize (%d)", MaxAlloc, MaxStringSize)
	}
}

func TestTypeName_NilHandling(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		expect string
	}{
		{"nil", nil, "nil"},
		{"int", 42, "int"},
		{"string", "hello", "string"},
		{"float64", 3.14, "float64"},
		{"bool", true, "bool"},
		{"slice", []int{1, 2, 3}, "[]int"},
		{"map", map[string]any{"a": 1}, "map[string]interface {}"},
		{"nil_interface", (interface{})(nil), "nil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("typeName panicked on %s: %v", tc.name, r)
				}
			}()
			result := typeName(tc.value)
			if result != tc.expect {
				t.Errorf("typeName(%v) = %q, want %q", tc.value, result, tc.expect)
			}
		})
	}
}

func TestTypeName_NilInTypeAssertion(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("typeName panicked: %v", r)
		}
	}()

	var nilPtr *int
	var nilSlice []int
	var nilMap map[string]any
	var nilChan chan int
	var nilFunc func()

	tests := []struct {
		value any
		name  string
	}{
		{nilPtr, "nil_pointer"},
		{nilSlice, "nil_slice"},
		{nilMap, "nil_map"},
		{nilChan, "nil_chan"},
		{nilFunc, "nil_func"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := typeName(tc.value)
			if result == "" {
				t.Errorf("typeName(%s) returned empty string", tc.name)
			}
		})
	}
}
