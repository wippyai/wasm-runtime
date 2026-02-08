package wasm_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLEB128Unsigned(t *testing.T) {
	tests := []struct {
		encoded []byte
		value   uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, 127},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xff, 0x01}, 255},
		{[]byte{0x80, 0x02}, 256},
		{[]byte{0xff, 0x7f}, 16383},
		{[]byte{0x80, 0x80, 0x01}, 16384},
		{[]byte{0xe5, 0x8e, 0x26}, 624485},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xFFFFFFFF},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			// Test encoding
			var buf bytes.Buffer
			wasm.WriteLEB128u(&buf, tt.value)
			if !bytes.Equal(buf.Bytes(), tt.encoded) {
				t.Errorf("encode %d: got %v, want %v", tt.value, buf.Bytes(), tt.encoded)
			}

			// Test decoding
			r := bytes.NewReader(tt.encoded)
			got, err := wasm.ReadLEB128u(r)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != tt.value {
				t.Errorf("decode: got %d, want %d", got, tt.value)
			}
		})
	}
}

func TestLEB128Signed(t *testing.T) {
	tests := []struct {
		encoded []byte
		value   int32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, -1},
		{[]byte{0x3f}, 63},
		{[]byte{0xc0, 0x00}, 64},
		{[]byte{0x40}, -64},
		{[]byte{0xbf, 0x7f}, -65},
		{[]byte{0xff, 0x00}, 127},
		{[]byte{0x80, 0x7f}, -128},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xff, 0x7e}, -129},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			var buf bytes.Buffer
			wasm.WriteLEB128s(&buf, tt.value)
			if !bytes.Equal(buf.Bytes(), tt.encoded) {
				t.Errorf("encode %d: got %v, want %v", tt.value, buf.Bytes(), tt.encoded)
			}

			r := bytes.NewReader(tt.encoded)
			got, err := wasm.ReadLEB128s(r)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != tt.value {
				t.Errorf("decode: got %d, want %d", got, tt.value)
			}
		})
	}
}

func TestLEB128u64(t *testing.T) {
	tests := []uint64{0, 1, 127, 128, 255, 256, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF}
	for _, v := range tests {
		var buf bytes.Buffer
		wasm.WriteLEB128u64(&buf, v)
		r := bytes.NewReader(buf.Bytes())
		got, err := wasm.ReadLEB128u64(r)
		if err != nil {
			t.Fatalf("ReadLEB128u64(%d): %v", v, err)
		}
		if got != v {
			t.Errorf("ReadLEB128u64: got %d, want %d", got, v)
		}
	}
}

func TestLEB128s64(t *testing.T) {
	tests := []int64{0, 1, -1, 63, 64, -64, -65, 127, -128, 0x7FFFFFFFFFFFFFFF, -0x8000000000000000}
	for _, v := range tests {
		var buf bytes.Buffer
		wasm.WriteLEB128s64(&buf, v)
		r := bytes.NewReader(buf.Bytes())
		got, err := wasm.ReadLEB128s64(r)
		if err != nil {
			t.Fatalf("ReadLEB128s64(%d): %v", v, err)
		}
		if got != v {
			t.Errorf("ReadLEB128s64: got %d, want %d", got, v)
		}
	}
}

func TestEncodeLEB128u(t *testing.T) {
	tests := []struct {
		expected []byte
		value    uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0xe5, 0x8e, 0x26}, 624485},
	}

	for _, tt := range tests {
		got := wasm.EncodeLEB128u(tt.value)
		if !bytes.Equal(got, tt.expected) {
			t.Errorf("EncodeLEB128u(%d) = %v, want %v", tt.value, got, tt.expected)
		}
	}
}

func TestEncodeLEB128s(t *testing.T) {
	tests := []struct {
		expected []byte
		value    int32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x7f}, -1},
		{[]byte{0xc0, 0x00}, 64},
		{[]byte{0x40}, -64},
	}

	for _, tt := range tests {
		got := wasm.EncodeLEB128s(tt.value)
		if !bytes.Equal(got, tt.expected) {
			t.Errorf("EncodeLEB128s(%d) = %v, want %v", tt.value, got, tt.expected)
		}
	}
}

func TestEncodeLEB128u64(t *testing.T) {
	tests := []uint64{0, 1, 127, 128, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF}
	for _, v := range tests {
		encoded := wasm.EncodeLEB128u64(v)
		r := bytes.NewReader(encoded)
		got, err := wasm.ReadLEB128u64(r)
		if err != nil {
			t.Fatalf("ReadLEB128u64(%d): %v", v, err)
		}
		if got != v {
			t.Errorf("EncodeLEB128u64 round-trip: got %d, want %d", got, v)
		}
	}
}

func TestEncodeLEB128s64(t *testing.T) {
	tests := []int64{0, 1, -1, 63, -64, 0x7FFFFFFFFFFFFFFF, -0x8000000000000000}
	for _, v := range tests {
		encoded := wasm.EncodeLEB128s64(v)
		r := bytes.NewReader(encoded)
		got, err := wasm.ReadLEB128s64(r)
		if err != nil {
			t.Fatalf("ReadLEB128s64(%d): %v", v, err)
		}
		if got != v {
			t.Errorf("EncodeLEB128s64 round-trip: got %d, want %d", got, v)
		}
	}
}

func TestLEB128Overflow(t *testing.T) {
	t.Run("u32 overflow", func(t *testing.T) {
		// More than 5 bytes with continuation bits (> 35 bits)
		data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
		r := bytes.NewReader(data)
		_, err := wasm.ReadLEB128u(r)
		if !errors.Is(err, wasm.ErrOverflow) {
			t.Errorf("expected ErrOverflow, got %v", err)
		}
	})

	t.Run("u64 overflow", func(t *testing.T) {
		// More than 10 bytes with continuation bits (> 70 bits)
		data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
		r := bytes.NewReader(data)
		_, err := wasm.ReadLEB128u64(r)
		if !errors.Is(err, wasm.ErrOverflow) {
			t.Errorf("expected ErrOverflow, got %v", err)
		}
	})

	t.Run("s32 overflow", func(t *testing.T) {
		// More than 5 bytes with continuation bits
		data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
		r := bytes.NewReader(data)
		_, err := wasm.ReadLEB128s(r)
		if !errors.Is(err, wasm.ErrOverflow) {
			t.Errorf("expected ErrOverflow, got %v", err)
		}
	})

	t.Run("s64 overflow", func(t *testing.T) {
		// More than 10 bytes with continuation bits
		data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
		r := bytes.NewReader(data)
		_, err := wasm.ReadLEB128s64(r)
		if !errors.Is(err, wasm.ErrOverflow) {
			t.Errorf("expected ErrOverflow, got %v", err)
		}
	})
}

func TestFloatReadWrite(t *testing.T) {
	t.Run("f32", func(t *testing.T) {
		tests := []float32{0, 1.5, -3.14, 1e38}
		for _, v := range tests {
			var buf bytes.Buffer
			wasm.WriteFloat32(&buf, v)
			r := bytes.NewReader(buf.Bytes())
			got, err := wasm.ReadFloat32(r)
			if err != nil {
				t.Fatalf("ReadFloat32: %v", err)
			}
			if got != v {
				t.Errorf("got %v, want %v", got, v)
			}
		}
	})

	t.Run("f64", func(t *testing.T) {
		tests := []float64{0, 1.5, -3.14, 1e308}
		for _, v := range tests {
			var buf bytes.Buffer
			wasm.WriteFloat64(&buf, v)
			r := bytes.NewReader(buf.Bytes())
			got, err := wasm.ReadFloat64(r)
			if err != nil {
				t.Fatalf("ReadFloat64: %v", err)
			}
			if got != v {
				t.Errorf("got %v, want %v", got, v)
			}
		}
	})
}
