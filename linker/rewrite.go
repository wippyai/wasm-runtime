package linker

import (
	internalwasm "github.com/wippyai/wasm-runtime/linker/internal/wasm"
)

// EmptyModuleName is the sentinel used to replace empty module names in imports.
const EmptyModuleName = internalwasm.EmptyModuleName

// rewriteEmptyModuleNames delegates to internal/wasm.
func rewriteEmptyModuleNames(wasm []byte) []byte {
	return internalwasm.RewriteEmptyModuleNames(wasm)
}
