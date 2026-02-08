// Package wasm provides WebAssembly binary format parsing and encoding.
//
// This package implements a complete parser and encoder for WebAssembly
// binary modules according to the WebAssembly 2.0 specification, with
// support for several post-2.0 proposals.
//
// # Supported Features
//
//	WebAssembly 2.0:
//	  - Core value types (i32, i64, f32, f64)
//	  - Functions, tables, memories, globals
//	  - Control flow, calls, local/global access
//	  - Memory and table operations
//	  - Import/export of all definitions
//
//	Post-2.0 Proposals:
//	  - GC (structs, arrays, typed references, heap types)
//	  - Exception handling (tags, try/catch, throw)
//	  - Tail calls (return_call, return_call_indirect)
//	  - SIMD (128-bit vector operations, v128 type)
//	  - Threads (atomic operations, shared memory)
//	  - Bulk memory (memory.copy, memory.fill, data.drop)
//	  - Reference types (funcref, externref, ref.null, ref.is_null)
//	  - Multi-memory (multiple memory instances)
//	  - Memory64 (64-bit memory addressing)
//
// # Parsing
//
// Parse a WebAssembly module from binary:
//
//	data, _ := os.ReadFile("module.wasm")
//	module, err := wasm.ParseModule(data)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// Parse with validation enabled:
//
//	module, err := wasm.ParseModuleValidate(data)
//
// # Encoding
//
// Encode a module back to binary:
//
//	encoded := module.Encode()
//
// Round-trip parsing and encoding preserves module semantics:
//
//	original, _ := wasm.ParseModule(data)
//	roundtrip, _ := wasm.ParseModule(original.Encode())
//	// original and roundtrip are semantically equivalent
//
// # Module Structure
//
// A parsed module contains all sections:
//
//	module.Types      []FuncType    // Function signatures
//	module.Funcs      []uint32      // Type indices for functions
//	module.Tables     []TableType   // Table definitions
//	module.Memories   []MemoryType  // Memory definitions
//	module.Globals    []Global      // Global definitions
//	module.Imports    []Import      // Imported definitions
//	module.Exports    []Export      // Exported definitions
//	module.Code       []FuncBody    // Function bodies
//	module.Data       []DataSegment // Data segments
//	module.Elements   []Element     // Element segments
//
// # Instructions
//
// Decode instructions from bytecode:
//
//	instructions, err := wasm.DecodeInstructions(code)
//	for _, instr := range instructions {
//	    fmt.Printf("%s\n", instr.Opcode)
//	}
//
// Encode instructions back to bytecode:
//
//	encoded := wasm.EncodeInstructions(instructions)
//
// # Validation
//
// Validate module structure:
//
//	if err := module.Validate(); err != nil {
//	    log.Printf("invalid module: %v", err)
//	}
//
// Validation checks:
//   - Type indices are in bounds
//   - Function signatures match
//   - Import/export names are valid UTF-8
//   - Table and memory limits are valid
//   - Instructions are well-formed
//
// # LEB128 Encoding
//
// The package provides LEB128 utilities used throughout:
//
//	n, bytesRead := wasm.ReadLEB128u(data)  // Unsigned
//	n, bytesRead := wasm.ReadLEB128s(data)  // Signed
package wasm
