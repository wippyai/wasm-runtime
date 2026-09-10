package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

func TestHandleLayoutSafety(t *testing.T) {
	for _, kind := range []wit.TypeDefKind{&wit.Own{}, &wit.Borrow{}} {
		def := &wit.TypeDef{Kind: kind}
		for _, value := range []any{struct{}{}, struct{ Small byte }{}, struct{ Pointer *uint32 }{}, struct{ Text string }{}, struct{ hidden uint32 }{}} {
			if _, err := NewCompiler().Compile(def, reflect.TypeOf(value)); err == nil {
				t.Fatalf("accepted unsafe %T handle layout %T", kind, value)
			}
		}
		type Handle struct { //nolint:govet // The ABI requires the uint32 field at offset zero.
			ID   uint32
			Keep *uint32
		}
		keep := uint32(123)
		out := Handle{Keep: &keep}
		ct, err := NewCompiler().Compile(def, reflect.TypeOf(out))
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewDecoder().LiftFromStack(ct, []uint64{42}, unsafe.Pointer(&out), newMockMemory(64))
		if err != nil {
			t.Fatal(err)
		}
		stack := make([]uint64, 1)
		mem := newMockMemory(64)
		if _, err := NewEncoder().LowerToStack(ct, unsafe.Pointer(&out), stack, mem, newMockAllocator(mem)); err != nil {
			t.Fatal(err)
		}
		if stack[0] != 42 {
			t.Fatalf("lost handle during lowering: %v", stack)
		}
		if err := NewEncoder().StoreCompiledToMemory(0, ct, unsafe.Pointer(&out), mem, newMockAllocator(mem), nil); err != nil {
			t.Fatal(err)
		}
		out.ID = 0
		if err := NewDecoder().decodeFieldFromMemory(0, ct, unsafe.Pointer(&out), mem, nil); err != nil {
			t.Fatal(err)
		}
		if out.ID != 42 || out.Keep != &keep || *out.Keep != 123 {
			t.Fatalf("corrupted handle wrapper: %+v", out)
		}
	}
}
