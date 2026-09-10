package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

// These cases exercise the direct slice-header path used by
// encodeListToMemory. They cover representations for which reflect.Index used
// to supply the element address: a named slice, nested slice headers, and a
// zero-sized Go element.
func TestEncodeListToMemory_CompiledSliceHeader(t *testing.T) {
	enc := NewEncoder()

	t.Run("named scalar slice", func(t *testing.T) {
		type words []uint32
		values := words{0x01020304, 0xa0b0c0d0}
		ct, err := enc.compiler.Compile(
			&wit.TypeDef{Kind: &wit.List{Type: wit.U32{}}},
			reflect.TypeOf(values),
		)
		if err != nil {
			t.Fatal(err)
		}
		mem := newMockMemory(4096)
		if err := enc.encodeListToMemory(0, ct, unsafe.Pointer(&values), mem, newMockAllocator(mem), nil, nil); err != nil {
			t.Fatal(err)
		}
		addr, err := mem.ReadU32(0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := mem.ReadU32(addr + 4)
		if err != nil {
			t.Fatal(err)
		}
		if got != values[1] {
			t.Fatalf("second element = %#x, want %#x", got, values[1])
		}
	})

	t.Run("nested list", func(t *testing.T) {
		values := [][]uint32{{7, 8}, {9}}
		typ := &wit.TypeDef{Kind: &wit.List{Type: &wit.TypeDef{Kind: &wit.List{Type: wit.U32{}}}}}
		ct, err := enc.compiler.Compile(typ, reflect.TypeOf(values))
		if err != nil {
			t.Fatal(err)
		}
		mem := newMockMemory(4096)
		if err := enc.encodeListToMemory(0, ct, unsafe.Pointer(&values), mem, newMockAllocator(mem), nil, nil); err != nil {
			t.Fatal(err)
		}
		outerAddr, _ := mem.ReadU32(0)
		firstAddr, _ := mem.ReadU32(outerAddr)
		firstLen, _ := mem.ReadU32(outerAddr + 4)
		secondAddr, _ := mem.ReadU32(outerAddr + 8)
		secondLen, _ := mem.ReadU32(outerAddr + 12)
		if firstLen != 2 || secondLen != 1 {
			t.Fatalf("nested lengths = (%d, %d), want (2, 1)", firstLen, secondLen)
		}
		firstSecond, _ := mem.ReadU32(firstAddr + 4)
		secondFirst, _ := mem.ReadU32(secondAddr)
		if firstSecond != 8 || secondFirst != 9 {
			t.Fatalf("nested values = (%d, %d), want (8, 9)", firstSecond, secondFirst)
		}
	})

	t.Run("zero sized record elements", func(t *testing.T) {
		values := []struct{}{{}, {}}
		typ := &wit.TypeDef{Kind: &wit.List{Type: &wit.TypeDef{Kind: &wit.Record{}}}}
		ct, err := enc.compiler.Compile(typ, reflect.TypeOf(values))
		if err != nil {
			t.Fatal(err)
		}
		mem := newMockMemory(4096)
		if err := enc.encodeListToMemory(0, ct, unsafe.Pointer(&values), mem, newMockAllocator(mem), nil, nil); err != nil {
			t.Fatal(err)
		}
		length, err := mem.ReadU32(4)
		if err != nil {
			t.Fatal(err)
		}
		if length != uint32(len(values)) {
			t.Fatalf("length = %d, want %d", length, len(values))
		}
	})
}
