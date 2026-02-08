package component

import (
	"bytes"
	"testing"
)

func TestParsePrimitiveTypes(t *testing.T) {
	tests := []struct {
		name     string
		typeByte byte
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader([]byte{tt.typeByte})
			valType, err := parseValType(r)
			if err != nil {
				t.Fatalf("parseValType() error = %v", err)
			}

			primType, ok := valType.(PrimValType)
			if !ok {
				t.Fatalf("expected PrimValType, got %T", valType)
			}

			if primType.Type != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, primType.Type)
			}
		})
	}
}

func TestParseRecordType(t *testing.T) {
	// Record with 2 fields: {name: string, age: u32}
	// 0x72 = record type
	// 0x02 = 2 fields
	// Field 1: "name" (4 bytes) + 0x73 (string)
	// Field 2: "age" (3 bytes) + 0x79 (u32)
	data := []byte{
		0x02,                           // 2 fields
		0x04, 'n', 'a', 'm', 'e', 0x73, // field "name": string
		0x03, 'a', 'g', 'e', 0x79, // field "age": u32
	}

	r := bytes.NewReader(data)
	record, err := parseRecordType(r)
	if err != nil {
		t.Fatalf("parseRecordType() error = %v", err)
	}

	if len(record.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(record.Fields))
	}

	if record.Fields[0].Name != "name" {
		t.Errorf("field 0 name = %q, want %q", record.Fields[0].Name, "name")
	}

	if primType, ok := record.Fields[0].Type.(PrimValType); !ok || primType.Type != PrimString {
		t.Errorf("field 0 type = %v, want PrimString", record.Fields[0].Type)
	}

	if record.Fields[1].Name != "age" {
		t.Errorf("field 1 name = %q, want %q", record.Fields[1].Name, "age")
	}

	if primType, ok := record.Fields[1].Type.(PrimValType); !ok || primType.Type != PrimU32 {
		t.Errorf("field 1 type = %v, want PrimU32", record.Fields[1].Type)
	}
}

func TestParseEnumType(t *testing.T) {
	// Enum with 3 cases: red, green, blue
	data := []byte{
		0x03,                // 3 cases
		0x03, 'r', 'e', 'd', // "red"
		0x05, 'g', 'r', 'e', 'e', 'n', // "green"
		0x04, 'b', 'l', 'u', 'e', // "blue"
	}

	r := bytes.NewReader(data)
	enum, err := parseEnumType(r)
	if err != nil {
		t.Fatalf("parseEnumType() error = %v", err)
	}

	if len(enum.Cases) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(enum.Cases))
	}

	expected := []string{"red", "green", "blue"}
	for i, exp := range expected {
		if enum.Cases[i] != exp {
			t.Errorf("case %d = %q, want %q", i, enum.Cases[i], exp)
		}
	}
}

func TestParseFlagsType(t *testing.T) {
	// Flags with 2 flags: read, write
	data := []byte{
		0x02,                     // 2 flags
		0x04, 'r', 'e', 'a', 'd', // "read"
		0x05, 'w', 'r', 'i', 't', 'e', // "write"
	}

	r := bytes.NewReader(data)
	flags, err := parseFlagsType(r)
	if err != nil {
		t.Fatalf("parseFlagsType() error = %v", err)
	}

	if len(flags.Names) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(flags.Names))
	}

	if flags.Names[0] != "read" {
		t.Errorf("flag 0 = %q, want %q", flags.Names[0], "read")
	}

	if flags.Names[1] != "write" {
		t.Errorf("flag 1 = %q, want %q", flags.Names[1], "write")
	}
}

func TestParseListType(t *testing.T) {
	// List of u32: list<u32>
	// 0x70 = list type
	// 0x79 = u32
	data := []byte{0x79} // element type is u32

	r := bytes.NewReader(data)
	elemType, err := parseValType(r)
	if err != nil {
		t.Fatalf("parseValType() error = %v", err)
	}

	list := ListType{ElemType: elemType}

	if primType, ok := list.ElemType.(PrimValType); !ok || primType.Type != PrimU32 {
		t.Errorf("list element type = %v, want PrimU32", list.ElemType)
	}
}

func TestParseTupleType(t *testing.T) {
	// Tuple (string, u32)
	data := []byte{
		0x02, // 2 elements
		0x73, // string
		0x79, // u32
	}

	r := bytes.NewReader(data)
	tuple, err := parseTupleType(r)
	if err != nil {
		t.Fatalf("parseTupleType() error = %v", err)
	}

	if len(tuple.Types) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(tuple.Types))
	}

	if primType, ok := tuple.Types[0].(PrimValType); !ok || primType.Type != PrimString {
		t.Errorf("tuple element 0 = %v, want PrimString", tuple.Types[0])
	}

	if primType, ok := tuple.Types[1].(PrimValType); !ok || primType.Type != PrimU32 {
		t.Errorf("tuple element 1 = %v, want PrimU32", tuple.Types[1])
	}
}

func TestParseOptionType(t *testing.T) {
	// Option<string>
	// Inner type is string (0x73)
	data := []byte{0x73}

	r := bytes.NewReader(data)
	elemType, err := parseValType(r)
	if err != nil {
		t.Fatalf("parseValType() error = %v", err)
	}

	option := OptionType{Type: elemType}

	if primType, ok := option.Type.(PrimValType); !ok || primType.Type != PrimString {
		t.Errorf("option type = %v, want PrimString", option.Type)
	}
}

func TestParseResultType(t *testing.T) {
	// Result<u32, string>
	// has-ok = 1, ok-type = u32 (0x79)
	// has-err = 1, err-type = string (0x73)
	data := []byte{
		0x01, // has-ok = true
		0x79, // ok type = u32
		0x01, // has-err = true
		0x73, // err type = string
	}

	r := bytes.NewReader(data)
	result, err := parseResultType(r)
	if err != nil {
		t.Fatalf("parseResultType() error = %v", err)
	}

	if result.OK == nil {
		t.Fatal("expected OK type, got nil")
	}

	if primType, ok := (*result.OK).(PrimValType); !ok || primType.Type != PrimU32 {
		t.Errorf("result OK type = %v, want PrimU32", *result.OK)
	}

	if result.Err == nil {
		t.Fatal("expected Err type, got nil")
	}

	if primType, ok := (*result.Err).(PrimValType); !ok || primType.Type != PrimString {
		t.Errorf("result Err type = %v, want PrimString", *result.Err)
	}
}

func TestParseVariantType(t *testing.T) {
	// Variant with 2 cases:
	// - none (no type, no refines)
	// - some(u32, no refines)
	// Per spec: case ::= l:<label'> t?:<valtype>? refines?:<u32>?
	//   where <T>? ::= 0x00 | 0x01 t:<T>
	data := []byte{
		0x02,                     // 2 cases
		0x04, 'n', 'o', 'n', 'e', // name "none"
		0x00,                     // no type
		0x00,                     // no refines
		0x04, 's', 'o', 'm', 'e', // name "some"
		0x01, 0x79, // has type: u32
		0x00, // no refines
	}

	r := bytes.NewReader(data)
	variant, err := parseVariantType(r)
	if err != nil {
		t.Fatalf("parseVariantType() error = %v", err)
	}

	if len(variant.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(variant.Cases))
	}

	// Case 0: none
	if variant.Cases[0].Name != "none" {
		t.Errorf("case 0 name = %q, want %q", variant.Cases[0].Name, "none")
	}

	if variant.Cases[0].Type != nil {
		t.Errorf("case 0 should have no type, got %v", variant.Cases[0].Type)
	}

	// Case 1: some(u32)
	if variant.Cases[1].Name != "some" {
		t.Errorf("case 1 name = %q, want %q", variant.Cases[1].Name, "some")
	}

	if variant.Cases[1].Type == nil {
		t.Fatal("case 1 should have a type")
	}

	if primType, ok := (*variant.Cases[1].Type).(PrimValType); !ok || primType.Type != PrimU32 {
		t.Errorf("case 1 type = %v, want PrimU32", *variant.Cases[1].Type)
	}
}

func TestParseFuncType(t *testing.T) {
	// func(a: u32, b: string) -> u32
	// Per spec: 0x40 + paramlist + resultlist
	// paramlist: vec(labelvaltype) = count + (name + type)*
	// resultlist: 0x00 + type OR 0x01 0x00
	data := []byte{
		0x02,            // 2 params
		0x01, 'a', 0x79, // param "a": u32
		0x01, 'b', 0x73, // param "b": string
		0x00, 0x79, // resultlist: 0x00 (has result) + u32
	}

	r := bytes.NewReader(data)
	funcType, err := parseFuncType(r)
	if err != nil {
		t.Fatalf("parseFuncType() error = %v", err)
	}

	if len(funcType.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(funcType.Params))
	}

	// Param 0
	if funcType.Params[0].Name != "a" {
		t.Errorf("param 0 name = %q, want %q", funcType.Params[0].Name, "a")
	}

	if primType, ok := funcType.Params[0].Type.(PrimValType); !ok || primType.Type != PrimU32 {
		t.Errorf("param 0 type = %v, want PrimU32", funcType.Params[0].Type)
	}

	// Param 1
	if funcType.Params[1].Name != "b" {
		t.Errorf("param 1 name = %q, want %q", funcType.Params[1].Name, "b")
	}

	if primType, ok := funcType.Params[1].Type.(PrimValType); !ok || primType.Type != PrimString {
		t.Errorf("param 1 type = %v, want PrimString", funcType.Params[1].Type)
	}

	// Result
	if funcType.Result == nil {
		t.Fatal("expected result, got nil")
	}

	if primType, ok := (*funcType.Result).(PrimValType); !ok || primType.Type != PrimU32 {
		t.Errorf("result type = %v, want PrimU32", *funcType.Result)
	}
}

func TestParseTypeSection(t *testing.T) {
	// Type section with 2 types:
	// 0: func(a: u32) -> u32
	// 1: instance { export "test": func }
	data := []byte{
		0x02, // 2 types
		// Type 0: function
		0x40,            // func type
		0x01,            // 1 param
		0x01, 'a', 0x79, // param "a": u32
		0x00, 0x79, // resultlist: 0x00 (has result) + u32
		// Type 1: instance (simplified, just marker)
		0x42, // instance type
		0x00, // 0 declarations
	}

	section, err := ParseTypeSection(data)
	if err != nil {
		t.Fatalf("ParseTypeSection() error = %v", err)
	}

	if len(section.Types) != 2 {
		t.Fatalf("expected 2 types, got %d", len(section.Types))
	}

	// Check type 0 is a function
	funcType, ok := section.Types[0].(*FuncType)
	if !ok {
		t.Fatalf("type 0 should be FuncType, got %T", section.Types[0])
	}

	if len(funcType.Params) != 1 {
		t.Errorf("expected 1 param, got %d", len(funcType.Params))
	}

	// Check type 1 is an instance
	_, ok = section.Types[1].(*InstanceType)
	if !ok {
		t.Fatalf("type 1 should be InstanceType, got %T", section.Types[1])
	}
}

func TestReadString(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		data     []byte
		wantErr  bool
	}{
		{
			name:     "simple string",
			data:     []byte{0x05, 'h', 'e', 'l', 'l', 'o'},
			expected: "hello",
			wantErr:  false,
		},
		{
			name:     "empty string",
			data:     []byte{0x00},
			expected: "",
			wantErr:  false,
		},
		{
			name:    "truncated string",
			data:    []byte{0x05, 'h', 'i'},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.data)
			result, err := readString(r)

			if (err != nil) != tt.wantErr {
				t.Errorf("readString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != tt.expected {
				t.Errorf("readString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestParseComponentValType_OwnBorrow verifies that parseValType
// correctly returns OwnType and BorrowType for 0x69 and 0x68 bytes.
func TestParseComponentValType_OwnBorrow(t *testing.T) {
	tests := []struct {
		name     string
		wantType string
		data     []byte
		wantIdx  uint32
	}{
		{
			name:     "own type index 0",
			data:     []byte{0x69, 0x00},
			wantType: "OwnType",
			wantIdx:  0,
		},
		{
			name:     "own type index 5",
			data:     []byte{0x69, 0x05},
			wantType: "OwnType",
			wantIdx:  5,
		},
		{
			name:     "borrow type index 0",
			data:     []byte{0x68, 0x00},
			wantType: "BorrowType",
			wantIdx:  0,
		},
		{
			name:     "borrow type index 3",
			data:     []byte{0x68, 0x03},
			wantType: "BorrowType",
			wantIdx:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.data)
			result, err := parseValType(r)
			if err != nil {
				t.Fatalf("parseValType() error = %v", err)
			}

			switch v := result.(type) {
			case OwnType:
				if tt.wantType != "OwnType" {
					t.Errorf("got OwnType, want %s", tt.wantType)
				}
				if v.TypeIndex != tt.wantIdx {
					t.Errorf("OwnType.TypeIndex = %d, want %d", v.TypeIndex, tt.wantIdx)
				}
			case BorrowType:
				if tt.wantType != "BorrowType" {
					t.Errorf("got BorrowType, want %s", tt.wantType)
				}
				if v.TypeIndex != tt.wantIdx {
					t.Errorf("BorrowType.TypeIndex = %d, want %d", v.TypeIndex, tt.wantIdx)
				}
			case TypeIndexRef:
				t.Errorf("got TypeIndexRef{Index: %d}, want %s - parser should not return TypeIndexRef for own/borrow", v.Index, tt.wantType)
			default:
				t.Errorf("got %T, want %s", result, tt.wantType)
			}
		})
	}
}

// TestParseDefTypeAsComponentType_OwnBorrow verifies that parseDefTypeAsType
// correctly returns OwnType and BorrowType for 0x69 and 0x68 bytes.
// This is the function called when parsing top-level component types.
func TestParseDefTypeAsComponentType_OwnBorrow(t *testing.T) {
	tests := []struct {
		name     string
		wantType string
		data     []byte
		wantIdx  uint32
	}{
		{
			name:     "own type index 0",
			data:     []byte{0x69, 0x00},
			wantType: "OwnType",
			wantIdx:  0,
		},
		{
			name:     "borrow type index 0",
			data:     []byte{0x68, 0x00},
			wantType: "BorrowType",
			wantIdx:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.data)
			result, err := parseDefTypeAsType(r)
			if err != nil {
				t.Fatalf("parseDefTypeAsType() error = %v", err)
			}

			switch v := result.(type) {
			case OwnType:
				if tt.wantType != "OwnType" {
					t.Errorf("got OwnType, want %s", tt.wantType)
				}
				if v.TypeIndex != tt.wantIdx {
					t.Errorf("OwnType.TypeIndex = %d, want %d", v.TypeIndex, tt.wantIdx)
				}
			case BorrowType:
				if tt.wantType != "BorrowType" {
					t.Errorf("got BorrowType, want %s", tt.wantType)
				}
				if v.TypeIndex != tt.wantIdx {
					t.Errorf("BorrowType.TypeIndex = %d, want %d", v.TypeIndex, tt.wantIdx)
				}
			case TypeIndexRef:
				t.Errorf("got TypeIndexRef{Index: %d}, want %s - parser should not return TypeIndexRef for own/borrow", v.Index, tt.wantType)
			default:
				t.Errorf("got %T, want %s", result, tt.wantType)
			}
		})
	}
}

// TestParseFuncType_BorrowParam verifies that function types with
// borrow parameters are correctly parsed with BorrowType, not TypeIndexRef.
func TestParseFuncType_BorrowParam(t *testing.T) {
	// func(self: borrow<0>) -> ()
	// 0x40 = func type
	// param count = 1
	// param name = "self"
	// param type = 0x68 0x00 (borrow type index 0)
	// resultlist = 0x01 0x00 (no result)
	data := []byte{
		0x01,                     // 1 param
		0x04, 's', 'e', 'l', 'f', // param name "self"
		0x68, 0x00, // borrow type index 0
		0x01, 0x00, // no result
	}

	r := bytes.NewReader(data)
	funcType, err := parseFuncType(r)
	if err != nil {
		t.Fatalf("parseFuncType() error = %v", err)
	}

	if len(funcType.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(funcType.Params))
	}

	if funcType.Params[0].Name != "self" {
		t.Errorf("param name = %q, want %q", funcType.Params[0].Name, "self")
	}

	BorrowType, ok := funcType.Params[0].Type.(BorrowType)
	if !ok {
		t.Fatalf("param type = %T, want BorrowType", funcType.Params[0].Type)
	}

	if BorrowType.TypeIndex != 0 {
		t.Errorf("BorrowType.TypeIndex = %d, want 0", BorrowType.TypeIndex)
	}
}
