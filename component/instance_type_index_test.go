package component

import (
	"testing"
)

// TestInstanceTypeIndexSpace verifies that the type index space within instance types
// is built correctly according to the Component Model spec:
// - Type declarations (kind=0x01) add to type index space
// - Type aliases (kind=0x02) add to type index space
// - Exports (kind=0x04) do NOT add to type index space
func TestInstanceTypeIndexSpace(t *testing.T) {
	// Create a simple instance type with:
	// - Type 0: a function type
	// - Type 1: another function type
	// - Export "foo" referencing type 0
	// - Export "bar" referencing type 1
	funcType0 := &FuncType{
		Params: []paramType{{Name: "x", Type: PrimValType{Type: PrimU32}}},
		Result: nil,
	}
	funcType1 := &FuncType{
		Params: []paramType{{Name: "a", Type: PrimValType{Type: PrimS32}}, {Name: "b", Type: PrimValType{Type: PrimS32}}},
		Result: func() *ValType { t := ValType(PrimValType{Type: PrimS32}); return &t }(),
	}

	instType := &InstanceType{
		Decls: []InstanceDecl{
			// Type declaration at index 0
			{
				ExternKind: 0x01,
				DeclType:   InstanceDeclType{Type: funcType0},
			},
			// Type declaration at index 1
			{
				ExternKind: 0x01,
				DeclType:   InstanceDeclType{Type: funcType1},
			},
			// Export "foo" -> func at type index 0
			{
				Name:       "foo",
				ExternKind: 0x04,
				DeclType: InstanceDeclExport{Export: exportDecl{
					Name:       "foo",
					externDesc: externDesc{Kind: 0x01, TypeIndex: 0}, // func, type 0
				}},
			},
			// Export "bar" -> func at type index 1
			{
				Name:       "bar",
				ExternKind: 0x04,
				DeclType: InstanceDeclExport{Export: exportDecl{
					Name:       "bar",
					externDesc: externDesc{Kind: 0x01, TypeIndex: 1}, // func, type 1
				}},
			},
		},
	}

	reg := &CanonRegistry{}

	t.Run("find foo function", func(t *testing.T) {
		ft, internalTypes := reg.findFuncInInstanceType(instType, "foo")
		if ft == nil {
			t.Fatal("expected to find function 'foo', got nil")
		}
		if len(ft.Params) != 1 || ft.Params[0].Name != "x" {
			t.Errorf("wrong function type for 'foo': got %+v", ft)
		}
		if len(internalTypes) != 2 {
			t.Errorf("expected 2 internal types, got %d", len(internalTypes))
		}
	})

	t.Run("find bar function", func(t *testing.T) {
		ft, internalTypes := reg.findFuncInInstanceType(instType, "bar")
		if ft == nil {
			t.Fatal("expected to find function 'bar', got nil")
		}
		if len(ft.Params) != 2 || ft.Params[0].Name != "a" {
			t.Errorf("wrong function type for 'bar': got %+v", ft)
		}
		if len(internalTypes) != 2 {
			t.Errorf("expected 2 internal types, got %d", len(internalTypes))
		}
	})

	t.Run("not found function", func(t *testing.T) {
		ft, _ := reg.findFuncInInstanceType(instType, "nonexistent")
		if ft != nil {
			t.Errorf("expected nil for nonexistent function, got %+v", ft)
		}
	})
}

// TestInstanceTypeTypeExportsAddToTypeIndex verifies that TYPE exports (Kind=0x03)
// add to the type index space (they create fresh type bounds)
func TestInstanceTypeTypeExportsAddToTypeIndex(t *testing.T) {
	funcType := &FuncType{
		Params: []paramType{{Name: "self", Type: PrimValType{Type: PrimU32}}},
		Result: nil,
	}

	// Instance type mimicking WASI pattern:
	// - Type export "pollable" (creates fresh resource type at index 0)
	// - Type declaration (function type at index 1)
	// - Export "method" referencing type 1
	instType := &InstanceType{
		Decls: []InstanceDecl{
			// Type export - DOES add to type index space (index 0)
			{
				Name:       "pollable",
				ExternKind: 0x04,
				DeclType: InstanceDeclExport{Export: exportDecl{
					Name:       "pollable",
					externDesc: externDesc{Kind: 0x03, TypeIndex: 0}, // type export
				}},
			},
			// Type declaration - at index 1 (after the type export)
			{
				ExternKind: 0x01,
				DeclType:   InstanceDeclType{Type: funcType},
			},
			// Export "method" -> func at type index 1
			{
				Name:       "method",
				ExternKind: 0x04,
				DeclType: InstanceDeclExport{Export: exportDecl{
					Name:       "method",
					externDesc: externDesc{Kind: 0x01, TypeIndex: 1}, // func, type 1
				}},
			},
		},
	}

	reg := &CanonRegistry{}
	ft, internalTypes := reg.findFuncInInstanceType(instType, "method")

	if ft == nil {
		t.Fatal("expected to find function 'method', got nil")
	}
	if len(ft.Params) != 1 || ft.Params[0].Name != "self" {
		t.Errorf("wrong function type: got %+v", ft)
	}
	// 1 type export + 1 type declaration = 2 entries in internal types
	if len(internalTypes) != 2 {
		t.Errorf("expected 2 internal types (type export + type decl), got %d", len(internalTypes))
	}
	// Type at index 0 should be the resource type (represented as u32)
	if _, ok := internalTypes[0].(PrimValType); !ok {
		t.Errorf("expected type 0 to be PrimValType (resource), got %T", internalTypes[0])
	}
	// Type at index 1 should be the function type
	if _, ok := internalTypes[1].(*FuncType); !ok {
		t.Errorf("expected type 1 to be *FuncType, got %T", internalTypes[1])
	}
}

// TestInstanceTypeWithMixedDecls tests a complex instance type with mixed declarations
func TestInstanceTypeWithMixedDecls(t *testing.T) {
	recType := RecordType{
		Fields: []FieldType{
			{Name: "value", Type: PrimValType{Type: PrimU64}},
		},
	}
	funcType := &FuncType{
		Params: []paramType{{Name: "self", Type: PrimValType{Type: PrimU32}}},
		Result: func() *ValType { t := ValType(TypeIndexRef{Index: 0}); return &t }(), // result is type 0 (record)
	}

	// Complex instance type like WASI interfaces:
	// - Type 0: record type
	// - Type 1: function type
	// - Func export "get-value" referencing type 1
	// Note: no type export in this test to keep it simpler
	instType := &InstanceType{
		Decls: []InstanceDecl{
			// Type 0: record
			{
				ExternKind: 0x01,
				DeclType:   InstanceDeclType{Type: recType},
			},
			// Type 1: function
			{
				ExternKind: 0x01,
				DeclType:   InstanceDeclType{Type: funcType},
			},
			// Func export (doesn't add to type index)
			{
				Name:       "get-value",
				ExternKind: 0x04,
				DeclType: InstanceDeclExport{Export: exportDecl{
					Name:       "get-value",
					externDesc: externDesc{Kind: 0x01, TypeIndex: 1}, // func at type 1
				}},
			},
		},
	}

	reg := &CanonRegistry{}
	ft, internalTypes := reg.findFuncInInstanceType(instType, "get-value")

	if ft == nil {
		t.Fatal("expected to find function 'get-value', got nil")
	}
	if len(ft.Params) != 1 || ft.Params[0].Name != "self" {
		t.Errorf("wrong function params: got %+v", ft.Params)
	}
	// 2 type declarations
	if len(internalTypes) != 2 {
		t.Errorf("expected 2 internal types, got %d", len(internalTypes))
	}
	// Verify type 0 is the record
	if _, ok := internalTypes[0].(RecordType); !ok {
		t.Errorf("expected type 0 to be RecordType, got %T", internalTypes[0])
	}
}
