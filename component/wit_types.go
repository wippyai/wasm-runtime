package component

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// WIT Type System - Binary format parsing
// Based on Component Model spec: https://github.com/WebAssembly/component-model/blob/main/design/mvp/Binary.md

// Type represents the top-level type structure
type Type interface {
	isType()
}

// DefType represents a type definition
type DefType interface {
	isDefType()
}

// PrimType represents primitive types
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

// RecordType represents a record (struct)
type RecordType struct {
	Fields []FieldType
}

func (RecordType) isDefType() {}
func (RecordType) isValType() {}
func (RecordType) isType()    {}

// FieldType represents a field in a record type
type FieldType struct {
	Type ValType
	Name string
}

// VariantType represents a variant (tagged union)
type VariantType struct {
	Cases []CaseType
}

func (VariantType) isDefType() {}
func (VariantType) isValType() {}
func (VariantType) isType()    {}

// CaseType represents a case in a variant type
type CaseType struct {
	Type    *ValType
	Refines *uint32
	Name    string
}

// ListType represents a list
type ListType struct {
	ElemType ValType
}

func (ListType) isDefType() {}
func (ListType) isValType() {}
func (ListType) isType()    {}

// TupleType represents a tuple
type TupleType struct {
	Types []ValType
}

func (TupleType) isDefType() {}
func (TupleType) isValType() {}
func (TupleType) isType()    {}

// FlagsType represents flags (bitfield)
type FlagsType struct {
	Names []string
}

func (FlagsType) isDefType() {}
func (FlagsType) isValType() {}
func (FlagsType) isType()    {}

// EnumType represents an enum
type EnumType struct {
	Cases []string
}

func (EnumType) isDefType() {}
func (EnumType) isValType() {}
func (EnumType) isType()    {}

// OptionType represents an optional value
type OptionType struct {
	Type ValType
}

func (OptionType) isDefType() {}
func (OptionType) isValType() {}
func (OptionType) isType()    {}

// ResultType represents a result (Ok/Err)
type ResultType struct {
	OK  *ValType
	Err *ValType
}

func (ResultType) isDefType() {}
func (ResultType) isValType() {}
func (ResultType) isType()    {}

// OwnType represents an owned resource handle
type OwnType struct {
	TypeIndex uint32
}

func (OwnType) isDefType() {}
func (OwnType) isValType() {}
func (OwnType) isType()    {}

// BorrowType represents a borrowed resource handle
type BorrowType struct {
	TypeIndex uint32
}

func (BorrowType) isDefType() {}
func (BorrowType) isValType() {}
func (BorrowType) isType()    {}

// ValType represents a value type
type ValType interface {
	isValType()
}

// PrimValType represents primitive value types
type PrimValType struct {
	Type PrimType
}

func (PrimValType) isValType() {}
func (PrimValType) isType()    {}

// TypeIndexRef represents a reference to a type by index
type TypeIndexRef struct {
	Index uint32
}

func (TypeIndexRef) isValType() {}
func (TypeIndexRef) isType()    {}

// typeAlias represents an alias to a type exported from an instance.
// This allows deferred resolution - we store the instance and export name,
// and resolve the actual type later when the full type context is available.
type typeAlias struct {
	ExportName  string
	InstanceIdx uint32
}

func (typeAlias) isValType() {}
func (typeAlias) isType()    {}

// FuncType represents a function type
// Per spec: functype ::= 0x40 ps:<paramlist> rs:<resultlist>
// resultlist can be either 0x00+type (single result) or 0x01 0x00 (no result)
type FuncType struct {
	Result *ValType
	Params []paramType
}

func (FuncType) isType() {}

type paramType struct {
	Type ValType
	Name string
}

// InstanceType represents an instance type
type InstanceType struct {
	Decls []InstanceDecl
}

func (InstanceType) isType() {}

// componentTypeDecl represents a component type (0x41)
type componentTypeDecl struct {
	Decls []componentDecl
}

func (componentTypeDecl) isType() {}

type componentDecl struct {
	Import       *importDecl
	instanceDecl *instanceDeclFull
	Kind         byte
}

type importDecl struct {
	Name       string
	externDesc externDesc
}

type externDesc struct {
	TypeIndex uint32
	Kind      byte
	BoundKind byte
	HasBound  bool
}

// InstanceDecl represents a declaration within an instance type
type InstanceDecl struct {
	DeclType   instanceDeclType
	Name       string
	ExternKind byte
}

type instanceDeclFull struct {
	Type     Type
	CoreType *[]byte
	Alias    *aliasDecl
	Export   *exportDecl
	Kind     byte
}

type aliasDecl struct {
	Data []byte
	Kind byte
}

type exportDecl struct {
	Name       string
	externDesc externDesc
}

type instanceDeclType interface {
	isInstanceDeclType()
}

// InstanceDeclCoreType represents a core type declaration
type InstanceDeclCoreType struct {
	Type []byte // Raw core type data
}

func (InstanceDeclCoreType) isInstanceDeclType() {}

// InstanceDeclType represents a component type declaration
type InstanceDeclType struct {
	Type Type
}

func (InstanceDeclType) isInstanceDeclType() {}

// InstanceDeclAlias represents an alias declaration
type InstanceDeclAlias struct {
	Alias aliasDecl
}

func (InstanceDeclAlias) isInstanceDeclType() {}

// InstanceDeclExport represents an export declaration
type InstanceDeclExport struct {
	Export exportDecl
}

func (InstanceDeclExport) isInstanceDeclType() {}

// resourceType represents a resource type
type resourceType struct {
	Rep     *ValType // representation type, nil for abstract
	Dtor    *uint32  // destructor function index
	Methods []resourceMethod
}

func (resourceType) isDefType() {}

type resourceMethod struct {
	Name string
	Func FuncType
}

// TypeSection represents a parsed Type section (Section 7)
type TypeSection struct {
	Types []Type
}

// ParseTypeSection parses a Type section (section 7) from binary data
func ParseTypeSection(data []byte) (*TypeSection, error) {
	r := getReader(data)
	defer putReader(r)

	count, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read type count: %w", err)
	}

	if count > 10000 {
		return nil, fmt.Errorf("type count %d exceeds maximum", count)
	}

	section := &TypeSection{
		Types: make([]Type, 0, count),
	}

	for i := uint32(0); i < count; i++ {
		typ, err := parseType(r)
		if err != nil {
			return nil, fmt.Errorf("type %d: %w", i, err)
		}
		section.Types = append(section.Types, typ)
	}

	return section, nil
}

func parseType(r io.Reader) (Type, error) {
	var typeByte byte
	if err := binary.Read(r, binary.LittleEndian, &typeByte); err != nil {
		return nil, fmt.Errorf("read type byte: %w", err)
	}

	switch typeByte {
	case 0x40:
		return parseFuncType(r)
	case 0x41:
		return parseTypeDecl(r)
	case 0x42:
		return parseInstanceType(r)
	default:
		r = io.MultiReader(bytes.NewReader([]byte{typeByte}), r)
		return parseDefTypeAsType(r)
	}
}

func parseDefTypeAsType(r io.Reader) (Type, error) {
	var typeByte byte
	if err := binary.Read(r, binary.LittleEndian, &typeByte); err != nil {
		return nil, fmt.Errorf("read type byte: %w", err)
	}

	switch typeByte {
	case 0x72:
		rec, err := parseRecordType(r)
		if err != nil {
			return nil, err
		}
		return rec, nil
	case 0x71:
		v, err := parseVariantType(r)
		if err != nil {
			return nil, err
		}
		return v, nil
	case 0x70:
		elemType, err := parseValType(r)
		if err != nil {
			return nil, err
		}
		return ListType{ElemType: elemType}, nil
	case 0x6f:
		t, err := parseTupleType(r)
		if err != nil {
			return nil, fmt.Errorf("parse tuple: %w", err)
		}
		return t, nil
	case 0x6e:
		f, err := parseFlagsType(r)
		if err != nil {
			return nil, err
		}
		return f, nil
	case 0x6d:
		e, err := parseEnumType(r)
		if err != nil {
			return nil, err
		}
		return e, nil
	case 0x6b: // option
		elemType, err := parseValType(r)
		if err != nil {
			return nil, err
		}
		return OptionType{Type: elemType}, nil
	case 0x6a: // result
		res, err := parseResultType(r)
		if err != nil {
			return nil, err
		}
		return res, nil
	case 0x69: // own
		idx, err := readLEB128(r)
		if err != nil {
			return nil, err
		}
		return OwnType{TypeIndex: idx}, nil
	case 0x68: // borrow
		idx, err := readLEB128(r)
		if err != nil {
			return nil, err
		}
		return BorrowType{TypeIndex: idx}, nil
	default:
		if typeByte >= 0x73 && typeByte <= 0x7f {
			return PrimValType{Type: PrimType(typeByte)}, nil
		}
		if typeByte < 0x68 {
			return TypeIndexRef{Index: uint32(typeByte)}, nil
		}
		return nil, fmt.Errorf("unknown deftype byte: 0x%02x", typeByte)
	}
}

// parseFuncType parses a component function type
//
// Binary format per spec: 0x40 + paramlist + resultlist
//
// paramlist ::= vec(<labelvaltype>)
//
//	where labelvaltype ::= label' + valtype
//
// resultlist encoding (this is NOT a vector):
//
//	0x00 + valtype  => single result of given type
//	0x01 0x00       => no result (void function)
//
// The resultlist encoding is NOT the same as paramlist. It does NOT use vec() encoding.
// This was a major source of confusion - the spec uses a discriminated union, not a count+items.
// Unlike parameters which have names, results are anonymous and limited to 0 or 1.
//
// Reference: github.com/WebAssembly/component-model/blob/main/design/mvp/Binary.md
func parseFuncType(r io.Reader) (*FuncType, error) {
	paramCount, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read param count: %w", err)
	}

	if paramCount > 1000 {
		return nil, fmt.Errorf("param count %d exceeds maximum", paramCount)
	}

	params := make([]paramType, 0, paramCount)
	for i := uint32(0); i < paramCount; i++ {
		name, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("param %d name: %w", i, err)
		}

		valType, err := parseValType(r)
		if err != nil {
			return nil, fmt.Errorf("param %d type: %w", i, err)
		}

		params = append(params, paramType{
			Name: name,
			Type: valType,
		})
	}

	// resultlist: discriminated union encoding (NOT a vector)
	var resultByte byte
	if err := binary.Read(r, binary.LittleEndian, &resultByte); err != nil {
		return nil, fmt.Errorf("read result discriminant: %w", err)
	}

	var result *ValType
	switch resultByte {
	case 0x00:
		// Has single result
		valType, err := parseValType(r)
		if err != nil {
			return nil, fmt.Errorf("read result type: %w", err)
		}
		result = &valType
	case 0x01:
		// No result, followed by 0x00 end marker
		var secondByte byte
		if err := binary.Read(r, binary.LittleEndian, &secondByte); err != nil {
			return nil, fmt.Errorf("read result end marker: %w", err)
		}
		if secondByte != 0x00 {
			return nil, fmt.Errorf("expected 0x00 after 0x01 in resultlist, got 0x%02x", secondByte)
		}
		result = nil
	default:
		return nil, fmt.Errorf("unknown resultlist discriminant: 0x%02x", resultByte)
	}

	return &FuncType{
		Params: params,
		Result: result,
	}, nil
}

func parseInstanceType(r io.Reader) (*InstanceType, error) {
	count, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read instance decl count: %w", err)
	}

	if count > 10000 {
		return nil, fmt.Errorf("instance decl count %d exceeds maximum", count)
	}

	decls := make([]InstanceDecl, 0, count)
	for i := uint32(0); i < count; i++ {
		decl, err := parseInstanceDecl(r)
		if err != nil {
			return nil, fmt.Errorf("instance decl %d: %w", i, err)
		}
		decls = append(decls, decl)
	}

	return &InstanceType{
		Decls: decls,
	}, nil
}

func parseInstanceDecl(r io.Reader) (InstanceDecl, error) {
	var kind byte
	if err := binary.Read(r, binary.LittleEndian, &kind); err != nil {
		return InstanceDecl{}, fmt.Errorf("read kind: %w", err)
	}

	switch kind {
	case 0x00:
		data, err := readCoreType(r)
		if err != nil {
			return InstanceDecl{}, fmt.Errorf("read core type: %w", err)
		}
		return InstanceDecl{
			DeclType:   InstanceDeclCoreType{Type: data},
			ExternKind: kind,
		}, nil

	case 0x01:
		// type decl: read the component type (can be func, instance, component, or value type)
		typ, err := parseType(r)
		if err != nil {
			return InstanceDecl{}, fmt.Errorf("read type: %w", err)
		}
		return InstanceDecl{
			DeclType:   InstanceDeclType{Type: typ},
			ExternKind: kind,
		}, nil

	case 0x02:
		// alias decl
		alias, err := readAlias(r)
		if err != nil {
			return InstanceDecl{}, fmt.Errorf("read alias: %w", err)
		}
		return InstanceDecl{
			DeclType:   InstanceDeclAlias{Alias: alias},
			ExternKind: kind,
		}, nil

	case 0x04:
		// export decl
		export, err := parseExportDecl(r)
		if err != nil {
			return InstanceDecl{}, fmt.Errorf("read export: %w", err)
		}
		return InstanceDecl{
			Name:       export.Name,
			DeclType:   InstanceDeclExport{Export: export},
			ExternKind: kind,
		}, nil

	default:
		return InstanceDecl{}, fmt.Errorf("unknown instance decl kind: 0x%02x", kind)
	}
}

func parseImportDecl(r io.Reader) (importDecl, error) {
	var nameKind byte
	if err := binary.Read(r, binary.LittleEndian, &nameKind); err != nil {
		return importDecl{}, fmt.Errorf("read name kind: %w", err)
	}

	nameLen, err := readLEB128(r)
	if err != nil {
		return importDecl{}, fmt.Errorf("read name length: %w", err)
	}

	if nameLen > 10000 {
		return importDecl{}, fmt.Errorf("name length %d exceeds maximum", nameLen)
	}

	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		return importDecl{}, fmt.Errorf("read name: %w", err)
	}

	var externKind byte
	if err := binary.Read(r, binary.LittleEndian, &externKind); err != nil {
		return importDecl{}, fmt.Errorf("read extern kind: %w", err)
	}

	if externKind == ExternCoreModule {
		var extraByte byte
		if err := binary.Read(r, binary.LittleEndian, &extraByte); err != nil {
			return importDecl{}, fmt.Errorf("read core module extra byte: %w", err)
		}
	}

	typeIndex, err := readLEB128(r)
	if err != nil {
		return importDecl{}, fmt.Errorf("read type index: %w", err)
	}

	return importDecl{
		Name: string(nameBytes),
		externDesc: externDesc{
			Kind:      externKind,
			TypeIndex: typeIndex,
		},
	}, nil
}

func parseInstanceDeclFull(r io.Reader) (instanceDeclFull, error) {
	var kind byte
	if err := binary.Read(r, binary.LittleEndian, &kind); err != nil {
		return instanceDeclFull{}, fmt.Errorf("read instance decl kind: %w", err)
	}

	switch kind {
	case 0x00:
		data, err := readCoreType(r)
		if err != nil {
			return instanceDeclFull{}, fmt.Errorf("read core type: %w", err)
		}
		return instanceDeclFull{Kind: kind, CoreType: &data}, nil

	case 0x01:
		typ, err := parseType(r)
		if err != nil {
			return instanceDeclFull{}, fmt.Errorf("read type: %w", err)
		}
		return instanceDeclFull{Kind: kind, Type: typ}, nil

	case 0x02:
		alias, err := readAlias(r)
		if err != nil {
			return instanceDeclFull{}, fmt.Errorf("read alias: %w", err)
		}
		return instanceDeclFull{Kind: kind, Alias: &alias}, nil

	case 0x04:
		export, err := parseExportDecl(r)
		if err != nil {
			return instanceDeclFull{}, fmt.Errorf("read export: %w", err)
		}
		return instanceDeclFull{Kind: kind, Export: &export}, nil

	default:
		return instanceDeclFull{}, fmt.Errorf("unknown instance decl kind: 0x%02x", kind)
	}
}

func parseExportDecl(r io.Reader) (exportDecl, error) {
	var nameKind byte
	if err := binary.Read(r, binary.LittleEndian, &nameKind); err != nil {
		return exportDecl{}, fmt.Errorf("read name kind: %w", err)
	}

	nameLen, err := readLEB128(r)
	if err != nil {
		return exportDecl{}, fmt.Errorf("read name length: %w", err)
	}

	if nameLen > 10000 {
		return exportDecl{}, fmt.Errorf("name length %d exceeds maximum", nameLen)
	}

	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		return exportDecl{}, fmt.Errorf("read name: %w", err)
	}

	var externKind byte
	if err := binary.Read(r, binary.LittleEndian, &externKind); err != nil {
		return exportDecl{}, fmt.Errorf("read extern kind: %w", err)
	}

	var typeIndex uint32

	switch externKind {
	case 0x00:
		var extraByte byte
		if err := binary.Read(r, binary.LittleEndian, &extraByte); err != nil {
			return exportDecl{}, fmt.Errorf("read core module extra byte: %w", err)
		}
		typeIndex, err = readLEB128(r)
		if err != nil {
			return exportDecl{}, fmt.Errorf("read type index: %w", err)
		}

	case 0x01, 0x04, 0x05:
		typeIndex, err = readLEB128(r)
		if err != nil {
			return exportDecl{}, fmt.Errorf("read type index: %w", err)
		}

	case 0x02:
		var boundKind byte
		if err := binary.Read(r, binary.LittleEndian, &boundKind); err != nil {
			return exportDecl{}, fmt.Errorf("read value bound kind: %w", err)
		}
		switch boundKind {
		case 0x00:
			typeIndex, err = readLEB128(r)
			if err != nil {
				return exportDecl{}, fmt.Errorf("read value index: %w", err)
			}
		case 0x01:
			_, err = parseValType(r)
			if err != nil {
				return exportDecl{}, fmt.Errorf("read value type: %w", err)
			}
		default:
			return exportDecl{}, fmt.Errorf("unknown value bound kind: 0x%02x", boundKind)
		}

	case 0x03:
		var boundKind byte
		if err := binary.Read(r, binary.LittleEndian, &boundKind); err != nil {
			return exportDecl{}, fmt.Errorf("read type bound kind: %w", err)
		}
		switch boundKind {
		case 0x00: // Eq bound - type index follows
			typeIndex, err = readLEB128(r)
			if err != nil {
				return exportDecl{}, fmt.Errorf("read type index: %w", err)
			}
		case 0x01: // SubResource bound - no additional data
			// SubResource bound indicates this type can be any subresource
		default:
			return exportDecl{}, fmt.Errorf("unknown type bound kind: 0x%02x", boundKind)
		}
		return exportDecl{
			Name: string(nameBytes),
			externDesc: externDesc{
				Kind:      externKind,
				TypeIndex: typeIndex,
				BoundKind: boundKind,
				HasBound:  true,
			},
		}, nil

	default:
		return exportDecl{}, fmt.Errorf("unknown extern kind: 0x%02x", externKind)
	}

	return exportDecl{
		Name: string(nameBytes),
		externDesc: externDesc{
			Kind:      externKind,
			TypeIndex: typeIndex,
		},
	}, nil
}

func readCoreType(r io.Reader) ([]byte, error) {
	buf := &bytes.Buffer{}

	var typeByte byte
	if err := binary.Read(r, binary.LittleEndian, &typeByte); err != nil {
		return nil, fmt.Errorf("read type byte: %w", err)
	}
	buf.WriteByte(typeByte)

	switch typeByte {
	case 0x4E:
		if err := readCoreRecType(r, buf); err != nil {
			return nil, fmt.Errorf("read rectype: %w", err)
		}
	case 0x00:
		var nextByte byte
		if err := binary.Read(r, binary.LittleEndian, &nextByte); err != nil {
			return nil, fmt.Errorf("read next byte after 0x00: %w", err)
		}
		buf.WriteByte(nextByte)
		if nextByte == 0x50 {
			if err := readCoreSubType(r, buf); err != nil {
				return nil, fmt.Errorf("read subtype (0x00 0x50): %w", err)
			}
		} else {
			return nil, fmt.Errorf("unknown byte after 0x00: 0x%02x", nextByte)
		}
	case 0x4F:
		if err := readCoreSubType(r, buf); err != nil {
			return nil, fmt.Errorf("read subtype (0x4F): %w", err)
		}
	case 0x50:
		if err := readCoreModuleType(r, buf); err != nil {
			return nil, fmt.Errorf("read moduletype: %w", err)
		}
	case 0x5E:
		if err := readCoreFieldType(r, buf); err != nil {
			return nil, fmt.Errorf("read array type: %w", err)
		}
	case 0x5F:
		count, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read struct field count: %w", err)
		}
		writeLEB128(buf, count)
		for i := uint32(0); i < count; i++ {
			if err := readCoreFieldType(r, buf); err != nil {
				return nil, fmt.Errorf("read struct field %d: %w", i, err)
			}
		}
	case 0x60:
		if err := readCoreFuncType(r, buf); err != nil {
			return nil, fmt.Errorf("read func type: %w", err)
		}
	default:
		if typeByte < 0x4E {
			return []byte{typeByte}, nil
		}
		return nil, fmt.Errorf("unknown core type byte: 0x%02x", typeByte)
	}

	return buf.Bytes(), nil
}

func readCoreModuleType(r io.Reader, buf *bytes.Buffer) error {
	count, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read module decl count: %w", err)
	}
	writeLEB128(buf, count)

	for i := uint32(0); i < count; i++ {
		var declKind byte
		if err := binary.Read(r, binary.LittleEndian, &declKind); err != nil {
			return fmt.Errorf("read module decl %d kind: %w", i, err)
		}
		buf.WriteByte(declKind)

		switch declKind {
		case 0x00:
			if err := readCoreImport(r, buf); err != nil {
				return fmt.Errorf("read core import %d: %w", i, err)
			}
		case 0x01:
			if err := readCoreTypeDef(r, buf); err != nil {
				return fmt.Errorf("read core type %d: %w", i, err)
			}
		case 0x02:
			if err := readCoreAliasInModule(r, buf); err != nil {
				return fmt.Errorf("read core alias %d: %w", i, err)
			}
		case 0x03:
			if err := readCoreExportDecl(r, buf); err != nil {
				return fmt.Errorf("read core export %d: %w", i, err)
			}
		default:
			return fmt.Errorf("unknown module decl kind: 0x%02x", declKind)
		}
	}

	return nil
}

func readCoreImport(r io.Reader, buf *bytes.Buffer) error {
	modLen, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read module name length: %w", err)
	}
	writeLEB128(buf, modLen)

	modName := make([]byte, modLen)
	if _, err := io.ReadFull(r, modName); err != nil {
		return fmt.Errorf("read module name: %w", err)
	}
	buf.Write(modName)

	nameLen, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read name length: %w", err)
	}
	writeLEB128(buf, nameLen)

	name := make([]byte, nameLen)
	if _, err := io.ReadFull(r, name); err != nil {
		return fmt.Errorf("read name: %w", err)
	}
	buf.Write(name)

	var importKind byte
	if err := binary.Read(r, binary.LittleEndian, &importKind); err != nil {
		return fmt.Errorf("read import kind: %w", err)
	}
	buf.WriteByte(importKind)

	switch importKind {
	case 0x00:
		idx, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read func type index: %w", err)
		}
		writeLEB128(buf, idx)
	case 0x01:
		if err := readCoreTableType(r, buf); err != nil {
			return fmt.Errorf("read table type: %w", err)
		}
	case 0x02:
		if err := readCoreMemoryType(r, buf); err != nil {
			return fmt.Errorf("read memory type: %w", err)
		}
	case 0x03:
		if err := readCoreGlobalType(r, buf); err != nil {
			return fmt.Errorf("read global type: %w", err)
		}
	default:
		return fmt.Errorf("unknown import kind: 0x%02x", importKind)
	}

	return nil
}

func readCoreTypeDef(r io.Reader, buf *bytes.Buffer) error {
	typeBytes, err := readCoreType(r)
	if err != nil {
		return fmt.Errorf("read core type: %w", err)
	}
	buf.Write(typeBytes)
	return nil
}

func readCoreAliasInModule(r io.Reader, buf *bytes.Buffer) error {
	var sort byte
	if err := binary.Read(r, binary.LittleEndian, &sort); err != nil {
		return fmt.Errorf("read sort: %w", err)
	}
	buf.WriteByte(sort)

	var targetKind byte
	if err := binary.Read(r, binary.LittleEndian, &targetKind); err != nil {
		return fmt.Errorf("read target kind: %w", err)
	}
	buf.WriteByte(targetKind)

	switch targetKind {
	case 0x01:
		idx, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read instance idx: %w", err)
		}
		writeLEB128(buf, idx)

		nameLen, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read name length: %w", err)
		}
		writeLEB128(buf, nameLen)

		name := make([]byte, nameLen)
		if _, err := io.ReadFull(r, name); err != nil {
			return fmt.Errorf("read name: %w", err)
		}
		buf.Write(name)
	case 0x02:
		ct, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read ct: %w", err)
		}
		writeLEB128(buf, ct)

		idx, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read idx: %w", err)
		}
		writeLEB128(buf, idx)
	default:
		return fmt.Errorf("unknown alias target kind: 0x%02x", targetKind)
	}

	return nil
}

func readCoreExportDecl(r io.Reader, buf *bytes.Buffer) error {
	nameLen, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read name length: %w", err)
	}
	writeLEB128(buf, nameLen)

	name := make([]byte, nameLen)
	if _, err := io.ReadFull(r, name); err != nil {
		return fmt.Errorf("read name: %w", err)
	}
	buf.Write(name)

	var exportKind byte
	if err := binary.Read(r, binary.LittleEndian, &exportKind); err != nil {
		return fmt.Errorf("read export kind: %w", err)
	}
	buf.WriteByte(exportKind)

	idx, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}
	writeLEB128(buf, idx)

	return nil
}

func readCoreTableType(r io.Reader, buf *bytes.Buffer) error {
	if err := readCoreRefType(r, buf); err != nil {
		return fmt.Errorf("read ref type: %w", err)
	}
	return readCoreLimits(r, buf)
}

func readCoreMemoryType(r io.Reader, buf *bytes.Buffer) error {
	return readCoreLimits(r, buf)
}

func readCoreGlobalType(r io.Reader, buf *bytes.Buffer) error {
	if err := readCoreValType(r, buf); err != nil {
		return fmt.Errorf("read val type: %w", err)
	}

	var mut byte
	if err := binary.Read(r, binary.LittleEndian, &mut); err != nil {
		return fmt.Errorf("read mutability: %w", err)
	}
	buf.WriteByte(mut)

	return nil
}

func readCoreRefType(r io.Reader, buf *bytes.Buffer) error {
	var refByte byte
	if err := binary.Read(r, binary.LittleEndian, &refByte); err != nil {
		return fmt.Errorf("read ref type: %w", err)
	}
	buf.WriteByte(refByte)

	if refByte == 0x63 || refByte == 0x64 {
		idx, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read heap type index: %w", err)
		}
		writeLEB128(buf, idx)
	}

	return nil
}

func readCoreLimits(r io.Reader, buf *bytes.Buffer) error {
	var flags byte
	if err := binary.Read(r, binary.LittleEndian, &flags); err != nil {
		return fmt.Errorf("read limits flags: %w", err)
	}
	buf.WriteByte(flags)

	minVal, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read min: %w", err)
	}
	writeLEB128(buf, minVal)

	if flags&0x01 != 0 {
		maxVal, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read max: %w", err)
		}
		writeLEB128(buf, maxVal)
	}

	return nil
}

func readCoreRecType(r io.Reader, buf *bytes.Buffer) error {
	count, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read subtype count: %w", err)
	}
	writeLEB128(buf, count)

	for i := uint32(0); i < count; i++ {
		var subByte byte
		if err := binary.Read(r, binary.LittleEndian, &subByte); err != nil {
			return fmt.Errorf("read subtype %d byte: %w", i, err)
		}
		buf.WriteByte(subByte)

		switch subByte {
		case 0x4F, 0x50:
			if err := readCoreSubType(r, buf); err != nil {
				return fmt.Errorf("read subtype %d: %w", i, err)
			}
		case 0x5E, 0x5F, 0x60:
			r = io.MultiReader(bytes.NewReader([]byte{subByte}), r)
			if err := readCoreCompType(r, buf); err != nil {
				return fmt.Errorf("read comptype %d: %w", i, err)
			}
		default:
			return fmt.Errorf("unknown subtype byte: 0x%02x", subByte)
		}
	}

	return nil
}

func readCoreSubType(r io.Reader, buf *bytes.Buffer) error {
	superCount, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read super count: %w", err)
	}
	writeLEB128(buf, superCount)

	for i := uint32(0); i < superCount; i++ {
		idx, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read super %d: %w", i, err)
		}
		writeLEB128(buf, idx)
	}

	return readCoreCompType(r, buf)
}

func readCoreCompType(r io.Reader, buf *bytes.Buffer) error {
	var compByte byte
	if err := binary.Read(r, binary.LittleEndian, &compByte); err != nil {
		return fmt.Errorf("read comptype byte: %w", err)
	}
	buf.WriteByte(compByte)

	switch compByte {
	case 0x5E:
		return readCoreFieldType(r, buf)
	case 0x5F:
		count, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read struct field count: %w", err)
		}
		writeLEB128(buf, count)
		for i := uint32(0); i < count; i++ {
			if err := readCoreFieldType(r, buf); err != nil {
				return fmt.Errorf("read field %d: %w", i, err)
			}
		}
		return nil
	case 0x60:
		return readCoreFuncType(r, buf)
	default:
		return fmt.Errorf("unknown comptype byte: 0x%02x", compByte)
	}
}

func readCoreFieldType(r io.Reader, buf *bytes.Buffer) error {
	if err := readCoreValType(r, buf); err != nil {
		return fmt.Errorf("read val type: %w", err)
	}

	var mut byte
	if err := binary.Read(r, binary.LittleEndian, &mut); err != nil {
		return fmt.Errorf("read mutability: %w", err)
	}
	buf.WriteByte(mut)

	return nil
}

func readCoreFuncType(r io.Reader, buf *bytes.Buffer) error {
	paramCount, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read param count: %w", err)
	}
	writeLEB128(buf, paramCount)

	for i := uint32(0); i < paramCount; i++ {
		if err := readCoreValType(r, buf); err != nil {
			return fmt.Errorf("read param %d: %w", i, err)
		}
	}

	resultCount, err := readLEB128(r)
	if err != nil {
		return fmt.Errorf("read result count: %w", err)
	}
	writeLEB128(buf, resultCount)

	for i := uint32(0); i < resultCount; i++ {
		if err := readCoreValType(r, buf); err != nil {
			return fmt.Errorf("read result %d: %w", i, err)
		}
	}

	return nil
}

func readCoreValType(r io.Reader, buf *bytes.Buffer) error {
	var valByte byte
	if err := binary.Read(r, binary.LittleEndian, &valByte); err != nil {
		return fmt.Errorf("read val type byte: %w", err)
	}
	buf.WriteByte(valByte)

	switch valByte {
	case 0x7F, 0x7E, 0x7D, 0x7C:
		return nil
	case 0x70, 0x6F:
		return nil
	case 0x64:
		idx, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read type index: %w", err)
		}
		writeLEB128(buf, idx)
		return nil
	case 0x63:
		idx, err := readLEB128(r)
		if err != nil {
			return fmt.Errorf("read type index: %w", err)
		}
		writeLEB128(buf, idx)
		return nil
	default:
		return fmt.Errorf("unknown core val type: 0x%02x", valByte)
	}
}

func readAlias(r io.Reader) (aliasDecl, error) {
	var sort byte
	if err := binary.Read(r, binary.LittleEndian, &sort); err != nil {
		return aliasDecl{}, fmt.Errorf("read sort: %w", err)
	}

	var targetKind byte
	if err := binary.Read(r, binary.LittleEndian, &targetKind); err != nil {
		return aliasDecl{}, fmt.Errorf("read target kind: %w", err)
	}

	var data []byte

	switch targetKind {
	case 0x00:
		instanceIdx, err := readLEB128(r)
		if err != nil {
			return aliasDecl{}, fmt.Errorf("read instance idx: %w", err)
		}

		nameLen, err := readLEB128(r)
		if err != nil {
			return aliasDecl{}, fmt.Errorf("read name length: %w", err)
		}

		if nameLen > 10000 {
			return aliasDecl{}, fmt.Errorf("name length %d exceeds maximum", nameLen)
		}

		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return aliasDecl{}, fmt.Errorf("read name: %w", err)
		}

		buf := &bytes.Buffer{}
		buf.WriteByte(sort)
		buf.WriteByte(targetKind)
		writeLEB128(buf, instanceIdx)
		writeLEB128(buf, nameLen)
		buf.Write(nameBytes)
		data = buf.Bytes()

	case 0x01:
		instanceIdx, err := readLEB128(r)
		if err != nil {
			return aliasDecl{}, fmt.Errorf("read core instance idx: %w", err)
		}

		nameLen, err := readLEB128(r)
		if err != nil {
			return aliasDecl{}, fmt.Errorf("read core name length: %w", err)
		}

		if nameLen > 10000 {
			return aliasDecl{}, fmt.Errorf("core name length %d exceeds maximum", nameLen)
		}

		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return aliasDecl{}, fmt.Errorf("read core name: %w", err)
		}

		buf := &bytes.Buffer{}
		buf.WriteByte(sort)
		buf.WriteByte(targetKind)
		writeLEB128(buf, instanceIdx)
		writeLEB128(buf, nameLen)
		buf.Write(nameBytes)
		data = buf.Bytes()

	case 0x02:
		ct, err := readLEB128(r)
		if err != nil {
			return aliasDecl{}, fmt.Errorf("read ct: %w", err)
		}

		idx, err := readLEB128(r)
		if err != nil {
			return aliasDecl{}, fmt.Errorf("read idx: %w", err)
		}

		buf := &bytes.Buffer{}
		buf.WriteByte(sort)
		buf.WriteByte(targetKind)
		writeLEB128(buf, ct)
		writeLEB128(buf, idx)
		data = buf.Bytes()

	default:
		return aliasDecl{}, fmt.Errorf("unknown alias target kind: 0x%02x", targetKind)
	}

	return aliasDecl{Kind: sort, Data: data}, nil
}

func writeLEB128(w *bytes.Buffer, value uint32) {
	for {
		b := byte(value & 0x7F)
		value >>= 7
		if value != 0 {
			b |= 0x80
		}
		w.WriteByte(b)
		if value == 0 {
			break
		}
	}
}

func parseTypeDecl(r io.Reader) (Type, error) {
	count, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read component decl count: %w", err)
	}

	if count > 10000 {
		return nil, fmt.Errorf("component decl count %d exceeds maximum", count)
	}

	decls := make([]componentDecl, 0, count)

	for i := uint32(0); i < count; i++ {
		var declKind byte
		if err := binary.Read(r, binary.LittleEndian, &declKind); err != nil {
			return nil, fmt.Errorf("component decl %d: read kind: %w", i, err)
		}

		if declKind == 0x03 {
			decl, err := parseImportDecl(r)
			if err != nil {
				return nil, fmt.Errorf("component decl %d (kind=0x03 import): parse import: %w", i, err)
			}
			decls = append(decls, componentDecl{
				Kind:   declKind,
				Import: &decl,
			})
		} else {
			r = io.MultiReader(bytes.NewReader([]byte{declKind}), r)
			decl, err := parseInstanceDeclFull(r)
			if err != nil {
				return nil, fmt.Errorf("component decl %d (kind=0x%02x instancedecl): parse instance decl: %w", i, declKind, err)
			}
			decls = append(decls, componentDecl{
				Kind:         declKind,
				instanceDecl: &decl,
			})
		}
	}

	return &componentTypeDecl{Decls: decls}, nil
}

// parseValType parses a component value type
//
// This is the most complex parsing function due to overlapping byte ranges.
// The implementation follows wasm-tools Rust reference implementation exactly:
// Check primitives FIRST, then defined type opcodes, then read remaining as SLEB128 type index.
//
// Parsing strategy (ORDER MATTERS):
//
//  1. Check primitive types (0x73-0x7f, 0x64):
//     0x7f=bool, 0x7e=s8, 0x7d=u8, 0x7c=s16, 0x7b=u16, 0x7a=s32, 0x79=u32,
//     0x78=s64, 0x77=u64, 0x76=f32, 0x75=f64, 0x74=char, 0x73=string, 0x64=error-context
//
//  2. Check defined type opcodes (0x68-0x72):
//     0x72=record, 0x71=variant, 0x70=list, 0x6f=tuple, 0x6e=flags, 0x6d=enum,
//     0x6b=option, 0x6a=result, 0x69=own, 0x68=borrow
//     Note: 0x6c is RESERVED/UNUSED in current spec
//
//  3. Everything else: read as signed LEB128 type index (var_s33 - signed 33-bit)
//     Type indices can be negative in the encoding (though logically should be >= 0)
//     Uses SLEB128 encoding, NOT unsigned LEB128 - this was a critical bug fix
//
// Reference: github.com/bytecodealliance/wasm-tools component_type.rs ValType::from_reader
func parseValType(r io.Reader) (ValType, error) {
	var typeByte byte
	if err := binary.Read(r, binary.LittleEndian, &typeByte); err != nil {
		return nil, fmt.Errorf("read val type byte: %w", err)
	}

	// Step 1: Check primitive types (highest priority)
	if (typeByte >= 0x73 && typeByte <= 0x7f) || typeByte == 0x64 {
		return PrimValType{Type: PrimType(typeByte)}, nil
	}

	// Step 2: Check defined type opcodes
	switch typeByte {
	case 0x72:
		return parseDefType(r, typeByte)
	case 0x71:
		return parseDefType(r, typeByte)
	case 0x70:
		return parseDefType(r, typeByte)
	case 0x6f:
		return parseDefType(r, typeByte)
	case 0x6e:
		return parseDefType(r, typeByte)
	case 0x6d:
		return parseDefType(r, typeByte)
	case 0x6b:
		return parseDefType(r, typeByte)
	case 0x6a:
		return parseDefType(r, typeByte)
	case 0x69: // own
		idx, err := readLEB128(r)
		if err != nil {
			return nil, err
		}
		return OwnType{TypeIndex: idx}, nil
	case 0x68: // borrow
		idx, err := readLEB128(r)
		if err != nil {
			return nil, err
		}
		return BorrowType{TypeIndex: idx}, nil
	}

	// Step 3: Read as signed LEB128 type index (var_s33)
	// Put the byte back into the reader and read as SLEB128
	mr := io.MultiReader(bytes.NewReader([]byte{typeByte}), r)
	idx, err := readSLEB128(mr)
	if err != nil {
		return nil, fmt.Errorf("read type index: %w", err)
	}

	if idx < 0 {
		return nil, fmt.Errorf("negative type index: %d", idx)
	}

	return TypeIndexRef{Index: uint32(idx)}, nil
}

func parseDefType(r io.Reader, typeByte byte) (ValType, error) {
	switch typeByte {
	case 0x72: // record
		def, err := parseRecordType(r)
		if err != nil {
			return nil, err
		}
		return def, nil
	case 0x71: // variant
		def, err := parseVariantType(r)
		if err != nil {
			return nil, err
		}
		return def, nil
	case 0x70: // list
		elemType, err := parseValType(r)
		if err != nil {
			return nil, err
		}
		return ListType{ElemType: elemType}, nil
	case 0x6f: // tuple
		return parseTupleType(r)
	case 0x6e: // flags
		return parseFlagsType(r)
	case 0x6d: // enum
		return parseEnumType(r)
	case 0x6b: // option
		elemType, err := parseValType(r)
		if err != nil {
			return nil, err
		}
		return OptionType{Type: elemType}, nil
	case 0x6a: // result
		return parseResultType(r)
	default:
		return nil, fmt.Errorf("unknown def type: 0x%02x", typeByte)
	}
}

func parseRecordType(r io.Reader) (RecordType, error) {
	count, err := readLEB128(r)
	if err != nil {
		return RecordType{}, err
	}

	if count > 1000 {
		return RecordType{}, fmt.Errorf("record field count %d exceeds maximum", count)
	}

	fields := make([]FieldType, 0, count)
	for i := uint32(0); i < count; i++ {
		name, err := readString(r)
		if err != nil {
			return RecordType{}, fmt.Errorf("field %d name: %w", i, err)
		}

		valType, err := parseValType(r)
		if err != nil {
			return RecordType{}, fmt.Errorf("field %d type: %w", i, err)
		}

		fields = append(fields, FieldType{
			Name: name,
			Type: valType,
		})
	}

	return RecordType{Fields: fields}, nil
}

// parseVariantType parses a variant type (tagged union)
//
// Binary format per spec: 0x71 + vec(case)
// where case ::= label' + type? + refines?
//
// Each variant case has THREE parts (not just name + type):
//  1. Name: string with LEB128 length prefix (label')
//  2. Optional type: <ValType>? encoding (0x00 = none, 0x01 + type = some)
//     - 0x00: unit case (no payload)
//     - 0x01 + ValType: case with typed payload
//  3. Optional refines: <u32>? encoding (0x00 = none, 0x01 + index = some)
//     - 0x00: no refinement
//     - 0x01 + u32: refines case at given index (enables case inheritance/specialization)
//
// The refines field allows variant cases to refine (specialize) other cases,
// enabling inheritance-like semantics in the type system. This was a major source
// of byte alignment errors during implementation - the refines field must be read
// even when not stored in caseType.
//
// Reference: github.com/WebAssembly/component-model/blob/main/design/mvp/Binary.md
func parseVariantType(r io.Reader) (VariantType, error) {
	count, err := readLEB128(r)
	if err != nil {
		return VariantType{}, err
	}

	if count > 1000 {
		return VariantType{}, fmt.Errorf("variant case count %d exceeds maximum", count)
	}

	cases := make([]CaseType, 0, count)
	for i := uint32(0); i < count; i++ {
		name, err := readString(r)
		if err != nil {
			return VariantType{}, fmt.Errorf("case %d name: %w", i, err)
		}

		// Optional type encoding: <T>? ::= 0x00 | 0x01 t:<T>
		var hasType byte
		if err := binary.Read(r, binary.LittleEndian, &hasType); err != nil {
			return VariantType{}, fmt.Errorf("case %d has-type: %w", i, err)
		}

		var caseValType *ValType
		if hasType == 0x01 {
			valType, err := parseValType(r)
			if err != nil {
				return VariantType{}, fmt.Errorf("case %d type: %w", i, err)
			}
			caseValType = &valType
		} else if hasType != 0x00 {
			return VariantType{}, fmt.Errorf("case %d: invalid has-type discriminant 0x%02x", i, hasType)
		}

		// Optional refines encoding: <u32>? ::= 0x00 | 0x01 idx:<u32>
		var hasRefines byte
		if err := binary.Read(r, binary.LittleEndian, &hasRefines); err != nil {
			return VariantType{}, fmt.Errorf("case %d has-refines: %w", i, err)
		}

		var refinesIndex *uint32
		if hasRefines == 0x01 {
			idx, err := readLEB128(r)
			if err != nil {
				return VariantType{}, fmt.Errorf("case %d refines index: %w", i, err)
			}
			// Refines index must reference a previous case
			if idx >= i {
				return VariantType{}, fmt.Errorf("case %d: refines index %d out of bounds (must be < %d)", i, idx, i)
			}
			refinesIndex = &idx
		} else if hasRefines != 0x00 {
			return VariantType{}, fmt.Errorf("case %d: invalid refines discriminant 0x%02x", i, hasRefines)
		}

		cases = append(cases, CaseType{
			Name:    name,
			Type:    caseValType,
			Refines: refinesIndex,
		})
	}

	return VariantType{Cases: cases}, nil
}

func parseTupleType(r io.Reader) (TupleType, error) {
	count, err := readLEB128(r)
	if err != nil {
		return TupleType{}, err
	}

	if count > 1000 {
		return TupleType{}, fmt.Errorf("tuple element count %d exceeds maximum", count)
	}

	types := make([]ValType, 0, count)
	for i := uint32(0); i < count; i++ {
		valType, err := parseValType(r)
		if err != nil {
			return TupleType{}, fmt.Errorf("tuple element %d: %w", i, err)
		}
		types = append(types, valType)
	}

	return TupleType{Types: types}, nil
}

func parseFlagsType(r io.Reader) (FlagsType, error) {
	count, err := readLEB128(r)
	if err != nil {
		return FlagsType{}, err
	}

	if count > 1000 {
		return FlagsType{}, fmt.Errorf("flags count %d exceeds maximum", count)
	}

	names := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		name, err := readString(r)
		if err != nil {
			return FlagsType{}, fmt.Errorf("flag %d name: %w", i, err)
		}
		names = append(names, name)
	}

	return FlagsType{Names: names}, nil
}

func parseEnumType(r io.Reader) (EnumType, error) {
	count, err := readLEB128(r)
	if err != nil {
		return EnumType{}, err
	}

	if count > 1000 {
		return EnumType{}, fmt.Errorf("enum case count %d exceeds maximum", count)
	}

	cases := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		name, err := readString(r)
		if err != nil {
			return EnumType{}, fmt.Errorf("enum case %d: %w", i, err)
		}
		cases = append(cases, name)
	}

	return EnumType{Cases: cases}, nil
}

// parseResultType parses a result type (similar to Rust's Result<T, E>)
//
// Binary format per spec: 0x6a + <ValType>? + <ValType>?
// where <T>? ::= 0x00 | 0x01 t:<T>
//
// Both OK and Err types are optional using the <T>? encoding:
//
//	0x00       => None (no type)
//	0x01 + T   => Some(T)
//
// This allows four combinations:
//
//	result<_, _>      (both optional)
//	result<T, _>      (ok type, no err type)
//	result<_, E>      (no ok type, err type)
//	result<T, E>      (both types)
//
// The optional encoding pattern <T>? is used throughout the spec for
// variant case types, refines indices, and result types.
func parseResultType(r io.Reader) (ResultType, error) {
	var hasOK byte
	if err := binary.Read(r, binary.LittleEndian, &hasOK); err != nil {
		return ResultType{}, fmt.Errorf("read has-ok: %w", err)
	}

	var okType *ValType
	if hasOK == 0x01 {
		valType, err := parseValType(r)
		if err != nil {
			return ResultType{}, fmt.Errorf("ok type: %w", err)
		}
		okType = &valType
	} else if hasOK != 0x00 {
		return ResultType{}, fmt.Errorf("invalid has-ok discriminant: 0x%02x (must be 0x00 or 0x01)", hasOK)
	}

	var hasErr byte
	if err := binary.Read(r, binary.LittleEndian, &hasErr); err != nil {
		return ResultType{}, fmt.Errorf("read has-err: %w", err)
	}

	var errType *ValType
	if hasErr == 0x01 {
		valType, err := parseValType(r)
		if err != nil {
			return ResultType{}, fmt.Errorf("err type: %w", err)
		}
		errType = &valType
	} else if hasErr != 0x00 {
		return ResultType{}, fmt.Errorf("invalid has-err discriminant: 0x%02x (must be 0x00 or 0x01)", hasErr)
	}

	return ResultType{
		OK:  okType,
		Err: errType,
	}, nil
}

func readString(r io.Reader) (string, error) {
	length, err := readLEB128(r)
	if err != nil {
		return "", fmt.Errorf("read string length: %w", err)
	}

	if length > 10000 {
		return "", fmt.Errorf("string length %d exceeds maximum", length)
	}

	if length == 0 {
		return "", nil
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("read string bytes: %w", err)
	}

	return string(buf), nil
}
