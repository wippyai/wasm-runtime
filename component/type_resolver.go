package component

import (
	"fmt"

	"go.bytecodealliance.org/wit"
)

// maxTypeResolveDepth bounds recursive type resolution for cyclic or deeply nested input.
const maxTypeResolveDepth = 128

// Bound expansion of shallow shared type graphs as well as recursive depth.
const maxTypeResolveNodes = 65536

// TypeResolver converts component binary types to wit.Type
type TypeResolver struct {
	types         []Type
	instanceTypes []uint32 // Maps instance index to type index
}

// globalTypeRef is an instance-type-space entry introduced by an outer type alias.
// Nested type indices inside the referenced type belong to the enclosing component type space.
type globalTypeRef struct {
	Index uint32
}

func (globalTypeRef) isValType() {}
func (globalTypeRef) isType()    {}

type aliasKey struct {
	name     string
	instance uint32
}

type resolveFrame struct {
	global map[uint32]struct{}
	alias  map[aliasKey]struct{}
	depth  int
	nodes  int
}

func newResolveFrame() *resolveFrame {
	return &resolveFrame{
		global: make(map[uint32]struct{}),
		alias:  make(map[aliasKey]struct{}),
	}
}

func (f *resolveFrame) enter() error {
	if f.nodes >= maxTypeResolveNodes {
		return fmt.Errorf("type resolution exceeds maximum expanded nodes %d", maxTypeResolveNodes)
	}
	f.nodes++
	if f.depth >= maxTypeResolveDepth {
		return fmt.Errorf("type resolution exceeds maximum depth %d", maxTypeResolveDepth)
	}
	f.depth++
	return nil
}

func (f *resolveFrame) leave() {
	f.depth--
}

// NewTypeResolverWithInstances creates a resolver with instance type mappings
func NewTypeResolverWithInstances(types []Type, instanceTypes []uint32) *TypeResolver {
	return &TypeResolver{types: types, instanceTypes: instanceTypes}
}

// Resolve converts a ValType to wit.Type
func (r *TypeResolver) Resolve(cvt ValType) (wit.Type, error) {
	return r.resolve(cvt, newResolveFrame())
}

func (r *TypeResolver) resolve(cvt ValType, frame *resolveFrame) (wit.Type, error) {
	if err := frame.enter(); err != nil {
		return nil, err
	}
	defer frame.leave()

	switch t := cvt.(type) {
	case PrimValType:
		return r.resolvePrimitive(t.Type)
	case TypeIndexRef:
		return r.resolveTypeIndex(t.Index, frame)
	case typeAlias:
		return r.resolveTypeAlias(t, frame)
	case globalTypeRef:
		return r.resolveTypeIndex(t.Index, frame)
	case RecordType:
		return r.resolveRecord(t, frame)
	case ListType:
		return r.resolveList(t, frame)
	case TupleType:
		return r.resolveTuple(t, frame)
	case FlagsType:
		return r.resolveFlags(t)
	case EnumType:
		return r.resolveEnum(t)
	case OptionType:
		return r.resolveOption(t, frame)
	case ResultType:
		return r.resolveResult(t, frame)
	case VariantType:
		return r.resolveVariant(t, frame)
	case BorrowType:
		return wit.U32{}, nil
	case OwnType:
		return wit.U32{}, nil
	default:
		return nil, fmt.Errorf("unsupported component val type: %T", cvt)
	}
}

func (r *TypeResolver) resolvePrimitive(p PrimType) (wit.Type, error) {
	switch p {
	case PrimBool:
		return wit.Bool{}, nil
	case PrimS8:
		return wit.S8{}, nil
	case PrimU8:
		return wit.U8{}, nil
	case PrimS16:
		return wit.S16{}, nil
	case PrimU16:
		return wit.U16{}, nil
	case PrimS32:
		return wit.S32{}, nil
	case PrimU32:
		return wit.U32{}, nil
	case PrimS64:
		return wit.S64{}, nil
	case PrimU64:
		return wit.U64{}, nil
	case PrimF32:
		return wit.F32{}, nil
	case PrimF64:
		return wit.F64{}, nil
	case PrimChar:
		return wit.Char{}, nil
	case PrimString:
		return wit.String{}, nil
	default:
		return nil, fmt.Errorf("unknown primitive type: 0x%02x", p)
	}
}

func (r *TypeResolver) resolveTypeIndex(idx uint32, frame *resolveFrame) (wit.Type, error) {
	if err := frame.enter(); err != nil {
		return nil, err
	}
	defer frame.leave()

	if _, cycling := frame.global[idx]; cycling {
		return nil, fmt.Errorf("type resolution cycle at type index %d", idx)
	}
	if int(idx) >= len(r.types) {
		return nil, fmt.Errorf("type index out of range: %d >= %d", idx, len(r.types))
	}

	frame.global[idx] = struct{}{}
	defer delete(frame.global, idx)

	ct := r.types[idx]

	switch t := ct.(type) {
	case PrimValType:
		return r.resolvePrimitive(t.Type)
	case RecordType:
		return r.resolveRecord(t, frame)
	case ListType:
		return r.resolveList(t, frame)
	case TupleType:
		return r.resolveTuple(t, frame)
	case FlagsType:
		return r.resolveFlags(t)
	case EnumType:
		return r.resolveEnum(t)
	case OptionType:
		return r.resolveOption(t, frame)
	case ResultType:
		return r.resolveResult(t, frame)
	case VariantType:
		return r.resolveVariant(t, frame)
	case *FuncType:
		return nil, fmt.Errorf("cannot convert function type to wit.Type")
	case *InstanceType:
		return wit.U32{}, nil
	case OwnType:
		return wit.U32{}, nil
	case BorrowType:
		return wit.U32{}, nil
	case *componentTypeDecl:
		return nil, fmt.Errorf("cannot convert component type decl to wit.Type")
	case TypeIndexRef:
		return r.resolveTypeIndex(t.Index, frame)
	case typeAlias:
		return r.resolveTypeAlias(t, frame)
	case globalTypeRef:
		return r.resolveTypeIndex(t.Index, frame)
	default:
		return nil, fmt.Errorf("unsupported type at index %d: %T", idx, ct)
	}
}

func (r *TypeResolver) resolveRecord(rec RecordType, frame *resolveFrame) (wit.Type, error) {
	fields := make([]wit.Field, len(rec.Fields))
	for i, f := range rec.Fields {
		fieldType, err := r.resolve(f.Type, frame)
		if err != nil {
			return nil, fmt.Errorf("record field %q: %w", f.Name, err)
		}
		fields[i] = wit.Field{
			Name: f.Name,
			Type: fieldType,
		}
	}

	return &wit.TypeDef{
		Kind: &wit.Record{Fields: fields},
	}, nil
}

func (r *TypeResolver) resolveList(l ListType, frame *resolveFrame) (wit.Type, error) {
	elemType, err := r.resolve(l.ElemType, frame)
	if err != nil {
		return nil, fmt.Errorf("list element: %w", err)
	}

	return &wit.TypeDef{
		Kind: &wit.List{Type: elemType},
	}, nil
}

func (r *TypeResolver) resolveTuple(t TupleType, frame *resolveFrame) (wit.Type, error) {
	types := make([]wit.Type, len(t.Types))
	for i, elem := range t.Types {
		elemType, err := r.resolve(elem, frame)
		if err != nil {
			return nil, fmt.Errorf("tuple element %d: %w", i, err)
		}
		types[i] = elemType
	}

	return &wit.TypeDef{
		Kind: &wit.Tuple{Types: types},
	}, nil
}

func (r *TypeResolver) resolveFlags(f FlagsType) (wit.Type, error) {
	flags := make([]wit.Flag, len(f.Names))
	for i, name := range f.Names {
		flags[i] = wit.Flag{Name: name}
	}

	return &wit.TypeDef{
		Kind: &wit.Flags{Flags: flags},
	}, nil
}

func (r *TypeResolver) resolveEnum(e EnumType) (wit.Type, error) {
	cases := make([]wit.EnumCase, len(e.Cases))
	for i, name := range e.Cases {
		cases[i] = wit.EnumCase{Name: name}
	}

	return &wit.TypeDef{
		Kind: &wit.Enum{Cases: cases},
	}, nil
}

func (r *TypeResolver) resolveOption(o OptionType, frame *resolveFrame) (wit.Type, error) {
	innerType, err := r.resolve(o.Type, frame)
	if err != nil {
		return nil, fmt.Errorf("option type: %w", err)
	}

	return &wit.TypeDef{
		Kind: &wit.Option{Type: innerType},
	}, nil
}

func (r *TypeResolver) resolveResult(res ResultType, frame *resolveFrame) (wit.Type, error) {
	var okType, errType wit.Type
	var err error

	if res.OK != nil {
		okType, err = r.resolve(*res.OK, frame)
		if err != nil {
			return nil, fmt.Errorf("result ok: %w", err)
		}
	}

	if res.Err != nil {
		errType, err = r.resolve(*res.Err, frame)
		if err != nil {
			return nil, fmt.Errorf("result err: %w", err)
		}
	}

	return &wit.TypeDef{
		Kind: &wit.Result{OK: okType, Err: errType},
	}, nil
}

func (r *TypeResolver) resolveVariant(v VariantType, frame *resolveFrame) (wit.Type, error) {
	cases := make([]wit.Case, len(v.Cases))
	for i, c := range v.Cases {
		var CaseType wit.Type
		if c.Type != nil {
			var err error
			CaseType, err = r.resolve(*c.Type, frame)
			if err != nil {
				return nil, fmt.Errorf("variant case %q: %w", c.Name, err)
			}
		}
		cases[i] = wit.Case{
			Name: c.Name,
			Type: CaseType,
		}
	}

	return &wit.TypeDef{
		Kind: &wit.Variant{Cases: cases},
	}, nil
}

// resolveInternalType resolves a type in an instance type's component type index space.
// TypeIndexRef indices address that space.
func (r *TypeResolver) resolveInternalType(cvt ValType, internalTypes map[uint32]Type) (wit.Type, error) {
	return r.resolveInternalTypeFrame(cvt, internalTypes, newResolveFrame(), make(map[uint32]struct{}))
}

func (r *TypeResolver) resolveInternalTypeFrame(cvt ValType, internalTypes map[uint32]Type, frame *resolveFrame, inFlight map[uint32]struct{}) (wit.Type, error) {
	if err := frame.enter(); err != nil {
		return nil, err
	}
	defer frame.leave()

	switch t := cvt.(type) {
	case TypeIndexRef:
		if _, cycling := inFlight[t.Index]; cycling {
			return nil, fmt.Errorf("type resolution cycle at instance type index %d", t.Index)
		}
		internalType, found := internalTypes[t.Index]
		if !found || internalType == nil {
			return nil, fmt.Errorf("instance type index %d out of range", t.Index)
		}
		inFlight[t.Index] = struct{}{}
		defer delete(inFlight, t.Index)
		return r.resolveStoredInternalType(internalType, internalTypes, frame, inFlight)
	case globalTypeRef:
		return r.resolveTypeIndex(t.Index, frame)
	case typeAlias:
		return r.resolveTypeAlias(t, frame)
	case RecordType:
		fields := make([]wit.Field, len(t.Fields))
		for i, f := range t.Fields {
			fieldType, err := r.resolveInternalTypeFrame(f.Type, internalTypes, frame, inFlight)
			if err != nil {
				return nil, fmt.Errorf("record field %q: %w", f.Name, err)
			}
			fields[i] = wit.Field{
				Name: f.Name,
				Type: fieldType,
			}
		}
		return &wit.TypeDef{
			Kind: &wit.Record{Fields: fields},
		}, nil
	case ListType:
		elemType, err := r.resolveInternalTypeFrame(t.ElemType, internalTypes, frame, inFlight)
		if err != nil {
			return nil, fmt.Errorf("list element: %w", err)
		}
		return &wit.TypeDef{
			Kind: &wit.List{Type: elemType},
		}, nil
	case TupleType:
		types := make([]wit.Type, len(t.Types))
		for i, elem := range t.Types {
			elemType, err := r.resolveInternalTypeFrame(elem, internalTypes, frame, inFlight)
			if err != nil {
				return nil, fmt.Errorf("tuple element %d: %w", i, err)
			}
			types[i] = elemType
		}
		return &wit.TypeDef{
			Kind: &wit.Tuple{Types: types},
		}, nil
	case OptionType:
		innerType, err := r.resolveInternalTypeFrame(t.Type, internalTypes, frame, inFlight)
		if err != nil {
			return nil, fmt.Errorf("option inner: %w", err)
		}
		return &wit.TypeDef{
			Kind: &wit.Option{Type: innerType},
		}, nil
	case ResultType:
		var okType, errType wit.Type
		var err error

		if t.OK != nil {
			okType, err = r.resolveInternalTypeFrame(*t.OK, internalTypes, frame, inFlight)
			if err != nil {
				return nil, fmt.Errorf("result ok: %w", err)
			}
		}

		if t.Err != nil {
			errType, err = r.resolveInternalTypeFrame(*t.Err, internalTypes, frame, inFlight)
			if err != nil {
				return nil, fmt.Errorf("result err: %w", err)
			}
		}

		return &wit.TypeDef{
			Kind: &wit.Result{OK: okType, Err: errType},
		}, nil
	case VariantType:
		cases := make([]wit.Case, len(t.Cases))
		for i, c := range t.Cases {
			var CaseType wit.Type
			if c.Type != nil {
				var err error
				CaseType, err = r.resolveInternalTypeFrame(*c.Type, internalTypes, frame, inFlight)
				if err != nil {
					return nil, fmt.Errorf("variant case %q: %w", c.Name, err)
				}
			}
			cases[i] = wit.Case{
				Name: c.Name,
				Type: CaseType,
			}
		}
		return &wit.TypeDef{
			Kind: &wit.Variant{Cases: cases},
		}, nil
	case PrimValType:
		return r.resolvePrimitive(t.Type)
	case FlagsType:
		return r.resolveFlags(t)
	case EnumType:
		return r.resolveEnum(t)
	case BorrowType, OwnType:
		return wit.U32{}, nil
	default:
		return nil, fmt.Errorf("unsupported instance val type: %T", cvt)
	}
}

func (r *TypeResolver) resolveStoredInternalType(internalType Type, internalTypes map[uint32]Type, frame *resolveFrame, inFlight map[uint32]struct{}) (wit.Type, error) {
	switch t := internalType.(type) {
	case ValType:
		return r.resolveInternalTypeFrame(t, internalTypes, frame, inFlight)
	case *InstanceType:
		return wit.U32{}, nil
	case *FuncType:
		return nil, fmt.Errorf("cannot convert function type to wit.Type")
	case *componentTypeDecl:
		return nil, fmt.Errorf("cannot convert component type decl to wit.Type")
	default:
		return nil, fmt.Errorf("internal type is not a value type: %T", internalType)
	}
}

// resolveTypeAlias resolves a type alias from an instance export
func (r *TypeResolver) resolveTypeAlias(alias typeAlias, frame *resolveFrame) (wit.Type, error) {
	if err := frame.enter(); err != nil {
		return nil, err
	}
	defer frame.leave()

	key := aliasKey{instance: alias.InstanceIdx, name: alias.ExportName}
	if _, cycling := frame.alias[key]; cycling {
		return nil, fmt.Errorf("type resolution cycle at instance %d export %q", alias.InstanceIdx, alias.ExportName)
	}
	frame.alias[key] = struct{}{}
	defer delete(frame.alias, key)

	if int(alias.InstanceIdx) >= len(r.instanceTypes) {
		return nil, fmt.Errorf("instance index %d out of range", alias.InstanceIdx)
	}
	typeIdx := r.instanceTypes[alias.InstanceIdx]

	if int(typeIdx) >= len(r.types) {
		return nil, fmt.Errorf("instance type index %d out of range", typeIdx)
	}

	instType, ok := r.types[typeIdx].(*InstanceType)
	if !ok {
		return nil, fmt.Errorf("type at index %d is not an instance type: %T", typeIdx, r.types[typeIdx])
	}

	space := buildInstanceTypeIndexSpace(instType, r.types)
	internalIdx, found := space.exportIndex[alias.ExportName]
	if !found {
		return nil, fmt.Errorf("type export %q not found in instance %d", alias.ExportName, alias.InstanceIdx)
	}
	internalType, found := space.types[internalIdx]
	if !found || internalType == nil {
		return nil, fmt.Errorf("internal type index %d not found in instance type", internalIdx)
	}
	return r.resolveStoredInternalType(internalType, space.types, frame, make(map[uint32]struct{}))
}

// instanceTypeSpace is the component type index space of an instance type.
type instanceTypeSpace struct {
	types       map[uint32]Type
	exportIndex map[string]uint32
}

// buildInstanceTypeIndexSpace assigns type indices in declaration order to
// type declarations (0x01), type aliases (0x02, sort type), and type exports
// (export externtype type).
func buildInstanceTypeIndexSpace(instType *InstanceType, globalTypes []Type) instanceTypeSpace {
	space := instanceTypeSpace{
		types:       make(map[uint32]Type),
		exportIndex: make(map[string]uint32),
	}
	if instType == nil {
		return space
	}

	var typeIdx uint32
	for _, decl := range instType.Decls {
		switch d := decl.DeclType.(type) {
		case InstanceDeclType:
			space.types[typeIdx] = d.Type
			typeIdx++

		case InstanceDeclAlias:
			if d.Alias.Kind != SortType {
				continue
			}
			space.types[typeIdx] = typeFromInstanceAlias(d.Alias, space.types, globalTypes)
			typeIdx++

		case InstanceDeclExport:
			if d.Export.externDesc.Kind != ExternType {
				continue
			}
			name := d.Export.Name
			if name == "" {
				name = decl.Name
			}
			space.types[typeIdx] = typeFromTypeExport(d.Export, space.types)
			space.exportIndex[name] = typeIdx
			typeIdx++
		}
	}
	return space
}

func typeFromInstanceAlias(alias aliasDecl, space map[uint32]Type, globalTypes []Type) Type {
	parsed, err := parseSingleAlias(alias.Kind, alias.Data)
	if err != nil {
		return nil
	}
	switch parsed.TargetKind {
	case 0x00:
		return typeAlias{InstanceIdx: parsed.Instance, ExportName: parsed.Name}
	case 0x02:
		if parsed.OuterCount == 0 {
			if t, ok := space[parsed.OuterIndex]; ok {
				return t
			}
			return nil
		}
		if parsed.OuterCount != 1 || int(parsed.OuterIndex) >= len(globalTypes) {
			return nil
		}
		return globalTypeRef{Index: parsed.OuterIndex}
	default:
		return nil
	}
}

func typeFromTypeExport(export exportDecl, space map[uint32]Type) Type {
	if export.externDesc.BoundKind == 0x01 {
		return PrimValType{Type: PrimU32}
	}
	if t, ok := space[export.externDesc.TypeIndex]; ok {
		return t
	}
	return nil
}

// ResolveFunc resolves a component function type to wit types
func (r *TypeResolver) ResolveFunc(f *FuncType) (params []wit.Type, result wit.Type, err error) {
	frame := newResolveFrame()
	params = make([]wit.Type, len(f.Params))
	for i, p := range f.Params {
		params[i], err = r.resolve(p.Type, frame)
		if err != nil {
			return nil, nil, fmt.Errorf("param %q: %w", p.Name, err)
		}
	}

	if f.Result != nil {
		result, err = r.resolve(*f.Result, frame)
		if err != nil {
			return nil, nil, fmt.Errorf("result: %w", err)
		}
	}

	return params, result, nil
}

// ResolveFuncWithInternalTypes resolves a function type using instance-internal type context
func (r *TypeResolver) ResolveFuncWithInternalTypes(f *FuncType, internalTypes map[uint32]Type) (params []wit.Type, result wit.Type, err error) {
	frame := newResolveFrame()
	inFlight := make(map[uint32]struct{})
	params = make([]wit.Type, len(f.Params))
	for i, p := range f.Params {
		params[i], err = r.resolveInternalTypeFrame(p.Type, internalTypes, frame, inFlight)
		if err != nil {
			return nil, nil, fmt.Errorf("param %q: %w", p.Name, err)
		}
	}

	if f.Result != nil {
		result, err = r.resolveInternalTypeFrame(*f.Result, internalTypes, frame, inFlight)
		if err != nil {
			return nil, nil, fmt.Errorf("result: %w", err)
		}
	}

	return params, result, nil
}

// ResolveFuncType finds and resolves a function type by index
func (r *TypeResolver) ResolveFuncType(typeIdx uint32) (*FuncType, error) {
	if int(typeIdx) >= len(r.types) {
		return nil, fmt.Errorf("type index out of range: %d >= %d", typeIdx, len(r.types))
	}

	ft, ok := r.types[typeIdx].(*FuncType)
	if !ok {
		return nil, fmt.Errorf("type at index %d is not a function type: %T", typeIdx, r.types[typeIdx])
	}

	return ft, nil
}
