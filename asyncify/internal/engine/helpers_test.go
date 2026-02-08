package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TestHelperBuilder_BuildGetState tests get_state helper generation.
func TestHelperBuilder_BuildGetState(t *testing.T) {
	hb := NewHelperBuilder(GlobalIndices{StateGlobal: 5, DataGlobal: 6})
	code := hb.BuildGetState()

	if len(code) == 0 {
		t.Error("BuildGetState() returned empty code")
	}

	// Should contain global.get and end
	instrs, err := wasm.DecodeInstructions(code)
	if err != nil {
		t.Fatalf("DecodeInstructions: %v", err)
	}

	foundGlobalGet := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpGlobalGet {
			if imm, ok := instr.Imm.(wasm.GlobalImm); ok && imm.GlobalIdx == 5 {
				foundGlobalGet = true
			}
		}
	}

	if !foundGlobalGet {
		t.Error("BuildGetState() should contain global.get for state global")
	}
}

// TestHelperBuilder_BuildStartUnwind tests start_unwind helper generation.
func TestHelperBuilder_BuildStartUnwind(t *testing.T) {
	hb := NewHelperBuilder(GlobalIndices{StateGlobal: 5, DataGlobal: 6})
	code := hb.BuildStartUnwind()

	if len(code) == 0 {
		t.Error("BuildStartUnwind() returned empty code")
	}

	instrs, err := wasm.DecodeInstructions(code)
	if err != nil {
		t.Fatalf("DecodeInstructions: %v", err)
	}

	// Should set state to 1 (unwinding) and store data pointer
	var foundStateSet, foundDataSet bool
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpGlobalSet {
			if imm, ok := instr.Imm.(wasm.GlobalImm); ok {
				if imm.GlobalIdx == 5 {
					foundStateSet = true
				}
				if imm.GlobalIdx == 6 {
					foundDataSet = true
				}
			}
		}
	}

	if !foundStateSet {
		t.Error("BuildStartUnwind() should set state global")
	}
	if !foundDataSet {
		t.Error("BuildStartUnwind() should set data global")
	}
}

// TestHelperBuilder_BuildStopUnwind tests stop_unwind helper generation.
func TestHelperBuilder_BuildStopUnwind(t *testing.T) {
	hb := NewHelperBuilder(GlobalIndices{StateGlobal: 5, DataGlobal: 6})
	code := hb.BuildStopUnwind()

	if len(code) == 0 {
		t.Error("BuildStopUnwind() returned empty code")
	}

	instrs, err := wasm.DecodeInstructions(code)
	if err != nil {
		t.Fatalf("DecodeInstructions: %v", err)
	}

	// Should set state to 0 (normal)
	foundStateSet := false
	for i, instr := range instrs {
		if instr.Opcode == wasm.OpI32Const {
			if imm, ok := instr.Imm.(wasm.I32Imm); ok && imm.Value == StateNormal {
				// Check next instruction is global.set
				if i+1 < len(instrs) && instrs[i+1].Opcode == wasm.OpGlobalSet {
					foundStateSet = true
				}
			}
		}
	}

	if !foundStateSet {
		t.Error("BuildStopUnwind() should set state to normal (0)")
	}
}

// TestHelperBuilder_BuildStartRewind tests start_rewind helper generation.
func TestHelperBuilder_BuildStartRewind(t *testing.T) {
	hb := NewHelperBuilder(GlobalIndices{StateGlobal: 5, DataGlobal: 6})
	code := hb.BuildStartRewind()

	if len(code) == 0 {
		t.Error("BuildStartRewind() returned empty code")
	}

	instrs, err := wasm.DecodeInstructions(code)
	if err != nil {
		t.Fatalf("DecodeInstructions: %v", err)
	}

	// Should set state to 2 (rewinding)
	foundRewindState := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpI32Const {
			if imm, ok := instr.Imm.(wasm.I32Imm); ok && imm.Value == StateRewinding {
				foundRewindState = true
			}
		}
	}

	if !foundRewindState {
		t.Error("BuildStartRewind() should set state to rewinding (2)")
	}
}

// TestHelperBuilder_BuildStopRewind tests stop_rewind helper generation.
func TestHelperBuilder_BuildStopRewind(t *testing.T) {
	hb := NewHelperBuilder(GlobalIndices{StateGlobal: 5, DataGlobal: 6})
	code := hb.BuildStopRewind()

	if len(code) == 0 {
		t.Error("BuildStopRewind() returned empty code")
	}
}

// TestHelperBuilder_StopFunctions_HaveBoundsCheck verifies that stop functions
// include bounds validation to detect stack corruption.
func TestHelperBuilder_StopFunctions_HaveBoundsCheck(t *testing.T) {
	hb := NewHelperBuilder(GlobalIndices{StateGlobal: 0, DataGlobal: 1})

	tests := []struct {
		name string
		code []byte
	}{
		{"stop_unwind", hb.BuildStopUnwind()},
		{"stop_rewind", hb.BuildStopRewind()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Parse code to find bounds check pattern:
			// i32.load (stack_ptr), i32.load (stack_end), i32.gt_u, if, unreachable
			instrs, err := wasm.DecodeInstructions(tc.code)
			if err != nil {
				t.Fatalf("failed to decode instructions: %v", err)
			}

			// Look for unreachable instruction (indicates bounds check)
			hasUnreachable := false
			hasGtU := false
			for _, instr := range instrs {
				if instr.Opcode == wasm.OpUnreachable {
					hasUnreachable = true
				}
				if instr.Opcode == wasm.OpI32GtU {
					hasGtU = true
				}
			}

			if !hasUnreachable {
				t.Error("stop function should have unreachable instruction for bounds check trap")
			}
			if !hasGtU {
				t.Error("stop function should have i32.gt_u for bounds comparison")
			}
		})
	}
}

// TestExportManager_AddAsyncifyExports tests adding all exports.
func TestExportManager_AddAsyncifyExports(t *testing.T) {
	m := &wasm.Module{
		Types:    []wasm.FuncType{},
		Funcs:    []uint32{},
		Code:     []wasm.FuncBody{},
		Exports:  []wasm.Export{},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
	}

	em := NewExportManager(m, GlobalIndices{StateGlobal: 0, DataGlobal: 1})
	em.AddAsyncifyExports()

	expectedExports := []string{
		"asyncify_get_state",
		"asyncify_start_unwind",
		"asyncify_stop_unwind",
		"asyncify_start_rewind",
		"asyncify_stop_rewind",
	}

	if len(m.Exports) != len(expectedExports) {
		t.Errorf("AddAsyncifyExports() added %d exports, want %d", len(m.Exports), len(expectedExports))
	}

	exportNames := make(map[string]bool)
	for _, exp := range m.Exports {
		exportNames[exp.Name] = true
	}

	for _, name := range expectedExports {
		if !exportNames[name] {
			t.Errorf("missing export: %s", name)
		}
	}
}

// TestExportManager_EnsureFuncType tests type deduplication.
func TestExportManager_EnsureFuncType(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{}, // void -> void at index 0
		},
	}

	em := NewExportManager(m, GlobalIndices{})

	// Should reuse existing type
	idx := em.ensureFuncType(wasm.FuncType{})
	if idx != 0 {
		t.Errorf("ensureFuncType(void->void) = %d, want 0 (reuse)", idx)
	}

	// Should add new type
	idx = em.ensureFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}})
	if idx != 1 {
		t.Errorf("ensureFuncType(void->i32) = %d, want 1 (new)", idx)
	}

	// Should reuse newly added type
	idx = em.ensureFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}})
	if idx != 1 {
		t.Errorf("ensureFuncType(void->i32) = %d, want 1 (reuse)", idx)
	}
}

// TestFuncTypesEqual tests function type comparison.
func TestFuncTypesEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b wasm.FuncType
		want bool
	}{
		{
			name: "empty equal",
			a:    wasm.FuncType{},
			b:    wasm.FuncType{},
			want: true,
		},
		{
			name: "same params",
			a:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}},
			b:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}},
			want: true,
		},
		{
			name: "different params",
			a:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}},
			b:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI64}},
			want: false,
		},
		{
			name: "different param count",
			a:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}},
			b:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI32, wasm.ValI32}},
			want: false,
		},
		{
			name: "same results",
			a:    wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}},
			b:    wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}},
			want: true,
		},
		{
			name: "different results",
			a:    wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}},
			b:    wasm.FuncType{Results: []wasm.ValType{wasm.ValI64}},
			want: false,
		},
		{
			name: "full match",
			a:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI32, wasm.ValI64}, Results: []wasm.ValType{wasm.ValF32}},
			b:    wasm.FuncType{Params: []wasm.ValType{wasm.ValI32, wasm.ValI64}, Results: []wasm.ValType{wasm.ValF32}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := funcTypesEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("funcTypesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestExportManager_AddFunction tests function addition.
func TestExportManager_AddFunction(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{{}},
		Funcs: []uint32{},
		Code:  []wasm.FuncBody{},
	}

	em := NewExportManager(m, GlobalIndices{})

	code := []byte{0x0B} // just end
	idx := em.addFunction(0, code)

	if len(m.Funcs) != 1 {
		t.Errorf("addFunction() funcs count = %d, want 1", len(m.Funcs))
	}
	if len(m.Code) != 1 {
		t.Errorf("addFunction() code count = %d, want 1", len(m.Code))
	}
	if idx != 0 {
		t.Errorf("addFunction() returned idx = %d, want 0", idx)
	}
}

// TestExportManager_AddExport tests export addition.
func TestExportManager_AddExport(t *testing.T) {
	m := &wasm.Module{
		Exports: []wasm.Export{},
	}

	em := NewExportManager(m, GlobalIndices{})
	em.addExport("test_func", 42)

	if len(m.Exports) != 1 {
		t.Fatalf("addExport() exports count = %d, want 1", len(m.Exports))
	}

	exp := m.Exports[0]
	if exp.Name != "test_func" {
		t.Errorf("export name = %s, want test_func", exp.Name)
	}
	if exp.Kind != 0 {
		t.Errorf("export kind = %d, want 0 (function)", exp.Kind)
	}
	if exp.Idx != 42 {
		t.Errorf("export idx = %d, want 42", exp.Idx)
	}
}
