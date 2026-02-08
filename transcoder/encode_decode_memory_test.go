package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

// testMem implements Memory for testing encode/decode operations.
// Uses Must* methods for writes since test buffers are pre-allocated.
type testMem struct {
	t    *testing.T
	data []byte
}

func newTestMem(t *testing.T, size int) *testMem {
	return &testMem{t: t, data: make([]byte, size)}
}

func (m *testMem) Read(offset uint32, length uint32) ([]byte, error) {
	return m.data[offset : offset+length], nil
}

func (m *testMem) Write(offset uint32, data []byte) error {
	copy(m.data[offset:], data)
	return nil
}

func (m *testMem) ReadU8(offset uint32) (uint8, error) {
	return m.data[offset], nil
}

func (m *testMem) ReadU16(offset uint32) (uint16, error) {
	return uint16(m.data[offset]) | uint16(m.data[offset+1])<<8, nil
}

func (m *testMem) ReadU32(offset uint32) (uint32, error) {
	return uint32(m.data[offset]) | uint32(m.data[offset+1])<<8 |
		uint32(m.data[offset+2])<<16 | uint32(m.data[offset+3])<<24, nil
}

func (m *testMem) ReadU64(offset uint32) (uint64, error) {
	lo, _ := m.ReadU32(offset)
	hi, _ := m.ReadU32(offset + 4)
	return uint64(lo) | uint64(hi)<<32, nil
}

func (m *testMem) WriteU8(offset uint32, value uint8) error {
	m.data[offset] = value
	return nil
}

func (m *testMem) WriteU16(offset uint32, value uint16) error {
	m.data[offset] = byte(value)
	m.data[offset+1] = byte(value >> 8)
	return nil
}

func (m *testMem) WriteU32(offset uint32, value uint32) error {
	m.data[offset] = byte(value)
	m.data[offset+1] = byte(value >> 8)
	m.data[offset+2] = byte(value >> 16)
	m.data[offset+3] = byte(value >> 24)
	return nil
}

func (m *testMem) WriteU64(offset uint32, value uint64) error {
	if err := m.WriteU32(offset, uint32(value)); err != nil {
		return err
	}
	return m.WriteU32(offset+4, uint32(value>>32))
}

// mustWriteU8 writes or fails test
func (m *testMem) mustWriteU8(offset uint32, value uint8) {
	m.t.Helper()
	if err := m.WriteU8(offset, value); err != nil {
		m.t.Fatalf("WriteU8 failed: %v", err)
	}
}

// mustWriteU16 writes or fails test
func (m *testMem) mustWriteU16(offset uint32, value uint16) {
	m.t.Helper()
	if err := m.WriteU16(offset, value); err != nil {
		m.t.Fatalf("WriteU16 failed: %v", err)
	}
}

// mustWriteU32 writes or fails test
func (m *testMem) mustWriteU32(offset uint32, value uint32) {
	m.t.Helper()
	if err := m.WriteU32(offset, value); err != nil {
		m.t.Fatalf("WriteU32 failed: %v", err)
	}
}

// mustWriteU64 writes or fails test
func (m *testMem) mustWriteU64(offset uint32, value uint64) {
	m.t.Helper()
	if err := m.WriteU64(offset, value); err != nil {
		m.t.Fatalf("WriteU64 failed: %v", err)
	}
}

// testAlloc implements Allocator for testing
type testAlloc struct {
	offset uint32
}

func newTestAlloc(start uint32) *testAlloc {
	return &testAlloc{offset: start}
}

func (a *testAlloc) Alloc(size, align uint32) (uint32, error) {
	a.offset = alignTo(a.offset, align)
	addr := a.offset
	a.offset += size
	return addr, nil
}

func (a *testAlloc) Free(ptr, size, align uint32) {}

// Tests for encodeFieldToMemory

func TestEncoderEncodeFieldToMemoryPrimitives(t *testing.T) {
	e := NewEncoder()
	mem := newTestMem(t, 1024)
	alloc := newTestAlloc(100)

	tests := []struct {
		value    any
		readBack func(addr uint32) any
		name     string
		kind     TypeKind
	}{
		{true, func(addr uint32) any { v, _ := mem.ReadU8(addr); return v != 0 }, "bool_true", KindBool},
		{false, func(addr uint32) any { v, _ := mem.ReadU8(addr); return v != 0 }, "bool_false", KindBool},
		{uint8(42), func(addr uint32) any { v, _ := mem.ReadU8(addr); return v }, "u8", KindU8},
		{int8(-10), func(addr uint32) any { v, _ := mem.ReadU8(addr); return int8(v) }, "s8", KindS8},
		{uint16(1234), func(addr uint32) any { v, _ := mem.ReadU16(addr); return v }, "u16", KindU16},
		{int16(-567), func(addr uint32) any { v, _ := mem.ReadU16(addr); return int16(v) }, "s16", KindS16},
		{uint32(123456), func(addr uint32) any { v, _ := mem.ReadU32(addr); return v }, "u32", KindU32},
		{int32(-78901), func(addr uint32) any { v, _ := mem.ReadU32(addr); return int32(v) }, "s32", KindS32},
		{uint64(123456789012), func(addr uint32) any { v, _ := mem.ReadU64(addr); return v }, "u64", KindU64},
		{int64(-987654321098), func(addr uint32) any { v, _ := mem.ReadU64(addr); return int64(v) }, "s64", KindS64},
		{'A', func(addr uint32) any { v, _ := mem.ReadU32(addr); return rune(v) }, "char", KindChar},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &CompiledType{Kind: tt.kind}
			var ptr unsafe.Pointer

			switch v := tt.value.(type) {
			case bool:
				ptr = unsafe.Pointer(&v)
			case uint8:
				ptr = unsafe.Pointer(&v)
			case int8:
				ptr = unsafe.Pointer(&v)
			case uint16:
				ptr = unsafe.Pointer(&v)
			case int16:
				ptr = unsafe.Pointer(&v)
			case uint32:
				ptr = unsafe.Pointer(&v)
			case int32:
				ptr = unsafe.Pointer(&v)
			case uint64:
				ptr = unsafe.Pointer(&v)
			case int64:
				ptr = unsafe.Pointer(&v)
			}

			err := e.encodeFieldToMemory(0, ct, ptr, mem, alloc, nil, nil)
			if err != nil {
				t.Fatalf("encodeFieldToMemory failed: %v", err)
			}

			got := tt.readBack(0)
			if got != tt.value {
				t.Errorf("got %v, want %v", got, tt.value)
			}
		})
	}
}

func TestEncoderEncodeFloats(t *testing.T) {
	e := NewEncoder()
	mem := newTestMem(t, 1024)
	alloc := newTestAlloc(100)

	t.Run("f32", func(t *testing.T) {
		ct := &CompiledType{Kind: KindF32}
		val := float32(3.14)
		err := e.encodeFieldToMemory(0, ct, unsafe.Pointer(&val), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
	})

	t.Run("f64", func(t *testing.T) {
		ct := &CompiledType{Kind: KindF64}
		val := float64(3.14159265359)
		err := e.encodeFieldToMemory(0, ct, unsafe.Pointer(&val), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
	})
}

func TestEncoderEncodeString(t *testing.T) {
	e := NewEncoder()

	t.Run("non_empty", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		alloc := newTestAlloc(100)

		err := e.encodeStringToMemory(0, "hello", mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		ptr, _ := mem.ReadU32(0)
		length, _ := mem.ReadU32(4)

		if length != 5 {
			t.Errorf("length = %d, want 5", length)
		}
		if ptr == 0 {
			t.Error("ptr should not be 0")
		}

		data, _ := mem.Read(ptr, length)
		if string(data) != "hello" {
			t.Errorf("got %q, want hello", string(data))
		}
	})

	t.Run("empty", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		alloc := newTestAlloc(100)

		err := e.encodeStringToMemory(0, "", mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		ptr, _ := mem.ReadU32(0)
		length, _ := mem.ReadU32(4)

		if ptr != 0 || length != 0 {
			t.Errorf("empty string: ptr=%d len=%d, want 0,0", ptr, length)
		}
	})
}

func TestEncoderEncodeRecord(t *testing.T) {
	e := NewEncoder()
	mem := newTestMem(t, 1024)
	alloc := newTestAlloc(100)

	type TestRecord struct {
		A uint32
		B uint64
	}

	ct := &CompiledType{
		Kind: KindRecord,
		Fields: []CompiledField{
			{Name: "A", WitName: "a", GoOffset: 0, WitOffset: 0, Type: &CompiledType{Kind: KindU32}},
			{Name: "B", WitName: "b", GoOffset: 8, WitOffset: 8, Type: &CompiledType{Kind: KindU64}},
		},
	}

	rec := TestRecord{A: 42, B: 123456}
	err := e.encodeRecordToMemory(0, ct, unsafe.Pointer(&rec), mem, alloc, nil, nil)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	a, _ := mem.ReadU32(0)
	b, _ := mem.ReadU64(8)

	if a != 42 {
		t.Errorf("a = %d, want 42", a)
	}
	if b != 123456 {
		t.Errorf("b = %d, want 123456", b)
	}
}

func TestEncoderEncodeList(t *testing.T) {
	e := NewEncoder()

	t.Run("bytes", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		alloc := newTestAlloc(100)

		ct := &CompiledType{
			Kind:     KindList,
			GoType:   reflect.TypeOf([]byte{}),
			ElemType: &CompiledType{Kind: KindU8, GoType: reflect.TypeOf(uint8(0)), WitSize: 1, WitAlign: 1},
		}

		data := []byte{1, 2, 3, 4, 5}
		err := e.encodeListToMemory(0, ct, unsafe.Pointer(&data), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		dataPtr, _ := mem.ReadU32(0)
		length, _ := mem.ReadU32(4)

		if length != 5 {
			t.Errorf("length = %d, want 5", length)
		}

		readData, _ := mem.Read(dataPtr, length)
		for i, b := range readData {
			if b != data[i] {
				t.Errorf("byte[%d] = %d, want %d", i, b, data[i])
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		alloc := newTestAlloc(100)

		ct := &CompiledType{
			Kind:     KindList,
			GoType:   reflect.TypeOf([]byte{}),
			ElemType: &CompiledType{Kind: KindU8, GoType: reflect.TypeOf(uint8(0)), WitSize: 1, WitAlign: 1},
		}

		data := []byte{}
		err := e.encodeListToMemory(0, ct, unsafe.Pointer(&data), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		dataPtr, _ := mem.ReadU32(0)
		length, _ := mem.ReadU32(4)

		if dataPtr != 0 || length != 0 {
			t.Errorf("empty list: ptr=%d len=%d", dataPtr, length)
		}
	})

	t.Run("int32s", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		alloc := newTestAlloc(100)

		ct := &CompiledType{
			Kind:     KindList,
			GoType:   reflect.TypeOf([]int32{}),
			ElemType: &CompiledType{Kind: KindS32, GoType: reflect.TypeOf(int32(0)), WitSize: 4, WitAlign: 4},
		}

		data := []int32{10, 20, 30}
		err := e.encodeListToMemory(0, ct, unsafe.Pointer(&data), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		dataPtr, _ := mem.ReadU32(0)
		length, _ := mem.ReadU32(4)

		if length != 3 {
			t.Errorf("length = %d, want 3", length)
		}

		for i := uint32(0); i < length; i++ {
			v, _ := mem.ReadU32(dataPtr + i*4)
			if int32(v) != data[i] {
				t.Errorf("elem[%d] = %d, want %d", i, int32(v), data[i])
			}
		}
	})
}

func TestEncoderEncodeOption(t *testing.T) {
	e := NewEncoder()

	t.Run("some", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		alloc := newTestAlloc(100)

		ct := &CompiledType{
			Kind:     KindOption,
			ElemType: &CompiledType{Kind: KindU32, WitAlign: 4},
		}

		val := uint32(42)
		ptrVal := &val
		err := e.encodeOptionToMemory(0, ct, unsafe.Pointer(&ptrVal), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		disc, _ := mem.ReadU8(0)
		if disc != 1 {
			t.Errorf("discriminant = %d, want 1", disc)
		}
	})

	t.Run("none", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		alloc := newTestAlloc(100)

		ct := &CompiledType{
			Kind:     KindOption,
			ElemType: &CompiledType{Kind: KindU32, WitAlign: 4},
		}

		var ptrVal *uint32
		err := e.encodeOptionToMemory(0, ct, unsafe.Pointer(&ptrVal), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		disc, _ := mem.ReadU8(0)
		if disc != 0 {
			t.Errorf("discriminant = %d, want 0", disc)
		}
	})
}

// Tests for decodeFieldFromMemory

func TestDecoderDecodeFieldFromMemoryPrimitives(t *testing.T) {
	d := NewDecoder()

	t.Run("bool", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 1)
		var val bool
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindBool}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if !val {
			t.Error("expected true")
		}
	})

	t.Run("u8", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 42)
		var val uint8
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindU8}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != 42 {
			t.Errorf("got %d, want 42", val)
		}
	})

	t.Run("s8", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 0xF6)
		var val int8
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindS8}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != -10 {
			t.Errorf("got %d, want -10", val)
		}
	})

	t.Run("u16", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU16(0, 1234)
		var val uint16
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindU16}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != 1234 {
			t.Errorf("got %d, want 1234", val)
		}
	})

	t.Run("s16", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		expected := int16(-567)
		mem.mustWriteU16(0, uint16(expected))
		var val int16
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindS16}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != expected {
			t.Errorf("got %d, want %d", val, expected)
		}
	})

	t.Run("u32", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU32(0, 123456)
		var val uint32
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindU32}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != 123456 {
			t.Errorf("got %d, want 123456", val)
		}
	})

	t.Run("s32", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		expected := int32(-78901)
		mem.mustWriteU32(0, uint32(expected))
		var val int32
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindS32}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != expected {
			t.Errorf("got %d, want %d", val, expected)
		}
	})

	t.Run("u64", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU64(0, 123456789012)
		var val uint64
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindU64}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != 123456789012 {
			t.Errorf("got %d, want 123456789012", val)
		}
	})

	t.Run("s64", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		expected := int64(-987654321098)
		mem.mustWriteU64(0, uint64(expected))
		var val int64
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindS64}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != expected {
			t.Errorf("got %d, want %d", val, expected)
		}
	})

	t.Run("char", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU32(0, uint32('X'))
		var val rune
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindChar}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != 'X' {
			t.Errorf("got %c, want X", val)
		}
	})
}

func TestDecoderDecodeFloats(t *testing.T) {
	d := NewDecoder()

	t.Run("f32", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU32(0, 0x40490FDB)
		var val float32
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindF32}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val < 3.1 || val > 3.2 {
			t.Errorf("got %f, expected ~3.14", val)
		}
	})

	t.Run("f64", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU64(0, 0x400921FB54442D18)
		var val float64
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindF64}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val < 3.14 || val > 3.15 {
			t.Errorf("got %f, expected ~3.14159", val)
		}
	})
}

func TestDecoderDecodeString(t *testing.T) {
	d := NewDecoder()

	t.Run("non_empty", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		copy(mem.data[100:], []byte("hello world"))
		mem.mustWriteU32(0, 100)
		mem.mustWriteU32(4, 11)

		var val string
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindString}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != "hello world" {
			t.Errorf("got %q, want hello world", val)
		}
	})

	t.Run("empty", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU32(0, 0)
		mem.mustWriteU32(4, 0)

		var val string
		err := d.decodeFieldFromMemory(0, &CompiledType{Kind: KindString}, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}
		if val != "" {
			t.Errorf("got %q, want empty", val)
		}
	})
}

func TestDecoderDecodeRecord(t *testing.T) {
	d := NewDecoder()
	mem := newTestMem(t, 1024)

	type TestRecord struct {
		A uint32
		B uint64
	}

	ct := &CompiledType{
		Kind:   KindRecord,
		GoType: reflect.TypeOf(TestRecord{}),
		Fields: []CompiledField{
			{Name: "A", GoOffset: 0, WitOffset: 0, Type: &CompiledType{Kind: KindU32, GoType: reflect.TypeOf(uint32(0))}},
			{Name: "B", GoOffset: 8, WitOffset: 8, Type: &CompiledType{Kind: KindU64, GoType: reflect.TypeOf(uint64(0))}},
		},
	}

	mem.mustWriteU32(0, 42)
	mem.mustWriteU64(8, 123456)

	var rec TestRecord
	err := d.decodeRecordFromMemory(0, ct, unsafe.Pointer(&rec), mem, nil)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	if rec.A != 42 {
		t.Errorf("A = %d, want 42", rec.A)
	}
	if rec.B != 123456 {
		t.Errorf("B = %d, want 123456", rec.B)
	}
}

func TestDecoderDecodeList(t *testing.T) {
	d := NewDecoder()

	t.Run("bytes", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		copy(mem.data[100:], []byte{1, 2, 3, 4, 5})
		mem.mustWriteU32(0, 100)
		mem.mustWriteU32(4, 5)

		ct := &CompiledType{
			Kind:   KindList,
			GoType: reflect.TypeOf([]byte{}),
			ElemType: &CompiledType{
				Kind: KindU8, GoType: reflect.TypeOf(uint8(0)), WitSize: 1, WitAlign: 1,
			},
		}

		var val []byte
		err := d.decodeListFromMemory(0, ct, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		expected := []byte{1, 2, 3, 4, 5}
		if len(val) != len(expected) {
			t.Fatalf("length = %d, want %d", len(val), len(expected))
		}
		for i, b := range val {
			if b != expected[i] {
				t.Errorf("val[%d] = %d, want %d", i, b, expected[i])
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU32(0, 0)
		mem.mustWriteU32(4, 0)

		ct := &CompiledType{
			Kind:   KindList,
			GoType: reflect.TypeOf([]byte{}),
			ElemType: &CompiledType{
				Kind: KindU8, GoType: reflect.TypeOf(uint8(0)), WitSize: 1, WitAlign: 1,
			},
		}

		var val []byte
		err := d.decodeListFromMemory(0, ct, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		if len(val) != 0 {
			t.Errorf("expected empty, got len=%d", len(val))
		}
	})
}

func TestDecoderDecodeOption(t *testing.T) {
	d := NewDecoder()

	t.Run("some", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 1)
		mem.mustWriteU32(4, 42)

		ct := &CompiledType{
			Kind: KindOption,
			ElemType: &CompiledType{
				Kind: KindU32, GoType: reflect.TypeOf(uint32(0)), WitAlign: 4,
			},
		}

		var val *uint32
		err := d.decodeOptionFromMemory(0, ct, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		if val == nil {
			t.Fatal("expected non-nil")
		}
		if *val != 42 {
			t.Errorf("got %d, want 42", *val)
		}
	})

	t.Run("none", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 0)

		ct := &CompiledType{
			Kind: KindOption,
			ElemType: &CompiledType{
				Kind: KindU32, GoType: reflect.TypeOf(uint32(0)), WitAlign: 4,
			},
		}

		val := new(uint32)
		err := d.decodeOptionFromMemory(0, ct, unsafe.Pointer(&val), mem, nil)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		if val != nil {
			t.Error("expected nil")
		}
	})
}

// Tests for LoadValue

func TestDecoderLoadTypeDefRecord(t *testing.T) {
	d := NewDecoder()
	mem := newTestMem(t, 1024)

	mem.mustWriteU32(0, 42)
	mem.mustWriteU32(4, 100)
	mem.mustWriteU32(8, 5)
	copy(mem.data[100:], []byte("hello"))

	recordTypeDef := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.String{}},
			},
		},
	}

	result, err := d.LoadValue(recordTypeDef, 0, mem)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	rec := result.(map[string]any)
	if rec["a"] != uint32(42) {
		t.Errorf("a = %v, want 42", rec["a"])
	}
	if rec["b"] != "hello" {
		t.Errorf("b = %v, want hello", rec["b"])
	}
}

func TestDecoderLoadTypeDefList(t *testing.T) {
	d := NewDecoder()
	mem := newTestMem(t, 1024)

	mem.mustWriteU32(0, 100)
	mem.mustWriteU32(4, 3)
	mem.mustWriteU32(100, 10)
	mem.mustWriteU32(104, 20)
	mem.mustWriteU32(108, 30)

	listTypeDef := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	result, err := d.LoadValue(listTypeDef, 0, mem)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	list := result.([]any)
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}

	expected := []uint32{10, 20, 30}
	for i, v := range list {
		if v != expected[i] {
			t.Errorf("list[%d] = %v, want %d", i, v, expected[i])
		}
	}
}

func TestDecoderLoadTypeDefOption(t *testing.T) {
	d := NewDecoder()

	t.Run("some", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 1)
		mem.mustWriteU32(4, 42)

		optTypeDef := &wit.TypeDef{
			Kind: &wit.Option{Type: wit.U32{}},
		}

		result, err := d.LoadValue(optTypeDef, 0, mem)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		if result != uint32(42) {
			t.Errorf("got %v, want 42", result)
		}
	})

	t.Run("none", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 0)

		optTypeDef := &wit.TypeDef{
			Kind: &wit.Option{Type: wit.U32{}},
		}

		result, err := d.LoadValue(optTypeDef, 0, mem)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})
}

func TestDecoderLoadTypeDefTuple(t *testing.T) {
	d := NewDecoder()
	mem := newTestMem(t, 1024)

	mem.mustWriteU32(0, 42)
	mem.mustWriteU64(8, 123456)

	tupleTypeDef := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U32{}, wit.U64{}}},
	}

	result, err := d.LoadValue(tupleTypeDef, 0, mem)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	tuple := result.([]any)
	if len(tuple) != 2 {
		t.Fatalf("len = %d, want 2", len(tuple))
	}
	if tuple[0] != uint32(42) {
		t.Errorf("tuple[0] = %v, want 42", tuple[0])
	}
	if tuple[1] != uint64(123456) {
		t.Errorf("tuple[1] = %v, want 123456", tuple[1])
	}
}

func TestDecoderLoadTypeDefResult(t *testing.T) {
	d := NewDecoder()

	t.Run("ok", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 0)
		mem.mustWriteU32(4, 42)

		resultTypeDef := &wit.TypeDef{
			Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}},
		}

		result, err := d.LoadValue(resultTypeDef, 0, mem)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		res := result.(map[string]any)
		if res["ok"] != uint32(42) {
			t.Errorf("ok = %v, want 42", res["ok"])
		}
	})

	t.Run("err", func(t *testing.T) {
		mem := newTestMem(t, 1024)
		mem.mustWriteU8(0, 1)
		mem.mustWriteU32(4, 100)
		mem.mustWriteU32(8, 5)
		copy(mem.data[100:], []byte("error"))

		resultTypeDef := &wit.TypeDef{
			Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}},
		}

		result, err := d.LoadValue(resultTypeDef, 0, mem)
		if err != nil {
			t.Fatalf("failed: %v", err)
		}

		res := result.(map[string]any)
		if res["err"] != "error" {
			t.Errorf("err = %v, want error", res["err"])
		}
	})
}

func TestDecoderLoadTypeDefVariant(t *testing.T) {
	d := NewDecoder()
	mem := newTestMem(t, 1024)

	mem.mustWriteU32(0, 1)
	mem.mustWriteU32(4, 42)

	variantTypeDef := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "none"},
				{Name: "some", Type: wit.U32{}},
			},
		},
	}

	result, err := d.LoadValue(variantTypeDef, 0, mem)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	v := result.(map[string]any)
	if v["some"] != uint32(42) {
		t.Errorf("some = %v, want 42", v["some"])
	}
}
