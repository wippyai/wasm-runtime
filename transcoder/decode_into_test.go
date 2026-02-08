package transcoder

import (
	"math"
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestDecodeInto_NilResult(t *testing.T) {
	d := NewDecoder()
	err := d.DecodeInto([]wit.Type{wit.U32{}}, []uint64{42}, nil, nil)
	if err != nil {
		t.Errorf("DecodeInto nil result: %v", err)
	}
}

func TestDecodeInto_NonPointer(t *testing.T) {
	d := NewDecoder()
	err := d.DecodeInto([]wit.Type{wit.U32{}}, []uint64{42}, nil, 42)
	if err == nil {
		t.Error("expected error for non-pointer result")
	}
}

func TestDecodeInto_NilPointer(t *testing.T) {
	d := NewDecoder()
	var ptr *uint32
	err := d.DecodeInto([]wit.Type{wit.U32{}}, []uint64{42}, nil, ptr)
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestDecodeInto_Primitives(t *testing.T) {
	d := NewDecoder()

	tests := []struct {
		witType  wit.Type
		expected any
		name     string
		flat     []uint64
	}{
		{wit.Bool{}, true, "bool true", []uint64{1}},
		{wit.Bool{}, false, "bool false", []uint64{0}},
		{wit.U8{}, uint8(42), "u8", []uint64{42}},
		{wit.S8{}, int8(-10), "s8", []uint64{0xF6}}, // -10 as uint8
		{wit.U16{}, uint16(1000), "u16", []uint64{1000}},
		{wit.S16{}, int16(-1000), "s16", []uint64{0xFC18}}, // -1000 as uint16
		{wit.U32{}, uint32(100000), "u32", []uint64{100000}},
		{wit.S32{}, int32(-100000), "s32", []uint64{0xFFFE7960}}, // -100000 as uint32
		{wit.U64{}, uint64(1000000000000), "u64", []uint64{1000000000000}},
		{wit.S64{}, int64(-1000000000000), "s64", []uint64{0xFFFFFF172B5AF000}},
		{wit.F32{}, float32(3.14), "f32", []uint64{uint64(math.Float32bits(3.14))}},
		{wit.F64{}, 3.14159, "f64", []uint64{math.Float64bits(3.14159)}},
		{wit.Char{}, rune(0x1F600), "char", []uint64{0x1F600}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.witType.(type) {
			case wit.Bool:
				var result bool
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(bool) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.U8:
				var result uint8
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(uint8) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.S8:
				var result int8
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(int8) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.U16:
				var result uint16
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(uint16) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.S16:
				var result int16
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(int16) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.U32:
				var result uint32
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(uint32) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.S32:
				var result int32
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(int32) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.U64:
				var result uint64
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(uint64) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.S64:
				var result int64
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(int64) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.F32:
				var result float32
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(float32) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.F64:
				var result float64
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(float64) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			case wit.Char:
				var result rune
				err := d.DecodeInto([]wit.Type{tt.witType}, tt.flat, nil, &result)
				if err != nil {
					t.Fatalf("DecodeInto failed: %v", err)
				}
				if result != tt.expected.(rune) {
					t.Errorf("got %v, want %v", result, tt.expected)
				}
			}
		})
	}
}

func TestDecodeInto_String(t *testing.T) {
	d := NewDecoder()
	mem := newMockMemory(1024)
	copy(mem.data[100:], []byte("hello world"))

	var result string
	// flat: [addr, len]
	err := d.DecodeInto([]wit.Type{wit.String{}}, []uint64{100, 11}, mem, &result)
	if err != nil {
		t.Fatalf("DecodeInto string: %v", err)
	}
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestDecodeInto_StringEmpty(t *testing.T) {
	d := NewDecoder()
	mem := newMockMemory(1024)

	var result string
	err := d.DecodeInto([]wit.Type{wit.String{}}, []uint64{0, 0}, mem, &result)
	if err != nil {
		t.Fatalf("DecodeInto empty string: %v", err)
	}
	if result != "" {
		t.Errorf("got %q, want empty string", result)
	}
}

func TestDecodeInto_MultipleResults(t *testing.T) {
	d := NewDecoder()

	type Results struct {
		A uint32
		B uint64
	}

	var result Results
	flat := []uint64{42, 100}
	err := d.DecodeInto([]wit.Type{wit.U32{}, wit.U64{}}, flat, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto multiple: %v", err)
	}
	if result.A != 42 || result.B != 100 {
		t.Errorf("got %+v, want {A:42 B:100}", result)
	}
}

func TestDecodeInto_MultipleResultsNonStruct(t *testing.T) {
	d := NewDecoder()
	var result uint32
	err := d.DecodeInto([]wit.Type{wit.U32{}, wit.U64{}}, []uint64{42, 100}, nil, &result)
	if err == nil {
		t.Error("expected error for non-struct multiple results")
	}
}

func TestDecodeInto_Enum(t *testing.T) {
	d := NewDecoder()
	enumType := &wit.TypeDef{
		Kind: &wit.Enum{Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
	}

	var result uint32
	err := d.DecodeInto([]wit.Type{enumType}, []uint64{2}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto enum: %v", err)
	}
	if result != 2 {
		t.Errorf("got %d, want 2", result)
	}
}

func TestDecodeInto_Flags(t *testing.T) {
	d := NewDecoder()
	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{Flags: []wit.Flag{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
	}

	var result uint64
	err := d.DecodeInto([]wit.Type{flagsType}, []uint64{0b101}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto flags: %v", err)
	}
	if result != 0b101 {
		t.Errorf("got %d, want 5", result)
	}
}

func TestDecodeInto_Record(t *testing.T) {
	d := NewDecoder()
	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	var result map[string]any
	// flat: [x, y]
	err := d.DecodeInto([]wit.Type{recordType}, []uint64{10, 20}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto record: %v", err)
	}
	if result["x"] != uint32(10) || result["y"] != uint32(20) {
		t.Errorf("got %+v, want {x:10 y:20}", result)
	}
}

func TestDecodeInto_Tuple(t *testing.T) {
	d := NewDecoder()
	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U32{}, wit.U64{}}},
	}

	var result []any
	err := d.DecodeInto([]wit.Type{tupleType}, []uint64{42, 100}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto tuple: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d elements, want 2", len(result))
	}
	if result[0] != uint32(42) || result[1] != uint64(100) {
		t.Errorf("got %+v, want [42 100]", result)
	}
}

func TestDecodeInto_OptionNone(t *testing.T) {
	d := NewDecoder()
	optType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	var result any
	// disc=0 (None), followed by padding for inner type
	err := d.DecodeInto([]wit.Type{optType}, []uint64{0, 0}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto option none: %v", err)
	}
	if result != nil {
		t.Errorf("got %v, want nil", result)
	}
}

func TestDecodeInto_OptionSome(t *testing.T) {
	d := NewDecoder()
	optType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	var result any
	// disc=1 (Some), value=42
	err := d.DecodeInto([]wit.Type{optType}, []uint64{1, 42}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto option some: %v", err)
	}
	if result != uint32(42) {
		t.Errorf("got %v, want 42", result)
	}
}

func TestDecodeInto_ResultOk(t *testing.T) {
	d := NewDecoder()
	resultType := &wit.TypeDef{
		Kind: &wit.Result{OK: wit.U32{}, Err: nil},
	}

	var result map[string]any
	// disc=0 (Ok), value=42
	err := d.DecodeInto([]wit.Type{resultType}, []uint64{0, 42}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto result ok: %v", err)
	}
	if result["ok"] != uint32(42) {
		t.Errorf("got %+v, want {ok:42}", result)
	}
}

func TestDecodeInto_ResultErr(t *testing.T) {
	d := NewDecoder()
	resultType := &wit.TypeDef{
		Kind: &wit.Result{OK: nil, Err: wit.U32{}},
	}

	var result map[string]any
	// disc=1 (Err), value=99
	err := d.DecodeInto([]wit.Type{resultType}, []uint64{1, 99}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto result err: %v", err)
	}
	if result["err"] != uint32(99) {
		t.Errorf("got %+v, want {err:99}", result)
	}
}

func TestDecodeInto_ResultOkNil(t *testing.T) {
	d := NewDecoder()
	resultType := &wit.TypeDef{
		Kind: &wit.Result{OK: nil, Err: nil},
	}

	var result map[string]any
	err := d.DecodeInto([]wit.Type{resultType}, []uint64{0}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto result ok nil: %v", err)
	}
	if _, ok := result["ok"]; !ok {
		t.Errorf("expected 'ok' key in result")
	}
}

func TestDecodeInto_ResultErrNil(t *testing.T) {
	d := NewDecoder()
	resultType := &wit.TypeDef{
		Kind: &wit.Result{OK: nil, Err: nil},
	}

	var result map[string]any
	err := d.DecodeInto([]wit.Type{resultType}, []uint64{1}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto result err nil: %v", err)
	}
	if _, ok := result["err"]; !ok {
		t.Errorf("expected 'err' key in result")
	}
}

func TestDecodeInto_Variant(t *testing.T) {
	d := NewDecoder()
	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "none", Type: nil},
				{Name: "some", Type: wit.U32{}},
			},
		},
	}

	// Test case 0 (none)
	var result map[string]any
	err := d.DecodeInto([]wit.Type{variantType}, []uint64{0, 0}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto variant none: %v", err)
	}
	if result["none"] != nil {
		t.Errorf("got %+v, want {none:nil}", result)
	}

	// Test case 1 (some)
	result = nil
	err = d.DecodeInto([]wit.Type{variantType}, []uint64{1, 42}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto variant some: %v", err)
	}
	if result["some"] != uint32(42) {
		t.Errorf("got %+v, want {some:42}", result)
	}
}

func TestDecodeInto_VariantInvalidDiscriminant(t *testing.T) {
	d := NewDecoder()
	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{{Name: "a", Type: nil}},
		},
	}

	var result map[string]any
	err := d.DecodeInto([]wit.Type{variantType}, []uint64{5, 0}, nil, &result)
	if err == nil {
		t.Error("expected error for invalid discriminant")
	}
}

func TestDecodeInto_List(t *testing.T) {
	d := NewDecoder()
	mem := newMockMemory(1024)

	// Write list data to memory
	mem.WriteU32(100, 1)
	mem.WriteU32(104, 2)
	mem.WriteU32(108, 3)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	var result []any
	// flat: [addr=100, len=3]
	err := d.DecodeInto([]wit.Type{listType}, []uint64{100, 3}, mem, &result)
	if err != nil {
		t.Fatalf("DecodeInto list: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("got %d elements, want 3", len(result))
	}
	if result[0] != uint32(1) || result[1] != uint32(2) || result[2] != uint32(3) {
		t.Errorf("got %+v, want [1 2 3]", result)
	}
}

func TestDecodeInto_ListEmpty(t *testing.T) {
	d := NewDecoder()
	mem := newMockMemory(1024)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	var result []any
	err := d.DecodeInto([]wit.Type{listType}, []uint64{0, 0}, mem, &result)
	if err != nil {
		t.Fatalf("DecodeInto empty list: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d elements, want 0", len(result))
	}
}

func TestDecodeInto_TypeDefAlias(t *testing.T) {
	d := NewDecoder()
	// TypeDef wrapping a primitive
	aliasType := &wit.TypeDef{
		Kind: wit.U32{},
	}

	var result uint32
	err := d.DecodeInto([]wit.Type{aliasType}, []uint64{42}, nil, &result)
	if err != nil {
		t.Fatalf("DecodeInto alias: %v", err)
	}
	if result != 42 {
		t.Errorf("got %d, want 42", result)
	}
}
