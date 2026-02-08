package invoke

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestFlatCount_NilType(t *testing.T) {
	count := FlatCount(nil)
	if count != 0 {
		t.Errorf("expected 0 for nil type, got %d", count)
	}
}

func TestFlatCount_Primitives(t *testing.T) {
	tests := []struct {
		typ      wit.Type
		name     string
		expected int
	}{
		{wit.Bool{}, "Bool", 1},
		{wit.U8{}, "U8", 1},
		{wit.S8{}, "S8", 1},
		{wit.U16{}, "U16", 1},
		{wit.S16{}, "S16", 1},
		{wit.U32{}, "U32", 1},
		{wit.S32{}, "S32", 1},
		{wit.U64{}, "U64", 1},
		{wit.S64{}, "S64", 1},
		{wit.F32{}, "F32", 1},
		{wit.F64{}, "F64", 1},
		{wit.Char{}, "Char", 1},
		{wit.String{}, "String", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := FlatCount(tt.typ)
			if count != tt.expected {
				t.Errorf("expected %d for %s, got %d", tt.expected, tt.name, count)
			}
		})
	}
}

func TestFlatCount_NilTypeDef(t *testing.T) {
	var td *wit.TypeDef
	count := FlatCount(td)
	if count != 0 {
		t.Errorf("expected 0 for nil TypeDef, got %d", count)
	}
}

func TestFlatCount_TypeDefWithNilKind(t *testing.T) {
	td := &wit.TypeDef{
		Kind: nil,
	}
	count := FlatCount(td)
	if count != 0 {
		t.Errorf("expected 0 for TypeDef with nil Kind, got %d", count)
	}
}

func TestFlatCount_Record(t *testing.T) {
	record := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.U32{}},
				{Name: "c", Type: wit.String{}}, // 2 flat values
			},
		},
	}
	count := FlatCount(record)
	if count != 4 { // 1 + 1 + 2
		t.Errorf("expected 4 for record, got %d", count)
	}
}

func TestFlatCount_List(t *testing.T) {
	list := &wit.TypeDef{
		Kind: &wit.List{
			Type: wit.U32{},
		},
	}
	count := FlatCount(list)
	if count != 2 { // ptr + len
		t.Errorf("expected 2 for list, got %d", count)
	}
}

func TestFlatCount_Tuple(t *testing.T) {
	tuple := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U32{}, wit.U64{}},
		},
	}
	count := FlatCount(tuple)
	if count != 3 {
		t.Errorf("expected 3 for tuple, got %d", count)
	}
}

func TestFlatCount_Option(t *testing.T) {
	option := &wit.TypeDef{
		Kind: &wit.Option{
			Type: wit.U32{},
		},
	}
	count := FlatCount(option)
	if count != 2 { // discriminant + payload
		t.Errorf("expected 2 for option, got %d", count)
	}
}

func TestFlatCount_Result(t *testing.T) {
	result := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{}, // 2 flat values
		},
	}
	count := FlatCount(result)
	if count != 3 { // discriminant + max(ok=1, err=2)
		t.Errorf("expected 3 for result, got %d", count)
	}
}

func TestFlatCount_Variant(t *testing.T) {
	variant := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "a", Type: wit.U32{}},    // 1
				{Name: "b", Type: wit.String{}}, // 2
				{Name: "c", Type: nil},          // 0
			},
		},
	}
	count := FlatCount(variant)
	if count != 3 { // discriminant + max(1, 2, 0)
		t.Errorf("expected 3 for variant, got %d", count)
	}
}

func TestFlatCount_Enum(t *testing.T) {
	enum := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}},
		},
	}
	count := FlatCount(enum)
	if count != 1 {
		t.Errorf("expected 1 for enum, got %d", count)
	}
}

func TestFlatCount_Flags(t *testing.T) {
	flags := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: make([]wit.Flag, 16),
		},
	}
	count := FlatCount(flags)
	if count != 1 {
		t.Errorf("expected 1 for flags with <= 32 flags, got %d", count)
	}

	// Flags with > 32 flags need 2 i32 values
	bigFlags := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: make([]wit.Flag, 33),
		},
	}
	count = FlatCount(bigFlags)
	if count != 2 {
		t.Errorf("expected 2 for flags with > 32 flags, got %d", count)
	}
}

func TestTotalFlatCount(t *testing.T) {
	types := []wit.Type{
		wit.U32{},
		wit.U32{},
		wit.String{}, // 2
	}
	count := TotalFlatCount(types)
	if count != 4 {
		t.Errorf("expected 4, got %d", count)
	}
}

func TestTotalFlatCount_Nil(t *testing.T) {
	count := TotalFlatCount(nil)
	if count != 0 {
		t.Errorf("expected 0 for nil types, got %d", count)
	}
}

func TestUsesRetptr(t *testing.T) {
	tests := []struct {
		name     string
		types    []wit.Type
		expected bool
	}{
		{"empty", nil, false},
		{"single i32", []wit.Type{wit.U32{}}, false},
		{"single i64", []wit.Type{wit.U64{}}, false},
		{"single string", []wit.Type{wit.String{}}, true}, // 2 > 1
		{"two i32", []wit.Type{wit.U32{}, wit.U32{}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UsesRetptr(tt.types)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
