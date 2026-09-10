package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/wippyai/wasm-runtime/errors"
	"go.bytecodealliance.org/wit"
)

// --- Tuple Hazard Tests ---

func TestCompiler_Tuple_UndersizedArrayRejection(t *testing.T) {
	c := NewCompiler()

	tuple3 := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U32{}, wit.U32{}},
		},
	}

	tests := []struct {
		goType reflect.Type
		name   string
	}{
		{reflect.TypeOf([0]uint32{}), "array length 0"},
		{reflect.TypeOf([1]uint32{}), "array length 1"},
		{reflect.TypeOf([2]uint32{}), "array length 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Compile(tuple3, tt.goType)
			if err == nil {
				t.Fatalf("expected compile error for undersized array %v, got nil", tt.goType)
			}
			if e, ok := err.(*errors.Error); ok {
				if e.Kind != errors.KindTypeMismatch {
					t.Errorf("error Kind = %v, want KindTypeMismatch", e.Kind)
				}
			}
		})
	}
}

func TestCompiler_Tuple_UndersizedStructRejection(t *testing.T) {
	c := NewCompiler()

	tuple3 := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U32{}, wit.U32{}},
		},
	}

	type OneField struct {
		A uint32
	}
	type TwoFields struct {
		A uint32
		B uint32
	}

	for _, goType := range []reflect.Type{reflect.TypeOf(OneField{}), reflect.TypeOf(TwoFields{})} {
		_, err := c.Compile(tuple3, goType)
		if err == nil {
			t.Fatalf("expected compile error for undersized struct %v, got nil", goType)
		}
	}
}

func TestCompiler_Tuple_NonStructNonArrayRejection(t *testing.T) {
	c := NewCompiler()

	tuple2 := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U32{}},
		},
	}

	for _, goType := range []reflect.Type{
		reflect.TypeOf(uint32(0)),
		reflect.TypeOf("string"),
		reflect.TypeOf([]uint32{}),
		reflect.TypeOf(map[string]uint32{}),
	} {
		_, err := c.Compile(tuple2, goType)
		if err == nil {
			t.Fatalf("expected compile error for non-struct non-array type %v, got nil", goType)
		}
	}
}

func TestCompiler_Tuple_ValidRoundtrip(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	t.Run("array exact size", func(t *testing.T) {
		tupleDef := &wit.TypeDef{
			Kind: &wit.Tuple{
				Types: []wit.Type{wit.U32{}, wit.U32{}, wit.U32{}},
			},
		}

		input := [3]uint32{10, 20, 30}
		ct, err := enc.compiler.Compile(tupleDef, reflect.TypeOf(input))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		// Stack roundtrip
		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}
		if consumed != 3 {
			t.Fatalf("LowerToStack consumed = %d, want 3", consumed)
		}

		var stackOut [3]uint32
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&stackOut), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if stackOut != input {
			t.Errorf("stack roundtrip: got %v, want %v", stackOut, input)
		}

		// Memory roundtrip
		addr := uint32(64)
		if err := enc.StoreCompiledToMemory(addr, ct, unsafe.Pointer(&input), mem, alloc, nil); err != nil {
			t.Fatalf("StoreCompiledToMemory failed: %v", err)
		}

		var memOut [3]uint32
		if err := dec.decodeFieldFromMemory(addr, ct, unsafe.Pointer(&memOut), mem, nil); err != nil {
			t.Fatalf("decodeFieldFromMemory failed: %v", err)
		}
		if memOut != input {
			t.Errorf("memory roundtrip: got %v, want %v", memOut, input)
		}
	})

	t.Run("array oversized preserved", func(t *testing.T) {
		tupleDef := &wit.TypeDef{
			Kind: &wit.Tuple{
				Types: []wit.Type{wit.U32{}, wit.U32{}},
			},
		}

		input := [4]uint32{100, 200, 0, 0}
		ct, err := enc.compiler.Compile(tupleDef, reflect.TypeOf(input))
		if err != nil {
			t.Fatalf("Compile oversized array failed: %v", err)
		}

		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var stackOut [4]uint32
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&stackOut), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if stackOut[0] != 100 || stackOut[1] != 200 {
			t.Errorf("got [%d, %d], want [100, 200]", stackOut[0], stackOut[1])
		}
	})

	t.Run("struct roundtrip", func(t *testing.T) {
		tupleDef := &wit.TypeDef{
			Kind: &wit.Tuple{
				Types: []wit.Type{wit.U32{}, wit.U64{}},
			},
		}

		type Pair struct {
			A uint32
			B uint64
		}

		input := Pair{A: 42, B: 1234567890123}
		ct, err := enc.compiler.Compile(tupleDef, reflect.TypeOf(input))
		if err != nil {
			t.Fatalf("Compile struct failed: %v", err)
		}

		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var output Pair
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if output != input {
			t.Errorf("got %+v, want %+v", output, input)
		}
	})
}

// --- Variant Hazard Tests ---

func TestCompiler_Variant_CompileRejections(t *testing.T) {
	c := NewCompiler()

	variantDef := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "num", Type: wit.U32{}},
				{Name: "str", Type: wit.String{}},
			},
		},
	}

	t.Run("reject nonstruct", func(t *testing.T) {
		for _, badType := range []reflect.Type{
			reflect.TypeOf(42),
			reflect.TypeOf("hello"),
			reflect.TypeOf([]*uint32{}),
			reflect.TypeOf([2]*uint32{}),
			reflect.TypeOf(map[string]*uint32{}),
		} {
			_, err := c.Compile(variantDef, badType)
			if err == nil {
				t.Fatalf("expected compile error for nonstruct %v, got nil", badType)
			}
		}
	})

	t.Run("reject missing field", func(t *testing.T) {
		type MissingStr struct {
			Num *uint32
		}
		_, err := c.Compile(variantDef, reflect.TypeOf(MissingStr{}))
		if err == nil {
			t.Fatal("expected compile error for missing variant field 'str'")
		}
		if e, ok := err.(*errors.Error); ok && e.Kind != errors.KindFieldMissing {
			t.Errorf("error Kind = %v, want KindFieldMissing", e.Kind)
		}
	})

	t.Run("reject missing field for unit case", func(t *testing.T) {
		varWithUnit := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "none", Type: nil},
					{Name: "some", Type: wit.U32{}},
				},
			},
		}

		type OnlySome struct {
			Some *uint32
		}
		_, err := c.Compile(varWithUnit, reflect.TypeOf(OnlySome{}))
		if err == nil {
			t.Fatal("expected compile error for missing unit field 'none'")
		}
		if e, ok := err.(*errors.Error); ok && e.Kind != errors.KindFieldMissing {
			t.Errorf("error Kind = %v, want KindFieldMissing", e.Kind)
		}
	})

	t.Run("reject unexported field", func(t *testing.T) {
		type UnexportedField struct {
			Str *string
			num *uint32 // unexported
		}
		_ = (&UnexportedField{}).num
		_, err := c.Compile(variantDef, reflect.TypeOf(UnexportedField{}))
		if err == nil {
			t.Fatal("expected compile error for unexported field 'num'")
		}
	})

	t.Run("reject nonpointer payload field", func(t *testing.T) {
		type NonPointerField struct {
			Str *string
			Num uint32 // value type, not pointer!
		}
		_, err := c.Compile(variantDef, reflect.TypeOf(NonPointerField{}))
		if err == nil {
			t.Fatal("expected compile error for nonpointer payload field 'Num'")
		}
		if e, ok := err.(*errors.Error); ok && e.Kind != errors.KindTypeMismatch {
			t.Errorf("error Kind = %v, want KindTypeMismatch", e.Kind)
		}
	})

	t.Run("reject nonpointer unit field", func(t *testing.T) {
		varWithUnit := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "none", Type: nil},
					{Name: "some", Type: wit.U32{}},
				},
			},
		}

		type NonPointerUnit struct {
			Some *uint32
			None bool // value type, not pointer!
		}
		_, err := c.Compile(varWithUnit, reflect.TypeOf(NonPointerUnit{}))
		if err == nil {
			t.Fatal("expected compile error for nonpointer unit field 'None'")
		}
		if e, ok := err.(*errors.Error); ok && e.Kind != errors.KindTypeMismatch {
			t.Errorf("error Kind = %v, want KindTypeMismatch", e.Kind)
		}
	})

	t.Run("reject incompatible payload type", func(t *testing.T) {
		type WrongPayloadType struct {
			Num *string // WIT requires u32, but Go field is *string
			Str *string
		}
		_, err := c.Compile(variantDef, reflect.TypeOf(WrongPayloadType{}))
		if err == nil {
			t.Fatal("expected compile error for incompatible payload type in 'Num'")
		}
		if e, ok := err.(*errors.Error); ok && e.Kind != errors.KindTypeMismatch {
			t.Errorf("error Kind = %v, want KindTypeMismatch", e.Kind)
		}
	})

	t.Run("reject unexported kebab field", func(t *testing.T) {
		kebabVar := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "user-name", Type: wit.String{}},
				},
			},
		}
		type UnexportedKebab struct {
			userName *string // unexported
		}
		_ = (&UnexportedKebab{}).userName
		_, err := c.Compile(kebabVar, reflect.TypeOf(UnexportedKebab{}))
		if err == nil {
			t.Fatal("expected compile error for unexported kebab field 'userName'")
		}
	})
}

func TestCompiler_Variant_ValidRoundtrip(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	t.Run("payload variant roundtrip", func(t *testing.T) {
		variantDef := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "num", Type: wit.U32{}},
					{Name: "str", Type: wit.String{}},
				},
			},
		}

		type MyVariant struct {
			Num *uint32
			Str *string
		}

		ct, err := enc.compiler.Compile(variantDef, reflect.TypeOf(MyVariant{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		// Case 0: num
		valNum := uint32(99)
		input0 := MyVariant{Num: &valNum}

		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&input0), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack case 0 failed: %v", err)
		}

		var out0 MyVariant
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&out0), mem)
		if err != nil {
			t.Fatalf("LiftFromStack case 0 failed: %v", err)
		}
		if out0.Num == nil || *out0.Num != 99 || out0.Str != nil {
			t.Fatalf("case 0 lift mismatch: got %+v", out0)
		}

		// Memory roundtrip case 0
		addr := uint32(128)
		if err := enc.StoreCompiledToMemory(addr, ct, unsafe.Pointer(&input0), mem, alloc, nil); err != nil {
			t.Fatalf("StoreCompiledToMemory failed: %v", err)
		}
		var memOut0 MyVariant
		if err := dec.decodeFieldFromMemory(addr, ct, unsafe.Pointer(&memOut0), mem, nil); err != nil {
			t.Fatalf("decodeFieldFromMemory failed: %v", err)
		}
		if memOut0.Num == nil || *memOut0.Num != 99 || memOut0.Str != nil {
			t.Fatalf("case 0 memory decode mismatch: got %+v", memOut0)
		}

		// Case 1: str
		valStr := "hello actor"
		input1 := MyVariant{Str: &valStr}

		consumed, err = enc.LowerToStack(ct, unsafe.Pointer(&input1), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack case 1 failed: %v", err)
		}

		var out1 MyVariant
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&out1), mem)
		if err != nil {
			t.Fatalf("LiftFromStack case 1 failed: %v", err)
		}
		if out1.Str == nil || *out1.Str != "hello actor" || out1.Num != nil {
			t.Fatalf("case 1 lift mismatch: got %+v", out1)
		}
	})

	t.Run("unit case with unsafe.Pointer", func(t *testing.T) {
		variantDef := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "none", Type: nil},
					{Name: "some", Type: wit.U32{}},
				},
			},
		}

		type UnsafeUnitVariant struct {
			None unsafe.Pointer
			Some *uint32
		}

		ct, err := enc.compiler.Compile(variantDef, reflect.TypeOf(UnsafeUnitVariant{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		// Unit case: None
		inputNone := UnsafeUnitVariant{None: UnitPtr()}
		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&inputNone), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack unit failed: %v", err)
		}

		var outNone UnsafeUnitVariant
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&outNone), mem)
		if err != nil {
			t.Fatalf("LiftFromStack unit failed: %v", err)
		}
		if outNone.None == nil || outNone.Some != nil {
			t.Fatalf("unit lift mismatch: None=%v, Some=%v", outNone.None, outNone.Some)
		}

		// Payload case: Some
		valSome := uint32(777)
		inputSome := UnsafeUnitVariant{Some: &valSome}
		consumed, err = enc.LowerToStack(ct, unsafe.Pointer(&inputSome), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack payload failed: %v", err)
		}

		var outSome UnsafeUnitVariant
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&outSome), mem)
		if err != nil {
			t.Fatalf("LiftFromStack payload failed: %v", err)
		}
		if outSome.Some == nil || *outSome.Some != 777 || outSome.None != nil {
			t.Fatalf("payload lift mismatch: Some=%v, None=%v", outSome.Some, outSome.None)
		}
	})

	t.Run("unit case with struct pointer", func(t *testing.T) {
		variantDef := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "none", Type: nil},
					{Name: "some", Type: wit.U32{}},
				},
			},
		}

		type StructUnitVariant struct {
			None *struct{}
			Some *uint32
		}

		ct, err := enc.compiler.Compile(variantDef, reflect.TypeOf(StructUnitVariant{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		// Unit case
		inputNone := StructUnitVariant{None: &struct{}{}}
		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&inputNone), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack unit failed: %v", err)
		}

		var outNone StructUnitVariant
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&outNone), mem)
		if err != nil {
			t.Fatalf("LiftFromStack unit failed: %v", err)
		}
		if outNone.None == nil || outNone.Some != nil {
			t.Fatalf("unit lift mismatch: None=%v, Some=%v", outNone.None, outNone.Some)
		}
	})

	t.Run("tag support", func(t *testing.T) {
		variantDef := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "first-case", Type: wit.U32{}},
					{Name: "second-case", Type: wit.String{}},
				},
			},
		}

		type TaggedVariant struct {
			RenamedFirst  *uint32 `wit:"first-case"`
			RenamedSecond *string `wit:"second-case"`
		}

		ct, err := enc.compiler.Compile(variantDef, reflect.TypeOf(TaggedVariant{}))
		if err != nil {
			t.Fatalf("Compile tagged variant failed: %v", err)
		}

		val := uint32(55)
		input := TaggedVariant{RenamedFirst: &val}
		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var output TaggedVariant
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if output.RenamedFirst == nil || *output.RenamedFirst != 55 || output.RenamedSecond != nil {
			t.Fatalf("tagged variant lift mismatch: got %+v", output)
		}
	})

	t.Run("kebab-case support", func(t *testing.T) {
		variantDef := &wit.TypeDef{
			Kind: &wit.Variant{
				Cases: []wit.Case{
					{Name: "user-age", Type: wit.U32{}},
				},
			},
		}

		type KebabVariant struct {
			UserAge *uint32
		}

		ct, err := enc.compiler.Compile(variantDef, reflect.TypeOf(KebabVariant{}))
		if err != nil {
			t.Fatalf("Compile kebab variant failed: %v", err)
		}

		val := uint32(33)
		input := KebabVariant{UserAge: &val}
		stack := make([]uint64, ct.FlatCount)
		consumed, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var output KebabVariant
		_, err = dec.LiftFromStack(ct, stack[:consumed], unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}
		if output.UserAge == nil || *output.UserAge != 33 {
			t.Fatalf("kebab variant lift mismatch: got %+v", output)
		}
	})
}
