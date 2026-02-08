package transcoder

import (
	"encoding/binary"
	"math"
	"reflect"
	"strconv"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

// mockMemory implements Memory for testing
type mockMemory struct {
	data []byte
}

func newMockMemory(size int) *mockMemory {
	return &mockMemory{data: make([]byte, size)}
}

func (m *mockMemory) Read(offset uint32, length uint32) ([]byte, error) {
	return m.data[offset : offset+length], nil
}

func (m *mockMemory) Write(offset uint32, data []byte) error {
	copy(m.data[offset:], data)
	return nil
}

func (m *mockMemory) ReadU8(offset uint32) (uint8, error) {
	return m.data[offset], nil
}

func (m *mockMemory) ReadU16(offset uint32) (uint16, error) {
	return binary.LittleEndian.Uint16(m.data[offset:]), nil
}

func (m *mockMemory) ReadU32(offset uint32) (uint32, error) {
	return binary.LittleEndian.Uint32(m.data[offset:]), nil
}

func (m *mockMemory) ReadU64(offset uint32) (uint64, error) {
	return binary.LittleEndian.Uint64(m.data[offset:]), nil
}

func (m *mockMemory) WriteU8(offset uint32, value uint8) error {
	m.data[offset] = value
	return nil
}

func (m *mockMemory) WriteU16(offset uint32, value uint16) error {
	binary.LittleEndian.PutUint16(m.data[offset:], value)
	return nil
}

func (m *mockMemory) WriteU32(offset uint32, value uint32) error {
	binary.LittleEndian.PutUint32(m.data[offset:], value)
	return nil
}

func (m *mockMemory) WriteU64(offset uint32, value uint64) error {
	binary.LittleEndian.PutUint64(m.data[offset:], value)
	return nil
}

// mockAllocator implements Allocator for testing
type mockAllocator struct {
	mem    *mockMemory
	offset uint32
}

func newMockAllocator(mem *mockMemory) *mockAllocator {
	return &mockAllocator{offset: 1024, mem: mem} // start at 1024 to test non-zero offsets
}

func (a *mockAllocator) Alloc(size, align uint32) (uint32, error) {
	// Align offset
	a.offset = alignTo(a.offset, align)
	ptr := a.offset
	a.offset += size
	return ptr, nil
}

func (a *mockAllocator) Free(ptr, size, align uint32) {}

// Test primitive encoding/decoding round-trips
func TestEncoder_Primitives(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	tests := []struct {
		typ    wit.Type
		value  any
		expect any
		name   string
	}{
		{wit.Bool{}, true, true, "bool_true"},
		{wit.Bool{}, false, false, "bool_false"},
		{wit.U8{}, uint8(42), uint8(42), "u8"},
		{wit.S8{}, int8(-42), int8(-42), "s8"},
		{wit.U16{}, uint16(1234), uint16(1234), "u16"},
		{wit.S16{}, int16(-1234), int16(-1234), "s16"},
		{wit.U32{}, uint32(12345678), uint32(12345678), "u32"},
		{wit.S32{}, int32(-12345678), int32(-12345678), "s32"},
		{wit.U64{}, uint64(123456789012), uint64(123456789012), "u64"},
		{wit.S64{}, int64(-123456789012), int64(-123456789012), "s64"},
		{wit.F32{}, float32(3.14), float32(3.14), "f32"},
		{wit.F64{}, float64(3.14159265359), float64(3.14159265359), "f64"},
		{wit.Char{}, rune('A'), rune('A'), "char"},
		{wit.Char{}, rune('\U0001F600'), rune('\U0001F600'), "char_unicode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocList := NewAllocationList()
			flat, err := enc.EncodeParams([]wit.Type{tt.typ}, []any{tt.value}, mem, alloc, allocList)
			if err != nil {
				t.Fatalf("EncodeParams failed: %v", err)
			}

			results, err := dec.DecodeResults([]wit.Type{tt.typ}, flat, mem)
			if err != nil {
				t.Fatalf("DecodeResults failed: %v", err)
			}

			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}

			if results[0] != tt.expect {
				t.Errorf("got %v (%T), want %v (%T)", results[0], results[0], tt.expect, tt.expect)
			}
		})
	}
}

func TestEncoder_String(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	tests := []string{
		"",
		"hello",
		"hello world",
		"unicode: \U0001F600",
		string(make([]byte, 1000)),
	}

	for _, s := range tests {
		t.Run("len_"+strconv.Itoa(len(s)), func(t *testing.T) {
			allocList.Reset()
			alloc.offset = 1024

			flat, err := enc.EncodeParams([]wit.Type{wit.String{}}, []any{s}, mem, alloc, allocList)
			if err != nil {
				t.Fatalf("EncodeParams failed: %v", err)
			}

			results, err := dec.DecodeResults([]wit.Type{wit.String{}}, flat, mem)
			if err != nil {
				t.Fatalf("DecodeResults failed: %v", err)
			}

			if results[0] != s {
				t.Errorf("got %q, want %q", results[0], s)
			}
		})
	}
}

func TestEncoder_Record(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	input := map[string]any{
		"x": uint32(100),
		"y": uint32(200),
	}

	flat, err := enc.EncodeParams([]wit.Type{recordType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{recordType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].(map[string]any)
	if result["x"] != uint32(100) || result["y"] != uint32(200) {
		t.Errorf("got %v, want {x:100 y:200}", result)
	}
}

func TestEncoder_List(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	input := []uint32{1, 2, 3, 4, 5}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]uint32)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i, v := range result {
		if v != input[i] {
			t.Errorf("element %d: got %v, want %v", i, v, input[i])
		}
	}
}

func TestEncoder_Option(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	t.Run("some", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		flat, err := enc.EncodeParams([]wit.Type{optionType}, []any{uint32(42)}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{optionType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		if results[0].(uint32) != 42 {
			t.Errorf("got %v, want 42", results[0])
		}
	})

	t.Run("none", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		flat, err := enc.EncodeParams([]wit.Type{optionType}, []any{nil}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{optionType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		if results[0] != nil {
			t.Errorf("got %v, want nil", results[0])
		}
	})
}

func TestEncoder_Result(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	t.Run("ok", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"ok": uint32(42)}
		flat, err := enc.EncodeParams([]wit.Type{resultType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if val, ok := result["ok"]; !ok || val.(uint32) != 42 {
			t.Errorf("got %v, want {ok:42}", result)
		}
	})

	t.Run("err", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"err": "something failed"}
		flat, err := enc.EncodeParams([]wit.Type{resultType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if val, ok := result["err"]; !ok || val != "something failed" {
			t.Errorf("got %v, want {err:something failed}", result)
		}
	})
}

// Test compiler with WIT tags
func TestCompiler_WitTags(t *testing.T) {
	type User struct {
		FirstName string `wit:"first-name"`
		LastName  string `wit:"last-name"`
		ID        uint64 `wit:"id"`
	}

	compiler := NewCompiler()

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "id", Type: wit.U64{}},
				{Name: "first-name", Type: wit.String{}},
				{Name: "last-name", Type: wit.String{}},
			},
		},
	}

	compiled, err := compiler.Compile(recordType, reflect.TypeOf(User{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(compiled.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(compiled.Fields))
	}

	// Check field mapping
	for _, f := range compiled.Fields {
		switch f.WitName {
		case "id":
			if f.Name != "ID" {
				t.Errorf("expected Go name ID, got %s", f.Name)
			}
		case "first-name":
			if f.Name != "FirstName" {
				t.Errorf("expected Go name FirstName, got %s", f.Name)
			}
		case "last-name":
			if f.Name != "LastName" {
				t.Errorf("expected Go name LastName, got %s", f.Name)
			}
		}
	}
}

// Test unsafe struct encoding
func TestEncoder_StructUnsafe(t *testing.T) {
	type Point struct {
		X uint32
		Y uint32
	}

	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiled, err := compiler.Compile(recordType, reflect.TypeOf(Point{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	input := Point{X: 100, Y: 200}
	stack := make([]uint64, 16)

	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, nil, nil)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}

	var output Point
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}

	if output != input {
		t.Errorf("got %+v, want %+v", output, input)
	}
}

// Test boundary values
func TestEncoder_BoundaryValues(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	tests := []struct {
		typ   wit.Type
		value any
		name  string
	}{
		{wit.U8{}, uint8(255), "u8_max"},
		{wit.S8{}, int8(-128), "s8_min"},
		{wit.S8{}, int8(127), "s8_max"},
		{wit.U16{}, uint16(65535), "u16_max"},
		{wit.S16{}, int16(-32768), "s16_min"},
		{wit.S16{}, int16(32767), "s16_max"},
		{wit.U32{}, uint32(4294967295), "u32_max"},
		{wit.S32{}, int32(-2147483648), "s32_min"},
		{wit.S32{}, int32(2147483647), "s32_max"},
		{wit.U64{}, uint64(18446744073709551615), "u64_max"},
		{wit.S64{}, int64(-9223372036854775808), "s64_min"},
		{wit.S64{}, int64(9223372036854775807), "s64_max"},
		{wit.F32{}, float32(math.Inf(1)), "f32_inf"},
		{wit.F32{}, float32(math.Inf(-1)), "f32_neginf"},
		{wit.F64{}, math.Inf(1), "f64_inf"},
		{wit.F64{}, math.Inf(-1), "f64_neginf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocList := NewAllocationList()
			alloc.offset = 1024

			flat, err := enc.EncodeParams([]wit.Type{tt.typ}, []any{tt.value}, mem, alloc, allocList)
			if err != nil {
				t.Fatalf("EncodeParams failed: %v", err)
			}

			results, err := dec.DecodeResults([]wit.Type{tt.typ}, flat, mem)
			if err != nil {
				t.Fatalf("DecodeResults failed: %v", err)
			}

			// Handle NaN specially
			if _, isF32 := tt.typ.(wit.F32); isF32 {
				got := results[0].(float32)
				want := tt.value.(float32)
				if math.IsNaN(float64(want)) {
					if !math.IsNaN(float64(got)) {
						t.Errorf("expected NaN, got %v", got)
					}
				} else if got != want {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if _, isF64 := tt.typ.(wit.F64); isF64 {
				got := results[0].(float64)
				want := tt.value.(float64)
				if math.IsNaN(want) {
					if !math.IsNaN(got) {
						t.Errorf("expected NaN, got %v", got)
					}
				} else if got != want {
					t.Errorf("got %v, want %v", got, want)
				}
			} else if results[0] != tt.value {
				t.Errorf("got %v, want %v", results[0], tt.value)
			}
		})
	}
}

// Test error cases
func TestEncoder_Errors(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	t.Run("type_mismatch", func(t *testing.T) {
		_, err := enc.EncodeParams([]wit.Type{wit.U32{}}, []any{"not a number"}, mem, alloc, allocList)
		if err == nil {
			t.Error("expected error for type mismatch")
		}
	})

	t.Run("missing_field", func(t *testing.T) {
		recordType := &wit.TypeDef{
			Kind: &wit.Record{
				Fields: []wit.Field{
					{Name: "x", Type: wit.U32{}},
					{Name: "y", Type: wit.U32{}},
				},
			},
		}

		input := map[string]any{"x": uint32(100)} // missing y
		_, err := enc.EncodeParams([]wit.Type{recordType}, []any{input}, mem, alloc, allocList)
		if err == nil {
			t.Error("expected error for missing field")
		}
	})

	t.Run("invalid_utf8", func(t *testing.T) {
		// Create invalid UTF-8
		invalid := string([]byte{0xff, 0xfe})
		_, err := enc.EncodeParams([]wit.Type{wit.String{}}, []any{invalid}, mem, alloc, allocList)
		if err == nil {
			t.Error("expected error for invalid UTF-8")
		}
	})
}

// Test tuple encoding/decoding
func TestEncoder_Tuple(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.String{}, wit.Bool{}},
		},
	}

	t.Run("basic", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := []any{uint32(42), "hello", true}
		flat, err := enc.EncodeParams([]wit.Type{tupleType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{tupleType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].([]any)
		if len(result) != 3 {
			t.Fatalf("expected 3 elements, got %d", len(result))
		}
		if result[0].(uint32) != 42 {
			t.Errorf("element 0: got %v, want 42", result[0])
		}
		if result[1].(string) != "hello" {
			t.Errorf("element 1: got %v, want hello", result[1])
		}
		if result[2].(bool) != true {
			t.Errorf("element 2: got %v, want true", result[2])
		}
	})

	t.Run("nested", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		innerTupleType := &wit.TypeDef{
			Kind: &wit.Tuple{
				Types: []wit.Type{wit.U32{}, wit.U32{}},
			},
		}
		nestedTupleType := &wit.TypeDef{
			Kind: &wit.Tuple{
				Types: []wit.Type{innerTupleType, wit.String{}},
			},
		}

		input := []any{[]any{uint32(1), uint32(2)}, "test"}
		flat, err := enc.EncodeParams([]wit.Type{nestedTupleType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{nestedTupleType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].([]any)
		inner := result[0].([]any)
		if inner[0].(uint32) != 1 || inner[1].(uint32) != 2 {
			t.Errorf("inner tuple: got %v, want [1, 2]", inner)
		}
		if result[1].(string) != "test" {
			t.Errorf("outer string: got %v, want test", result[1])
		}
	})

	t.Run("wrong_length", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := []any{uint32(42), "hello"} // missing third element
		_, err := enc.EncodeParams([]wit.Type{tupleType}, []any{input}, mem, alloc, allocList)
		if err == nil {
			t.Error("expected error for wrong tuple length")
		}
	})
}

// Test enum encoding/decoding
func TestEncoder_Enum(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "red"},
				{Name: "green"},
				{Name: "blue"},
			},
		},
	}

	tests := []struct {
		input any
		name  string
		want  uint32
	}{
		{uint32(0), "first_case", 0},
		{uint32(1), "second_case", 1},
		{uint32(2), "third_case", 2},
		{int(1), "int_value", 1},
		{uint8(2), "uint8_value", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocList.Reset()
			alloc.offset = 1024

			flat, err := enc.EncodeParams([]wit.Type{enumType}, []any{tt.input}, mem, alloc, allocList)
			if err != nil {
				t.Fatalf("EncodeParams failed: %v", err)
			}

			results, err := dec.DecodeResults([]wit.Type{enumType}, flat, mem)
			if err != nil {
				t.Fatalf("DecodeResults failed: %v", err)
			}

			if results[0].(uint32) != tt.want {
				t.Errorf("got %v, want %v", results[0], tt.want)
			}
		})
	}

	t.Run("invalid_discriminant", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		_, err := enc.EncodeParams([]wit.Type{enumType}, []any{uint32(5)}, mem, alloc, allocList)
		if err == nil {
			t.Error("expected error for invalid discriminant")
		}
	})
}

// Test variant encoding/decoding
func TestEncoder_Variant(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "none", Type: nil},
				{Name: "some-int", Type: wit.U32{}},
				{Name: "some-string", Type: wit.String{}},
			},
		},
	}

	t.Run("unit_case", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"none": nil}
		flat, err := enc.EncodeParams([]wit.Type{variantType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{variantType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if _, ok := result["none"]; !ok {
			t.Errorf("expected 'none' case, got %v", result)
		}
	})

	t.Run("int_case", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"some-int": uint32(42)}
		flat, err := enc.EncodeParams([]wit.Type{variantType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{variantType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if val, ok := result["some-int"]; !ok || val.(uint32) != 42 {
			t.Errorf("expected {some-int: 42}, got %v", result)
		}
	})

	t.Run("string_case", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"some-string": "hello"}
		flat, err := enc.EncodeParams([]wit.Type{variantType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{variantType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if val, ok := result["some-string"]; !ok || val.(string) != "hello" {
			t.Errorf("expected {some-string: hello}, got %v", result)
		}
	})

	t.Run("invalid_case", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"unknown": uint32(42)}
		_, err := enc.EncodeParams([]wit.Type{variantType}, []any{input}, mem, alloc, allocList)
		if err == nil {
			t.Error("expected error for unknown case")
		}
	})
}

// Test flags encoding/decoding
func TestEncoder_Flags(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: []wit.Flag{
				{Name: "read"},
				{Name: "write"},
				{Name: "execute"},
			},
		},
	}

	tests := []struct {
		name  string
		input uint64
		want  uint64
	}{
		{"none", 0, 0},
		{"read_only", 1, 1},    // bit 0
		{"write_only", 2, 2},   // bit 1
		{"execute_only", 4, 4}, // bit 2
		{"read_write", 3, 3},   // bits 0,1
		{"all", 7, 7},          // bits 0,1,2
		{"read_execute", 5, 5}, // bits 0,2
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocList.Reset()
			alloc.offset = 1024

			flat, err := enc.EncodeParams([]wit.Type{flagsType}, []any{tt.input}, mem, alloc, allocList)
			if err != nil {
				t.Fatalf("EncodeParams failed: %v", err)
			}

			results, err := dec.DecodeResults([]wit.Type{flagsType}, flat, mem)
			if err != nil {
				t.Fatalf("DecodeResults failed: %v", err)
			}

			if results[0].(uint64) != tt.want {
				t.Errorf("got %v, want %v", results[0], tt.want)
			}
		})
	}

	t.Run("type_mismatch", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		_, err := enc.EncodeParams([]wit.Type{flagsType}, []any{"not_a_number"}, mem, alloc, allocList)
		if err == nil {
			t.Error("expected error for type mismatch")
		}
	})
}

// Test large list (to verify path indexing works beyond single digit)
func TestEncoder_LargeList(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(65536)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	// Create list with 100 elements to test path indexing beyond single digit
	input := make([]any, 100)
	for i := range input {
		input[i] = uint32(i)
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]uint32)
	if len(result) != 100 {
		t.Fatalf("expected 100 elements, got %d", len(result))
	}

	for i, v := range result {
		if v != uint32(i) {
			t.Errorf("element %d: got %v, want %v", i, v, i)
		}
	}
}

// Test nested record with option
func TestEncoder_NestedRecordWithOption(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "name", Type: wit.String{}},
				{Name: "age", Type: optionType},
			},
		},
	}

	t.Run("with_some", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{
			"name": "test",
			"age":  uint32(25),
		}
		flat, err := enc.EncodeParams([]wit.Type{recordType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{recordType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if result["name"].(string) != "test" {
			t.Errorf("name: got %v, want test", result["name"])
		}
		if result["age"].(uint32) != 25 {
			t.Errorf("age: got %v, want 25", result["age"])
		}
	})

	t.Run("with_none", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{
			"name": "test",
			"age":  nil,
		}
		flat, err := enc.EncodeParams([]wit.Type{recordType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{recordType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if result["name"].(string) != "test" {
			t.Errorf("name: got %v, want test", result["name"])
		}
		if result["age"] != nil {
			t.Errorf("age: got %v, want nil", result["age"])
		}
	})
}

// Test kebab case conversion
func TestToKebabCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FirstName", "first-name"},
		{"ID", "i-d"},
		{"XMLParser", "x-m-l-parser"},
		{"simple", "simple"},
		{"CamelCase", "camel-case"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toKebabCase(tt.input)
			if result != tt.expected {
				t.Errorf("toKebabCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Test layout calculator for various types
func TestLayoutCalculator(t *testing.T) {
	lc := NewLayoutCalculator()

	tests := []struct {
		typ   wit.Type
		name  string
		size  uint32
		align uint32
	}{
		{wit.Bool{}, "bool", 1, 1},
		{wit.U8{}, "u8", 1, 1},
		{wit.S8{}, "s8", 1, 1},
		{wit.U16{}, "u16", 2, 2},
		{wit.S16{}, "s16", 2, 2},
		{wit.U32{}, "u32", 4, 4},
		{wit.S32{}, "s32", 4, 4},
		{wit.U64{}, "u64", 8, 8},
		{wit.S64{}, "s64", 8, 8},
		{wit.F32{}, "f32", 4, 4},
		{wit.F64{}, "f64", 8, 8},
		{wit.Char{}, "char", 4, 4},
		{wit.String{}, "string", 8, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout := lc.Calculate(tt.typ)
			if layout.Size != tt.size {
				t.Errorf("size: got %d, want %d", layout.Size, tt.size)
			}
			if layout.Align != tt.align {
				t.Errorf("align: got %d, want %d", layout.Align, tt.align)
			}
		})
	}
}

// Test layout for complex types
func TestLayoutCalculator_ComplexTypes(t *testing.T) {
	lc := NewLayoutCalculator()

	t.Run("record", func(t *testing.T) {
		recordType := &wit.TypeDef{
			Kind: &wit.Record{
				Fields: []wit.Field{
					{Name: "x", Type: wit.U32{}},
					{Name: "y", Type: wit.U32{}},
				},
			},
		}
		layout := lc.Calculate(recordType)
		if layout.Size != 8 {
			t.Errorf("size: got %d, want 8", layout.Size)
		}
		if layout.Align != 4 {
			t.Errorf("align: got %d, want 4", layout.Align)
		}
	})

	t.Run("list", func(t *testing.T) {
		listType := &wit.TypeDef{
			Kind: &wit.List{Type: wit.U32{}},
		}
		layout := lc.Calculate(listType)
		if layout.Size != 8 {
			t.Errorf("size: got %d, want 8", layout.Size)
		}
		if layout.Align != 4 {
			t.Errorf("align: got %d, want 4", layout.Align)
		}
	})

	t.Run("option", func(t *testing.T) {
		optionType := &wit.TypeDef{
			Kind: &wit.Option{Type: wit.U32{}},
		}
		layout := lc.Calculate(optionType)
		// option<u32>: 1 byte discriminant + 3 padding + 4 bytes u32 = 8
		if layout.Size != 8 {
			t.Errorf("size: got %d, want 8", layout.Size)
		}
		if layout.Align != 4 {
			t.Errorf("align: got %d, want 4", layout.Align)
		}
	})

	t.Run("result", func(t *testing.T) {
		resultType := &wit.TypeDef{
			Kind: &wit.Result{
				OK:  wit.U32{},
				Err: wit.String{},
			},
		}
		layout := lc.Calculate(resultType)
		// result<u32, string>: discriminant + max(u32, string) with proper alignment
		if layout.Size < 8 {
			t.Errorf("size: got %d, want >= 8", layout.Size)
		}
	})

	t.Run("tuple", func(t *testing.T) {
		tupleType := &wit.TypeDef{
			Kind: &wit.Tuple{
				Types: []wit.Type{wit.U32{}, wit.U64{}},
			},
		}
		layout := lc.Calculate(tupleType)
		// u32(4) + padding(4) + u64(8) = 16
		if layout.Size != 16 {
			t.Errorf("size: got %d, want 16", layout.Size)
		}
		if layout.Align != 8 {
			t.Errorf("align: got %d, want 8", layout.Align)
		}
	})

	t.Run("enum_3", func(t *testing.T) {
		cases := make([]wit.EnumCase, 3)
		for i := range cases {
			cases[i] = wit.EnumCase{Name: string(rune('a' + i))}
		}
		enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
		layout := lc.Calculate(enumType)
		if layout.Size != 1 || layout.Align != 1 {
			t.Errorf("3 cases: got size=%d align=%d, want size=1 align=1", layout.Size, layout.Align)
		}
	})

	t.Run("enum_257", func(t *testing.T) {
		cases := make([]wit.EnumCase, 257)
		for i := range cases {
			cases[i] = wit.EnumCase{Name: string(rune('a' + i))}
		}
		enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
		layout := lc.Calculate(enumType)
		if layout.Size != 2 || layout.Align != 2 {
			t.Errorf("257 cases: got size=%d align=%d, want size=2 align=2", layout.Size, layout.Align)
		}
	})

	t.Run("enum_65537", func(t *testing.T) {
		cases := make([]wit.EnumCase, 65537)
		for i := range cases {
			cases[i] = wit.EnumCase{Name: string(rune('a' + i))}
		}
		enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
		layout := lc.Calculate(enumType)
		if layout.Size != 4 || layout.Align != 4 {
			t.Errorf("65537 cases: got size=%d align=%d, want size=4 align=4", layout.Size, layout.Align)
		}
	})

	t.Run("variant", func(t *testing.T) {
		variantType := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "none", Type: nil},
					{Name: "some", Type: wit.U32{}},
				},
			},
		}
		layout := lc.Calculate(variantType)
		// 1 byte discriminant + 3 padding + 4 bytes u32 = 8
		if layout.Size != 8 {
			t.Errorf("size: got %d, want 8", layout.Size)
		}
	})

	t.Run("flags_0", func(t *testing.T) {
		flagsType := &wit.TypeDef{
			Kind: &wit.Flags{Flags: []wit.Flag{}},
		}
		layout := lc.Calculate(flagsType)
		if layout.Size != 0 {
			t.Errorf("size: got %d, want 0", layout.Size)
		}
	})

	t.Run("flags_3", func(t *testing.T) {
		flags := make([]wit.Flag, 3)
		for i := range flags {
			flags[i] = wit.Flag{Name: string(rune('a' + i))}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		layout := lc.Calculate(flagsType)
		if layout.Size != 1 || layout.Align != 1 {
			t.Errorf("3 flags: got size=%d align=%d, want size=1 align=1", layout.Size, layout.Align)
		}
	})

	t.Run("flags_9", func(t *testing.T) {
		flags := make([]wit.Flag, 9)
		for i := range flags {
			flags[i] = wit.Flag{Name: string(rune('a' + i))}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		layout := lc.Calculate(flagsType)
		if layout.Size != 2 || layout.Align != 2 {
			t.Errorf("9 flags: got size=%d align=%d, want size=2 align=2", layout.Size, layout.Align)
		}
	})

	t.Run("flags_17", func(t *testing.T) {
		flags := make([]wit.Flag, 17)
		for i := range flags {
			flags[i] = wit.Flag{Name: string(rune('a' + i))}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		layout := lc.Calculate(flagsType)
		if layout.Size != 4 || layout.Align != 4 {
			t.Errorf("17 flags: got size=%d align=%d, want size=4 align=4", layout.Size, layout.Align)
		}
	})

	t.Run("flags_33", func(t *testing.T) {
		flags := make([]wit.Flag, 33)
		for i := range flags {
			flags[i] = wit.Flag{Name: string(rune('a' + i))}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		layout := lc.Calculate(flagsType)
		if layout.Size != 8 || layout.Align != 8 {
			t.Errorf("33 flags: got size=%d align=%d, want size=8 align=8", layout.Size, layout.Align)
		}
	})

	t.Run("flags_65", func(t *testing.T) {
		flags := make([]wit.Flag, 65)
		for i := range flags {
			flags[i] = wit.Flag{Name: string(rune('a' + i))}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		layout := lc.Calculate(flagsType)
		// 65 flags need 3 u32s = 12 bytes
		if layout.Size != 12 || layout.Align != 4 {
			t.Errorf("65 flags: got size=%d align=%d, want size=12 align=4", layout.Size, layout.Align)
		}
	})
}

// Test TypeKind String method
func TestTypeKind_String(t *testing.T) {
	tests := []struct {
		want string
		kind TypeKind
	}{
		{"bool", KindBool},
		{"u8", KindU8},
		{"s8", KindS8},
		{"u16", KindU16},
		{"s16", KindS16},
		{"u32", KindU32},
		{"s32", KindS32},
		{"u64", KindU64},
		{"s64", KindS64},
		{"f32", KindF32},
		{"f64", KindF64},
		{"char", KindChar},
		{"string", KindString},
		{"record", KindRecord},
		{"list", KindList},
		{"variant", KindVariant},
		{"option", KindOption},
		{"result", KindResult},
		{"tuple", KindTuple},
		{"enum", KindEnum},
		{"flags", KindFlags},
		{"unknown", TypeKind(255)},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("TypeKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

// Test CompiledType IsPrimitive
func TestCompiledType_IsPrimitive(t *testing.T) {
	tests := []struct {
		kind TypeKind
		want bool
	}{
		{KindBool, true},
		{KindU32, true},
		{KindChar, true},
		{KindString, false},
		{KindRecord, false},
		{KindList, false},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			ct := &CompiledType{Kind: tt.kind}
			if got := ct.IsPrimitive(); got != tt.want {
				t.Errorf("IsPrimitive() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test CompiledType IsPure
func TestCompiledType_IsPure(t *testing.T) {
	t.Run("primitive", func(t *testing.T) {
		ct := &CompiledType{Kind: KindU32}
		if !ct.IsPure() {
			t.Error("primitive should be pure")
		}
	})

	t.Run("string", func(t *testing.T) {
		ct := &CompiledType{Kind: KindString}
		if ct.IsPure() {
			t.Error("string should not be pure")
		}
	})

	t.Run("list", func(t *testing.T) {
		ct := &CompiledType{Kind: KindList}
		if ct.IsPure() {
			t.Error("list should not be pure")
		}
	})

	t.Run("record_with_primitives", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindRecord,
			Fields: []CompiledField{
				{Type: &CompiledType{Kind: KindU32}},
				{Type: &CompiledType{Kind: KindBool}},
			},
		}
		if !ct.IsPure() {
			t.Error("record with only primitives should be pure")
		}
	})

	t.Run("record_with_string", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindRecord,
			Fields: []CompiledField{
				{Type: &CompiledType{Kind: KindU32}},
				{Type: &CompiledType{Kind: KindString}},
			},
		}
		if ct.IsPure() {
			t.Error("record with string should not be pure")
		}
	})

	t.Run("option_pure", func(t *testing.T) {
		ct := &CompiledType{
			Kind:     KindOption,
			ElemType: &CompiledType{Kind: KindU32},
		}
		if !ct.IsPure() {
			t.Error("option<u32> should be pure")
		}
	})

	t.Run("option_impure", func(t *testing.T) {
		ct := &CompiledType{
			Kind:     KindOption,
			ElemType: &CompiledType{Kind: KindString},
		}
		if ct.IsPure() {
			t.Error("option<string> should not be pure")
		}
	})

	t.Run("result_pure", func(t *testing.T) {
		ct := &CompiledType{
			Kind:    KindResult,
			OkType:  &CompiledType{Kind: KindU32},
			ErrType: &CompiledType{Kind: KindBool},
		}
		if !ct.IsPure() {
			t.Error("result<u32, bool> should be pure")
		}
	})

	t.Run("result_impure", func(t *testing.T) {
		ct := &CompiledType{
			Kind:    KindResult,
			OkType:  &CompiledType{Kind: KindU32},
			ErrType: &CompiledType{Kind: KindString},
		}
		if ct.IsPure() {
			t.Error("result<u32, string> should not be pure")
		}
	})

	t.Run("variant_pure", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindVariant,
			Cases: []CompiledCase{
				{Name: "none", Type: nil},
				{Name: "some", Type: &CompiledType{Kind: KindU32}},
			},
		}
		if !ct.IsPure() {
			t.Error("variant with primitives should be pure")
		}
	})

	t.Run("variant_impure", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindVariant,
			Cases: []CompiledCase{
				{Name: "none", Type: nil},
				{Name: "some", Type: &CompiledType{Kind: KindString}},
			},
		}
		if ct.IsPure() {
			t.Error("variant with string should not be pure")
		}
	})

	t.Run("tuple_pure", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindTuple,
			Fields: []CompiledField{
				{Type: &CompiledType{Kind: KindU32}},
				{Type: &CompiledType{Kind: KindBool}},
			},
		}
		if !ct.IsPure() {
			t.Error("tuple with primitives should be pure")
		}
	})
}

// Test unsafe struct decoding
func TestDecoder_StructUnsafe(t *testing.T) {
	type Point struct {
		X uint32
		Y uint32
	}

	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiled, err := compiler.Compile(recordType, reflect.TypeOf(Point{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	stack := make([]uint64, 16)

	// Test multiple round trips
	for i := 0; i < 10; i++ {
		input := Point{X: uint32(i * 100), Y: uint32(i * 200)}

		n, err := enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, nil, nil)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var output Point
		_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}

		if output != input {
			t.Errorf("iteration %d: got %+v, want %+v", i, output, input)
		}
	}
}

// Test unsafe struct with nested types
func TestEncoder_NestedStructUnsafe(t *testing.T) {
	type Inner struct {
		A uint32
		B uint32
	}

	type Outer struct {
		X     uint32
		Inner Inner
		Y     uint32
	}

	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)

	innerType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.U32{}},
			},
		},
	}

	outerType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "inner", Type: innerType},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiled, err := compiler.Compile(outerType, reflect.TypeOf(Outer{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	input := Outer{
		X:     100,
		Inner: Inner{A: 200, B: 300},
		Y:     400,
	}

	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, nil, nil)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}

	var output Outer
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}

	if output != input {
		t.Errorf("got %+v, want %+v", output, input)
	}
}

// Test allocation list
func TestAllocationList(t *testing.T) {
	allocList := NewAllocationList()

	allocList.Add(100, 10, 4)
	allocList.Add(200, 20, 8)

	if allocList.Count() != 2 {
		t.Errorf("Count() = %d, want 2", allocList.Count())
	}

	allocList.Reset()
	if allocList.Count() != 0 {
		t.Errorf("Count() after reset = %d, want 0", allocList.Count())
	}
}

// Test alignTo function
func TestAlignTo(t *testing.T) {
	tests := []struct {
		offset, align, expected uint32
	}{
		{0, 1, 0},
		{0, 4, 0},
		{1, 1, 1},
		{1, 4, 4},
		{3, 4, 4},
		{4, 4, 4},
		{5, 4, 8},
		{7, 8, 8},
		{8, 8, 8},
		{9, 8, 16},
		{0, 0, 0}, // align=0 edge case
		{5, 0, 5}, // align=0 edge case
	}

	lc := NewLayoutCalculator()
	for _, tt := range tests {
		// Test via Calculate on a type with known alignment
		result := alignTo(tt.offset, tt.align)
		if result != tt.expected {
			t.Errorf("alignTo(%d, %d) = %d, want %d", tt.offset, tt.align, result, tt.expected)
		}
		_ = lc // just to use it
	}
}
