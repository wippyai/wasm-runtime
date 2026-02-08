package encoder

import (
	"github.com/wippyai/wasm-runtime/wat/internal/ast"
)

func Encode(m *ast.Module) []byte {
	buf := &Buffer{}

	buf.WriteBytes([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}) // magic + version

	if len(m.Types) > 0 {
		encodeTypeSection(buf, m)
	}
	if len(m.Imports) > 0 {
		encodeImportSection(buf, m)
	}
	if len(m.Funcs) > 0 {
		encodeFuncSection(buf, m)
	}
	if len(m.Tables) > 0 {
		encodeTableSection(buf, m)
	}
	if len(m.Memories) > 0 {
		encodeMemorySection(buf, m)
	}
	if len(m.Globals) > 0 {
		encodeGlobalSection(buf, m)
	}
	if len(m.Exports) > 0 {
		encodeExportSection(buf, m)
	}
	if m.Start != nil {
		encodeStartSection(buf, m)
	}
	if len(m.Elems) > 0 {
		encodeElemSection(buf, m)
	}
	// DataCount must precede Code when passive data segments exist
	if hasPassiveData(m) && len(m.Code) > 0 {
		encodeDataCountSection(buf, m)
	}
	if len(m.Code) > 0 {
		encodeCodeSection(buf, m)
	}
	if len(m.Data) > 0 {
		encodeDataSection(buf, m)
	}

	return buf.Bytes
}

func hasPassiveData(m *ast.Module) bool {
	for _, d := range m.Data {
		if d.Passive {
			return true
		}
	}
	return false
}
