package linker

import (
	internalwasm "github.com/wippyai/wasm-runtime/linker/internal/wasm"
)

// EmptyModuleName is the sentinel used to replace empty module names in imports.
const EmptyModuleName = internalwasm.EmptyModuleName

// EmptyFieldName is the sentinel used to replace empty import field names and the
// matching host-module export names (see internal/wasm.RewriteEmptyModuleNames).
const EmptyFieldName = internalwasm.EmptyFieldName

// RootModuleName is the sentinel module name for the reactor start-shim's ("", "")
// import, kept distinct from EmptyModuleName (see internal/wasm.RootModuleName).
const RootModuleName = internalwasm.RootModuleName

// exportName maps an empty export name to EmptyFieldName so it stays consistent
// with the rewritten guest import; non-empty names pass through unchanged.
func exportName(name string) string {
	if name == "" {
		return EmptyFieldName
	}
	return name
}

// rewriteEmptyModuleNames delegates to internal/wasm.
func rewriteEmptyModuleNames(wasm []byte) []byte {
	return internalwasm.RewriteEmptyModuleNames(wasm)
}
