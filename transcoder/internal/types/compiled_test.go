package types

import (
	"reflect"
	"testing"
)

func TestCompiledTypeIsPrimitive(t *testing.T) {
	primitiveType := &CompiledType{Kind: KindU32}
	if !primitiveType.IsPrimitive() {
		t.Error("u32 should be primitive")
	}

	stringType := &CompiledType{Kind: KindString}
	if stringType.IsPrimitive() {
		t.Error("string should not be primitive")
	}
}

func TestCompiledTypeIsPure(t *testing.T) {
	t.Run("primitive_is_pure", func(t *testing.T) {
		ct := &CompiledType{Kind: KindU32}
		if !ct.IsPure() {
			t.Error("primitive should be pure")
		}
	})

	t.Run("string_not_pure", func(t *testing.T) {
		ct := &CompiledType{Kind: KindString}
		if ct.IsPure() {
			t.Error("string should not be pure")
		}
	})

	t.Run("list_not_pure", func(t *testing.T) {
		ct := &CompiledType{Kind: KindList}
		if ct.IsPure() {
			t.Error("list should not be pure")
		}
	})

	t.Run("pure_record", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindRecord,
			Fields: []Field{
				{Type: &CompiledType{Kind: KindU32}},
				{Type: &CompiledType{Kind: KindBool}},
			},
		}
		if !ct.IsPure() {
			t.Error("record with only primitives should be pure")
		}
	})

	t.Run("impure_record", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindRecord,
			Fields: []Field{
				{Type: &CompiledType{Kind: KindU32}},
				{Type: &CompiledType{Kind: KindString}},
			},
		}
		if ct.IsPure() {
			t.Error("record with string should not be pure")
		}
	})

	t.Run("pure_option", func(t *testing.T) {
		ct := &CompiledType{
			Kind:     KindOption,
			ElemType: &CompiledType{Kind: KindU32},
		}
		if !ct.IsPure() {
			t.Error("option<u32> should be pure")
		}
	})

	t.Run("impure_option", func(t *testing.T) {
		ct := &CompiledType{
			Kind:     KindOption,
			ElemType: &CompiledType{Kind: KindString},
		}
		if ct.IsPure() {
			t.Error("option<string> should not be pure")
		}
	})

	t.Run("pure_result", func(t *testing.T) {
		ct := &CompiledType{
			Kind:    KindResult,
			OkType:  &CompiledType{Kind: KindU32},
			ErrType: &CompiledType{Kind: KindBool},
		}
		if !ct.IsPure() {
			t.Error("result<u32, bool> should be pure")
		}
	})

	t.Run("impure_result_ok", func(t *testing.T) {
		ct := &CompiledType{
			Kind:    KindResult,
			OkType:  &CompiledType{Kind: KindString},
			ErrType: &CompiledType{Kind: KindBool},
		}
		if ct.IsPure() {
			t.Error("result<string, bool> should not be pure")
		}
	})

	t.Run("impure_result_err", func(t *testing.T) {
		ct := &CompiledType{
			Kind:    KindResult,
			OkType:  &CompiledType{Kind: KindU32},
			ErrType: &CompiledType{Kind: KindList},
		}
		if ct.IsPure() {
			t.Error("result<u32, list> should not be pure")
		}
	})

	t.Run("pure_variant", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindVariant,
			Cases: []Case{
				{Name: "none", Type: nil},
				{Name: "some", Type: &CompiledType{Kind: KindU32}},
			},
		}
		if !ct.IsPure() {
			t.Error("variant with only primitives should be pure")
		}
	})

	t.Run("impure_variant", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindVariant,
			Cases: []Case{
				{Name: "none", Type: nil},
				{Name: "some", Type: &CompiledType{Kind: KindString}},
			},
		}
		if ct.IsPure() {
			t.Error("variant with string should not be pure")
		}
	})

	t.Run("pure_tuple", func(t *testing.T) {
		ct := &CompiledType{
			Kind: KindTuple,
			Fields: []Field{
				{Type: &CompiledType{Kind: KindU32}},
				{Type: &CompiledType{Kind: KindBool}},
			},
		}
		if !ct.IsPure() {
			t.Error("tuple with only primitives should be pure")
		}
	})

	t.Run("enum_is_pure", func(t *testing.T) {
		ct := &CompiledType{Kind: KindEnum}
		if !ct.IsPure() {
			t.Error("enum should be pure")
		}
	})

	t.Run("flags_is_pure", func(t *testing.T) {
		ct := &CompiledType{Kind: KindFlags}
		if !ct.IsPure() {
			t.Error("flags should be pure")
		}
	})
}

func TestFieldStructure(t *testing.T) {
	field := Field{
		Name:      "TestField",
		WitName:   "test-field",
		GoOffset:  8,
		WitOffset: 4,
		Type:      &CompiledType{Kind: KindU32},
		IsPointer: false,
	}

	if field.Name != "TestField" {
		t.Error("Name not set correctly")
	}
	if field.WitName != "test-field" {
		t.Error("WitName not set correctly")
	}
	if field.GoOffset != 8 {
		t.Error("GoOffset not set correctly")
	}
	if field.WitOffset != 4 {
		t.Error("WitOffset not set correctly")
	}
	if field.IsPointer {
		t.Error("IsPointer should be false")
	}
}

func TestCaseStructure(t *testing.T) {
	cs := Case{
		Name:     "some",
		Type:     &CompiledType{Kind: KindU32},
		GoOffset: 16,
	}

	if cs.Name != "some" {
		t.Error("Name not set correctly")
	}
	if cs.Type.Kind != KindU32 {
		t.Error("Type not set correctly")
	}
	if cs.GoOffset != 16 {
		t.Error("GoOffset not set correctly")
	}
}

func TestCompiledTypeGoType(t *testing.T) {
	ct := &CompiledType{
		GoType:   reflect.TypeOf(uint32(0)),
		GoSize:   4,
		WitSize:  4,
		WitAlign: 4,
		Kind:     KindU32,
	}

	if ct.GoType.Kind() != reflect.Uint32 {
		t.Error("GoType not set correctly")
	}
	if ct.GoSize != 4 {
		t.Error("GoSize not set correctly")
	}
}
