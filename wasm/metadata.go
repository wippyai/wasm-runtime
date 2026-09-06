package wasm

// ParseModuleMetadata parses WebAssembly module metadata (types, imports, functions, exports, etc.)
// while skipping code and data section payloads without copying or decoding function bodies.
// It checks their section extents, not their contents. This is metadata inspection,
// not executable-module validation; callers must still validate with their Wasm engine.
func ParseModuleMetadata(data []byte) (*Module, error) {
	return parseModule(data, true)
}
