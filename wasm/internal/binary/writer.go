package binary

import (
	"bytes"
	"encoding/binary"
)

// Writer provides buffered writing utilities for WASM binary encoding.
type Writer struct {
	buf *bytes.Buffer
}

// NewWriter creates a new Writer.
func NewWriter() *Writer {
	return &Writer{buf: &bytes.Buffer{}}
}

// Bytes returns the written bytes.
func (w *Writer) Bytes() []byte {
	return w.buf.Bytes()
}

// Len returns the number of bytes written.
func (w *Writer) Len() int {
	return w.buf.Len()
}

// Byte writes a single byte.
func (w *Writer) Byte(b byte) {
	w.buf.WriteByte(b)
}

// WriteBytes writes a byte slice.
func (w *Writer) WriteBytes(data []byte) {
	w.buf.Write(data)
}

// WriteU32 writes an unsigned LEB128 encoded uint32.
func (w *Writer) WriteU32(v uint32) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		w.buf.WriteByte(b)
		if v == 0 {
			break
		}
	}
}

// WriteU64 writes an unsigned LEB128 encoded uint64.
func (w *Writer) WriteU64(v uint64) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		w.buf.WriteByte(b)
		if v == 0 {
			break
		}
	}
}

// WriteS64 writes a signed LEB128 encoded int64.
func (w *Writer) WriteS64(v int64) {
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0) {
			more = false
		} else {
			b |= 0x80
		}
		w.buf.WriteByte(b)
	}
}

// WriteName writes a UTF-8 encoded name (length-prefixed).
func (w *Writer) WriteName(s string) {
	w.WriteU32(uint32(len(s)))
	w.buf.WriteString(s)
}

// WriteU32LE writes a little-endian uint32 (fixed 4 bytes).
func (w *Writer) WriteU32LE(v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	w.buf.Write(buf[:])
}
