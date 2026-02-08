package wasm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// LEB128 encoding/decoding utilities for WebAssembly binary format

// ErrOverflow is returned when a LEB128 value exceeds the maximum bit width.
var ErrOverflow = errors.New("leb128: overflow")

// ReadLEB128u reads an unsigned LEB128 value
func ReadLEB128u(r io.ByteReader) (uint32, error) {
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
			return 0, ErrOverflow
		}
	}
}

// ReadLEB128u64 reads an unsigned 64-bit LEB128 value
func ReadLEB128u64(r io.ByteReader) (uint64, error) {
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
			return 0, ErrOverflow
		}
	}
}

// ReadLEB128s reads a signed LEB128 value (32-bit)
func ReadLEB128s(r io.ByteReader) (int32, error) {
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
			return 0, ErrOverflow
		}
	}
	// Sign extend
	if shift < 32 && b&0x40 != 0 {
		result |= ^int32(0) << shift
	}
	return result, nil
}

// ReadLEB128s64 reads a signed 64-bit LEB128 value
func ReadLEB128s64(r io.ByteReader) (int64, error) {
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
			return 0, ErrOverflow
		}
	}
	// Sign extend
	if shift < 64 && b&0x40 != 0 {
		result |= ^int64(0) << shift
	}
	return result, nil
}

// WriteLEB128u writes an unsigned LEB128 value
func WriteLEB128u(w *bytes.Buffer, v uint32) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		w.WriteByte(b)
		if v == 0 {
			break
		}
	}
}

// WriteLEB128u64 writes an unsigned 64-bit LEB128 value
func WriteLEB128u64(w *bytes.Buffer, v uint64) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		w.WriteByte(b)
		if v == 0 {
			break
		}
	}
}

// WriteLEB128s writes a signed LEB128 value
func WriteLEB128s(w *bytes.Buffer, v int32) {
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			more = false
		} else {
			b |= 0x80
		}
		w.WriteByte(b)
	}
}

// WriteLEB128s64 writes a signed 64-bit LEB128 value
func WriteLEB128s64(w *bytes.Buffer, v int64) {
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			more = false
		} else {
			b |= 0x80
		}
		w.WriteByte(b)
	}
}

// EncodeLEB128u encodes an unsigned 32-bit LEB128 value to bytes.
func EncodeLEB128u(v uint32) []byte {
	var buf bytes.Buffer
	WriteLEB128u(&buf, v)
	return buf.Bytes()
}

// EncodeLEB128s encodes a signed 32-bit LEB128 value to bytes.
func EncodeLEB128s(v int32) []byte {
	var buf bytes.Buffer
	WriteLEB128s(&buf, v)
	return buf.Bytes()
}

// EncodeLEB128u64 encodes an unsigned 64-bit LEB128 value to bytes.
func EncodeLEB128u64(v uint64) []byte {
	var buf bytes.Buffer
	WriteLEB128u64(&buf, v)
	return buf.Bytes()
}

// EncodeLEB128s64 encodes a signed 64-bit LEB128 value to bytes.
func EncodeLEB128s64(v int64) []byte {
	var buf bytes.Buffer
	WriteLEB128s64(&buf, v)
	return buf.Bytes()
}

// ReadFloat32 reads a little-endian float32
func ReadFloat32(r io.Reader) (float32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	bits := binary.LittleEndian.Uint32(buf[:])
	return math.Float32frombits(bits), nil
}

// ReadFloat64 reads a little-endian float64
func ReadFloat64(r io.Reader) (float64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	bits := binary.LittleEndian.Uint64(buf[:])
	return math.Float64frombits(bits), nil
}

// WriteFloat32 writes a little-endian float32
func WriteFloat32(w *bytes.Buffer, v float32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
	w.Write(buf[:])
}

// WriteFloat64 writes a little-endian float64
func WriteFloat64(w *bytes.Buffer, v float64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
	w.Write(buf[:])
}
