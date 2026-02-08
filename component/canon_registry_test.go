package component

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestCanonRegistry_FindLift(t *testing.T) {
	reg := &CanonRegistry{
		Lifts: map[string]*LiftDef{
			"foo": {Name: "foo", CoreFuncIdx: 1},
			"bar": {Name: "bar", CoreFuncIdx: 2},
		},
	}

	if lift := reg.FindLift("foo"); lift == nil || lift.Name != "foo" {
		t.Errorf("expected foo lift, got %v", lift)
	}
	if lift := reg.FindLift("bar"); lift == nil || lift.Name != "bar" {
		t.Errorf("expected bar lift, got %v", lift)
	}
	if lift := reg.FindLift("notexist"); lift != nil {
		t.Errorf("expected nil for nonexistent lift, got %v", lift)
	}
}

func TestCanonRegistry_FindLower(t *testing.T) {
	reg := &CanonRegistry{
		Lowers: map[string]*LowerDef{
			"foo": {Name: "foo", FuncIdx: 1},
			"bar": {Name: "bar", FuncIdx: 2},
		},
	}

	if lower := reg.FindLower("foo"); lower == nil || lower.Name != "foo" {
		t.Errorf("expected foo lower, got %v", lower)
	}
	if lower := reg.FindLower("bar"); lower == nil || lower.Name != "bar" {
		t.Errorf("expected bar lower, got %v", lower)
	}
	if lower := reg.FindLower("notexist"); lower != nil {
		t.Errorf("expected nil for nonexistent lower, got %v", lower)
	}
}

func TestCanonRegistry_AllLifts(t *testing.T) {
	reg := &CanonRegistry{
		Lifts: map[string]*LiftDef{
			"a": {Name: "a"},
			"b": {Name: "b"},
			"c": {Name: "c"},
		},
	}

	lifts := reg.AllLifts()
	if len(lifts) != 3 {
		t.Errorf("expected 3 lifts, got %d", len(lifts))
	}

	names := make(map[string]bool)
	for _, l := range lifts {
		names[l.Name] = true
	}
	for _, name := range []string{"a", "b", "c"} {
		if !names[name] {
			t.Errorf("missing lift %q", name)
		}
	}
}

func TestCanonRegistry_AllLowers(t *testing.T) {
	reg := &CanonRegistry{
		Lowers: map[string]*LowerDef{
			"x": {Name: "x"},
			"y": {Name: "y"},
		},
	}

	lowers := reg.AllLowers()
	if len(lowers) != 2 {
		t.Errorf("expected 2 lowers, got %d", len(lowers))
	}

	names := make(map[string]bool)
	for _, l := range lowers {
		names[l.Name] = true
	}
	for _, name := range []string{"x", "y"} {
		if !names[name] {
			t.Errorf("missing lower %q", name)
		}
	}
}

func TestCanonRegistry_AllLiftsEmpty(t *testing.T) {
	reg := &CanonRegistry{
		Lifts: map[string]*LiftDef{},
	}
	lifts := reg.AllLifts()
	if len(lifts) != 0 {
		t.Errorf("expected 0 lifts, got %d", len(lifts))
	}
}

func TestCanonRegistry_AllLowersEmpty(t *testing.T) {
	reg := &CanonRegistry{
		Lowers: map[string]*LowerDef{},
	}
	lowers := reg.AllLowers()
	if len(lowers) != 0 {
		t.Errorf("expected 0 lowers, got %d", len(lowers))
	}
}

func TestCanonRegistry_findExportNameByFuncIdx(t *testing.T) {
	comp := &Component{
		Exports: []Export{
			{Name: "func-a", Sort: 0x01, SortIndex: 0},
			{Name: "instance-b", Sort: 0x02, SortIndex: 0},
			{Name: "func-c", Sort: 0x01, SortIndex: 1},
		},
	}

	reg := &CanonRegistry{}

	if name := reg.findExportNameByFuncIdx(comp, 0); name != "func-a" {
		t.Errorf("expected func-a, got %q", name)
	}
	if name := reg.findExportNameByFuncIdx(comp, 1); name != "func-c" {
		t.Errorf("expected func-c, got %q", name)
	}
	// Non-existent index returns fallback
	if name := reg.findExportNameByFuncIdx(comp, 99); name != "func_99" {
		t.Errorf("expected func_99, got %q", name)
	}
}

func TestCanonRegistry_findImportName(t *testing.T) {
	comp := &Component{
		Imports: []Import{
			{Name: "wasi:io/streams@0.2.0", ExternKind: ExternInstance},
			{Name: "wasi:random/random@0.2.0", ExternKind: ExternInstance},
		},
		FuncIndexSpace: []FuncIndexEntry{
			{InstanceIdx: 0, ExportName: "read"},
			{InstanceIdx: 1, ExportName: "get-random-bytes"},
			{InstanceIdx: 99, ExportName: "missing"},
		},
	}

	reg := &CanonRegistry{}

	if name := reg.findImportName(comp, 0); name != "wasi:io/streams@0.2.0#read" {
		t.Errorf("expected wasi:io/streams@0.2.0#read, got %q", name)
	}
	if name := reg.findImportName(comp, 1); name != "wasi:random/random@0.2.0#get-random-bytes" {
		t.Errorf("expected wasi:random/random@0.2.0#get-random-bytes, got %q", name)
	}
	// Out of range instance idx returns empty
	if name := reg.findImportName(comp, 2); name != "" {
		t.Errorf("expected empty, got %q", name)
	}
	// Out of range func idx returns empty
	if name := reg.findImportName(comp, 999); name != "" {
		t.Errorf("expected empty, got %q", name)
	}
}

func TestCanonRegistry_findFuncInInstanceType(t *testing.T) {
	// Create instance type with a function export
	funcType := &FuncType{
		Params: []paramType{
			{Name: "x", Type: PrimValType{Type: PrimU32}},
		},
		Result: nil,
	}

	instType := &InstanceType{
		Decls: []InstanceDecl{
			{
				Name:     "",
				DeclType: InstanceDeclType{Type: funcType},
			},
			{
				Name: "my-func",
				DeclType: InstanceDeclExport{
					Export: exportDecl{
						Name: "my-func",
						externDesc: externDesc{
							Kind:      0x01, // Func
							TypeIndex: 0,
						},
					},
				},
			},
		},
	}

	reg := &CanonRegistry{}
	ft, internalTypes := reg.findFuncInInstanceType(instType, "my-func")

	if ft == nil {
		t.Fatal("expected non-nil function type")
	}
	if len(ft.Params) != 1 {
		t.Errorf("expected 1 param, got %d", len(ft.Params))
	}
	if len(internalTypes) != 1 {
		t.Errorf("expected 1 internal type, got %d", len(internalTypes))
	}
}

func TestCanonRegistry_findFuncInInstanceType_NotFound(t *testing.T) {
	instType := &InstanceType{
		Decls: []InstanceDecl{},
	}

	reg := &CanonRegistry{}
	ft, _ := reg.findFuncInInstanceType(instType, "nonexistent")

	if ft != nil {
		t.Error("expected nil for nonexistent export")
	}
}

func TestCanonRegistry_findFuncInInstanceType_TypeExport(t *testing.T) {
	// Test that type exports add to type index space
	funcType := &FuncType{}

	instType := &InstanceType{
		Decls: []InstanceDecl{
			{
				DeclType: InstanceDeclType{Type: PrimValType{Type: PrimU32}},
			},
			{
				Name: "my-type",
				DeclType: InstanceDeclExport{
					Export: exportDecl{
						Name: "my-type",
						externDesc: externDesc{
							Kind:      0x03, // Type
							TypeIndex: 0,
						},
					},
				},
			},
			{
				DeclType: InstanceDeclType{Type: funcType},
			},
			{
				Name: "my-func",
				DeclType: InstanceDeclExport{
					Export: exportDecl{
						Name: "my-func",
						externDesc: externDesc{
							Kind:      0x01, // Func
							TypeIndex: 2,    // Points to type at index 2 (the func after type export)
						},
					},
				},
			},
		},
	}

	reg := &CanonRegistry{}
	ft, internalTypes := reg.findFuncInInstanceType(instType, "my-func")

	// Type decl (idx 0) + type export (idx 1) + func type (idx 2) = 3 types
	if len(internalTypes) != 3 {
		t.Errorf("expected 3 internal types, got %d", len(internalTypes))
	}
	if ft == nil {
		t.Error("expected non-nil function type")
	}
}

func TestCanonRegistry_NewCanonRegistry_EmptyComponent(t *testing.T) {
	types := []Type{
		&FuncType{},
	}
	resolver := NewTypeResolverWithInstances(types, nil)

	comp := &Component{
		SectionOrder: []SectionMarker{},
		Canons:       []Canon{},
	}

	reg, err := NewCanonRegistry(comp, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(reg.Lifts) != 0 {
		t.Errorf("expected 0 lifts, got %d", len(reg.Lifts))
	}
	if len(reg.Lowers) != 0 {
		t.Errorf("expected 0 lowers, got %d", len(reg.Lowers))
	}
}

func TestCanonRegistry_NewCanonRegistry_WithLift(t *testing.T) {
	funcType := &FuncType{
		Params: []paramType{
			{Name: "a", Type: PrimValType{Type: PrimU32}},
		},
		Result: nil,
	}

	types := []Type{funcType}
	resolver := NewTypeResolverWithInstances(types, nil)

	comp := &Component{
		SectionOrder: []SectionMarker{
			{Kind: SectionCanon, StartIndex: 0, Count: 1},
			{Kind: SectionExport, StartIndex: 0, Count: 1},
		},
		Canons: []Canon{
			{
				Parsed: &CanonDef{
					Kind:      CanonLift,
					FuncIndex: 0,
					TypeIndex: 0,
					Options:   []CanonOption{},
				},
			},
		},
		Exports: []Export{
			{Name: "my-export", Sort: 0x01, SortIndex: 0},
		},
	}

	reg, err := NewCanonRegistry(comp, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Lifts) != 1 {
		t.Errorf("expected 1 lift, got %d", len(reg.Lifts))
	}

	lift := reg.FindLift("my-export")
	if lift == nil {
		t.Fatal("expected to find my-export lift")
	}
	if len(lift.Params) != 1 {
		t.Errorf("expected 1 param, got %d", len(lift.Params))
	}
	if _, ok := lift.Params[0].(wit.U32); !ok {
		t.Errorf("expected wit.U32 param, got %T", lift.Params[0])
	}
}

func TestCanonRegistry_NewCanonRegistry_WithLower(t *testing.T) {
	funcType := &FuncType{
		Params: []paramType{
			{Name: "b", Type: PrimValType{Type: PrimString}},
		},
		Result: nil,
	}

	instType := &InstanceType{
		Decls: []InstanceDecl{
			{DeclType: InstanceDeclType{Type: funcType}},
			{
				Name: "do-thing",
				DeclType: InstanceDeclExport{
					Export: exportDecl{
						Name:       "do-thing",
						externDesc: externDesc{Kind: 0x01, TypeIndex: 0},
					},
				},
			},
		},
	}

	types := []Type{funcType, instType}
	resolver := NewTypeResolverWithInstances(types, nil)

	comp := &Component{
		SectionOrder: []SectionMarker{
			{Kind: SectionCanon, StartIndex: 0, Count: 1},
		},
		Canons: []Canon{
			{
				Parsed: &CanonDef{
					Kind:      CanonLower,
					FuncIndex: 0,
					TypeIndex: 0,
					Options:   []CanonOption{},
				},
			},
		},
		Imports: []Import{
			{Name: "test:pkg/iface@1.0.0", ExternKind: ExternInstance},
		},
		FuncIndexSpace: []FuncIndexEntry{
			{InstanceIdx: 0, ExportName: "do-thing"},
		},
		InstanceTypes:  []uint32{1},
		TypeIndexSpace: types,
	}

	reg, err := NewCanonRegistry(comp, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Lowers) != 1 {
		t.Errorf("expected 1 lower, got %d", len(reg.Lowers))
	}

	lower := reg.FindLower("test:pkg/iface@1.0.0#do-thing")
	if lower == nil {
		t.Fatal("expected to find lower")
	}
}

func TestCanonRegistry_NewCanonRegistry_SkipsNilParsed(t *testing.T) {
	types := []Type{&FuncType{}}
	resolver := NewTypeResolverWithInstances(types, nil)

	comp := &Component{
		SectionOrder: []SectionMarker{
			{Kind: SectionCanon, StartIndex: 0, Count: 2},
		},
		Canons: []Canon{
			{Parsed: nil}, // Should be skipped
			{Parsed: nil}, // Should be skipped
		},
	}

	reg, err := NewCanonRegistry(comp, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Lifts) != 0 {
		t.Errorf("expected 0 lifts, got %d", len(reg.Lifts))
	}
}

func TestCanonRegistry_NewCanonRegistry_WithAliases(t *testing.T) {
	funcType := &FuncType{}
	types := []Type{funcType}
	resolver := NewTypeResolverWithInstances(types, nil)

	comp := &Component{
		SectionOrder: []SectionMarker{
			{Kind: SectionAlias, StartIndex: 0, Count: 2},
			{Kind: SectionCanon, StartIndex: 0, Count: 1},
			{Kind: SectionExport, StartIndex: 0, Count: 1},
		},
		Aliases: []Alias{
			{Parsed: &ParsedAlias{Sort: 0x01}}, // Func alias, adds to func space
			{Parsed: &ParsedAlias{Sort: 0x00}}, // Non-func alias, doesn't add
		},
		Canons: []Canon{
			{Parsed: &CanonDef{Kind: CanonLift, TypeIndex: 0}},
		},
		Exports: []Export{
			{Name: "after-alias", Sort: 0x01, SortIndex: 1},
		},
	}

	reg, err := NewCanonRegistry(comp, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Lift should be at index 1 (after the func alias)
	lift := reg.FindLift("after-alias")
	if lift == nil {
		t.Fatal("expected to find lift")
	}
}

func TestCanonRegistry_processLower_ResourceMethods(t *testing.T) {
	instType := &InstanceType{
		Decls: []InstanceDecl{},
	}

	types := []Type{instType}
	resolver := NewTypeResolverWithInstances(types, nil)

	comp := &Component{
		SectionOrder: []SectionMarker{
			{Kind: SectionCanon, StartIndex: 0, Count: 1},
		},
		Canons: []Canon{
			{Parsed: &CanonDef{Kind: CanonLower, FuncIndex: 0}},
		},
		Imports: []Import{
			{Name: "test:pkg/iface@1.0.0", ExternKind: ExternInstance},
		},
		FuncIndexSpace: []FuncIndexEntry{
			{InstanceIdx: 0, ExportName: "[method]resource.ready"},
		},
		InstanceTypes:  []uint32{0},
		TypeIndexSpace: types,
	}

	reg, err := NewCanonRegistry(comp, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lower := reg.FindLower("test:pkg/iface@1.0.0#[method]resource.ready")
	if lower == nil {
		t.Fatal("expected to find lower")
	}
	// Resource method ending in .ready should get bool result
	if len(lower.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(lower.Results))
	}
}

func TestCanonRegistry_LiftDef_Fields(t *testing.T) {
	lift := &LiftDef{
		Name:        "test-lift",
		CoreFuncIdx: 5,
		TypeIdx:     3,
		Params:      []wit.Type{wit.U32{}, wit.String{}},
		Results:     []wit.Type{wit.Bool{}},
		ParamNames:  []string{"a", "b"},
		MemoryIdx:   0,
		ReallocIdx:  1,
	}

	if lift.Name != "test-lift" {
		t.Errorf("wrong name: %s", lift.Name)
	}
	if lift.CoreFuncIdx != 5 {
		t.Errorf("wrong CoreFuncIdx: %d", lift.CoreFuncIdx)
	}
	if len(lift.Params) != 2 {
		t.Errorf("wrong param count: %d", len(lift.Params))
	}
	if len(lift.Results) != 1 {
		t.Errorf("wrong result count: %d", len(lift.Results))
	}
}

func TestCanonRegistry_LowerDef_Fields(t *testing.T) {
	lower := &LowerDef{
		Name:       "test-lower",
		FuncIdx:    7,
		Params:     []wit.Type{wit.S64{}},
		Results:    []wit.Type{wit.F64{}},
		ParamNames: []string{"x"},
		MemoryIdx:  0,
		ReallocIdx: 2,
	}

	if lower.Name != "test-lower" {
		t.Errorf("wrong name: %s", lower.Name)
	}
	if lower.FuncIdx != 7 {
		t.Errorf("wrong FuncIdx: %d", lower.FuncIdx)
	}
	if len(lower.Params) != 1 {
		t.Errorf("wrong param count: %d", len(lower.Params))
	}
}
