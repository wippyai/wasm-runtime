// Package wasm provides low-level WebAssembly binary encoding and parsing utilities.
//
// This package handles the binary-level details of WebAssembly modules, including
// LEB128 encoding, section parsing, and synthetic module generation for the linker.
//
// # Encoding
//
// LEB128 (Little Endian Base 128) encoding for WebAssembly integers:
//
//	encoded := wasm.EncodeULEB128(300)   // unsigned
//	encoded := wasm.EncodeSLEB128(-100)  // signed
//
// # Parsing
//
// Extract sections from WebAssembly binaries:
//
//	section := wasm.FindExportSection(wasmBytes)
//	sigs := wasm.ParseTypeSection(typeBytes)
//
// # Rewriting
//
// Modify import sections for component model requirements:
//
//	modified := wasm.RewriteImportSection(wasmBytes, renames)
//
// # Synthetic Modules
//
// Generate minimal WASM modules for bridging:
//
//	mod := wasm.SynthModule(imports, exports)
//
// This package is internal to the linker and should not be used directly.
package wasm
