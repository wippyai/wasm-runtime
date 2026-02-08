package component

import (
	"bytes"
	"testing"
)

// Helper functions for constructing binary test data

func makeLEB128(n uint32) []byte {
	var buf []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if n == 0 {
			break
		}
	}
	return buf
}

func makeString(s string) []byte {
	return append(makeLEB128(uint32(len(s))), []byte(s)...)
}

// Section 1: Primitive Type Tests (0x7f - 0x73, 0x64)

func TestPrimValType_AllPrimitives_Spec(t *testing.T) {
	tests := []struct {
		name     string
		input    byte
		expected PrimType
	}{
		{"bool", 0x7f, PrimBool},
		{"s8", 0x7e, PrimS8},
		{"u8", 0x7d, PrimU8},
		{"s16", 0x7c, PrimS16},
		{"u16", 0x7b, PrimU16},
		{"s32", 0x7a, PrimS32},
		{"u32", 0x79, PrimU32},
		{"s64", 0x78, PrimS64},
		{"u64", 0x77, PrimU64},
		{"f32", 0x76, PrimF32},
		{"f64", 0x75, PrimF64},
		{"char", 0x74, PrimChar},
		{"string", 0x73, PrimString},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader([]byte{tc.input})
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			prim, ok := got.(PrimValType)
			if !ok {
				t.Fatalf("expected PrimValType, got %T", got)
			}
			if prim.Type != tc.expected {
				t.Errorf("PrimType = 0x%02x, want 0x%02x", prim.Type, tc.expected)
			}
		})
	}
}

// Section 2: Record Type Tests (0x72)

func TestRecordType_Basic_Spec(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		fields  int
		wantErr bool
	}{
		{
			name:    "single_field_bool",
			input:   append([]byte{0x72, 0x01}, append(makeString("x"), 0x7f)...),
			fields:  1,
			wantErr: false,
		},
		{
			name: "two_fields_mixed",
			input: func() []byte {
				buf := []byte{0x72, 0x02}
				buf = append(buf, makeString("a")...)
				buf = append(buf, 0x79) // u32
				buf = append(buf, makeString("b")...)
				buf = append(buf, 0x73) // string
				return buf
			}(),
			fields:  2,
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			got, err := parseValType(r)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			rec, ok := got.(RecordType)
			if !ok {
				t.Fatalf("expected RecordType, got %T", got)
			}
			if len(rec.Fields) != tc.fields {
				t.Errorf("field count = %d, want %d", len(rec.Fields), tc.fields)
			}
		})
	}
}

func TestRecordType_Nested_Spec(t *testing.T) {
	// record { inner: record { x: u32 } }
	innerRecord := func() []byte {
		buf := []byte{0x72, 0x01}
		buf = append(buf, makeString("x")...)
		buf = append(buf, 0x79) // u32
		return buf
	}()

	outerRecord := func() []byte {
		buf := []byte{0x72, 0x01}
		buf = append(buf, makeString("inner")...)
		buf = append(buf, innerRecord...)
		return buf
	}()

	r := bytes.NewReader(outerRecord)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec, ok := got.(RecordType)
	if !ok {
		t.Fatalf("expected RecordType, got %T", got)
	}
	if len(rec.Fields) != 1 {
		t.Errorf("field count = %d, want 1", len(rec.Fields))
	}
	if rec.Fields[0].Name != "inner" {
		t.Errorf("field name = %q, want %q", rec.Fields[0].Name, "inner")
	}
	innerRec, ok := rec.Fields[0].Type.(RecordType)
	if !ok {
		t.Fatalf("expected inner RecordType, got %T", rec.Fields[0].Type)
	}
	if len(innerRec.Fields) != 1 {
		t.Errorf("inner field count = %d, want 1", len(innerRec.Fields))
	}
}

func TestRecordType_WithList_Spec(t *testing.T) {
	// record { items: list<u8> }
	buf := []byte{0x72, 0x01}
	buf = append(buf, makeString("items")...)
	buf = append(buf, 0x70, 0x7d) // list<u8>

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec, ok := got.(RecordType)
	if !ok {
		t.Fatalf("expected RecordType, got %T", got)
	}
	_, ok = rec.Fields[0].Type.(ListType)
	if !ok {
		t.Fatalf("expected list field, got %T", rec.Fields[0].Type)
	}
}

func TestRecordType_WithOption_Spec(t *testing.T) {
	// record { maybe: option<string> }
	buf := []byte{0x72, 0x01}
	buf = append(buf, makeString("maybe")...)
	buf = append(buf, 0x6b, 0x73) // option<string>

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec, ok := got.(RecordType)
	if !ok {
		t.Fatalf("expected RecordType, got %T", got)
	}
	_, ok = rec.Fields[0].Type.(OptionType)
	if !ok {
		t.Fatalf("expected option field, got %T", rec.Fields[0].Type)
	}
}

func TestRecordType_WithResult_Spec(t *testing.T) {
	// record { res: result<u32, string> }
	buf := []byte{0x72, 0x01}
	buf = append(buf, makeString("res")...)
	buf = append(buf, 0x6a, 0x01, 0x79, 0x01, 0x73) // result<u32, string>

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec, ok := got.(RecordType)
	if !ok {
		t.Fatalf("expected RecordType, got %T", got)
	}
	_, ok = rec.Fields[0].Type.(ResultType)
	if !ok {
		t.Fatalf("expected result field, got %T", rec.Fields[0].Type)
	}
}

func TestRecordType_DeeplyNested_Spec(t *testing.T) {
	// record { a: record { b: record { c: u32 } } }
	innermost := func() []byte {
		buf := []byte{0x72, 0x01}
		buf = append(buf, makeString("c")...)
		buf = append(buf, 0x79)
		return buf
	}()

	middle := func() []byte {
		buf := []byte{0x72, 0x01}
		buf = append(buf, makeString("b")...)
		buf = append(buf, innermost...)
		return buf
	}()

	outer := func() []byte {
		buf := []byte{0x72, 0x01}
		buf = append(buf, makeString("a")...)
		buf = append(buf, middle...)
		return buf
	}()

	r := bytes.NewReader(outer)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec1, ok := got.(RecordType)
	if !ok {
		t.Fatalf("expected RecordType, got %T", got)
	}
	rec2, ok := rec1.Fields[0].Type.(RecordType)
	if !ok {
		t.Fatalf("expected nested RecordType, got %T", rec1.Fields[0].Type)
	}
	rec3, ok := rec2.Fields[0].Type.(RecordType)
	if !ok {
		t.Fatalf("expected deeply nested RecordType, got %T", rec2.Fields[0].Type)
	}
	if rec3.Fields[0].Name != "c" {
		t.Errorf("innermost field name = %q, want %q", rec3.Fields[0].Name, "c")
	}
}

// Section 3: List Type Tests (0x70)

func TestListType_AllPrimitives_Spec(t *testing.T) {
	primitives := []struct {
		name string
		elem byte
	}{
		{"list_bool", 0x7f},
		{"list_s8", 0x7e},
		{"list_u8", 0x7d},
		{"list_s16", 0x7c},
		{"list_u16", 0x7b},
		{"list_s32", 0x7a},
		{"list_u32", 0x79},
		{"list_s64", 0x78},
		{"list_u64", 0x77},
		{"list_f32", 0x76},
		{"list_f64", 0x75},
		{"list_char", 0x74},
		{"list_string", 0x73},
	}

	for _, tc := range primitives {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader([]byte{0x70, tc.elem})
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			list, ok := got.(ListType)
			if !ok {
				t.Fatalf("expected ListType, got %T", got)
			}
			prim, ok := list.ElemType.(PrimValType)
			if !ok {
				t.Fatalf("expected PrimValType element, got %T", list.ElemType)
			}
			if prim.Type != PrimType(tc.elem) {
				t.Errorf("element type = 0x%02x, want 0x%02x", prim.Type, tc.elem)
			}
		})
	}
}

func TestListType_Nested_Spec(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		depth int
	}{
		{"list_of_list", []byte{0x70, 0x70, 0x7d}, 2},
		{"list_3_deep", []byte{0x70, 0x70, 0x70, 0x7f}, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			current := got
			for i := 0; i < tc.depth; i++ {
				list, ok := current.(ListType)
				if !ok {
					t.Fatalf("depth %d: expected ListType, got %T", i, current)
				}
				current = list.ElemType
			}
		})
	}
}

func TestListType_OfRecord_Spec(t *testing.T) {
	// list<record { x: u32 }>
	record := func() []byte {
		buf := []byte{0x72, 0x01}
		buf = append(buf, makeString("x")...)
		buf = append(buf, 0x79)
		return buf
	}()

	buf := append([]byte{0x70}, record...)
	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, ok := got.(ListType)
	if !ok {
		t.Fatalf("expected ListType, got %T", got)
	}
	_, ok = list.ElemType.(RecordType)
	if !ok {
		t.Fatalf("expected RecordType element, got %T", list.ElemType)
	}
}

func TestListType_OfOption_Spec(t *testing.T) {
	// list<option<u32>>
	r := bytes.NewReader([]byte{0x70, 0x6b, 0x79})
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, ok := got.(ListType)
	if !ok {
		t.Fatalf("expected ListType, got %T", got)
	}
	_, ok = list.ElemType.(OptionType)
	if !ok {
		t.Fatalf("expected OptionType element, got %T", list.ElemType)
	}
}

func TestListType_OfResult_Spec(t *testing.T) {
	// list<result<u32, string>>
	r := bytes.NewReader([]byte{0x70, 0x6a, 0x01, 0x79, 0x01, 0x73})
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, ok := got.(ListType)
	if !ok {
		t.Fatalf("expected ListType, got %T", got)
	}
	_, ok = list.ElemType.(ResultType)
	if !ok {
		t.Fatalf("expected ResultType element, got %T", list.ElemType)
	}
}

// Section 4: Tuple Type Tests (0x6f)

func TestTupleType_Basic_Spec(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		elems int
	}{
		{"tuple_1", []byte{0x6f, 0x01, 0x7f}, 1},
		{"tuple_2_same", []byte{0x6f, 0x02, 0x79, 0x79}, 2},
		{"tuple_2_mixed", []byte{0x6f, 0x02, 0x7f, 0x73}, 2},
		{"tuple_3", []byte{0x6f, 0x03, 0x7d, 0x7b, 0x79}, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tuple, ok := got.(TupleType)
			if !ok {
				t.Fatalf("expected TupleType, got %T", got)
			}
			if len(tuple.Types) != tc.elems {
				t.Errorf("element count = %d, want %d", len(tuple.Types), tc.elems)
			}
		})
	}
}

func TestTupleType_Nested_Spec(t *testing.T) {
	// tuple<bool, tuple<u32>>
	r := bytes.NewReader([]byte{0x6f, 0x02, 0x7f, 0x6f, 0x01, 0x79})
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tuple, ok := got.(TupleType)
	if !ok {
		t.Fatalf("expected TupleType, got %T", got)
	}
	_, ok = tuple.Types[1].(TupleType)
	if !ok {
		t.Fatalf("expected nested TupleType, got %T", tuple.Types[1])
	}
}

func TestTupleType_WithComplexElements_Spec(t *testing.T) {
	// tuple<list<u8>, option<u32>>
	r := bytes.NewReader([]byte{0x6f, 0x02, 0x70, 0x7d, 0x6b, 0x79})
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tuple, ok := got.(TupleType)
	if !ok {
		t.Fatalf("expected TupleType, got %T", got)
	}
	_, ok = tuple.Types[0].(ListType)
	if !ok {
		t.Fatalf("first element: expected ListType, got %T", tuple.Types[0])
	}
	_, ok = tuple.Types[1].(OptionType)
	if !ok {
		t.Fatalf("second element: expected OptionType, got %T", tuple.Types[1])
	}
}

// Section 5: Option Type Tests (0x6b)

func TestOptionType_AllPrimitives_Spec(t *testing.T) {
	primitives := []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x7a, 0x79, 0x78, 0x77, 0x76, 0x75, 0x74, 0x73}

	for _, prim := range primitives {
		t.Run("option_"+string(prim), func(t *testing.T) {
			r := bytes.NewReader([]byte{0x6b, prim})
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			opt, ok := got.(OptionType)
			if !ok {
				t.Fatalf("expected OptionType, got %T", got)
			}
			_, ok = opt.Type.(PrimValType)
			if !ok {
				t.Fatalf("expected PrimValType inner, got %T", opt.Type)
			}
		})
	}
}

func TestOptionType_Nested_Spec(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		depth int
	}{
		{"option_of_option", []byte{0x6b, 0x6b, 0x7f}, 2},
		{"option_3_deep", []byte{0x6b, 0x6b, 0x6b, 0x79}, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			current := got
			for i := 0; i < tc.depth; i++ {
				opt, ok := current.(OptionType)
				if !ok {
					t.Fatalf("depth %d: expected OptionType, got %T", i, current)
				}
				current = opt.Type
			}
		})
	}
}

func TestOptionType_OfList_Spec(t *testing.T) {
	// option<list<u32>>
	r := bytes.NewReader([]byte{0x6b, 0x70, 0x79})
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	opt, ok := got.(OptionType)
	if !ok {
		t.Fatalf("expected OptionType, got %T", got)
	}
	_, ok = opt.Type.(ListType)
	if !ok {
		t.Fatalf("expected ListType inner, got %T", opt.Type)
	}
}

func TestOptionType_OfResult_Spec(t *testing.T) {
	// option<result<u32, string>>
	r := bytes.NewReader([]byte{0x6b, 0x6a, 0x01, 0x79, 0x01, 0x73})
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	opt, ok := got.(OptionType)
	if !ok {
		t.Fatalf("expected OptionType, got %T", got)
	}
	_, ok = opt.Type.(ResultType)
	if !ok {
		t.Fatalf("expected ResultType inner, got %T", opt.Type)
	}
}

// Section 6: Result Type Tests (0x6a)

func TestResultType_FourCombinations_Spec(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		hasOk  bool
		hasErr bool
	}{
		{"result_unit_unit", []byte{0x6a, 0x00, 0x00}, false, false},
		{"result_ok_only", []byte{0x6a, 0x01, 0x7f, 0x00}, true, false},
		{"result_err_only", []byte{0x6a, 0x00, 0x01, 0x73}, false, true},
		{"result_both", []byte{0x6a, 0x01, 0x79, 0x01, 0x73}, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			res, ok := got.(ResultType)
			if !ok {
				t.Fatalf("expected ResultType, got %T", got)
			}
			if (res.OK != nil) != tc.hasOk {
				t.Errorf("hasOk = %v, want %v", res.OK != nil, tc.hasOk)
			}
			if (res.Err != nil) != tc.hasErr {
				t.Errorf("hasErr = %v, want %v", res.Err != nil, tc.hasErr)
			}
		})
	}
}

func TestResultType_Nested_Spec(t *testing.T) {
	// result<result<u32, string>, u32>
	inner := []byte{0x6a, 0x01, 0x79, 0x01, 0x73}
	outer := append([]byte{0x6a, 0x01}, inner...)
	outer = append(outer, 0x01, 0x79)

	r := bytes.NewReader(outer)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res, ok := got.(ResultType)
	if !ok {
		t.Fatalf("expected ResultType, got %T", got)
	}
	_, ok = (*res.OK).(ResultType)
	if !ok {
		t.Fatalf("expected nested ResultType in OK, got %T", *res.OK)
	}
}

func TestResultType_ComplexOk_Spec(t *testing.T) {
	// result<list<u8>, string>
	r := bytes.NewReader([]byte{0x6a, 0x01, 0x70, 0x7d, 0x01, 0x73})
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res, ok := got.(ResultType)
	if !ok {
		t.Fatalf("expected ResultType, got %T", got)
	}
	_, ok = (*res.OK).(ListType)
	if !ok {
		t.Fatalf("expected ListType in OK, got %T", *res.OK)
	}
}

// Section 7: Own/Borrow Type Tests (0x69, 0x68) - CRITICAL

func TestOwnType_Spec(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		typeIndex uint32
	}{
		{"own_resource_0", []byte{0x69, 0x00}, 0},
		{"own_resource_1", []byte{0x69, 0x01}, 1},
		{"own_resource_127", []byte{0x69, 0x7f}, 127},
		{"own_resource_128", []byte{0x69, 0x80, 0x01}, 128},
		{"own_resource_256", []byte{0x69, 0x80, 0x02}, 256},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			own, ok := got.(OwnType)
			if !ok {
				t.Fatalf("expected OwnType, got %T", got)
			}
			if own.TypeIndex != tc.typeIndex {
				t.Errorf("TypeIndex = %d, want %d", own.TypeIndex, tc.typeIndex)
			}
		})
	}
}

func TestBorrowType_Spec(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		typeIndex uint32
	}{
		{"borrow_resource_0", []byte{0x68, 0x00}, 0},
		{"borrow_resource_1", []byte{0x68, 0x01}, 1},
		{"borrow_resource_127", []byte{0x68, 0x7f}, 127},
		{"borrow_resource_128", []byte{0x68, 0x80, 0x01}, 128},
		{"borrow_resource_256", []byte{0x68, 0x80, 0x02}, 256},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			borrow, ok := got.(BorrowType)
			if !ok {
				t.Fatalf("expected BorrowType, got %T", got)
			}
			if borrow.TypeIndex != tc.typeIndex {
				t.Errorf("TypeIndex = %d, want %d", borrow.TypeIndex, tc.typeIndex)
			}
		})
	}
}

// Section 8: Variant Type Tests (0x71)

func TestVariantType_Basic_Spec(t *testing.T) {
	// variant { none }
	buf := []byte{0x71, 0x01}
	buf = append(buf, makeString("none")...)
	buf = append(buf, 0x00, 0x00) // no type, no refines

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.(VariantType)
	if !ok {
		t.Fatalf("expected VariantType, got %T", got)
	}
	if len(v.Cases) != 1 {
		t.Errorf("case count = %d, want 1", len(v.Cases))
	}
	if v.Cases[0].Name != "none" {
		t.Errorf("case name = %q, want %q", v.Cases[0].Name, "none")
	}
}

func TestVariantType_WithPayload_Spec(t *testing.T) {
	// variant { some(bool) }
	buf := []byte{0x71, 0x01}
	buf = append(buf, makeString("some")...)
	buf = append(buf, 0x01, 0x7f, 0x00) // has type (bool), no refines

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.(VariantType)
	if !ok {
		t.Fatalf("expected VariantType, got %T", got)
	}
	if v.Cases[0].Type == nil {
		t.Error("expected case to have type")
	}
}

func TestVariantType_OptionLike_Spec(t *testing.T) {
	// variant { none, some(u32) }
	buf := []byte{0x71, 0x02}
	buf = append(buf, makeString("none")...)
	buf = append(buf, 0x00, 0x00) // no type, no refines
	buf = append(buf, makeString("some")...)
	buf = append(buf, 0x01, 0x79, 0x00) // has type (u32), no refines

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.(VariantType)
	if !ok {
		t.Fatalf("expected VariantType, got %T", got)
	}
	if len(v.Cases) != 2 {
		t.Errorf("case count = %d, want 2", len(v.Cases))
	}
}

func TestVariantType_WithComplexPayload_Spec(t *testing.T) {
	// variant { data(list<u8>) }
	buf := []byte{0x71, 0x01}
	buf = append(buf, makeString("data")...)
	buf = append(buf, 0x01, 0x70, 0x7d, 0x00) // has type (list<u8>), no refines

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.(VariantType)
	if !ok {
		t.Fatalf("expected VariantType, got %T", got)
	}
	if v.Cases[0].Type == nil {
		t.Fatal("expected case to have type")
	}
	_, ok = (*v.Cases[0].Type).(ListType)
	if !ok {
		t.Fatalf("expected ListType payload, got %T", *v.Cases[0].Type)
	}
}

// Section 9: Flags Type Tests (0x6e)

func TestFlagsType_Basic_Spec(t *testing.T) {
	// flags { a, b, c }
	buf := []byte{0x6e, 0x03}
	buf = append(buf, makeString("a")...)
	buf = append(buf, makeString("b")...)
	buf = append(buf, makeString("c")...)

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	flags, ok := got.(FlagsType)
	if !ok {
		t.Fatalf("expected FlagsType, got %T", got)
	}
	if len(flags.Names) != 3 {
		t.Errorf("flag count = %d, want 3", len(flags.Names))
	}
}

// Section 10: Enum Type Tests (0x6d)

func TestEnumType_Basic_Spec(t *testing.T) {
	// enum { red, green, blue }
	buf := []byte{0x6d, 0x03}
	buf = append(buf, makeString("red")...)
	buf = append(buf, makeString("green")...)
	buf = append(buf, makeString("blue")...)

	r := bytes.NewReader(buf)
	got, err := parseValType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	enum, ok := got.(EnumType)
	if !ok {
		t.Fatalf("expected EnumType, got %T", got)
	}
	if len(enum.Cases) != 3 {
		t.Errorf("case count = %d, want 3", len(enum.Cases))
	}
}

// Section 11: Function Type Tests (0x40)

func TestFuncType_VoidVoid_Spec(t *testing.T) {
	// () -> () - no params, no result (0x01 discriminant + 0x00 end marker)
	r := bytes.NewReader([]byte{0x00, 0x01, 0x00})
	got, err := parseFuncType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Params) != 0 {
		t.Errorf("param count = %d, want 0", len(got.Params))
	}
	if got.Result != nil {
		t.Error("expected nil result for void function")
	}
}

func TestFuncType_VoidU32_Spec(t *testing.T) {
	// () -> u32 - no params, single result (0x00 discriminant + valtype)
	r := bytes.NewReader([]byte{0x00, 0x00, 0x79})
	got, err := parseFuncType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Params) != 0 {
		t.Errorf("param count = %d, want 0", len(got.Params))
	}
	if got.Result == nil {
		t.Fatal("expected result")
	}
}

func TestFuncType_WithParams_Spec(t *testing.T) {
	// (x: u32) -> ()
	buf := []byte{0x01} // 1 param
	buf = append(buf, makeString("x")...)
	buf = append(buf, 0x79)       // u32
	buf = append(buf, 0x01, 0x00) // no result

	r := bytes.NewReader(buf)
	got, err := parseFuncType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Params) != 1 {
		t.Errorf("param count = %d, want 1", len(got.Params))
	}
	if got.Params[0].Name != "x" {
		t.Errorf("param name = %q, want %q", got.Params[0].Name, "x")
	}
}

func TestFuncType_WithComplexParam_Spec(t *testing.T) {
	// (data: list<u8>) -> ()
	buf := []byte{0x01} // 1 param
	buf = append(buf, makeString("data")...)
	buf = append(buf, 0x70, 0x7d) // list<u8>
	buf = append(buf, 0x01, 0x00) // no result

	r := bytes.NewReader(buf)
	got, err := parseFuncType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := got.Params[0].Type.(ListType)
	if !ok {
		t.Fatalf("expected ListType param, got %T", got.Params[0].Type)
	}
}

func TestFuncType_WithComplexResult_Spec(t *testing.T) {
	// () -> result<u32, string>
	buf := []byte{0x00, 0x00, 0x6a, 0x01, 0x79, 0x01, 0x73} // 0 params, result discriminant, result type

	r := bytes.NewReader(buf)
	got, err := parseFuncType(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := (*got.Result).(ResultType)
	if !ok {
		t.Fatalf("expected ResultType result, got %T", *got.Result)
	}
}

// Section 12: Validation Tests - Truncated Data

func TestTruncated_List_Spec(t *testing.T) {
	r := bytes.NewReader([]byte{0x70})
	_, err := parseValType(r)
	if err == nil {
		t.Error("expected error for truncated list")
	}
}

func TestTruncated_Option_Spec(t *testing.T) {
	r := bytes.NewReader([]byte{0x6b})
	_, err := parseValType(r)
	if err == nil {
		t.Error("expected error for truncated option")
	}
}

func TestTruncated_Own_Spec(t *testing.T) {
	r := bytes.NewReader([]byte{0x69})
	_, err := parseValType(r)
	if err == nil {
		t.Error("expected error for truncated own")
	}
}

func TestTruncated_Borrow_Spec(t *testing.T) {
	r := bytes.NewReader([]byte{0x68})
	_, err := parseValType(r)
	if err == nil {
		t.Error("expected error for truncated borrow")
	}
}

// Section 13: Deep Nesting Stress Tests

func TestDeepNesting_List_Spec(t *testing.T) {
	depths := []int{10, 50, 100}

	for _, depth := range depths {
		t.Run("depth_"+string(rune('0'+depth%10)), func(t *testing.T) {
			buf := make([]byte, depth+1)
			for i := 0; i < depth; i++ {
				buf[i] = 0x70 // list
			}
			buf[depth] = 0x7f // bool at innermost

			r := bytes.NewReader(buf)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error at depth %d: %v", depth, err)
			}

			current := got
			for i := 0; i < depth; i++ {
				list, ok := current.(ListType)
				if !ok {
					t.Fatalf("depth %d: expected ListType, got %T", i, current)
				}
				current = list.ElemType
			}
		})
	}
}

func TestDeepNesting_Option_Spec(t *testing.T) {
	depths := []int{10, 50, 100}

	for _, depth := range depths {
		t.Run("depth_"+string(rune('0'+depth%10)), func(t *testing.T) {
			buf := make([]byte, depth+1)
			for i := 0; i < depth; i++ {
				buf[i] = 0x6b // option
			}
			buf[depth] = 0x79 // u32 at innermost

			r := bytes.NewReader(buf)
			got, err := parseValType(r)
			if err != nil {
				t.Fatalf("unexpected error at depth %d: %v", depth, err)
			}

			current := got
			for i := 0; i < depth; i++ {
				opt, ok := current.(OptionType)
				if !ok {
					t.Fatalf("depth %d: expected OptionType, got %T", i, current)
				}
				current = opt.Type
			}
		})
	}
}
