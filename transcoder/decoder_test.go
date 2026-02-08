package transcoder

import (
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestDecoder_New(t *testing.T) {
	d := NewDecoder()
	if d == nil {
		t.Fatal("NewDecoder returned nil")
	}
}

func TestDecoder_NewWithCompiler(t *testing.T) {
	c := NewCompiler()
	d := NewDecoderWithCompiler(c)
	if d == nil {
		t.Fatal("NewDecoderWithCompiler returned nil")
	}
}

func TestDecoder_DecodeResultsPrimitives(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	// Test bool
	flat := []uint64{1}
	results, err := d.DecodeResults([]wit.Type{wit.Bool{}}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults bool failed: %v", err)
	}
	if len(results) != 1 || results[0] != true {
		t.Errorf("expected [true], got %v", results)
	}

	// Test u32
	flat = []uint64{42}
	results, err = d.DecodeResults([]wit.Type{wit.U32{}}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults u32 failed: %v", err)
	}
	if len(results) != 1 || results[0] != uint32(42) {
		t.Errorf("expected [42], got %v", results)
	}

	// Test s32
	flat = []uint64{0xFFFFFFFF} // -1 in two's complement for i32
	results, err = d.DecodeResults([]wit.Type{wit.S32{}}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults s32 failed: %v", err)
	}
	if len(results) != 1 || results[0] != int32(-1) {
		t.Errorf("expected [-1], got %v", results)
	}
}

func TestDecoder_DecodeResultsString(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	// Write string "hello" at offset 100
	copy(mem.data[100:], []byte("hello"))

	// Flat params: [ptr, len]
	flat := []uint64{100, 5}

	results, err := d.DecodeResults([]wit.Type{wit.String{}}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults string failed: %v", err)
	}
	if len(results) != 1 || results[0] != "hello" {
		t.Errorf("expected ['hello'], got %v", results)
	}
}

func TestDecoder_DecodeResultsEmptyString(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	flat := []uint64{0, 0}
	results, err := d.DecodeResults([]wit.Type{wit.String{}}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults empty string failed: %v", err)
	}
	if len(results) != 1 || results[0] != "" {
		t.Errorf("expected [''], got %v", results)
	}
}

func TestDecoder_LoadValueString(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	// Write string data at offset 200
	copy(mem.data[200:], []byte("test"))

	// Write pointer (200) and length (4) at offset 0
	mem.WriteU32(0, 200)
	mem.WriteU32(4, 4)

	result, err := d.LoadValue(wit.String{}, 0, mem)
	if err != nil {
		t.Fatalf("LoadValue string failed: %v", err)
	}
	if result != "test" {
		t.Errorf("expected 'test', got %v", result)
	}
}

func TestDecoder_LoadValueU32(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	mem.WriteU32(0, 123)
	result, err := d.LoadValue(wit.U32{}, 0, mem)
	if err != nil {
		t.Fatalf("LoadValue u32 failed: %v", err)
	}
	if result != uint32(123) {
		t.Errorf("expected 123, got %v", result)
	}
}

func TestDecoder_DecodeResultsMultiple(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	// Multiple results: (u32, bool, s64)
	flat := []uint64{42, 1, 0xFFFFFFFFFFFFFFFF} // 42, true, -1
	types := []wit.Type{wit.U32{}, wit.Bool{}, wit.S64{}}

	results, err := d.DecodeResults(types, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults multiple failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0] != uint32(42) {
		t.Errorf("expected u32=42, got %v", results[0])
	}
	if results[1] != true {
		t.Errorf("expected bool=true, got %v", results[1])
	}
	if results[2] != int64(-1) {
		t.Errorf("expected s64=-1, got %v", results[2])
	}
}

func TestDecoder_DecodeResultsChar(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	flat := []uint64{0x1F600} // emoji
	results, err := d.DecodeResults([]wit.Type{wit.Char{}}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults char failed: %v", err)
	}
	if len(results) != 1 || results[0] != rune(0x1F600) {
		t.Errorf("expected emoji rune, got %v", results)
	}
}

func TestDecoder_DecodeResultsFloat(t *testing.T) {
	d := NewDecoder()
	mem := &mockMemory{data: make([]byte, 1024)}

	flat := []uint64{0x40490FDB} // ~3.14159 as f32 bits
	results, err := d.DecodeResults([]wit.Type{wit.F32{}}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults f32 failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}
