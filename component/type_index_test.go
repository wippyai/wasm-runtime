package component

import (
	"os"
	"testing"
)

// TestComplexTypeIndexSpace verifies the TypeIndexSpace matches wasm-tools output
func TestComplexTypeIndexSpace(t *testing.T) {
	data, err := os.ReadFile("../testbed/complex.wasm")
	if err != nil {
		t.Skip("complex.wasm not found")
	}

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Expected types from wasm-tools print output
	// Type 0: instance type containing type definitions
	// Types 1,3,5,7,9,11,13: aliases to instance exports (typeAlias)
	// Types 2,4,6,8,10,12,14: type imports (TypeIndexRef)
	// Types 15+: component function and value types
	expected := map[int]string{
		0:  "InstanceType", // instance type with nested types
		1:  "typeAlias",    // alias export 0 "point"
		2:  "TypeIndexRef", // import "point" (type (eq 1))
		3:  "typeAlias",    // alias export 0 "person"
		4:  "TypeIndexRef", // import "person" (type (eq 3))
		5:  "typeAlias",    // alias export 0 "rectangle"
		6:  "TypeIndexRef", // import "rectangle" (type (eq 5))
		7:  "typeAlias",    // alias export 0 "color"
		8:  "TypeIndexRef", // import "color" (type (eq 7))
		9:  "typeAlias",    // alias export 0 "shape"
		10: "TypeIndexRef", // import "shape" (type (eq 9))
		11: "typeAlias",    // alias export 0 "permissions"
		12: "TypeIndexRef", // import "permissions" (type (eq 11))
		13: "typeAlias",    // alias export 0 "error-info"
		14: "TypeIndexRef", // import "error-info" (type (eq 13))

		// Component function and value types
		15: "FuncType",   // func (param "p" 2) (result 2)
		16: "FuncType",   // func (param "p" 4) (result 4)
		17: "FuncType",   // func (param "r" 6) (result 6)
		18: "FuncType",   // func (param "c" 8) (result 8)
		19: "FuncType",   // func (param "s" 10) (result 10)
		20: "FuncType",   // func (param "p" 12) (result 12)
		21: "ListType",   // list s32
		22: "FuncType",   // func (param "items" 21) (result 21)
		23: "ListType",   // list string
		24: "FuncType",   // func (param "items" 23) (result 23)
		25: "ListType",   // list 2 (list of points)
		26: "FuncType",   // func (param "items" 25) (result 25)
		27: "OptionType", // option 2
		28: "FuncType",   // func (param "make-some" bool) (result 27)
		29: "OptionType", // option string
		30: "FuncType",   // func (param "make-some" bool) (result 29)
		31: "ResultType", // result s32 (error 14)
		32: "FuncType",   // func (param "a" s32) (param "b" s32) (result 31)
		33: "ResultType", // result s32 (error string)
		34: "FuncType",   // func (param "s" string) (result 33)
		35: "TupleType",  // tuple s32 s32
		36: "FuncType",   // func (param "a" s32) (param "b" s32) (result 35)
		37: "TupleType",  // tuple s32 s32 s32
		38: "FuncType",   // func (param "x" s32) (result 37)
		39: "ListType",   // list 4 (list of persons)
		40: "FuncType",   // func (param "people" 39) (result 39)
		41: "FuncType",   // func (param "numbers" 21) (result s64)
		42: "FuncType",   // func (param "numbers" 21) (result 21)
	}

	t.Logf("TypeIndexSpace has %d types", len(comp.TypeIndexSpace))

	// Check each expected type
	for idx, expectedKind := range expected {
		if idx >= len(comp.TypeIndexSpace) {
			t.Errorf("Type[%d]: missing (expected %s)", idx, expectedKind)
			continue
		}

		actualType := comp.TypeIndexSpace[idx]
		actualKind := getTypeKind(actualType)

		if actualKind != expectedKind {
			t.Errorf("Type[%d]: got %s, want %s", idx, actualKind, expectedKind)
		} else {
			t.Logf("Type[%d]: ✓ %s", idx, actualKind)
		}
	}

	// Report any extra types
	if len(comp.TypeIndexSpace) > len(expected) {
		for i := len(expected); i < len(comp.TypeIndexSpace); i++ {
			t.Logf("Type[%d]: extra - %s", i, getTypeKind(comp.TypeIndexSpace[i]))
		}
	}
}

func getTypeKind(t Type) string {
	switch t.(type) {
	case RecordType:
		return "RecordType"
	case VariantType:
		return "VariantType"
	case ListType:
		return "ListType"
	case TupleType:
		return "TupleType"
	case FlagsType:
		return "FlagsType"
	case EnumType:
		return "EnumType"
	case OptionType:
		return "OptionType"
	case ResultType:
		return "ResultType"
	case *FuncType:
		return "FuncType"
	case *InstanceType:
		return "InstanceType"
	case PrimValType:
		return "PrimValType"
	case TypeIndexRef:
		return "TypeIndexRef"
	case typeAlias:
		return "typeAlias"
	default:
		return "Unknown"
	}
}

func TestMinimalTypeIndexSpace(t *testing.T) {
	data, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Skip("minimal.wasm not found")
	}

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Minimal component should have at least some types
	if len(comp.TypeIndexSpace) == 0 {
		t.Error("expected at least one type in minimal.wasm")
	}
}

func TestCalculatorTypeIndexSpace(t *testing.T) {
	data, err := os.ReadFile("../testbed/calculator.wasm")
	if err != nil {
		t.Skip("calculator.wasm not found")
	}

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Calculator component should have function types for add, sub, mul, div
	if len(comp.TypeIndexSpace) < 4 {
		t.Errorf("expected at least 4 types for calculator operations, got %d", len(comp.TypeIndexSpace))
	}

	// Verify we have at least one FuncType
	hasFuncType := false
	for _, typ := range comp.TypeIndexSpace {
		if _, ok := typ.(*FuncType); ok {
			hasFuncType = true
			break
		}
	}
	if !hasFuncType {
		t.Error("expected at least one FuncType in calculator component")
	}
}
