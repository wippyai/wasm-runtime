package wasm

// EmptyModuleName is the sentinel used to replace empty module names in imports.
const EmptyModuleName = "$"

// EmptyFieldName is the sentinel used to replace empty import field names. The
// wit-component reactor start-shim imports its target as ("", ""); wazero cannot
// resolve an empty-named function import, so both the import field and the
// matching host-module export are rewritten to this sentinel.
const EmptyFieldName = "$root-export$"

// RootModuleName is the sentinel module name for the reactor start-shim's single
// ("", "") import. It is kept distinct from EmptyModuleName so the start-shim's
// _initialize bridge does not collide with the wit-component indirect-call shim,
// which also uses an empty module name but with non-empty ("0".."N") fields.
const RootModuleName = "$root$"

// RewriteEmptyModuleNames rewrites WASM module bytes to replace empty module
// names in imports with EmptyModuleName and empty import field names with
// EmptyFieldName. Returns original bytes if no rewrite needed.
func RewriteEmptyModuleNames(wasm []byte) []byte {
	if len(wasm) < 8 {
		return wasm
	}

	if !hasEmptyImportName(wasm) {
		return wasm
	}

	idx := 8
	result := make([]byte, 0, len(wasm)+16)
	result = append(result, wasm[:idx]...)

	for idx < len(wasm) {
		sectionID := wasm[idx]
		idx++

		sectionSize, n := DecodeULEB128(wasm[idx:])
		sectionSizeBytes := wasm[idx : idx+n]
		idx += n

		sectionStart := idx
		sectionEnd := idx + int(sectionSize)
		if sectionEnd > len(wasm) {
			sectionEnd = len(wasm)
		}

		if sectionID == 0x02 {
			rewrittenSection := rewriteImportSection(wasm[sectionStart:sectionEnd])
			result = append(result, sectionID)
			result = append(result, EncodeULEB128(uint32(len(rewrittenSection)))...)
			result = append(result, rewrittenSection...)
		} else {
			result = append(result, sectionID)
			result = append(result, sectionSizeBytes...)
			result = append(result, wasm[sectionStart:sectionEnd]...)
		}
		idx = sectionEnd
	}

	return result
}

func hasEmptyImportName(wasm []byte) bool {
	idx := 8
	for idx < len(wasm) {
		sectionID := wasm[idx]
		idx++

		sectionSize, n := DecodeULEB128(wasm[idx:])
		idx += n

		if sectionID == 0x02 {
			end := idx + int(sectionSize)
			if end > len(wasm) {
				end = len(wasm)
			}
			return importSectionHasEmptyName(wasm[idx:end])
		}
		idx += int(sectionSize)
	}
	return false
}

func importSectionHasEmptyName(section []byte) bool {
	idx := 0
	numImports, n := DecodeULEB128(section[idx:])
	idx += n

	for i := uint32(0); i < numImports; i++ {
		modNameLen, n := DecodeULEB128(section[idx:])
		idx += n + int(modNameLen)

		importNameLen, n := DecodeULEB128(section[idx:])
		idx += n + int(importNameLen)

		if modNameLen == 0 || importNameLen == 0 {
			return true
		}

		kind := section[idx]
		idx++
		switch kind {
		case 0x00:
			_, n := DecodeULEB128(section[idx:])
			idx += n
		case 0x01:
			idx++
			hasMax := section[idx]
			idx++
			_, n := DecodeULEB128(section[idx:])
			idx += n
			if hasMax&0x01 != 0 {
				_, n := DecodeULEB128(section[idx:])
				idx += n
			}
		case 0x02:
			hasMax := section[idx]
			idx++
			_, n := DecodeULEB128(section[idx:])
			idx += n
			if hasMax&0x01 != 0 {
				_, n := DecodeULEB128(section[idx:])
				idx += n
			}
		case 0x03:
			idx += 2
		}
	}
	return false
}

func rewriteImportSection(section []byte) []byte {
	result := make([]byte, 0, len(section)+16)
	idx := 0

	numImports, n := DecodeULEB128(section[idx:])
	result = append(result, section[idx:idx+n]...)
	idx += n

	emitName := func(name string) {
		result = append(result, EncodeULEB128(uint32(len(name)))...)
		result = append(result, name...)
	}

	for i := uint32(0); i < numImports; i++ {
		modNameLen, n := DecodeULEB128(section[idx:])
		idx += n
		modName := section[idx : idx+int(modNameLen)]
		idx += int(modNameLen)

		importNameLen, n := DecodeULEB128(section[idx:])
		idx += n
		importName := section[idx : idx+int(importNameLen)]
		idx += int(importNameLen)

		// The reactor start-shim's single ("", "") import is routed to a distinct
		// RootModuleName so its bridge never collides with the indirect-call shim,
		// which uses an empty module name with non-empty fields.
		switch {
		case modNameLen == 0 && importNameLen == 0:
			emitName(RootModuleName)
			emitName(EmptyFieldName)
		case modNameLen == 0:
			emitName(EmptyModuleName)
			emitName(string(importName))
		default:
			emitName(string(modName))
			if importNameLen == 0 {
				emitName(EmptyFieldName)
			} else {
				emitName(string(importName))
			}
		}

		kind := section[idx]
		result = append(result, kind)
		idx++

		switch kind {
		case 0x00:
			_, n := DecodeULEB128(section[idx:])
			result = append(result, section[idx:idx+n]...)
			idx += n
		case 0x01:
			result = append(result, section[idx])
			idx++
			hasMax := section[idx]
			result = append(result, hasMax)
			idx++
			_, n := DecodeULEB128(section[idx:])
			result = append(result, section[idx:idx+n]...)
			idx += n
			if hasMax&0x01 != 0 {
				_, n := DecodeULEB128(section[idx:])
				result = append(result, section[idx:idx+n]...)
				idx += n
			}
		case 0x02:
			hasMax := section[idx]
			result = append(result, hasMax)
			idx++
			_, n := DecodeULEB128(section[idx:])
			result = append(result, section[idx:idx+n]...)
			idx += n
			if hasMax&0x01 != 0 {
				_, n := DecodeULEB128(section[idx:])
				result = append(result, section[idx:idx+n]...)
				idx += n
			}
		case 0x03:
			result = append(result, section[idx:idx+2]...)
			idx += 2
		}
	}

	return result
}
