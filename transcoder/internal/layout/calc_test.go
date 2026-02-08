package layout

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestCalculatePrimitives(t *testing.T) {
	c := NewCalculator()

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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := c.Calculate(tc.typ)
			if info.Size != tc.size {
				t.Errorf("size: got %d, want %d", info.Size, tc.size)
			}
			if info.Align != tc.align {
				t.Errorf("align: got %d, want %d", info.Align, tc.align)
			}
		})
	}
}

func TestCalculateRecord(t *testing.T) {
	c := NewCalculator()

	t.Run("empty", func(t *testing.T) {
		record := &wit.Record{Fields: []wit.Field{}}
		typedef := &wit.TypeDef{Kind: record}
		info := c.Calculate(typedef)
		if info.Size != 0 {
			t.Errorf("size: got %d, want 0", info.Size)
		}
	})

	t.Run("single_u32", func(t *testing.T) {
		record := &wit.Record{
			Fields: []wit.Field{{Name: "x", Type: wit.U32{}}},
		}
		typedef := &wit.TypeDef{Kind: record}
		info := c.Calculate(typedef)
		if info.Size != 4 {
			t.Errorf("size: got %d, want 4", info.Size)
		}
		if info.Align != 4 {
			t.Errorf("align: got %d, want 4", info.Align)
		}
		if info.FieldOffs["x"] != 0 {
			t.Errorf("field x offset: got %d, want 0", info.FieldOffs["x"])
		}
	})

	t.Run("mixed_alignment", func(t *testing.T) {
		record := &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U8{}},
				{Name: "b", Type: wit.U32{}},
				{Name: "c", Type: wit.U8{}},
			},
		}
		typedef := &wit.TypeDef{Kind: record}
		info := c.Calculate(typedef)

		if info.FieldOffs["a"] != 0 {
			t.Errorf("field a offset: got %d, want 0", info.FieldOffs["a"])
		}
		if info.FieldOffs["b"] != 4 {
			t.Errorf("field b offset: got %d, want 4", info.FieldOffs["b"])
		}
		if info.FieldOffs["c"] != 8 {
			t.Errorf("field c offset: got %d, want 8", info.FieldOffs["c"])
		}
		if info.Size != 12 {
			t.Errorf("size: got %d, want 12", info.Size)
		}
		if info.Align != 4 {
			t.Errorf("align: got %d, want 4", info.Align)
		}
	})

	t.Run("u64_alignment", func(t *testing.T) {
		record := &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U8{}},
				{Name: "b", Type: wit.U64{}},
			},
		}
		typedef := &wit.TypeDef{Kind: record}
		info := c.Calculate(typedef)

		if info.FieldOffs["a"] != 0 {
			t.Errorf("field a offset: got %d, want 0", info.FieldOffs["a"])
		}
		if info.FieldOffs["b"] != 8 {
			t.Errorf("field b offset: got %d, want 8", info.FieldOffs["b"])
		}
		if info.Size != 16 {
			t.Errorf("size: got %d, want 16", info.Size)
		}
		if info.Align != 8 {
			t.Errorf("align: got %d, want 8", info.Align)
		}
	})
}

func TestCalculateList(t *testing.T) {
	c := NewCalculator()

	list := &wit.List{Type: wit.U32{}}
	typedef := &wit.TypeDef{Kind: list}
	info := c.Calculate(typedef)

	if info.Size != 8 {
		t.Errorf("size: got %d, want 8", info.Size)
	}
	if info.Align != 4 {
		t.Errorf("align: got %d, want 4", info.Align)
	}
}

func TestCalculateTuple(t *testing.T) {
	c := NewCalculator()

	t.Run("empty", func(t *testing.T) {
		tuple := &wit.Tuple{Types: []wit.Type{}}
		typedef := &wit.TypeDef{Kind: tuple}
		info := c.Calculate(typedef)
		if info.Size != 0 {
			t.Errorf("size: got %d, want 0", info.Size)
		}
	})

	t.Run("two_u32", func(t *testing.T) {
		tuple := &wit.Tuple{Types: []wit.Type{wit.U32{}, wit.U32{}}}
		typedef := &wit.TypeDef{Kind: tuple}
		info := c.Calculate(typedef)

		if info.Size != 8 {
			t.Errorf("size: got %d, want 8", info.Size)
		}
		if info.Align != 4 {
			t.Errorf("align: got %d, want 4", info.Align)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		tuple := &wit.Tuple{Types: []wit.Type{wit.U8{}, wit.U64{}, wit.U8{}}}
		typedef := &wit.TypeDef{Kind: tuple}
		info := c.Calculate(typedef)

		if info.Size != 24 {
			t.Errorf("size: got %d, want 24", info.Size)
		}
		if info.Align != 8 {
			t.Errorf("align: got %d, want 8", info.Align)
		}
	})
}

func TestCalculateEnum(t *testing.T) {
	c := NewCalculator()

	tests := []struct {
		name      string
		numCases  int
		wantSize  uint32
		wantAlign uint32
	}{
		{"1_case", 1, 1, 1},
		{"255_cases", 255, 1, 1},
		{"256_cases", 256, 1, 1},
		{"257_cases", 257, 2, 2},
		{"65535_cases", 65535, 2, 2},
		{"65536_cases", 65536, 2, 2},
		{"65537_cases", 65537, 4, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cases := make([]wit.EnumCase, tc.numCases)
			for i := range cases {
				cases[i] = wit.EnumCase{Name: "case"}
			}
			enum := &wit.Enum{Cases: cases}
			typedef := &wit.TypeDef{Kind: enum}
			info := c.Calculate(typedef)

			if info.Size != tc.wantSize {
				t.Errorf("size: got %d, want %d", info.Size, tc.wantSize)
			}
			if info.Align != tc.wantAlign {
				t.Errorf("align: got %d, want %d", info.Align, tc.wantAlign)
			}
		})
	}
}

func TestCalculateFlags(t *testing.T) {
	c := NewCalculator()

	tests := []struct {
		name      string
		numFlags  int
		wantSize  uint32
		wantAlign uint32
	}{
		{"0_flags", 0, 0, 1},
		{"1_flag", 1, 1, 1},
		{"8_flags", 8, 1, 1},
		{"9_flags", 9, 2, 2},
		{"16_flags", 16, 2, 2},
		{"17_flags", 17, 4, 4},
		{"32_flags", 32, 4, 4},
		{"33_flags", 33, 8, 8},
		{"64_flags", 64, 8, 8},
		{"65_flags", 65, 12, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flags := make([]wit.Flag, tc.numFlags)
			for i := range flags {
				flags[i] = wit.Flag{Name: "flag"}
			}
			witFlags := &wit.Flags{Flags: flags}
			typedef := &wit.TypeDef{Kind: witFlags}
			info := c.Calculate(typedef)

			if info.Size != tc.wantSize {
				t.Errorf("size: got %d, want %d", info.Size, tc.wantSize)
			}
			if info.Align != tc.wantAlign {
				t.Errorf("align: got %d, want %d", info.Align, tc.wantAlign)
			}
		})
	}
}

func TestCalculateOption(t *testing.T) {
	c := NewCalculator()

	t.Run("option_u8", func(t *testing.T) {
		option := &wit.Option{Type: wit.U8{}}
		typedef := &wit.TypeDef{Kind: option}
		info := c.Calculate(typedef)

		if info.Size != 2 {
			t.Errorf("size: got %d, want 2", info.Size)
		}
		if info.Align != 1 {
			t.Errorf("align: got %d, want 1", info.Align)
		}
	})

	t.Run("option_u32", func(t *testing.T) {
		option := &wit.Option{Type: wit.U32{}}
		typedef := &wit.TypeDef{Kind: option}
		info := c.Calculate(typedef)

		if info.Size != 8 {
			t.Errorf("size: got %d, want 8", info.Size)
		}
		if info.Align != 4 {
			t.Errorf("align: got %d, want 4", info.Align)
		}
	})

	t.Run("option_u64", func(t *testing.T) {
		option := &wit.Option{Type: wit.U64{}}
		typedef := &wit.TypeDef{Kind: option}
		info := c.Calculate(typedef)

		if info.Size != 16 {
			t.Errorf("size: got %d, want 16", info.Size)
		}
		if info.Align != 8 {
			t.Errorf("align: got %d, want 8", info.Align)
		}
	})
}

func TestCalculateResult(t *testing.T) {
	c := NewCalculator()

	t.Run("result_u32_string", func(t *testing.T) {
		result := &wit.Result{OK: wit.U32{}, Err: wit.String{}}
		typedef := &wit.TypeDef{Kind: result}
		info := c.Calculate(typedef)

		if info.Align != 4 {
			t.Errorf("align: got %d, want 4", info.Align)
		}
		if info.Size != 12 {
			t.Errorf("size: got %d, want 12", info.Size)
		}
	})

	t.Run("result_unit_unit", func(t *testing.T) {
		result := &wit.Result{OK: nil, Err: nil}
		typedef := &wit.TypeDef{Kind: result}
		info := c.Calculate(typedef)

		if info.Size != 1 {
			t.Errorf("size: got %d, want 1", info.Size)
		}
		if info.Align != 1 {
			t.Errorf("align: got %d, want 1", info.Align)
		}
	})
}

func TestCalculateVariant(t *testing.T) {
	c := NewCalculator()

	t.Run("empty", func(t *testing.T) {
		variant := &wit.Variant{Cases: []wit.Case{}}
		typedef := &wit.TypeDef{Kind: variant}
		info := c.Calculate(typedef)

		if info.Size != 0 {
			t.Errorf("size: got %d, want 0", info.Size)
		}
	})

	t.Run("unit_cases", func(t *testing.T) {
		variant := &wit.Variant{
			Cases: []wit.Case{
				{Name: "a", Type: nil},
				{Name: "b", Type: nil},
			},
		}
		typedef := &wit.TypeDef{Kind: variant}
		info := c.Calculate(typedef)

		if info.Size != 1 {
			t.Errorf("size: got %d, want 1", info.Size)
		}
		if info.Align != 1 {
			t.Errorf("align: got %d, want 1", info.Align)
		}
	})

	t.Run("with_payload", func(t *testing.T) {
		variant := &wit.Variant{
			Cases: []wit.Case{
				{Name: "none", Type: nil},
				{Name: "some", Type: wit.U32{}},
			},
		}
		typedef := &wit.TypeDef{Kind: variant}
		info := c.Calculate(typedef)

		if info.Align != 4 {
			t.Errorf("align: got %d, want 4", info.Align)
		}
		if info.Size != 8 {
			t.Errorf("size: got %d, want 8", info.Size)
		}
	})
}

func TestCaching(t *testing.T) {
	c := NewCalculator()

	record := &wit.Record{
		Fields: []wit.Field{{Name: "x", Type: wit.U32{}}},
	}
	typedef := &wit.TypeDef{Kind: record}

	info1 := c.Calculate(typedef)
	info2 := c.Calculate(typedef)

	if info1.Size != info2.Size {
		t.Error("cached results should be identical")
	}
}

func TestNestedTypes(t *testing.T) {
	c := NewCalculator()

	innerRecord := &wit.Record{
		Fields: []wit.Field{
			{Name: "a", Type: wit.U32{}},
			{Name: "b", Type: wit.U64{}},
		},
	}
	innerTypedef := &wit.TypeDef{Kind: innerRecord}

	outerRecord := &wit.Record{
		Fields: []wit.Field{
			{Name: "inner", Type: innerTypedef},
			{Name: "flag", Type: wit.Bool{}},
		},
	}
	outerTypedef := &wit.TypeDef{Kind: outerRecord}

	info := c.Calculate(outerTypedef)

	if info.FieldOffs["inner"] != 0 {
		t.Errorf("inner offset: got %d, want 0", info.FieldOffs["inner"])
	}
	if info.FieldOffs["flag"] != 16 {
		t.Errorf("flag offset: got %d, want 16", info.FieldOffs["flag"])
	}
	if info.Size != 24 {
		t.Errorf("size: got %d, want 24", info.Size)
	}
}

func TestTypeAlias(t *testing.T) {
	c := NewCalculator()

	typedef := &wit.TypeDef{Kind: wit.U32{}}
	info := c.Calculate(typedef)

	if info.Size != 4 {
		t.Errorf("size: got %d, want 4", info.Size)
	}
	if info.Align != 4 {
		t.Errorf("align: got %d, want 4", info.Align)
	}
}
