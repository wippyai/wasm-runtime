package asyncify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestTransform_NoAsyncFuncs(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32, wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "add", Kind: 0, Idx: 0},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
				{Opcode: wasm.OpI32Add},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.sleep"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	exports := make(map[string]bool)
	for _, exp := range out.Exports {
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

func TestTransform_WithAsyncImport(t *testing.T) {
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
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.sleep"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Globals) < 2 {
		t.Errorf("expected at least 2 globals, got %d", len(out.Globals))
	}

	if len(out.Code) == 0 {
		t.Fatal("no code sections")
	}
	if len(out.Code[0].Code) < 20 {
		t.Errorf("transformed code seems too small: %d bytes", len(out.Code[0].Code))
	}
}

func TestTransform_PreservesExports(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{Results: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1, 2},
		Exports: []wasm.Export{
			{Name: "foo", Kind: 0, Idx: 1},
			{Name: "bar", Kind: 0, Idx: 2},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpEnd},
			})},
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.sleep"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	exports := make(map[string]bool)
	for _, exp := range out.Exports {
		exports[exp.Name] = true
	}

	if !exports["foo"] {
		t.Error("missing original export: foo")
	}
	if !exports["bar"] {
		t.Error("missing original export: bar")
	}
	if !exports["asyncify_get_state"] {
		t.Error("missing asyncify_get_state")
	}
}

func TestTransform_TransitiveAsync(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1, 1},
		Exports: []wasm.Export{
			{Name: "outer", Kind: 0, Idx: 2},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 50}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.sleep"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	numUserFuncs := 2
	for i := 0; i < numUserFuncs; i++ {
		body := out.Code[i]
		if len(body.Locals) < 10 {
			t.Errorf("func %d: expected at least 10 scratch locals, got %d", i, len(body.Locals))
		}
	}
}

func TestTransform_AddsMemory(t *testing.T) {
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
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.sleep"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Memories) != 1 {
		t.Errorf("expected 1 memory (added by asyncify), got %d", len(out.Memories))
	}
}

func TestTransform_PreservesMemory(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 2}}},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.sleep"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Memories) != 1 {
		t.Errorf("expected 1 memory, got %d", len(out.Memories))
	}
	if out.Memories[0].Limits.Min != 2 {
		t.Errorf("expected memory min=2, got %d", out.Memories[0].Limits.Min)
	}
}

func TestTransform_NilMatcher(t *testing.T) {
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
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: nil,
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result is invalid WASM: %v", err)
	}
}

func TestTransform_InvalidWasm(t *testing.T) {
	_, err := Transform([]byte{0x00, 0x01, 0x02, 0x03}, Config{})
	if err == nil {
		t.Error("expected error for invalid WASM")
	}
}

func TestTransform_GlobalIndices(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Globals: []wasm.Global{
			{
				Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true},
				Init: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.sleep"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Globals) != 3 {
		t.Errorf("expected 3 globals (1 existing + 2 asyncify), got %d", len(out.Globals))
	}
}

func TestTransform_WITMatcher(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
		},
		Imports: []wasm.Import{
			{Module: "wasi:io/poll@0.2.0", Name: "block", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "run", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewWITMatcher([]string{"wasi:io/poll#block"}),
	})
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

// Integration tests using WAT fixtures

func watToWasm(t *testing.T, watPath string) []byte {
	t.Helper()

	content, err := os.ReadFile(watPath)
	if err != nil {
		t.Fatalf("reading WAT file: %v", err)
	}
	wasmData, err := wat.Compile(string(content))
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	return wasmData
}

func TestTransform_FromWATFixture(t *testing.T) {
	watPath := filepath.Join("testdata", "simple_async.wat")
	if _, err := os.Stat(watPath); os.IsNotExist(err) {
		t.Skip("testdata/simple_async.wat not found")
	}

	wasmData := watToWasm(t, watPath)

	result, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.get_value"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Verify asyncify exports added
	exports := make(map[string]bool)
	for _, exp := range out.Exports {
		exports[exp.Name] = true
	}

	required := []string{
		"test",
		"asyncify_get_state",
		"asyncify_start_unwind",
		"asyncify_stop_unwind",
		"asyncify_start_rewind",
		"asyncify_stop_rewind",
	}
	for _, name := range required {
		if !exports[name] {
			t.Errorf("missing export: %s", name)
		}
	}

	// Verify globals added
	if len(out.Globals) < 2 {
		t.Errorf("expected at least 2 asyncify globals, got %d", len(out.Globals))
	}

	// Verify function was transformed (has scratch locals)
	if len(out.Code) > 0 && len(out.Code[0].Locals) < 10 {
		t.Errorf("expected at least 10 scratch locals, got %d", len(out.Code[0].Locals))
	}
}

func TestTransform_OutputStructure(t *testing.T) {
	// Build a simple module with async import
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "get_value", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.get_value"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Verify transformed function structure
	if len(out.Code) == 0 {
		t.Fatal("no code sections")
	}

	body := out.Code[0]

	// Must have scratch locals
	if len(body.Locals) < 10 {
		t.Errorf("expected at least 10 scratch locals, got %d", len(body.Locals))
	}

	// Decode instructions and check for asyncify patterns
	instrs, err := wasm.DecodeInstructions(body.Code)
	if err != nil {
		t.Fatalf("decode instructions: %v", err)
	}

	// Check for global.get (state check)
	hasGlobalGet := false
	hasStateCheck := false
	hasBlock := false

	for _, instr := range instrs {
		switch instr.Opcode {
		case wasm.OpGlobalGet:
			hasGlobalGet = true
		case wasm.OpI32Eq:
			hasStateCheck = true
		case wasm.OpBlock:
			hasBlock = true
		}
	}

	if !hasGlobalGet {
		t.Error("transformed code should have global.get for state check")
	}
	if !hasStateCheck {
		t.Error("transformed code should have i32.eq for state comparison")
	}
	if !hasBlock {
		t.Error("transformed code should have block instructions")
	}
}

func TestTransform_MultipleAsyncCalls(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async_op", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 3}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.async_op"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Count call instructions in transformed code
	instrs, err := wasm.DecodeInstructions(out.Code[0].Code)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	callCount := 0
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpCall {
			if imm, ok := instr.Imm.(wasm.CallImm); ok && imm.FuncIdx == 0 {
				callCount++
			}
		}
	}

	// Each async call should still exist
	if callCount < 3 {
		t.Errorf("expected at least 3 calls to async import, got %d", callCount)
	}
}

func TestTransform_AsyncImportsConfig(t *testing.T) {
	// Test asyncImportMatcher via AsyncImports config
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}},
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "env", Name: "other", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 1}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 2},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // sleep
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}}, // other
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	// Test with pattern matching module.name format
	result, err := Transform(m.Encode(), Config{
		AsyncImports: []string{"env.sleep"},
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Should have transformed (has scratch locals)
	if len(out.Code[0].Locals) < 10 {
		t.Errorf("expected transformation with AsyncImports, got %d locals", len(out.Code[0].Locals))
	}
}

func TestTransform_AsyncImportsWithFallback(t *testing.T) {
	// Test asyncImportMatcher with fallback matcher
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
			{Module: "wasi", Name: "wait", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 2},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	// AsyncImports + Matcher fallback
	result, err := Transform(m.Encode(), Config{
		AsyncImports: []string{"env.sleep"},
		Matcher:      NewExactMatcher([]string{"wasi.wait"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Both should be detected as async
	if len(out.Code[0].Locals) < 10 {
		t.Error("expected both imports to trigger transformation")
	}
}

func TestTransform_AsyncImportsNameOnly(t *testing.T) {
	// Test pattern matching name-only format
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "anymodule", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	// Match by name only (not module.name)
	result, err := Transform(m.Encode(), Config{
		AsyncImports: []string{"sleep"},
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if len(out.Code[0].Locals) < 10 {
		t.Error("expected name-only pattern to match")
	}
}

func TestTransform_NonI32Types(t *testing.T) {
	// Test with i64, f32, f64 parameters and returns
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI64}, Results: []wasm.ValType{wasm.ValF64}},
			{Params: []wasm.ValType{wasm.ValF32}, Results: []wasm.ValType{wasm.ValI64}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "process", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 100}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpDrop},
				{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	result, err := Transform(m.Encode(), Config{
		Matcher: NewExactMatcher([]string{"env.process"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	out, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Verify it produces valid WASM
	if len(out.Code) == 0 {
		t.Fatal("no code sections")
	}
}
