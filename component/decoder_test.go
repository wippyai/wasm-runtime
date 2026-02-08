package component

import (
	"os"
	"testing"
)

func TestIsComponent(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "valid component header",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x0D, 0x00, 0x01, 0x00},
			expected: true,
		},
		{
			name:     "core wasm module (version 1)",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
			expected: false,
		},
		{
			name:     "too short",
			data:     []byte{0x00, 0x61, 0x73},
			expected: false,
		},
		{
			name:     "invalid magic",
			data:     []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0D, 0x00, 0x01, 0x00},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsComponent(tt.data)
			if result != tt.expected {
				t.Errorf("IsComponent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDecodeReferenceComponent(t *testing.T) {
	data, err := os.ReadFile("../testbed/reference-component.wasm")
	if err != nil {
		t.Skipf("Reference component not found: %v", err)
	}

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: false})
	if err != nil {
		t.Fatalf("DecodeWithOptions() error = %v", err)
	}

	if len(comp.CoreModules) != 13 {
		t.Errorf("CoreModules count = %d, want 13", len(comp.CoreModules))
	}

	if len(comp.Imports) != 27 {
		t.Errorf("Imports count = %d, want 27", len(comp.Imports))
	}

	if len(comp.Exports) != 5 {
		t.Errorf("Exports count = %d, want 5", len(comp.Exports))
	}

	expectedImports := []string{
		"wasi:cli/environment@0.2.0",
		"wasi:cli/exit@0.2.0",
		"wasi:io/error@0.2.0",
		"wasi:io/poll@0.2.0",
		"wasi:io/streams@0.2.0",
	}

	for i, expected := range expectedImports {
		if i >= len(comp.Imports) {
			break
		}
		if comp.Imports[i].Name != expected {
			t.Errorf("Import[%d].Name = %q, want %q", i, comp.Imports[i].Name, expected)
		}
	}

	expectedExports := []string{
		"exports",
		"hello",
		"add",
		"process",
		"greet",
	}

	for i, expected := range expectedExports {
		if comp.Exports[i].Name != expected {
			t.Errorf("Export[%d].Name = %q, want %q", i, comp.Exports[i].Name, expected)
		}
	}

	if len(comp.Types) != 32 {
		t.Errorf("Types count = %d, want 32", len(comp.Types))
	}

	if len(comp.Canons) != 205 {
		t.Errorf("Canons count = %d, want 205", len(comp.Canons))
	}

	if len(comp.Aliases) != 271 {
		t.Errorf("Aliases count = %d, want 271", len(comp.Aliases))
	}

	if len(comp.CoreInstances) != 73 {
		t.Errorf("CoreInstances count = %d, want 73", len(comp.CoreInstances))
	}

	if len(comp.CustomSections) != 1 {
		t.Errorf("CustomSections count = %d, want 1", len(comp.CustomSections))
	}
}

func TestDecodeInvalidComponent(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "not a component",
			data:    []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
			wantErr: true,
		},
		{
			name:    "truncated data",
			data:    []byte{0x00, 0x61, 0x73, 0x6D, 0x0D, 0x00, 0x01, 0x00, 0x01},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeWithOptions(tt.data, DecodeOptions{ParseTypes: false})
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeWithOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadLEB128(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected uint32
		wantErr  bool
	}{
		{
			name:     "single byte",
			input:    []byte{0x05},
			expected: 5,
			wantErr:  false,
		},
		{
			name:     "two bytes",
			input:    []byte{0x80, 0x01},
			expected: 128,
			wantErr:  false,
		},
		{
			name:     "max single byte",
			input:    []byte{0x7F},
			expected: 127,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &testReader{data: tt.input}
			result, err := readLEB128(r)
			if (err != nil) != tt.wantErr {
				t.Errorf("readLEB128() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("readLEB128() = %d, want %d", result, tt.expected)
			}
		})
	}
}

type testReader struct {
	data []byte
	pos  int
}

func (r *testReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, os.ErrClosed
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func TestDecodeCalculatorTypeIndexSpace(t *testing.T) {
	data, err := os.ReadFile("../testbed/calculator.wasm")
	if err != nil {
		t.Skipf("Calculator component not found: %v", err)
	}

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Based on wasm-tools output, calculator.wasm needs type indices up to 24
	// Type 23: (func (param "a" u32) (param "b" u32) (result u32))
	// Type 24: (func (param "a" u32) (param "b" u32) (param "msg" string) (result u32))
	if comp.TypeIndexSpace == nil {
		t.Fatal("TypeIndexSpace should not be nil")
	}

	if len(comp.TypeIndexSpace) < 25 {
		t.Errorf("TypeIndexSpace has %d entries, need at least 25 (indices 0-24)", len(comp.TypeIndexSpace))
	}

	// Verify the exported function types exist at indices 23 and 24
	if len(comp.TypeIndexSpace) >= 24 {
		funcType23, ok := comp.TypeIndexSpace[23].(*FuncType)
		if !ok {
			t.Errorf("TypeIndexSpace[23] is not a function type: %T", comp.TypeIndexSpace[23])
		} else if len(funcType23.Params) != 2 {
			t.Errorf("Type 23 param count = %d, want 2", len(funcType23.Params))
		}
	}

	if len(comp.TypeIndexSpace) >= 25 {
		funcType24, ok := comp.TypeIndexSpace[24].(*FuncType)
		if !ok {
			t.Errorf("TypeIndexSpace[24] is not a function type: %T", comp.TypeIndexSpace[24])
		} else if len(funcType24.Params) != 3 {
			t.Errorf("Type 24 param count = %d, want 3", len(funcType24.Params))
		}
	}
}

func TestDecodeMinimalTypeIndexSpace(t *testing.T) {
	data, err := os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		t.Skipf("Minimal component not found: %v", err)
	}

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if comp.TypeIndexSpace == nil {
		t.Fatal("TypeIndexSpace should not be nil")
	}

	// Verify we have the types needed for the exports
	t.Logf("TypeIndexSpace has %d entries", len(comp.TypeIndexSpace))
	for i, typ := range comp.TypeIndexSpace {
		t.Logf("  Type %d: %T", i, typ)
	}
}

func TestDecodeCalculatorFuncIndexSpace(t *testing.T) {
	data, err := os.ReadFile("../testbed/calculator.wasm")
	if err != nil {
		t.Skipf("Calculator component not found: %v", err)
	}

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Verify FuncIndexSpace is populated
	if len(comp.FuncIndexSpace) == 0 {
		t.Fatal("FuncIndexSpace should not be empty")
	}

	// Check that we have log and compute functions from instance 0
	foundLog := false
	foundCompute := false
	for _, entry := range comp.FuncIndexSpace {
		if entry.InstanceIdx == 0 && entry.ExportName == "log" {
			foundLog = true
		}
		if entry.InstanceIdx == 0 && entry.ExportName == "compute" {
			foundCompute = true
		}
	}

	if !foundLog {
		t.Error("FuncIndexSpace should contain 'log' from instance 0")
	}
	if !foundCompute {
		t.Error("FuncIndexSpace should contain 'compute' from instance 0")
	}

	// Verify InstanceTypes
	if len(comp.InstanceTypes) == 0 {
		t.Fatal("InstanceTypes should not be empty")
	}

	// Instance 0 should have type 0 (the wisma:calculator/host type)
	if len(comp.InstanceTypes) > 0 && comp.InstanceTypes[0] != 0 {
		t.Errorf("Instance 0 should have type index 0, got %d", comp.InstanceTypes[0])
	}
}

func TestReadSLEB128(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int32
		wantErr  bool
	}{
		{
			name:     "zero",
			input:    []byte{0x00},
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "positive",
			input:    []byte{0x05},
			expected: 5,
			wantErr:  false,
		},
		{
			name:     "negative -1",
			input:    []byte{0x7F},
			expected: -1,
			wantErr:  false,
		},
		{
			name:     "negative -64",
			input:    []byte{0x40},
			expected: -64,
			wantErr:  false,
		},
		{
			name:     "two bytes positive",
			input:    []byte{0x80, 0x01},
			expected: 128,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &testReader{data: tt.input}
			result, err := readSLEB128(r)
			if (err != nil) != tt.wantErr {
				t.Errorf("readSLEB128() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("readSLEB128() = %d, want %d", result, tt.expected)
			}
		})
	}
}
