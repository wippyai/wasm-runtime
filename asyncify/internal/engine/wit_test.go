package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TestWITCanonicalABI_Types verifies that all WIT Canonical ABI lowered types
// are compatible with asyncify. The Component Model lowers all complex types
// to primitives before crossing component boundaries.
func TestWITCanonicalABI_Types(t *testing.T) {
	tests := []struct {
		name    string
		witType string
		params  []wasm.ValType
		results []wasm.ValType
	}{
		// Primitive types
		{"bool", "bool", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"s8", "s8", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"u8", "u8", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"s16", "s16", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"u16", "u16", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"s32", "s32", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"u32", "u32", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"s64", "s64", []wasm.ValType{wasm.ValI64}, []wasm.ValType{wasm.ValI64}},
		{"u64", "u64", []wasm.ValType{wasm.ValI64}, []wasm.ValType{wasm.ValI64}},
		{"f32", "f32", []wasm.ValType{wasm.ValF32}, []wasm.ValType{wasm.ValF32}},
		{"f64", "f64", []wasm.ValType{wasm.ValF64}, []wasm.ValType{wasm.ValF64}},
		{"char", "char", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},

		// String: lowered to (ptr: i32, len: i32)
		{"string param", "string", []wasm.ValType{wasm.ValI32, wasm.ValI32}, nil},
		{"string result", "string", nil, []wasm.ValType{wasm.ValI32}}, // retptr

		// List<T>: lowered to (ptr: i32, len: i32)
		{"list<u8>", "list<u8>", []wasm.ValType{wasm.ValI32, wasm.ValI32}, nil},
		{"list<u32>", "list<u32>", []wasm.ValType{wasm.ValI32, wasm.ValI32}, nil},

		// Option<T>: discriminant (i32) + optional payload
		{"option<u32>", "option<u32>", []wasm.ValType{wasm.ValI32, wasm.ValI32}, nil},
		{"option<string>", "option<string>", []wasm.ValType{wasm.ValI32, wasm.ValI32, wasm.ValI32}, nil},

		// Result<T, E>: discriminant (i32) + union payload
		{"result<u32, string>", "result<u32, string>", []wasm.ValType{wasm.ValI32, wasm.ValI32, wasm.ValI32}, nil},
		{"result<_, error>", "result<_, error>", nil, []wasm.ValType{wasm.ValI32}}, // retptr

		// Tuple: flattened members
		{"tuple<u32, u64>", "tuple<u32, u64>", []wasm.ValType{wasm.ValI32, wasm.ValI64}, nil},
		{"tuple<f32, f64, i32>", "tuple<f32, f64, i32>", []wasm.ValType{wasm.ValF32, wasm.ValF64, wasm.ValI32}, nil},

		// Record: flattened fields
		{"record{a: u32, b: u64}", "record", []wasm.ValType{wasm.ValI32, wasm.ValI64}, nil},
		{"record{x: f32, y: f32, z: f32}", "record", []wasm.ValType{wasm.ValF32, wasm.ValF32, wasm.ValF32}, nil},

		// Enum: discriminant only (i32)
		{"enum{a, b, c}", "enum", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},

		// Flags: bitfield (i32 or i64 depending on count)
		{"flags<8>", "flags", []wasm.ValType{wasm.ValI32}, []wasm.ValType{wasm.ValI32}},
		{"flags<64>", "flags", []wasm.ValType{wasm.ValI64}, []wasm.ValType{wasm.ValI64}},

		// Resource handles: u32 index
		{"own<file>", "own<file>", []wasm.ValType{wasm.ValI32}, nil},
		{"borrow<file>", "borrow<file>", []wasm.ValType{wasm.ValI32}, nil},

		// Complex nested types (all flatten to primitives)
		{"list<record{a: u32, b: string}>", "list<record>", []wasm.ValType{wasm.ValI32, wasm.ValI32}, nil},
		{"option<result<u32, string>>", "option<result>", []wasm.ValType{wasm.ValI32, wasm.ValI32, wasm.ValI32, wasm.ValI32}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify all param types can be stored
			for i, vt := range tt.params {
				if !CanStoreToMemory(vt) {
					t.Errorf("WIT %s param[%d] type %s cannot be stored to memory", tt.witType, i, vt)
				}
			}

			// Verify all result types can be stored
			for i, vt := range tt.results {
				if !CanStoreToMemory(vt) {
					t.Errorf("WIT %s result[%d] type %s cannot be stored to memory", tt.witType, i, vt)
				}
			}

			// Build a module with this function signature and verify transform works
			m := buildWITTestModule(tt.params, tt.results)
			eng := New(Config{Matcher: newExactMatcher([]string{"host.async_func"})})
			result, err := eng.Transform(m.Encode())
			if err != nil {
				t.Fatalf("Transform failed for WIT %s: %v", tt.witType, err)
			}
			if len(result) == 0 {
				t.Errorf("Transform returned empty result for WIT %s", tt.witType)
			}
		})
	}
}

// buildWITTestModule creates a test module with given function signature
func buildWITTestModule(params, results []wasm.ValType) *wasm.Module {
	// Type for the async import
	asyncType := wasm.FuncType{
		Params:  params,
		Results: results,
	}

	// Type for wrapper function
	wrapperType := wasm.FuncType{
		Params:  params,
		Results: results,
	}

	// Build call instruction args
	var callArgs []wasm.Instruction
	for i := range params {
		callArgs = append(callArgs, wasm.Instruction{
			Opcode: wasm.OpLocalGet,
			Imm:    wasm.LocalImm{LocalIdx: uint32(i)},
		})
	}
	callArgs = append(callArgs, wasm.Instruction{
		Opcode: wasm.OpCall,
		Imm:    wasm.CallImm{FuncIdx: 0},
	})
	callArgs = append(callArgs, wasm.Instruction{Opcode: wasm.OpEnd})

	return &wasm.Module{
		Types: []wasm.FuncType{asyncType, wrapperType},
		Imports: []wasm.Import{
			{Module: "host", Name: "async_func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions(callArgs)},
		},
	}
}

// TestWITModuleMatching tests matching WIT-style module names
func TestWITModuleMatching(t *testing.T) {
	// Test that WIT module names work with exact matcher
	tests := []struct {
		module   string
		name     string
		patterns []string
		expected bool
	}{
		// Exact match
		{"wasi:filesystem/types@0.2.0", "read", []string{"wasi:filesystem/types@0.2.0.read"}, true},
		{"wasi:clocks/monotonic-clock@0.2.0", "subscribe-duration", []string{"wasi:clocks/monotonic-clock@0.2.0.subscribe-duration"}, true},
		{"myapp:db/connection@1.0.0", "query", []string{"myapp:db/connection@1.0.0.query"}, true},

		// Non-matching
		{"wasi:cli/environment@0.2.0", "get-arguments", []string{"wasi:filesystem/types@0.2.0.read"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.module+"."+tt.name, func(t *testing.T) {
			m := newExactMatcher(tt.patterns)
			got := m.Match(tt.module, tt.name)
			if got != tt.expected {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.module, tt.name, got, tt.expected)
			}
		})
	}
}

// TestEngine_Transform_AllPrimitiveLocals tests all primitive local types work
func TestEngine_Transform_AllPrimitiveLocals(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
			{
				Params:  []wasm.ValType{wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64},
				Results: []wasm.ValType{wasm.ValI32},
			},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async_func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{
					{Count: 1, ValType: wasm.ValI32},
					{Count: 1, ValType: wasm.ValI64},
					{Count: 1, ValType: wasm.ValF32},
					{Count: 1, ValType: wasm.ValF64},
				},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async_func"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}
	if len(result) == 0 {
		t.Error("Transform() returned empty result")
	}
}

// TestValidateLocalsForAsyncify tests the validation function
func TestValidateLocalsForAsyncify(t *testing.T) {
	tests := []struct {
		name    string
		params  []wasm.ValType
		locals  []wasm.LocalEntry
		wantErr bool
	}{
		{
			name:    "all primitives",
			params:  []wasm.ValType{wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64},
			locals:  []wasm.LocalEntry{{Count: 2, ValType: wasm.ValI32}},
			wantErr: false,
		},
		{
			name:    "funcref param",
			params:  []wasm.ValType{wasm.ValFuncRef},
			locals:  nil,
			wantErr: true,
		},
		{
			name:    "externref param",
			params:  []wasm.ValType{wasm.ValExtern},
			locals:  nil,
			wantErr: true,
		},
		{
			name:    "funcref local",
			params:  []wasm.ValType{wasm.ValI32},
			locals:  []wasm.LocalEntry{{Count: 1, ValType: wasm.ValFuncRef}},
			wantErr: true,
		},
		{
			name:    "externref local",
			params:  nil,
			locals:  []wasm.LocalEntry{{Count: 1, ValType: wasm.ValExtern}},
			wantErr: true,
		},
		{
			name:   "mixed with reference",
			params: []wasm.ValType{wasm.ValI32},
			locals: []wasm.LocalEntry{
				{Count: 1, ValType: wasm.ValI64},
				{Count: 1, ValType: wasm.ValFuncRef},
			},
			wantErr: true,
		},
		{
			name:    "v128 allowed",
			params:  []wasm.ValType{wasm.ValV128},
			locals:  []wasm.LocalEntry{{Count: 1, ValType: wasm.ValV128}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLocalsForAsyncify(tt.params, tt.locals)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLocalsForAsyncify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
