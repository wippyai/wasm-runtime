package encoder

import (
	"encoding/binary"
	"math"
)

type Buffer struct {
	Bytes []byte
}

func (b *Buffer) AppendByte(v byte) {
	b.Bytes = append(b.Bytes, v)
}

func (b *Buffer) WriteBytes(v []byte) {
	b.Bytes = append(b.Bytes, v...)
}

// WriteU32 writes unsigned LEB128 encoding.
func (b *Buffer) WriteU32(v uint32) {
	for {
		byt := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			byt |= 0x80
		}
		b.AppendByte(byt)
		if v == 0 {
			break
		}
	}
}

// WriteI32 writes signed LEB128 encoding.
func (b *Buffer) WriteI32(v int32) {
	for {
		byt := byte(v & 0x7F)
		v >>= 7
		if (v == 0 && byt&0x40 == 0) || (v == -1 && byt&0x40 != 0) {
			b.AppendByte(byt)
			break
		}
		b.AppendByte(byt | 0x80)
	}
}

// WriteI64 writes signed LEB128 encoding.
func (b *Buffer) WriteI64(v int64) {
	for {
		byt := byte(v & 0x7F)
		v >>= 7
		if (v == 0 && byt&0x40 == 0) || (v == -1 && byt&0x40 != 0) {
			b.AppendByte(byt)
			break
		}
		b.AppendByte(byt | 0x80)
	}
}

// WriteI33 writes signed LEB128 for block type indices (33-bit range per WASM spec).
func (b *Buffer) WriteI33(v int64) {
	b.WriteI64(v)
}

func (b *Buffer) WriteF32(v float32) {
	bits := math.Float32bits(v)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, bits)
	b.WriteBytes(buf)
}

func (b *Buffer) WriteF64(v float64) {
	bits := math.Float64bits(v)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, bits)
	b.WriteBytes(buf)
}

func (b *Buffer) WriteString(s string) {
	b.WriteU32(uint32(len(s)))
	b.WriteBytes([]byte(s))
}

func (b *Buffer) WriteLimits(min uint32, max *uint32) {
	if max != nil {
		b.AppendByte(0x01)
		b.WriteU32(min)
		b.WriteU32(*max)
	} else {
		b.AppendByte(0x00)
		b.WriteU32(min)
	}
}
