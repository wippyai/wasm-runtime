package abi

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestGetFlatCount(t *testing.T) {
	tests := []struct {
		typ  wit.Type
		name string
		want int
	}{
		// Primitives
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

		// String
		{wit.String{}, "string", 2},

		// List
		{&wit.TypeDef{Kind: &wit.List{Type: wit.U32{}}}, "list<u32>", 2},
		{&wit.TypeDef{Kind: &wit.List{Type: wit.String{}}}, "list<string>", 2},

		// Record
		{&wit.TypeDef{Kind: &wit.Record{}}, "empty record", 0},
		{&wit.TypeDef{Kind: &wit.Record{
			Fields: []wit.Field{{Name: "a", Type: wit.U32{}}},
		}}, "record{a: u32}", 1},
		{&wit.TypeDef{Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.U64{}},
			},
		}}, "record{a: u32, b: u64}", 2},
		{&wit.TypeDef{Kind: &wit.Record{
			Fields: []wit.Field{{Name: "a", Type: wit.String{}}},
		}}, "record{a: string}", 2},
		{&wit.TypeDef{Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.String{}},
			},
		}}, "record{a: u32, b: string}", 3},

		// Tuple
		{&wit.TypeDef{Kind: &wit.Tuple{}}, "empty tuple", 0},
		{&wit.TypeDef{Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}},
		}}, "tuple<u32>", 1},
		{&wit.TypeDef{Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U64{}},
		}}, "tuple<u32, u64>", 2},
		{&wit.TypeDef{Kind: &wit.Tuple{
			Types: []wit.Type{wit.String{}, wit.U32{}},
		}}, "tuple<string, u32>", 3},

		// Option
		{&wit.TypeDef{Kind: &wit.Option{Type: wit.U32{}}}, "option<u32>", 2},
		{&wit.TypeDef{Kind: &wit.Option{Type: wit.String{}}}, "option<string>", 3},

		// Enum
		{&wit.TypeDef{Kind: &wit.Enum{
			Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}},
		}}, "enum", 1},

		// Flags
		{&wit.TypeDef{Kind: &wit.Flags{
			Flags: []wit.Flag{{Name: "read"}, {Name: "write"}},
		}}, "flags", 1},

		// Result
		{&wit.TypeDef{Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		}}, "result", 3},

		// Variant
		{&wit.TypeDef{Kind: &wit.Variant{
			Cases: []wit.Case{{Name: "a", Type: wit.U32{}}, {Name: "b"}},
		}}, "variant", 2},

		// Type alias
		{&wit.TypeDef{Kind: wit.U32{}}, "type alias", 1},

		// Nested record
		{&wit.TypeDef{Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "inner", Type: &wit.TypeDef{Kind: &wit.Record{
					Fields: []wit.Field{{Name: "x", Type: wit.U32{}}},
				}}},
			},
		}}, "nested record", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFlatCount(tt.typ)
			if got != tt.want {
				t.Errorf("GetFlatCount(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestDiscriminantSize(t *testing.T) {
	tests := []struct {
		numCases int
		want     uint32
	}{
		{0, 1},
		{1, 1},
		{2, 1},
		{255, 1},
		{256, 1},
		{257, 2},
		{65535, 2},
		{65536, 2},
		{65537, 4},
		{1000000, 4},
	}

	for _, tt := range tests {
		got := DiscriminantSize(tt.numCases)
		if got != tt.want {
			t.Errorf("DiscriminantSize(%d) = %d, want %d", tt.numCases, got, tt.want)
		}
	}
}

func TestDiscriminantAlign(t *testing.T) {
	tests := []struct {
		numCases int
		want     uint32
	}{
		{1, 1},
		{256, 1},
		{257, 2},
		{65536, 2},
		{65537, 4},
	}

	for _, tt := range tests {
		got := DiscriminantAlign(tt.numCases)
		if got != tt.want {
			t.Errorf("DiscriminantAlign(%d) = %d, want %d", tt.numCases, got, tt.want)
		}
	}
}
