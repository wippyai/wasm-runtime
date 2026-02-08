package component

import (
	"fmt"

	"go.bytecodealliance.org/wit"
)

// TypeResolver converts component binary types to wit.Type
type TypeResolver struct {
	types         []Type
	instanceTypes []uint32 // Maps instance index to type index
}

// NewTypeResolverWithInstances creates a resolver with instance type mappings
func NewTypeResolverWithInstances(types []Type, instanceTypes []uint32) *TypeResolver {
	return &TypeResolver{types: types, instanceTypes: instanceTypes}
}

// Resolve converts a ValType to wit.Type
func (r *TypeResolver) Resolve(cvt ValType) (wit.Type, error) {
	switch t := cvt.(type) {
	case PrimValType:
		return r.resolvePrimitive(t.Type)
	case TypeIndexRef:
		return r.resolveTypeIndex(t.Index)
	case typeAlias:
		return r.resolveTypeAlias(t)
	case RecordType:
		return r.resolveRecord(t)
	case ListType:
		return r.resolveList(t)
	case TupleType:
		return r.resolveTuple(t)
	case FlagsType:
		return r.resolveFlags(t)
	case EnumType:
		return r.resolveEnum(t)
	case OptionType:
		return r.resolveOption(t)
	case ResultType:
		return r.resolveResult(t)
	case VariantType:
		return r.resolveVariant(t)
	case BorrowType:
		// Borrow handles are u32 at Canonical ABI level
		return wit.U32{}, nil
	case OwnType:
		// Own handles are u32 at Canonical ABI level
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

func (r *TypeResolver) resolveTypeIndex(idx uint32) (wit.Type, error) {
	if int(idx) >= len(r.types) {
		return nil, fmt.Errorf("type index out of range: %d >= %d", idx, len(r.types))
	}

	ct := r.types[idx]

	switch t := ct.(type) {
	case PrimValType:
		return r.resolvePrimitive(t.Type)
	case RecordType:
		return r.resolveRecord(t)
	case ListType:
		return r.resolveList(t)
	case TupleType:
		return r.resolveTuple(t)
	case FlagsType:
		return r.resolveFlags(t)
	case EnumType:
		return r.resolveEnum(t)
	case OptionType:
		return r.resolveOption(t)
	case ResultType:
		return r.resolveResult(t)
	case VariantType:
		return r.resolveVariant(t)
	case *FuncType:
		return nil, fmt.Errorf("cannot convert function type to wit.Type")
	case *InstanceType:
		// InstanceType in parameter/result position represents a resource handle
		// At Canonical ABI level, all resource handles (borrow<T>, own<T>) are u32
		return wit.U32{}, nil
	case OwnType:
		// own<T> is a resource handle - at Canonical ABI level, it's a u32
		return wit.U32{}, nil
	case BorrowType:
		// borrow<T> is a resource handle - at Canonical ABI level, it's a u32
		return wit.U32{}, nil
	case *componentTypeDecl:
		return nil, fmt.Errorf("cannot convert component type decl to wit.Type")
	case TypeIndexRef:
		return r.resolveTypeIndex(t.Index)
	case typeAlias:
		return r.resolveTypeAlias(t)
	default:
		return nil, fmt.Errorf("unsupported type at index %d: %T", idx, ct)
	}
}

func (r *TypeResolver) resolveRecord(rec RecordType) (wit.Type, error) {
	fields := make([]wit.Field, len(rec.Fields))
	for i, f := range rec.Fields {
		fieldType, err := r.Resolve(f.Type)
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

func (r *TypeResolver) resolveList(l ListType) (wit.Type, error) {
	elemType, err := r.Resolve(l.ElemType)
	if err != nil {
		return nil, fmt.Errorf("list element: %w", err)
	}

	return &wit.TypeDef{
		Kind: &wit.List{Type: elemType},
	}, nil
}

func (r *TypeResolver) resolveTuple(t TupleType) (wit.Type, error) {
	types := make([]wit.Type, len(t.Types))
	for i, elem := range t.Types {
		elemType, err := r.Resolve(elem)
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

func (r *TypeResolver) resolveOption(o OptionType) (wit.Type, error) {
	innerType, err := r.Resolve(o.Type)
	if err != nil {
		return nil, fmt.Errorf("option type: %w", err)
	}

	return &wit.TypeDef{
		Kind: &wit.Option{Type: innerType},
	}, nil
}

func (r *TypeResolver) resolveResult(res ResultType) (wit.Type, error) {
	var okType, errType wit.Type
	var err error

	if res.OK != nil {
		okType, err = r.Resolve(*res.OK)
		if err != nil {
			return nil, fmt.Errorf("result ok: %w", err)
		}
	}

	if res.Err != nil {
		errType, err = r.Resolve(*res.Err)
		if err != nil {
			return nil, fmt.Errorf("result err: %w", err)
		}
	}

	return &wit.TypeDef{
		Kind: &wit.Result{OK: okType, Err: errType},
	}, nil
}

func (r *TypeResolver) resolveVariant(v VariantType) (wit.Type, error) {
	cases := make([]wit.Case, len(v.Cases))
	for i, c := range v.Cases {
		var CaseType wit.Type
		if c.Type != nil {
			var err error
			CaseType, err = r.Resolve(*c.Type)
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

// resolveInternalType resolves a type that's internal to a InstanceType
// TypeIndexRef indices are resolved using the internalTypes map instead of global types
func (r *TypeResolver) resolveInternalType(cvt ValType, internalTypes map[uint32]Type) (wit.Type, error) {
	switch t := cvt.(type) {
	case TypeIndexRef:
		// Look up in internal types first
		if internalType, found := internalTypes[t.Index]; found {
			// If the internal type is another TypeIndexRef, try to resolve it internally first
			if ref, isRef := internalType.(TypeIndexRef); isRef {
				// Check if ref.Index points to another internal type (avoids infinite recursion)
				if ref.Index != t.Index {
					if innerType, found := internalTypes[ref.Index]; found {
						if innerValType, ok := innerType.(ValType); ok {
							return r.resolveInternalType(innerValType, internalTypes)
						}
					}
				}
				// Fallback to global resolution
				return r.resolveTypeIndex(ref.Index)
			}
			if valType, ok := internalType.(ValType); ok {
				return r.resolveInternalType(valType, internalTypes)
			}
			return nil, fmt.Errorf("internal type index %d is not a value type: %T", t.Index, internalType)
		}
		// Fallback to global resolution
		return r.resolveTypeIndex(t.Index)
	case RecordType:
		fields := make([]wit.Field, len(t.Fields))
		for i, f := range t.Fields {
			fieldType, err := r.resolveInternalType(f.Type, internalTypes)
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
		elemType, err := r.resolveInternalType(t.ElemType, internalTypes)
		if err != nil {
			return nil, fmt.Errorf("list element: %w", err)
		}
		return &wit.TypeDef{
			Kind: &wit.List{Type: elemType},
		}, nil
	case TupleType:
		types := make([]wit.Type, len(t.Types))
		for i, elem := range t.Types {
			elemType, err := r.resolveInternalType(elem, internalTypes)
			if err != nil {
				return nil, fmt.Errorf("tuple element %d: %w", i, err)
			}
			types[i] = elemType
		}
		return &wit.TypeDef{
			Kind: &wit.Tuple{Types: types},
		}, nil
	case OptionType:
		innerType, err := r.resolveInternalType(t.Type, internalTypes)
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
			okType, err = r.resolveInternalType(*t.OK, internalTypes)
			if err != nil {
				return nil, fmt.Errorf("result ok: %w", err)
			}
		}

		if t.Err != nil {
			errType, err = r.resolveInternalType(*t.Err, internalTypes)
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
				CaseType, err = r.resolveInternalType(*c.Type, internalTypes)
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
	default:
		// For other types (PrimValType, FlagsType, EnumType, typeAlias), use normal resolution
		return r.Resolve(cvt)
	}
}

// resolveTypeAlias resolves a type alias from an instance export
func (r *TypeResolver) resolveTypeAlias(alias typeAlias) (wit.Type, error) {
	// Get the instance's type index
	if int(alias.InstanceIdx) >= len(r.instanceTypes) {
		return nil, fmt.Errorf("instance index %d out of range", alias.InstanceIdx)
	}
	typeIdx := r.instanceTypes[alias.InstanceIdx]

	// Get the instance type
	if int(typeIdx) >= len(r.types) {
		return nil, fmt.Errorf("instance type index %d out of range", typeIdx)
	}

	instType, ok := r.types[typeIdx].(*InstanceType)
	if !ok {
		return nil, fmt.Errorf("type at index %d is not an instance type: %T", typeIdx, r.types[typeIdx])
	}

	// Build the internal type index space for this instance type.
	// Type indices within an instance type are assigned by their position
	// in the declaration stream. Each declaration gets an index, but only
	// type declarations (kind=0x01) define actual types.
	internalTypes := make(map[uint32]Type)
	for i, decl := range instType.Decls {
		if d, ok := decl.DeclType.(InstanceDeclType); ok {
			internalTypes[uint32(i)] = d.Type
		}
	}

	// Find the export by name and get its type
	for _, decl := range instType.Decls {
		if export, ok := decl.DeclType.(InstanceDeclExport); ok {
			if decl.Name == alias.ExportName || export.Export.Name == alias.ExportName {
				// Type exports have kind 0x03
				if export.Export.externDesc.Kind == 0x03 {
					internalIdx := export.Export.externDesc.TypeIndex
					if internalType, found := internalTypes[internalIdx]; found {
						return r.resolveInternalType(internalType.(ValType), internalTypes)
					}
					return nil, fmt.Errorf("internal type index %d not found in instance type", internalIdx)
				}
			}
		}
	}

	return nil, fmt.Errorf("type export %q not found in instance %d", alias.ExportName, alias.InstanceIdx)
}

// ResolveFunc resolves a component function type to wit types
func (r *TypeResolver) ResolveFunc(f *FuncType) (params []wit.Type, result wit.Type, err error) {
	params = make([]wit.Type, len(f.Params))
	for i, p := range f.Params {
		params[i], err = r.Resolve(p.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("param %q: %w", p.Name, err)
		}
	}

	if f.Result != nil {
		result, err = r.Resolve(*f.Result)
		if err != nil {
			return nil, nil, fmt.Errorf("result: %w", err)
		}
	}

	return params, result, nil
}

// ResolveFuncWithInternalTypes resolves a function type using instance-internal type context
func (r *TypeResolver) ResolveFuncWithInternalTypes(f *FuncType, internalTypes map[uint32]Type) (params []wit.Type, result wit.Type, err error) {
	params = make([]wit.Type, len(f.Params))
	for i, p := range f.Params {
		params[i], err = r.resolveInternalType(p.Type, internalTypes)
		if err != nil {
			return nil, nil, fmt.Errorf("param %q: %w", p.Name, err)
		}
	}

	if f.Result != nil {
		result, err = r.resolveInternalType(*f.Result, internalTypes)
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
