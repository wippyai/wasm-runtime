package binary

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReaderReadByte(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	r := NewReader(bytes.NewReader(data))

	for i, want := range data {
		if r.Position() != i {
			t.Errorf("position before read %d: got %d, want %d", i, r.Position(), i)
		}
		b, err := r.ReadByte()
		if err != nil {
			t.Fatalf("ReadByte %d: %v", i, err)
		}
		if b != want {
			t.Errorf("ReadByte %d: got 0x%02x, want 0x%02x", i, b, want)
		}
	}

	if r.Position() != 3 {
		t.Errorf("final position: got %d, want 3", r.Position())
	}

	_, err := r.ReadByte()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestReaderReadBytes(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	r := NewReader(bytes.NewReader(data))

	got, err := r.ReadBytes(3)
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}
	if !bytes.Equal(got, []byte{0x01, 0x02, 0x03}) {
		t.Errorf("ReadBytes: got %v, want [1 2 3]", got)
	}

	if r.Position() != 3 {
		t.Errorf("position: got %d, want 3", r.Position())
	}

	_, err = r.ReadBytes(10)
	if err == nil {
		t.Error("expected error for reading past EOF")
	}
}

func TestReaderReadU32(t *testing.T) {
	tests := []struct {
		encoded []byte
		want    uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, 127},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xff, 0x01}, 255},
		{[]byte{0xe5, 0x8e, 0x26}, 624485},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xFFFFFFFF},
	}

	for _, tt := range tests {
		r := NewReader(bytes.NewReader(tt.encoded))
		got, err := r.ReadU32()
		if err != nil {
			t.Errorf("ReadU32(%v): %v", tt.encoded, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ReadU32(%v): got %d, want %d", tt.encoded, got, tt.want)
		}
	}
}

func TestReaderReadU32Overflow(t *testing.T) {
	data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadU32()
	if err == nil {
		t.Error("expected overflow error")
	}
	if !errors.Is(err, ErrOverflow) {
		t.Errorf("expected ErrOverflow, got %v", err)
	}
}

func TestReaderReadU64(t *testing.T) {
	tests := []struct {
		encoded []byte
		want    uint64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xFFFFFFFF},
	}

	for _, tt := range tests {
		r := NewReader(bytes.NewReader(tt.encoded))
		got, err := r.ReadU64()
		if err != nil {
			t.Errorf("ReadU64(%v): %v", tt.encoded, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ReadU64(%v): got %d, want %d", tt.encoded, got, tt.want)
		}
	}
}

func TestReaderReadS32(t *testing.T) {
	tests := []struct {
		encoded []byte
		want    int32
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
		r := NewReader(bytes.NewReader(tt.encoded))
		got, err := r.ReadS32()
		if err != nil {
			t.Errorf("ReadS32(%v): %v", tt.encoded, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ReadS32(%v): got %d, want %d", tt.encoded, got, tt.want)
		}
	}
}

func TestReaderReadS64(t *testing.T) {
	tests := []struct {
		encoded []byte
		want    int64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, -1},
		{[]byte{0x3f}, 63},
		{[]byte{0x40}, -64},
	}

	for _, tt := range tests {
		r := NewReader(bytes.NewReader(tt.encoded))
		got, err := r.ReadS64()
		if err != nil {
			t.Errorf("ReadS64(%v): %v", tt.encoded, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ReadS64(%v): got %d, want %d", tt.encoded, got, tt.want)
		}
	}
}

func TestReaderReadName(t *testing.T) {
	w := NewWriter()
	w.WriteName("hello")
	data := w.Bytes()

	r := NewReader(bytes.NewReader(data))
	got, err := r.ReadName()
	if err != nil {
		t.Fatalf("ReadName: %v", err)
	}
	if got != "hello" {
		t.Errorf("ReadName: got %q, want %q", got, "hello")
	}
}

func TestReaderReadNameInvalidUTF8(t *testing.T) {
	data := []byte{0x02, 0xff, 0xfe}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadName()
	if err == nil {
		t.Error("expected error for invalid UTF-8")
	}
}

func TestReaderReadU32LE(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	r := NewReader(bytes.NewReader(data))
	got, err := r.ReadU32LE()
	if err != nil {
		t.Fatalf("ReadU32LE: %v", err)
	}
	want := uint32(0x04030201)
	if got != want {
		t.Errorf("ReadU32LE: got 0x%08x, want 0x%08x", got, want)
	}
}

func TestReaderReset(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	r := NewReader(bytes.NewReader(data))

	r.ReadBytes(3)
	if r.Position() != 3 {
		t.Errorf("position: got %d, want 3", r.Position())
	}

	err := r.Reset(1)
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if r.Position() != 1 {
		t.Errorf("position after reset: got %d, want 1", r.Position())
	}

	b, _ := r.ReadByte()
	if b != 0x02 {
		t.Errorf("ReadByte after reset: got 0x%02x, want 0x02", b)
	}
}

func TestReaderReadRemaining(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	r := NewReader(bytes.NewReader(data))
	r.ReadBytes(2)

	remaining, err := r.ReadRemaining()
	if err != nil {
		t.Fatalf("ReadRemaining: %v", err)
	}
	if !bytes.Equal(remaining, []byte{0x03, 0x04, 0x05}) {
		t.Errorf("ReadRemaining: got %v, want [3 4 5]", remaining)
	}
}

func TestReaderWrapError(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte{0x01, 0x02}))
	r.ReadByte()
	r.ReadByte()

	err := r.WrapError("test section", errors.New("test error"))
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T", err)
	}
	if pe.Position != 2 {
		t.Errorf("Position: got %d, want 2", pe.Position)
	}
	if pe.Section != "test section" {
		t.Errorf("Section: got %q, want %q", pe.Section, "test section")
	}
	if pe.Err.Error() != "test error" {
		t.Errorf("Err: got %q, want %q", pe.Err.Error(), "test error")
	}

	errStr := pe.Error()
	if errStr != "wasm: test section at position 2: test error" {
		t.Errorf("Error(): got %q", errStr)
	}
}

func TestParseErrorNoSection(t *testing.T) {
	pe := &ParseError{Position: 5, Err: errors.New("some error")}
	errStr := pe.Error()
	if errStr != "wasm: at position 5: some error" {
		t.Errorf("Error(): got %q", errStr)
	}
}

func TestWriterBasic(t *testing.T) {
	w := NewWriter()
	if w.Len() != 0 {
		t.Errorf("initial Len: got %d, want 0", w.Len())
	}

	w.Byte(0x42)
	if w.Len() != 1 {
		t.Errorf("Len after Byte: got %d, want 1", w.Len())
	}

	w.WriteBytes([]byte{0x01, 0x02, 0x03})
	if w.Len() != 4 {
		t.Errorf("Len after WriteBytes: got %d, want 4", w.Len())
	}

	got := w.Bytes()
	want := []byte{0x42, 0x01, 0x02, 0x03}
	if !bytes.Equal(got, want) {
		t.Errorf("Bytes: got %v, want %v", got, want)
	}
}

func TestWriterWriteU32(t *testing.T) {
	tests := []struct {
		want  []byte
		value uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, 127},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xff, 0x01}, 255},
		{[]byte{0xe5, 0x8e, 0x26}, 624485},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xFFFFFFFF},
	}

	for _, tt := range tests {
		w := NewWriter()
		w.WriteU32(tt.value)
		got := w.Bytes()
		if !bytes.Equal(got, tt.want) {
			t.Errorf("WriteU32(%d): got %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestWriterWriteU64(t *testing.T) {
	tests := []struct {
		want  []byte
		value uint64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7f}, 127},
		{[]byte{0x80, 0x01}, 128},
	}

	for _, tt := range tests {
		w := NewWriter()
		w.WriteU64(tt.value)
		got := w.Bytes()
		if !bytes.Equal(got, tt.want) {
			t.Errorf("WriteU64(%d): got %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestWriterWriteS64(t *testing.T) {
	tests := []struct {
		want  []byte
		value int64
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
		w := NewWriter()
		w.WriteS64(tt.value)
		got := w.Bytes()
		if !bytes.Equal(got, tt.want) {
			t.Errorf("WriteS64(%d): got %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestWriterWriteName(t *testing.T) {
	w := NewWriter()
	w.WriteName("test")
	got := w.Bytes()
	want := []byte{0x04, 't', 'e', 's', 't'}
	if !bytes.Equal(got, want) {
		t.Errorf("WriteName: got %v, want %v", got, want)
	}
}

func TestWriterWriteU32LE(t *testing.T) {
	w := NewWriter()
	w.WriteU32LE(0x04030201)
	got := w.Bytes()
	want := []byte{0x01, 0x02, 0x03, 0x04}
	if !bytes.Equal(got, want) {
		t.Errorf("WriteU32LE: got %v, want %v", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	w := NewWriter()
	w.WriteU32(12345)
	w.WriteS64(-9876)
	w.WriteName("roundtrip")
	w.WriteU32LE(0xDEADBEEF)

	r := NewReader(bytes.NewReader(w.Bytes()))

	u32, err := r.ReadU32()
	if err != nil {
		t.Fatalf("ReadU32: %v", err)
	}
	if u32 != 12345 {
		t.Errorf("ReadU32: got %d, want 12345", u32)
	}

	s64, err := r.ReadS64()
	if err != nil {
		t.Fatalf("ReadS64: %v", err)
	}
	if s64 != -9876 {
		t.Errorf("ReadS64: got %d, want -9876", s64)
	}

	name, err := r.ReadName()
	if err != nil {
		t.Fatalf("ReadName: %v", err)
	}
	if name != "roundtrip" {
		t.Errorf("ReadName: got %q, want %q", name, "roundtrip")
	}

	u32le, err := r.ReadU32LE()
	if err != nil {
		t.Fatalf("ReadU32LE: %v", err)
	}
	if u32le != 0xDEADBEEF {
		t.Errorf("ReadU32LE: got 0x%08x, want 0xDEADBEEF", u32le)
	}
}

func TestParseErrorUnwrap(t *testing.T) {
	inner := errors.New("inner error")
	pe := &ParseError{Position: 10, Section: "test", Err: inner}
	if !errors.Is(pe.Unwrap(), inner) {
		t.Error("Unwrap should return inner error")
	}
}

func TestReaderResetSeeker(t *testing.T) {
	// Test Reset with a non-seekable reader - should fail
	data := []byte{0x01, 0x02, 0x03}
	r := NewReader(bytes.NewReader(data))
	r.ReadBytes(2)

	// Reset to position beyond start should work (we have a Seeker)
	err := r.Reset(1)
	if err != nil {
		t.Errorf("Reset(1) should work: %v", err)
	}

	// Reset to 0 should work
	err = r.Reset(0)
	if err != nil {
		t.Errorf("Reset(0) should work: %v", err)
	}
}

func TestReaderReadU32Truncated(t *testing.T) {
	// LEB128 that needs more bytes but EOF
	data := []byte{0x80}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadU32()
	if err == nil {
		t.Error("expected error for truncated LEB128")
	}
}

func TestReaderReadU64Overflow(t *testing.T) {
	// LEB128 that overflows 64 bits
	data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadU64()
	if err == nil {
		t.Error("expected overflow error")
	}
}

func TestReaderReadS32Overflow(t *testing.T) {
	// Signed LEB128 that overflows 32 bits
	data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadS32()
	if err == nil {
		t.Error("expected overflow error")
	}
}

func TestReaderReadS64Overflow(t *testing.T) {
	// Signed LEB128 that overflows 64 bits
	data := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadS64()
	if err == nil {
		t.Error("expected overflow error")
	}
}

func TestReaderReadNameTruncated(t *testing.T) {
	// Name length says 5 but only 2 bytes available
	data := []byte{0x05, 0x61, 0x62}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadName()
	if err == nil {
		t.Error("expected error for truncated name")
	}
}

func TestReaderReadU32LETruncated(t *testing.T) {
	// Only 2 bytes instead of 4
	data := []byte{0x01, 0x02}
	r := NewReader(bytes.NewReader(data))
	_, err := r.ReadU32LE()
	if err == nil {
		t.Error("expected error for truncated u32le")
	}
}

// customByteReader is a ByteReader that is NOT a *bytes.Reader
// to test the fallback path in ReadRemaining
type customByteReader struct {
	data []byte
	pos  int
}

func (c *customByteReader) ReadByte() (byte, error) {
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	b := c.data[c.pos]
	c.pos++
	return b, nil
}

func TestReaderReadRemainingFallback(t *testing.T) {
	custom := &customByteReader{data: []byte{0x01, 0x02, 0x03, 0x04, 0x05}}
	custom.ReadByte() // skip first byte
	custom.ReadByte() // skip second byte

	r := NewReader(custom)
	remaining, err := r.ReadRemaining()
	if err != nil {
		t.Fatalf("ReadRemaining: %v", err)
	}
	want := []byte{0x03, 0x04, 0x05}
	if !bytes.Equal(remaining, want) {
		t.Errorf("ReadRemaining fallback: got %v, want %v", remaining, want)
	}
}

func TestReaderResetNonSeeker(t *testing.T) {
	custom := &customByteReader{data: []byte{0x01, 0x02, 0x03}}
	r := NewReader(custom)
	r.ReadByte()

	err := r.Reset(0)
	if err == nil {
		t.Error("expected error for Reset on non-seeker")
	}
}
