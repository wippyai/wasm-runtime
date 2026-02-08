package engine

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestFlatCountPrimitives(t *testing.T) {
	tests := []struct {
		typ      wit.Type
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
			got := flatCount(tt.typ)
			if got != tt.expected {
				t.Errorf("flatCount(%s) = %d, want %d", tt.name, got, tt.expected)
			}
		})
	}
}

func TestFlatCountTypeDef(t *testing.T) {
	// List
	listType := &wit.TypeDef{Kind: &wit.List{Type: wit.U32{}}}
	if got := flatCount(listType); got != 2 {
		t.Errorf("flatCount(list<u32>) = %d, want 2", got)
	}

	// Record with two u32 fields
	recordType := &wit.TypeDef{Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "a", Type: wit.U32{}},
			{Name: "b", Type: wit.U32{}},
		},
	}}
	if got := flatCount(recordType); got != 2 {
		t.Errorf("flatCount(record{u32,u32}) = %d, want 2", got)
	}

	// Tuple
	tupleType := &wit.TypeDef{Kind: &wit.Tuple{
		Types: []wit.Type{wit.U32{}, wit.U64{}, wit.U32{}},
	}}
	if got := flatCount(tupleType); got != 3 {
		t.Errorf("flatCount(tuple<u32,u64,u32>) = %d, want 3", got)
	}

	// Option
	optionType := &wit.TypeDef{Kind: &wit.Option{Type: wit.U32{}}}
	if got := flatCount(optionType); got != 2 {
		t.Errorf("flatCount(option<u32>) = %d, want 2 (1 discriminant + 1 payload)", got)
	}

	// Result with ok and err
	resultType := &wit.TypeDef{Kind: &wit.Result{
		OK:  wit.U32{},
		Err: wit.U64{},
	}}
	if got := flatCount(resultType); got != 2 {
		t.Errorf("flatCount(result<u32,u64>) = %d, want 2 (1 discriminant + max(1,1))", got)
	}

	// Result with no payload
	resultEmpty := &wit.TypeDef{Kind: &wit.Result{}}
	if got := flatCount(resultEmpty); got != 1 {
		t.Errorf("flatCount(result<_,_>) = %d, want 1", got)
	}

	// Variant
	variantType := &wit.TypeDef{Kind: &wit.Variant{
		Cases: []wit.Case{
			{Name: "none", Type: nil},
			{Name: "some", Type: wit.String{}},
		},
	}}
	if got := flatCount(variantType); got != 3 {
		t.Errorf("flatCount(variant{none,some(string)}) = %d, want 3 (1 + 2)", got)
	}

	// Enum
	enumType := &wit.TypeDef{Kind: &wit.Enum{
		Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}},
	}}
	if got := flatCount(enumType); got != 1 {
		t.Errorf("flatCount(enum) = %d, want 1", got)
	}

	// Flags
	flagsType := &wit.TypeDef{Kind: &wit.Flags{
		Flags: []wit.Flag{{Name: "read"}, {Name: "write"}},
	}}
	if got := flatCount(flagsType); got != 1 {
		t.Errorf("flatCount(flags) = %d, want 1", got)
	}
}

func TestFlatResultCount(t *testing.T) {
	tests := []struct {
		name     string
		types    []wit.Type
		expected int
	}{
		{"empty", nil, 0},
		{"single u32", []wit.Type{wit.U32{}}, 1},
		{"string", []wit.Type{wit.String{}}, 2},
		{"u32 and string", []wit.Type{wit.U32{}, wit.String{}}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flatResultCount(tt.types)
			if got != tt.expected {
				t.Errorf("flatResultCount = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestUsesRetptr(t *testing.T) {
	// Single u32 - fits in single return
	if usesRetptr([]wit.Type{wit.U32{}}) {
		t.Error("single u32 should not use retptr")
	}

	// String - 2 flat values, needs retptr (MaxFlatResults=1)
	if !usesRetptr([]wit.Type{wit.String{}}) {
		t.Error("string should use retptr")
	}

	// Empty - no results
	if usesRetptr(nil) {
		t.Error("empty results should not use retptr")
	}
}

func TestResultSize(t *testing.T) {
	tests := []struct {
		typ      wit.Type
		name     string
		expected uint32
	}{
		{wit.String{}, "string", 8},
		{wit.Bool{}, "bool", 1},
		{wit.U8{}, "u8", 1},
		{wit.S8{}, "s8", 1},
		{wit.U16{}, "u16", 2},
		{wit.S16{}, "s16", 2},
		{wit.U32{}, "u32", 4},
		{wit.S32{}, "s32", 4},
		{wit.F32{}, "f32", 4},
		{wit.Char{}, "char", 4},
		{wit.U64{}, "u64", 8},
		{wit.S64{}, "s64", 8},
		{wit.F64{}, "f64", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resultSize(tt.typ)
			if got != tt.expected {
				t.Errorf("resultSize(%s) = %d, want %d", tt.name, got, tt.expected)
			}
		})
	}
}

func TestResultSizeTypeDef(t *testing.T) {
	// Record
	recordType := &wit.TypeDef{Kind: &wit.Record{
		Fields: []wit.Field{
			{Name: "a", Type: wit.U32{}},
			{Name: "b", Type: wit.U32{}},
		},
	}}
	size := resultSize(recordType)
	if size == 0 {
		t.Error("resultSize(record) should not be 0")
	}
}
