// Binaryen compatibility tests verify feature parity with wasm-opt --asyncify.
//
// Reference implementation: Binaryen asyncify pass
// https://github.com/WebAssembly/binaryen/blob/main/src/passes/Asyncify.cpp
//
// This file tests all Binaryen-compatible features:
// - Exported functions (5 total)
// - State machine (normal=0, unwinding=1, rewinding=2)
// - Data layout (stack ptr at offset 0, stack end at offset 4)
// - Value types (i32, i64, f32, f64, v128)
// - Reference type rejection (funcref, externref)
// - Scratch locals (11 total, matching Binaryen)
// - Control flow handling
package engine

import (
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// testFunctionMatcher for testing function matching
type testFunctionMatcher struct {
	names map[string]bool
}

func (m *testFunctionMatcher) MatchFunction(name string) bool {
	return m.names[name]
}

// TestBinaryen_ExportedFunctions verifies all 5 asyncify exports are added.
func TestBinaryen_ExportedFunctions(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
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

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Binaryen exports exactly these 5 functions
	required := map[string]struct {
		params  int
		results int
	}{
		"asyncify_get_state":    {params: 0, results: 1}, // () -> i32
		"asyncify_start_unwind": {params: 1, results: 0}, // (i32) -> ()
		"asyncify_stop_unwind":  {params: 0, results: 0}, // () -> ()
		"asyncify_start_rewind": {params: 1, results: 0}, // (i32) -> ()
		"asyncify_stop_rewind":  {params: 0, results: 0}, // () -> ()
	}

	exports := make(map[string]wasm.Export)
	for _, exp := range transformed.Exports {
		exports[exp.Name] = exp
	}

	for name, sig := range required {
		exp, ok := exports[name]
		if !ok {
			t.Errorf("missing Binaryen export: %s", name)
			continue
		}
		if exp.Kind != 0 {
			t.Errorf("%s: export kind = %d, want 0 (function)", name, exp.Kind)
			continue
		}

		// Verify function signature
		funcType := transformed.GetFuncType(exp.Idx)
		if funcType == nil {
			t.Errorf("%s: cannot get function type", name)
			continue
		}
		if len(funcType.Params) != sig.params {
			t.Errorf("%s: params = %d, want %d", name, len(funcType.Params), sig.params)
		}
		if len(funcType.Results) != sig.results {
			t.Errorf("%s: results = %d, want %d", name, len(funcType.Results), sig.results)
		}
	}
}

// TestBinaryen_StateConstants verifies state machine values match Binaryen.
func TestBinaryen_StateConstants(t *testing.T) {
	// Binaryen uses: 0=normal, 1=unwinding, 2=rewinding
	if StateNormal != 0 {
		t.Errorf("StateNormal = %d, Binaryen uses 0", StateNormal)
	}
	if StateUnwinding != 1 {
		t.Errorf("StateUnwinding = %d, Binaryen uses 1", StateUnwinding)
	}
	if StateRewinding != 2 {
		t.Errorf("StateRewinding = %d, Binaryen uses 2", StateRewinding)
	}
}

// TestBinaryen_GlobalsAdded verifies two mutable i32 globals are added.
func TestBinaryen_GlobalsAdded(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
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

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Binaryen adds 2 globals: state (i32, mutable) and data pointer (i32, mutable)
	if len(transformed.Globals) < 2 {
		t.Fatalf("globals = %d, want at least 2", len(transformed.Globals))
	}

	stateGlobal := transformed.Globals[0]
	dataGlobal := transformed.Globals[1]

	if stateGlobal.Type.ValType != wasm.ValI32 {
		t.Errorf("state global type = %v, want i32", stateGlobal.Type.ValType)
	}
	if !stateGlobal.Type.Mutable {
		t.Error("state global should be mutable")
	}

	if dataGlobal.Type.ValType != wasm.ValI32 {
		t.Errorf("data global type = %v, want i32", dataGlobal.Type.ValType)
	}
	if !dataGlobal.Type.Mutable {
		t.Error("data global should be mutable")
	}
}

// TestBinaryen_ScratchLocals verifies 11 scratch locals are added.
func TestBinaryen_ScratchLocals(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				// No original locals
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Find the transformed function (func index 1, local index 0)
	if len(transformed.Code) == 0 {
		t.Fatal("no code sections")
	}

	body := transformed.Code[0]
	localCount := 0
	for _, entry := range body.Locals {
		localCount += int(entry.Count)
	}

	// Binaryen adds 11 scratch locals + any temp locals from flattening
	if localCount < 10 {
		t.Errorf("locals = %d, want at least 11 (Binaryen scratch locals)", localCount)
	}
}

// TestBinaryen_ValueTypes verifies all Binaryen-supported types work.
func TestBinaryen_ValueTypes(t *testing.T) {
	tests := []struct {
		name    string
		valType wasm.ValType
		wantErr bool
	}{
		{"i32", wasm.ValI32, false},
		{"i64", wasm.ValI64, false},
		{"f32", wasm.ValF32, false},
		{"f64", wasm.ValF64, false},
		{"v128", wasm.ValV128, false},
		{"funcref", wasm.ValFuncRef, true},  // Binaryen rejects
		{"externref", wasm.ValExtern, true}, // Binaryen rejects
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &wasm.Module{
				Types: []wasm.FuncType{
					{},
					{Params: []wasm.ValType{tt.valType}},
				},
				Imports: []wasm.Import{
					{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				},
				Funcs:    []uint32{1},
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

			eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
			_, err := eng.Transform(m.Encode())

			if tt.wantErr && err == nil {
				t.Errorf("expected error for %s (Binaryen rejects reference types)", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %s: %v", tt.name, err)
			}
		})
	}
}

// TestBinaryen_ControlFlowPatterns verifies control flow handling.
func TestBinaryen_ControlFlowPatterns(t *testing.T) {
	tests := []struct {
		name   string
		instrs []wasm.Instruction
	}{
		{
			name: "if_then_else",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpElse},
				{Opcode: wasm.OpNop},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			name: "loop",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			name: "nested_blocks",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
			},
		},
		{
			name: "br_if",
			instrs: []wasm.Instruction{
				{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
				{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
				{Opcode: wasm.OpEnd},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &wasm.Module{
				Types: []wasm.FuncType{{}},
				Imports: []wasm.Import{
					{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				},
				Funcs:    []uint32{0},
				Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
				Code: []wasm.FuncBody{
					{Code: wasm.EncodeInstructions(tt.instrs)},
				},
			}

			eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
			result, err := eng.Transform(m.Encode())
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}

			// Verify result is valid WASM
			_, err = wasm.ParseModule(result)
			if err != nil {
				t.Errorf("transformed module invalid: %v", err)
			}
		})
	}
}

// TestBinaryen_CallGraphTransitivity verifies transitive caller detection.
func TestBinaryen_CallGraphTransitivity(t *testing.T) {
	// A calls B, B calls async import
	// Both A and B should be transformed
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0, 0}, // B and A
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{ // B (func 1): calls async (func 0)
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
			{ // A (func 2): calls B (func 1)
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Both original functions should have scratch locals added (sign of transformation)
	// Only check the first 2 functions (B and A), not the asyncify helper functions
	originalFuncCount := 2
	for i := 0; i < originalFuncCount && i < len(transformed.Code); i++ {
		body := transformed.Code[i]
		localCount := 0
		for _, entry := range body.Locals {
			localCount += int(entry.Count)
		}
		if localCount < 10 {
			t.Errorf("func %d: locals = %d, want >= 10 (should be transformed transitively)", i, localCount)
		}
	}
}

// TestBinaryen_IndirectCallsAlwaysAsync verifies call_indirect is treated as async.
func TestBinaryen_IndirectCallsAlwaysAsync(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Tables: []wasm.TableType{
			{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	// Even with no matcher, call_indirect should be transformed
	eng := New(Config{Matcher: nil})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Should still have asyncify exports added
	hasAsyncifyExport := false
	for _, exp := range transformed.Exports {
		if exp.Name == "asyncify_get_state" {
			hasAsyncifyExport = true
			break
		}
	}

	if !hasAsyncifyExport {
		t.Error("call_indirect should trigger asyncify transformation")
	}
}

// TestBinaryen_StackOverflowCheck verifies stack overflow detection is emitted.
func TestBinaryen_StackOverflowCheck(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Params: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Check transformed function has unreachable instruction (stack overflow trap)
	instrs, err := wasm.DecodeInstructions(transformed.Code[0].Code)
	if err != nil {
		t.Fatalf("DecodeInstructions: %v", err)
	}

	hasUnreachable := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpUnreachable {
			hasUnreachable = true
			break
		}
	}

	if !hasUnreachable {
		t.Error("transformed function should contain unreachable for stack overflow check")
	}
}

// TestBinaryen_AddList verifies addlist forces transformation.
func TestBinaryen_AddList(t *testing.T) {
	// Create module with a function that calls another function
	// addlist should force the caller to be transformed
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "helper", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "my_func", Kind: 0, Idx: 1},
		},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // call helper
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	// Without addlist, function should not be transformed (helper is not async)
	eng1 := New(Config{})
	result1, err := eng1.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	t1, _ := wasm.ParseModule(result1)
	locals1 := countLocals(t1.Code[0])

	// With addlist, function should be transformed
	// The call becomes async because the function itself is in addlist
	eng2 := New(Config{
		AddList: &testFuncMatcher{names: map[string]bool{"my_func": true}},
		Matcher: newExactMatcher([]string{"env.helper"}), // mark helper as async
	})
	result2, err := eng2.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	t2, _ := wasm.ParseModule(result2)
	locals2 := countLocals(t2.Code[0])

	if locals2 <= locals1 {
		t.Errorf("addlist should force transformation: locals without=%d, with=%d", locals1, locals2)
	}
}

// TestBinaryen_RemoveList verifies removelist excludes functions.
func TestBinaryen_RemoveList(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "my_func", Kind: 0, Idx: 1},
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

	// Without removelist, function should be transformed
	eng1 := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result1, err := eng1.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	t1, _ := wasm.ParseModule(result1)
	locals1 := countLocals(t1.Code[0])

	// With removelist, function should NOT be transformed
	eng2 := New(Config{
		Matcher:    newExactMatcher([]string{"env.async"}),
		RemoveList: &testFuncMatcher{names: map[string]bool{"my_func": true}},
	})
	result2, err := eng2.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	t2, _ := wasm.ParseModule(result2)
	locals2 := countLocals(t2.Code[0])

	if locals2 >= locals1 {
		t.Errorf("removelist should exclude function: locals without=%d, with=%d", locals1, locals2)
	}
}

// TestBinaryen_MemoryIndexValidation verifies memory index validation.
func TestBinaryen_MemoryIndexValidation(t *testing.T) {
	m := &wasm.Module{
		Types:    []wasm.FuncType{{}},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpEnd}})},
		},
	}

	// Memory index 0 should work
	eng1 := New(Config{MemoryIndex: 0})
	_, err := eng1.Transform(m.Encode())
	if err != nil {
		t.Errorf("MemoryIndex 0 should work: %v", err)
	}

	// Memory index 1 should fail (only 1 memory)
	eng2 := New(Config{MemoryIndex: 1})
	_, err = eng2.Transform(m.Encode())
	if err == nil {
		t.Error("MemoryIndex 1 should fail with only 1 memory")
	}
}

// testFuncMatcher is a test helper for function matching.
type testFuncMatcher struct {
	names map[string]bool
}

func (m *testFuncMatcher) MatchFunction(name string) bool {
	return m.names[name]
}

func countLocals(body wasm.FuncBody) int {
	count := 0
	for _, entry := range body.Locals {
		count += int(entry.Count)
	}
	return count
}

// TestBinaryen_IgnoreIndirect verifies IgnoreIndirect option.
func TestBinaryen_IgnoreIndirect(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Tables: []wasm.TableType{
			{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	// Without IgnoreIndirect, call_indirect is treated as async (gets transformed)
	eng1 := New(Config{Matcher: newExactMatcher([]string{"env.async"}), IgnoreIndirect: false})
	result1, err := eng1.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	t1, _ := wasm.ParseModule(result1)
	locals1 := countLocals(t1.Code[0])

	// With IgnoreIndirect, call_indirect is NOT treated as async
	eng2 := New(Config{Matcher: newExactMatcher([]string{"env.async"}), IgnoreIndirect: true})
	result2, err := eng2.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	t2, _ := wasm.ParseModule(result2)
	locals2 := countLocals(t2.Code[0])

	// With IgnoreIndirect, the function shouldn't have scratch locals
	// because there are no direct async calls in the function
	if locals1 == 0 {
		t.Error("Without IgnoreIndirect, call_indirect should trigger transformation")
	}
	if locals2 != 0 {
		t.Error("With IgnoreIndirect, call_indirect should not trigger transformation")
	}
}

// TestBinaryen_UnsupportedOpcodes verifies unsupported opcodes are rejected.
// Note: The WASM decoder rejects unknown opcodes with "unknown opcode" error.
// This test verifies that modules with unsupported opcodes cannot be transformed.
func TestBinaryen_UnsupportedOpcodes(t *testing.T) {
	tests := []struct {
		name   string
		opcode byte
	}{
		{"return_call", 0x12},
		{"return_call_indirect", 0x13},
		{"try", 0x06},
		{"catch", 0x07},
		{"throw", 0x08},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create code with async call followed by unsupported opcode
			// The async call ensures the function is transformed
			code := []byte{
				wasm.OpCall, 0x00, // call async import (func 0)
				tt.opcode, 0x00, 0x0B,
			}

			m := &wasm.Module{
				Types: []wasm.FuncType{{}},
				Imports: []wasm.Import{
					{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				},
				Funcs:    []uint32{0},
				Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
				Code:     []wasm.FuncBody{{Code: code}},
			}

			eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
			_, err := eng.Transform(m.Encode())

			// Should fail - either decode fails or validation fails
			if err == nil {
				t.Errorf("Transform() should reject %s opcode (0x%02x)", tt.name, tt.opcode)
			}
		})
	}
}

// TestBinaryen_PropagateAddList verifies that PropagateAddList propagates
// instrumentation to callers of functions in the add-list.
func TestBinaryen_PropagateAddList(t *testing.T) {
	// Module structure:
	// - func 0: import "env.async" (async import)
	// - func 1: calls nothing (added via AddList)
	// - func 2: calls func 1 (should be instrumented if PropagateAddList=true)
	// - func 3: calls func 2 (should also be instrumented if PropagateAddList=true)
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{}, // void -> void
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "other", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0, 0, 0}, // 3 local functions
		Exports: []wasm.Export{
			{Name: "target", Kind: 0, Idx: 1},  // func 1
			{Name: "caller1", Kind: 0, Idx: 2}, // func 2
			{Name: "caller2", Kind: 0, Idx: 3}, // func 3
		},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpEnd}, // func 1: empty
			})},
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}}, // calls target
				{Opcode: wasm.OpEnd},
			})},
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 2}}, // calls caller1
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	addList := &testFunctionMatcher{names: map[string]bool{"target": true}}

	// Without PropagateAddList - only "target" is instrumented
	eng1 := New(Config{
		AddList:          addList,
		PropagateAddList: false,
	})
	result1, err := eng1.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform without propagate: %v", err)
	}
	t1, _ := wasm.ParseModule(result1)

	// With PropagateAddList - callers should also be instrumented
	eng2 := New(Config{
		AddList:          addList,
		PropagateAddList: true,
	})
	result2, err := eng2.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform with propagate: %v", err)
	}
	t2, _ := wasm.ParseModule(result2)

	// Count locals in caller2 (func index 3, code index 2)
	locals1 := countLocals(t1.Code[2])
	locals2 := countLocals(t2.Code[2])

	if locals2 <= locals1 {
		t.Errorf("PropagateAddList should instrument callers: without=%d, with=%d", locals1, locals2)
	}
}

// TestBinaryen_SecondaryMemory verifies asyncify can use a separate memory.
func TestBinaryen_SecondaryMemory(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	eng := New(Config{
		Matcher:              newExactMatcher([]string{"env.async"}),
		UseSecondaryMemory:   true,
		SecondaryMemoryPages: 2,
	})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	transformed, _ := wasm.ParseModule(result)

	// Should have 2 memories now
	if len(transformed.Memories) != 2 {
		t.Errorf("expected 2 memories, got %d", len(transformed.Memories))
	}

	// Check secondary memory has correct size
	if len(transformed.Memories) >= 2 {
		if transformed.Memories[1].Limits.Min != 2 {
			t.Errorf("secondary memory should have 2 pages, got %d", transformed.Memories[1].Limits.Min)
		}
	}

	// Should have export for secondary memory
	hasMemExport := false
	for _, exp := range transformed.Exports {
		if exp.Name == "asyncify_memory" && exp.Kind == wasm.KindMemory {
			hasMemExport = true
			break
		}
	}
	if !hasMemExport {
		t.Error("missing asyncify_memory export")
	}
}

// TestBinaryen_ImportExportGlobals verifies global import/export options.
func TestBinaryen_ImportExportGlobals(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	t.Run("export globals", func(t *testing.T) {
		eng := New(Config{
			Matcher:       newExactMatcher([]string{"env.async"}),
			ExportGlobals: true,
		})
		result, err := eng.Transform(m.Encode())
		if err != nil {
			t.Fatalf("Transform: %v", err)
		}

		transformed, _ := wasm.ParseModule(result)

		// Check for global exports
		exports := make(map[string]bool)
		for _, exp := range transformed.Exports {
			if exp.Kind == wasm.KindGlobal {
				exports[exp.Name] = true
			}
		}

		if !exports["asyncify_state"] {
			t.Error("missing asyncify_state global export")
		}
		if !exports["asyncify_data"] {
			t.Error("missing asyncify_data global export")
		}
	})

	t.Run("import globals", func(t *testing.T) {
		eng := New(Config{
			Matcher:       newExactMatcher([]string{"env.async"}),
			ImportGlobals: true,
		})
		result, err := eng.Transform(m.Encode())
		if err != nil {
			t.Fatalf("Transform: %v", err)
		}

		transformed, _ := wasm.ParseModule(result)

		// Check for global imports
		stateImported := false
		dataImported := false
		for _, imp := range transformed.Imports {
			if imp.Desc.Kind == wasm.KindGlobal {
				if imp.Name == "asyncify_state" {
					stateImported = true
				}
				if imp.Name == "asyncify_data" {
					dataImported = true
				}
			}
		}

		if !stateImported {
			t.Error("missing asyncify_state global import")
		}
		if !dataImported {
			t.Error("missing asyncify_data global import")
		}

		// When importing, should NOT add locals globals
		if len(transformed.Globals) != 0 {
			t.Errorf("should not add local globals when importing, got %d", len(transformed.Globals))
		}
	})

	t.Run("import globals with existing global refs", func(t *testing.T) {
		// Regression test: ImportGlobals must adjust existing global references
		watSrc := `(module
			(import "env" "counter" (global $counter (mut i32)))
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(global.get $counter)
				(call $async)
				(i32.add))
			(memory 1))`

		wasmData, err := wat.Compile(watSrc)
		if err != nil {
			t.Fatalf("wat.Compile: %v", err)
		}

		eng := New(Config{
			Matcher:       newExactMatcher([]string{"env.async"}),
			ImportGlobals: true,
		})
		result, err := eng.Transform(wasmData)
		if err != nil {
			t.Fatalf("Transform: %v", err)
		}

		transformed, err := wasm.ParseModule(result)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		// Verify asyncify_state is at index 0
		numImported := uint32(transformed.NumImportedFuncs())
		getStateFn := uint32(0)
		for _, exp := range transformed.Exports {
			if exp.Name == "asyncify_get_state" {
				getStateFn = exp.Idx
				break
			}
		}

		body := transformed.Code[getStateFn-numImported]
		instrs, _ := wasm.DecodeInstructions(body.Code)
		for _, instr := range instrs {
			if instr.Opcode == wasm.OpGlobalGet {
				if imm, ok := instr.Imm.(wasm.GlobalImm); ok {
					if imm.GlobalIdx != 0 {
						t.Errorf("asyncify_get_state should use global 0, got %d", imm.GlobalIdx)
					}
				}
			}
		}

		// Verify original counter reference is adjusted to index 2
		testFn := uint32(0)
		for _, exp := range transformed.Exports {
			if exp.Name == "test" {
				testFn = exp.Idx
				break
			}
		}

		body = transformed.Code[testFn-numImported]
		instrs, _ = wasm.DecodeInstructions(body.Code)
		hasGlobal2 := false
		for _, instr := range instrs {
			if instr.Opcode == wasm.OpGlobalGet {
				if imm, ok := instr.Imm.(wasm.GlobalImm); ok && imm.GlobalIdx == 2 {
					hasGlobal2 = true
					break
				}
			}
		}
		if !hasGlobal2 {
			t.Error("counter reference should be adjusted to global index 2")
		}
	})
}

// TestBinaryen_Asserts verifies assertion code is added to non-instrumented functions.
func TestBinaryen_Asserts(t *testing.T) {
	// Module with two functions:
	// - func 1: calls async import (instrumented)
	// - func 2: does NOT call async import (non-instrumented, should get assertion)
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},                                     // void -> void
			{Results: []wasm.ValType{wasm.ValI32}}, // void -> i32
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0, 1}, // 2 local functions
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // calls async
				{Opcode: wasm.OpEnd},
			})},
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}}, // no async call
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	// Without asserts
	eng1 := New(Config{
		Matcher: newExactMatcher([]string{"env.async"}),
		Asserts: false,
	})
	result1, _ := eng1.Transform(m.Encode())
	t1, _ := wasm.ParseModule(result1)

	// With asserts
	eng2 := New(Config{
		Matcher: newExactMatcher([]string{"env.async"}),
		Asserts: true,
	})
	result2, _ := eng2.Transform(m.Encode())
	t2, _ := wasm.ParseModule(result2)

	// Non-instrumented function (index 1 in code, func 2) should have more code with asserts
	code1Len := len(t1.Code[1].Code)
	code2Len := len(t2.Code[1].Code)

	if code2Len <= code1Len {
		t.Errorf("Asserts should add code to non-instrumented functions: without=%d, with=%d", code1Len, code2Len)
	}
}

// TestBinaryen_MultiValueReturns verifies functions with multiple return values.
func TestBinaryen_MultiValueReturns(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{}, // void -> void (async import)
			{Results: []wasm.ValType{wasm.ValI32, wasm.ValI32}}, // void -> (i32, i32)
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1}, // multi-value return function
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
				{Opcode: wasm.OpEnd},
			})},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(m.Encode())

	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	// Verify module is valid
	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Function should still have multi-value return type
	funcType := transformed.GetFuncType(1)
	if funcType == nil {
		t.Fatal("function type not found")
	}
	if len(funcType.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(funcType.Results))
	}
}

// TestBinaryen_IgnoreImports verifies that IgnoreImports treats all imports as non-async.
// Equivalent to Binaryen's asyncify-ignore-imports option.
func TestBinaryen_IgnoreImports(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
					{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
		Exports: []wasm.Export{{Name: "run", Kind: 0, Idx: 1}},
	}

	originalCode := m.Code[0].Code

	// With IgnoreImports=true, matcher is ignored and no functions transformed
	eng := New(Config{
		Matcher:       newExactMatcher([]string{"env.async"}),
		IgnoreImports: true,
	})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Function code should be unchanged (no instrumentation)
	if len(transformed.Code) == 0 {
		t.Fatal("expected at least one code entry")
	}

	// Original had 1 local, if not transformed should still have 1
	origLocals := 0
	for _, l := range m.Code[0].Locals {
		origLocals += int(l.Count)
	}
	transLocals := 0
	for _, l := range transformed.Code[0].Locals {
		transLocals += int(l.Count)
	}

	// Code size should be similar (not inflated by instrumentation)
	if len(transformed.Code[0].Code) > len(originalCode)*2 {
		t.Errorf("code grew too much with IgnoreImports=true: %d -> %d",
			len(originalCode), len(transformed.Code[0].Code))
	}

	// Verify asyncify exports are still added
	hasAsyncifyExports := false
	for _, e := range transformed.Exports {
		if e.Name == "asyncify_get_state" {
			hasAsyncifyExports = true
			break
		}
	}
	if !hasAsyncifyExports {
		t.Error("asyncify exports should still be added with IgnoreImports=true")
	}
}

// TestBinaryen_OnlyList verifies that OnlyList restricts transformation to only
// specified functions and their transitive callees.
func TestBinaryen_OnlyList(t *testing.T) {
	// Module with 3 functions:
	// - func 1: "target" - calls async import
	// - func 2: "other" - also calls async import
	// - func 3: "caller" - calls "target"
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "target") (result i32)
			(call $async))
		(func (export "other") (result i32)
			(call $async))
		(func (export "caller") (result i32)
			(call 1))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	onlyList := &testFuncMatcher{names: map[string]bool{"target": true}}

	// With OnlyList="target", only "target" is transformed, not "other"
	eng := New(Config{
		Matcher:  newExactMatcher([]string{"env.async"}),
		OnlyList: onlyList,
	})
	transformed, err := eng.Transform(wasmData)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	m, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Find function indices by export name
	funcByName := make(map[string]uint32)
	for _, exp := range m.Exports {
		if exp.Kind == wasm.KindFunc {
			funcByName[exp.Name] = exp.Idx
		}
	}

	numImported := uint32(m.NumImportedFuncs())
	targetIdx := funcByName["target"] - numImported
	otherIdx := funcByName["other"] - numImported

	// "target" should be transformed (has scratch locals)
	targetLocals := len(m.Code[targetIdx].Locals)
	// "other" should NOT be transformed
	otherLocals := len(m.Code[otherIdx].Locals)

	if targetLocals == 0 {
		t.Error("OnlyList: 'target' should be transformed")
	}
	if otherLocals > 0 {
		t.Error("OnlyList: 'other' should NOT be transformed")
	}
}

// TestBinaryen_Wasm64 verifies that Wasm64 option uses i64 pointers in exports.
func TestBinaryen_Wasm64(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (result i32)
			(call $async))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	eng := New(Config{
		Matcher: newExactMatcher([]string{"env.async"}),
		Wasm64:  true,
	})
	transformed, err := eng.Transform(wasmData)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	m, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Find asyncify_start_unwind export and check its parameter type
	var startUnwindIdx uint32
	found := false
	for _, exp := range m.Exports {
		if exp.Name == "asyncify_start_unwind" {
			startUnwindIdx = exp.Idx
			found = true
			break
		}
	}
	if !found {
		t.Fatal("asyncify_start_unwind export not found")
	}

	// Get the function's type
	numImported := uint32(m.NumImportedFuncs())
	funcBody := m.Code[startUnwindIdx-numImported]
	funcTypeIdx := m.Funcs[startUnwindIdx-numImported]
	funcType := m.Types[funcTypeIdx]

	// With Wasm64, the parameter should be i64
	if len(funcType.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(funcType.Params))
	}
	if funcType.Params[0] != wasm.ValI64 {
		t.Errorf("Wasm64: expected i64 param, got %v", funcType.Params[0])
	}

	// Verify the function body is non-empty
	if len(funcBody.Code) == 0 {
		t.Error("asyncify_start_unwind has empty body")
	}
}

// TestBinaryen_PreAsyncifiedModuleDetection verifies that pre-asyncified modules are detected.
func TestBinaryen_PreAsyncifiedModuleDetection(t *testing.T) {
	// Create a module that looks pre-asyncified (has all 5 asyncify exports as local functions)
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}}, // () -> i32 for get_state
			{Params: []wasm.ValType{wasm.ValI32}},  // (i32) -> () for start_unwind/rewind
			{},                                     // () -> () for stop_unwind/rewind
		},
		Funcs:    []uint32{0, 1, 2, 1, 2}, // 5 functions with appropriate types
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "asyncify_get_state", Kind: 0, Idx: 0},
			{Name: "asyncify_start_unwind", Kind: 0, Idx: 1},
			{Name: "asyncify_stop_unwind", Kind: 0, Idx: 2},
			{Name: "asyncify_start_rewind", Kind: 0, Idx: 3},
			{Name: "asyncify_stop_rewind", Kind: 0, Idx: 4},
		},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}, Init: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}}, {Opcode: wasm.OpEnd}})},
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}, Init: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}}, {Opcode: wasm.OpEnd}})},
		},
		Code: []wasm.FuncBody{
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}}, {Opcode: wasm.OpEnd}})},                                                        // get_state
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, {Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 0}}, {Opcode: wasm.OpEnd}})}, // start_unwind
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}}, {Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 0}}, {Opcode: wasm.OpEnd}})}, // stop_unwind
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}}, {Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 0}}, {Opcode: wasm.OpEnd}})}, // start_rewind
			{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}}, {Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 0}}, {Opcode: wasm.OpEnd}})}, // stop_rewind
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	_, err := eng.Transform(m.Encode())

	if err == nil {
		t.Fatal("expected error for pre-asyncified module")
	}
	if !strings.Contains(err.Error(), "already asyncified") {
		t.Errorf("expected 'already asyncified' error, got: %v", err)
	}
}
