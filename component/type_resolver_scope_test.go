package component

import (
	"bytes"
	"strings"
	"testing"

	"go.bytecodealliance.org/wit"
)

func typeDecl(t Type) InstanceDecl {
	return InstanceDecl{
		ExternKind: 0x01,
		DeclType:   InstanceDeclType{Type: t},
	}
}

func typeExportEq(name string, idx uint32) InstanceDecl {
	return InstanceDecl{
		Name:       name,
		ExternKind: 0x04,
		DeclType: InstanceDeclExport{Export: exportDecl{
			Name: name,
			externDesc: externDesc{
				Kind:      ExternType,
				TypeIndex: idx,
				BoundKind: 0x00,
				HasBound:  true,
			},
		}},
	}
}

func typeExportResource(name string) InstanceDecl {
	return InstanceDecl{
		Name:       name,
		ExternKind: 0x04,
		DeclType: InstanceDeclExport{Export: exportDecl{
			Name: name,
			externDesc: externDesc{
				Kind:      ExternType,
				BoundKind: 0x01,
				HasBound:  true,
			},
		}},
	}
}

func funcExport(name string, typeIdx uint32) InstanceDecl {
	return InstanceDecl{
		Name:       name,
		ExternKind: 0x04,
		DeclType: InstanceDeclExport{Export: exportDecl{
			Name:       name,
			externDesc: externDesc{Kind: ExternFunc, TypeIndex: typeIdx},
		}},
	}
}

func outerTypeAlias(outerCount, outerIndex uint32) InstanceDecl {
	buf := &bytes.Buffer{}
	buf.WriteByte(SortType)
	buf.WriteByte(0x02)
	writeLEB128(buf, outerCount)
	writeLEB128(buf, outerIndex)
	return InstanceDecl{
		ExternKind: 0x02,
		DeclType:   InstanceDeclAlias{Alias: aliasDecl{Kind: SortType, Data: buf.Bytes()}},
	}
}

func valIndex(idx uint32) *ValType {
	t := ValType(TypeIndexRef{Index: idx})
	return &t
}

func TestBuildInstanceTypeIndexSpace_SpecIndexAssignment(t *testing.T) {
	funcType := &FuncType{
		Params: []paramType{{Name: "x", Type: PrimValType{Type: PrimU32}}},
	}
	rec := RecordType{Fields: []FieldType{{Name: "y", Type: PrimValType{Type: PrimS32}}}}

	inst := &InstanceType{Decls: []InstanceDecl{
		typeExportResource("pollable"),
		typeDecl(funcType),
		funcExport("[method]pollable.block", 1),
		typeDecl(rec),
		typeExportEq("point", 2),
	}}

	space := buildInstanceTypeIndexSpace(inst, nil)
	if len(space.types) != 4 {
		t.Fatalf("type space size: got %d want 4", len(space.types))
	}
	if _, ok := space.types[0].(PrimValType); !ok {
		t.Fatalf("type 0: got %T want PrimValType", space.types[0])
	}
	if _, ok := space.types[1].(*FuncType); !ok {
		t.Fatalf("type 1: got %T want *FuncType", space.types[1])
	}
	if _, ok := space.types[2].(RecordType); !ok {
		t.Fatalf("type 2: got %T want RecordType", space.types[2])
	}
	if _, ok := space.types[3].(RecordType); !ok {
		t.Fatalf("type 3: got %T want RecordType (type export)", space.types[3])
	}
	if space.exportIndex["pollable"] != 0 {
		t.Fatalf("pollable export index: got %d want 0", space.exportIndex["pollable"])
	}
	if space.exportIndex["point"] != 3 {
		t.Fatalf("point export index: got %d want 3", space.exportIndex["point"])
	}
}

func TestBuildInstanceTypeIndexSpace_OuterAliasOccupiesIndex(t *testing.T) {
	global := []Type{
		PrimValType{Type: PrimU32},
		PrimValType{Type: PrimString},
	}
	funcType := &FuncType{
		Params: []paramType{{Name: "self", Type: TypeIndexRef{Index: 0}}},
	}
	inst := &InstanceType{Decls: []InstanceDecl{
		outerTypeAlias(1, 1),
		typeExportEq("error", 0),
		typeDecl(funcType),
		funcExport("fail", 2),
	}}

	space := buildInstanceTypeIndexSpace(inst, global)
	if len(space.types) != 3 {
		t.Fatalf("type space size: got %d want 3", len(space.types))
	}
	ref, ok := space.types[0].(globalTypeRef)
	if !ok || ref.Index != 1 {
		t.Fatalf("type 0: got %#v want globalTypeRef{1}", space.types[0])
	}
	if _, ok := space.types[2].(*FuncType); !ok {
		t.Fatalf("type 2: got %T want *FuncType", space.types[2])
	}

	reg := &CanonRegistry{resolver: NewTypeResolverWithInstances(global, nil)}
	ft, internal := reg.findFuncInInstanceType(inst, "fail")
	if ft == nil {
		t.Fatal("expected function type at spec index 2")
	}
	if len(internal) != 3 {
		t.Fatalf("internal types: got %d want 3", len(internal))
	}
}

func TestTypeResolver_TypeExportIndexNotDeclarationPosition(t *testing.T) {
	recA := RecordType{Fields: []FieldType{{Name: "x", Type: PrimValType{Type: PrimU32}}}}
	recB := RecordType{Fields: []FieldType{{Name: "y", Type: PrimValType{Type: PrimS32}}}}
	inst := &InstanceType{Decls: []InstanceDecl{
		typeDecl(recA),
		funcExport("ignored", 0),
		typeDecl(recB),
		typeExportEq("b", 1),
	}}

	r := NewTypeResolverWithInstances([]Type{inst}, []uint32{0})
	result, err := r.Resolve(typeAlias{InstanceIdx: 0, ExportName: "b"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("got %T want *wit.TypeDef", result)
	}
	rec, ok := td.Kind.(*wit.Record)
	if !ok {
		t.Fatalf("got %T want *wit.Record", td.Kind)
	}
	if len(rec.Fields) != 1 || rec.Fields[0].Name != "y" {
		t.Fatalf("resolved the declaration-position type, not spec type index 1: %+v", rec.Fields)
	}
}

func TestTypeResolver_IpSocketAddressPattern(t *testing.T) {
	ipv4 := RecordType{Fields: []FieldType{
		{Name: "port", Type: PrimValType{Type: PrimU16}},
		{Name: "address", Type: TypeIndexRef{Index: 6}},
	}}
	ipv6 := RecordType{Fields: []FieldType{
		{Name: "port", Type: PrimValType{Type: PrimU16}},
		{Name: "address", Type: TypeIndexRef{Index: 10}},
	}}
	inst := &InstanceType{Decls: []InstanceDecl{
		typeDecl(EnumType{Cases: []string{"ipv4", "ipv6"}}),
		typeExportEq("ip-address-family", 0),
		typeDecl(EnumType{Cases: []string{"unknown"}}),
		typeExportEq("error-code", 2),
		typeExportResource("network"),
		typeDecl(TupleType{Types: []ValType{
			PrimValType{Type: PrimU8}, PrimValType{Type: PrimU8},
			PrimValType{Type: PrimU8}, PrimValType{Type: PrimU8},
		}}),
		typeExportEq("ipv4-address", 5),
		typeDecl(ipv4),
		typeExportEq("ipv4-socket-address", 7),
		typeDecl(TupleType{Types: []ValType{PrimValType{Type: PrimU16}}}),
		typeExportEq("ipv6-address", 9),
		typeDecl(ipv6),
		typeExportEq("ipv6-socket-address", 11),
		typeDecl(VariantType{Cases: []CaseType{
			{Name: "ipv4", Type: valIndex(8)},
			{Name: "ipv6", Type: valIndex(12)},
		}}),
		typeExportEq("ip-socket-address", 13),
	}}

	types := make([]Type, 9)
	types[0] = inst
	types[8] = typeAlias{InstanceIdx: 0, ExportName: "ip-socket-address"}
	r := NewTypeResolverWithInstances(types, []uint32{0})

	result, err := r.Resolve(TypeIndexRef{Index: 8})
	if err != nil {
		t.Fatalf("resolve type 8: %v", err)
	}
	td, ok := result.(*wit.TypeDef)
	if !ok {
		t.Fatalf("got %T want *wit.TypeDef", result)
	}
	variant, ok := td.Kind.(*wit.Variant)
	if !ok {
		t.Fatalf("got %T want *wit.Variant", td.Kind)
	}
	if len(variant.Cases) != 2 || variant.Cases[0].Name != "ipv4" || variant.Cases[1].Name != "ipv6" {
		t.Fatalf("variant cases: %+v", variant.Cases)
	}
	ipv4TD, ok := variant.Cases[0].Type.(*wit.TypeDef)
	if !ok {
		t.Fatalf("ipv4 case: got %T", variant.Cases[0].Type)
	}
	ipv4Rec, ok := ipv4TD.Kind.(*wit.Record)
	if !ok || len(ipv4Rec.Fields) != 2 || ipv4Rec.Fields[0].Name != "port" {
		t.Fatalf("ipv4 record: %+v", ipv4TD.Kind)
	}
}

func TestTypeResolver_SubResourceTypeExport(t *testing.T) {
	inst := &InstanceType{Decls: []InstanceDecl{
		typeExportResource("error"),
	}}
	r := NewTypeResolverWithInstances([]Type{inst}, []uint32{0})
	result, err := r.Resolve(typeAlias{InstanceIdx: 0, ExportName: "error"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, ok := result.(wit.U32); !ok {
		t.Fatalf("got %T want wit.U32", result)
	}
}

func TestTypeResolver_GlobalTypeIndexCycle(t *testing.T) {
	r := NewTypeResolverWithInstances([]Type{
		TypeIndexRef{Index: 1},
		TypeIndexRef{Index: 0},
	}, nil)
	_, err := r.Resolve(TypeIndexRef{Index: 0})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestTypeResolver_InstanceTypeIndexCycle(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)
	internal := map[uint32]Type{
		0: TypeIndexRef{Index: 1},
		1: TypeIndexRef{Index: 0},
	}
	_, err := r.resolveInternalType(TypeIndexRef{Index: 0}, internal)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestTypeResolver_RecursiveRecordCycle(t *testing.T) {
	r := NewTypeResolverWithInstances(nil, nil)
	internal := map[uint32]Type{
		0: RecordType{Fields: []FieldType{{Name: "self", Type: TypeIndexRef{Index: 0}}}},
	}
	_, err := r.resolveInternalType(TypeIndexRef{Index: 0}, internal)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestTypeResolver_TypeAliasCycle(t *testing.T) {
	inst := &InstanceType{Decls: []InstanceDecl{
		typeDecl(TypeIndexRef{Index: 1}),
		typeExportEq("loop", 0),
	}}
	types := []Type{
		inst,
		typeAlias{InstanceIdx: 0, ExportName: "loop"},
	}
	r := NewTypeResolverWithInstances(types, []uint32{0})
	_, err := r.Resolve(typeAlias{InstanceIdx: 0, ExportName: "loop"})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestTypeResolver_DeeplyNestedList(t *testing.T) {
	var nested ValType = PrimValType{Type: PrimU32}
	for i := 0; i < maxTypeResolveDepth+8; i++ {
		nested = ListType{ElemType: nested}
	}
	r := NewTypeResolverWithInstances(nil, nil)
	_, err := r.Resolve(nested)
	if err == nil || !strings.Contains(err.Error(), "maximum depth") {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestTypeResolver_TypeAliasMissingExport(t *testing.T) {
	inst := &InstanceType{Decls: []InstanceDecl{
		typeDecl(PrimValType{Type: PrimU32}),
	}}
	r := NewTypeResolverWithInstances([]Type{inst}, []uint32{0})
	_, err := r.Resolve(typeAlias{InstanceIdx: 0, ExportName: "nope"})
	if err == nil {
		t.Fatal("expected missing export error")
	}
}

func TestFindFuncInInstanceType_MixedDecls(t *testing.T) {
	ft := &FuncType{
		Params: []paramType{{Name: "self", Type: TypeIndexRef{Index: 0}}},
	}
	inst := &InstanceType{Decls: []InstanceDecl{
		typeExportResource("sock"),
		outerTypeAlias(1, 0),
		typeExportEq("network", 1),
		typeDecl(ft),
		funcExport("[method]sock.start", 3),
	}}
	global := []Type{PrimValType{Type: PrimU32}}
	reg := &CanonRegistry{resolver: NewTypeResolverWithInstances(global, nil)}
	got, internal := reg.findFuncInInstanceType(inst, "[method]sock.start")
	if got != ft {
		t.Fatalf("got %#v want func type at spec index 3", got)
	}
	if len(internal) != 4 {
		t.Fatalf("internal types: got %d want 4", len(internal))
	}
}

func TestTypeResolver_MissingInstanceIndexDoesNotUseGlobalType(t *testing.T) {
	inst := &InstanceType{Decls: []InstanceDecl{
		typeDecl(VariantType{Cases: []CaseType{
			{Name: "ipv4", Type: valIndex(8)},
		}}),
		typeExportEq("ip-socket-address", 0),
	}}
	types := make([]Type, 9)
	types[0] = inst
	types[8] = typeAlias{InstanceIdx: 0, ExportName: "ip-socket-address"}
	r := NewTypeResolverWithInstances(types, []uint32{0})

	_, err := r.Resolve(TypeIndexRef{Index: 8})
	if err == nil {
		t.Fatal("expected instance type index 8 to be rejected")
	}
	if strings.Contains(err.Error(), "instance 0 export") && strings.Contains(err.Error(), "cycle") {
		t.Fatalf("instance type index 8 fell through to global type alias: %v", err)
	}
	if !strings.Contains(err.Error(), "instance type index 8") && !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected instance-space range error, got %v", err)
	}
}

func TestTypeResolver_SharedGraphExpansionBound(t *testing.T) {
	types := []Type{PrimValType{Type: PrimU32}}
	for i := 1; i < 20; i++ {
		types = append(types, TupleType{Types: []ValType{TypeIndexRef{Index: uint32(i - 1)}, TypeIndexRef{Index: uint32(i - 1)}}})
	}
	_, err := NewTypeResolverWithInstances(types, nil).Resolve(TypeIndexRef{Index: 19})
	if err == nil || !strings.Contains(err.Error(), "expanded nodes") {
		t.Fatalf("expected expansion bound, got %v", err)
	}
}

func TestTypeResolver_UnsupportedOuterScopeDoesNotUseGlobal(t *testing.T) {
	inst := &InstanceType{Decls: []InstanceDecl{outerTypeAlias(2, 0), typeExportEq("wrong-scope", 0)}}
	types := []Type{PrimValType{Type: PrimString}, inst}
	_, err := NewTypeResolverWithInstances(types, []uint32{1}).Resolve(typeAlias{InstanceIdx: 0, ExportName: "wrong-scope"})
	if err == nil {
		t.Fatal("resolved unavailable enclosing scope as global")
	}
}
