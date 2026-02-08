package binary

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// ErrOverflow is returned when a LEB128 value exceeds the maximum size.
var ErrOverflow = errors.New("leb128: overflow")

// Reader wraps an io.Reader with position tracking and WASM-specific read methods.
type Reader struct {
	r   io.ByteReader
	pos int
}

// NewReader creates a new Reader wrapping the given io.ByteReader.
func NewReader(r io.ByteReader) *Reader {
	return &Reader{r: r, pos: 0}
}

// Position returns the current byte position.
func (r *Reader) Position() int {
	return r.pos
}

// Reset seeks to the given position. Only works with bytes.Reader.
func (r *Reader) Reset(pos int) error {
	if br, ok := r.r.(*bytes.Reader); ok {
		_, err := br.Seek(int64(pos), io.SeekStart)
		if err != nil {
			return err
		}
		r.pos = pos
		return nil
	}
	return errors.New("Reset not supported on this reader type")
}

// ReadByte reads a single byte and advances the position.
func (r *Reader) ReadByte() (byte, error) {
	b, err := r.r.ReadByte()
	if err != nil {
		return 0, err
	}
	r.pos++
	return b, nil
}

// ReadBytes reads exactly n bytes.
func (r *Reader) ReadBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	for i := 0; i < n; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		buf[i] = b
	}
	return buf, nil
}

// ReadU32 reads an unsigned LEB128 encoded uint32.
func (r *Reader) ReadU32() (uint32, error) {
	var result uint32
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 35 {
			return 0, r.wrapError(ErrOverflow)
		}
	}
}

// ReadU64 reads an unsigned LEB128 encoded uint64.
func (r *Reader) ReadU64() (uint64, error) {
	var result uint64
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 70 {
			return 0, r.wrapError(ErrOverflow)
		}
	}
}

// ReadS32 reads a signed LEB128 encoded int32.
func (r *Reader) ReadS32() (int32, error) {
	var result int32
	var shift uint
	var b byte
	var err error
	for {
		b, err = r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int32(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
		if shift >= 35 {
			return 0, r.wrapError(ErrOverflow)
		}
	}
	// Sign extend
	if shift < 32 && b&0x40 != 0 {
		result |= ^int32(0) << shift
	}
	return result, nil
}

// ReadS64 reads a signed LEB128 encoded int64.
func (r *Reader) ReadS64() (int64, error) {
	var result int64
	var shift uint
	var b byte
	var err error
	for {
		b, err = r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int64(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
		if shift >= 70 {
			return 0, r.wrapError(ErrOverflow)
		}
	}
	// Sign extend
	if shift < 64 && b&0x40 != 0 {
		result |= ^int64(0) << shift
	}
	return result, nil
}

// ReadName reads a UTF-8 encoded name (length-prefixed byte sequence).
func (r *Reader) ReadName() (string, error) {
	length, err := r.ReadU32()
	if err != nil {
		return "", err
	}
	data, err := r.ReadBytes(int(length))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", r.wrapError(errors.New("invalid UTF-8 in name"))
	}
	return string(data), nil
}

// ReadU32LE reads a little-endian uint32 (fixed 4 bytes).
func (r *Reader) ReadU32LE() (uint32, error) {
	buf, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf), nil
}

// ReadRemaining reads all remaining bytes from the reader.
func (r *Reader) ReadRemaining() ([]byte, error) {
	if br, ok := r.r.(*bytes.Reader); ok {
		remaining := br.Len()
		return r.ReadBytes(remaining)
	}
	// Fallback for non-bytes.Reader
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		buf.WriteByte(b)
	}
	return buf.Bytes(), nil
}

func (r *Reader) wrapError(err error) error {
	return fmt.Errorf("at position %d: %w", r.pos, err)
}

// ParseError represents an error during binary parsing with position information.
type ParseError struct {
	Err      error
	Section  string
	Position int
}

func (e *ParseError) Error() string {
	if e.Section != "" {
		return fmt.Sprintf("wasm: %s at position %d: %v", e.Section, e.Position, e.Err)
	}
	return fmt.Sprintf("wasm: at position %d: %v", e.Position, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

// WrapError creates a ParseError with the current position.
func (r *Reader) WrapError(section string, err error) error {
	return &ParseError{
		Position: r.pos,
		Section:  section,
		Err:      err,
	}
}
