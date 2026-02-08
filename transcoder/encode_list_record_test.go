package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

func TestEncode_SingleRecordInList(t *testing.T) {
	mem := newTestMemory(10000)
	alloc := newTestAllocator(mem.data, 1000)
	compiler := NewCompiler()

	recordTypeDef := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "id", Type: wit.U32{}},
				{Name: "name", Type: wit.String{}},
			},
		},
	}
	listTypeDef := &wit.TypeDef{Kind: &wit.List{Type: recordTypeDef}}

	goType := reflect.TypeOf([]SimpleRecord{})
	compiled, err := compiler.Compile(listTypeDef, goType)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	input := []SimpleRecord{
		{ID: 42, Name: "Test"},
	}

	inputPtr := unsafe.Pointer(&input)

	var stack [2]uint64
	encoder := NewEncoder()
	consumed, err := encoder.LowerToStack(compiled, inputPtr, stack[:], mem, alloc)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	t.Logf("Encoded: consumed=%d stack slots", consumed)
	t.Logf("Stack[0] (dataAddr)=%d, Stack[1] (length)=%d", stack[0], stack[1])

	if stack[1] != 1 {
		t.Fatalf("expected length=1, got %d", stack[1])
	}

	dataAddr := uint32(stack[0])
	recordSize := compiled.ElemType.WitSize

	recordData, err := mem.Read(dataAddr, recordSize)
	if err != nil {
		t.Fatalf("failed to read record: %v", err)
	}

	t.Logf("Record data (hex): %x", recordData)

	id := uint32(recordData[0]) | uint32(recordData[1])<<8 | uint32(recordData[2])<<16 | uint32(recordData[3])<<24
	t.Logf("Decoded ID: %d", id)

	if id != 42 {
		t.Errorf("ID: expected 42, got %d", id)
	}

	nameAddr := uint32(recordData[4]) | uint32(recordData[5])<<8 | uint32(recordData[6])<<16 | uint32(recordData[7])<<24
	nameLen := uint32(recordData[8]) | uint32(recordData[9])<<8 | uint32(recordData[10])<<16 | uint32(recordData[11])<<24

	t.Logf("Name: addr=%d, len=%d", nameAddr, nameLen)

	if nameLen != 4 {
		t.Errorf("Name length: expected 4, got %d", nameLen)
	}

	nameData, err := mem.Read(nameAddr, nameLen)
	if err != nil {
		t.Fatalf("failed to read name: %v", err)
	}

	if string(nameData) != "Test" {
		t.Errorf("Name: expected %q, got %q", "Test", string(nameData))
	}
}
