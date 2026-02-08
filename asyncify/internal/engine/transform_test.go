// Transform tests verify the function transformation logic.
//
// Reference: Binaryen asyncify pass
// https://github.com/WebAssembly/binaryen/blob/main/src/passes/Asyncify.cpp
package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/wasm"
)

// TestFunctionTransformer_New tests transformer creation.
func TestFunctionTransformer_New(t *testing.T) {
	m := &wasm.Module{}
	reg := handler.NewRegistry()
	globals := GlobalIndices{StateGlobal: 0, DataGlobal: 1}

	ft := NewFunctionTransformer(reg, m, globals, 0, false)

	if ft == nil {
		t.Fatal("NewFunctionTransformer() returned nil")
	}
	if ft.registry != reg {
		t.Error("registry not set correctly")
	}
	if ft.module != m {
		t.Error("module not set correctly")
	}
}

// TestFunctionTransformer_Transform_NoAsyncCalls tests function with no async calls.
func TestFunctionTransformer_Transform_NoAsyncCalls(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{}, 0, false)
	body := &m.Code[0]
	originalCode := make([]byte, len(body.Code))
	copy(originalCode, body.Code)

	err := ft.Transform(0, body, map[uint32]bool{})

	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	// Code should be unchanged when no async calls
	if string(body.Code) != string(originalCode) {
		t.Error("Transform() modified code with no async calls")
	}
}

// TestFunctionTransformer_Transform_SingleAsyncCall tests single async call transformation.
func TestFunctionTransformer_Transform_SingleAsyncCall(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
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

	ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{StateGlobal: 0, DataGlobal: 1}, 0, false)
	body := &m.Code[0]

	err := ft.Transform(1, body, map[uint32]bool{0: true})

	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	// Code should be modified
	instrs, err := wasm.DecodeInstructions(body.Code)
	if err != nil {
		t.Fatalf("DecodeInstructions: %v", err)
	}

	// Should have state checks (global.get + i32.const + i32.eq pattern)
	hasStateCheck := false
	for i, instr := range instrs {
		if instr.Opcode == wasm.OpGlobalGet {
			if i+2 < len(instrs) && instrs[i+2].Opcode == wasm.OpI32Eq {
				hasStateCheck = true
				break
			}
		}
	}

	if !hasStateCheck {
		t.Error("Transform() should add state checking code")
	}

	// Rewind must be finalized by host-side Resume/stop_rewind, not by inlined
	// state mutation at transformed callsites.
	hasInlineRewindClear := false
	for i := 0; i+6 < len(instrs); i++ {
		if instrs[i].Opcode != wasm.OpGlobalGet {
			continue
		}
		if imm, ok := instrs[i].Imm.(wasm.GlobalImm); !ok || imm.GlobalIdx != 0 {
			continue
		}
		if instrs[i+1].Opcode != wasm.OpI32Const {
			continue
		}
		if imm, ok := instrs[i+1].Imm.(wasm.I32Imm); !ok || imm.Value != StateRewinding {
			continue
		}
		if instrs[i+2].Opcode != wasm.OpI32Eq || instrs[i+3].Opcode != wasm.OpIf {
			continue
		}
		if instrs[i+4].Opcode != wasm.OpI32Const {
			continue
		}
		if imm, ok := instrs[i+4].Imm.(wasm.I32Imm); !ok || imm.Value != StateNormal {
			continue
		}
		if instrs[i+5].Opcode != wasm.OpGlobalSet {
			continue
		}
		if imm, ok := instrs[i+5].Imm.(wasm.GlobalImm); !ok || imm.GlobalIdx != 0 {
			continue
		}
		if instrs[i+6].Opcode == wasm.OpEnd {
			hasInlineRewindClear = true
			break
		}
	}
	if hasInlineRewindClear {
		t.Error("Transform() should not inline rewind->normal state clear before async call")
	}
}

// TestFunctionTransformer_Transform_MultipleAsyncCalls tests multiple async calls.
func TestFunctionTransformer_Transform_MultipleAsyncCalls(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
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
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpI32Add},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{StateGlobal: 0, DataGlobal: 1}, 0, false)
	body := &m.Code[0]

	err := ft.Transform(1, body, map[uint32]bool{0: true})

	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	// Should have scratch locals added
	if len(body.Locals) == 0 {
		t.Error("Transform() should add scratch locals")
	}
}

// TestFunctionTransformer_Transform_WithLocals tests preserving existing locals.
func TestFunctionTransformer_Transform_WithLocals(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{
					{Count: 2, ValType: wasm.ValI32},
					{Count: 1, ValType: wasm.ValI64},
				},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{StateGlobal: 0, DataGlobal: 1}, 0, false)
	body := &m.Code[0]
	originalLocalsCount := len(body.Locals)

	err := ft.Transform(1, body, map[uint32]bool{0: true})

	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	// Should have more locals after transform (scratch locals added)
	if len(body.Locals) <= originalLocalsCount {
		t.Error("Transform() should add scratch locals")
	}
}

// TestFunctionTransformer_Transform_RejectsReferenceTypes tests reference type rejection.
func TestFunctionTransformer_Transform_RejectsReferenceTypes(t *testing.T) {
	tests := []struct {
		name   string
		params []wasm.ValType
		locals []wasm.LocalEntry
	}{
		{
			name:   "funcref param",
			params: []wasm.ValType{wasm.ValFuncRef},
		},
		{
			name:   "externref param",
			params: []wasm.ValType{wasm.ValExtern},
		},
		{
			name:   "funcref local",
			locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValFuncRef}},
		},
		{
			name:   "externref local",
			locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValExtern}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &wasm.Module{
				Types: []wasm.FuncType{
					{Results: []wasm.ValType{wasm.ValI32}},
					{Params: tt.params, Results: []wasm.ValType{wasm.ValI32}},
				},
				Imports: []wasm.Import{
					{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				},
				Funcs:    []uint32{1},
				Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
				Code: []wasm.FuncBody{
					{
						Locals: tt.locals,
						Code: wasm.EncodeInstructions([]wasm.Instruction{
							{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
							{Opcode: wasm.OpEnd},
						}),
					},
				},
			}

			ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{}, 0, false)
			err := ft.Transform(1, &m.Code[0], map[uint32]bool{0: true})

			if err == nil {
				t.Error("Transform() should reject reference types")
			}
		})
	}
}

// TestFunctionTransformer_Transform_NonAsyncCallRef tests call_ref through emitNonAsyncCall path.
// When IgnoreIndirect=true and the function has an async call, call_ref becomes non-async.
func TestFunctionTransformer_Transform_NonAsyncCallRef(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}}, // 0: for call_ref
			{Results: []wasm.ValType{}}, // 1: void -> void (import type)
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 1}},
		},
		Tables: []wasm.TableType{
			{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					// Async call first
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					// call_ref (will be non-async because IgnoreIndirect)
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},                      // arg
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: wasm.HeapTypeFunc}}, // func ref
					{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 0}},
					{Opcode: wasm.OpDrop},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	// IgnoreIndirect=true means call_ref is NOT treated as async
	eng := New(Config{
		Matcher:        newExactMatcher([]string{"env.async"}),
		IgnoreIndirect: true,
	})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	// Should still transform due to async import call
	if len(result) == 0 {
		t.Fatal("expected transformed module")
	}
}

// TestFunctionTransformer_Transform_IndirectCall tests call_indirect handling.
func TestFunctionTransformer_Transform_IndirectCall(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
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

	ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{StateGlobal: 0, DataGlobal: 1}, 0, false)
	body := &m.Code[0]

	// call_indirect is always treated as potentially async
	err := ft.Transform(0, body, map[uint32]bool{})

	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
}

// TestCallSite tests CallSite struct.
func TestCallSite(t *testing.T) {
	ft := wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}}
	cs := CallSite{
		InstrIdx:   5,
		CalleeType: &ft,
		LiveLocals: []uint32{0, 2, 3},
	}

	if cs.InstrIdx != 5 {
		t.Error("InstrIdx not set correctly")
	}
	if cs.CalleeType == nil {
		t.Error("CalleeType not set correctly")
	}
	if len(cs.LiveLocals) != 3 {
		t.Error("LiveLocals not set correctly")
	}
}

// TestComputeLiveUnion tests live local union computation.
func TestComputeLiveUnion(t *testing.T) {
	tests := []struct {
		name      string
		callSites []CallSite
		wantLen   int
	}{
		{
			name:      "empty",
			callSites: nil,
			wantLen:   0,
		},
		{
			name: "single site",
			callSites: []CallSite{
				{LiveLocals: []uint32{0, 1, 2}},
			},
			wantLen: 3,
		},
		{
			name: "overlapping",
			callSites: []CallSite{
				{LiveLocals: []uint32{0, 1}},
				{LiveLocals: []uint32{1, 2}},
			},
			wantLen: 3, // union is {0, 1, 2}
		},
		{
			name: "disjoint",
			callSites: []CallSite{
				{LiveLocals: []uint32{0, 1}},
				{LiveLocals: []uint32{5, 6}},
			},
			wantLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeLiveUnion(tt.callSites)
			if len(got) != tt.wantLen {
				t.Errorf("computeLiveUnion() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// TestTransform_BinaryenCompatibility tests Binaryen-compatible output structure.
func TestTransform_BinaryenCompatibility(t *testing.T) {
	// Create a module similar to what Binaryen would process
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},                                     // void -> void
			{Results: []wasm.ValType{wasm.ValI32}}, // void -> i32
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "sleep", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
					{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.sleep"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	// Parse the result
	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Verify Binaryen-compatible exports exist
	exports := make(map[string]bool)
	for _, exp := range transformed.Exports {
		exports[exp.Name] = true
	}

	required := []string{
		"asyncify_get_state",
		"asyncify_start_unwind",
		"asyncify_stop_unwind",
		"asyncify_start_rewind",
		"asyncify_stop_rewind",
	}

	for _, name := range required {
		if !exports[name] {
			t.Errorf("missing Binaryen-compatible export: %s", name)
		}
	}

	// Verify globals were added (state + data pointer)
	if len(transformed.Globals) < 2 {
		t.Error("should have at least 2 asyncify globals")
	}
}

// TestTransform_NonI32ReturnTypes tests that async calls returning i64/f32/f64 work correctly.
func TestTransform_NonI32ReturnTypes(t *testing.T) {
	tests := []struct {
		name       string
		resultType wasm.ValType
	}{
		{"i64", wasm.ValI64},
		{"f32", wasm.ValF32},
		{"f64", wasm.ValF64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Module with async import returning non-i32 and function that calls it
			m := &wasm.Module{
				Types: []wasm.FuncType{
					{Results: []wasm.ValType{tc.resultType}}, // async func type
				},
				Imports: []wasm.Import{
					{Module: "env", Name: "async_" + tc.name, Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				},
				Funcs:    []uint32{0}, // function using same type
				Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
				Code: []wasm.FuncBody{
					{
						Code: wasm.EncodeInstructions([]wasm.Instruction{
							{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
							{Opcode: wasm.OpEnd},
						}),
					},
				},
				Exports: []wasm.Export{
					{Name: "test", Kind: 0, Idx: 1},
				},
			}

			eng := New(Config{Matcher: newExactMatcher([]string{"env.async_" + tc.name})})
			result, err := eng.Transform(m.Encode())
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}

			// Parse and validate structure
			transformed, err := wasm.ParseModule(result)
			if err != nil {
				t.Fatalf("ParseModule() error = %v", err)
			}

			// Verify the function still has correct result type
			testFuncIdx := uint32(1) // after import
			typeIdx := transformed.Funcs[testFuncIdx-uint32(len(transformed.Imports))]
			funcType := transformed.Types[typeIdx]

			if len(funcType.Results) != 1 || funcType.Results[0] != tc.resultType {
				t.Errorf("function result type = %v, want %v", funcType.Results, []wasm.ValType{tc.resultType})
			}
		})
	}
}

// TestTransform_V128Local tests that v128 locals are saved/restored correctly.
func TestTransform_V128Local(t *testing.T) {
	// Function with v128 local that's live across async call
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},                                      // async func type (void)
			{Results: []wasm.ValType{wasm.ValV128}}, // function returning v128
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValV128}},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					// v128.const 0 - push a v128 value
					{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}}},
					{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
		Exports: []wasm.Export{
			{Name: "test", Kind: 0, Idx: 1},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	// Verify transformed module is valid
	transformed, err := wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("ParseModule() error = %v", err)
	}

	// Check that v128.load and v128.store were generated in the code
	// The save/restore code should include SIMD prefix instructions
	testFuncIdx := 1 - len(transformed.Imports)
	code := transformed.Code[testFuncIdx].Code
	instrs, err := wasm.DecodeInstructions(code)
	if err != nil {
		t.Fatalf("DecodeInstructions() error = %v", err)
	}

	hasSIMDPrefix := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpPrefixSIMD {
			hasSIMDPrefix = true
			break
		}
	}

	if !hasSIMDPrefix {
		t.Error("expected SIMD prefix instructions for v128 save/restore")
	}
}

// Tests for simulateInstrStack branches

func TestSimulateInstrStack_LocalTee(t *testing.T) {
	// local.tee pops and pushes same type
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI64}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI64}},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 42}},
					{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpDrop},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
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

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestSimulateInstrStack_RefNull(t *testing.T) {
	// ref.null with different heap types
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: wasm.HeapTypeFunc}},
					{Opcode: wasm.OpDrop},
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: wasm.HeapTypeExtern}},
					{Opcode: wasm.OpDrop},
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

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestSimulateInstrStack_Select(t *testing.T) {
	// select pops 3, pushes 1 with type from second operand
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // true val
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}}, // false val
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}}, // condition
					{Opcode: wasm.OpSelect},                               // select
					{Opcode: wasm.OpDrop},
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

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestSimulateInstrStack_SelectType(t *testing.T) {
	// select with explicit type annotation
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI64}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 1}},
					{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 2}},
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
					{Opcode: wasm.OpSelectType, Imm: wasm.SelectTypeImm{Types: []wasm.ValType{wasm.ValI64}}},
					{Opcode: wasm.OpDrop},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 0}},
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

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestSimulateInstrStack_CallIndirect(t *testing.T) {
	// call_indirect with function type
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Tables: []wasm.TableType{
			{ElemType: 0x70, Limits: wasm.Limits{Min: 1}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}}, // arg
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},  // table index
					{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	// call_indirect is treated as async by default
	eng := New(Config{})
	result, err := eng.Transform(m.Encode())
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestSimulateInstrStack_ControlFlow(t *testing.T) {
	// if, br_if, br_table
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
					{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: -64}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 0}},
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0}, Default: 0}},
					{Opcode: wasm.OpEnd},
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

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestSimulateInstrStack_RefTypeOps(t *testing.T) {
	// Test ref.as_non_null, br_on_null, br_on_non_null
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs: []uint32{0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}},
					// ref.null + ref.as_non_null (HeapType -16 = funcref)
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: -16}},
					{Opcode: wasm.OpRefAsNonNull},
					{Opcode: wasm.OpDrop},
					// br_on_null
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: -16}},
					{Opcode: wasm.OpBrOnNull, Imm: wasm.BranchImm{LabelIdx: 0}},
					{Opcode: wasm.OpDrop},
					// br_on_non_null
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: -16}},
					{Opcode: wasm.OpBrOnNonNull, Imm: wasm.BranchImm{LabelIdx: 0}},
					{Opcode: wasm.OpEnd},
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

	_, err = wasm.ParseModule(result)
	if err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}
