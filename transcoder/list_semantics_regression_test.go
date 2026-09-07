package transcoder

import (
	"reflect"
	"strconv"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

func TestListSemanticCharRejectsSurrogate(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	typ := &wit.TypeDef{Kind: &wit.List{Type: wit.Char{}}}
	values := []rune{0xd800}
	ct, err := enc.compiler.Compile(typ, reflect.TypeOf(values))
	if err != nil {
		t.Fatal(err)
	}
	if err = enc.StoreCompiledToMemory(0, ct, unsafe.Pointer(&values), mem, alloc, nil); err == nil {
		t.Fatal("list<char> accepted a surrogate through its Go int32 representation")
	}
}
func TestListSemanticEnumUsesCanonicalWidth(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	elem := &wit.TypeDef{Kind: &wit.Enum{Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}}}}
	typ := &wit.TypeDef{Kind: &wit.List{Type: elem}}
	values := []uint32{1, 1}
	ct, err := enc.compiler.Compile(typ, reflect.TypeOf(values))
	if err != nil {
		t.Fatal(err)
	}
	if err = enc.StoreCompiledToMemory(0, ct, unsafe.Pointer(&values), mem, alloc, nil); err != nil {
		t.Fatal(err)
	}
	ptr, err := mem.ReadU32(0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := mem.Read(ptr, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 1 || got[1] != 1 {
		t.Fatalf("canonical list<enum> bytes=%v, want [1 1]", got)
	}
}
func TestListSemanticLengthDoesNotWrap(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("requires 64-bit int")
	}
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	count := uint64(1) << 32
	values := make([]struct{}, int(count))
	typ := &wit.TypeDef{Kind: &wit.List{Type: &wit.TypeDef{Kind: &wit.Record{}}}}
	ct, err := enc.compiler.Compile(typ, reflect.TypeOf(values))
	if err != nil {
		t.Fatal(err)
	}
	if err = enc.StoreCompiledToMemory(0, ct, unsafe.Pointer(&values), mem, alloc, nil); err == nil {
		t.Fatal("2^32 zero-sized records silently encoded as an empty list")
	}
}

func TestPrimitiveCacheSeparatesCharAndS32(t *testing.T) {
	for _, charFirst := range []bool{false, true} {
		enc := NewEncoder()
		order := []wit.Type{wit.S32{}, wit.Char{}}
		if charFirst {
			order = []wit.Type{wit.Char{}, wit.S32{}}
		}
		compiled := map[bool]*CompiledType{}
		for _, typ := range order {
			ct, err := enc.compiler.Compile(typ, reflect.TypeOf(int32(0)))
			if err != nil {
				t.Fatal(err)
			}
			_, isChar := typ.(wit.Char)
			compiled[isChar] = ct
		}
		value := int32(-1)
		stack := make([]uint64, 1)
		if _, err := enc.LowerToStack(compiled[true], unsafe.Pointer(&value), stack, nil, nil); err == nil {
			t.Errorf("charFirst=%v: cached s32 semantics accepted invalid char", charFirst)
		}
		if _, err := enc.LowerToStack(compiled[false], unsafe.Pointer(&value), stack, nil, nil); err != nil {
			t.Errorf("charFirst=%v: cached char semantics rejected valid s32: %v", charFirst, err)
		}
	}
}

func semanticFlags(count int) *wit.TypeDef {
	labels := make([]wit.Flag, count)
	for i := range labels {
		labels[i].Name = "flag" + strconv.Itoa(i)
	}
	return &wit.TypeDef{Kind: &wit.Flags{Flags: labels}}
}
func semanticEnum(count int) *wit.TypeDef {
	cases := make([]wit.EnumCase, count)
	for i := range cases {
		cases[i].Name = "case" + strconv.Itoa(i)
	}
	return &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
}

func TestFlagsRejectInsufficientGoStorage(t *testing.T) {
	for _, tc := range []struct {
		value any
		count int
	}{
		{value: uint8(0), count: 9},
		{value: uint16(0), count: 17},
		{value: uint32(0), count: 33},
	} {
		if _, err := NewCompiler().Compile(semanticFlags(tc.count), reflect.TypeOf(tc.value)); err == nil {
			t.Errorf("%d flags accepted Go %T storage", tc.count, tc.value)
		}
	}
}

func TestCanonicalListWireLayoutMatrix(t *testing.T) {
	tests := []struct {
		name   string
		elem   wit.Type
		values any
		wire   []byte
	}{
		{"enum_u32", semanticEnum(2), []uint32{1, 0, 1}, []byte{1, 0, 1}},
		{"enum_u16", semanticEnum(2), []uint16{1, 0, 1}, []byte{1, 0, 1}},
		{"enum_wide", semanticEnum(300), []uint32{256, 299}, []byte{0, 1, 43, 1}},
		{"flags3_u64", semanticFlags(3), []uint64{7, 0, 3}, []byte{7, 0, 3}},
		{"flags9_u64", semanticFlags(9), []uint64{257, 511}, []byte{1, 1, 255, 1}},
		{"flags17_u64", semanticFlags(17), []uint64{65537}, []byte{1, 0, 1, 0}},
		{"char", wit.Char{}, []rune{'A', 0x10ffff}, []byte{65, 0, 0, 0, 255, 255, 16, 0}},
		{"u32", wit.U32{}, []uint32{0xffffffff}, []byte{255, 255, 255, 255}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiler := NewCompiler()
			typ := &wit.TypeDef{Kind: &wit.List{Type: tc.elem}}
			goType := reflect.TypeOf(tc.values)
			ct, err := compiler.Compile(typ, goType)
			if err != nil {
				t.Fatal(err)
			}
			for _, stackMode := range []bool{false, true} {
				t.Run(map[bool]string{false: "memory", true: "stack"}[stackMode], func(t *testing.T) {
					enc := NewEncoderWithCompiler(compiler)
					mem := newMockMemory(4096)
					for i := range mem.data {
						mem.data[i] = 0xa5
					}
					alloc := newMockAllocator(mem)
					rv := reflect.New(goType)
					rv.Elem().Set(reflect.ValueOf(tc.values))
					stack := make([]uint64, 2)
					if stackMode {
						_, err = enc.LowerToStack(ct, unsafe.Pointer(rv.Pointer()), stack, mem, alloc)
					} else {
						err = enc.StoreCompiledToMemory(0, ct, unsafe.Pointer(rv.Pointer()), mem, alloc, nil)
					}
					if err != nil {
						t.Fatal(err)
					}
					var ptr, length uint32
					if stackMode {
						ptr, length = uint32(stack[0]), uint32(stack[1])
					} else {
						ptr, _ = mem.ReadU32(0)
						length, _ = mem.ReadU32(4)
					}
					if int(length) != reflect.ValueOf(tc.values).Len() {
						t.Fatalf("length=%d", length)
					}
					got, _ := mem.Read(ptr, uint32(len(tc.wire)))
					if !reflect.DeepEqual(got, tc.wire) {
						t.Errorf("wire=%v want%v", got, tc.wire)
					}
					for i := ptr + uint32(len(tc.wire)); i < ptr+uint32(len(tc.wire))+8; i++ {
						if mem.data[i] != 0xa5 {
							t.Errorf("write beyond canonical element area at%d", i)
							break
						}
					}
					// Decode independent hand-authored wire bytes; never roundtrip encoder output.
					readMem := newMockMemory(4096)
					for i := range readMem.data {
						readMem.data[i] = 0xa5
					}
					_ = readMem.Write(128, tc.wire)
					_ = readMem.WriteU32(0, 128)
					_ = readMem.WriteU32(4, length)
					dest := reflect.New(goType)
					dec := NewDecoderWithCompiler(compiler)
					if stackMode {
						_, err = dec.LiftFromStack(ct, []uint64{128, uint64(length)}, unsafe.Pointer(dest.Pointer()), readMem)
					} else {
						err = dec.decodeFieldFromMemory(0, ct, unsafe.Pointer(dest.Pointer()), readMem, nil)
					}
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(dest.Elem().Interface(), tc.values) {
						t.Errorf("decoded=%v want%v", dest.Elem().Interface(), tc.values)
					}
				})
			}
		})
	}
}

func TestCanonicalListRejectsInvalidWireValues(t *testing.T) {
	for _, tc := range []struct {
		elem   wit.Type
		goType reflect.Type
		name   string
		wire   uint32
	}{
		{name: "surrogate", elem: wit.Char{}, goType: reflect.TypeOf([]rune{}), wire: 0xd800},
		{name: "out_of_unicode", elem: wit.Char{}, goType: reflect.TypeOf([]rune{}), wire: 0x110000},
		{name: "enum", elem: semanticEnum(2), goType: reflect.TypeOf([]uint32{}), wire: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCompiler()
			ct, err := c.Compile(&wit.TypeDef{Kind: &wit.List{Type: tc.elem}}, tc.goType)
			if err != nil {
				t.Fatal(err)
			}
			for _, stackMode := range []bool{false, true} {
				mem := newMockMemory(4096)
				_ = mem.WriteU32(128, tc.wire)
				_ = mem.WriteU32(0, 128)
				_ = mem.WriteU32(4, 1)
				dest := reflect.New(tc.goType)
				d := NewDecoderWithCompiler(c)
				if stackMode {
					_, err = d.LiftFromStack(ct, []uint64{128, 1}, unsafe.Pointer(dest.Pointer()), mem)
				} else {
					err = d.decodeFieldFromMemory(0, ct, unsafe.Pointer(dest.Pointer()), mem, nil)
				}
				if err == nil {
					t.Errorf("stack=%v accepted invalid wire value%#x", stackMode, tc.wire)
				}
			}
		})
	}
}
