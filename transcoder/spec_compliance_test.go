package transcoder

import (
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/wippyai/wasm-runtime/transcoder/internal/abi"
	"go.bytecodealliance.org/wit"
)

// TestNaN32Canonicalization verifies that NaN values are canonicalized per spec.
// Spec: "NaN values are canonicalized to fixed bit-patterns (0x7fc00000 for 32-bit)"
func TestNaN32Canonicalization(t *testing.T) {
	tests := []struct {
		name     string
		bits     uint32
		expected uint32
	}{
		{"positive normal", 0x3F800000, 0x3F800000}, // 1.0
		{"negative normal", 0xBF800000, 0xBF800000}, // -1.0
		{"positive zero", 0x00000000, 0x00000000},
		{"negative zero", 0x80000000, 0x80000000},
		{"positive infinity", 0x7F800000, 0x7F800000},
		{"negative infinity", 0xFF800000, 0xFF800000},
		{"canonical NaN", abi.CanonicalNaN32, abi.CanonicalNaN32},
		{"signaling NaN 1", 0x7F800001, abi.CanonicalNaN32},
		{"signaling NaN 2", 0x7FBFFFFF, abi.CanonicalNaN32},
		{"quiet NaN 1", 0x7FC00001, abi.CanonicalNaN32},
		{"quiet NaN max", 0x7FFFFFFF, abi.CanonicalNaN32},
		{"negative NaN", 0xFFC00000, abi.CanonicalNaN32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := abi.CanonicalizeF32(tt.bits)
			if result != tt.expected {
				t.Errorf("CanonicalizeF32(0x%08X) = 0x%08X, want 0x%08X", tt.bits, result, tt.expected)
			}
		})
	}
}

// TestNaN64Canonicalization verifies that 64-bit NaN values are canonicalized per spec.
// Spec: "NaN values are canonicalized to fixed bit-patterns (0x7ff8000000000000 for 64-bit)"
func TestNaN64Canonicalization(t *testing.T) {
	tests := []struct {
		name     string
		bits     uint64
		expected uint64
	}{
		{"positive normal", 0x3FF0000000000000, 0x3FF0000000000000}, // 1.0
		{"negative normal", 0xBFF0000000000000, 0xBFF0000000000000}, // -1.0
		{"positive zero", 0x0000000000000000, 0x0000000000000000},
		{"negative zero", 0x8000000000000000, 0x8000000000000000},
		{"positive infinity", 0x7FF0000000000000, 0x7FF0000000000000},
		{"negative infinity", 0xFFF0000000000000, 0xFFF0000000000000},
		{"canonical NaN", abi.CanonicalNaN64, abi.CanonicalNaN64},
		{"signaling NaN", 0x7FF0000000000001, abi.CanonicalNaN64},
		{"quiet NaN", 0x7FF8000000000001, abi.CanonicalNaN64},
		{"negative NaN", 0xFFF8000000000000, abi.CanonicalNaN64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := abi.CanonicalizeF64(tt.bits)
			if result != tt.expected {
				t.Errorf("CanonicalizeF64(0x%016X) = 0x%016X, want 0x%016X", tt.bits, result, tt.expected)
			}
		})
	}
}

// TestCharValidation verifies Unicode scalar value validation per spec.
// Spec: "rejects surrogates (0xD800-0xDFFF) and values >= 0x110000"
func TestCharValidation(t *testing.T) {
	tests := []struct {
		name  string
		r     rune
		valid bool
	}{
		{"null", 0, true},
		{"ASCII A", 'A', true},
		{"Basic Latin", 0x007F, true},
		{"Latin Extended", 0x00FF, true},
		{"CJK", 0x4E00, true},
		{"Emoji", 0x1F600, true},
		{"Max valid", 0x10FFFF, true},
		{"Surrogate start", 0xD800, false},
		{"Surrogate middle", 0xDB00, false},
		{"Surrogate end", 0xDFFF, false},
		{"Just above max", 0x110000, false},
		{"Way above max", 0x200000, false},
		{"Negative (as rune)", rune(-1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := abi.ValidateChar(tt.r)
			if result != tt.valid {
				t.Errorf("ValidateChar(0x%X) = %v, want %v", tt.r, result, tt.valid)
			}
		})
	}
}

// TestDiscriminantSize verifies discriminant sizing per spec.
// Spec: "1 byte for <= 256 cases, 2 bytes for <= 65536, else 4"
func TestDiscriminantSize(t *testing.T) {
	tests := []struct {
		numCases int
		expected uint32
	}{
		{0, 1},
		{1, 1},
		{255, 1},
		{256, 1},
		{257, 2},
		{65535, 2},
		{65536, 2},
		{65537, 4},
		{1000000, 4},
	}

	for _, tt := range tests {
		result := abi.DiscriminantSize(tt.numCases)
		if result != tt.expected {
			t.Errorf("DiscriminantSize(%d) = %d, want %d", tt.numCases, result, tt.expected)
		}
	}
}

// TestSpec_FlatCount_Result verifies flat count calculation for Result types.
// Spec: FlatCount = 1 (discriminant) + max(ok flat count, err flat count)
func TestSpec_FlatCount_Result(t *testing.T) {
	tests := []struct {
		ok       wit.Type
		err      wit.Type
		name     string
		expected int
	}{
		{nil, nil, "Result<(), ()>", 1},
		{wit.U32{}, nil, "Result<u32, ()>", 2},
		{nil, wit.String{}, "Result<(), string>", 3},
		{wit.U32{}, wit.String{}, "Result<u32, string>", 3},
		{wit.String{}, wit.U32{}, "Result<string, u32>", 3},
		{&wit.TypeDef{Kind: &wit.Tuple{Types: []wit.Type{wit.U32{}, wit.U32{}}}}, wit.U32{}, "Result<(u32, u32), u32>", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultType := &wit.TypeDef{
				Kind: &wit.Result{OK: tt.ok, Err: tt.err},
			}
			result := abi.GetFlatCount(resultType)
			if result != tt.expected {
				t.Errorf("GetFlatCount(%s) = %d, want %d", tt.name, result, tt.expected)
			}
		})
	}
}

// TestSpec_FlatCount_Variant verifies flat count calculation for Variant types.
// Spec: FlatCount = 1 (discriminant) + max(case flat counts)
func TestSpec_FlatCount_Variant(t *testing.T) {
	tests := []struct {
		name     string
		cases    []wit.Case
		expected int
	}{
		{"no cases", nil, 1},
		{"single unit case", []wit.Case{{Name: "a"}}, 1},
		{"single u32 case", []wit.Case{{Name: "a", Type: wit.U32{}}}, 2},
		{"single string case", []wit.Case{{Name: "a", Type: wit.String{}}}, 3},
		{"u32 and string", []wit.Case{{Name: "a", Type: wit.U32{}}, {Name: "b", Type: wit.String{}}}, 3},
		{"unit and u32", []wit.Case{{Name: "a"}, {Name: "b", Type: wit.U32{}}}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variantType := &wit.TypeDef{
				Kind: &wit.Variant{Cases: tt.cases},
			}
			result := abi.GetFlatCount(variantType)
			if result != tt.expected {
				t.Errorf("GetFlatCount(%s) = %d, want %d", tt.name, result, tt.expected)
			}
		})
	}
}

// TestDecoder_NaNCanonicalizes verifies decoder canonicalizes NaN on lift.
func TestDecoder_NaNCanonicalizes(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("f32_signaling_nan", func(t *testing.T) {
		addr := uint32(100)
		// Write signaling NaN
		mem.WriteU32(addr, 0x7F800001)

		result, err := dec.LoadValue(wit.F32{}, addr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		f := result.(float32)
		bits := math.Float32bits(f)
		if bits != abi.CanonicalNaN32 {
			t.Errorf("loaded NaN bits = 0x%08X, want 0x%08X", bits, abi.CanonicalNaN32)
		}
	})

	t.Run("f64_signaling_nan", func(t *testing.T) {
		addr := uint32(200)
		// Write signaling NaN
		mem.WriteU64(addr, 0x7FF0000000000001)

		result, err := dec.LoadValue(wit.F64{}, addr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		f := result.(float64)
		bits := math.Float64bits(f)
		if bits != abi.CanonicalNaN64 {
			t.Errorf("loaded NaN bits = 0x%016X, want 0x%016X", bits, abi.CanonicalNaN64)
		}
	})
}

// TestDecoder_CharValidation verifies decoder rejects invalid Unicode scalar values.
func TestDecoder_CharValidation(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("valid_char", func(t *testing.T) {
		addr := uint32(100)
		mem.WriteU32(addr, uint32('A'))

		result, err := dec.LoadValue(wit.Char{}, addr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		if result.(rune) != 'A' {
			t.Errorf("got %c, want A", result.(rune))
		}
	})

	t.Run("surrogate_rejected", func(t *testing.T) {
		addr := uint32(200)
		mem.WriteU32(addr, 0xD800)

		_, err := dec.LoadValue(wit.Char{}, addr, mem)
		if err == nil {
			t.Error("expected error for surrogate code point, got nil")
		}
	})

	t.Run("out_of_range_rejected", func(t *testing.T) {
		addr := uint32(300)
		mem.WriteU32(addr, 0x110000)

		_, err := dec.LoadValue(wit.Char{}, addr, mem)
		if err == nil {
			t.Error("expected error for out-of-range code point, got nil")
		}
	})
}

// TestCompiledType_EnumCases verifies enum case count is captured.
func TestCompiledType_EnumCases(t *testing.T) {
	enc := NewEncoder()

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "red"},
				{Name: "green"},
				{Name: "blue"},
			},
		},
	}

	type MyEnum uint8

	compiled, err := enc.compiler.Compile(enumType, reflect.TypeOf(MyEnum(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(compiled.Cases) != 3 {
		t.Errorf("len(compiled.Cases) = %d, want 3", len(compiled.Cases))
	}
}

// TestCompiledType_FlagsCases verifies flags case count is captured.
func TestCompiledType_FlagsCases(t *testing.T) {
	enc := NewEncoder()

	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: []wit.Flag{
				{Name: "read"},
				{Name: "write"},
				{Name: "execute"},
			},
		},
	}

	type MyFlags uint8

	compiled, err := enc.compiler.Compile(flagsType, reflect.TypeOf(MyFlags(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(compiled.Cases) != 3 {
		t.Errorf("len(compiled.Cases) = %d, want 3", len(compiled.Cases))
	}
}

// TestLiftFromStack_CharValidation verifies stack lift rejects invalid chars.
func TestLiftFromStack_CharValidation(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	charType := &wit.TypeDef{Kind: wit.Char{}}

	compiled, err := enc.compiler.Compile(charType, reflect.TypeOf(rune(0)))
	if err != nil {
		// Char compiles as primitive, not TypeDef
		compiled = &CompiledType{
			GoType:    reflect.TypeOf(rune(0)),
			GoSize:    4,
			WitSize:   4,
			WitAlign:  4,
			FlatCount: 1,
			Kind:      KindChar,
		}
	}
	_ = alloc

	t.Run("valid", func(t *testing.T) {
		stack := []uint64{uint64('A')}
		var r rune
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&r), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if r != 'A' {
			t.Errorf("got %c, want A", r)
		}
	})

	t.Run("surrogate_rejected", func(t *testing.T) {
		stack := []uint64{0xD800}
		var r rune
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&r), mem)
		if err == nil {
			t.Error("expected error for surrogate, got nil")
		}
	})
}

// TestEncoder_CharValidation verifies encoder rejects invalid Unicode scalar values.
func TestEncoder_CharValidation(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	compiled := &CompiledType{
		GoType:    reflect.TypeOf(rune(0)),
		GoSize:    4,
		WitSize:   4,
		WitAlign:  4,
		FlatCount: 1,
		Kind:      KindChar,
	}

	t.Run("valid_lower", func(t *testing.T) {
		r := rune('A')
		stack := make([]uint64, 1)
		_, err := enc.LowerToStack(compiled, unsafe.Pointer(&r), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}
		if stack[0] != uint64('A') {
			t.Errorf("got %d, want %d", stack[0], uint64('A'))
		}
	})

	t.Run("surrogate_rejected", func(t *testing.T) {
		r := rune(0xD800)
		stack := make([]uint64, 1)
		_, err := enc.LowerToStack(compiled, unsafe.Pointer(&r), stack, mem, alloc)
		if err == nil {
			t.Error("expected error for surrogate, got nil")
		}
	})

	t.Run("out_of_range_rejected", func(t *testing.T) {
		r := rune(0x110000)
		stack := make([]uint64, 1)
		_, err := enc.LowerToStack(compiled, unsafe.Pointer(&r), stack, mem, alloc)
		if err == nil {
			t.Error("expected error for out-of-range, got nil")
		}
	})
}

// TestEncoder_InvalidUTF8Rejected verifies encoder rejects invalid UTF-8 strings.
func TestEncoder_InvalidUTF8Rejected(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	compiled := &CompiledType{
		GoType:    reflect.TypeOf(""),
		GoSize:    16,
		WitSize:   8,
		WitAlign:  4,
		FlatCount: 2,
		Kind:      KindString,
	}

	// Invalid UTF-8 byte sequence
	invalidUTF8 := string([]byte{0xff, 0xfe, 0xfd})
	stack := make([]uint64, 2)
	_, err := enc.LowerToStack(compiled, unsafe.Pointer(&invalidUTF8), stack, mem, alloc)
	if err == nil {
		t.Error("expected error for invalid UTF-8, got nil")
	}
}

// TestDecoder_EnumDiscriminantBounds verifies decoder rejects out-of-bounds enum discriminants.
func TestDecoder_EnumDiscriminantBounds(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	// 3-case enum
	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "red"},
				{Name: "green"},
				{Name: "blue"},
			},
		},
	}

	type MyEnum uint8

	compiled, err := dec.compiler.Compile(enumType, reflect.TypeOf(MyEnum(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	t.Run("valid_discriminant", func(t *testing.T) {
		addr := uint32(100)
		mem.WriteU8(addr, 2) // blue = 2, valid

		var result MyEnum
		err := dec.decodeFieldFromMemory(addr, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decodeFieldFromMemory failed: %v", err)
		}
		if result != 2 {
			t.Errorf("got %d, want 2", result)
		}
	})

	t.Run("out_of_bounds_discriminant", func(t *testing.T) {
		addr := uint32(200)
		mem.WriteU8(addr, 5) // Invalid - only 0, 1, 2 are valid

		var result MyEnum
		err := dec.decodeFieldFromMemory(addr, compiled, unsafe.Pointer(&result), mem, nil)
		if err == nil {
			t.Error("expected error for out-of-bounds discriminant, got nil")
		}
	})
}

// TestOverflowProtection verifies overflow checks work correctly.
func TestOverflowProtection(t *testing.T) {
	t.Run("safeMulU32", func(t *testing.T) {
		tests := []struct {
			a, b     uint32
			expected uint32
			ok       bool
		}{
			{0, 0, 0, true},
			{1, 1, 1, true},
			{100, 100, 10000, true},
			{0xFFFFFFFF, 2, 0, false},          // Overflow
			{0x80000000, 2, 0, false},          // Overflow
			{0x10000, 0x10000, 0, false},       // Overflow (65536 * 65536)
			{0xFFFF, 0xFFFF, 0xFFFE0001, true}, // Max safe
		}

		for _, tt := range tests {
			result, ok := abi.SafeMulU32(tt.a, tt.b)
			if ok != tt.ok {
				t.Errorf("SafeMulU32(%d, %d) ok = %v, want %v", tt.a, tt.b, ok, tt.ok)
			}
			if ok && result != tt.expected {
				t.Errorf("SafeMulU32(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		}
	})
}

// TestFloatListNaNCanonicalizes verifies that NaN values in float lists are canonicalized.
func TestFloatListNaNCanonicalizes(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("f32_list_nan", func(t *testing.T) {
		// Write a list of 3 F32 values: [1.0, signaling NaN, 2.0]
		dataAddr := uint32(100)
		mem.WriteU32(dataAddr, 0x3F800000)   // 1.0
		mem.WriteU32(dataAddr+4, 0x7F800001) // signaling NaN
		mem.WriteU32(dataAddr+8, 0x40000000) // 2.0

		// Write list header at addr 200
		listAddr := uint32(200)
		mem.WriteU32(listAddr, dataAddr) // data pointer
		mem.WriteU32(listAddr+4, 3)      // length

		listType := &wit.TypeDef{
			Kind: &wit.List{Type: wit.F32{}},
		}

		result, err := dec.LoadValue(listType, listAddr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		floats := result.([]any)
		if len(floats) != 3 {
			t.Fatalf("got %d elements, want 3", len(floats))
		}

		// Check that second element (NaN) is canonicalized
		f1 := floats[1].(float32)
		bits := math.Float32bits(f1)
		if bits != abi.CanonicalNaN32 {
			t.Errorf("NaN bits = 0x%08X, want 0x%08X (canonical)", bits, abi.CanonicalNaN32)
		}
	})

	t.Run("f64_list_nan", func(t *testing.T) {
		// Write a list of 2 F64 values: [1.0, signaling NaN]
		dataAddr := uint32(300)
		mem.WriteU64(dataAddr, 0x3FF0000000000000)   // 1.0
		mem.WriteU64(dataAddr+8, 0x7FF0000000000001) // signaling NaN

		// Write list header at addr 400
		listAddr := uint32(400)
		mem.WriteU32(listAddr, dataAddr) // data pointer
		mem.WriteU32(listAddr+4, 2)      // length

		listType := &wit.TypeDef{
			Kind: &wit.List{Type: wit.F64{}},
		}

		result, err := dec.LoadValue(listType, listAddr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		floats := result.([]any)
		if len(floats) != 2 {
			t.Fatalf("got %d elements, want 2", len(floats))
		}

		// Check that second element (NaN) is canonicalized
		f1 := floats[1].(float64)
		bits := math.Float64bits(f1)
		if bits != abi.CanonicalNaN64 {
			t.Errorf("NaN bits = 0x%016X, want 0x%016X (canonical)", bits, abi.CanonicalNaN64)
		}
	})
}

// TestLiftFloatListNaN verifies NaN canonicalization in liftList path.
func TestLiftFloatListNaN(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("lift_f32_list", func(t *testing.T) {
		// Write F32 values with NaN
		dataAddr := uint32(100)
		mem.WriteU32(dataAddr, 0x7FBFFFFF)   // signaling NaN
		mem.WriteU32(dataAddr+4, 0x7FC00001) // quiet NaN with payload

		listType := &wit.TypeDef{
			Kind: &wit.List{Type: wit.F32{}},
		}

		flat := []uint64{uint64(dataAddr), 2}
		result, consumed, err := dec.liftTypeDef(listType, flat, mem, nil)
		if err != nil {
			t.Fatalf("liftTypeDef failed: %v", err)
		}
		if consumed != 2 {
			t.Errorf("consumed = %d, want 2", consumed)
		}

		// Both NaN values should be canonicalized
		floats := result.([]float32)
		for i, f := range floats {
			bits := math.Float32bits(f)
			if bits != abi.CanonicalNaN32 {
				t.Errorf("floats[%d] NaN bits = 0x%08X, want 0x%08X", i, bits, abi.CanonicalNaN32)
			}
		}
	})

	t.Run("lift_f64_list", func(t *testing.T) {
		// Write F64 values with NaN
		dataAddr := uint32(200)
		mem.WriteU64(dataAddr, 0x7FF8000000000001)   // quiet NaN with payload
		mem.WriteU64(dataAddr+8, 0xFFF8000000000000) // negative NaN

		listType := &wit.TypeDef{
			Kind: &wit.List{Type: wit.F64{}},
		}

		flat := []uint64{uint64(dataAddr), 2}
		result, consumed, err := dec.liftTypeDef(listType, flat, mem, nil)
		if err != nil {
			t.Fatalf("liftTypeDef failed: %v", err)
		}
		if consumed != 2 {
			t.Errorf("consumed = %d, want 2", consumed)
		}

		// Both NaN values should be canonicalized
		floats := result.([]float64)
		for i, f := range floats {
			bits := math.Float64bits(f)
			if bits != abi.CanonicalNaN64 {
				t.Errorf("floats[%d] NaN bits = 0x%016X, want 0x%016X", i, bits, abi.CanonicalNaN64)
			}
		}
	})
}

// TestLiftStringFromStack_UTF8Validation verifies UTF-8 validation in stack string lifting.
func TestLiftStringFromStack_UTF8Validation(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	compiled := &CompiledType{
		GoType:    reflect.TypeOf(""),
		GoSize:    16,
		WitSize:   8,
		WitAlign:  4,
		FlatCount: 2,
		Kind:      KindString,
	}

	t.Run("valid_utf8", func(t *testing.T) {
		// Write valid UTF-8 string "hello"
		dataAddr := uint32(100)
		mem.Write(dataAddr, []byte("hello"))

		stack := []uint64{uint64(dataAddr), 5}
		var result string
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if result != "hello" {
			t.Errorf("got %q, want %q", result, "hello")
		}
	})

	t.Run("invalid_utf8_rejected", func(t *testing.T) {
		// Write invalid UTF-8 bytes
		dataAddr := uint32(200)
		mem.Write(dataAddr, []byte{0xff, 0xfe, 0xfd})

		stack := []uint64{uint64(dataAddr), 3}
		var result string
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err == nil {
			t.Error("expected error for invalid UTF-8, got nil")
		}
	})

	t.Run("empty_string_valid", func(t *testing.T) {
		stack := []uint64{0, 0}
		var result string
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if result != "" {
			t.Errorf("got %q, want empty string", result)
		}
	})
}

// TestStackBoundsChecking verifies stack bounds are validated.
func TestStackBoundsChecking(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("string_insufficient_stack", func(t *testing.T) {
		compiled := &CompiledType{
			GoType:    reflect.TypeOf(""),
			GoSize:    16,
			WitSize:   8,
			WitAlign:  4,
			FlatCount: 2,
			Kind:      KindString,
		}

		// Only 1 value when 2 are needed
		stack := []uint64{100}
		var result string
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err == nil {
			t.Error("expected error for insufficient stack, got nil")
		}
	})

	t.Run("list_insufficient_stack", func(t *testing.T) {
		compiled := &CompiledType{
			GoType:    reflect.TypeOf([]uint32{}),
			GoSize:    24,
			WitSize:   8,
			WitAlign:  4,
			FlatCount: 2,
			Kind:      KindList,
			ElemType: &CompiledType{
				GoType:    reflect.TypeOf(uint32(0)),
				GoSize:    4,
				WitSize:   4,
				WitAlign:  4,
				FlatCount: 1,
				Kind:      KindU32,
			},
		}

		// Only 1 value when 2 are needed
		stack := []uint64{100}
		var result []uint32
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err == nil {
			t.Error("expected error for insufficient stack, got nil")
		}
	})
}

// TestStringListUTF8Validation verifies UTF-8 validation in string list lifting.
func TestStringListUTF8Validation(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	compiled := &CompiledType{
		GoType:    reflect.TypeOf([]string{}),
		GoSize:    24,
		WitSize:   8,
		WitAlign:  4,
		FlatCount: 2,
		Kind:      KindList,
		ElemType: &CompiledType{
			GoType:    reflect.TypeOf(""),
			GoSize:    16,
			WitSize:   8,
			WitAlign:  4,
			FlatCount: 2,
			Kind:      KindString,
		},
	}

	t.Run("valid_strings", func(t *testing.T) {
		// Write string data
		str1Addr := uint32(100)
		mem.Write(str1Addr, []byte("hello"))
		str2Addr := uint32(110)
		mem.Write(str2Addr, []byte("world"))

		// Write metadata (addr + len for each string)
		metaAddr := uint32(200)
		mem.WriteU32(metaAddr, str1Addr)
		mem.WriteU32(metaAddr+4, 5)
		mem.WriteU32(metaAddr+8, str2Addr)
		mem.WriteU32(metaAddr+12, 5)

		stack := []uint64{uint64(metaAddr), 2}
		var result []string
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("got %d elements, want 2", len(result))
		}
		if result[0] != "hello" || result[1] != "world" {
			t.Errorf("got %v, want [hello, world]", result)
		}
	})

	t.Run("invalid_utf8_rejected", func(t *testing.T) {
		// Write invalid UTF-8 string data
		str1Addr := uint32(300)
		mem.Write(str1Addr, []byte{0xff, 0xfe, 0xfd})

		// Write metadata
		metaAddr := uint32(400)
		mem.WriteU32(metaAddr, str1Addr)
		mem.WriteU32(metaAddr+4, 3)

		stack := []uint64{uint64(metaAddr), 1}
		var result []string
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err == nil {
			t.Error("expected error for invalid UTF-8, got nil")
		}
	})
}

// TestDecodeIntoStringUTF8Validation tests that DecodeInto rejects invalid UTF-8
func TestDecodeIntoStringUTF8Validation(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	// Write invalid UTF-8 data
	invalidUTF8 := []byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb}
	dataAddr := uint32(100)
	mem.Write(dataAddr, invalidUTF8)

	// Flat representation: [dataAddr, length]
	flat := []uint64{uint64(dataAddr), uint64(len(invalidUTF8))}

	var result string
	witTypes := []wit.Type{wit.String{}}
	err := dec.DecodeInto(witTypes, flat, mem, &result)
	if err == nil {
		t.Error("expected error for invalid UTF-8 in DecodeInto, got nil")
	}
}

// TestEncodeCharValidation_EncodeParams tests that EncodeParams rejects invalid Unicode scalar values
func TestEncodeCharValidation_EncodeParams(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	tests := []struct {
		name  string
		char  rune
		valid bool
	}{
		{"valid_ascii", 'A', true},
		{"valid_unicode", '\u4e2d', true},
		{"valid_emoji", '\U0001F600', true},
		{"surrogate_low", rune(0xD800), false},
		{"surrogate_high", rune(0xDFFF), false},
		{"too_large", rune(0x110000), false},
	}

	witTypes := []wit.Type{wit.Char{}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.EncodeParams(witTypes, []any{tt.char}, mem, alloc, nil)
			if tt.valid && err != nil {
				t.Errorf("expected no error for valid char 0x%X, got: %v", tt.char, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected error for invalid char 0x%X, got nil", tt.char)
			}
		})
	}
}

// TestEncoder_NaNCanonicalizes tests that encoder canonicalizes NaN values per spec
func TestEncoder_NaNCanonicalizes(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	t.Run("f32_nan", func(t *testing.T) {
		// Signaling NaN that should be canonicalized to 0x7fc00000
		signalingNaN := math.Float32frombits(0x7f800001)
		witTypes := []wit.Type{wit.F32{}}
		flat, err := enc.EncodeParams(witTypes, []any{signalingNaN}, mem, alloc, nil)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}
		if len(flat) != 1 {
			t.Fatalf("expected 1 flat value, got %d", len(flat))
		}
		if uint32(flat[0]) != abi.CanonicalNaN32 {
			t.Errorf("F32 NaN not canonicalized: got 0x%08X, want 0x%08X", uint32(flat[0]), abi.CanonicalNaN32)
		}
	})

	t.Run("f64_nan", func(t *testing.T) {
		// Signaling NaN that should be canonicalized to 0x7ff8000000000000
		signalingNaN := math.Float64frombits(0x7ff0000000000001)
		witTypes := []wit.Type{wit.F64{}}
		flat, err := enc.EncodeParams(witTypes, []any{signalingNaN}, mem, alloc, nil)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}
		if len(flat) != 1 {
			t.Fatalf("expected 1 flat value, got %d", len(flat))
		}
		if flat[0] != abi.CanonicalNaN64 {
			t.Errorf("F64 NaN not canonicalized: got 0x%016X, want 0x%016X", flat[0], abi.CanonicalNaN64)
		}
	})
}

// TestEncoderLowerToStack_NaN tests that LowerToStack canonicalizes NaN values
func TestEncoderLowerToStack_NaN(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	t.Run("f32", func(t *testing.T) {
		compiled := &CompiledType{
			GoType:    reflect.TypeOf(float32(0)),
			GoSize:    4,
			WitSize:   4,
			WitAlign:  4,
			FlatCount: 1,
			Kind:      KindF32,
		}
		signalingNaN := math.Float32frombits(0x7fbfffff)
		stack := make([]uint64, 1)
		_, err := enc.LowerToStack(compiled, unsafe.Pointer(&signalingNaN), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}
		if uint32(stack[0]) != abi.CanonicalNaN32 {
			t.Errorf("F32 NaN not canonicalized: got 0x%08X, want 0x%08X", uint32(stack[0]), abi.CanonicalNaN32)
		}
	})

	t.Run("f64", func(t *testing.T) {
		compiled := &CompiledType{
			GoType:    reflect.TypeOf(float64(0)),
			GoSize:    8,
			WitSize:   8,
			WitAlign:  8,
			FlatCount: 1,
			Kind:      KindF64,
		}
		signalingNaN := math.Float64frombits(0x7ff7ffffffffffff)
		stack := make([]uint64, 1)
		_, err := enc.LowerToStack(compiled, unsafe.Pointer(&signalingNaN), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}
		if stack[0] != abi.CanonicalNaN64 {
			t.Errorf("F64 NaN not canonicalized: got 0x%016X, want 0x%016X", stack[0], abi.CanonicalNaN64)
		}
	})
}

// TestEncoderEnumBoundsCheck tests that encoder validates enum discriminant bounds
func TestEncoderEnumBoundsCheck(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "a"},
				{Name: "b"},
				{Name: "c"},
			},
		},
	}

	type MyEnum uint8

	compiled, err := enc.compiler.Compile(enumType, reflect.TypeOf(MyEnum(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	t.Run("valid_discriminant", func(t *testing.T) {
		val := MyEnum(2) // last valid case
		_, err := enc.LowerToStack(compiled, unsafe.Pointer(&val), make([]uint64, 1), mem, alloc)
		if err != nil {
			t.Errorf("unexpected error for valid discriminant: %v", err)
		}
	})

	t.Run("invalid_discriminant", func(t *testing.T) {
		val := MyEnum(5) // out of bounds
		_, err := enc.LowerToStack(compiled, unsafe.Pointer(&val), make([]uint64, 1), mem, alloc)
		if err == nil {
			t.Error("expected error for invalid discriminant, got nil")
		}
	})
}

// TestDecodeInto_NaNCanonicalizes tests that DecodeInto canonicalizes NaN values
func TestDecodeInto_NaNCanonicalizes(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("f32", func(t *testing.T) {
		// Signaling NaN that should be canonicalized
		flat := []uint64{uint64(0x7f800001)}
		var result float32
		err := dec.DecodeInto([]wit.Type{wit.F32{}}, flat, mem, &result)
		if err != nil {
			t.Fatalf("DecodeInto failed: %v", err)
		}
		bits := math.Float32bits(result)
		if bits != abi.CanonicalNaN32 {
			t.Errorf("F32 NaN not canonicalized: got 0x%08X, want 0x%08X", bits, abi.CanonicalNaN32)
		}
	})

	t.Run("f64", func(t *testing.T) {
		// Signaling NaN that should be canonicalized
		flat := []uint64{0x7ff0000000000001}
		var result float64
		err := dec.DecodeInto([]wit.Type{wit.F64{}}, flat, mem, &result)
		if err != nil {
			t.Fatalf("DecodeInto failed: %v", err)
		}
		bits := math.Float64bits(result)
		if bits != abi.CanonicalNaN64 {
			t.Errorf("F64 NaN not canonicalized: got 0x%016X, want 0x%016X", bits, abi.CanonicalNaN64)
		}
	})
}

// TestDecodeInto_CharValidation tests that DecodeInto validates Char values
func TestDecodeInto_CharValidation(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("valid_char", func(t *testing.T) {
		flat := []uint64{uint64('A')}
		var result rune
		err := dec.DecodeInto([]wit.Type{wit.Char{}}, flat, mem, &result)
		if err != nil {
			t.Fatalf("DecodeInto failed: %v", err)
		}
		if result != 'A' {
			t.Errorf("got %c, want A", result)
		}
	})

	t.Run("surrogate_rejected", func(t *testing.T) {
		flat := []uint64{0xD800} // surrogate
		var result rune
		err := dec.DecodeInto([]wit.Type{wit.Char{}}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for surrogate, got nil")
		}
	})

	t.Run("out_of_range_rejected", func(t *testing.T) {
		flat := []uint64{0x110000} // beyond Unicode range
		var result rune
		err := dec.DecodeInto([]wit.Type{wit.Char{}}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for out-of-range char, got nil")
		}
	})
}

// TestDecodeInto_EnumBoundsCheck tests that DecodeInto validates enum discriminant bounds
func TestDecodeInto_EnumBoundsCheck(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "a"},
				{Name: "b"},
				{Name: "c"},
			},
		},
	}

	t.Run("valid_discriminant", func(t *testing.T) {
		flat := []uint64{2} // last valid case
		var result uint32
		err := dec.DecodeInto([]wit.Type{enumType}, flat, mem, &result)
		if err != nil {
			t.Fatalf("DecodeInto failed: %v", err)
		}
		if result != 2 {
			t.Errorf("got %d, want 2", result)
		}
	})

	t.Run("invalid_discriminant", func(t *testing.T) {
		flat := []uint64{5} // out of bounds
		var result uint32
		err := dec.DecodeInto([]wit.Type{enumType}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for invalid discriminant, got nil")
		}
	})

	t.Run("large_discriminant", func(t *testing.T) {
		flat := []uint64{^uint64(0)} // max uint64, would be negative if cast to int
		var result uint32
		err := dec.DecodeInto([]wit.Type{enumType}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for very large discriminant, got nil")
		}
	})
}

// TestVariantLargeDiscriminant tests that variant decoding handles large discriminant values safely
func TestVariantLargeDiscriminant(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "a", Type: nil},
				{Name: "b", Type: nil},
			},
		},
	}

	t.Run("large_discriminant_rejected", func(t *testing.T) {
		// Value that would be negative when cast to int
		flat := []uint64{^uint64(0)}
		var result map[string]any
		err := dec.DecodeInto([]wit.Type{variantType}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for very large discriminant, got nil")
		}
	})
}

// TestDecodeInto_FlatArrayBoundsChecking tests that DecodeInto validates flat array bounds
func TestDecodeInto_FlatArrayBoundsChecking(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	t.Run("string_insufficient_flat", func(t *testing.T) {
		flat := []uint64{0} // Only 1 value, string needs 2
		var result string
		err := dec.DecodeInto([]wit.Type{wit.String{}}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for insufficient flat values for string, got nil")
		}
	})

	t.Run("list_insufficient_flat", func(t *testing.T) {
		listType := &wit.TypeDef{
			Kind: &wit.List{Type: wit.U32{}},
		}
		flat := []uint64{0} // Only 1 value, list needs 2
		var result []any
		err := dec.DecodeInto([]wit.Type{listType}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for insufficient flat values for list, got nil")
		}
	})

	t.Run("option_insufficient_flat", func(t *testing.T) {
		optionType := &wit.TypeDef{
			Kind: &wit.Option{Type: wit.U32{}},
		}
		flat := []uint64{} // Empty, option needs at least 1
		var result any
		err := dec.DecodeInto([]wit.Type{optionType}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for insufficient flat values for option, got nil")
		}
	})

	t.Run("result_insufficient_flat", func(t *testing.T) {
		resultType := &wit.TypeDef{
			Kind: &wit.Result{OK: wit.U32{}, Err: nil},
		}
		flat := []uint64{} // Empty, result needs at least 1
		var result map[string]any
		err := dec.DecodeInto([]wit.Type{resultType}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for insufficient flat values for result, got nil")
		}
	})

	t.Run("variant_insufficient_flat", func(t *testing.T) {
		variantType := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "a", Type: nil},
				},
			},
		}
		flat := []uint64{} // Empty, variant needs at least 1
		var result map[string]any
		err := dec.DecodeInto([]wit.Type{variantType}, flat, mem, &result)
		if err == nil {
			t.Error("expected error for insufficient flat values for variant, got nil")
		}
	})
}

// TestLiftVariantFromStack_InvalidDiscriminant tests that liftVariantFromStack returns error for invalid discriminant
func TestLiftVariantFromStack_InvalidDiscriminant(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	// Create a variant type with correct Go types
	type MyVariant struct {
		A *int32
		B *string
	}

	variantDef := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "a", Type: wit.S32{}},
				{Name: "b", Type: wit.String{}},
			},
		},
	}

	compiled, err := enc.compiler.Compile(variantDef, reflect.TypeOf(MyVariant{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	_ = alloc

	t.Run("invalid_discriminant", func(t *testing.T) {
		// Stack with invalid discriminant (5 for a 2-case variant)
		stack := []uint64{5, 0, 0}
		var result MyVariant
		_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
		if err == nil {
			t.Error("expected error for invalid discriminant, got nil")
		}
	})
}

// TestLiftValue_FlatArrayBoundsCheck tests that liftValue checks bounds before accessing flat array
func TestLiftValue_FlatArrayBoundsCheck(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	tests := []struct {
		witType wit.Type
		name    string
	}{
		{wit.Bool{}, "Bool"},
		{wit.U8{}, "U8"},
		{wit.S8{}, "S8"},
		{wit.U16{}, "U16"},
		{wit.S16{}, "S16"},
		{wit.U32{}, "U32"},
		{wit.S32{}, "S32"},
		{wit.U64{}, "U64"},
		{wit.S64{}, "S64"},
		{wit.F32{}, "F32"},
		{wit.F64{}, "F64"},
		{wit.Char{}, "Char"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emptyFlat := []uint64{}
			_, _, err := dec.liftValue(tt.witType, emptyFlat, mem, nil)
			if err == nil {
				t.Errorf("expected error for empty flat array on %s, got nil", tt.name)
			}
		})
	}
}

// TestLiftEnum_FlatArrayBoundsCheck tests that liftEnum checks bounds
func TestLiftEnum_FlatArrayBoundsCheck(t *testing.T) {
	dec := NewDecoder()

	enumDef := &wit.Enum{
		Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}},
	}

	emptyFlat := []uint64{}
	_, _, err := dec.liftEnum(enumDef, emptyFlat, nil)
	if err == nil {
		t.Error("expected error for empty flat array on enum, got nil")
	}
}

// TestLiftFlags_FlatArrayBoundsCheck tests that liftFlags checks bounds
func TestLiftFlags_FlatArrayBoundsCheck(t *testing.T) {
	dec := NewDecoder()

	flagsDef := &wit.Flags{
		Flags: []wit.Flag{{Name: "a"}, {Name: "b"}},
	}

	emptyFlat := []uint64{}
	_, _, err := dec.liftFlags(flagsDef, emptyFlat, nil)
	if err == nil {
		t.Error("expected error for empty flat array on flags, got nil")
	}
}

// TestLiftResult_InvalidDiscriminant tests that Result discriminant must be 0 or 1
func TestLiftResult_InvalidDiscriminant(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	resultDef := &wit.Result{
		OK:  wit.U32{},
		Err: wit.String{},
	}

	tests := []struct {
		name string
		disc uint64
		ok   bool
	}{
		{"disc_0_ok", 0, true},
		{"disc_1_err", 1, true},
		{"disc_2_invalid", 2, false},
		{"disc_255_invalid", 255, false},
		{"disc_max_invalid", 0xFFFFFFFFFFFFFFFF, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flat := []uint64{tt.disc, 0, 0}
			_, _, err := dec.liftResult(resultDef, flat, mem, nil)
			if tt.ok && err != nil {
				t.Errorf("expected success for disc=%d, got error: %v", tt.disc, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("expected error for invalid disc=%d, got nil", tt.disc)
			}
		})
	}
}

// TestDecodeValueIntoWithCount_BoundsCheck tests offset bounds checking
func TestDecodeValueIntoWithCount_BoundsCheck(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	var result uint32
	ptr := unsafe.Pointer(&result)

	// Test with offset beyond flat array
	flat := []uint64{1, 2, 3}
	_, err := dec.decodeValueIntoWithCount(wit.U32{}, flat, 10, mem, ptr)
	if err == nil {
		t.Error("expected error for offset beyond array bounds, got nil")
	}

	// Test with empty flat array
	emptyFlat := []uint64{}
	_, err = dec.decodeValueIntoWithCount(wit.U32{}, emptyFlat, 0, mem, ptr)
	if err == nil {
		t.Error("expected error for empty flat array, got nil")
	}
}

// TestStoreValue_UTF8Validation tests that storeValue validates UTF-8 for strings
func TestStoreValue_UTF8Validation(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(1024)
	alloc := newMockAllocator(mem)

	// Create invalid UTF-8 string using unsafe
	invalidBytes := []byte{0xFF, 0xFE, 0x00}
	invalidStr := unsafe.String(&invalidBytes[0], len(invalidBytes))

	err := enc.storeValue(wit.String{}, invalidStr, 0, mem, alloc, nil, nil)
	if err == nil {
		t.Error("expected error for invalid UTF-8 string, got nil")
	}
}

// TestDecodeResultFromMemory_InvalidDiscriminant tests Result discriminant must be 0 or 1
func TestDecodeResultFromMemory_InvalidDiscriminant(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	// Write invalid discriminant (2) to memory
	mem.WriteU8(0, 2)

	resultDef := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	compiled, err := dec.compiler.Compile(resultDef, reflect.TypeOf(struct {
		Ok  *uint32
		Err *string
	}{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	var result struct {
		Ok  *uint32
		Err *string
	}
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
	if err == nil {
		t.Error("expected error for invalid Result discriminant, got nil")
	}
}

// TestLiftResult_DiscriminantMustBe0Or1 tests Result discriminant in flat representation
func TestLiftResult_DiscriminantMustBe0Or1(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	resultDef := &wit.Result{
		OK:  wit.U32{},
		Err: wit.String{},
	}

	tests := []struct {
		name string
		disc uint64
		ok   bool
	}{
		{"disc_0_valid", 0, true},
		{"disc_1_valid", 1, true},
		{"disc_2_invalid", 2, false},
		{"disc_100_invalid", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Need extra values for payload
			flat := []uint64{tt.disc, 0, 0, 0}
			_, _, err := dec.liftResult(resultDef, flat, mem, nil)
			if tt.ok && err != nil {
				t.Errorf("expected success for disc=%d, got error: %v", tt.disc, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("expected error for invalid disc=%d, got nil", tt.disc)
			}
		})
	}
}

// TestLiftOption_DiscriminantMustBe0Or1 tests Option discriminant validation
func TestLiftOption_DiscriminantMustBe0Or1(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	optionDef := &wit.Option{
		Type: wit.U32{},
	}

	tests := []struct {
		name string
		disc uint64
		ok   bool
	}{
		{"disc_0_none", 0, true},
		{"disc_1_some", 1, true},
		{"disc_2_invalid", 2, false},
		{"disc_255_invalid", 255, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flat := []uint64{tt.disc, 42}
			_, _, err := dec.liftOption(optionDef, flat, mem, nil)
			if tt.ok && err != nil {
				t.Errorf("expected success for disc=%d, got error: %v", tt.disc, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("expected error for invalid disc=%d, got nil", tt.disc)
			}
		})
	}
}

// TestLiftOptionFromStack_DiscriminantValidation tests stack-based option discriminant
func TestLiftOptionFromStack_DiscriminantValidation(t *testing.T) {
	dec := NewDecoder()
	enc := NewEncoder()
	mem := newMockMemory(1024)

	optionDef := &wit.TypeDef{
		Kind: &wit.Option{
			Type: wit.U32{},
		},
	}

	compiled, err := enc.compiler.Compile(optionDef, reflect.TypeOf((*uint32)(nil)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	tests := []struct {
		name string
		disc uint64
		ok   bool
	}{
		{"disc_0_none", 0, true},
		{"disc_1_some", 1, true},
		{"disc_2_invalid", 2, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := []uint64{tt.disc, 42}
			var result *uint32
			_, err := dec.LiftFromStack(compiled, stack, unsafe.Pointer(&result), mem)
			if tt.ok && err != nil {
				t.Errorf("expected success for disc=%d, got error: %v", tt.disc, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("expected error for invalid disc=%d, got nil", tt.disc)
			}
		})
	}
}

// TestCompileFlags_Max64 tests that flags types with >64 flags are rejected
func TestCompileFlags_Max64(t *testing.T) {
	enc := NewEncoder()

	t.Run("64_flags_ok", func(t *testing.T) {
		flags := make([]wit.Flag, 64)
		for i := range flags {
			flags[i] = wit.Flag{Name: "f" + string(rune('a'+i%26)) + string(rune('0'+i/26))}
		}
		flagsDef := &wit.TypeDef{
			Kind: &wit.Flags{Flags: flags},
		}
		_, err := enc.compiler.Compile(flagsDef, reflect.TypeOf(uint64(0)))
		if err != nil {
			t.Errorf("expected 64 flags to compile, got error: %v", err)
		}
	})

	t.Run("65_flags_rejected", func(t *testing.T) {
		flags := make([]wit.Flag, 65)
		for i := range flags {
			flags[i] = wit.Flag{Name: "f" + string(rune('a'+i%26)) + string(rune('0'+i/26))}
		}
		flagsDef := &wit.TypeDef{
			Kind: &wit.Flags{Flags: flags},
		}
		_, err := enc.compiler.Compile(flagsDef, reflect.TypeOf(uint64(0)))
		if err == nil {
			t.Error("expected 65 flags to be rejected, got nil error")
		}
	})
}
