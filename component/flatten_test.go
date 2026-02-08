package component

import (
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component/internal/arena"
	"go.bytecodealliance.org/wit"
)

func TestFlattenType_Primitives(t *testing.T) {
	tests := []struct {
		name     string
		typ      wit.Type
		expected []CoreValType
	}{
		{"bool", wit.Bool{}, []CoreValType{api.ValueTypeI32}},
		{"u8", wit.U8{}, []CoreValType{api.ValueTypeI32}},
		{"u16", wit.U16{}, []CoreValType{api.ValueTypeI32}},
		{"u32", wit.U32{}, []CoreValType{api.ValueTypeI32}},
		{"s8", wit.S8{}, []CoreValType{api.ValueTypeI32}},
		{"s16", wit.S16{}, []CoreValType{api.ValueTypeI32}},
		{"s32", wit.S32{}, []CoreValType{api.ValueTypeI32}},
		{"char", wit.Char{}, []CoreValType{api.ValueTypeI32}},
		{"u64", wit.U64{}, []CoreValType{api.ValueTypeI64}},
		{"s64", wit.S64{}, []CoreValType{api.ValueTypeI64}},
		{"f32", wit.F32{}, []CoreValType{api.ValueTypeF32}},
		{"f64", wit.F64{}, []CoreValType{api.ValueTypeF64}},
		{"string", wit.String{}, []CoreValType{api.ValueTypeI32, api.ValueTypeI32}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FlattenType(tc.typ)
			if len(result) != len(tc.expected) {
				t.Fatalf("expected %d types, got %d", len(tc.expected), len(result))
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("index %d: expected %v, got %v", i, tc.expected[i], v)
				}
			}
		})
	}
}

func TestFlattenType_Nil(t *testing.T) {
	result := FlattenType(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestFlattenTypes_Multiple(t *testing.T) {
	types := []wit.Type{wit.U32{}, wit.F64{}, wit.String{}}
	result := FlattenTypes(types)

	// u32 -> i32, f64 -> f64, string -> i32,i32
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeF64, api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestFlattenType_Record(t *testing.T) {
	// Record { x: u32, y: u32, z: f64 }
	record := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
				{Name: "z", Type: wit.F64{}},
			},
		},
	}

	result := FlattenType(record)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeF64}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestFlattenType_RecordNested(t *testing.T) {
	// Record { point: Record { x: f32, y: f32 }, name: string }
	innerRecord := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.F32{}},
				{Name: "y", Type: wit.F32{}},
			},
		},
	}

	outerRecord := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "point", Type: innerRecord},
				{Name: "name", Type: wit.String{}},
			},
		},
	}

	result := FlattenType(outerRecord)
	// f32 + f32 + (i32, i32 for string)
	expected := []CoreValType{api.ValueTypeF32, api.ValueTypeF32, api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestFlattenType_Tuple(t *testing.T) {
	// tuple<u32, f64, string>
	tuple := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.F64{}, wit.String{}},
		},
	}

	result := FlattenType(tuple)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeF64, api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestFlattenType_List(t *testing.T) {
	// list<u32> -> (ptr, len)
	list := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	result := FlattenType(list)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
}

func TestFlattenType_Option(t *testing.T) {
	// option<u64> -> discriminant(i32) + u64
	option := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U64{}},
	}

	result := FlattenType(option)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI64}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestFlattenType_OptionNil(t *testing.T) {
	// option with nil type (option<_>)
	option := &wit.TypeDef{
		Kind: &wit.Option{Type: nil},
	}

	result := FlattenType(option)
	// Just discriminant
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32], got %v", result)
	}
}

func TestFlattenType_Result(t *testing.T) {
	// result<u32, string> -> discriminant + max(u32, string)
	res := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	result := FlattenType(res)
	// discriminant(i32) + joined(u32[i32], string[i32,i32]) = i32 + i32 + i32
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
}

func TestFlattenType_ResultOkOnly(t *testing.T) {
	// result<u64, _>
	res := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U64{},
			Err: nil,
		},
	}

	result := FlattenType(res)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI64}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
}

func TestFlattenType_ResultErrOnly(t *testing.T) {
	// result<_, string>
	res := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  nil,
			Err: wit.String{},
		},
	}

	result := FlattenType(res)
	// discriminant + string(i32,i32)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
}

func TestFlattenType_Variant(t *testing.T) {
	// variant { none, some(u64), error(string) }
	variant := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "none", Type: nil},
				{Name: "some", Type: wit.U64{}},
				{Name: "error", Type: wit.String{}},
			},
		},
	}

	result := FlattenType(variant)
	// discriminant(i32) + max(none[], some[i64], error[i32,i32])
	// joined: [i64, i32] with type joining
	if len(result) < 2 {
		t.Fatalf("expected at least 2 types, got %d", len(result))
	}
	if result[0] != api.ValueTypeI32 {
		t.Errorf("discriminant should be i32, got %v", result[0])
	}
}

func TestFlattenType_Enum(t *testing.T) {
	// enum { red, green, blue }
	enum := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "red"},
				{Name: "green"},
				{Name: "blue"},
			},
		},
	}

	result := FlattenType(enum)
	expected := []CoreValType{api.ValueTypeI32}
	if len(result) != 1 || result[0] != expected[0] {
		t.Errorf("expected [i32], got %v", result)
	}
}

func TestFlattenType_Flags(t *testing.T) {
	// flags { read, write, execute } (3 flags)
	flags := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: []wit.Flag{
				{Name: "read"},
				{Name: "write"},
				{Name: "execute"},
			},
		},
	}

	result := FlattenType(flags)
	expected := []CoreValType{api.ValueTypeI32}
	if len(result) != 1 || result[0] != expected[0] {
		t.Errorf("expected [i32], got %v", result)
	}
}

func TestFlattenType_FlagsLarge(t *testing.T) {
	// > 32 flags should use i64
	var flagsList []wit.Flag
	for i := 0; i < 40; i++ {
		flagsList = append(flagsList, wit.Flag{Name: string(rune('a' + i))})
	}

	flags := &wit.TypeDef{
		Kind: &wit.Flags{Flags: flagsList},
	}

	result := FlattenType(flags)
	expected := []CoreValType{api.ValueTypeI64}
	if len(result) != 1 || result[0] != expected[0] {
		t.Errorf("expected [i64] for >32 flags, got %v", result)
	}
}

func TestFlattenType_Own(t *testing.T) {
	// own<resource> -> i32 (handle)
	own := &wit.TypeDef{
		Kind: &wit.Own{},
	}

	result := FlattenType(own)
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32] for own, got %v", result)
	}
}

func TestFlattenType_Borrow(t *testing.T) {
	// borrow<resource> -> i32 (handle)
	borrow := &wit.TypeDef{
		Kind: &wit.Borrow{},
	}

	result := FlattenType(borrow)
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32] for borrow, got %v", result)
	}
}

func TestFlattenType_TypeDefNil(t *testing.T) {
	// TypeDef with nil kind
	td := &wit.TypeDef{Kind: nil}
	result := FlattenType(td)
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32] default, got %v", result)
	}
}

func TestFlattenType_TypeDefString(t *testing.T) {
	// TypeDef wrapping string
	td := &wit.TypeDef{Kind: wit.String{}}
	result := FlattenType(td)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
}

func TestFlattenType_TypeDefPrimitives(t *testing.T) {
	tests := []struct {
		kind     wit.TypeDefKind
		expected []CoreValType
	}{
		{wit.Bool{}, []CoreValType{api.ValueTypeI32}},
		{wit.U8{}, []CoreValType{api.ValueTypeI32}},
		{wit.U16{}, []CoreValType{api.ValueTypeI32}},
		{wit.U32{}, []CoreValType{api.ValueTypeI32}},
		{wit.S8{}, []CoreValType{api.ValueTypeI32}},
		{wit.S16{}, []CoreValType{api.ValueTypeI32}},
		{wit.S32{}, []CoreValType{api.ValueTypeI32}},
		{wit.Char{}, []CoreValType{api.ValueTypeI32}},
		{wit.U64{}, []CoreValType{api.ValueTypeI64}},
		{wit.S64{}, []CoreValType{api.ValueTypeI64}},
		{wit.F32{}, []CoreValType{api.ValueTypeF32}},
		{wit.F64{}, []CoreValType{api.ValueTypeF64}},
	}

	for _, tc := range tests {
		td := &wit.TypeDef{Kind: tc.kind}
		result := FlattenType(td)
		if len(result) != len(tc.expected) {
			t.Errorf("TypeDef(%T): expected %d types, got %d", tc.kind, len(tc.expected), len(result))
			continue
		}
		for i, v := range result {
			if v != tc.expected[i] {
				t.Errorf("TypeDef(%T) index %d: expected %v, got %v", tc.kind, i, tc.expected[i], v)
			}
		}
	}
}

func TestJoinTypes(t *testing.T) {
	tests := []struct {
		a, b     CoreValType
		expected CoreValType
	}{
		{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
		{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
		{api.ValueTypeF32, api.ValueTypeF32, api.ValueTypeF32},
		{api.ValueTypeF64, api.ValueTypeF64, api.ValueTypeF64},
		{api.ValueTypeI32, api.ValueTypeF32, api.ValueTypeI32}, // both 32-bit -> i32
		{api.ValueTypeF32, api.ValueTypeI32, api.ValueTypeI32}, // both 32-bit -> i32
		{api.ValueTypeI32, api.ValueTypeI64, api.ValueTypeI64}, // different sizes -> i64
		{api.ValueTypeF32, api.ValueTypeI64, api.ValueTypeI64}, // different sizes -> i64
		{api.ValueTypeF64, api.ValueTypeI32, api.ValueTypeI64}, // different sizes -> i64
	}

	for _, tc := range tests {
		result := joinTypes(tc.a, tc.b)
		if result != tc.expected {
			t.Errorf("joinTypes(%v, %v): expected %v, got %v", tc.a, tc.b, tc.expected, result)
		}
	}
}

func TestDiscriminantType(t *testing.T) {
	tests := []struct {
		numCases int
		expected CoreValType
	}{
		{0, api.ValueTypeI32},
		{1, api.ValueTypeI32},
		{2, api.ValueTypeI32},
		{255, api.ValueTypeI32},
		{256, api.ValueTypeI32},
		{65536, api.ValueTypeI32},
	}

	for _, tc := range tests {
		result := discriminantType(tc.numCases)
		if len(result) != 1 || result[0] != tc.expected {
			t.Errorf("discriminantType(%d): expected [%v], got %v", tc.numCases, tc.expected, result)
		}
	}
}

func TestFlattenArenaType_Primitives(t *testing.T) {
	tests := []struct {
		name     string
		typ      interface{}
		expected []CoreValType
	}{
		{"primBool", arena.ResolvedBool{}, []CoreValType{api.ValueTypeI32}},
		{"primU8", arena.ResolvedU8{}, []CoreValType{api.ValueTypeI32}},
		{"primU16", arena.ResolvedU16{}, []CoreValType{api.ValueTypeI32}},
		{"primU32", arena.ResolvedU32{}, []CoreValType{api.ValueTypeI32}},
		{"primS8", arena.ResolvedS8{}, []CoreValType{api.ValueTypeI32}},
		{"primS16", arena.ResolvedS16{}, []CoreValType{api.ValueTypeI32}},
		{"primS32", arena.ResolvedS32{}, []CoreValType{api.ValueTypeI32}},
		{"primChar", arena.ResolvedChar{}, []CoreValType{api.ValueTypeI32}},
		{"primU64", arena.ResolvedU64{}, []CoreValType{api.ValueTypeI64}},
		{"primS64", arena.ResolvedS64{}, []CoreValType{api.ValueTypeI64}},
		{"primF32", arena.ResolvedF32{}, []CoreValType{api.ValueTypeF32}},
		{"primF64", arena.ResolvedF64{}, []CoreValType{api.ValueTypeF64}},
		{"primString", arena.ResolvedString{}, []CoreValType{api.ValueTypeI32, api.ValueTypeI32}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := flattenArenaType(tc.typ)
			if len(result) != len(tc.expected) {
				t.Fatalf("expected %d types, got %d", len(tc.expected), len(result))
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("index %d: expected %v, got %v", i, tc.expected[i], v)
				}
			}
		})
	}
}

func TestFlattenArenaType_Record(t *testing.T) {
	rec := arena.Record{
		Fields: []arena.Field{
			{Name: "x", Type: arena.ResolvedU32{}},
			{Name: "y", Type: arena.ResolvedF64{}},
		},
	}

	result := flattenArenaType(rec)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeF64}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestFlattenArenaType_List(t *testing.T) {
	list := arena.List{Elem: arena.ResolvedU32{}}
	result := flattenArenaType(list)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected [i32, i32], got %v", result)
	}
}

func TestFlattenArenaType_Tuple(t *testing.T) {
	tuple := arena.Tuple{
		Types: []interface{}{arena.ResolvedU32{}, arena.ResolvedF64{}, arena.ResolvedString{}},
	}

	result := flattenArenaType(tuple)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeF64, api.ValueTypeI32, api.ValueTypeI32}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
}

func TestFlattenArenaType_Option(t *testing.T) {
	opt := arena.Option{Type: arena.ResolvedU64{}}
	result := flattenArenaType(opt)
	expected := []CoreValType{api.ValueTypeI32, api.ValueTypeI64}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
}

func TestFlattenArenaType_OptionNil(t *testing.T) {
	opt := arena.Option{Type: nil}
	result := flattenArenaType(opt)
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32], got %v", result)
	}
}

func TestFlattenArenaType_Result(t *testing.T) {
	res := arena.Result{
		OK:  arena.ResolvedU32{},
		Err: arena.ResolvedString{},
	}

	result := flattenArenaType(res)
	// discriminant + max(u32[i32], string[i32,i32]) = i32 + i32 + i32
	if len(result) != 3 {
		t.Fatalf("expected 3 types, got %d: %v", len(result), result)
	}
	if result[0] != api.ValueTypeI32 {
		t.Errorf("discriminant should be i32")
	}
}

func TestFlattenArenaType_Flags(t *testing.T) {
	// <= 32 flags
	flags := arena.Flags{Count: 8}
	result := flattenArenaType(flags)
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32], got %v", result)
	}

	// > 32 flags
	flags = arena.Flags{Count: 64}
	result = flattenArenaType(flags)
	if len(result) != 1 || result[0] != api.ValueTypeI64 {
		t.Errorf("expected [i64] for >32 flags, got %v", result)
	}
}

func TestFlattenArenaType_Enum(t *testing.T) {
	enum := arena.Enum{Count: 3}
	result := flattenArenaType(enum)
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32], got %v", result)
	}
}

func TestFlattenArenaType_Variant(t *testing.T) {
	variant := arena.Variant{
		Cases: []arena.Case{
			{Name: "none", Type: nil},
			{Name: "some", Type: arena.ResolvedU64{}},
		},
	}

	result := flattenArenaType(variant)
	// discriminant + max(none[], some[i64]) = i32 + i64
	if len(result) < 2 {
		t.Fatalf("expected at least 2 types, got %d", len(result))
	}
	if result[0] != api.ValueTypeI32 {
		t.Errorf("discriminant should be i32")
	}
}

func TestFlattenArenaType_Unknown(t *testing.T) {
	// Unknown type defaults to i32
	result := flattenArenaType("unknown")
	if len(result) != 1 || result[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32] default, got %v", result)
	}
}

func TestFlattenArenaType_NestedRecord(t *testing.T) {
	inner := arena.Record{
		Fields: []arena.Field{
			{Name: "x", Type: arena.ResolvedF32{}},
			{Name: "y", Type: arena.ResolvedF32{}},
		},
	}
	outer := arena.Record{
		Fields: []arena.Field{
			{Name: "point", Type: inner},
			{Name: "z", Type: arena.ResolvedU64{}},
		},
	}

	result := flattenArenaType(outer)
	expected := []CoreValType{api.ValueTypeF32, api.ValueTypeF32, api.ValueTypeI64}
	if len(result) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("index %d: expected %v, got %v", i, expected[i], v)
		}
	}
}

func TestFlattenFuncTypeData_Simple(t *testing.T) {
	a := arena.NewTypeArena()

	// func(x: u32, y: f64) -> string
	ft := arena.FuncTypeData{
		Params: []arena.ParamData{
			{Name: "x", Type: arena.ValType{Primitive: arena.PrimU32}},
			{Name: "y", Type: arena.ValType{Primitive: arena.PrimF64}},
		},
		Result: &arena.ValType{Primitive: arena.PrimString},
	}

	params, results, err := FlattenFuncTypeData(a, &ft, "lift")
	if err != nil {
		t.Fatalf("FlattenFuncTypeData error: %v", err)
	}

	// Params: u32 -> i32, f64 -> f64
	expectedParams := []CoreValType{api.ValueTypeI32, api.ValueTypeF64}
	if len(params) != len(expectedParams) {
		t.Fatalf("expected %d params, got %d", len(expectedParams), len(params))
	}
	for i, v := range params {
		if v != expectedParams[i] {
			t.Errorf("param %d: expected %v, got %v", i, expectedParams[i], v)
		}
	}

	// Result: string -> i32, i32 (but MAX_FLAT_RESULTS=1, so becomes i32 for lift)
	if len(results) != 1 || results[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32] for lift with multi-result, got %v", results)
	}
}

func TestFlattenFuncTypeData_NoResult(t *testing.T) {
	a := arena.NewTypeArena()

	// func(x: u32)
	ft := arena.FuncTypeData{
		Params: []arena.ParamData{
			{Name: "x", Type: arena.ValType{Primitive: arena.PrimU32}},
		},
		Result: nil,
	}

	params, results, err := FlattenFuncTypeData(a, &ft, "lift")
	if err != nil {
		t.Fatalf("FlattenFuncTypeData error: %v", err)
	}

	if len(params) != 1 || params[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32], got %v", params)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %v", results)
	}
}

func TestFlattenFuncTypeData_ManyParams(t *testing.T) {
	a := arena.NewTypeArena()

	// func with > 16 params should collapse to single i32
	var params []arena.ParamData
	for i := 0; i < 20; i++ {
		params = append(params, arena.ParamData{Name: "p", Type: arena.ValType{Primitive: arena.PrimU32}})
	}

	ft := arena.FuncTypeData{
		Params: params,
		Result: nil,
	}

	flatParams, _, err := FlattenFuncTypeData(a, &ft, "lift")
	if err != nil {
		t.Fatalf("FlattenFuncTypeData error: %v", err)
	}

	// > MAX_FLAT_PARAMS should collapse to single i32
	if len(flatParams) != 1 || flatParams[0] != api.ValueTypeI32 {
		t.Errorf("expected [i32] for >16 params, got %v", flatParams)
	}
}

func TestFlattenFuncTypeData_LowerContext(t *testing.T) {
	a := arena.NewTypeArena()

	// func() -> string (2 flat results, > MAX_FLAT_RESULTS)
	ft := arena.FuncTypeData{
		Params: nil,
		Result: &arena.ValType{Primitive: arena.PrimString},
	}

	params, results, err := FlattenFuncTypeData(a, &ft, "lower")
	if err != nil {
		t.Fatalf("FlattenFuncTypeData error: %v", err)
	}

	// For lower context: results > 1 adds retptr param and clears results
	if len(params) != 1 || params[0] != api.ValueTypeI32 {
		t.Errorf("expected retptr param [i32], got %v", params)
	}
	if len(results) != 0 {
		t.Errorf("expected no results for lower, got %v", results)
	}
}

func TestFlattenFuncTypeData_SingleResult(t *testing.T) {
	a := arena.NewTypeArena()

	// func() -> u64 (single flat result, under limit)
	ft := arena.FuncTypeData{
		Params: nil,
		Result: &arena.ValType{Primitive: arena.PrimU64},
	}

	params, results, err := FlattenFuncTypeData(a, &ft, "lift")
	if err != nil {
		t.Fatalf("FlattenFuncTypeData error: %v", err)
	}

	if len(params) != 0 {
		t.Errorf("expected no params, got %v", params)
	}
	if len(results) != 1 || results[0] != api.ValueTypeI64 {
		t.Errorf("expected [i64], got %v", results)
	}
}
