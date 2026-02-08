package component

import (
	"testing"

	"github.com/wippyai/wasm-runtime/component/internal/arena"
)

// TestWitTypesInterfaceImplementation tests that all wit types implement their interfaces
func TestWitTypesInterfaceImplementation(t *testing.T) {
	// Test DefType implementations
	var _ DefType = RecordType{}
	var _ DefType = VariantType{}
	var _ DefType = ListType{}
	var _ DefType = TupleType{}
	var _ DefType = FlagsType{}
	var _ DefType = EnumType{}
	var _ DefType = OptionType{}
	var _ DefType = ResultType{}
	var _ DefType = OwnType{}
	var _ DefType = BorrowType{}

	// Test ValType implementations
	var _ ValType = RecordType{}
	var _ ValType = VariantType{}
	var _ ValType = ListType{}
	var _ ValType = TupleType{}
	var _ ValType = FlagsType{}
	var _ ValType = EnumType{}
	var _ ValType = OptionType{}
	var _ ValType = ResultType{}
	var _ ValType = PrimValType{}
	var _ ValType = TypeIndexRef{}
	var _ ValType = OwnType{}
	var _ ValType = BorrowType{}
	var _ ValType = typeAlias{}

	// Test Type implementations
	var _ Type = RecordType{}
	var _ Type = VariantType{}
	var _ Type = ListType{}
	var _ Type = TupleType{}
	var _ Type = FlagsType{}
	var _ Type = EnumType{}
	var _ Type = OptionType{}
	var _ Type = ResultType{}
	var _ Type = FuncType{}
	var _ Type = InstanceType{}
	var _ Type = componentTypeDecl{}
	var _ Type = PrimValType{}
	var _ Type = TypeIndexRef{}
	var _ Type = typeAlias{}

	// Call marker methods to ensure they're covered
	RecordType{}.isDefType()
	RecordType{}.isValType()
	RecordType{}.isType()

	VariantType{}.isDefType()
	VariantType{}.isValType()
	VariantType{}.isType()

	ListType{}.isDefType()
	ListType{}.isValType()
	ListType{}.isType()

	TupleType{}.isDefType()
	TupleType{}.isValType()
	TupleType{}.isType()

	FlagsType{}.isDefType()
	FlagsType{}.isValType()
	FlagsType{}.isType()

	EnumType{}.isDefType()
	EnumType{}.isValType()
	EnumType{}.isType()

	OptionType{}.isDefType()
	OptionType{}.isValType()
	OptionType{}.isType()

	ResultType{}.isDefType()
	ResultType{}.isValType()
	ResultType{}.isType()

	OwnType{}.isDefType()
	OwnType{}.isValType()
	OwnType{}.isType()

	BorrowType{}.isDefType()
	BorrowType{}.isValType()
	BorrowType{}.isType()

	PrimValType{}.isValType()
	PrimValType{}.isType()

	TypeIndexRef{}.isValType()
	TypeIndexRef{}.isType()

	typeAlias{}.isValType()
	typeAlias{}.isType()

	FuncType{}.isType()
	InstanceType{}.isType()
	componentTypeDecl{}.isType()
}

func TestInstanceDeclTypeMarkers(t *testing.T) {
	var _ instanceDeclType = InstanceDeclCoreType{}
	var _ instanceDeclType = InstanceDeclType{}
	var _ instanceDeclType = InstanceDeclAlias{}
	var _ instanceDeclType = InstanceDeclExport{}

	InstanceDeclCoreType{}.isInstanceDeclType()
	InstanceDeclType{}.isInstanceDeclType()
	InstanceDeclAlias{}.isInstanceDeclType()
	InstanceDeclExport{}.isInstanceDeclType()
}

func TestRecordTypeFields(t *testing.T) {
	rec := RecordType{
		Fields: []FieldType{
			{Name: "foo", Type: PrimValType{Type: PrimU32}},
			{Name: "bar", Type: PrimValType{Type: PrimString}},
		},
	}
	if len(rec.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(rec.Fields))
	}
	if rec.Fields[0].Name != "foo" {
		t.Errorf("expected field name 'foo', got %q", rec.Fields[0].Name)
	}
}

func TestVariantTypeCases(t *testing.T) {
	u32Type := ValType(PrimValType{Type: PrimU32})
	variant := VariantType{
		Cases: []CaseType{
			{Name: "none", Type: nil},
			{Name: "some", Type: &u32Type},
		},
	}
	if len(variant.Cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(variant.Cases))
	}
	if variant.Cases[0].Type != nil {
		t.Error("first case should have nil type")
	}
	if variant.Cases[1].Type == nil {
		t.Error("second case should have non-nil type")
	}
}

func TestResultType(t *testing.T) {
	ok := ValType(PrimValType{Type: PrimU32})
	err := ValType(PrimValType{Type: PrimString})
	result := ResultType{OK: &ok, Err: &err}

	if result.OK == nil {
		t.Error("OK should be non-nil")
	}
	if result.Err == nil {
		t.Error("Err should be non-nil")
	}
}

func TestOptionType(t *testing.T) {
	opt := OptionType{Type: PrimValType{Type: PrimU32}}
	if opt.Type == nil {
		t.Error("option type should be non-nil")
	}
}

func TestEnumType(t *testing.T) {
	enum := EnumType{Cases: []string{"a", "b", "c"}}
	if len(enum.Cases) != 3 {
		t.Errorf("expected 3 cases, got %d", len(enum.Cases))
	}
}

func TestFlagsType(t *testing.T) {
	flags := FlagsType{Names: []string{"read", "write", "execute"}}
	if len(flags.Names) != 3 {
		t.Errorf("expected 3 flags, got %d", len(flags.Names))
	}
}

func TestOwnBorrowTypes(t *testing.T) {
	own := OwnType{TypeIndex: 5}
	borrow := BorrowType{TypeIndex: 5}

	if own.TypeIndex != 5 {
		t.Errorf("own type idx = %d, want 5", own.TypeIndex)
	}
	if borrow.TypeIndex != 5 {
		t.Errorf("borrow type idx = %d, want 5", borrow.TypeIndex)
	}
}

func TestFuncType(t *testing.T) {
	result := ValType(PrimValType{Type: PrimBool})
	fn := FuncType{
		Params: []paramType{
			{Name: "x", Type: PrimValType{Type: PrimU32}},
		},
		Result: &result,
	}
	if len(fn.Params) != 1 {
		t.Errorf("expected 1 param, got %d", len(fn.Params))
	}
	if fn.Result == nil {
		t.Error("result should be non-nil")
	}
}

func TestPrimTypeConstants(t *testing.T) {
	// Test that primitive type constants have correct hex values per spec
	tests := []struct {
		prim PrimType
		hex  byte
	}{
		{PrimBool, 0x7f},
		{PrimS8, 0x7e},
		{PrimU8, 0x7d},
		{PrimS16, 0x7c},
		{PrimU16, 0x7b},
		{PrimS32, 0x7a},
		{PrimU32, 0x79},
		{PrimS64, 0x78},
		{PrimU64, 0x77},
		{PrimF32, 0x76},
		{PrimF64, 0x75},
		{PrimChar, 0x74},
		{PrimString, 0x73},
	}
	for _, tt := range tests {
		if byte(tt.prim) != tt.hex {
			t.Errorf("primitive %02x mismatch: got %02x", tt.hex, tt.prim)
		}
	}
}

func TestTypeArena(t *testing.T) {
	a := arena.NewTypeArena()

	// Test AllocDefined
	defIdx := a.AllocDefined(arena.DefinedType{Kind: arena.DefinedKindRecord})
	if defIdx != 0 {
		t.Errorf("first defined idx = %d, want 0", defIdx)
	}
	defIdx2 := a.AllocDefined(arena.DefinedType{Kind: arena.DefinedKindEnum})
	if defIdx2 != 1 {
		t.Errorf("second defined idx = %d, want 1", defIdx2)
	}

	// Test AllocFunc
	funcIdx := a.AllocFunc(arena.FuncTypeData{})
	if funcIdx != 0 {
		t.Errorf("first func idx = %d, want 0", funcIdx)
	}

	// Test AllocInstance
	instIdx := a.AllocInstance(arena.InstanceTypeData{})
	if instIdx != 0 {
		t.Errorf("first instance idx = %d, want 0", instIdx)
	}

	// Test AllocComponent
	compIdx := a.AllocComponent(arena.TypeData{})
	if compIdx != 0 {
		t.Errorf("first component idx = %d, want 0", compIdx)
	}

	// Test AllocResource
	resIdx := a.AllocResource()
	if resIdx != 0 {
		t.Errorf("first resource idx = %d, want 0", resIdx)
	}

	// Test GetDefined
	got := a.GetDefined(defIdx)
	if got == nil {
		t.Fatal("should return defined type")
	}
	if got.Kind != arena.DefinedKindRecord {
		t.Errorf("kind = %d, want DefinedKindRecord", got.Kind)
	}

	// Test GetFunc
	fn := a.GetFunc(funcIdx)
	if fn == nil {
		t.Error("should return func type")
	}

	// Test GetInstance
	inst := a.GetInstance(instIdx)
	if inst == nil {
		t.Error("should return instance type")
	}

	// Test GetComponent
	comp := a.GetComponent(compIdx)
	if comp == nil {
		t.Error("should return component type")
	}

	// Test out of bounds
	if a.GetDefined(999) != nil {
		t.Error("out of bounds should return nil")
	}
	if a.GetFunc(999) != nil {
		t.Error("out of bounds should return nil")
	}
	if a.GetInstance(999) != nil {
		t.Error("out of bounds should return nil")
	}
	if a.GetComponent(999) != nil {
		t.Error("out of bounds should return nil")
	}
}

func TestComponentState(t *testing.T) {
	state := arena.NewState(arena.KindComponent)

	// Test type operations
	state.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: 0})
	state.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: 1})
	if state.TypeCount() != 2 {
		t.Errorf("type count = %d, want 2", state.TypeCount())
	}

	typ, err := state.GetType(0)
	if err != nil {
		t.Errorf("type 0 error: %v", err)
	}
	if typ.Kind != arena.TypeKindDefined {
		t.Errorf("type 0 kind = %d, want TypeKindDefined", typ.Kind)
	}

	// Test func operations
	state.AddFunc(0)
	if state.FuncCount() != 1 {
		t.Errorf("func count = %d, want 1", state.FuncCount())
	}
	_, err = state.GetFunc(0)
	if err != nil {
		t.Errorf("func 0 error: %v", err)
	}

	// Test value operations
	state.AddValue(arena.ValType{Primitive: arena.PrimU32})
	_, err = state.GetValue(0)
	if err != nil {
		t.Errorf("value 0 error: %v", err)
	}
	state.MarkValueUsed(0)

	// Test instance operations
	state.AddInstance(10)
	if state.InstanceCount() != 1 {
		t.Errorf("instance count = %d, want 1", state.InstanceCount())
	}
	_, err = state.GetInstance(0)
	if err != nil {
		t.Errorf("instance 0 error: %v", err)
	}

	// Test component operations
	state.AddComponent(20)
	_, err = state.GetComponent(0)
	if err != nil {
		t.Errorf("component 0 error: %v", err)
	}

	// Test imports
	state.AddImport("test", arena.EntityType{})

	// Test exports
	state.AddExport("out", arena.EntityType{})
}

func TestCoreInstanceKind(t *testing.T) {
	// Test that CoreInstanceKind constants work
	if CoreInstanceInstantiate != 0 {
		t.Errorf("CoreInstanceInstantiate = %d, want 0", CoreInstanceInstantiate)
	}
	if CoreInstanceFromExports != 1 {
		t.Errorf("CoreInstanceFromExports = %d, want 1", CoreInstanceFromExports)
	}
}

func TestTypeAliasType(t *testing.T) {
	alias := typeAlias{
		InstanceIdx: 3,
		ExportName:  "my-type",
	}
	if alias.InstanceIdx != 3 {
		t.Errorf("instance idx = %d, want 3", alias.InstanceIdx)
	}
	if alias.ExportName != "my-type" {
		t.Errorf("export name = %q, want 'my-type'", alias.ExportName)
	}
}

func TestTypeIndexRef(t *testing.T) {
	ref := TypeIndexRef{Index: 42}
	if ref.Index != 42 {
		t.Errorf("index = %d, want 42", ref.Index)
	}
}

func TestResourceType(t *testing.T) {
	dtor := uint32(5)
	res := resourceType{
		Dtor: &dtor,
		Methods: []resourceMethod{
			{Name: "get", Func: FuncType{}},
			{Name: "new", Func: FuncType{}},
		},
	}
	if res.Dtor == nil || *res.Dtor != 5 {
		t.Error("dtor should be 5")
	}
	if len(res.Methods) != 2 {
		t.Errorf("methods len = %d, want 2", len(res.Methods))
	}
	if res.Methods[0].Name != "get" {
		t.Errorf("first method name = %q, want 'get'", res.Methods[0].Name)
	}
}
