package wasm

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/wippyai/wasm-runtime/wasm/internal/binary"
)

// Parsing errors returned by ParseModule.
var (
	ErrInvalidMagic   = errors.New("invalid wasm magic number")
	ErrInvalidVersion = errors.New("invalid wasm version")
)

// ParseModule parses a WebAssembly binary module
func ParseModule(data []byte) (*Module, error) {
	r := binary.NewReader(bytes.NewReader(data))

	// Check magic number
	magic, err := r.ReadU32LE()
	if err != nil {
		return nil, r.WrapError("header", err)
	}
	if magic != Magic {
		return nil, ErrInvalidMagic
	}

	// Check version
	version, err := r.ReadU32LE()
	if err != nil {
		return nil, r.WrapError("header", err)
	}
	if version != Version {
		return nil, ErrInvalidVersion
	}

	m := &Module{}

	// Track section ordering using canonical order, not section IDs
	// WASM spec order: Type(1), Import(2), Function(3), Table(4), Memory(5),
	// Tag(13), Global(6), Export(7), Start(8), Element(9), DataCount(12), Code(10), Data(11)
	var lastSectionOrder int

	// Parse sections
	for {
		sectionID, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, r.WrapError("section header", err)
		}

		// Validate section ordering (custom sections can appear anywhere)
		if sectionID != SectionCustom {
			order := sectionOrder(sectionID)
			if order <= lastSectionOrder {
				return nil, fmt.Errorf("section %d appears out of order", sectionID)
			}
			lastSectionOrder = order
		}

		sectionSize, err := r.ReadU32()
		if err != nil {
			return nil, r.WrapError("section size", err)
		}

		sectionData, err := r.ReadBytes(int(sectionSize))
		if err != nil {
			return nil, r.WrapError("section data", err)
		}

		sr := binary.NewReader(bytes.NewReader(sectionData))

		switch sectionID {
		case SectionCustom:
			if err := parseCustomSection(sr, m); err != nil {
				return nil, fmt.Errorf("custom section: %w", err)
			}
		case SectionType:
			if err := parseTypeSection(sr, m); err != nil {
				return nil, fmt.Errorf("type section: %w", err)
			}
		case SectionImport:
			if err := parseImportSection(sr, m); err != nil {
				return nil, fmt.Errorf("import section: %w", err)
			}
		case SectionFunction:
			if err := parseFunctionSection(sr, m); err != nil {
				return nil, fmt.Errorf("function section: %w", err)
			}
		case SectionTable:
			if err := parseTableSection(sr, m); err != nil {
				return nil, fmt.Errorf("table section: %w", err)
			}
		case SectionMemory:
			if err := parseMemorySection(sr, m); err != nil {
				return nil, fmt.Errorf("memory section: %w", err)
			}
		case SectionGlobal:
			if err := parseGlobalSection(sr, m); err != nil {
				return nil, fmt.Errorf("global section: %w", err)
			}
		case SectionExport:
			if err := parseExportSection(sr, m); err != nil {
				return nil, fmt.Errorf("export section: %w", err)
			}
		case SectionStart:
			if err := parseStartSection(sr, m); err != nil {
				return nil, fmt.Errorf("start section: %w", err)
			}
		case SectionElement:
			if err := parseElementSection(sr, m); err != nil {
				return nil, fmt.Errorf("element section: %w", err)
			}
		case SectionCode:
			if err := parseCodeSection(sr, m); err != nil {
				return nil, fmt.Errorf("code section: %w", err)
			}
		case SectionData:
			if err := parseDataSection(sr, m); err != nil {
				return nil, fmt.Errorf("data section: %w", err)
			}
		case SectionDataCount:
			if err := parseDataCountSection(sr, m); err != nil {
				return nil, fmt.Errorf("data count section: %w", err)
			}
		case SectionTag:
			if err := parseTagSection(sr, m); err != nil {
				return nil, fmt.Errorf("tag section: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown section ID: 0x%02x", sectionID)
		}
	}

	return m, nil
}

// sectionOrder returns the canonical ordering for a section ID.
// WASM spec requires sections in specific order, which differs from section IDs.
func sectionOrder(id byte) int {
	switch id {
	case SectionType:
		return 1
	case SectionImport:
		return 2
	case SectionFunction:
		return 3
	case SectionTable:
		return 4
	case SectionMemory:
		return 5
	case SectionTag:
		return 6 // Tag comes after Memory, before Global
	case SectionGlobal:
		return 7
	case SectionExport:
		return 8
	case SectionStart:
		return 9
	case SectionElement:
		return 10
	case SectionDataCount:
		return 11 // DataCount must come before Code
	case SectionCode:
		return 12
	case SectionData:
		return 13
	default:
		return 100 // Unknown sections at end
	}
}

func parseCustomSection(r *binary.Reader, m *Module) error {
	name, err := r.ReadName()
	if err != nil {
		return err
	}
	rest, err := r.ReadRemaining()
	if err != nil {
		return err
	}
	m.CustomSections = append(m.CustomSections, CustomSection{
		Name: name,
		Data: rest,
	})
	return nil
}

func parseTypeSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}

	// First pass: detect if we have any GC types
	// Read all type forms first to detect if we need TypeDefs
	startPos := r.Position()
	hasGCTypes := false

	for i := uint32(0); i < count; i++ {
		form, err := r.ReadByte()
		if err != nil {
			return err
		}

		switch form {
		case FuncTypeByte: // 0x60 - function type
			if err := skipFuncType(r); err != nil {
				return err
			}
		case RecTypeByte, SubTypeByte, SubFinalByte, StructTypeByte, ArrayTypeByte:
			hasGCTypes = true
		default:
			return fmt.Errorf("unsupported type form 0x%02x", form)
		}

		if hasGCTypes {
			break
		}
	}

	// Reset to start of type entries
	if err := r.Reset(startPos); err != nil {
		return err
	}

	// If no GC types, use simple parsing (only populate Types)
	if !hasGCTypes {
		m.Types = make([]FuncType, count)
		for i := uint32(0); i < count; i++ {
			form, err := r.ReadByte()
			if err != nil {
				return fmt.Errorf("read type form at index %d: %w", i, err)
			}
			if form != FuncTypeByte {
				return fmt.Errorf("expected functype (0x60), got 0x%02x", form)
			}
			ft, err := readFuncType(r)
			if err != nil {
				return err
			}
			m.Types[i] = ft
		}
		return nil
	}

	// GC types present - populate both TypeDefs and Types
	m.TypeDefs = make([]TypeDef, 0, count)
	m.Types = make([]FuncType, 0, count)

	for i := uint32(0); i < count; i++ {
		form, err := r.ReadByte()
		if err != nil {
			return err
		}

		switch form {
		case FuncTypeByte: // 0x60 - shorthand function type
			ft, err := readFuncType(r)
			if err != nil {
				return err
			}
			m.TypeDefs = append(m.TypeDefs, TypeDef{Kind: TypeDefKindFunc, Func: &ft})
			m.Types = append(m.Types, ft)

		case RecTypeByte: // 0x4E - recursive type group
			recCount, err := r.ReadU32()
			if err != nil {
				return err
			}
			rec := RecType{Types: make([]SubType, recCount)}
			for j := uint32(0); j < recCount; j++ {
				sub, err := readSubType(r)
				if err != nil {
					return err
				}
				rec.Types[j] = sub
				// Also add to flat Types for function types
				if sub.CompType.Kind == CompKindFunc && sub.CompType.Func != nil {
					m.Types = append(m.Types, *sub.CompType.Func)
				}
			}
			m.TypeDefs = append(m.TypeDefs, TypeDef{Kind: TypeDefKindRec, Rec: &rec})

		case SubTypeByte, SubFinalByte: // 0x50, 0x4F - subtype
			sub, err := readSubTypeWithPrefix(r, form)
			if err != nil {
				return err
			}
			m.TypeDefs = append(m.TypeDefs, TypeDef{Kind: TypeDefKindSub, Sub: &sub})
			if sub.CompType.Kind == CompKindFunc && sub.CompType.Func != nil {
				m.Types = append(m.Types, *sub.CompType.Func)
			}

		case StructTypeByte: // 0x5F - direct struct type (rare, usually wrapped)
			st, err := readStructType(r)
			if err != nil {
				return err
			}
			sub := SubType{Final: true, CompType: CompType{Kind: CompKindStruct, Struct: &st}}
			m.TypeDefs = append(m.TypeDefs, TypeDef{Kind: TypeDefKindSub, Sub: &sub})

		case ArrayTypeByte: // 0x5E - direct array type (rare, usually wrapped)
			at, err := readArrayType(r)
			if err != nil {
				return err
			}
			sub := SubType{Final: true, CompType: CompType{Kind: CompKindArray, Array: &at}}
			m.TypeDefs = append(m.TypeDefs, TypeDef{Kind: TypeDefKindSub, Sub: &sub})

		default:
			return fmt.Errorf("unsupported type form 0x%02x", form)
		}
	}
	return nil
}

func skipFuncType(r *binary.Reader) error {
	// Skip params
	paramCount, err := r.ReadU32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < paramCount; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if b == byte(ValRefNull) || b == byte(ValRef) {
			if _, err := ReadLEB128s64(r); err != nil {
				return err
			}
		}
	}
	// Skip results
	resultCount, err := r.ReadU32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < resultCount; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if b == byte(ValRefNull) || b == byte(ValRef) {
			if _, err := ReadLEB128s64(r); err != nil {
				return err
			}
		}
	}
	return nil
}

func readFuncType(r *binary.Reader) (FuncType, error) {
	extParams, simpleParams, err := readExtValTypes(r)
	if err != nil {
		return FuncType{}, err
	}
	extResults, simpleResults, err := readExtValTypes(r)
	if err != nil {
		return FuncType{}, err
	}
	return FuncType{
		Params:     simpleParams,
		Results:    simpleResults,
		ExtParams:  extParams,
		ExtResults: extResults,
	}, nil
}

func readSubType(r *binary.Reader) (SubType, error) {
	form, err := r.ReadByte()
	if err != nil {
		return SubType{}, err
	}
	return readSubTypeWithPrefix(r, form)
}

func readSubTypeWithPrefix(r *binary.Reader, form byte) (SubType, error) {
	var sub SubType

	switch form {
	case SubTypeByte, SubFinalByte: // 0x50, 0x4F - sub with parents
		sub.Final = form == SubFinalByte
		parentCount, err := r.ReadU32()
		if err != nil {
			return SubType{}, err
		}
		sub.Parents = make([]uint32, parentCount)
		for i := uint32(0); i < parentCount; i++ {
			sub.Parents[i], err = r.ReadU32()
			if err != nil {
				return SubType{}, err
			}
		}
		comp, err := readCompType(r)
		if err != nil {
			return SubType{}, err
		}
		sub.CompType = comp

	case FuncTypeByte: // 0x60 - shorthand (no sub wrapper)
		ft, err := readFuncType(r)
		if err != nil {
			return SubType{}, err
		}
		sub.Final = true
		sub.CompType = CompType{Kind: CompKindFunc, Func: &ft}

	case StructTypeByte: // 0x5F - shorthand struct
		st, err := readStructType(r)
		if err != nil {
			return SubType{}, err
		}
		sub.Final = true
		sub.CompType = CompType{Kind: CompKindStruct, Struct: &st}

	case ArrayTypeByte: // 0x5E - shorthand array
		at, err := readArrayType(r)
		if err != nil {
			return SubType{}, err
		}
		sub.Final = true
		sub.CompType = CompType{Kind: CompKindArray, Array: &at}

	default:
		return SubType{}, fmt.Errorf("invalid subtype form 0x%02x", form)
	}

	return sub, nil
}

func readCompType(r *binary.Reader) (CompType, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return CompType{}, err
	}

	switch kind {
	case FuncTypeByte: // 0x60
		ft, err := readFuncType(r)
		if err != nil {
			return CompType{}, err
		}
		return CompType{Kind: CompKindFunc, Func: &ft}, nil

	case StructTypeByte: // 0x5F
		st, err := readStructType(r)
		if err != nil {
			return CompType{}, err
		}
		return CompType{Kind: CompKindStruct, Struct: &st}, nil

	case ArrayTypeByte: // 0x5E
		at, err := readArrayType(r)
		if err != nil {
			return CompType{}, err
		}
		return CompType{Kind: CompKindArray, Array: &at}, nil

	default:
		return CompType{}, fmt.Errorf("invalid composite type 0x%02x", kind)
	}
}

func readStructType(r *binary.Reader) (StructType, error) {
	fieldCount, err := r.ReadU32()
	if err != nil {
		return StructType{}, err
	}
	fields := make([]FieldType, fieldCount)
	for i := uint32(0); i < fieldCount; i++ {
		ft, err := readFieldType(r)
		if err != nil {
			return StructType{}, err
		}
		fields[i] = ft
	}
	return StructType{Fields: fields}, nil
}

func readArrayType(r *binary.Reader) (ArrayType, error) {
	ft, err := readFieldType(r)
	if err != nil {
		return ArrayType{}, err
	}
	return ArrayType{Element: ft}, nil
}

func readFieldType(r *binary.Reader) (FieldType, error) {
	st, err := readStorageType(r)
	if err != nil {
		return FieldType{}, err
	}
	mutByte, err := r.ReadByte()
	if err != nil {
		return FieldType{}, err
	}
	return FieldType{Type: st, Mutable: mutByte != 0}, nil
}

func readStorageType(r *binary.Reader) (StorageType, error) {
	b, err := r.ReadByte()
	if err != nil {
		return StorageType{}, err
	}

	switch b {
	case PackedI8: // 0x78
		return StorageType{Kind: StorageKindPacked, Packed: PackedI8}, nil
	case PackedI16: // 0x77
		return StorageType{Kind: StorageKindPacked, Packed: PackedI16}, nil
	case byte(ValRefNull), byte(ValRef): // 0x63, 0x64 - reference type with heap type
		nullable := b == byte(ValRefNull)
		heapType, err := ReadLEB128s64(r)
		if err != nil {
			return StorageType{}, err
		}
		return StorageType{
			Kind:    StorageKindRef,
			RefType: RefType{Nullable: nullable, HeapType: heapType},
		}, nil
	default:
		// Check if it's a valtype
		return StorageType{Kind: StorageKindVal, ValType: ValType(b)}, nil
	}
}

func parseImportSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Imports = make([]Import, count)
	for i := uint32(0); i < count; i++ {
		module, err := r.ReadName()
		if err != nil {
			return err
		}
		name, err := r.ReadName()
		if err != nil {
			return err
		}
		kind, err := r.ReadByte()
		if err != nil {
			return err
		}

		imp := Import{Module: module, Name: name, Desc: ImportDesc{Kind: kind}}

		switch kind {
		case KindFunc:
			imp.Desc.TypeIdx, err = r.ReadU32()
			if err != nil {
				return err
			}
		case KindTable:
			table, err := readTableType(r)
			if err != nil {
				return err
			}
			imp.Desc.Table = &table
		case KindMemory:
			memory, err := readMemoryType(r)
			if err != nil {
				return err
			}
			imp.Desc.Memory = &memory
		case KindGlobal:
			global, err := readGlobalType(r)
			if err != nil {
				return err
			}
			imp.Desc.Global = &global
		case KindTag:
			tag, err := readTagType(r)
			if err != nil {
				return err
			}
			imp.Desc.Tag = &tag
		default:
			return fmt.Errorf("unknown import kind: %d", kind)
		}

		m.Imports[i] = imp
	}
	return nil
}

func parseFunctionSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Funcs = make([]uint32, count)
	for i := uint32(0); i < count; i++ {
		m.Funcs[i], err = r.ReadU32()
		if err != nil {
			return err
		}
	}
	return nil
}

func parseTableSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Tables = make([]TableType, count)
	for i := uint32(0); i < count; i++ {
		m.Tables[i], err = readTableType(r)
		if err != nil {
			return err
		}
	}
	return nil
}

func parseMemorySection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Memories = make([]MemoryType, count)
	for i := uint32(0); i < count; i++ {
		m.Memories[i], err = readMemoryType(r)
		if err != nil {
			return err
		}
	}
	return nil
}

func parseGlobalSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Globals = make([]Global, count)
	for i := uint32(0); i < count; i++ {
		globalType, err := readGlobalType(r)
		if err != nil {
			return err
		}
		init, err := readInitExpr(r)
		if err != nil {
			return err
		}
		m.Globals[i] = Global{
			Type: globalType,
			Init: init,
		}
	}
	return nil
}

func parseExportSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Exports = make([]Export, count)
	for i := uint32(0); i < count; i++ {
		name, err := r.ReadName()
		if err != nil {
			return err
		}
		kind, err := r.ReadByte()
		if err != nil {
			return err
		}
		if kind > KindTag {
			return fmt.Errorf("invalid export kind: 0x%02x", kind)
		}
		idx, err := r.ReadU32()
		if err != nil {
			return err
		}
		m.Exports[i] = Export{Name: name, Kind: kind, Idx: idx}
	}
	return nil
}

func parseStartSection(r *binary.Reader, m *Module) error {
	idx, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Start = &idx
	return nil
}

func parseElementSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Elements = make([]Element, count)
	for i := uint32(0); i < count; i++ {
		flags, err := r.ReadU32()
		if err != nil {
			return err
		}
		if flags > 7 {
			return fmt.Errorf("invalid element segment flags: %d", flags)
		}

		elem := Element{Flags: flags}

		// Bit 1: passive/declarative (no table index or offset)
		// Bit 2: explicit table index
		hasTableIdx := flags&0x02 != 0 && flags&0x01 == 0
		hasOffset := flags&0x01 == 0
		usesExprs := flags&0x04 != 0

		if hasTableIdx {
			elem.TableIdx, err = r.ReadU32()
			if err != nil {
				return err
			}
		}

		if hasOffset {
			elem.Offset, err = readInitExpr(r)
			if err != nil {
				return err
			}
		}

		// Flags 1, 2, 3: elemkind follows (must be 0x00 for funcref)
		// Flags 5, 6, 7: reftype follows
		if flags&0x03 != 0 {
			if usesExprs {
				// reftype - may be GC reference type with heap type
				t, refType, err := readRefType(r)
				if err != nil {
					return err
				}
				elem.Type = ValType(t)
				elem.RefType = refType
			} else {
				// elemkind
				elem.ElemKind, err = r.ReadByte()
				if err != nil {
					return err
				}
			}
		}

		// Read the vector of indices or expressions
		vecCount, err := r.ReadU32()
		if err != nil {
			return err
		}

		if usesExprs {
			elem.Exprs = make([][]byte, vecCount)
			for j := uint32(0); j < vecCount; j++ {
				elem.Exprs[j], err = readInitExpr(r)
				if err != nil {
					return err
				}
			}
		} else {
			elem.FuncIdxs = make([]uint32, vecCount)
			for j := uint32(0); j < vecCount; j++ {
				elem.FuncIdxs[j], err = r.ReadU32()
				if err != nil {
					return err
				}
			}
		}

		m.Elements[i] = elem
	}
	return nil
}

func parseCodeSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Code = make([]FuncBody, count)
	for i := uint32(0); i < count; i++ {
		bodySize, err := r.ReadU32()
		if err != nil {
			return err
		}
		bodyData, err := r.ReadBytes(int(bodySize))
		if err != nil {
			return err
		}

		br := binary.NewReader(bytes.NewReader(bodyData))

		localCount, err := br.ReadU32()
		if err != nil {
			return err
		}
		var locals []LocalEntry
		for j := uint32(0); j < localCount; j++ {
			n, err := br.ReadU32()
			if err != nil {
				return err
			}
			t, err := br.ReadByte()
			if err != nil {
				return err
			}
			entry := LocalEntry{Count: n, ValType: ValType(t)}
			// Handle GC reference types (0x63/0x64) with heap type
			if t == byte(ValRefNull) || t == byte(ValRef) {
				heapType, err := ReadLEB128s64(br)
				if err != nil {
					return err
				}
				entry.ExtType = &ExtValType{
					Kind:    ExtValKindRef,
					ValType: ValType(t),
					RefType: RefType{Nullable: t == byte(ValRefNull), HeapType: heapType},
				}
			}
			locals = append(locals, entry)
		}

		code, err := br.ReadRemaining()
		if err != nil {
			return err
		}

		m.Code[i] = FuncBody{Locals: locals, Code: code}
	}
	return nil
}

func parseDataSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Data = make([]DataSegment, count)
	for i := uint32(0); i < count; i++ {
		flags, err := r.ReadU32()
		if err != nil {
			return err
		}
		if flags > 2 {
			return fmt.Errorf("invalid data segment flags: %d", flags)
		}

		seg := DataSegment{Flags: flags}

		// flags=0: active, memIdx=0, offset, data
		// flags=1: passive, data only
		// flags=2: active, memIdx, offset, data
		if flags == 2 {
			seg.MemIdx, err = r.ReadU32()
			if err != nil {
				return err
			}
		}

		if flags != 1 {
			seg.Offset, err = readInitExpr(r)
			if err != nil {
				return err
			}
		}

		initLen, err := r.ReadU32()
		if err != nil {
			return err
		}
		seg.Init, err = r.ReadBytes(int(initLen))
		if err != nil {
			return err
		}

		m.Data[i] = seg
	}
	return nil
}

func parseDataCountSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.DataCount = &count
	return nil
}

func parseTagSection(r *binary.Reader, m *Module) error {
	count, err := r.ReadU32()
	if err != nil {
		return err
	}
	m.Tags = make([]TagType, count)
	for i := uint32(0); i < count; i++ {
		m.Tags[i], err = readTagType(r)
		if err != nil {
			return err
		}
	}
	return nil
}

// readExtValTypes reads value types with full extended type information.
// Returns both extended types (for GC support) and simplified ValType slice (for compatibility).
func readExtValTypes(r *binary.Reader) ([]ExtValType, []ValType, error) {
	count, err := r.ReadU32()
	if err != nil {
		return nil, nil, err
	}
	extTypes := make([]ExtValType, count)
	simpleTypes := make([]ValType, count)

	for i := uint32(0); i < count; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return nil, nil, err
		}

		switch b {
		case byte(ValRefNull): // 0x63 - (ref null ht)
			heapType, err := ReadLEB128s64(r)
			if err != nil {
				return nil, nil, err
			}
			extTypes[i] = ExtValType{
				Kind:    ExtValKindRef,
				ValType: ValRefNull,
				RefType: RefType{Nullable: true, HeapType: heapType},
			}
			simpleTypes[i] = ValRefNull

		case byte(ValRef): // 0x64 - (ref ht)
			heapType, err := ReadLEB128s64(r)
			if err != nil {
				return nil, nil, err
			}
			extTypes[i] = ExtValType{
				Kind:    ExtValKindRef,
				ValType: ValRef,
				RefType: RefType{Nullable: false, HeapType: heapType},
			}
			simpleTypes[i] = ValRef

		default:
			// Simple value type
			extTypes[i] = ExtValType{
				Kind:    ExtValKindSimple,
				ValType: ValType(b),
			}
			simpleTypes[i] = ValType(b)
		}
	}
	return extTypes, simpleTypes, nil
}

func readLimits(r *binary.Reader) (Limits, error) {
	flags, err := r.ReadByte()
	if err != nil {
		return Limits{}, err
	}

	memory64 := flags&LimitsMemory64 != 0
	l := Limits{
		Shared:   flags&LimitsShared != 0,
		Memory64: memory64,
	}

	if memory64 {
		l.Min, err = r.ReadU64()
		if err != nil {
			return Limits{}, err
		}
		if flags&LimitsHasMax != 0 {
			maxVal, err := r.ReadU64()
			if err != nil {
				return Limits{}, err
			}
			l.Max = &maxVal
		}
	} else {
		minVal, err := r.ReadU32()
		if err != nil {
			return Limits{}, err
		}
		l.Min = uint64(minVal)
		if flags&LimitsHasMax != 0 {
			maxVal, err := r.ReadU32()
			if err != nil {
				return Limits{}, err
			}
			max64 := uint64(maxVal)
			l.Max = &max64
		}
	}

	// Validate min <= max
	if l.Max != nil && l.Min > *l.Max {
		return Limits{}, fmt.Errorf("limits min (%d) exceeds max (%d)", l.Min, *l.Max)
	}

	return l, nil
}

func readTableType(r *binary.Reader) (TableType, error) {
	first, err := r.ReadByte()
	if err != nil {
		return TableType{}, err
	}

	// Check for table with init expression (0x40 0x00 prefix)
	if first == 0x40 {
		zero, err := r.ReadByte()
		if err != nil {
			return TableType{}, err
		}
		if zero != 0x00 {
			return TableType{}, fmt.Errorf("expected 0x00 after 0x40, got 0x%02x", zero)
		}
		elemType, refElemType, err := readRefType(r)
		if err != nil {
			return TableType{}, err
		}
		limits, err := readLimits(r)
		if err != nil {
			return TableType{}, err
		}
		init, err := readInitExpr(r)
		if err != nil {
			return TableType{}, err
		}
		return TableType{ElemType: elemType, Limits: limits, Init: init, RefElemType: refElemType}, nil
	}

	// Standard format: reftype limits
	// Handle GC reference types (0x63/0x64) with heap type
	var refElemType *RefType
	if first == byte(ValRefNull) || first == byte(ValRef) {
		heapType, err := ReadLEB128s64(r)
		if err != nil {
			return TableType{}, err
		}
		refElemType = &RefType{Nullable: first == byte(ValRefNull), HeapType: heapType}
	}

	limits, err := readLimits(r)
	if err != nil {
		return TableType{}, err
	}
	return TableType{ElemType: first, Limits: limits, RefElemType: refElemType}, nil
}

// readRefType reads a reference type that may be 0x63/0x64 with heap type
func readRefType(r *binary.Reader) (byte, *RefType, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if b == byte(ValRefNull) || b == byte(ValRef) {
		heapType, err := ReadLEB128s64(r)
		if err != nil {
			return 0, nil, err
		}
		return b, &RefType{Nullable: b == byte(ValRefNull), HeapType: heapType}, nil
	}
	return b, nil, nil
}

func readMemoryType(r *binary.Reader) (MemoryType, error) {
	limits, err := readLimits(r)
	if err != nil {
		return MemoryType{}, err
	}
	return MemoryType{Limits: limits}, nil
}

func readGlobalType(r *binary.Reader) (GlobalType, error) {
	valType, err := r.ReadByte()
	if err != nil {
		return GlobalType{}, err
	}
	gt := GlobalType{ValType: ValType(valType)}

	// Handle GC reference types (0x63/0x64) with heap type
	if valType == byte(ValRefNull) || valType == byte(ValRef) {
		heapType, err := ReadLEB128s64(r)
		if err != nil {
			return GlobalType{}, err
		}
		gt.ExtType = &ExtValType{
			Kind:    ExtValKindRef,
			ValType: ValType(valType),
			RefType: RefType{Nullable: valType == byte(ValRefNull), HeapType: heapType},
		}
	}

	mut, err := r.ReadByte()
	if err != nil {
		return GlobalType{}, err
	}
	gt.Mutable = mut != 0
	return gt, nil
}

func readTagType(r *binary.Reader) (TagType, error) {
	attribute, err := r.ReadByte()
	if err != nil {
		return TagType{}, err
	}
	typeIdx, err := r.ReadU32()
	if err != nil {
		return TagType{}, err
	}
	return TagType{Attribute: attribute, TypeIdx: typeIdx}, nil
}

func readInitExpr(r *binary.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		buf.WriteByte(b)
		if b == OpEnd {
			break
		}
		// Copy immediate based on opcode
		if err := copyInitExprImmediate(r, &buf, b); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func copyInitExprImmediate(r *binary.Reader, buf *bytes.Buffer, opcode byte) error {
	switch opcode {
	case OpI32Const:
		return copyLEB128(r, buf)
	case OpI64Const:
		return copyLEB128(r, buf)
	case OpF32Const:
		return copyBytes(r, buf, 4)
	case OpF64Const:
		return copyBytes(r, buf, 8)
	case OpGlobalGet:
		return copyLEB128(r, buf)
	case OpRefNull:
		// ref.null has a heap type immediate (s33)
		return copyLEB128(r, buf)
	case OpRefFunc:
		// ref.func has a function index immediate
		return copyLEB128(r, buf)
	// Extended-const proposal: arithmetic and bitwise in init expressions
	case OpI32Add, OpI32Sub, OpI32Mul, OpI32And, OpI32Or, OpI32Xor,
		OpI64Add, OpI64Sub, OpI64Mul, OpI64And, OpI64Or, OpI64Xor:
		// No immediates
		return nil
	case OpPrefixSIMD:
		subOp, err := r.ReadU32()
		if err != nil {
			return err
		}
		WriteLEB128u(buf, subOp)
		if subOp == SimdV128Const {
			// v128.const has 16 bytes of immediate data
			return copyBytes(r, buf, 16)
		}
		// Other SIMD ops not valid in init expressions
		return nil
	case OpPrefixGC:
		// GC operations valid in const expressions
		subOp, err := r.ReadU32()
		if err != nil {
			return err
		}
		WriteLEB128u(buf, subOp)
		switch subOp {
		case GCStructNew, GCStructNewDefault, GCArrayNew, GCArrayNewDefault:
			// typeidx
			return copyLEB128(r, buf)
		case GCArrayNewFixed:
			// typeidx, count
			if err := copyLEB128(r, buf); err != nil {
				return err
			}
			return copyLEB128(r, buf)
		case GCArrayNewData, GCArrayNewElem:
			// typeidx, dataidx/elemidx
			if err := copyLEB128(r, buf); err != nil {
				return err
			}
			return copyLEB128(r, buf)
		case GCAnyConvertExtern, GCExternConvertAny, GCRefI31:
			// No immediates
			return nil
		}
	}
	return nil
}

func copyLEB128(r *binary.Reader, buf *bytes.Buffer) error {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		buf.WriteByte(b)
		if b&0x80 == 0 {
			break
		}
	}
	return nil
}

func copyBytes(r *binary.Reader, buf *bytes.Buffer, n int) error {
	data, err := r.ReadBytes(n)
	if err != nil {
		return err
	}
	buf.Write(data)
	return nil
}
