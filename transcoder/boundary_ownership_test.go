package transcoder

import (
	"reflect"
	"strconv"
	"sync"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

type nonComparableWIT struct {
	wit.Type
	extra []byte
}

func TestCompilerRejectsInvalidWITWithoutPanic(t *testing.T) {
	for _, typ := range []wit.Type{nil, (*wit.TypeDef)(nil), nonComparableWIT{Type: wit.U32{}, extra: []byte{1}}, struct{ wit.Type }{nonComparableWIT{Type: wit.U32{}, extra: []byte{1}}}} {
		if _, err := NewCompiler().Compile(typ, reflect.TypeOf(uint32(0))); err == nil {
			t.Errorf("accepted invalid WIT %T", typ)
		}
	}
}

func TestCanonicalListHostLengthAdmission(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("requires 64-bit int")
	}
	for _, count := range []uint64{uint64(MaxListLength) + 1, 1 << 32, (1 << 32) + 1} {
		for _, path := range []string{"compiled_memory", "compiled_stack", "dynamic_memory", "dynamic_stack"} {
			t.Run(strconv.FormatUint(count, 10)+"/"+path, func(t *testing.T) {
				values := make([]struct{}, int(count))
				typ := &wit.TypeDef{Kind: &wit.List{Type: &wit.TypeDef{Kind: &wit.Record{}}}}
				enc := NewEncoder()
				ct, err := enc.compiler.Compile(typ, reflect.TypeOf(values))
				if err != nil {
					t.Fatal(err)
				}
				mem := newMockMemory(4096)
				alloc := newMockAllocator(mem)
				switch path {
				case "compiled_memory":
					err = enc.StoreCompiledToMemory(0, ct, unsafe.Pointer(&values), mem, alloc, nil)
				case "compiled_stack":
					_, err = enc.LowerToStack(ct, unsafe.Pointer(&values), make([]uint64, 2), mem, alloc)
				case "dynamic_memory":
					err = enc.StoreToMemory(typ, values, 0, mem, alloc, nil)
				case "dynamic_stack":
					_, err = enc.EncodeParams([]wit.Type{typ}, []any{values}, mem, alloc, nil)
				}
				if err == nil {
					t.Fatal("oversized host list was not rejected before narrowing")
				}
				if alloc.offset != 1024 {
					t.Fatal("oversized list allocated guest memory before admission")
				}
			})
		}
	}
}

func TestFlagsCanonicalRangeAndUnusedBits(t *testing.T) {
	for _, count := range []int{0, 33, 64} {
		typ := semanticFlags(count)
		if _, err := NewCompiler().Compile(typ, reflect.TypeOf(uint64(0))); err == nil {
			t.Errorf("accepted noncanonical flag count%d", count)
		}
	}
	for _, count := range []int{1, 8, 9, 16, 17, 32} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			c := NewCompiler()
			typ := semanticFlags(count)
			ct, err := c.Compile(typ, reflect.TypeOf(uint64(0)))
			if err != nil {
				t.Fatal(err)
			}
			mem := newMockMemory(4096)
			value := ^uint64(0)
			enc := NewEncoderWithCompiler(c)
			if err := enc.StoreCompiledToMemory(0, ct, unsafe.Pointer(&value), mem, nil, nil); err != nil {
				t.Fatal(err)
			}
			decoded := ^uint64(0)
			dec := NewDecoderWithCompiler(c)
			if err := dec.decodeFieldFromMemory(0, ct, unsafe.Pointer(&decoded), mem, nil); err != nil {
				t.Fatal(err)
			}
			want := (uint64(1) << count) - 1
			if decoded != want {
				t.Fatalf("stale/undeclared bits retained:%#x want%#x", decoded, want)
			}
			// Independently feed all bits set, including canonical padding, into decoder.
			for i := uint32(0); i < ct.WitSize; i++ {
				mem.data[i] = 255
			}
			decoded = ^uint64(0)
			if err := dec.decodeFieldFromMemory(0, ct, unsafe.Pointer(&decoded), mem, nil); err != nil {
				t.Fatal(err)
			}
			if decoded != want {
				t.Fatalf("padding bits leaked:%#x want%#x", decoded, want)
			}
			decoded = ^uint64(0)
			if _, err := dec.LiftFromStack(ct, []uint64{0xffffffff}, unsafe.Pointer(&decoded), mem); err != nil {
				t.Fatal(err)
			}
			if decoded != want {
				t.Fatalf("flat flags leaked:%#x want%#x", decoded, want)
			}
		})
	}
}

func TestCompilerAndDynamicLayoutConcurrent(t *testing.T) {
	type pair struct {
		A uint8
		B uint64
	}
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 100; i++ {
				typ := &wit.TypeDef{Kind: &wit.Record{Fields: []wit.Field{{Name: "a", Type: wit.U8{}}, {Name: "b", Type: wit.U64{}}}}}
				ct, err := compiler.Compile(typ, reflect.TypeOf(pair{}))
				if err != nil {
					t.Error(err)
					return
				}
				if ct.WitSize != 16 {
					t.Errorf("record layout size=%d", ct.WitSize)
					return
				}
				mem := newMockMemory(64)
				if err := enc.StoreToMemory(typ, map[string]any{"a": uint8(1), "b": uint64(2)}, 0, mem, nil, nil); err != nil {
					t.Error(err)
					return
				}
				if b, _ := mem.ReadU64(8); b != 2 {
					t.Errorf("concurrent layout corrupt:%d", b)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestDynamicFlagsRejectNoncanonicalDeclarations(t *testing.T) {
	for _, count := range []int{0, 33, 64} {
		typ := semanticFlags(count)
		mem := newMockMemory(4096)
		enc := NewEncoder()
		if err := enc.StoreToMemory(typ, map[string]bool{}, 0, mem, nil, nil); err == nil {
			t.Errorf("memory accepted %d flags", count)
		}
		if _, err := enc.EncodeParams([]wit.Type{typ}, []any{uint64(0)}, mem, nil, nil); err == nil {
			t.Errorf("flat accepted %d flags", count)
		}
		if _, _, err := NewDecoder().liftFlags(typ.Kind.(*wit.Flags), []uint64{0}, nil); err == nil {
			t.Errorf("lift accepted %d flags", count)
		}
	}
}
