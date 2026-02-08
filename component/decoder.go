package component

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Component holds the decoded structure of a WebAssembly Component
type Component struct {
	CoreModules    [][]byte
	CoreTypes      [][]byte
	CoreInstances  []CoreInstance
	Instances      []Instance
	Aliases        []Alias
	Types          []TypeDef
	Canons         []Canon
	Start          *StartFunc
	Imports        []Import
	Exports        []Export
	CustomSections []CustomSection
	Components     [][]byte

	// TypeIndexSpace is the cumulative type index space (requires ParseTypes=true)
	TypeIndexSpace []Type

	// FuncIndexSpace is the component function index space
	FuncIndexSpace []FuncIndexEntry

	// InstanceTypes maps instance index to its type index
	InstanceTypes []uint32

	// LiftedFuncIndex maps canon lift index to component function index
	LiftedFuncIndex map[int]uint32

	// SectionOrder records section ordering for index space construction
	SectionOrder []SectionMarker

	// CoreFuncIndexSpace tracks the core func index space
	CoreFuncIndexSpace []CoreFuncEntry
}

// StartFunc holds the component's start function (section 9)
type StartFunc struct {
	Args      []uint32
	FuncIndex uint32
	Results   uint32
}

// CoreFuncEntry describes a core function in the core func index space
type CoreFuncEntry struct {
	ExportName  string
	Kind        CoreFuncKind
	InstanceIdx int
	FuncIndex   uint32
	Resource    uint32
	MemoryIdx   int32
	ReallocIdx  int32
}

type CoreFuncKind int

const (
	CoreFuncAliasExport  CoreFuncKind = iota // Alias from instance export
	CoreFuncCanonLower                       // canon lower
	CoreFuncResourceDrop                     // canon resource.drop
	CoreFuncResourceNew                      // canon resource.new
	CoreFuncResourceRep                      // canon resource.rep
)

// SectionMarker identifies a section for index space construction
type SectionMarker struct {
	Kind       SectionKind
	StartIndex int // Starting index in the corresponding slice (Aliases, Canons, or Exports)
	Count      int // Number of items in this section
}

type SectionKind int

const (
	SectionAlias SectionKind = iota
	SectionCanon
	SectionExport
	SectionType
)

// FuncIndexEntry describes a function in the component function index space.
// InstanceIdx and ExportName together identify the function's source.
type FuncIndexEntry struct {
	ExportName  string
	InstanceIdx uint32
}

type Import struct {
	Name       string
	ExternKind byte
	TypeIndex  uint32
}

type Export struct {
	Name      string
	Sort      byte
	SortIndex uint32
}

type CoreInstance struct {
	Parsed  *ParsedCoreInstance
	RawData []byte
}

type Instance struct {
	RawData []byte
}

type Alias struct {
	Parsed  *ParsedAlias
	RawData []byte
}

// ParsedAlias holds parsed alias data
type ParsedAlias struct {
	Name       string
	Instance   uint32
	OuterCount uint32
	OuterIndex uint32
	Sort       byte
	CoreSort   byte
	TargetKind byte
}

type TypeDef struct {
	Parsed  *TypeSection
	RawData []byte
}

type Canon struct {
	Parsed  *CanonDef
	RawData []byte
}

type CustomSection struct {
	Name string
	Data []byte
}

// externDesc kinds
const (
	ExternCoreModule byte = 0x00
	ExternFunc       byte = 0x01
	ExternValue      byte = 0x02
	ExternType       byte = 0x03
	ExternComponent  byte = 0x04
	ExternInstance   byte = 0x05
)

// Sort kinds
const (
	SortCore      byte = 0x00
	SortFunc      byte = 0x01
	SortValue     byte = 0x02
	SortType      byte = 0x03
	SortComponent byte = 0x04
	SortInstance  byte = 0x05
)

// maxNameLength bounds allocations to prevent OOM from malformed binaries
const maxNameLength = 100000

func IsComponent(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	if data[0] != 0x00 || data[1] != 0x61 || data[2] != 0x73 || data[3] != 0x6D {
		return false
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	return version > 1
}

// DecodeOptions controls decoding behavior
type DecodeOptions struct {
	ParseTypes bool // Parse WIT type definitions (Section 7)
}

// DecodeAndValidate decodes a component with full type resolution and validation
func DecodeAndValidate(data []byte) (*ValidatedComponent, error) {
	if !IsComponent(data) {
		return nil, fmt.Errorf("not a component")
	}

	// First decode the raw component
	raw, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		return nil, fmt.Errorf("decode raw component: %w", err)
	}

	// Now run streaming validation to get properly resolved types
	validator := NewStreamingValidator()

	// Process version header
	version := binary.LittleEndian.Uint32(data[4:8])
	if err := validator.Version(uint16(version)); err != nil {
		return nil, err
	}

	r := getReader(data[8:])
	defer putReader(r)

	sectionCount := 0
	maxSections := 100000

	for {
		sectionCount++
		if sectionCount > maxSections {
			return nil, fmt.Errorf("exceeded maximum section count %d", maxSections)
		}

		sectionID, err := readByte(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read section ID: %w", err)
		}

		size, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read section size: %w", err)
		}

		if size > uint32(len(data)) {
			return nil, fmt.Errorf("section %d size %d exceeds component size %d", sectionCount, size, len(data))
		}

		sectionData := make([]byte, size)
		_, err = io.ReadFull(r, sectionData)
		if err != nil {
			return nil, fmt.Errorf("read section data: %w", err)
		}

		// Process section in streaming validator
		if err := validator.ProcessSection(sectionID, sectionData); err != nil {
			return nil, fmt.Errorf("validate section %d: %w", sectionID, err)
		}
	}

	validated, err := validator.End()
	if err != nil {
		return nil, err
	}

	// Attach the raw component data
	validated.Raw = raw

	return validated, nil
}

// DecodeWithOptions decodes a component with the given options
func DecodeWithOptions(data []byte, opts DecodeOptions) (*Component, error) {
	if !IsComponent(data) {
		return nil, fmt.Errorf("not a component")
	}

	r := getReader(data[8:])
	defer putReader(r)
	comp := &Component{}

	sectionCount := 0
	maxSections := 100000

	for {
		sectionCount++
		if sectionCount > maxSections {
			return nil, fmt.Errorf("exceeded maximum section count %d", maxSections)
		}

		sectionID, err := readByte(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read section ID: %w", err)
		}

		size, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read section size: %w", err)
		}

		// Sanity check on section size
		if size > uint32(len(data)) {
			return nil, fmt.Errorf("section %d size %d exceeds component size %d", sectionCount, size, len(data))
		}

		sectionData := make([]byte, size)
		_, err = io.ReadFull(r, sectionData)
		if err != nil {
			return nil, fmt.Errorf("read section data: %w", err)
		}

		switch sectionID {
		case 0:
			customSection, err := decodeCustomSection(sectionData)
			if err != nil {
				return nil, fmt.Errorf("decode custom section: %w", err)
			}
			comp.CustomSections = append(comp.CustomSections, customSection)
		case 1:
			comp.CoreModules = append(comp.CoreModules, sectionData)
		case 2:
			if opts.ParseTypes && len(sectionData) > 0 {
				parsed, err := ParseCoreInstanceSection(sectionData)
				if err == nil {
					for _, p := range parsed {
						comp.CoreInstances = append(comp.CoreInstances, CoreInstance{
							RawData: sectionData,
							Parsed:  p,
						})
					}
				} else {
					comp.CoreInstances = append(comp.CoreInstances, CoreInstance{RawData: sectionData})
				}
			} else {
				comp.CoreInstances = append(comp.CoreInstances, CoreInstance{RawData: sectionData})
			}
		case 3:
			comp.CoreTypes = append(comp.CoreTypes, sectionData)
		case 4:
			comp.Components = append(comp.Components, sectionData)
		case 5:
			comp.Instances = append(comp.Instances, Instance{RawData: sectionData})
		case 6:
			startIdx := len(comp.Aliases)
			if opts.ParseTypes && len(sectionData) > 0 {
				parsedAliases, err := parseAliasSection(sectionData)
				if err == nil {
					for _, parsed := range parsedAliases {
						alias := Alias{RawData: sectionData, Parsed: parsed}
						comp.Aliases = append(comp.Aliases, alias)

						// Type aliases (sort=0x03) add to the type index space
						if parsed.Sort == 0x03 {
							// For instance export type aliases, store the full context
							// for deferred resolution
							if parsed.TargetKind == 0x00 {
								comp.TypeIndexSpace = append(comp.TypeIndexSpace, typeAlias{
									InstanceIdx: parsed.Instance,
									ExportName:  parsed.Name,
								})
							} else {
								// Outer alias - still use TypeIndexRef for now
								comp.TypeIndexSpace = append(comp.TypeIndexSpace, TypeIndexRef{Index: parsed.OuterIndex})
							}
						}

						// Function aliases (sort=0x01) add to the function index space
						if parsed.Sort == 0x01 && parsed.TargetKind == 0x00 {
							// Instance export alias - record the instance and export name
							comp.FuncIndexSpace = append(comp.FuncIndexSpace, FuncIndexEntry{
								InstanceIdx: parsed.Instance,
								ExportName:  parsed.Name,
							})
						}

						// Core func aliases (sort=0x00, coreSort=0x00) add to core func index space
						if parsed.Sort == 0x00 && parsed.CoreSort == 0x00 && parsed.TargetKind == 0x01 {
							comp.CoreFuncIndexSpace = append(comp.CoreFuncIndexSpace, CoreFuncEntry{
								Kind:        CoreFuncAliasExport,
								InstanceIdx: int(parsed.Instance),
								ExportName:  parsed.Name,
							})
						}
					}
					// Track section order
					comp.SectionOrder = append(comp.SectionOrder, SectionMarker{
						Kind:       SectionAlias,
						StartIndex: startIdx,
						Count:      len(parsedAliases),
					})
				} else {
					// Parsing failed, store raw data only
					comp.Aliases = append(comp.Aliases, Alias{RawData: sectionData})
				}
			} else {
				comp.Aliases = append(comp.Aliases, Alias{RawData: sectionData})
			}
		case 7:
			startIdx := len(comp.Types)
			if opts.ParseTypes {
				parsed, err := ParseTypeSection(sectionData)
				if err != nil {
					return nil, fmt.Errorf("parse type section %d: %w", sectionCount, err)
				}
				comp.Types = append(comp.Types, TypeDef{
					Parsed:  parsed,
					RawData: sectionData,
				})
				// Add all types from this section to the type index space
				comp.TypeIndexSpace = append(comp.TypeIndexSpace, parsed.Types...)
				// Track section order
				comp.SectionOrder = append(comp.SectionOrder, SectionMarker{
					Kind:       SectionType,
					StartIndex: startIdx,
					Count:      len(parsed.Types),
				})
			} else {
				comp.Types = append(comp.Types, TypeDef{
					Parsed:  nil,
					RawData: sectionData,
				})
			}
		case 8:
			startIdx := len(comp.Canons)
			parsed, err := ParseCanonSection(sectionData)
			if err != nil {
				return nil, fmt.Errorf("parse canon section %d: %w", sectionCount, err)
			}
			comp.Canons = append(comp.Canons, Canon{
				Parsed:  parsed,
				RawData: sectionData,
			})
			// Track section order
			comp.SectionOrder = append(comp.SectionOrder, SectionMarker{
				Kind:       SectionCanon,
				StartIndex: startIdx,
				Count:      1,
			})
			// Canon operations create core funcs
			if parsed != nil {
				switch parsed.Kind {
				case CanonLower:
					comp.CoreFuncIndexSpace = append(comp.CoreFuncIndexSpace, CoreFuncEntry{
						Kind:       CoreFuncCanonLower,
						FuncIndex:  parsed.FuncIndex,
						MemoryIdx:  int32(parsed.GetMemoryIndex()),
						ReallocIdx: parsed.GetReallocIndex(),
					})
				case CanonResourceDrop:
					comp.CoreFuncIndexSpace = append(comp.CoreFuncIndexSpace, CoreFuncEntry{
						Kind:     CoreFuncResourceDrop,
						Resource: parsed.ResourceType,
					})
				case CanonResourceNew:
					comp.CoreFuncIndexSpace = append(comp.CoreFuncIndexSpace, CoreFuncEntry{
						Kind:     CoreFuncResourceNew,
						Resource: parsed.ResourceType,
					})
				case CanonResourceRep:
					comp.CoreFuncIndexSpace = append(comp.CoreFuncIndexSpace, CoreFuncEntry{
						Kind:     CoreFuncResourceRep,
						Resource: parsed.ResourceType,
					})
				}
			}
		case 9:
			start, err := parseStartSection(sectionData)
			if err != nil {
				return nil, fmt.Errorf("parse start section: %w", err)
			}
			comp.Start = start
		case 10:
			imports, err := decodeImports(sectionData)
			if err != nil {
				return nil, fmt.Errorf("decode imports: %w", err)
			}
			comp.Imports = append(comp.Imports, imports...)

			if opts.ParseTypes {
				for _, imp := range imports {
					// Type imports (extern kind 0x03) add to the type index space
					if imp.ExternKind == ExternType {
						// The import references an existing type
						comp.TypeIndexSpace = append(comp.TypeIndexSpace, TypeIndexRef{Index: imp.TypeIndex})
					}

					// Instance imports (extern kind 0x05) create instances
					if imp.ExternKind == ExternInstance {
						comp.InstanceTypes = append(comp.InstanceTypes, imp.TypeIndex)
					}
				}
			}
		case 11:
			startIdx := len(comp.Exports)
			exports, err := decodeExports(sectionData)
			if err != nil {
				return nil, fmt.Errorf("decode exports: %w", err)
			}
			comp.Exports = append(comp.Exports, exports...)
			comp.SectionOrder = append(comp.SectionOrder, SectionMarker{
				Kind:       SectionExport,
				StartIndex: startIdx,
				Count:      len(exports),
			})
		}
	}

	if len(comp.CoreModules) == 0 {
		return nil, fmt.Errorf("no core modules found in component")
	}

	return comp, nil
}

func decodeCustomSection(data []byte) (CustomSection, error) {
	r := getReader(data)
	defer putReader(r)

	nameLen, err := readLEB128(r)
	if err != nil {
		return CustomSection{}, fmt.Errorf("read custom section name length: %w", err)
	}

	if nameLen > maxNameLength {
		return CustomSection{}, fmt.Errorf("custom section name length %d exceeds maximum %d", nameLen, maxNameLength)
	}
	if nameLen > uint32(len(data)) {
		return CustomSection{}, fmt.Errorf("custom section name length %d exceeds data size %d", nameLen, len(data))
	}

	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		return CustomSection{}, fmt.Errorf("read custom section name: %w", err)
	}

	// Read remaining bytes as custom section data
	remaining := make([]byte, r.Len())
	if _, err := io.ReadFull(r, remaining); err != nil && !errors.Is(err, io.EOF) {
		return CustomSection{}, fmt.Errorf("read custom section data: %w", err)
	}

	return CustomSection{
		Name: string(nameBytes),
		Data: remaining,
	}, nil
}

// parseStartSection parses section 9
func parseStartSection(data []byte) (*StartFunc, error) {
	r := getReader(data)
	defer putReader(r)

	funcIdx, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read func index: %w", err)
	}

	argCount, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read arg count: %w", err)
	}

	args := make([]uint32, argCount)
	for i := uint32(0); i < argCount; i++ {
		args[i], err = readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read arg %d: %w", i, err)
		}
	}

	results, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}

	return &StartFunc{
		FuncIndex: funcIdx,
		Args:      args,
		Results:   results,
	}, nil
}

// parseAliasSection parses an alias section (section 6)
func parseAliasSection(data []byte) ([]*ParsedAlias, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("alias section too short")
	}

	r := getReader(data)
	defer putReader(r)

	count, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	if count > 10000 {
		return nil, fmt.Errorf("alias count %d exceeds maximum", count)
	}

	aliases := make([]*ParsedAlias, 0, count)

	for i := uint32(0); i < count; i++ {
		sort, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("alias %d: read sort: %w", i, err)
		}

		// For core sort (0x00), read the additional core:sort byte
		var coreSort byte
		if sort == 0x00 {
			coreSort, err = readByte(r)
			if err != nil {
				return nil, fmt.Errorf("alias %d: read core:sort: %w", i, err)
			}
		}

		targetKind, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("alias %d: read target kind: %w", i, err)
		}

		alias := &ParsedAlias{
			Sort:       sort,
			CoreSort:   coreSort,
			TargetKind: targetKind,
		}

		switch targetKind {
		case 0x00: // instance export
			instIdx, err := readLEB128(r)
			if err != nil {
				return nil, fmt.Errorf("alias %d: read instance idx: %w", i, err)
			}
			name, err := readName(r)
			if err != nil {
				return nil, fmt.Errorf("alias %d: %w", i, err)
			}
			alias.Instance = instIdx
			alias.Name = name

		case 0x01: // core instance export (for core sort)
			instIdx, err := readLEB128(r)
			if err != nil {
				return nil, fmt.Errorf("alias %d: read core instance idx: %w", i, err)
			}
			name, err := readName(r)
			if err != nil {
				return nil, fmt.Errorf("alias %d core: %w", i, err)
			}
			alias.Instance = instIdx
			alias.Name = name

		case 0x02: // outer
			ct, err := readLEB128(r)
			if err != nil {
				return nil, fmt.Errorf("alias %d: read outer count: %w", i, err)
			}
			idx, err := readLEB128(r)
			if err != nil {
				return nil, fmt.Errorf("alias %d: read outer index: %w", i, err)
			}
			alias.OuterCount = ct
			alias.OuterIndex = idx

		default:
			return nil, fmt.Errorf("alias %d: unknown target kind: 0x%02x", i, targetKind)
		}

		aliases = append(aliases, alias)
	}

	return aliases, nil
}

func decodeImports(data []byte) ([]Import, error) {
	r := getReader(data)
	defer putReader(r)

	count, err := readLEB128(r)
	if err != nil {
		return nil, err
	}

	// Sanity check on count
	if count > 100000 {
		return nil, fmt.Errorf("import count %d exceeds maximum", count)
	}

	imports := make([]Import, 0, count)

	for i := uint32(0); i < count; i++ {
		nameKind, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("import %d: read name kind: %w", i, err)
		}

		nameLen, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("import %d: read name length: %w", i, err)
		}

		// Sanity check on name length
		if nameLen > 10000 {
			return nil, fmt.Errorf("import %d: name length %d exceeds maximum", i, nameLen)
		}

		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return nil, fmt.Errorf("import %d: read name: %w", i, err)
		}

		_ = nameKind // 0x01 = version embedded in name string

		externKind, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("import %d: read extern kind: %w", i, err)
		}

		if externKind == ExternCoreModule {
			extraByte, err := readByte(r)
			if err != nil {
				return nil, fmt.Errorf("import %d: read core module extra byte: %w", i, err)
			}
			if extraByte != 0x11 {
				return nil, fmt.Errorf("import %d: expected 0x11 after 0x00, got 0x%02x", i, extraByte)
			}
		}

		var typeIndex uint32
		if externKind == ExternType {
			boundsKind, err := readByte(r)
			if err != nil {
				return nil, fmt.Errorf("import %d: read type bounds kind: %w", i, err)
			}
			switch boundsKind {
			case 0x00:
				typeIndex, err = readLEB128(r)
				if err != nil {
					return nil, fmt.Errorf("import %d: read type bounds index: %w", i, err)
				}
			case 0x01:
				typeIndex = 0
			default:
				return nil, fmt.Errorf("import %d: unknown type bounds kind 0x%02x", i, boundsKind)
			}
		} else {
			typeIndex, err = readLEB128(r)
			if err != nil {
				return nil, fmt.Errorf("import %d: read type index: %w", i, err)
			}
		}

		imports = append(imports, Import{
			Name:       string(nameBytes),
			ExternKind: externKind,
			TypeIndex:  typeIndex,
		})
	}

	return imports, nil
}

func decodeExports(data []byte) ([]Export, error) {
	r := getReader(data)
	defer putReader(r)

	count, err := readLEB128(r)
	if err != nil {
		return nil, err
	}

	// Sanity check on count
	if count > 100000 {
		return nil, fmt.Errorf("export count %d exceeds maximum", count)
	}

	exports := make([]Export, 0, count)

	for i := uint32(0); i < count; i++ {
		nameKind, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("export %d: read name kind: %w", i, err)
		}

		nameLen, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("export %d: read name length: %w", i, err)
		}

		// Sanity check on name length
		if nameLen > 10000 {
			return nil, fmt.Errorf("export %d: name length %d exceeds maximum", i, nameLen)
		}

		nameBytes := make([]byte, nameLen)
		if _, err := io.ReadFull(r, nameBytes); err != nil {
			return nil, fmt.Errorf("export %d: read name: %w", i, err)
		}

		_ = nameKind

		sort, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("export %d: read sort: %w", i, err)
		}

		if sort == SortCore {
			_, err := readByte(r)
			if err != nil {
				return nil, fmt.Errorf("export %d: read core sort: %w", i, err)
			}
		}

		sortIndex, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("export %d: read sort index: %w", i, err)
		}

		exports = append(exports, Export{
			Name:      string(nameBytes),
			Sort:      sort,
			SortIndex: sortIndex,
		})
	}

	return exports, nil
}

func readLEB128(r io.Reader) (uint32, error) {
	var result uint32
	var shift uint
	for i := 0; i < 5; i++ { // Max 5 bytes for uint32
		b, err := readByte(r)
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 32 {
			return 0, fmt.Errorf("LEB128 value too large")
		}
	}
	return 0, fmt.Errorf("LEB128 encoding exceeded maximum length")
}

// readSLEB128 reads a signed LEB128 (var_s33 for type indices)
func readSLEB128(r io.Reader) (int32, error) {
	var result int32
	var shift uint
	var b byte

	for i := 0; i < 5; i++ { // Max 5 bytes for 33-bit value
		var err error
		b, err = readByte(r)
		if err != nil {
			return 0, err
		}

		result |= int32(b&0x7F) << shift
		shift += 7

		if b&0x80 == 0 {
			// Sign extend if the high bit of the last byte's payload is set
			if shift < 33 && (b&0x40) != 0 {
				result |= int32(-1) << shift
			}
			return result, nil
		}
	}

	return 0, fmt.Errorf("SLEB128 encoding exceeded maximum length")
}
