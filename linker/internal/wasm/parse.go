package wasm

import (
	"github.com/tetratelabs/wazero/api"
)

// GlobalExport represents an exported global from a WASM module.
type GlobalExport struct {
	Name         string
	ImportModule string
	ImportName   string
	ValType      api.ValueType
	Mutable      bool
	IsImport     bool
}

// GlobalImport represents a global that a module imports.
type GlobalImport struct {
	ModuleName string
	ImportName string
	ValType    api.ValueType
	Mutable    bool
}

type globalInfo struct {
	importModule string
	importName   string
	valType      api.ValueType
	mutable      bool
	isImport     bool
}

// ParseGlobalExports extracts global exports from raw WASM bytes.
func ParseGlobalExports(wasmBytes []byte) []GlobalExport {
	if len(wasmBytes) < 8 {
		return nil
	}

	pos := 8
	var importSectionStart, importSectionEnd int
	var globalSectionStart, globalSectionEnd int
	var exportSectionStart, exportSectionEnd int

	for pos < len(wasmBytes) {
		sectionID := wasmBytes[pos]
		pos++
		sectionSize, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		sectionEnd := pos + int(sectionSize)

		switch sectionID {
		case 0x02:
			importSectionStart = pos
			importSectionEnd = sectionEnd
		case 0x06:
			globalSectionStart = pos
			globalSectionEnd = sectionEnd
		case 0x07:
			exportSectionStart = pos
			exportSectionEnd = sectionEnd
		}
		pos = sectionEnd
	}

	var globalSpace []globalInfo

	// Parse imported globals
	if importSectionStart > 0 {
		pos := importSectionStart
		count, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		for i := uint32(0); i < count && pos < importSectionEnd; i++ {
			modLen, n := DecodeULEB128(wasmBytes[pos:])
			pos += n
			modName := string(wasmBytes[pos : pos+int(modLen)])
			pos += int(modLen)
			nameLen, n := DecodeULEB128(wasmBytes[pos:])
			pos += n
			impName := string(wasmBytes[pos : pos+int(nameLen)])
			pos += int(nameLen)
			kind := wasmBytes[pos]
			pos++

			switch kind {
			case 0x00:
				_, n := DecodeULEB128(wasmBytes[pos:])
				pos += n
			case 0x01:
				pos++
				pos++
				_, n := DecodeULEB128(wasmBytes[pos:])
				pos += n
				if wasmBytes[pos-n-1]&0x01 != 0 {
					_, n := DecodeULEB128(wasmBytes[pos:])
					pos += n
				}
			case 0x02:
				pos++
				_, n := DecodeULEB128(wasmBytes[pos:])
				pos += n
				if wasmBytes[pos-n-1]&0x01 != 0 {
					_, n := DecodeULEB128(wasmBytes[pos:])
					pos += n
				}
			case 0x03:
				valType := ParseValType(wasmBytes[pos])
				pos++
				mutable := wasmBytes[pos] == 0x01
				pos++
				globalSpace = append(globalSpace, globalInfo{
					valType:      valType,
					mutable:      mutable,
					isImport:     true,
					importModule: modName,
					importName:   impName,
				})
			}
		}
	}

	// Parse local globals
	if globalSectionStart > 0 {
		pos := globalSectionStart
		count, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		for i := uint32(0); i < count && pos < globalSectionEnd; i++ {
			valType := ParseValType(wasmBytes[pos])
			pos++
			mutable := wasmBytes[pos] == 0x01
			pos++
			globalSpace = append(globalSpace, globalInfo{
				valType: valType,
				mutable: mutable,
			})
			for pos < globalSectionEnd && wasmBytes[pos] != 0x0B {
				pos++
			}
			pos++
		}
	}

	// Parse exports
	var globals []GlobalExport
	if exportSectionStart > 0 {
		pos := exportSectionStart
		count, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		for i := uint32(0); i < count && pos < exportSectionEnd; i++ {
			nameLen, n := DecodeULEB128(wasmBytes[pos:])
			pos += n
			name := string(wasmBytes[pos : pos+int(nameLen)])
			pos += int(nameLen)
			kind := wasmBytes[pos]
			pos++
			idx, n := DecodeULEB128(wasmBytes[pos:])
			pos += n

			if kind == 0x03 {
				if int(idx) < len(globalSpace) {
					g := globalSpace[idx]
					globals = append(globals, GlobalExport{
						Name:         name,
						ValType:      g.valType,
						Mutable:      g.mutable,
						IsImport:     g.isImport,
						ImportModule: g.importModule,
						ImportName:   g.importName,
					})
				}
			}
		}
	}

	return globals
}

// ParseGlobalImports extracts global imports from raw WASM bytes.
func ParseGlobalImports(wasmBytes []byte) []GlobalImport {
	if len(wasmBytes) < 8 {
		return nil
	}

	pos := 8
	for pos < len(wasmBytes) {
		sectionID := wasmBytes[pos]
		pos++
		sectionSize, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		sectionEnd := pos + int(sectionSize)

		if sectionID != 0x02 {
			pos = sectionEnd
			continue
		}

		var globals []GlobalImport
		count, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		for i := uint32(0); i < count && pos < sectionEnd; i++ {
			modLen, n := DecodeULEB128(wasmBytes[pos:])
			pos += n
			modName := string(wasmBytes[pos : pos+int(modLen)])
			pos += int(modLen)
			nameLen, n := DecodeULEB128(wasmBytes[pos:])
			pos += n
			impName := string(wasmBytes[pos : pos+int(nameLen)])
			pos += int(nameLen)
			kind := wasmBytes[pos]
			pos++

			switch kind {
			case 0x00:
				_, n := DecodeULEB128(wasmBytes[pos:])
				pos += n
			case 0x01:
				pos++
				flag := wasmBytes[pos]
				pos++
				_, n := DecodeULEB128(wasmBytes[pos:])
				pos += n
				if flag&0x01 != 0 {
					_, n := DecodeULEB128(wasmBytes[pos:])
					pos += n
				}
			case 0x02:
				flag := wasmBytes[pos]
				pos++
				_, n := DecodeULEB128(wasmBytes[pos:])
				pos += n
				if flag&0x01 != 0 {
					_, n := DecodeULEB128(wasmBytes[pos:])
					pos += n
				}
			case 0x03:
				valType := ParseValType(wasmBytes[pos])
				pos++
				mutable := wasmBytes[pos] == 0x01
				pos++
				globals = append(globals, GlobalImport{
					ModuleName: modName,
					ImportName: impName,
					ValType:    valType,
					Mutable:    mutable,
				})
			}
		}
		return globals
	}

	return nil
}

// ParseTableExports extracts table export names from raw WASM bytes.
func ParseTableExports(wasmBytes []byte) []string {
	if len(wasmBytes) < 8 {
		return nil
	}

	pos := 8
	for pos < len(wasmBytes) {
		sectionID := wasmBytes[pos]
		pos++
		sectionSize, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		sectionEnd := pos + int(sectionSize)

		if sectionID != 0x07 {
			pos = sectionEnd
			continue
		}

		var tables []string
		count, n := DecodeULEB128(wasmBytes[pos:])
		pos += n
		for i := uint32(0); i < count && pos < sectionEnd; i++ {
			nameLen, n := DecodeULEB128(wasmBytes[pos:])
			pos += n
			name := string(wasmBytes[pos : pos+int(nameLen)])
			pos += int(nameLen)
			kind := wasmBytes[pos]
			pos++
			_, n = DecodeULEB128(wasmBytes[pos:])
			pos += n

			if kind == 0x01 {
				tables = append(tables, name)
			}
		}
		return tables
	}

	return nil
}
