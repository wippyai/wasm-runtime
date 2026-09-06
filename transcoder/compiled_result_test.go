package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

func TestStoreCompiledToMemory_NilChecks(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(64)

	ct, err := enc.compiler.Compile(wit.Bool{}, reflect.TypeOf(false))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	b := true
	// nil compiled type
	if err := enc.StoreCompiledToMemory(0, nil, unsafe.Pointer(&b), mem, nil, nil); err == nil {
		t.Error("expected error for nil CompiledType")
	}

	// nil pointer
	if err := enc.StoreCompiledToMemory(0, ct, nil, mem, nil, nil); err == nil {
		t.Error("expected error for nil pointer")
	}
}
