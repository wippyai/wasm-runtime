package arena

// PrimType represents a primitive value type
type PrimType byte

const (
	PrimBool   PrimType = 0x7f
	PrimS8     PrimType = 0x7e
	PrimU8     PrimType = 0x7d
	PrimS16    PrimType = 0x7c
	PrimU16    PrimType = 0x7b
	PrimS32    PrimType = 0x7a
	PrimU32    PrimType = 0x79
	PrimS64    PrimType = 0x78
	PrimU64    PrimType = 0x77
	PrimF32    PrimType = 0x76
	PrimF64    PrimType = 0x75
	PrimChar   PrimType = 0x74
	PrimString PrimType = 0x73
)

// Component Model Type System
//
// This implements a proper type arena with unique IDs, matching the semantics
// of the reference wasmparser implementation.
//
// Key concepts:
// - Each type gets a unique ID when stored in the arena
// - Type indices in binary are resolved to IDs during parsing
// - Instance types store exports as name -> resolved entity type
// - Scopes track their own index -> ID mappings

// TypeID is a unique identifier for a type in the arena
type TypeID uint32

// TypeKind identifies the kind of a component type
type TypeKind uint8

const (
	TypeKindDefined   TypeKind = iota // Record, variant, list, etc.
	TypeKindFunc                      // Component function type
	TypeKindInstance                  // Instance type
	TypeKindComponent                 // Component type
	TypeKindResource                  // Resource type
)

// AnyTypeID represents any component type by its kind and ID
type AnyTypeID struct {
	Kind TypeKind
	ID   TypeID
}

// ValType represents a component value type (primitive or defined type reference)
type ValType struct {
	Primitive PrimType // Non-zero if primitive
	TypeID    TypeID   // ID if not primitive (TypeKindDefined)
}

// IsPrimitive returns true if this is a primitive type
func (v ValType) IsPrimitive() bool {
	return v.Primitive != 0
}

// EntityKind identifies the kind of a component entity
type EntityKind uint8

const (
	EntityKindModule    EntityKind = iota // Core module
	EntityKindComponent                   // Component
	EntityKindInstance                    // Instance
	EntityKindFunc                        // Function
	EntityKindValue                       // Value
	EntityKindType                        // Type
)

// EntityType represents the type of an exported/imported entity
type EntityType struct {
	Kind EntityKind
	// For Type entities, this is the type ID
	// For Func entities, this is the function type ID
	// For Instance entities, this is the instance type ID
	// etc.
	ID TypeID
}

// DefinedType represents a component defined type (record, variant, list, etc.)
type DefinedType struct {
	Data interface{}
	Kind DefinedTypeKind
}

// DefinedTypeKind identifies the kind of defined type
type DefinedTypeKind uint8

const (
	DefinedKindPrimitive DefinedTypeKind = iota
	DefinedKindRecord
	DefinedKindVariant
	DefinedKindList
	DefinedKindTuple
	DefinedKindFlags
	DefinedKindEnum
	DefinedKindOption
	DefinedKindResult
	DefinedKindOwn
	DefinedKindBorrow
)

// RecordData contains record type data
type RecordData struct {
	Fields []FieldData
}

// FieldData represents a record field
type FieldData struct {
	Name string
	Type ValType
}

// VariantData contains variant type data
type VariantData struct {
	Cases []CaseData
}

// CaseData represents a variant case
type CaseData struct {
	Type    *ValType
	Refines *uint32
	Name    string
}

// TupleData contains tuple type data
type TupleData struct {
	Types []ValType
}

// ResultData contains result type data
type ResultData struct {
	OK  *ValType
	Err *ValType
}

// FuncTypeData represents a component function type
type FuncTypeData struct {
	Result *ValType
	Params []ParamData
}

// ParamData represents a function parameter
type ParamData struct {
	Name string
	Type ValType
}

// InstanceTypeData represents an instance type
type InstanceTypeData struct {
	// Exports maps export name to entity type with resolved IDs
	Exports map[string]EntityType
}

// TypeData represents a component type
type TypeData struct {
	Imports map[string]EntityType
	Exports map[string]EntityType
}

// TypeArena stores all types with unique IDs
type TypeArena struct {
	// Type storage by kind
	defined    []DefinedType
	funcs      []FuncTypeData
	instances  []InstanceTypeData
	components []TypeData

	// Resource ID counter
	nextResourceID TypeID
}

// NewTypeArena creates a new type arena
func NewTypeArena() *TypeArena {
	return &TypeArena{}
}

// AllocDefined allocates a new defined type and returns its ID
func (a *TypeArena) AllocDefined(t DefinedType) TypeID {
	id := TypeID(len(a.defined))
	a.defined = append(a.defined, t)
	return id
}

// AllocFunc allocates a new function type and returns its ID
func (a *TypeArena) AllocFunc(t FuncTypeData) TypeID {
	id := TypeID(len(a.funcs))
	a.funcs = append(a.funcs, t)
	return id
}

// AllocInstance allocates a new instance type and returns its ID
func (a *TypeArena) AllocInstance(t InstanceTypeData) TypeID {
	id := TypeID(len(a.instances))
	a.instances = append(a.instances, t)
	return id
}

// AllocComponent allocates a new component type and returns its ID
func (a *TypeArena) AllocComponent(t TypeData) TypeID {
	id := TypeID(len(a.components))
	a.components = append(a.components, t)
	return id
}

// AllocResource allocates a new resource type and returns its ID
func (a *TypeArena) AllocResource() TypeID {
	id := a.nextResourceID
	a.nextResourceID++
	return id
}

// GetDefined returns a defined type by ID
func (a *TypeArena) GetDefined(id TypeID) *DefinedType {
	if int(id) >= len(a.defined) {
		return nil
	}
	return &a.defined[id]
}

// GetFunc returns a function type by ID
func (a *TypeArena) GetFunc(id TypeID) *FuncTypeData {
	if int(id) >= len(a.funcs) {
		return nil
	}
	return &a.funcs[id]
}

// GetInstance returns an instance type by ID
func (a *TypeArena) GetInstance(id TypeID) *InstanceTypeData {
	if int(id) >= len(a.instances) {
		return nil
	}
	return &a.instances[id]
}

// GetComponent returns a component type by ID
func (a *TypeArena) GetComponent(id TypeID) *TypeData {
	if int(id) >= len(a.components) {
		return nil
	}
	return &a.components[id]
}

// ResolveValType converts a ValType to wit.Type
func (a *TypeArena) ResolveValType(vt ValType) (interface{}, error) {
	if vt.IsPrimitive() {
		return a.resolvePrimitive(vt.Primitive), nil
	}
	return a.resolveDefinedType(vt.TypeID)
}

func (a *TypeArena) resolvePrimitive(p PrimType) interface{} {
	switch p {
	case PrimBool:
		return ResolvedBool{}
	case PrimS8:
		return ResolvedS8{}
	case PrimU8:
		return ResolvedU8{}
	case PrimS16:
		return ResolvedS16{}
	case PrimU16:
		return ResolvedU16{}
	case PrimS32:
		return ResolvedS32{}
	case PrimU32:
		return ResolvedU32{}
	case PrimS64:
		return ResolvedS64{}
	case PrimU64:
		return ResolvedU64{}
	case PrimF32:
		return ResolvedF32{}
	case PrimF64:
		return ResolvedF64{}
	case PrimChar:
		return ResolvedChar{}
	case PrimString:
		return ResolvedString{}
	default:
		return ResolvedU32{}
	}
}

func (a *TypeArena) resolveDefinedType(id TypeID) (interface{}, error) {
	def := a.GetDefined(id)
	if def == nil {
		return ResolvedU32{}, nil // fallback for resources
	}

	switch def.Kind {
	case DefinedKindPrimitive:
		if p, ok := def.Data.(PrimType); ok {
			return a.resolvePrimitive(p), nil
		}
		return ResolvedU32{}, nil

	case DefinedKindRecord:
		data := def.Data.(RecordData)
		fields := make([]Field, len(data.Fields))
		for i, f := range data.Fields {
			ft, err := a.ResolveValType(f.Type)
			if err != nil {
				return nil, err
			}
			fields[i] = Field{Name: f.Name, Type: ft}
		}
		return Record{Fields: fields}, nil

	case DefinedKindList:
		elem := def.Data.(ValType)
		et, err := a.ResolveValType(elem)
		if err != nil {
			return nil, err
		}
		return List{Elem: et}, nil

	case DefinedKindTuple:
		data := def.Data.(TupleData)
		types := make([]interface{}, len(data.Types))
		for i, t := range data.Types {
			tt, err := a.ResolveValType(t)
			if err != nil {
				return nil, err
			}
			types[i] = tt
		}
		return Tuple{Types: types}, nil

	case DefinedKindOption:
		inner := def.Data.(ValType)
		it, err := a.ResolveValType(inner)
		if err != nil {
			return nil, err
		}
		return Option{Type: it}, nil

	case DefinedKindResult:
		data := def.Data.(ResultData)
		var okType, errType interface{}
		if data.OK != nil {
			var err error
			okType, err = a.ResolveValType(*data.OK)
			if err != nil {
				return nil, err
			}
		}
		if data.Err != nil {
			var err error
			errType, err = a.ResolveValType(*data.Err)
			if err != nil {
				return nil, err
			}
		}
		return Result{OK: okType, Err: errType}, nil

	case DefinedKindFlags:
		names := def.Data.([]string)
		return Flags{Count: len(names)}, nil

	case DefinedKindEnum:
		names := def.Data.([]string)
		return Enum{Count: len(names)}, nil

	case DefinedKindVariant:
		data := def.Data.(VariantData)
		cases := make([]Case, len(data.Cases))
		for i, c := range data.Cases {
			var caseType interface{}
			if c.Type != nil {
				var err error
				caseType, err = a.ResolveValType(*c.Type)
				if err != nil {
					return nil, err
				}
			}
			cases[i] = Case{Name: c.Name, Type: caseType}
		}
		return Variant{Cases: cases}, nil

	case DefinedKindOwn, DefinedKindBorrow:
		// Resource handles are always i32
		return ResolvedU32{}, nil

	default:
		return ResolvedU32{}, nil
	}
}

// Exported types for type resolution (used by flatten.go)
type ResolvedBool struct{}
type ResolvedS8 struct{}
type ResolvedU8 struct{}
type ResolvedS16 struct{}
type ResolvedU16 struct{}
type ResolvedS32 struct{}
type ResolvedU32 struct{}
type ResolvedS64 struct{}
type ResolvedU64 struct{}
type ResolvedF32 struct{}
type ResolvedF64 struct{}
type ResolvedChar struct{}
type ResolvedString struct{}

type Field struct {
	Type interface{}
	Name string
}

type Record struct {
	Fields []Field
}

type List struct {
	Elem interface{}
}

type Tuple struct {
	Types []interface{}
}

type Option struct {
	Type interface{}
}

type Result struct {
	OK  interface{}
	Err interface{}
}

type Flags struct {
	Count int
}

type Enum struct {
	Count int
}

type Case struct {
	Type interface{}
	Name string
}

type Variant struct {
	Cases []Case
}
