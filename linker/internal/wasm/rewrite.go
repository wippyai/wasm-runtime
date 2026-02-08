package wasm

// EmptyModuleName is the sentinel used to replace empty module names in imports.
const EmptyModuleName = "$"

// RewriteEmptyModuleNames rewrites WASM module bytes to replace empty module
// names in imports with EmptyModuleName. Returns original bytes if no rewrite needed.
func RewriteEmptyModuleNames(wasm []byte) []byte {
	if len(wasm) < 8 {
		return wasm
	}

	if !hasEmptyModuleNameImport(wasm) {
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

func hasEmptyModuleNameImport(wasm []byte) bool {
	idx := 8
	for idx < len(wasm) {
		sectionID := wasm[idx]
		idx++

		sectionSize, n := DecodeULEB128(wasm[idx:])
		idx += n

		if sectionID == 0x02 {
			sectionStart := idx
			numImports, n := DecodeULEB128(wasm[sectionStart:])
			if numImports > 0 {
				modNameLen, _ := DecodeULEB128(wasm[sectionStart+n:])
				return modNameLen == 0
			}
			return false
		}
		idx += int(sectionSize)
	}
	return false
}

func rewriteImportSection(section []byte) []byte {
	result := make([]byte, 0, len(section)+16)
	idx := 0

	numImports, n := DecodeULEB128(section[idx:])
	result = append(result, section[idx:idx+n]...)
	idx += n

	for i := uint32(0); i < numImports; i++ {
		modNameLen, n := DecodeULEB128(section[idx:])
		idx += n
		modName := section[idx : idx+int(modNameLen)]
		idx += int(modNameLen)

		if modNameLen == 0 {
			result = append(result, 0x01, '$')
		} else {
			result = append(result, EncodeULEB128(modNameLen)...)
			result = append(result, modName...)
		}

		importNameLen, n := DecodeULEB128(section[idx:])
		result = append(result, section[idx:idx+n]...)
		idx += n
		importName := section[idx : idx+int(importNameLen)]
		result = append(result, importName...)
		idx += int(importNameLen)

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
