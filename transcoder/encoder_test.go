package transcoder

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestGetFlatCount(t *testing.T) {
	tests := []struct {
		witType  wit.Type
		name     string
		expected int
	}{
		{wit.Bool{}, "bool", 1},
		{wit.U8{}, "u8", 1},
		{wit.S8{}, "s8", 1},
		{wit.U16{}, "u16", 1},
		{wit.S16{}, "s16", 1},
		{wit.U32{}, "u32", 1},
		{wit.S32{}, "s32", 1},
		{wit.U64{}, "u64", 1},
		{wit.S64{}, "s64", 1},
		{wit.F32{}, "f32", 1},
		{wit.F64{}, "f64", 1},
		{wit.Char{}, "char", 1},
		{wit.String{}, "string", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetFlatCount(tt.witType)
			if result != tt.expected {
				t.Errorf("GetFlatCount(%s) = %d, want %d", tt.name, result, tt.expected)
			}
		})
	}
}

func TestGetFlatCount_Record(t *testing.T) {
	record := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.U32{}},
				{Name: "c", Type: wit.String{}},
			},
		},
	}

	// 1 + 1 + 2 = 4
	result := GetFlatCount(record)
	if result != 4 {
		t.Errorf("GetFlatCount(record) = %d, want 4", result)
	}
}

func TestGetFlatCount_List(t *testing.T) {
	list := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U8{}},
	}

	// Lists flatten to 2 (ptr, len)
	result := GetFlatCount(list)
	if result != 2 {
		t.Errorf("GetFlatCount(list) = %d, want 2", result)
	}
}

func TestGetFlatCount_Option(t *testing.T) {
	option := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	// 1 discriminant + 1 payload = 2
	result := GetFlatCount(option)
	if result != 2 {
		t.Errorf("GetFlatCount(option<u32>) = %d, want 2", result)
	}
}

func TestGetFlatCount_Tuple(t *testing.T) {
	tuple := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U32{}, wit.U64{}, wit.String{}}},
	}

	// 1 + 1 + 2 = 4
	result := GetFlatCount(tuple)
	if result != 4 {
		t.Errorf("GetFlatCount(tuple) = %d, want 4", result)
	}
}

func TestGetFlatCount_Enum(t *testing.T) {
	enum := &wit.TypeDef{
		Kind: &wit.Enum{Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}}},
	}

	result := GetFlatCount(enum)
	if result != 1 {
		t.Errorf("GetFlatCount(enum) = %d, want 1", result)
	}
}

func TestGetFlatCount_Flags(t *testing.T) {
	flags := &wit.TypeDef{
		Kind: &wit.Flags{Flags: []wit.Flag{{Name: "a"}, {Name: "b"}}},
	}

	result := GetFlatCount(flags)
	if result != 1 {
		t.Errorf("GetFlatCount(flags) = %d, want 1", result)
	}
}

func TestGetFlatCount_Result(t *testing.T) {
	result := &wit.TypeDef{
		Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}},
	}

	// Results flatten to 3
	flatCount := GetFlatCount(result)
	if flatCount != 3 {
		t.Errorf("GetFlatCount(result) = %d, want 3", flatCount)
	}
}

func TestGetFlatCount_Variant(t *testing.T) {
	variant := &wit.TypeDef{
		Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "a", Type: wit.U32{}},
			{Name: "b", Type: wit.String{}},
		}},
	}

	// Variants flatten to 1 (discriminant) + max payload
	// Case a: U32 = 1, Case b: String = 2 -> max = 2 -> total = 3
	result := GetFlatCount(variant)
	if result != 3 {
		t.Errorf("GetFlatCount(variant) = %d, want 3", result)
	}
}

func TestSafeMulU32_Encoder(t *testing.T) {
	tests := []struct {
		a, b     uint32
		expected uint32
		ok       bool
	}{
		{0, 0, 0, true},
		{1, 1, 1, true},
		{100, 100, 10000, true},
		{0xFFFFFFFF, 2, 0, false}, // overflow
		{0x80000000, 2, 0, false}, // overflow
		{0, 0xFFFFFFFF, 0, true},  // zero multiplication
	}

	for _, tt := range tests {
		result, ok := safeMulU32(tt.a, tt.b)
		if ok != tt.ok {
			t.Errorf("safeMulU32(%d, %d) ok = %v, want %v", tt.a, tt.b, ok, tt.ok)
		}
		if ok && result != tt.expected {
			t.Errorf("safeMulU32(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestEncoder_FlattenBool(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	// True
	err := e.flattenBool(true, &flat, nil)
	if err != nil {
		t.Fatalf("flattenBool(true) error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 1 {
		t.Errorf("flattenBool(true) = %v, want [1]", flat)
	}

	// False
	flat = flat[:0]
	err = e.flattenBool(false, &flat, nil)
	if err != nil {
		t.Fatalf("flattenBool(false) error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 0 {
		t.Errorf("flattenBool(false) = %v, want [0]", flat)
	}

	// Invalid type
	flat = flat[:0]
	err = e.flattenBool("not a bool", &flat, nil)
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestEncoder_FlattenU8(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	err := e.flattenU8(uint8(42), &flat, nil)
	if err != nil {
		t.Fatalf("flattenU8 error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 42 {
		t.Errorf("flattenU8(42) = %v, want [42]", flat)
	}

	// Also accept int8
	flat = flat[:0]
	err = e.flattenU8(int8(-1), &flat, nil)
	if err != nil {
		t.Fatalf("flattenU8(int8) error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 255 {
		t.Errorf("flattenU8(-1) = %v, want [255]", flat)
	}
}

func TestEncoder_FlattenU16(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	err := e.flattenU16(uint16(1000), &flat, nil)
	if err != nil {
		t.Fatalf("flattenU16 error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 1000 {
		t.Errorf("flattenU16(1000) = %v, want [1000]", flat)
	}
}

func TestEncoder_FlattenU32(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	err := e.flattenU32(uint32(100000), &flat, nil)
	if err != nil {
		t.Fatalf("flattenU32 error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 100000 {
		t.Errorf("flattenU32(100000) = %v, want [100000]", flat)
	}
}

func TestEncoder_FlattenU64(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	err := e.flattenU64(uint64(1<<40), &flat, nil)
	if err != nil {
		t.Fatalf("flattenU64 error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 1<<40 {
		t.Errorf("flattenU64 = %v, want [%d]", flat, 1<<40)
	}
}

func TestEncoder_FlattenF32(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	err := e.flattenF32(float32(3.14), &flat, nil)
	if err != nil {
		t.Fatalf("flattenF32 error: %v", err)
	}
	if len(flat) != 1 {
		t.Errorf("flattenF32 length = %d, want 1", len(flat))
	}
}

func TestEncoder_FlattenF64(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	err := e.flattenF64(float64(3.14159), &flat, nil)
	if err != nil {
		t.Fatalf("flattenF64 error: %v", err)
	}
	if len(flat) != 1 {
		t.Errorf("flattenF64 length = %d, want 1", len(flat))
	}
}

func TestEncoder_FlattenChar(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	// rune
	err := e.flattenChar('A', &flat, nil)
	if err != nil {
		t.Fatalf("flattenChar error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 65 {
		t.Errorf("flattenChar('A') = %v, want [65]", flat)
	}

	// string (takes first rune)
	flat = flat[:0]
	err = e.flattenChar("B", &flat, nil)
	if err != nil {
		t.Fatalf("flattenChar(string) error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 66 {
		t.Errorf("flattenChar(\"B\") = %v, want [66]", flat)
	}

	// empty string error
	flat = flat[:0]
	err = e.flattenChar("", &flat, nil)
	if err == nil {
		t.Error("expected error for empty string")
	}
}

func TestEncoder_FlattenEnum(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	enum := &wit.Enum{Cases: []wit.EnumCase{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}}

	// Valid discriminant
	err := e.flattenEnum(enum, uint32(1), &flat, nil)
	if err != nil {
		t.Fatalf("flattenEnum error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 1 {
		t.Errorf("flattenEnum(1) = %v, want [1]", flat)
	}

	// Invalid discriminant
	flat = flat[:0]
	err = e.flattenEnum(enum, uint32(5), &flat, nil)
	if err == nil {
		t.Error("expected error for invalid discriminant")
	}
}

func TestEncoder_FlattenFlags(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	flags := &wit.Flags{Flags: []wit.Flag{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}}

	err := e.flattenFlags(flags, uint32(0b101), &flat, nil)
	if err != nil {
		t.Fatalf("flattenFlags error: %v", err)
	}
	if len(flat) != 1 || flat[0] != 5 {
		t.Errorf("flattenFlags(0b101) = %v, want [5]", flat)
	}
}

func TestNewEncoder(t *testing.T) {
	e := NewEncoder()
	if e == nil {
		t.Fatal("NewEncoder returned nil")
	}
	if e.compiler == nil {
		t.Error("NewEncoder did not initialize compiler")
	}
}

func TestNewEncoderWithCompiler(t *testing.T) {
	c := NewCompiler()
	e := NewEncoderWithCompiler(c)
	if e == nil {
		t.Fatal("NewEncoderWithCompiler returned nil")
	}
	if e.compiler != c {
		t.Error("NewEncoderWithCompiler did not use provided compiler")
	}
}

func TestEncoder_NilHandling_NoPanic(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	tests := []struct {
		flatten func() error
		name    string
	}{
		{func() error { return e.flattenBool(nil, &flat, []string{"test"}) }, "bool_nil"},
		{func() error { return e.flattenU8(nil, &flat, []string{"test"}) }, "u8_nil"},
		{func() error { return e.flattenU16(nil, &flat, []string{"test"}) }, "u16_nil"},
		{func() error { return e.flattenU32(nil, &flat, []string{"test"}) }, "u32_nil"},
		{func() error { return e.flattenU64(nil, &flat, []string{"test"}) }, "u64_nil"},
		{func() error { return e.flattenF32(nil, &flat, []string{"test"}) }, "f32_nil"},
		{func() error { return e.flattenF64(nil, &flat, []string{"test"}) }, "f64_nil"},
		{func() error { return e.flattenChar(nil, &flat, []string{"test"}) }, "char_nil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flat = flat[:0]
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked: %v", tc.name, r)
				}
			}()
			err := tc.flatten()
			if err == nil {
				t.Errorf("%s should return error for nil, not succeed", tc.name)
			}
		})
	}
}

func TestEncoder_FlattenEnum_NilNoPanic(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	enum := &wit.Enum{Cases: []wit.EnumCase{
		{Name: "a"},
		{Name: "b"},
	}}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("flattenEnum panicked on nil: %v", r)
		}
	}()

	err := e.flattenEnum(enum, nil, &flat, []string{"test"})
	if err == nil {
		t.Error("flattenEnum should return error for nil")
	}
}

func TestEncoder_FlattenFlags_NilNoPanic(t *testing.T) {
	e := NewEncoder()
	flat := make([]uint64, 0, 8)

	flags := &wit.Flags{Flags: []wit.Flag{{Name: "a"}}}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("flattenFlags panicked on nil: %v", r)
		}
	}()

	err := e.flattenFlags(flags, nil, &flat, []string{"test"})
	if err == nil {
		t.Error("flattenFlags should return error for nil")
	}
}
