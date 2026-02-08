package component

import (
	"bytes"
	"fmt"
	"io"
)

// ParsedCoreInstance holds a parsed core instance from section 2
type ParsedCoreInstance struct {
	Args        []CoreInstanceArg
	Exports     []CoreInstanceExport
	ModuleIndex uint32
	Kind        CoreInstanceKind
}

type CoreInstanceKind byte

const (
	CoreInstanceInstantiate CoreInstanceKind = 0x00
	CoreInstanceFromExports CoreInstanceKind = 0x01
)

// CoreInstanceArg holds an argument for module instantiation
type CoreInstanceArg struct {
	Name          string
	Kind          CoreInstantiateKind
	InstanceIndex uint32 // For instance arguments
}

type CoreInstantiateKind byte

const (
	CoreInstantiateInstance CoreInstantiateKind = 0x12 // instance reference
)

// CoreInstanceExport holds an export in a FromExports instance
type CoreInstanceExport struct {
	Name  string
	Kind  byte
	Index uint32
}

// CoreExport kind constants
const (
	CoreExportFunc   byte = 0x00
	CoreExportTable  byte = 0x01
	CoreExportMemory byte = 0x02
	CoreExportGlobal byte = 0x03
)

// ParseCoreInstanceSection parses section 2 containing vec(instance)
func ParseCoreInstanceSection(data []byte) ([]*ParsedCoreInstance, error) {
	r := bytes.NewReader(data)

	count, err := readLEB128(r)
	if err != nil {
		return nil, fmt.Errorf("read instance count: %w", err)
	}

	instances := make([]*ParsedCoreInstance, count)
	for i := uint32(0); i < count; i++ {
		inst, err := parseSingleCoreInstance(r)
		if err != nil {
			return nil, fmt.Errorf("parse instance %d: %w", i, err)
		}
		instances[i] = inst
	}

	return instances, nil
}

func parseSingleCoreInstance(r *bytes.Reader) (*ParsedCoreInstance, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read kind: %w", err)
	}

	inst := &ParsedCoreInstance{
		Kind: CoreInstanceKind(kind),
	}

	switch inst.Kind {
	case CoreInstanceInstantiate:
		// instantiate: module-index:u32 args:vec<(name, instanceidx)>
		modIdx, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read module index: %w", err)
		}
		inst.ModuleIndex = modIdx

		argCount, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read arg count: %w", err)
		}

		inst.Args = make([]CoreInstanceArg, argCount)
		for i := uint32(0); i < argCount; i++ {
			name, err := readName(r)
			if err != nil {
				return nil, fmt.Errorf("read arg %d name: %w", i, err)
			}

			argKind, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read arg %d kind: %w", i, err)
			}

			instIdx, err := readLEB128(r)
			if err != nil {
				return nil, fmt.Errorf("read arg %d instance: %w", i, err)
			}

			inst.Args[i] = CoreInstanceArg{
				Name:          name,
				Kind:          CoreInstantiateKind(argKind),
				InstanceIndex: instIdx,
			}
		}

	case CoreInstanceFromExports:
		// from-exports: vec<(name, sortidx)>
		exportCount, err := readLEB128(r)
		if err != nil {
			return nil, fmt.Errorf("read export count: %w", err)
		}

		inst.Exports = make([]CoreInstanceExport, exportCount)
		for i := uint32(0); i < exportCount; i++ {
			name, err := readName(r)
			if err != nil {
				return nil, fmt.Errorf("read export %d name: %w", i, err)
			}

			sortKind, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read export %d kind: %w", i, err)
			}

			idx, err := readLEB128(r)
			if err != nil {
				return nil, fmt.Errorf("read export %d index: %w", i, err)
			}

			inst.Exports[i] = CoreInstanceExport{
				Name:  name,
				Kind:  sortKind,
				Index: idx,
			}
		}

	default:
		return nil, fmt.Errorf("unknown core instance kind: %d", kind)
	}

	return inst, nil
}

// readName reads a LEB128 length-prefixed UTF-8 string
func readName(r io.Reader) (string, error) {
	length, err := readLEB128(r)
	if err != nil {
		return "", err
	}

	if length > maxNameLength {
		return "", fmt.Errorf("name too long: %d (max %d)", length, maxNameLength)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}

	return string(buf), nil
}
