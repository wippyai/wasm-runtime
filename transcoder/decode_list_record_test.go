package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

type SimpleRecord struct {
	Name string `wit:"name"`
	ID   uint32 `wit:"id"`
}

func TestDecode_SingleRecordInList(t *testing.T) {
	mem := newTestMemory(10000)
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

	t.Logf("Compiled type:")
	t.Logf("  Kind: %v", compiled.Kind)
	t.Logf("  ElemType.Kind: %v", compiled.ElemType.Kind)
	t.Logf("  ElemType.WitSize: %d", compiled.ElemType.WitSize)
	t.Logf("  ElemType.GoSize: %d", compiled.ElemType.GoSize)
	t.Logf("  ElemType.GoType.Kind: %v", compiled.ElemType.GoType.Kind())

	recordSize := compiled.ElemType.WitSize
	dataAddr := uint32(1000)

	recordData := make([]byte, recordSize)
	for i := range recordData {
		recordData[i] = 0
	}

	recordData[0] = 42
	recordData[1] = 0
	recordData[2] = 0
	recordData[3] = 0

	nameData := []byte("Test")
	nameAddr := uint32(2000)
	mem.Write(nameAddr, nameData)

	recordData[4] = byte(nameAddr)
	recordData[5] = byte(nameAddr >> 8)
	recordData[6] = byte(nameAddr >> 16)
	recordData[7] = byte(nameAddr >> 24)
	recordData[8] = 4
	recordData[9] = 0
	recordData[10] = 0
	recordData[11] = 0

	mem.Write(dataAddr, recordData)

	var stack [2]uint64
	stack[0] = uint64(dataAddr)
	stack[1] = 1

	var result []SimpleRecord
	resultPtr := unsafe.Pointer(&result)

	t.Logf("Before decode:")
	t.Logf("  result slice: len=%d, cap=%d", len(result), cap(result))
	if len(result) > 0 {
		t.Logf("  result[0]: ID=%d, Name=%q", result[0].ID, result[0].Name)
	}

	decoder := NewDecoder()
	consumed, err := decoder.LiftFromStack(compiled, stack[:], resultPtr, mem)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	t.Logf("After decode:")
	t.Logf("  consumed: %d stack slots", consumed)
	t.Logf("  result slice: len=%d, cap=%d", len(result), cap(result))

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	t.Logf("  result[0]: ID=%d, Name=%q", result[0].ID, result[0].Name)

	if result[0].ID != 42 {
		t.Errorf("ID: expected 42, got %d (0x%x)", result[0].ID, result[0].ID)
	}
	if result[0].Name != "Test" {
		t.Errorf("Name: expected %q, got %q", "Test", result[0].Name)
	}
}
