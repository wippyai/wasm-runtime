package wasm

// Module represents a parsed WebAssembly module
type Module struct {
	Types    []FuncType // Function types (for compatibility - populated from TypeDefs)
	TypeDefs []TypeDef  // Full type definitions including GC types
	Imports  []Import
	Funcs    []uint32 // Type indices for declared functions
	Tables   []TableType
	Memories []MemoryType
	Globals  []Global
	Exports  []Export
	Start    *uint32
	Elements []Element
	Code     []FuncBody
	Data     []DataSegment

	// DataCount holds the count from the DataCount section (ID 12).
	// Required when data indices appear in code (bulk memory operations).
	DataCount *uint32

	// Tags holds exception handling tags (ID 13).
	Tags []TagType

	CustomSections []CustomSection
}

// FuncType represents a WebAssembly function signature with parameter and result types.
type FuncType struct {
	Params  []ValType
	Results []ValType
	// Extended types for GC support - when ExtParams/ExtResults are set,
	// use them instead of Params/Results for full heap type info
	ExtParams  []ExtValType
	ExtResults []ExtValType
}

// ExtValType represents an extended value type that can include reference types
// with heap type information (for GC proposal support)
type ExtValType struct {
	Kind    byte    // ExtValKindSimple or ExtValKindRef
	ValType ValType // For simple types
	RefType RefType // For reference types (0x63, 0x64)
}

// Extended value type kinds
const (
	ExtValKindSimple byte = 0 // Simple valtype (single byte)
	ExtValKindRef    byte = 1 // Reference type with heap type
)

// FieldType represents a struct field with mutability and storage type
type FieldType struct {
	Type    StorageType
	Mutable bool
}

// StorageType represents a type that can be stored in a struct field or array.
// For value types, use PackedI8, PackedI16, or ValType cast to StorageType.
type StorageType struct {
	Kind    byte // StorageKindVal, StorageKindPacked, StorageKindRef
	ValType ValType
	Packed  byte // PackedI8, PackedI16
	RefType RefType
}

// Storage type kind constants
const (
	StorageKindVal    byte = 0
	StorageKindPacked byte = 1
	StorageKindRef    byte = 2
)

// Packed types for struct fields
const (
	PackedI8  byte = 0x78 // i8
	PackedI16 byte = 0x77 // i16
)

// RefType represents a reference type with nullable flag and heap type
type RefType struct {
	Nullable bool
	HeapType int64 // Encoded as s33: negative for abstract types, positive for type indices
}

// StructType represents a GC struct type definition
type StructType struct {
	Fields []FieldType
}

// ArrayType represents a GC array type definition
type ArrayType struct {
	Element FieldType
}

// SubType represents a subtype definition wrapping a composite type
type SubType struct {
	CompType CompType
	Parents  []uint32
	Final    bool
}

// CompType is a composite type: func, struct, or array
type CompType struct {
	Func   *FuncType
	Struct *StructType
	Array  *ArrayType
	Kind   byte
}

// Composite type kinds
const (
	CompKindFunc   byte = FuncTypeByte   // 0x60
	CompKindStruct byte = StructTypeByte // 0x5F
	CompKindArray  byte = ArrayTypeByte  // 0x5E
)

// RecType represents a recursive type group
type RecType struct {
	Types []SubType
}

// TypeDef represents any type definition in the type section
type TypeDef struct {
	Func *FuncType
	Sub  *SubType
	Rec  *RecType
	Kind byte
}

// Type definition kinds
const (
	TypeDefKindFunc byte = 0 // Shorthand function type
	TypeDefKindSub  byte = 1 // Sub/SubFinal type
	TypeDefKindRec  byte = 2 // Recursive type group
)

// ValType represents a WebAssembly value type.
// See constants.go for ValI32, ValI64, ValF32, ValF64, etc.
type ValType byte

func (v ValType) String() string {
	switch v {
	case ValI32:
		return "i32"
	case ValI64:
		return "i64"
	case ValF32:
		return "f32"
	case ValF64:
		return "f64"
	case ValV128:
		return "v128"
	case ValFuncRef:
		return "funcref"
	case ValExtern:
		return "externref"
	case ValAnyRef:
		return "anyref"
	case ValEqRef:
		return "eqref"
	case ValI31Ref:
		return "i31ref"
	case ValStructRef:
		return "structref"
	case ValArrayRef:
		return "arrayref"
	case ValNullRef:
		return "nullref"
	case ValNullExternRef:
		return "nullexternref"
	case ValNullFuncRef:
		return "nullfuncref"
	case ValRefNull:
		return "ref null"
	case ValRef:
		return "ref"
	default:
		return "unknown"
	}
}

// Import represents an imported function, table, memory, global, or tag.
type Import struct {
	Desc   ImportDesc
	Module string
	Name   string
}

// ImportDesc describes an imported item.
// Kind uses KindFunc, KindTable, KindMemory, KindGlobal, or KindTag constants.
type ImportDesc struct {
	Table   *TableType
	Memory  *MemoryType
	Global  *GlobalType
	Tag     *TagType
	TypeIdx uint32
	Kind    byte
}

// TableType describes a table with element type and size limits.
type TableType struct {
	RefElemType *RefType
	Limits      Limits
	Init        []byte
	ElemType    byte
}

// MemoryType describes a linear memory with size limits.
type MemoryType struct {
	Limits Limits
}

// Limits describes size constraints for tables and memories.
type Limits struct {
	Max      *uint64
	Min      uint64
	Shared   bool
	Memory64 bool
}

// GlobalType describes a global variable's type and mutability.
type GlobalType struct {
	ExtType *ExtValType
	ValType ValType
	Mutable bool
}

// Global represents a global variable with type and initialization.
type Global struct {
	Type GlobalType
	Init []byte // Raw init expression bytes
}

// TagType describes an exception handling tag type.
type TagType struct {
	Attribute byte   // Tag attribute (0 = exception)
	TypeIdx   uint32 // Function type index for tag signature
}

// Export describes an exported item.
// Kind uses KindFunc, KindTable, KindMemory, KindGlobal, or KindTag constants.
type Export struct {
	Name string
	Kind byte
	Idx  uint32
}

// Element represents an element segment.
// Flags determine the format:
//   - 0: active, tableIdx=0, offset expr, vec(funcidx)
//   - 1: passive, elemkind, vec(funcidx)
//   - 2: active, tableIdx, offset expr, elemkind, vec(funcidx)
//   - 3: declarative, elemkind, vec(funcidx)
//   - 4: active, tableIdx=0, offset expr, vec(expr)
//   - 5: passive, reftype, vec(expr)
//   - 6: active, tableIdx, offset expr, reftype, vec(expr)
//   - 7: declarative, reftype, vec(expr)
type Element struct {
	RefType  *RefType
	Offset   []byte
	FuncIdxs []uint32
	Exprs    [][]byte
	Flags    uint32
	TableIdx uint32
	ElemKind byte
	Type     ValType
}

// FuncBody represents a function's local declarations and bytecode.
type FuncBody struct {
	Locals []LocalEntry
	Code   []byte // Raw code bytes including end opcode
}

// LocalEntry represents a group of local variables with the same type.
type LocalEntry struct {
	ExtType *ExtValType
	Count   uint32
	ValType ValType
}

// DataSegment represents a data segment.
// Flags determine the format:
//   - 0: active, memIdx=0, offset expr, vec(byte)
//   - 1: passive, vec(byte)
//   - 2: active, memIdx, offset expr, vec(byte)
type DataSegment struct {
	Offset []byte
	Init   []byte
	Flags  uint32
	MemIdx uint32
}

// CustomSection holds a named custom section's data.
type CustomSection struct {
	Name string
	Data []byte
}

// NumImportedFuncs returns the number of imported functions
func (m *Module) NumImportedFuncs() int {
	count := 0
	for _, imp := range m.Imports {
		if imp.Desc.Kind == KindFunc {
			count++
		}
	}
	return count
}

// NumImportedGlobals returns the number of imported globals
func (m *Module) NumImportedGlobals() int {
	count := 0
	for _, imp := range m.Imports {
		if imp.Desc.Kind == KindGlobal {
			count++
		}
	}
	return count
}

// NumImportedTables returns the number of imported tables
func (m *Module) NumImportedTables() int {
	count := 0
	for _, imp := range m.Imports {
		if imp.Desc.Kind == KindTable {
			count++
		}
	}
	return count
}

// NumImportedMemories returns the number of imported memories
func (m *Module) NumImportedMemories() int {
	count := 0
	for _, imp := range m.Imports {
		if imp.Desc.Kind == KindMemory {
			count++
		}
	}
	return count
}

// NumImportedTags returns the number of imported tags
func (m *Module) NumImportedTags() int {
	count := 0
	for _, imp := range m.Imports {
		if imp.Desc.Kind == KindTag {
			count++
		}
	}
	return count
}

// NumTypes returns the number of types in the flat type index space.
// For GC modules with recursive types, this expands rec groups.
func (m *Module) NumTypes() int {
	if len(m.TypeDefs) > 0 {
		count := 0
		for i := range m.TypeDefs {
			switch m.TypeDefs[i].Kind {
			case TypeDefKindFunc, TypeDefKindSub:
				count++
			case TypeDefKindRec:
				count += len(m.TypeDefs[i].Rec.Types)
			}
		}
		return count
	}
	return len(m.Types)
}

// GetFuncType returns the type of a function by its index
func (m *Module) GetFuncType(funcIdx uint32) *FuncType {
	numImported := uint32(m.NumImportedFuncs())
	if funcIdx < numImported {
		for i, imp := range m.Imports {
			if imp.Desc.Kind == KindFunc {
				if funcIdx == 0 {
					return m.getFuncTypeByIdx(m.Imports[i].Desc.TypeIdx)
				}
				funcIdx--
			}
		}
	}
	localIdx := funcIdx - numImported
	if int(localIdx) >= len(m.Funcs) {
		return nil
	}
	return m.getFuncTypeByIdx(m.Funcs[localIdx])
}

// getFuncTypeByIdx returns the function type at the given type index.
// For GC modules with TypeDefs, it extracts the func type from the typedef.
// Note: rec groups expand into multiple type indices in the flat space.
func (m *Module) getFuncTypeByIdx(typeIdx uint32) *FuncType {
	// GC modules: look up in TypeDefs with flat index expansion
	if len(m.TypeDefs) > 0 {
		flatIdx := uint32(0)
		for i := range m.TypeDefs {
			td := &m.TypeDefs[i]
			switch td.Kind {
			case TypeDefKindFunc:
				if flatIdx == typeIdx {
					return td.Func
				}
				flatIdx++
			case TypeDefKindSub:
				if flatIdx == typeIdx {
					if td.Sub.CompType.Kind == CompKindFunc {
						return td.Sub.CompType.Func
					}
					return nil
				}
				flatIdx++
			case TypeDefKindRec:
				for j := range td.Rec.Types {
					if flatIdx == typeIdx {
						if td.Rec.Types[j].CompType.Kind == CompKindFunc {
							return td.Rec.Types[j].CompType.Func
						}
						return nil
					}
					flatIdx++
				}
			}
		}
		return nil
	}

	// Simple module: direct lookup
	if int(typeIdx) >= len(m.Types) {
		return nil
	}
	return &m.Types[typeIdx]
}

// AddType adds a function type and returns its index, reusing existing if equal
func (m *Module) AddType(ft FuncType) uint32 {
	for i, t := range m.Types {
		if typesEqual(t, ft) {
			return uint32(i)
		}
	}
	idx := uint32(len(m.Types))
	m.Types = append(m.Types, ft)
	return idx
}

func typesEqual(a, b FuncType) bool {
	// Compare extended types if present (GC support)
	if len(a.ExtParams) > 0 || len(b.ExtParams) > 0 {
		if len(a.ExtParams) != len(b.ExtParams) || len(a.ExtResults) != len(b.ExtResults) {
			return false
		}
		for i := range a.ExtParams {
			if !extValTypesEqual(a.ExtParams[i], b.ExtParams[i]) {
				return false
			}
		}
		for i := range a.ExtResults {
			if !extValTypesEqual(a.ExtResults[i], b.ExtResults[i]) {
				return false
			}
		}
		return true
	}

	// Simple type comparison
	if len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] {
			return false
		}
	}
	for i := range a.Results {
		if a.Results[i] != b.Results[i] {
			return false
		}
	}
	return true
}

func extValTypesEqual(a, b ExtValType) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == ExtValKindRef {
		return a.RefType.Nullable == b.RefType.Nullable && a.RefType.HeapType == b.RefType.HeapType
	}
	return a.ValType == b.ValType
}
