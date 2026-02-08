package wasm

import (
	"testing"

	"github.com/tetratelabs/wazero/api"
)

func TestEncodeULEB128(t *testing.T) {
	tests := []struct {
		expected []byte
		input    uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, 127},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xe5, 0x8e, 0x26}, 624485},
	}

	for _, tt := range tests {
		result := EncodeULEB128(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("EncodeULEB128(%d): expected len %d, got %d", tt.input, len(tt.expected), len(result))
			continue
		}
		for i, b := range result {
			if b != tt.expected[i] {
				t.Errorf("EncodeULEB128(%d)[%d]: expected 0x%02x, got 0x%02x", tt.input, i, tt.expected[i], b)
			}
		}
	}
}

func TestDecodeULEB128(t *testing.T) {
	tests := []struct {
		input         []byte
		expected      uint32
		expectedBytes int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x01}, 1, 1},
		{[]byte{0x7f}, 127, 1},
		{[]byte{0x80, 0x01}, 128, 2},
		{[]byte{0xe5, 0x8e, 0x26}, 624485, 3},
	}

	for _, tt := range tests {
		result, bytesRead := DecodeULEB128(tt.input)
		if result != tt.expected {
			t.Errorf("DecodeULEB128: expected %d, got %d", tt.expected, result)
		}
		if bytesRead != tt.expectedBytes {
			t.Errorf("DecodeULEB128: expected %d bytes, got %d", tt.expectedBytes, bytesRead)
		}
	}
}

func TestEncodeSLEB128_Int32(t *testing.T) {
	tests := []struct {
		expected []byte
		input    int32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, -1},
		{[]byte{0x3f}, 63},
		{[]byte{0x40}, -64},
		{[]byte{0xc0, 0x00}, 64},
		{[]byte{0xbf, 0x7f}, -65},
	}

	for _, tt := range tests {
		result := EncodeSLEB128(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("EncodeSLEB128(%d): expected len %d, got %d", tt.input, len(tt.expected), len(result))
			continue
		}
		for i, b := range result {
			if b != tt.expected[i] {
				t.Errorf("EncodeSLEB128(%d)[%d]: expected 0x%02x, got 0x%02x", tt.input, i, tt.expected[i], b)
			}
		}
	}
}

func TestEncodeSLEB128_Int64(t *testing.T) {
	tests := []struct {
		expected []byte
		input    int64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, -1},
	}

	for _, tt := range tests {
		result := EncodeSLEB128(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("EncodeSLEB128(%d): expected len %d, got %d", tt.input, len(tt.expected), len(result))
			continue
		}
		for i, b := range result {
			if b != tt.expected[i] {
				t.Errorf("EncodeSLEB128(%d)[%d]: expected 0x%02x, got 0x%02x", tt.input, i, tt.expected[i], b)
			}
		}
	}
}

func TestEncodeDecodeULEB128_RoundTrip(t *testing.T) {
	values := []uint32{0, 1, 127, 128, 255, 256, 1000, 10000, 100000, 0xFFFFFFFF}
	for _, v := range values {
		encoded := EncodeULEB128(v)
		decoded, _ := DecodeULEB128(encoded)
		if decoded != v {
			t.Errorf("round trip failed for %d: got %d", v, decoded)
		}
	}
}

func TestValTypeToWasm(t *testing.T) {
	tests := []struct {
		input    api.ValueType
		expected byte
	}{
		{api.ValueTypeI32, 0x7f},
		{api.ValueTypeI64, 0x7e},
		{api.ValueTypeF32, 0x7d},
		{api.ValueTypeF64, 0x7c},
	}

	for _, tt := range tests {
		result := ValTypeToWasm(tt.input)
		if result != tt.expected {
			t.Errorf("ValTypeToWasm(%v): expected 0x%02x, got 0x%02x", tt.input, tt.expected, result)
		}
	}
}

func TestParseValType(t *testing.T) {
	tests := []struct {
		input    byte
		expected api.ValueType
	}{
		{0x7F, api.ValueTypeI32},
		{0x7E, api.ValueTypeI64},
		{0x7D, api.ValueTypeF32},
		{0x7C, api.ValueTypeF64},
		{0x00, api.ValueTypeI32}, // unknown defaults to i32
	}

	for _, tt := range tests {
		result := ParseValType(tt.input)
		if result != tt.expected {
			t.Errorf("ParseValType(0x%02x): expected %v, got %v", tt.input, tt.expected, result)
		}
	}
}

func TestDecodeULEB128_EmptyInput(t *testing.T) {
	result, bytesRead := DecodeULEB128([]byte{})
	if result != 0 {
		t.Errorf("expected 0 for empty input, got %d", result)
	}
	if bytesRead != 0 {
		t.Errorf("expected 0 bytes read for empty input, got %d", bytesRead)
	}
}

func TestDecodeULEB128_ShiftOverflow(t *testing.T) {
	// Input with continuation bits that would cause shift overflow
	// More than 5 bytes with continuation bits
	input := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	result, _ := DecodeULEB128(input)
	// Should handle gracefully and return a result
	_ = result
}
