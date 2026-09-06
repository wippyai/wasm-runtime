package component

import (
	"bytes"
	"fmt"
	"io"
)

// Canon kinds per Component Model binary format section 8
const (
	CanonLift              byte = 0x00 // Followed by 0x00 discriminant
	CanonLower             byte = 0x01 // Followed by 0x00 discriminant
	CanonResourceNew       byte = 0x02
	CanonResourceDrop      byte = 0x03
	CanonResourceRep       byte = 0x04
	CanonTaskCancel        byte = 0x05
	CanonSubtaskCancel     byte = 0x06
	CanonResourceDropAsync byte = 0x07
)

// CanonOption kinds per Component Model binary format
const (
	CanonOptUTF8         byte = 0x00
	CanonOptUTF16        byte = 0x01
	CanonOptCompactUTF16 byte = 0x02
	CanonOptMemory       byte = 0x03
	CanonOptRealloc      byte = 0x04
	CanonOptPostReturn   byte = 0x05
	CanonOptAsync        byte = 0x06
	CanonOptCallback     byte = 0x07
	CanonOptCoreType     byte = 0x08
	CanonOptGc           byte = 0x09
)

// CanonDef holds parsed canonical ABI operation data
type CanonDef struct {
	Options      []CanonOption
	RawData      []byte // Borrowed complete section bytes, shared by entries in a vector.
	FuncIndex    uint32
	TypeIndex    uint32
	ResourceType uint32
	Kind         byte
}

// CanonOption holds a single option from canon lift/lower
type CanonOption struct {
	Index    uint32
	Kind     byte
	Encoding byte
}

// ParseCanonSection parses a canonical section containing exactly one entry.
//
// Deprecated: Use ParseCanonSectionEntries to support canonical section vectors.
func ParseCanonSection(data []byte) (*CanonDef, error) {
	entries, err := ParseCanonSectionEntries(data)
	if err != nil {
		return nil, err
	}
	if len(entries) != 1 {
		return nil, fmt.Errorf("expected 1 canon in section, got %d", len(entries))
	}
	return entries[0], nil
}

// ParseCanonSectionEntries parses a Canon section (section 8).
// Binary.md: section_8(vec(<canon>)) — entries are returned in index order.
func ParseCanonSectionEntries(data []byte) ([]*CanonDef, error) {
	r := getReader(data)
	defer putReader(r)

	count, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read canon vec count: %w", err)
	}
	if uint64(count) > uint64(r.Len()) {
		return nil, fmt.Errorf("canon count %d exceeds remaining section bytes %d", count, r.Len())
	}

	canons := make([]*CanonDef, 0, count)
	for i := uint32(0); i < count; i++ {
		canon, err := parseCanonDef(r)
		if err != nil {
			return nil, fmt.Errorf("canon %d: %w", i, err)
		}
		canon.RawData = data
		canons = append(canons, canon)
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("canon section has %d trailing bytes", r.Len())
	}

	return canons, nil
}

func parseCanonDef(r *bytes.Reader) (*CanonDef, error) {
	kind, err := readByte(r)
	if err != nil {
		return nil, fmt.Errorf("read canon kind: %w", err)
	}

	canon := &CanonDef{Kind: kind}

	switch kind {
	case CanonLift:
		// lift: 0x00 0x00 core_func:u32 opts:vec(canonopt) type:u32
		subKind, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("read lift sub-kind: %w", err)
		}
		if subKind != 0x00 {
			return nil, fmt.Errorf("unknown lift sub-kind: 0x%02x", subKind)
		}

		funcIdx, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read core func index: %w", err)
		}
		canon.FuncIndex = funcIdx

		opts, err := readCanonOptions(r)
		if err != nil {
			return nil, fmt.Errorf("read options: %w", err)
		}
		canon.Options = opts

		typeIdx, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read type index: %w", err)
		}
		canon.TypeIndex = typeIdx

	case CanonLower:
		// lower: 0x01 0x00 func:u32 opts:vec(canonopt)
		subKind, err := readByte(r)
		if err != nil {
			return nil, fmt.Errorf("read lower sub-kind: %w", err)
		}
		if subKind != 0x00 {
			return nil, fmt.Errorf("unknown lower sub-kind: 0x%02x", subKind)
		}

		funcIdx, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read func index: %w", err)
		}
		canon.FuncIndex = funcIdx

		opts, err := readCanonOptions(r)
		if err != nil {
			return nil, fmt.Errorf("read options: %w", err)
		}
		canon.Options = opts

	case CanonResourceNew, CanonResourceDrop, CanonResourceRep, CanonResourceDropAsync:
		resourceType, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read resource type: %w", err)
		}
		canon.ResourceType = resourceType

	case CanonTaskCancel, CanonSubtaskCancel:
		// No additional data for async task operations

	default:
		return nil, fmt.Errorf("unknown canon kind: 0x%02x", kind)
	}

	return canon, nil
}

func readCanonOptions(r *bytes.Reader) ([]CanonOption, error) {
	count, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read option count: %w", err)
	}

	// Every option consumes at least one byte. Reject forged vector counts
	// before they can cause a large allocation from a tiny section.
	if uint64(count) > uint64(r.Len()) {
		return nil, fmt.Errorf("option count %d exceeds remaining section bytes %d", count, r.Len())
	}
	opts := make([]CanonOption, 0, count)
	for i := uint32(0); i < count; i++ {
		opt, err := readCanonOption(r)
		if err != nil {
			return nil, fmt.Errorf("read option %d: %w", i, err)
		}
		opts = append(opts, opt)
	}

	return opts, nil
}

func readCanonOption(r io.Reader) (CanonOption, error) {
	kind, err := readByte(r)
	if err != nil {
		return CanonOption{}, fmt.Errorf("read option kind: %w", err)
	}

	opt := CanonOption{Kind: kind}

	switch kind {
	case CanonOptUTF8:
		opt.Encoding = 0
	case CanonOptUTF16:
		opt.Encoding = 1
	case CanonOptCompactUTF16:
		opt.Encoding = 2

	case CanonOptMemory, CanonOptRealloc, CanonOptPostReturn, CanonOptCallback, CanonOptCoreType:
		// Read index
		idx, err := readLEB128(r)
		if err != nil {
			return CanonOption{}, fmt.Errorf("read option index: %w", err)
		}
		opt.Index = idx

	case CanonOptAsync, CanonOptGc:
		// No additional data

	default:
		return CanonOption{}, fmt.Errorf("unknown canon option kind: 0x%02x", kind)
	}

	return opt, nil
}

// GetMemoryIndex returns the memory index, defaulting to 0
func (c *CanonDef) GetMemoryIndex() uint32 {
	for _, opt := range c.Options {
		if opt.Kind == CanonOptMemory {
			return opt.Index
		}
	}
	return 0 // Default to memory 0
}

// GetReallocIndex returns the realloc function index, or -1 if unspecified
func (c *CanonDef) GetReallocIndex() int32 {
	for _, opt := range c.Options {
		if opt.Kind == CanonOptRealloc {
			return int32(opt.Index)
		}
	}
	return -1 // No realloc specified
}

// GetStringEncoding returns 0=UTF8, 1=UTF16, 2=CompactUTF16. Defaults to UTF8.
func (c *CanonDef) GetStringEncoding() byte {
	for _, opt := range c.Options {
		switch opt.Kind {
		case CanonOptUTF8:
			return 0
		case CanonOptUTF16:
			return 1
		case CanonOptCompactUTF16:
			return 2
		}
	}
	return 0 // Default to UTF-8
}
