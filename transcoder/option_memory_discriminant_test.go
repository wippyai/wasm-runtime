package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

func TestMemoryOptionRejectsInvalidDiscriminant(t *testing.T) {
	compiler := NewCompiler()
	ct, err := compiler.Compile(&wit.TypeDef{Kind: &wit.Option{Type: wit.U32{}}}, reflect.TypeFor[*uint32]())
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoderWithCompiler(compiler)
	for _, tag := range []byte{2, 255} {
		memory := newTestMemory(8)
		memory.data[0] = tag
		original := uint32(42)
		target := &original
		if err := decoder.decodeOptionFromMemory(0, ct, unsafe.Pointer(&target), memory, nil); err == nil {
			t.Fatalf("accepted option tag %d", tag)
		}
		if target != &original || original != 42 {
			t.Fatal("invalid option mutated destination")
		}
	}
}
