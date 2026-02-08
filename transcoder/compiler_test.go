package transcoder

import (
	"reflect"
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestCompiler_Tuple(t *testing.T) {
	c := NewCompiler()

	// Define tuple<u32, u64>
	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U64{}},
		},
	}

	// Define Go struct to match
	type TupleStruct struct {
		A uint32
		B uint64
	}
	goType := reflect.TypeOf(TupleStruct{})

	ct, err := c.Compile(tupleType, goType)
	if err != nil {
		t.Fatalf("Compile tuple failed: %v", err)
	}

	if ct.Kind != KindTuple {
		t.Errorf("Kind = %v, want KindTuple", ct.Kind)
	}
	if len(ct.Fields) != 2 {
		t.Errorf("Fields len = %d, want 2", len(ct.Fields))
	}
}

func TestCompiler_TupleArray(t *testing.T) {
	c := NewCompiler()

	// Define tuple<u32, u32, u32>
	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U32{}, wit.U32{}},
		},
	}

	// Array can also represent tuple
	goType := reflect.TypeOf([3]uint32{})

	ct, err := c.Compile(tupleType, goType)
	if err != nil {
		t.Fatalf("Compile tuple array failed: %v", err)
	}

	if ct.Kind != KindTuple {
		t.Errorf("Kind = %v, want KindTuple", ct.Kind)
	}
	if len(ct.Fields) != 3 {
		t.Errorf("Fields len = %d, want 3", len(ct.Fields))
	}
}

func TestCompiler_TupleMismatch(t *testing.T) {
	c := NewCompiler()

	// Define tuple<u32, u64>
	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U64{}},
		},
	}

	// Invalid: int instead of struct
	goType := reflect.TypeOf(0)

	_, err := c.Compile(tupleType, goType)
	if err == nil {
		t.Error("expected error for tuple type mismatch")
	}
}

func TestCompiler_TupleFieldCount(t *testing.T) {
	c := NewCompiler()

	// Define tuple<u32, u64, u32>
	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U64{}, wit.U32{}},
		},
	}

	// Go struct has fewer fields
	type TwoFields struct {
		A uint32
		B uint64
	}
	goType := reflect.TypeOf(TwoFields{})

	_, err := c.Compile(tupleType, goType)
	if err == nil {
		t.Error("expected error for tuple field count mismatch")
	}
}

func TestCompiler_Enum(t *testing.T) {
	c := NewCompiler()

	// Define enum { red, green, blue }
	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "red"},
				{Name: "green"},
				{Name: "blue"},
			},
		},
	}

	// Enum can be any integer type
	tests := []struct {
		goType reflect.Type
		name   string
	}{
		{reflect.TypeOf(uint8(0)), "uint8"},
		{reflect.TypeOf(uint16(0)), "uint16"},
		{reflect.TypeOf(uint32(0)), "uint32"},
		{reflect.TypeOf(int8(0)), "int8"},
		{reflect.TypeOf(int16(0)), "int16"},
		{reflect.TypeOf(int32(0)), "int32"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, err := c.Compile(enumType, tt.goType)
			if err != nil {
				t.Fatalf("Compile enum failed: %v", err)
			}
			if ct.Kind != KindEnum {
				t.Errorf("Kind = %v, want KindEnum", ct.Kind)
			}
		})
	}
}

func TestCompiler_EnumInvalidType(t *testing.T) {
	c := NewCompiler()

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{{Name: "a"}},
		},
	}

	// Invalid: string instead of integer
	_, err := c.Compile(enumType, reflect.TypeOf(""))
	if err == nil {
		t.Error("expected error for enum type mismatch")
	}
}

func TestCompiler_Flags(t *testing.T) {
	c := NewCompiler()

	// Define flags { read, write, execute }
	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: []wit.Flag{
				{Name: "read"},
				{Name: "write"},
				{Name: "execute"},
			},
		},
	}

	// Flags can be unsigned integer types
	tests := []struct {
		goType reflect.Type
		name   string
	}{
		{reflect.TypeOf(uint8(0)), "uint8"},
		{reflect.TypeOf(uint16(0)), "uint16"},
		{reflect.TypeOf(uint32(0)), "uint32"},
		{reflect.TypeOf(uint64(0)), "uint64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, err := c.Compile(flagsType, tt.goType)
			if err != nil {
				t.Fatalf("Compile flags failed: %v", err)
			}
			if ct.Kind != KindFlags {
				t.Errorf("Kind = %v, want KindFlags", ct.Kind)
			}
		})
	}
}

func TestCompiler_FlagsInvalidType(t *testing.T) {
	c := NewCompiler()

	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: []wit.Flag{{Name: "a"}},
		},
	}

	// Invalid: signed int instead of unsigned
	_, err := c.Compile(flagsType, reflect.TypeOf(int32(0)))
	if err == nil {
		t.Error("expected error for flags type mismatch")
	}
}

func TestCompiler_Option(t *testing.T) {
	c := NewCompiler()

	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	// Options require pointer type
	goType := reflect.TypeOf((*uint32)(nil))

	ct, err := c.Compile(optionType, goType)
	if err != nil {
		t.Fatalf("Compile option failed: %v", err)
	}
	if ct.Kind != KindOption {
		t.Errorf("Kind = %v, want KindOption", ct.Kind)
	}
}

func TestCompiler_OptionInvalidType(t *testing.T) {
	c := NewCompiler()

	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	// Invalid: non-pointer type
	_, err := c.Compile(optionType, reflect.TypeOf(uint32(0)))
	if err == nil {
		t.Error("expected error for option type mismatch")
	}
}

func TestCompiler_NilGoType(t *testing.T) {
	c := NewCompiler()

	_, err := c.Compile(wit.U32{}, nil)
	if err == nil {
		t.Error("expected error for nil Go type")
	}
}

func TestCompiler_Cache(t *testing.T) {
	c := NewCompiler()

	goType := reflect.TypeOf(uint32(0))

	// First call
	ct1, err := c.Compile(wit.U32{}, goType)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Second call should return cached result
	ct2, err := c.Compile(wit.U32{}, goType)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Should be the same pointer (cached)
	if ct1 != ct2 {
		t.Error("expected cached result")
	}
}

func TestCompiler_PointerDereference(t *testing.T) {
	c := NewCompiler()

	// Pass pointer to struct, should dereference for non-option types
	type MyRecord struct {
		A uint32
	}
	goType := reflect.TypeOf(&MyRecord{})

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{{Name: "a", Type: wit.U32{}}},
		},
	}

	ct, err := c.Compile(recordType, goType)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if ct.Kind != KindRecord {
		t.Errorf("Kind = %v, want KindRecord", ct.Kind)
	}
}

func TestCompiler_ResultNoPayload(t *testing.T) {
	c := NewCompiler()

	// Result with no OK or Err type
	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  nil,
			Err: nil,
		},
	}

	// Result with no payload maps to any type
	goType := reflect.TypeOf(struct{}{})

	ct, err := c.Compile(resultType, goType)
	if err != nil {
		t.Fatalf("Compile result failed: %v", err)
	}
	if ct.Kind != KindResult {
		t.Errorf("Kind = %v, want KindResult", ct.Kind)
	}
}

func TestNewCompiler(t *testing.T) {
	c := NewCompiler()
	if c == nil {
		t.Fatal("NewCompiler returned nil")
	}
	if c.layout == nil {
		t.Error("layout is nil")
	}
}
