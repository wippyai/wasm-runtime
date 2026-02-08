package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// TestValTypeSize tests all value type sizes
func TestValTypeSize_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		vt       wasm.ValType
		wantSize int
	}{
		// Primitive types - fully supported
		{"i32", wasm.ValI32, 4},
		{"i64", wasm.ValI64, 8},
		{"f32", wasm.ValF32, 4},
		{"f64", wasm.ValF64, 8},

		// Reference types - cannot be stored to linear memory (returns -1)
		{"funcref", wasm.ValFuncRef, -1},
		{"externref", wasm.ValExtern, -1},

		// SIMD type - 128-bit vector
		{"v128", wasm.ValV128, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValTypeSize(tt.vt)
			if got != tt.wantSize {
				t.Errorf("ValTypeSize(%s) = %d, want %d", tt.name, got, tt.wantSize)
			}
		})
	}
}

// TestValTypeLoadOp tests load operations for all types
func TestValTypeLoadOp_AllTypes(t *testing.T) {
	tests := []struct {
		name      string
		vt        wasm.ValType
		wantOp    byte
		wantAlign uint32
	}{
		{"i32", wasm.ValI32, wasm.OpI32Load, 2},
		{"i64", wasm.ValI64, wasm.OpI64Load, 3},
		{"f32", wasm.ValF32, wasm.OpF32Load, 2},
		{"f64", wasm.ValF64, wasm.OpF64Load, 3},
		{"v128", wasm.ValV128, wasm.OpPrefixSIMD, 4},
		// Reference types return (0, 0) - cannot be loaded from memory
		{"funcref", wasm.ValFuncRef, 0, 0},
		{"externref", wasm.ValExtern, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, align := ValTypeLoadOp(tt.vt)
			if op != tt.wantOp {
				t.Errorf("ValTypeLoadOp(%s) op = 0x%02x, want 0x%02x", tt.name, op, tt.wantOp)
			}
			if align != tt.wantAlign {
				t.Errorf("ValTypeLoadOp(%s) align = %d, want %d", tt.name, align, tt.wantAlign)
			}
		})
	}
}

// TestValTypeStoreOp tests store operations for all types
func TestValTypeStoreOp_AllTypes(t *testing.T) {
	tests := []struct {
		name      string
		vt        wasm.ValType
		wantOp    byte
		wantAlign uint32
	}{
		{"i32", wasm.ValI32, wasm.OpI32Store, 2},
		{"i64", wasm.ValI64, wasm.OpI64Store, 3},
		{"f32", wasm.ValF32, wasm.OpF32Store, 2},
		{"f64", wasm.ValF64, wasm.OpF64Store, 3},
		{"v128", wasm.ValV128, wasm.OpPrefixSIMD, 4},
		// Reference types return (0, 0) - cannot be stored to memory
		{"funcref", wasm.ValFuncRef, 0, 0},
		{"externref", wasm.ValExtern, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, align := ValTypeStoreOp(tt.vt)
			if op != tt.wantOp {
				t.Errorf("ValTypeStoreOp(%s) op = 0x%02x, want 0x%02x", tt.name, op, tt.wantOp)
			}
			if align != tt.wantAlign {
				t.Errorf("ValTypeStoreOp(%s) align = %d, want %d", tt.name, align, tt.wantAlign)
			}
		})
	}
}

// TestValTypeSize_UnknownType tests default case for unknown types
func TestValTypeSize_UnknownType(t *testing.T) {
	// Use an invalid/unknown type value (0xFF is not a valid ValType)
	unknownType := wasm.ValType(0xFF)
	got := ValTypeSize(unknownType)
	// Default case returns 4
	if got != 4 {
		t.Errorf("ValTypeSize(unknown) = %d, want 4", got)
	}
}

// TestValTypeLoadOp_UnknownType tests default case for unknown types
func TestValTypeLoadOp_UnknownType(t *testing.T) {
	unknownType := wasm.ValType(0xFF)
	op, align := ValTypeLoadOp(unknownType)
	// Default case returns i32 load
	if op != wasm.OpI32Load {
		t.Errorf("ValTypeLoadOp(unknown) op = 0x%02x, want 0x%02x", op, wasm.OpI32Load)
	}
	if align != 2 {
		t.Errorf("ValTypeLoadOp(unknown) align = %d, want 2", align)
	}
}

// TestValTypeStoreOp_UnknownType tests default case for unknown types
func TestValTypeStoreOp_UnknownType(t *testing.T) {
	unknownType := wasm.ValType(0xFF)
	op, align := ValTypeStoreOp(unknownType)
	// Default case returns i32 store
	if op != wasm.OpI32Store {
		t.Errorf("ValTypeStoreOp(unknown) op = 0x%02x, want 0x%02x", op, wasm.OpI32Store)
	}
	if align != 2 {
		t.Errorf("ValTypeStoreOp(unknown) align = %d, want 2", align)
	}
}

// TestIsReferenceType tests reference type detection
func TestIsReferenceType(t *testing.T) {
	tests := []struct {
		vt   wasm.ValType
		want bool
	}{
		{wasm.ValI32, false},
		{wasm.ValI64, false},
		{wasm.ValF32, false},
		{wasm.ValF64, false},
		{wasm.ValV128, false},
		{wasm.ValFuncRef, true},
		{wasm.ValExtern, true},
	}

	for _, tt := range tests {
		t.Run(tt.vt.String(), func(t *testing.T) {
			got := IsReferenceType(tt.vt)
			if got != tt.want {
				t.Errorf("IsReferenceType(%s) = %v, want %v", tt.vt, got, tt.want)
			}
		})
	}
}

// TestCanStoreToMemory tests if types can be stored to linear memory
func TestCanStoreToMemory(t *testing.T) {
	tests := []struct {
		vt   wasm.ValType
		want bool
	}{
		{wasm.ValI32, true},
		{wasm.ValI64, true},
		{wasm.ValF32, true},
		{wasm.ValF64, true},
		{wasm.ValV128, true},
		{wasm.ValFuncRef, false},
		{wasm.ValExtern, false},
	}

	for _, tt := range tests {
		t.Run(tt.vt.String(), func(t *testing.T) {
			got := CanStoreToMemory(tt.vt)
			if got != tt.want {
				t.Errorf("CanStoreToMemory(%s) = %v, want %v", tt.vt, got, tt.want)
			}
		})
	}
}

// TestEngine_Transform_RejectsReferenceTypes tests that asyncify rejects modules with reference type locals
func TestEngine_Transform_RejectsReferenceTypes(t *testing.T) {
	tests := []struct {
		name    string
		locals  []wasm.LocalEntry
		wantErr bool
	}{
		{
			name:    "i32 locals only",
			locals:  []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}},
			wantErr: false,
		},
		{
			name:    "funcref local",
			locals:  []wasm.LocalEntry{{Count: 1, ValType: wasm.ValFuncRef}},
			wantErr: true,
		},
		{
			name:    "externref local",
			locals:  []wasm.LocalEntry{{Count: 1, ValType: wasm.ValExtern}},
			wantErr: true,
		},
		{
			name: "mixed with funcref",
			locals: []wasm.LocalEntry{
				{Count: 1, ValType: wasm.ValI32},
				{Count: 1, ValType: wasm.ValFuncRef},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &wasm.Module{
				Types: []wasm.FuncType{
					{Results: []wasm.ValType{wasm.ValI32}},
				},
				Imports: []wasm.Import{
					{Module: "env", Name: "async_func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
				},
				Funcs:    []uint32{0},
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

			eng := New(Config{Matcher: newExactMatcher([]string{"env.async_func"})})
			_, err := eng.Transform(m.Encode())

			if tt.wantErr && err == nil {
				t.Errorf("Transform() should have failed for %s", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Transform() failed unexpectedly: %v", err)
			}
		})
	}
}

// TestEngine_Transform_V128Locals tests v128 local support
func TestEngine_Transform_V128Locals(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async_func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{
					{Count: 1, ValType: wasm.ValV128},
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

// TestEngine_Transform_FuncrefParam tests funcref parameters
func TestEngine_Transform_FuncrefParam(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValFuncRef}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async_func", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{0},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async_func"})})
	_, err := eng.Transform(m.Encode())
	if err == nil {
		t.Error("Transform() should fail for funcref parameters in async functions")
	}
}
