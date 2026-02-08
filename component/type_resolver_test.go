package component

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestNewTypeResolverWithInstances(t *testing.T) {
	types := []Type{
		PrimValType{Type: PrimU32},
		PrimValType{Type: PrimString},
	}
	instances := []uint32{0, 1}

	r := NewTypeResolverWithInstances(types, instances)
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
	if len(r.types) != 2 {
		t.Errorf("expected 2 types, got %d", len(r.types))
	}
	if len(r.instanceTypes) != 2 {
		t.Errorf("expected 2 instance types, got %d", len(r.instanceTypes))
	}
}

func TestTypeResolver_ResolvePrimitives(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	tests := []struct {
		expected wit.Type
		prim     PrimType
	}{
		{wit.Bool{}, PrimBool},
		{wit.S8{}, PrimS8},
		{wit.U8{}, PrimU8},
		{wit.S16{}, PrimS16},
		{wit.U16{}, PrimU16},
		{wit.S32{}, PrimS32},
		{wit.U32{}, PrimU32},
		{wit.S64{}, PrimS64},
		{wit.U64{}, PrimU64},
		{wit.F32{}, PrimF32},
		{wit.F64{}, PrimF64},
		{wit.Char{}, PrimChar},
		{wit.String{}, PrimString},
	}

	for _, tc := range tests {
		result, err := r.Resolve(PrimValType{Type: tc.prim})
		if err != nil {
			t.Errorf("PrimType %d: unexpected error: %v", tc.prim, err)
			continue
		}
		if result == nil {
			t.Errorf("PrimType %d: expected non-nil wit.Type", tc.prim)
		}
	}
}

func TestTypeResolver_ResolvePrimitiveUnknown(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	_, err := r.Resolve(PrimValType{Type: PrimType(0xFF)})
	if err == nil {
		t.Error("expected error for unknown primitive type")
	}
}

func TestTypeResolver_ResolveRecord(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	rec := RecordType{
		Fields: []FieldType{
			{Name: "x", Type: PrimValType{Type: PrimU32}},
			{Name: "y", Type: PrimValType{Type: PrimF64}},
		},
	}

	result, err := r.Resolve(rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witRec, ok := td.Kind.(*wit.Record)
	if !ok {
		t.Fatalf("expected *wit.Record, got %T", td.Kind)
	}

	if len(witRec.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(witRec.Fields))
	}
	if witRec.Fields[0].Name != "x" {
		t.Errorf("expected field name 'x', got %q", witRec.Fields[0].Name)
	}
}

func TestTypeResolver_ResolveList(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	list := ListType{ElemType: PrimValType{Type: PrimU32}}

	result, err := r.Resolve(list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witList, ok := td.Kind.(*wit.List)
	if !ok {
		t.Fatalf("expected *wit.List, got %T", td.Kind)
	}

	if _, ok := witList.Type.(wit.U32); !ok {
		t.Errorf("expected wit.U32 element type, got %T", witList.Type)
	}
}

func TestTypeResolver_ResolveTuple(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	tuple := TupleType{
		Types: []ValType{
			PrimValType{Type: PrimU32},
			PrimValType{Type: PrimString},
		},
	}

	result, err := r.Resolve(tuple)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witTuple, ok := td.Kind.(*wit.Tuple)
	if !ok {
		t.Fatalf("expected *wit.Tuple, got %T", td.Kind)
	}

	if len(witTuple.Types) != 2 {
		t.Errorf("expected 2 types, got %d", len(witTuple.Types))
	}
}

func TestTypeResolver_ResolveFlags(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	flags := FlagsType{Names: []string{"read", "write", "execute"}}

	result, err := r.Resolve(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witFlags, ok := td.Kind.(*wit.Flags)
	if !ok {
		t.Fatalf("expected *wit.Flags, got %T", td.Kind)
	}

	if len(witFlags.Flags) != 3 {
		t.Errorf("expected 3 flags, got %d", len(witFlags.Flags))
	}
}

func TestTypeResolver_ResolveEnum(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	enum := EnumType{Cases: []string{"red", "green", "blue"}}

	result, err := r.Resolve(enum)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witEnum, ok := td.Kind.(*wit.Enum)
	if !ok {
		t.Fatalf("expected *wit.Enum, got %T", td.Kind)
	}

	if len(witEnum.Cases) != 3 {
		t.Errorf("expected 3 cases, got %d", len(witEnum.Cases))
	}
}

func TestTypeResolver_ResolveOption(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	option := OptionType{Type: PrimValType{Type: PrimU64}}

	result, err := r.Resolve(option)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witOption, ok := td.Kind.(*wit.Option)
	if !ok {
		t.Fatalf("expected *wit.Option, got %T", td.Kind)
	}

	if _, ok := witOption.Type.(wit.U64); !ok {
		t.Errorf("expected wit.U64, got %T", witOption.Type)
	}
}

func TestTypeResolver_ResolveResult(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	okType := ValType(PrimValType{Type: PrimU32})
	errType := ValType(PrimValType{Type: PrimString})

	result := ResultType{OK: &okType, Err: &errType}

	resolved, err := r.Resolve(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := resolved.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", resolved)
	}

	witResult, ok := td.Kind.(*wit.Result)
	if !ok {
		t.Fatalf("expected *wit.Result, got %T", td.Kind)
	}

	if witResult.OK == nil || witResult.Err == nil {
		t.Error("expected both OK and Err to be non-nil")
	}
}

func TestTypeResolver_ResolveResultNils(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	// result<_, _>
	result := ResultType{OK: nil, Err: nil}

	resolved, err := r.Resolve(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := resolved.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", resolved)
	}

	witResult, ok := td.Kind.(*wit.Result)
	if !ok {
		t.Fatalf("expected *wit.Result, got %T", td.Kind)
	}

	if witResult.OK != nil || witResult.Err != nil {
		t.Error("expected both OK and Err to be nil")
	}
}

func TestTypeResolver_ResolveVariant(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	someType := ValType(PrimValType{Type: PrimU64})

	variant := VariantType{
		Cases: []CaseType{
			{Name: "none", Type: nil},
			{Name: "some", Type: &someType},
		},
	}

	result, err := r.Resolve(variant)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witVariant, ok := td.Kind.(*wit.Variant)
	if !ok {
		t.Fatalf("expected *wit.Variant, got %T", td.Kind)
	}

	if len(witVariant.Cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(witVariant.Cases))
	}
	if witVariant.Cases[0].Name != "none" {
		t.Errorf("expected case name 'none', got %q", witVariant.Cases[0].Name)
	}
	if witVariant.Cases[0].Type != nil {
		t.Error("expected none case to have nil type")
	}
	if witVariant.Cases[1].Type == nil {
		t.Error("expected some case to have non-nil type")
	}
}

func TestTypeResolver_ResolveBorrow(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	result, err := r.Resolve(BorrowType{TypeIndex: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(wit.U32); !ok {
		t.Errorf("expected wit.U32 for borrow, got %T", result)
	}
}

func TestTypeResolver_ResolveOwn(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	result, err := r.Resolve(OwnType{TypeIndex: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(wit.U32); !ok {
		t.Errorf("expected wit.U32 for own, got %T", result)
	}
}

func TestTypeResolver_ResolveTypeIndex(t *testing.T) {
	types := []Type{
		PrimValType{Type: PrimU32},
		RecordType{Fields: []FieldType{{Name: "x", Type: PrimValType{Type: PrimU32}}}},
	}
	r := NewTypeResolverWithInstances(types, nil)

	// Resolve primitive at index 0
	result, err := r.Resolve(TypeIndexRef{Index: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(wit.U32); !ok {
		t.Errorf("expected wit.U32, got %T", result)
	}

	// Resolve record at index 1
	result, err = r.Resolve(TypeIndexRef{Index: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}
	if _, ok := td.Kind.(*wit.Record); !ok {
		t.Errorf("expected *wit.Record, got %T", td.Kind)
	}
}

func TestTypeResolver_ResolveTypeIndexOutOfRange(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	_, err := r.Resolve(TypeIndexRef{Index: 100})
	if err == nil {
		t.Error("expected error for out of range index")
	}
}

func TestTypeResolver_ResolveTypeIndexAllTypes(t *testing.T) {
	// Test various types through type index resolution
	types := []Type{
		ListType{ElemType: PrimValType{Type: PrimU32}},
		TupleType{Types: []ValType{PrimValType{Type: PrimU32}}},
		FlagsType{Names: []string{"a"}},
		EnumType{Cases: []string{"b"}},
		OptionType{Type: PrimValType{Type: PrimU32}},
		OwnType{TypeIndex: 0},
		BorrowType{TypeIndex: 0},
	}
	r := NewTypeResolverWithInstances(types, nil)

	for i := range types {
		_, err := r.Resolve(TypeIndexRef{Index: uint32(i)})
		if err != nil {
			t.Errorf("index %d: unexpected error: %v", i, err)
		}
	}
}

func TestTypeResolver_ResolveTypeIndexFuncType(t *testing.T) {
	types := []Type{
		&FuncType{},
	}
	r := NewTypeResolverWithInstances(types, nil)

	_, err := r.Resolve(TypeIndexRef{Index: 0})
	if err == nil {
		t.Error("expected error for function type resolution")
	}
}

func TestTypeResolver_ResolveTypeIndexInstanceType(t *testing.T) {
	types := []Type{
		&InstanceType{},
	}
	r := NewTypeResolverWithInstances(types, nil)

	// Instance type in value position resolves to u32 (resource handle)
	result, err := r.Resolve(TypeIndexRef{Index: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(wit.U32); !ok {
		t.Errorf("expected wit.U32, got %T", result)
	}
}

func TestTypeResolver_ResolveTypeIndexTypeIndexRef(t *testing.T) {
	// Test nested type index references
	types := []Type{
		PrimValType{Type: PrimU64},
		TypeIndexRef{Index: 0}, // Points to index 0
	}
	r := NewTypeResolverWithInstances(types, nil)

	result, err := r.Resolve(TypeIndexRef{Index: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(wit.U64); !ok {
		t.Errorf("expected wit.U64, got %T", result)
	}
}

func TestTypeResolver_ResolveUnsupportedType(t *testing.T) {
	types := []Type{
		&componentTypeDecl{}, // componentTypeDecl is not a valid value type
	}
	r := NewTypeResolverWithInstances(types, nil)

	_, err := r.Resolve(TypeIndexRef{Index: 0})
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestTypeResolver_ResolveFunc(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	ResultType := ValType(PrimValType{Type: PrimString})
	f := &FuncType{
		Params: []paramType{
			{Name: "x", Type: PrimValType{Type: PrimU32}},
			{Name: "y", Type: PrimValType{Type: PrimF64}},
		},
		Result: &ResultType,
	}

	params, result, err := r.ResolveFunc(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 2 {
		t.Errorf("expected 2 params, got %d", len(params))
	}
	if _, ok := params[0].(wit.U32); !ok {
		t.Errorf("expected wit.U32, got %T", params[0])
	}
	if _, ok := params[1].(wit.F64); !ok {
		t.Errorf("expected wit.F64, got %T", params[1])
	}
	if _, ok := result.(wit.String); !ok {
		t.Errorf("expected wit.String, got %T", result)
	}
}

func TestTypeResolver_ResolveFuncNoResult(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	f := &FuncType{
		Params: []paramType{
			{Name: "x", Type: PrimValType{Type: PrimU32}},
		},
		Result: nil,
	}

	params, result, err := r.ResolveFunc(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 1 {
		t.Errorf("expected 1 param, got %d", len(params))
	}
	if result != nil {
		t.Errorf("expected nil result, got %T", result)
	}
}

func TestTypeResolver_ResolveFuncType(t *testing.T) {
	ft := &FuncType{
		Params: []paramType{
			{Name: "x", Type: PrimValType{Type: PrimU32}},
		},
	}
	types := []Type{ft}
	r := NewTypeResolverWithInstances(types, nil)

	result, err := r.ResolveFuncType(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ft {
		t.Error("expected same function type pointer")
	}
}

func TestTypeResolver_ResolveFuncTypeOutOfRange(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	_, err := r.ResolveFuncType(100)
	if err == nil {
		t.Error("expected error for out of range index")
	}
}

func TestTypeResolver_ResolveFuncTypeNotFunc(t *testing.T) {
	types := []Type{PrimValType{Type: PrimU32}}
	r := NewTypeResolverWithInstances(types, nil)

	_, err := r.ResolveFuncType(0)
	if err == nil {
		t.Error("expected error for non-function type")
	}
}

func TestTypeResolver_ResolveFuncWithInternalTypes(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	internalTypes := map[uint32]Type{
		0: PrimValType{Type: PrimU64},
	}

	ResultType := ValType(TypeIndexRef{Index: 0})
	f := &FuncType{
		Params: []paramType{
			{Name: "x", Type: TypeIndexRef{Index: 0}},
		},
		Result: &ResultType,
	}

	params, result, err := r.ResolveFuncWithInternalTypes(f, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 1 {
		t.Errorf("expected 1 param, got %d", len(params))
	}
	if _, ok := params[0].(wit.U64); !ok {
		t.Errorf("expected wit.U64 from internal type, got %T", params[0])
	}
	if _, ok := result.(wit.U64); !ok {
		t.Errorf("expected wit.U64 from internal type, got %T", result)
	}
}

func TestTypeResolver_ResolveInternalType_Record(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	internalTypes := map[uint32]Type{
		0: PrimValType{Type: PrimF32},
	}

	rec := RecordType{
		Fields: []FieldType{
			{Name: "x", Type: TypeIndexRef{Index: 0}},
		},
	}

	result, err := r.resolveInternalType(rec, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witRec, ok := td.Kind.(*wit.Record)
	if !ok {
		t.Fatalf("expected *wit.Record, got %T", td.Kind)
	}

	if _, ok := witRec.Fields[0].Type.(wit.F32); !ok {
		t.Errorf("expected wit.F32, got %T", witRec.Fields[0].Type)
	}
}

func TestTypeResolver_ResolveInternalType_List(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	internalTypes := map[uint32]Type{
		0: PrimValType{Type: PrimU32},
	}

	list := ListType{ElemType: TypeIndexRef{Index: 0}}

	result, err := r.resolveInternalType(list, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witList, ok := td.Kind.(*wit.List)
	if !ok {
		t.Fatalf("expected *wit.List, got %T", td.Kind)
	}

	if _, ok := witList.Type.(wit.U32); !ok {
		t.Errorf("expected wit.U32, got %T", witList.Type)
	}
}

func TestTypeResolver_ResolveInternalType_Tuple(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	internalTypes := map[uint32]Type{
		0: PrimValType{Type: PrimF64},
	}

	tuple := TupleType{Types: []ValType{TypeIndexRef{Index: 0}}}

	result, err := r.resolveInternalType(tuple, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witTuple, ok := td.Kind.(*wit.Tuple)
	if !ok {
		t.Fatalf("expected *wit.Tuple, got %T", td.Kind)
	}

	if _, ok := witTuple.Types[0].(wit.F64); !ok {
		t.Errorf("expected wit.F64, got %T", witTuple.Types[0])
	}
}

func TestTypeResolver_ResolveInternalType_Option(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	internalTypes := map[uint32]Type{
		0: PrimValType{Type: PrimU64},
	}

	option := OptionType{Type: TypeIndexRef{Index: 0}}

	result, err := r.resolveInternalType(option, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witOption, ok := td.Kind.(*wit.Option)
	if !ok {
		t.Fatalf("expected *wit.Option, got %T", td.Kind)
	}

	if _, ok := witOption.Type.(wit.U64); !ok {
		t.Errorf("expected wit.U64, got %T", witOption.Type)
	}
}

func TestTypeResolver_ResolveInternalType_Result(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	internalTypes := map[uint32]Type{
		0: PrimValType{Type: PrimU32},
		1: PrimValType{Type: PrimString},
	}

	okType := ValType(TypeIndexRef{Index: 0})
	errType := ValType(TypeIndexRef{Index: 1})
	res := ResultType{OK: &okType, Err: &errType}

	result, err := r.resolveInternalType(res, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witResult, ok := td.Kind.(*wit.Result)
	if !ok {
		t.Fatalf("expected *wit.Result, got %T", td.Kind)
	}

	if witResult.OK == nil || witResult.Err == nil {
		t.Error("expected both OK and Err")
	}
}

func TestTypeResolver_ResolveInternalType_Variant(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	internalTypes := map[uint32]Type{
		0: PrimValType{Type: PrimU64},
	}

	someType := ValType(TypeIndexRef{Index: 0})
	variant := VariantType{
		Cases: []CaseType{
			{Name: "none", Type: nil},
			{Name: "some", Type: &someType},
		},
	}

	result, err := r.resolveInternalType(variant, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("expected *wit.TypeDef, got %T", result)
	}

	witVariant, ok := td.Kind.(*wit.Variant)
	if !ok {
		t.Fatalf("expected *wit.Variant, got %T", td.Kind)
	}

	if len(witVariant.Cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(witVariant.Cases))
	}
}

func TestTypeResolver_ResolveInternalType_NestedRef(t *testing.T) {
	globalTypes := []Type{
		PrimValType{Type: PrimS64},
	}
	r := NewTypeResolverWithInstances(globalTypes, nil)

	internalTypes := map[uint32]Type{
		0: TypeIndexRef{Index: 1}, // Points to internal index 1
		1: PrimValType{Type: PrimF32},
	}

	// TypeIndexRef -> TypeIndexRef -> PrimValType
	result, err := r.resolveInternalType(TypeIndexRef{Index: 0}, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(wit.F32); !ok {
		t.Errorf("expected wit.F32 through nested ref, got %T", result)
	}
}

func TestTypeResolver_ResolveInternalType_FallbackToGlobal(t *testing.T) {
	globalTypes := []Type{
		PrimValType{Type: PrimS64},
	}
	r := NewTypeResolverWithInstances(globalTypes, nil)

	internalTypes := map[uint32]Type{}

	// Index 0 not in internal types, falls back to global
	result, err := r.resolveInternalType(TypeIndexRef{Index: 0}, internalTypes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(wit.S64); !ok {
		t.Errorf("expected wit.S64 from global, got %T", result)
	}
}

// typeAlias resolution tests

func TestTypeResolver_ResolveTypeAlias(t *testing.T) {
	// Create an instance type with a type declaration and export
	instType := &InstanceType{
		Decls: []InstanceDecl{
			{
				Name:     "",
				DeclType: InstanceDeclType{Type: PrimValType{Type: PrimU32}},
			},
			{
				Name: "my-type",
				DeclType: InstanceDeclExport{
					Export: exportDecl{
						Name: "my-type",
						externDesc: externDesc{
							Kind:      0x03, // Type export
							TypeIndex: 0,    // Points to the type at index 0
						},
					},
				},
			},
		},
	}

	types := []Type{
		instType,
	}
	instanceTypes := []uint32{0}

	r := NewTypeResolverWithInstances(types, instanceTypes)

	alias := typeAlias{
		InstanceIdx: 0,
		ExportName:  "my-type",
	}

	result, err := r.Resolve(alias)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(wit.U32); !ok {
		t.Errorf("expected wit.U32, got %T", result)
	}
}

func TestTypeResolver_ResolveTypeAlias_InstanceOutOfRange(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)

	alias := typeAlias{
		InstanceIdx: 10,
		ExportName:  "foo",
	}

	_, err := r.Resolve(alias)
	if err == nil {
		t.Fatal("expected error for out of range instance")
	}
}

func TestTypeResolver_ResolveTypeAlias_TypeIndexOutOfRange(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, []uint32{100})

	alias := typeAlias{
		InstanceIdx: 0,
		ExportName:  "foo",
	}

	_, err := r.Resolve(alias)
	if err == nil {
		t.Fatal("expected error for out of range type index")
	}
}

func TestTypeResolver_ResolveTypeAlias_NotInstanceType(t *testing.T) {
	types := []Type{
		PrimValType{Type: PrimU32},
	}
	r := NewTypeResolverWithInstances(types, []uint32{0})

	alias := typeAlias{
		InstanceIdx: 0,
		ExportName:  "foo",
	}

	_, err := r.Resolve(alias)
	if err == nil {
		t.Fatal("expected error for non-instance type")
	}
}

func TestTypeResolver_ResolveTypeAlias_ExportNotFound(t *testing.T) {
	instType := &InstanceType{
		Decls: []InstanceDecl{},
	}

	types := []Type{instType}
	r := NewTypeResolverWithInstances(types, []uint32{0})

	alias := typeAlias{
		InstanceIdx: 0,
		ExportName:  "nonexistent",
	}

	_, err := r.Resolve(alias)
	if err == nil {
		t.Fatal("expected error for export not found")
	}
}

func TestTypeResolver_ResolveTypeAlias_InternalTypeNotFound(t *testing.T) {
	// Export points to internal index that doesn't exist
	instType := &InstanceType{
		Decls: []InstanceDecl{
			{
				Name: "missing-type",
				DeclType: InstanceDeclExport{
					Export: exportDecl{
						Name: "missing-type",
						externDesc: externDesc{
							Kind:      0x03,
							TypeIndex: 99, // Doesn't exist
						},
					},
				},
			},
		},
	}

	types := []Type{instType}
	r := NewTypeResolverWithInstances(types, []uint32{0})

	alias := typeAlias{
		InstanceIdx: 0,
		ExportName:  "missing-type",
	}

	_, err := r.Resolve(alias)
	if err == nil {
		t.Fatal("expected error for internal type not found")
	}
}
