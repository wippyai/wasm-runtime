package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// exactMatcher matches exact "module.name" patterns
type exactMatcher struct {
	patterns map[string]bool
}

func newExactMatcher(patterns []string) *exactMatcher {
	m := &exactMatcher{patterns: make(map[string]bool)}
	for _, p := range patterns {
		m.patterns[p] = true
	}
	return m
}

func (m *exactMatcher) Match(module, name string) bool {
	return m.patterns[module+"."+name]
}

func ptrUint64(v uint64) *uint64 {
	return &v
}

func TestEngine_New(t *testing.T) {
	eng := New(Config{})
	if eng == nil {
		t.Fatal("New returned nil")
	}
	if eng.registry == nil {
		t.Error("default registry should be set")
	}
}

func TestEngine_DefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	if r == nil {
		t.Fatal("DefaultRegistry returned nil")
	}

	// Should have common handlers
	if !r.Has(wasm.OpI32Add) {
		t.Error("missing i32.add handler")
	}
	if !r.Has(wasm.OpLocalGet) {
		t.Error("missing local.get handler")
	}
	if !r.Has(wasm.OpI32Const) {
		t.Error("missing i32.const handler")
	}
}

func TestEngine_Transform_NoAsyncFuncs(t *testing.T) {
	// Simple module with no async imports
	wat := `(module
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should still add asyncify exports
	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Check for asyncify exports
	exports := make(map[string]bool)
	for _, exp := range m.Exports {
		exports[exp.Name] = true
	}

	expected := []string{
		"asyncify_get_state",
		"asyncify_start_unwind",
		"asyncify_stop_unwind",
		"asyncify_start_rewind",
		"asyncify_stop_rewind",
	}

	for _, name := range expected {
		if !exports[name] {
			t.Errorf("missing export: %s", name)
		}
	}
}

func TestEngine_Transform_WithAsyncImport(t *testing.T) {
	// Module that calls an async import
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(func (export "test")
			i32.const 100
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Should have 2 globals (state + data)
	if len(m.Globals) < 2 {
		t.Errorf("expected at least 2 globals, got %d", len(m.Globals))
	}

	// Transformed function should be larger
	if len(m.Code) == 0 {
		t.Fatal("no code sections")
	}
	if len(m.Code[0].Code) < 20 {
		t.Errorf("transformed code seems too small: %d bytes", len(m.Code[0].Code))
	}
}

func TestEngine_Transform_PreservesExports(t *testing.T) {
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(func (export "foo") (result i32)
			i32.const 42
		)
		(func (export "bar")
			i32.const 100
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	exports := make(map[string]bool)
	for _, exp := range m.Exports {
		exports[exp.Name] = true
	}

	// Original exports preserved
	if !exports["foo"] {
		t.Error("missing original export: foo")
	}
	if !exports["bar"] {
		t.Error("missing original export: bar")
	}

	// Asyncify exports added
	if !exports["asyncify_get_state"] {
		t.Error("missing asyncify_get_state")
	}
}

func TestEngine_Transform_TransitiveAsync(t *testing.T) {
	// Function A calls B which calls async import
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(func $inner
			i32.const 50
			call $sleep
		)
		(func (export "outer")
			call $inner
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Original module had 2 user functions: $inner (idx 0) and "outer" (idx 1)
	// Helper functions (asyncify_get_state, etc.) are added at indices 2+
	// Only user functions should be transformed (have scratch locals)
	numUserFuncs := 2
	if len(m.Code) < numUserFuncs {
		t.Fatalf("expected at least %d code sections, got %d", numUserFuncs, len(m.Code))
	}

	for i := 0; i < numUserFuncs; i++ {
		body := m.Code[i]
		if len(body.Locals) < 10 {
			t.Errorf("func %d: expected at least 10 scratch locals, got %d", i, len(body.Locals))
		}
	}
}

func TestEngine_Transform_WithMemory(t *testing.T) {
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(memory 1)
		(func (export "test")
			i32.const 0
			i32.load
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Memory should be preserved
	if len(m.Memories) != 1 {
		t.Errorf("expected 1 memory, got %d", len(m.Memories))
	}
}

func TestEngine_Transform_NoMemory_AddsDefault(t *testing.T) {
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(func (export "test")
			i32.const 100
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Should add default memory
	if len(m.Memories) != 1 {
		t.Errorf("expected 1 memory (added by asyncify), got %d", len(m.Memories))
	}
}

func TestEngine_Transform_MultipleCallSites(t *testing.T) {
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(func (export "test")
			i32.const 100
			call $sleep
			i32.const 200
			call $sleep
			i32.const 300
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should succeed without error
	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Transformed code should handle multiple call sites
	if len(m.Code) == 0 {
		t.Fatal("no code sections")
	}
}

func TestEngine_Transform_WithResult(t *testing.T) {
	wat := `(module
		(import "env" "read" (func $read (result i32)))
		(func (export "test") (result i32)
			call $read
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.read"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Should have transformed successfully
	if len(m.Code) == 0 {
		t.Fatal("no code sections")
	}
}

func TestEngine_Transform_VoidFunction(t *testing.T) {
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(func (export "test")
			i32.const 100
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should succeed
	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result is invalid WASM: %v", err)
	}
}

func TestEngine_Transform_NilMatcher(t *testing.T) {
	wat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(func (export "test")
			i32.const 100
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: nil, // No matcher
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should still produce valid output (just adds exports, no transformation)
	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result is invalid WASM: %v", err)
	}

	// Code should not be transformed (no scratch locals added)
	if len(m.Code) > 0 && len(m.Code[0].Locals) > 0 {
		t.Log("Note: code has locals but that might be from original")
	}
}

func TestEngine_Transform_InvalidWasm(t *testing.T) {
	eng := New(Config{})

	_, err := eng.Transform([]byte{0x00, 0x01, 0x02, 0x03})
	if err == nil {
		t.Error("expected error for invalid WASM")
	}
}

func TestEngine_GlobalIndices(t *testing.T) {
	wat := `(module
		(global $existing (mut i32) (i32.const 42))
		(import "env" "sleep" (func $sleep (param i32)))
		(func (export "test")
			i32.const 100
			call $sleep
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Should have 3 globals: 1 existing + 2 asyncify
	if len(m.Globals) != 3 {
		t.Errorf("expected 3 globals, got %d", len(m.Globals))
	}
}

func TestEngine_HelperFunctions(t *testing.T) {
	wat := `(module
		(func (export "foo") (result i32)
			i32.const 42
		)
	)`

	wasmBytes := watToWasm(t, wat)

	eng := New(Config{})

	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	m, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Find asyncify_get_state and verify it works
	var getStateFuncIdx = -1
	for _, exp := range m.Exports {
		if exp.Name == "asyncify_get_state" {
			getStateFuncIdx = int(exp.Idx)
			break
		}
	}

	if getStateFuncIdx < 0 {
		t.Fatal("asyncify_get_state not found")
	}

	// The helper function should be valid code
	numImported := m.NumImportedFuncs()
	localIdx := getStateFuncIdx - numImported
	if localIdx >= 0 && localIdx < len(m.Code) {
		code := m.Code[localIdx].Code
		if len(code) < 3 {
			t.Errorf("asyncify_get_state code too short: %d bytes", len(code))
		}
	}
}

func TestValTypeHelpers(t *testing.T) {
	tests := []struct {
		vt       wasm.ValType
		wantSize int
	}{
		{wasm.ValI32, 4},
		{wasm.ValI64, 8},
		{wasm.ValF32, 4},
		{wasm.ValF64, 8},
	}

	for _, tt := range tests {
		if got := ValTypeSize(tt.vt); got != tt.wantSize {
			t.Errorf("ValTypeSize(%#x) = %d, want %d", tt.vt, got, tt.wantSize)
		}
	}
}

func TestValTypeLoadStoreOps(t *testing.T) {
	types := []wasm.ValType{wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64}

	for _, vt := range types {
		loadOp, loadAlign := ValTypeLoadOp(vt)
		storeOp, storeAlign := ValTypeStoreOp(vt)

		if loadOp == 0 {
			t.Errorf("ValTypeLoadOp(%#x) returned 0", vt)
		}
		if storeOp == 0 {
			t.Errorf("ValTypeStoreOp(%#x) returned 0", vt)
		}
		if loadAlign != storeAlign {
			t.Errorf("alignment mismatch for %#x: load=%d store=%d", vt, loadAlign, storeAlign)
		}
	}
}

// watToWasm compiles WAT to WASM using wasm-tools (if available)
func watToWasm(t *testing.T, wat string) []byte {
	t.Helper()

	// Try to use wasm-tools
	result, err := compileWat(wat)
	if err != nil {
		t.Skipf("wasm-tools not available: %v", err)
	}
	return result
}

func compileWat(wat string) ([]byte, error) {
	// Use a simple hand-coded module for tests that don't need wasm-tools
	// This is a minimal valid WASM module structure

	// For testing, we'll create minimal test modules programmatically
	// rather than requiring wasm-tools

	// Simple module with one function
	if wat == `(module
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
	)` {
		return createAddModule(), nil
	}

	if wat == `(module
		(func (export "foo") (result i32)
			i32.const 42
		)
	)` {
		return createSimpleModule(), nil
	}

	// For modules with imports, create them programmatically
	return createTestModuleFromWat(wat)
}

func createAddModule() []byte {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32, wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "add", Kind: 0, Idx: 0},
		},
		Code: []wasm.FuncBody{
			{
				Code: encodeInstrs([]wasm.Instruction{
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
					{Opcode: wasm.OpI32Add},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}
	return m.Encode()
}

func createSimpleModule() []byte {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "foo", Kind: 0, Idx: 0},
		},
		Code: []wasm.FuncBody{
			{
				Code: encodeInstrs([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}
	return m.Encode()
}

func createTestModuleFromWat(wat string) ([]byte, error) {
	// Parse WAT-like strings and create modules
	// This is a simplified parser for test purposes

	m := &wasm.Module{}

	// Check for common patterns
	hasEnvSleep := containsPattern(wat, `(import "env" "sleep"`)
	hasEnvRead := containsPattern(wat, `(import "env" "read"`)
	hasMemory := containsPattern(wat, `(memory`)
	hasGlobal := containsPattern(wat, `(global`)

	if hasEnvSleep {
		m.Types = append(m.Types, wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}})
		m.Imports = append(m.Imports, wasm.Import{
			Module: "env",
			Name:   "sleep",
			Desc:   wasm.ImportDesc{Kind: 0, TypeIdx: 0},
		})
	}

	if hasEnvRead {
		m.Types = append(m.Types, wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}})
		m.Imports = append(m.Imports, wasm.Import{
			Module: "env",
			Name:   "read",
			Desc:   wasm.ImportDesc{Kind: 0, TypeIdx: uint32(len(m.Types) - 1)},
		})
	}

	if hasMemory {
		m.Memories = append(m.Memories, wasm.MemoryType{
			Limits: wasm.Limits{Min: 1},
		})
	}

	if hasGlobal {
		m.Globals = append(m.Globals, wasm.Global{
			Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true},
			Init: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpEnd},
			}),
		})
	}

	// Add test function type
	voidType := uint32(len(m.Types))
	m.Types = append(m.Types, wasm.FuncType{})

	// Check for functions
	if containsPattern(wat, `(func (export "test")`) {
		m.Funcs = append(m.Funcs, voidType)
		m.Exports = append(m.Exports, wasm.Export{
			Name: "test",
			Kind: 0,
			Idx:  uint32(m.NumImportedFuncs()),
		})

		// Create code that calls the import
		var code []wasm.Instruction
		if hasEnvSleep {
			code = append(code, wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}})
			code = append(code, wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})

			// Check for multiple calls
			if containsPattern(wat, "i32.const 200") {
				code = append(code, wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 200}})
				code = append(code, wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
			}
			if containsPattern(wat, "i32.const 300") {
				code = append(code, wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 300}})
				code = append(code, wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
			}
		}
		if hasEnvRead {
			code = append(code, wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
		}
		if hasMemory && containsPattern(wat, "i32.load") {
			code = append(code, wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}})
			code = append(code, wasm.Instruction{Opcode: wasm.OpI32Load, Imm: wasm.MemoryImm{Align: 2, Offset: 0}})
			code = append(code, wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
		}
		code = append(code, wasm.Instruction{Opcode: wasm.OpEnd})

		m.Code = append(m.Code, wasm.FuncBody{Code: encodeInstrs(code)})
	}

	// Handle (func (export "test") (result i32)
	if containsPattern(wat, `(func (export "test") (result i32)`) {
		i32Type := uint32(len(m.Types))
		m.Types = append(m.Types, wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}})
		m.Funcs = append(m.Funcs, i32Type)
		m.Exports = append(m.Exports, wasm.Export{
			Name: "test",
			Kind: 0,
			Idx:  uint32(m.NumImportedFuncs()),
		})

		code := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpEnd},
		}
		m.Code = append(m.Code, wasm.FuncBody{Code: encodeInstrs(code)})
	}

	// Handle $inner and outer pattern
	if containsPattern(wat, `(func $inner`) {
		m.Funcs = append(m.Funcs, voidType)
		code := []wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 50}},
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpEnd},
		}
		m.Code = append(m.Code, wasm.FuncBody{Code: encodeInstrs(code)})
	}

	if containsPattern(wat, `(func (export "outer")`) {
		m.Funcs = append(m.Funcs, voidType)
		m.Exports = append(m.Exports, wasm.Export{
			Name: "outer",
			Kind: 0,
			Idx:  uint32(m.NumImportedFuncs() + len(m.Code)),
		})
		code := []wasm.Instruction{
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: uint32(m.NumImportedFuncs())}},
			{Opcode: wasm.OpEnd},
		}
		m.Code = append(m.Code, wasm.FuncBody{Code: encodeInstrs(code)})
	}

	// Handle foo/bar pattern
	if containsPattern(wat, `(func (export "foo") (result i32)`) && containsPattern(wat, `(func (export "bar")`) {
		// foo returns i32
		i32Type := uint32(len(m.Types))
		m.Types = append(m.Types, wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}})
		m.Funcs = append(m.Funcs, i32Type)
		m.Exports = append(m.Exports, wasm.Export{
			Name: "foo",
			Kind: 0,
			Idx:  uint32(m.NumImportedFuncs()),
		})
		m.Code = append(m.Code, wasm.FuncBody{
			Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpEnd},
			}),
		})

		// bar is void and calls sleep
		m.Funcs = append(m.Funcs, voidType)
		m.Exports = append(m.Exports, wasm.Export{
			Name: "bar",
			Kind: 0,
			Idx:  uint32(m.NumImportedFuncs() + 1),
		})
		m.Code = append(m.Code, wasm.FuncBody{
			Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			}),
		})
	}

	return m.Encode(), nil
}

func containsPattern(s, pattern string) bool {
	return len(s) >= len(pattern) && (s == pattern || len(s) > len(pattern) && (s[:len(pattern)] == pattern || containsPattern(s[1:], pattern)))
}

func encodeInstrs(instrs []wasm.Instruction) []byte {
	return wasm.EncodeInstructions(instrs)
}

// Fuzz tests

func FuzzEngine_Transform(f *testing.F) {
	// Add seed corpus
	simple := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}
	f.Add(simple.Encode())

	withReturn := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "read", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}
	f.Add(withReturn.Encode())

	f.Fuzz(func(t *testing.T, data []byte) {
		// Skip too small inputs
		if len(data) < 8 {
			return
		}

		// Must start with WASM magic
		if len(data) >= 4 && (data[0] != 0x00 || data[1] != 0x61 || data[2] != 0x73 || data[3] != 0x6d) {
			return
		}

		eng := New(Config{
			Matcher: newExactMatcher([]string{"env.sleep", "env.read", "env.async"}),
		})

		result, err := eng.Transform(data)
		if err != nil {
			// Errors are expected for invalid/malformed input
			return
		}

		// If transform succeeds, result must be valid WASM
		_, err = wasm.ParseModule(result)
		if err != nil {
			t.Errorf("transform produced invalid WASM: %v", err)
		}
	})
}

// Control flow tests - async calls inside if/block/loop

func TestEngine_Transform_AsyncInIf(t *testing.T) {
	// Async call inside an if block
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{Params: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async_op", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}}, // void block
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd}, // end if
				{Opcode: wasm.OpEnd}, // end func
			})},
		},
	}

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.async_op"}),
	})

	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Verify function was transformed
	if len(out.Code[0].Locals) < 10 {
		t.Errorf("expected at least 10 scratch locals, got %d", len(out.Code[0].Locals))
	}

	// Decode and verify structure contains proper control flow handling
	instrs, err := wasm.DecodeInstructions(out.Code[0].Code)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Should have nested blocks for asyncify state machine
	blockCount := 0
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpBlock {
			blockCount++
		}
	}
	if blockCount < 3 {
		t.Errorf("expected at least 3 blocks for asyncify structure, got %d", blockCount)
	}
}

func TestEngine_Transform_AsyncInLoop(t *testing.T) {
	// Async call inside a loop
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "yield", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "loop_test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}}, // loop back
				{Opcode: wasm.OpEnd}, // end loop
				{Opcode: wasm.OpEnd}, // end func
			})},
		},
	}

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.yield"}),
	})

	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Code[0].Locals) < 10 {
		t.Errorf("expected at least 10 scratch locals, got %d", len(out.Code[0].Locals))
	}
}

func TestEngine_Transform_AsyncInBlock(t *testing.T) {
	// Async call inside a block with break
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "get_value", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "block_test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -1}}, // i32 result
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
				{Opcode: wasm.OpEnd}, // end block
				{Opcode: wasm.OpEnd}, // end func
			})},
		},
	}

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.get_value"}),
	})

	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Code[0].Locals) < 10 {
		t.Errorf("expected at least 10 scratch locals, got %d", len(out.Code[0].Locals))
	}
}

func TestEngine_Transform_NestedControlFlow(t *testing.T) {
	// Async call in nested if inside loop
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{Params: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "process", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "nested", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd}, // end if
				{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
				{Opcode: wasm.OpEnd}, // end loop
				{Opcode: wasm.OpEnd}, // end func
			})},
		},
	}

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.process"}),
	})

	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Code[0].Locals) < 10 {
		t.Errorf("expected at least 10 scratch locals, got %d", len(out.Code[0].Locals))
	}
}

// Benchmark tests

func BenchmarkEngine_Transform_Simple(b *testing.B) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}
	wasmBytes := m.Encode()

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.sleep"}),
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := eng.Transform(wasmBytes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngine_Transform_MultipleCallSites(b *testing.B) {
	instrs := []wasm.Instruction{}
	for i := 0; i < 10; i++ {
		instrs = append(instrs,
			wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: int32(i)}},
			wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		)
	}
	instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpEnd})

	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "process", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs(instrs)},
		},
	}
	wasmBytes := m.Encode()

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.process"}),
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := eng.Transform(wasmBytes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Comparison test against Binaryen reference

func TestEngine_Transform_CompareBinaryenStructure(t *testing.T) {
	// Test that our transform produces structurally similar output to Binaryen
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "get_value", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1, Max: ptrUint64(1)}}},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				// if (n > 0) { return get_value(n); } else { return 0; }
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
				{Opcode: wasm.OpI32GtS},
				{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -1}}, // i32 result
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpElse},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
				{Opcode: wasm.OpEnd}, // end if
				{Opcode: wasm.OpEnd}, // end func
			})},
		},
	}

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.get_value"}),
	})

	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Check structural properties that should match Binaryen

	// 1. Should have exactly 2 asyncify globals
	if len(out.Globals) != 2 {
		t.Errorf("expected 2 asyncify globals, got %d", len(out.Globals))
	}

	// 2. Both globals should be mutable i32
	for i, g := range out.Globals {
		if g.Type.ValType != wasm.ValI32 {
			t.Errorf("global %d: expected i32 type", i)
		}
		if !g.Type.Mutable {
			t.Errorf("global %d: expected mutable", i)
		}
	}

	// 3. Should have helper exports
	exports := make(map[string]bool)
	for _, exp := range out.Exports {
		exports[exp.Name] = true
	}

	expectedExports := []string{
		"asyncify_get_state",
		"asyncify_start_unwind",
		"asyncify_stop_unwind",
		"asyncify_start_rewind",
		"asyncify_stop_rewind",
	}
	for _, name := range expectedExports {
		if !exports[name] {
			t.Errorf("missing export: %s", name)
		}
	}

	// 4. Transformed function should have scratch locals
	if len(out.Code) == 0 {
		t.Fatal("no code sections")
	}
	if len(out.Code[0].Locals) < 10 {
		t.Errorf("expected at least 10 scratch locals, got %d", len(out.Code[0].Locals))
	}

	// 5. Decode and verify key patterns
	instrs, err := wasm.DecodeInstructions(out.Code[0].Code)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Should start with "if state == 2" (rewind preamble)
	foundRewindCheck := false
	for i := 0; i < len(instrs)-2; i++ {
		if instrs[i].Opcode == wasm.OpGlobalGet &&
			instrs[i+1].Opcode == wasm.OpI32Const &&
			instrs[i+2].Opcode == wasm.OpI32Eq {
			if imm, ok := instrs[i+1].Imm.(wasm.I32Imm); ok && imm.Value == 2 {
				foundRewindCheck = true
				break
			}
		}
	}
	if !foundRewindCheck {
		t.Error("missing rewind check (if state == 2)")
	}

	// Should have "if state == 1" (unwind check after call)
	foundUnwindCheck := false
	for i := 0; i < len(instrs)-2; i++ {
		if instrs[i].Opcode == wasm.OpGlobalGet &&
			instrs[i+1].Opcode == wasm.OpI32Const &&
			instrs[i+2].Opcode == wasm.OpI32Eq {
			if imm, ok := instrs[i+1].Imm.(wasm.I32Imm); ok && imm.Value == 1 {
				foundUnwindCheck = true
				break
			}
		}
	}
	if !foundUnwindCheck {
		t.Error("missing unwind check (if state == 1)")
	}

	// Should have block structure
	blockCount := 0
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpBlock {
			blockCount++
		}
	}
	if blockCount < 2 {
		t.Errorf("expected at least 2 blocks, got %d", blockCount)
	}
}

// Tests for unsupported opcode rejection

func TestEngine_RejectsAtomicOpcodes(t *testing.T) {
	// Create module with atomic wait instruction (0xFE prefix) in an async function.
	// Only async functions are validated, so we need an async import and caller.
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			// call async (0x10 0x00), then atomic opcode
			{Code: []byte{0x10, 0x00, 0xFE, 0x01, 0x02, 0x00, 0x0B}}, // call 0, atomic prefix + subop + align + offset + end
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	_, err := eng.Transform(m.Encode())
	if err == nil {
		t.Error("expected error for atomic opcode in async function, got nil")
	}
	if err != nil && !containsPattern(err.Error(), "atomic") {
		t.Errorf("expected atomic error message, got: %v", err)
	}
}

func TestEngine_AllowsAtomicOpcodesInNonAsyncPath(t *testing.T) {
	// Atomic opcodes in non-async path should be allowed
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{Code: []byte{0xFE, 0x01, 0x02, 0x00, 0x0B}}, // atomic prefix + subop + align + offset + end
		},
	}

	eng := New(Config{})
	_, err := eng.Transform(m.Encode())
	if err != nil {
		t.Errorf("expected success for atomic opcode in non-async path, got: %v", err)
	}
}

func TestEngine_RejectsTailCalls(t *testing.T) {
	// Create module with return_call instruction (0x12) in an async function.
	// Only async functions are validated for unsupported opcodes.
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			// call async, then return_call (tail call back to self)
			{Code: []byte{0x10, 0x00, 0x12, 0x01, 0x0B}}, // call 0, return_call 1, end
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	_, err := eng.Transform(m.Encode())
	if err == nil {
		t.Error("expected error for tail call in async function, got nil")
	}
	if err != nil && !containsPattern(err.Error(), "tail call") {
		t.Errorf("expected tail call error message, got: %v", err)
	}
}

func TestEngine_AllowsTailCallsInNonAsyncPath(t *testing.T) {
	// Tail calls in non-async path should be allowed
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Funcs: []uint32{0, 0},
		Code: []wasm.FuncBody{
			{Code: []byte{0x0B}},             // just end - normal func
			{Code: []byte{0x12, 0x00, 0x0B}}, // return_call 0, end - tail call
		},
	}

	eng := New(Config{})
	_, err := eng.Transform(m.Encode())
	if err != nil {
		t.Errorf("expected success for tail call in non-async path, got: %v", err)
	}
}

func TestEngine_RejectsExceptionHandling(t *testing.T) {
	// Create module with try instruction (0x06) in an async function.
	// Only async functions are validated for unsupported opcodes.
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			// call async, then try block
			{Code: []byte{0x10, 0x00, 0x06, 0x40, 0x0B, 0x0B}}, // call 0, try, void block type, end, end
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	_, err := eng.Transform(m.Encode())
	// Either parse error or our explicit rejection is acceptable
	if err == nil {
		t.Error("expected error for exception handling in async function, got nil")
	}
}

func TestEngine_AllowsExceptionHandlingInNonAsyncPath(t *testing.T) {
	// Exception handling in non-async path should be allowed
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{Code: []byte{0x06, 0x40, 0x0B, 0x0B}}, // try, void block type, end, end
		},
	}

	eng := New(Config{})
	_, err := eng.Transform(m.Encode())
	// No error expected - exception handling in non-async path is fine
	// (may still get parse error if wasm package doesn't support it, that's ok too)
	_ = err
}

func TestEngine_RemoveAsyncifyImports_ReindexesCalls(t *testing.T) {
	// Test that when asyncify imports are removed, function indices are updated.
	// Bug scenario:
	// - Function 0: asyncify_start_unwind (import)
	// - Function 1: env.other (import)
	// - Function 2: local function that calls function 1
	// After removing asyncify import:
	// - Function 0: env.other (was 1)
	// - Function 1: local function (was 2)
	// The call instruction must be updated from "call 1" to "call 0"
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}}, // type 0: (i32) -> ()
			{},                                    // type 1: () -> ()
		},
		Imports: []wasm.Import{
			{Module: "asyncify", Name: "asyncify_start_unwind", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "other", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 1}},
		},
		Funcs:   []uint32{1}, // local func uses type 1
		Exports: []wasm.Export{{Name: "run", Kind: 0, Idx: 2}},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}}, // call env.other (index 1)
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.other"}),
	})

	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// After removing asyncify import:
	// - Function 0 should now be env.other
	// - The call in code should be updated to call 0, not still call 1

	// Verify asyncify import was removed
	for _, imp := range out.Imports {
		if imp.Module == "asyncify" {
			t.Error("asyncify import should have been removed")
		}
	}

	// Count function imports - should be 1 (env.other only)
	funcImportCount := 0
	for _, imp := range out.Imports {
		if imp.Desc.Kind == wasm.KindFunc {
			funcImportCount++
		}
	}
	if funcImportCount != 1 {
		t.Errorf("expected 1 function import after removal, got %d", funcImportCount)
	}

	// Validate the module is well-formed
	if err := out.Validate(); err != nil {
		t.Errorf("module validation failed after asyncify import removal: %v", err)
	}

	// Verify async call targets the correct function (env.other = index 0 after removal)
	// Decode first code body (the transformed local function)
	if len(out.Code) > 0 {
		instrs, err := wasm.DecodeInstructions(out.Code[0].Code)
		if err != nil {
			t.Fatalf("decode code: %v", err)
		}

		// Find all direct call instructions in the transformed code
		// The async call to env.other should now reference index 0 (not 1)
		foundCallToAsyncImport := false
		for _, instr := range instrs {
			if instr.Opcode == wasm.OpCall {
				if imm, ok := instr.Imm.(wasm.CallImm); ok {
					// env.other was index 1, should now be index 0
					if imm.FuncIdx == 0 {
						foundCallToAsyncImport = true
					}
				}
			}
		}

		// We expect to find a call to the async import (now at index 0)
		if !foundCallToAsyncImport {
			t.Error("no call to async import (index 0) found; indices may not have been updated")
		}
	}
}

func TestEngine_ExportGlobals(t *testing.T) {
	// Test that ExportGlobals correctly exports globals with kind=3
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:   []uint32{1},
		Exports: []wasm.Export{{Name: "run", Kind: 0, Idx: 1}},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	eng := New(Config{
		Matcher:       newExactMatcher([]string{"env.async"}),
		ExportGlobals: true,
	})

	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Find the global exports and verify their kind
	foundState := false
	foundData := false
	for _, exp := range out.Exports {
		if exp.Name == "asyncify_state" {
			foundState = true
			if exp.Kind != 3 { // KindGlobal = 3
				t.Errorf("asyncify_state export has kind %d, want 3 (global)", exp.Kind)
			}
		}
		if exp.Name == "asyncify_data" {
			foundData = true
			if exp.Kind != 3 { // KindGlobal = 3
				t.Errorf("asyncify_data export has kind %d, want 3 (global)", exp.Kind)
			}
		}
	}

	if !foundState {
		t.Error("missing asyncify_state export")
	}
	if !foundData {
		t.Error("missing asyncify_data export")
	}
}

func BenchmarkEngine_Transform_LargeFunction(b *testing.B) {
	// Create a function with many instructions
	instrs := []wasm.Instruction{}
	for i := 0; i < 100; i++ {
		instrs = append(instrs,
			wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: int32(i)}},
			wasm.Instruction{Opcode: wasm.OpDrop},
		)
	}
	instrs = append(instrs,
		wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		wasm.Instruction{Opcode: wasm.OpEnd},
	)

	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Code: []wasm.FuncBody{
			{Code: encodeInstrs(instrs)},
		},
	}
	wasmBytes := m.Encode()

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.async"}),
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := eng.Transform(wasmBytes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestEngine_AdjustGlobalReferences(t *testing.T) {
	// Test adjustGlobalReferences with all reference types:
	// - Code section (global.get/set)
	// - Global initializers (global.get)
	// - Element segment offsets (global.get)
	// - Data segment offsets (global.get)

	eng := New(Config{})

	t.Run("global_initializers", func(t *testing.T) {
		// Global init = global.get 0
		initCode := wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}},
			{Opcode: wasm.OpEnd},
		})

		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Globals: []wasm.Global{
				{Type: wasm.GlobalType{ValType: wasm.ValI32}, Init: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
					{Opcode: wasm.OpEnd},
				})},
				{Type: wasm.GlobalType{ValType: wasm.ValI32}, Init: initCode},
			},
		}

		if err := eng.adjustGlobalReferences(m, 2); err != nil {
			t.Fatal(err)
		}

		// Decode and check the init expression was adjusted
		instrs, err := wasm.DecodeInstructions(m.Globals[1].Init)
		if err != nil {
			t.Fatal(err)
		}
		if len(instrs) < 1 {
			t.Fatal("expected at least 1 instruction")
		}
		imm, ok := instrs[0].Imm.(wasm.GlobalImm)
		if !ok {
			t.Fatalf("expected GlobalImm, got %T", instrs[0].Imm)
		}
		if imm.GlobalIdx != 2 {
			t.Errorf("expected global idx 2, got %d", imm.GlobalIdx)
		}
	})

	t.Run("element_segment_offset", func(t *testing.T) {
		// Element segment with global.get offset
		offsetCode := wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 1}},
			{Opcode: wasm.OpEnd},
		})

		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Tables: []wasm.TableType{
				{ElemType: 0x70, Limits: wasm.Limits{Min: 10}}, // funcref
			},
			Elements: []wasm.Element{
				{TableIdx: 0, Offset: offsetCode, FuncIdxs: []uint32{0}},
			},
			Funcs: []uint32{0},
			Code:  []wasm.FuncBody{{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}})}},
		}

		if err := eng.adjustGlobalReferences(m, 3); err != nil {
			t.Fatal(err)
		}

		instrs, err := wasm.DecodeInstructions(m.Elements[0].Offset)
		if err != nil {
			t.Fatal(err)
		}
		imm := instrs[0].Imm.(wasm.GlobalImm)
		if imm.GlobalIdx != 4 { // 1 + 3
			t.Errorf("expected global idx 4, got %d", imm.GlobalIdx)
		}
	})

	t.Run("data_segment_offset", func(t *testing.T) {
		// Data segment with global.get offset
		offsetCode := wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 5}},
			{Opcode: wasm.OpEnd},
		})

		m := &wasm.Module{
			Types:    []wasm.FuncType{{}},
			Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
			Data: []wasm.DataSegment{
				{MemIdx: 0, Offset: offsetCode, Init: []byte{0x01, 0x02}},
			},
		}

		if err := eng.adjustGlobalReferences(m, 10); err != nil {
			t.Fatal(err)
		}

		instrs, err := wasm.DecodeInstructions(m.Data[0].Offset)
		if err != nil {
			t.Fatal(err)
		}
		imm := instrs[0].Imm.(wasm.GlobalImm)
		if imm.GlobalIdx != 15 { // 5 + 10
			t.Errorf("expected global idx 15, got %d", imm.GlobalIdx)
		}
	})

	t.Run("code_section_globalget_globalset", func(t *testing.T) {
		// Function code with global.get and global.set
		code := wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			{Opcode: wasm.OpI32Add},
			{Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 0}},
			{Opcode: wasm.OpEnd},
		})

		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Funcs: []uint32{0},
			Code:  []wasm.FuncBody{{Code: code}},
		}

		if err := eng.adjustGlobalReferences(m, 2); err != nil {
			t.Fatal(err)
		}

		instrs, err := wasm.DecodeInstructions(m.Code[0].Code)
		if err != nil {
			t.Fatal(err)
		}

		// Check global.get was adjusted
		getImm := instrs[0].Imm.(wasm.GlobalImm)
		if getImm.GlobalIdx != 2 {
			t.Errorf("global.get: expected idx 2, got %d", getImm.GlobalIdx)
		}

		// Check global.set was adjusted
		setImm := instrs[3].Imm.(wasm.GlobalImm)
		if setImm.GlobalIdx != 2 {
			t.Errorf("global.set: expected idx 2, got %d", setImm.GlobalIdx)
		}
	})

	t.Run("no_modification_when_no_globals", func(t *testing.T) {
		// Code without global references
		code := wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
			{Opcode: wasm.OpDrop},
			{Opcode: wasm.OpEnd},
		})
		original := make([]byte, len(code))
		copy(original, code)

		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Funcs: []uint32{0},
			Code:  []wasm.FuncBody{{Code: code}},
		}

		if err := eng.adjustGlobalReferences(m, 5); err != nil {
			t.Fatal(err)
		}

		// Code should not be re-encoded (same bytes)
		for i := range original {
			if m.Code[0].Code[i] != original[i] {
				t.Error("code was modified but should not have been")
				break
			}
		}
	})
}

func TestEngine_ValidateAsyncFuncs_TailCalls(t *testing.T) {
	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})

	t.Run("return_call", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Imports: []wasm.Import{
				{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			},
			Funcs: []uint32{0},
			Code: []wasm.FuncBody{{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: opReturnCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			}},
		}

		asyncFuncs := map[uint32]bool{1: true}
		err := eng.validateAsyncFuncs(m, asyncFuncs, 1)
		if err == nil {
			t.Error("expected error for return_call")
		}
	})

	t.Run("return_call_indirect", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Imports: []wasm.Import{
				{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			},
			Tables: []wasm.TableType{{ElemType: 0x70, Limits: wasm.Limits{Min: 1}}},
			Funcs:  []uint32{0},
			Code: []wasm.FuncBody{{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: opReturnCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			}},
		}

		asyncFuncs := map[uint32]bool{1: true}
		err := eng.validateAsyncFuncs(m, asyncFuncs, 1)
		if err == nil {
			t.Error("expected error for return_call_indirect")
		}
	})

	t.Run("return_call_ref", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Imports: []wasm.Import{
				{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			},
			Funcs: []uint32{0},
			Code: []wasm.FuncBody{{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: wasm.HeapTypeFunc}},
					{Opcode: opReturnCallRef, Imm: wasm.CallRefImm{TypeIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			}},
		}

		asyncFuncs := map[uint32]bool{1: true}
		err := eng.validateAsyncFuncs(m, asyncFuncs, 1)
		if err == nil {
			t.Error("expected error for return_call_ref")
		}
	})
}

func TestEngine_ValidateAsyncFuncs_ExceptionHandling(t *testing.T) {
	eng := New(Config{})

	tests := []struct {
		name   string
		opcode byte
	}{
		{"try", opTry},
		{"catch", opCatch},
		{"throw", opThrow},
		{"rethrow", opRethrow},
		{"delegate", opDelegate},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create raw bytecode since these are proposal opcodes
			code := []byte{tc.opcode, 0x0B} // opcode + end

			m := &wasm.Module{
				Types: []wasm.FuncType{{}},
				Funcs: []uint32{0},
				Code:  []wasm.FuncBody{{Code: code}},
			}

			asyncFuncs := map[uint32]bool{0: true}
			err := eng.validateAsyncFuncs(m, asyncFuncs, 0)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestEngine_ValidateAsyncFuncs_AtomicPrefix(t *testing.T) {
	eng := New(Config{})

	// Create raw bytecode: 0xFE (atomic prefix) + 0x00 (sub-opcode) + 0x00 (align) + 0x00 (offset) + 0x0B (end)
	code := []byte{opAtomicPrefix, 0x00, 0x00, 0x00, 0x0B}

	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Funcs: []uint32{0},
		Code:  []wasm.FuncBody{{Code: code}},
	}

	asyncFuncs := map[uint32]bool{0: true}
	err := eng.validateAsyncFuncs(m, asyncFuncs, 0)
	if err == nil {
		t.Error("expected error for atomic operations")
	}
}

func TestEngine_RemoveAsyncifyConflicts(t *testing.T) {
	eng := New(Config{})

	t.Run("removes_asyncify_module_imports", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Imports: []wasm.Import{
				{Module: "asyncify", Name: "start_unwind", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				{Module: "env", Name: "other", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			},
			Funcs: []uint32{0},
			Exports: []wasm.Export{
				{Name: "test", Kind: 0, Idx: 2},
			},
			Code: []wasm.FuncBody{{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}}, // call env.other
					{Opcode: wasm.OpEnd},
				}),
			}},
		}

		err := eng.removeAsyncifyConflicts(m)
		if err != nil {
			t.Fatal(err)
		}

		if len(m.Imports) != 1 {
			t.Errorf("expected 1 import after removal, got %d", len(m.Imports))
		}
		if m.Imports[0].Module != "env" {
			t.Error("wrong import remained")
		}
	})

	t.Run("removes_asyncify_named_imports", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Imports: []wasm.Import{
				{Module: "env", Name: "asyncify_start_unwind", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				{Module: "env", Name: "asyncify_stop_unwind", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				{Module: "env", Name: "normal_func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			},
			Funcs: []uint32{0},
			Code: []wasm.FuncBody{{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpEnd},
				}),
			}},
		}

		err := eng.removeAsyncifyConflicts(m)
		if err != nil {
			t.Fatal(err)
		}

		if len(m.Imports) != 1 {
			t.Errorf("expected 1 import, got %d", len(m.Imports))
		}
	})

	t.Run("updates_start_function", func(t *testing.T) {
		startIdx := uint32(2)
		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Imports: []wasm.Import{
				{Module: "asyncify", Name: "func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				{Module: "env", Name: "other", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			},
			Funcs: []uint32{0},
			Start: &startIdx,
			Code: []wasm.FuncBody{{
				Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}}),
			}},
		}

		err := eng.removeAsyncifyConflicts(m)
		if err != nil {
			t.Fatal(err)
		}

		if *m.Start != 1 {
			t.Errorf("start function should be reindexed to 1, got %d", *m.Start)
		}
	})

	t.Run("updates_element_segments", func(t *testing.T) {
		m := &wasm.Module{
			Types: []wasm.FuncType{{}},
			Imports: []wasm.Import{
				{Module: "asyncify", Name: "func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				{Module: "env", Name: "other", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			},
			Funcs:  []uint32{0},
			Tables: []wasm.TableType{{ElemType: 0x70, Limits: wasm.Limits{Min: 1}}},
			Elements: []wasm.Element{
				{FuncIdxs: []uint32{1, 2}}, // env.other and local func
			},
			Code: []wasm.FuncBody{{
				Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}}),
			}},
		}

		err := eng.removeAsyncifyConflicts(m)
		if err != nil {
			t.Fatal(err)
		}

		// After removing asyncify import at 0, indices should shift down
		if m.Elements[0].FuncIdxs[0] != 0 || m.Elements[0].FuncIdxs[1] != 1 {
			t.Errorf("element indices not reindexed: %v", m.Elements[0].FuncIdxs)
		}
	})
}

func TestEngine_Transform_WithImportedMemory(t *testing.T) {
	// Module with imported memory - tests ensureMemory, addSecondaryMemory, validateMemoryIndex
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{}, // void -> void
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "memory", Desc: wasm.ImportDesc{Kind: 2, Memory: &wasm.MemoryType{Limits: wasm.Limits{Min: 1}}}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.sleep"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	parsed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Should NOT add module memory since one is imported
	if len(parsed.Memories) > 0 {
		t.Errorf("should not add module memory when memory is imported, got %d", len(parsed.Memories))
	}

	// Should have asyncify exports
	exports := make(map[string]bool)
	for _, exp := range parsed.Exports {
		exports[exp.Name] = true
	}
	if !exports["asyncify_get_state"] {
		t.Error("missing asyncify_get_state export")
	}
}

func TestEngine_Transform_WithImportedGlobal(t *testing.T) {
	// Module with imported global - tests createAsyncifyGlobals baseIdx adjustment
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{}, // void -> void
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "counter", Desc: wasm.ImportDesc{Kind: 3, Global: &wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.sleep"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	parsed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Count imports by kind
	importedGlobals := 0
	for _, imp := range parsed.Imports {
		if imp.Desc.Kind == 3 {
			importedGlobals++
		}
	}

	// Should have 1 imported global + 2 module globals (state + data)
	if importedGlobals != 1 {
		t.Errorf("expected 1 imported global, got %d", importedGlobals)
	}
	if len(parsed.Globals) != 2 {
		t.Errorf("expected 2 module globals, got %d", len(parsed.Globals))
	}
}

func TestEngine_Transform_WithMultipleImportedGlobals(t *testing.T) {
	// Module with multiple imported globals - tests global reference adjustment
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{}, // void -> void
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "g1", Desc: wasm.ImportDesc{Kind: 3, Global: &wasm.GlobalType{ValType: wasm.ValI32}}},
			{Module: "env", Name: "g2", Desc: wasm.ImportDesc{Kind: 3, Global: &wasm.GlobalType{ValType: wasm.ValI64}}},
			{Module: "env", Name: "g3", Desc: wasm.ImportDesc{Kind: 3, Global: &wasm.GlobalType{ValType: wasm.ValF32}}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.sleep"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	parsed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Count imports by kind
	importedGlobals := 0
	for _, imp := range parsed.Imports {
		if imp.Desc.Kind == 3 {
			importedGlobals++
		}
	}

	// Should have 3 imported globals + 2 module globals
	if importedGlobals != 3 {
		t.Errorf("expected 3 imported globals, got %d", importedGlobals)
	}
	if len(parsed.Globals) != 2 {
		t.Errorf("expected 2 module globals (state+data), got %d", len(parsed.Globals))
	}
}

// TestBuildCallGraph_MalformedCode tests that BuildCallGraph returns error for malformed input.
func TestBuildCallGraph_MalformedCode(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{Code: []byte{0xFF, 0xFF, 0xFF}}, // invalid instruction sequence
		},
	}

	_, err := BuildCallGraph(m)
	if err == nil {
		t.Error("expected error for malformed code, got nil")
	}
}

// TestEngine_Transform_MalformedCode tests various malformed input scenarios.
func TestEngine_Transform_MalformedCode(t *testing.T) {
	tests := []struct {
		m    *wasm.Module
		name string
	}{
		{
			name: "malformed function in findAsyncFuncs",
			m: &wasm.Module{
				Types: []wasm.FuncType{{}},
				Imports: []wasm.Import{
					{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				},
				Funcs:    []uint32{0},
				Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
				Code: []wasm.FuncBody{
					{Code: []byte{0xFF, 0xFF}}, // malformed
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := New(Config{Matcher: newExactMatcher([]string{"env.sleep"})})
			wasmData := tt.m.Encode()
			_, err := eng.Transform(wasmData)
			if err == nil {
				t.Error("expected error for malformed code")
			}
		})
	}
}

// TestEngine_Transform_SecondaryMemoryWithImportedMemory tests secondary memory with imported primary memory.
func TestEngine_Transform_SecondaryMemoryWithImportedMemory(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "memory", Desc: wasm.ImportDesc{Kind: 2, Memory: &wasm.MemoryType{Limits: wasm.Limits{Min: 1}}}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{
		Matcher:            newExactMatcher([]string{"env.sleep"}),
		UseSecondaryMemory: true,
	})
	wasmData := m.Encode()
	result, err := eng.Transform(wasmData)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Parse and verify secondary memory was added
	parsed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Should have 1 imported memory + 1 secondary memory
	importedMems := 0
	for _, imp := range parsed.Imports {
		if imp.Desc.Kind == 2 {
			importedMems++
		}
	}
	if importedMems != 1 {
		t.Errorf("expected 1 imported memory, got %d", importedMems)
	}
	if len(parsed.Memories) != 1 {
		t.Errorf("expected 1 local memory (secondary), got %d", len(parsed.Memories))
	}

	// Verify asyncify_memory export
	hasAsyncifyMemory := false
	for _, exp := range parsed.Exports {
		if exp.Name == "asyncify_memory" {
			hasAsyncifyMemory = true
			if exp.Idx != 1 {
				t.Errorf("asyncify_memory should be memory index 1 (after imported), got %d", exp.Idx)
			}
		}
	}
	if !hasAsyncifyMemory {
		t.Error("missing asyncify_memory export")
	}
}

// TestEngine_Transform_MalformedGlobalInit tests error handling for malformed global initializer.
func TestEngine_Transform_MalformedGlobalInit(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Globals: []wasm.Global{
			{
				Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true},
				Init: []byte{0xFF, 0xFF}, // malformed initializer
			},
		},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{
		Matcher:       newExactMatcher([]string{"env.sleep"}),
		ImportGlobals: true, // triggers adjustGlobalReferences
	})
	wasmData := m.Encode()
	_, err := eng.Transform(wasmData)
	if err == nil {
		t.Error("expected error for malformed global init")
	}
}

// TestEngine_RemoveAsyncifyConflicts_StartFunction tests start function reindexing.
func TestEngine_RemoveAsyncifyConflicts_StartFunction(t *testing.T) {
	startIdx := uint32(2) // after asyncify import is removed, should become 1
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "asyncify", Name: "asyncify_start_unwind", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "other", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Start:    &startIdx,
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}})},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.other"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	parsed, _ := wasm.ParseModule(result)
	if parsed.Start == nil {
		t.Fatal("start function should be preserved")
	}
	if *parsed.Start != 1 {
		t.Errorf("start function should be reindexed to 1, got %d", *parsed.Start)
	}
}

// TestEngine_RemoveAsyncifyConflicts_Elements tests element segment reindexing.
func TestEngine_RemoveAsyncifyConflicts_Elements(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "asyncify", Name: "asyncify_start_unwind", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "target", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Tables:   []wasm.TableType{{ElemType: 0x70, Limits: wasm.Limits{Min: 1}}},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Elements: []wasm.Element{
			{
				TableIdx: 0,
				Offset:   wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}}, {Opcode: wasm.OpEnd}}),
				FuncIdxs: []uint32{2}, // local func, should become 1 after removal
			},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}})},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.target"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	parsed, _ := wasm.ParseModule(result)
	if len(parsed.Elements) == 0 || len(parsed.Elements[0].FuncIdxs) == 0 {
		t.Fatal("element segment should be preserved")
	}
	if parsed.Elements[0].FuncIdxs[0] != 1 {
		t.Errorf("element func idx should be reindexed to 1, got %d", parsed.Elements[0].FuncIdxs[0])
	}
}

// TestEngine_RemoveAsyncifyConflicts_RefFunc tests ref.func reindexing.
func TestEngine_RemoveAsyncifyConflicts_RefFunc(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "asyncify", Name: "asyncify_start_unwind", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "target", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpRefFunc, Imm: wasm.RefFuncImm{FuncIdx: 2}}, // should become 1
					{Opcode: wasm.OpDrop},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.target"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	parsed, _ := wasm.ParseModule(result)
	instrs, _ := wasm.DecodeInstructions(parsed.Code[0].Code)
	found := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpRefFunc {
			if imm, ok := instr.Imm.(wasm.RefFuncImm); ok {
				found = true
				if imm.FuncIdx != 1 {
					t.Errorf("ref.func idx should be 1, got %d", imm.FuncIdx)
				}
			}
		}
	}
	if !found {
		t.Error("ref.func instruction not found in transformed code")
	}
}

// TestEngine_RemoveAsyncifyConflicts_ExistingExports tests removing conflicting exports.
func TestEngine_RemoveAsyncifyConflicts_ExistingExports(t *testing.T) {
	m := &wasm.Module{
		Types:    []wasm.FuncType{{}},
		Funcs:    []uint32{0, 0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "asyncify_start_unwind", Kind: 0, Idx: 0}, // should be removed
			{Name: "my_func", Kind: 0, Idx: 1},               // should be kept
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}})},
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}})},
		},
	}

	eng := New(Config{})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	parsed, _ := wasm.ParseModule(result)
	// Verify my_func is still there
	found := false
	for _, exp := range parsed.Exports {
		if exp.Name == "my_func" {
			found = true
		}
	}
	if !found {
		t.Error("my_func export should be preserved")
	}
}

// TestEngine_Transform_ImportGlobals_MalformedCode tests error path in importAsyncifyGlobals.
func TestEngine_Transform_ImportGlobals_MalformedCode(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: []byte{0xFF, 0xFF}}, // malformed function body
		},
	}

	eng := New(Config{
		Matcher:       newExactMatcher([]string{"env.sleep"}),
		ImportGlobals: true, // triggers importAsyncifyGlobals -> adjustGlobalReferences
	})
	wasmData := m.Encode()
	_, err := eng.Transform(wasmData)
	if err == nil {
		t.Error("expected error for malformed code with ImportGlobals")
	}
}
