package runtime

import (
	"context"
	"testing"
)

func TestModule_IsComponent_Core(t *testing.T) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	defer rt.Close(ctx)

	mod, err := rt.LoadWASM(ctx, minimalWASM, "")
	if err != nil {
		t.Fatalf("LoadWASM error: %v", err)
	}

	if mod.IsComponent() {
		t.Error("core module should not be a component")
	}
}

func TestModule_GetFunctionTypes(t *testing.T) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	defer rt.Close(ctx)

	witText := "export add: func(a: s32, b: s32) -> s32;"
	mod, err := rt.LoadWASM(ctx, addWASM, witText)
	if err != nil {
		t.Fatalf("LoadWASM error: %v", err)
	}

	// Get function types
	params, results, err := mod.GetFunctionTypes("add")
	if err != nil {
		t.Fatalf("GetFunctionTypes error: %v", err)
	}

	if len(params) != 2 {
		t.Errorf("expected 2 params, got %d", len(params))
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	// Test non-existent function
	_, _, err = mod.GetFunctionTypes("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent function")
	}
}

func TestParseWitFunctions(t *testing.T) {
	witText := `
		package test:example@1.0.0;

		interface calc {
			export add: func(a: s32, b: s32) -> s32;
			export sub: func(x: s32, y: s32) -> s32;
			export get-value: func() -> u64;
		}
	`

	funcs, err := parseWitFunctions(witText)
	if err != nil {
		t.Fatalf("parseWitFunctions error: %v", err)
	}

	if len(funcs) != 3 {
		t.Errorf("expected 3 functions, got %d", len(funcs))
	}

	// Check add function
	addSig, ok := funcs["add"]
	if !ok {
		t.Error("add function not found")
	} else {
		if len(addSig.params) != 2 {
			t.Errorf("add: expected 2 params, got %d", len(addSig.params))
		}
		if len(addSig.results) != 1 {
			t.Errorf("add: expected 1 result, got %d", len(addSig.results))
		}
	}

	// Check sub function
	subSig, ok := funcs["sub"]
	if !ok {
		t.Error("sub function not found")
	} else if len(subSig.params) != 2 {
		t.Errorf("sub: expected 2 params, got %d", len(subSig.params))
	}
}

func TestParseWitFunctions_NoFunctions(t *testing.T) {
	witText := `
		package test:example@1.0.0;
		interface empty {}
	`

	_, err := parseWitFunctions(witText)
	if err == nil {
		t.Error("expected error for WIT with no functions")
	}
}

func TestParseWitFunctions_TupleResult(t *testing.T) {
	witText := `
		export divmod: func(a: s32, b: s32) -> (s32, s32);
	`

	funcs, err := parseWitFunctions(witText)
	if err != nil {
		t.Fatalf("parseWitFunctions error: %v", err)
	}

	sig, ok := funcs["divmod"]
	if !ok {
		t.Fatal("divmod function not found")
	}

	if len(sig.params) != 2 {
		t.Errorf("expected 2 params, got %d", len(sig.params))
	}

	if len(sig.results) != 2 {
		t.Errorf("expected 2 results, got %d", len(sig.results))
	}
}

func TestParseWitFunctions_NoResult(t *testing.T) {
	witText := `
		export log: func(msg: string);
	`

	funcs, err := parseWitFunctions(witText)
	if err != nil {
		t.Fatalf("parseWitFunctions error: %v", err)
	}

	sig, ok := funcs["log"]
	if !ok {
		t.Fatal("log function not found")
	}

	if len(sig.params) != 1 {
		t.Errorf("expected 1 param, got %d", len(sig.params))
	}

	if len(sig.results) != 0 {
		t.Errorf("expected 0 results, got %d", len(sig.results))
	}
}

func TestSplitParams(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a: s32, b: s32", []string{"a: s32", "b: s32"}},
		{"x: u32, y: s32", []string{"x: u32", "y: s32"}},
		{"nested: u64", []string{"nested: u64"}},
		{"", []string{}},
		{"single: u64", []string{"single: u64"}},
		{" a : s32 , b : s32 ", []string{"a : s32", "b : s32"}},
		{"a: s32, b: f32, c: bool", []string{"a: s32", "b: f32", "c: bool"}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := splitParams(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("expected %d parts, got %d: %v", len(tc.expected), len(result), result)
				return
			}
			for i, exp := range tc.expected {
				if result[i] != exp {
					t.Errorf("part %d: expected %q, got %q", i, exp, result[i])
				}
			}
		})
	}
}

func TestParseWitType(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"s32", true},
		{"s64", true},
		{"u8", true},
		{"u16", true},
		{"u32", true},
		{"u64", true},
		{"f32", true},
		{"f64", true},
		{"bool", true},
		{"string", true},
		{"char", true},
		{"invalid-type-xyz", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, err := parseWitType(tc.input)
			if tc.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tc.valid && err == nil {
				t.Error("expected error for invalid type")
			}
		})
	}
}

// Minimal valid WASM module (no exports)
var minimalWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
}

// WASM with add function export
var addWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	// Type section: (i32, i32) -> i32
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	// Function section: func 0 uses type 0
	0x03, 0x02, 0x01, 0x00,
	// Export section: "add" -> func 0
	0x07, 0x07, 0x01, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00,
	// Code section: local.get 0 + local.get 1 = i32.add
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
}
