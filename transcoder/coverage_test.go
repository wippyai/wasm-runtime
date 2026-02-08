package transcoder

import (
	"math"
	"reflect"
	"strconv"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

// Test list<u8> encoding/decoding
func TestEncoder_ListU8(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U8{}},
	}

	input := []uint8{1, 2, 3, 4, 5}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]uint8)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i, v := range result {
		if v != input[i] {
			t.Errorf("element %d: got %v, want %v", i, v, input[i])
		}
	}
}

// Test list<u16> encoding/decoding
func TestEncoder_ListU16(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U16{}},
	}

	input := []uint16{100, 200, 300, 400}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]uint16)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i, v := range result {
		if v != input[i] {
			t.Errorf("element %d: got %v, want %v", i, v, input[i])
		}
	}
}

// Test list<u64> encoding/decoding
func TestEncoder_ListU64(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U64{}},
	}

	input := []uint64{1000000, 2000000, 3000000}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]uint64)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}
}

// Test list<s8> encoding/decoding
func TestEncoder_ListS8(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.S8{}},
	}

	input := []int8{-1, -2, 3, 4}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]int8)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}
}

// Test list<s16> encoding/decoding
func TestEncoder_ListS16(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.S16{}},
	}

	input := []int16{-100, -200, 300, 400}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]int16)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}
}

// Test list<s64> encoding/decoding
func TestEncoder_ListS64(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.S64{}},
	}

	input := []int64{-1000000, 2000000, -3000000}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]int64)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}
}

// Test list<f32> encoding/decoding
func TestEncoder_ListF32(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.F32{}},
	}

	input := []float32{1.1, 2.2, 3.3}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]float32)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}
}

// Test list<f64> encoding/decoding
func TestEncoder_ListF64(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.F64{}},
	}

	input := []float64{1.1, 2.2, 3.3}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]float64)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}
}

// Test list<string> encoding/decoding
func TestEncoder_ListString(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(8192)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.String{}},
	}

	input := []string{"hello", "world", "test"}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]string)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i, v := range result {
		if v != input[i] {
			t.Errorf("element %d: got %q, want %q", i, v, input[i])
		}
	}
}

// Test list<bool> encoding/decoding
func TestEncoder_ListBool(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.Bool{}},
	}

	input := []bool{true, false, true, false}
	inputAny := make([]any, len(input))
	for i, v := range input {
		inputAny[i] = v
	}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{inputAny}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]any)
	if len(result) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(result), len(input))
	}

	for i, v := range result {
		if v.(bool) != input[i] {
			t.Errorf("element %d: got %v, want %v", i, v, input[i])
		}
	}
}

// Test empty list encoding/decoding
func TestEncoder_EmptyList(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	input := []any{}

	flat, err := enc.EncodeParams([]wit.Type{listType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{listType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]uint32)
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d elements", len(result))
	}
}

// Test variant encoding/decoding
func TestEncoder_VariantCoverage(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "none"},
				{Name: "some", Type: wit.U32{}},
			},
		},
	}

	t.Run("none", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"none": nil}
		flat, err := enc.EncodeParams([]wit.Type{variantType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{variantType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if _, ok := result["none"]; !ok {
			t.Errorf("expected 'none' variant, got %v", result)
		}
	})

	t.Run("some", func(t *testing.T) {
		allocList.Reset()
		alloc.offset = 1024

		input := map[string]any{"some": uint32(42)}
		flat, err := enc.EncodeParams([]wit.Type{variantType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{variantType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		result := results[0].(map[string]any)
		if v, ok := result["some"]; !ok || v != uint32(42) {
			t.Errorf("expected some(42), got %v", result)
		}
	})
}

// Test tuple encoding/decoding
func TestEncoder_TupleCoverage(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U64{}, wit.Bool{}},
		},
	}

	input := []any{uint32(42), uint64(100), true}

	flat, err := enc.EncodeParams([]wit.Type{tupleType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{tupleType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]any)
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
}

// Test compiled struct list encode/decode using LowerToStack and LiftFromStack
func TestCompiled_ListU8(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U8{}},
	}

	compiled, err := compiler.Compile(listType, reflect.TypeOf([]uint8{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	input := []uint8{1, 2, 3, 4, 5}
	stack := make([]uint64, 16)

	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}

	var output []uint8
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}

	if len(output) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(output), len(input))
	}

	for i, v := range output {
		if v != input[i] {
			t.Errorf("element %d: got %v, want %v", i, v, input[i])
		}
	}
}

// Test compiled struct list for u64
func TestCompiled_ListU64(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U64{}},
	}

	compiled, err := compiler.Compile(listType, reflect.TypeOf([]uint64{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	input := []uint64{1000000, 2000000, 3000000}
	stack := make([]uint64, 16)

	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}

	var output []uint64
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}

	if len(output) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(output), len(input))
	}
}

// Test compiled string list
func TestCompiled_ListString(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(8192)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.String{}},
	}

	compiled, err := compiler.Compile(listType, reflect.TypeOf([]string{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	input := []string{"hello", "world"}
	stack := make([]uint64, 16)

	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}

	var output []string
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}

	if len(output) != len(input) {
		t.Fatalf("length mismatch: got %d, want %d", len(output), len(input))
	}

	for i, v := range output {
		if v != input[i] {
			t.Errorf("element %d: got %q, want %q", i, v, input[i])
		}
	}
}

// Test stack bounds checking
func TestStack_BoundsCheck(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()

	// Compile a simple u32 type
	compiled, err := enc.compiler.Compile(wit.U32{}, reflect.TypeOf(uint32(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	// Test LowerToStack with insufficient stack
	var val uint32 = 42
	emptyStack := make([]uint64, 0)
	_, err = enc.LowerToStack(compiled, unsafe.Pointer(&val), emptyStack, mem, alloc)
	if err == nil {
		t.Error("LowerToStack should fail with empty stack")
	}

	// Test LiftFromStack with insufficient stack
	var output uint32
	_, err = dec.LiftFromStack(compiled, emptyStack, unsafe.Pointer(&output), mem)
	if err == nil {
		t.Error("LiftFromStack should fail with empty stack")
	}
}

// Test tuple encoding/decoding via memory
func TestEncodeDecode_Tuple_Memory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Tuple of (u32, string)
	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U32{}, wit.String{}}},
	}

	input := []any{uint32(42), "hello"}

	flat, err := enc.EncodeParams([]wit.Type{tupleType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{tupleType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]any)
	if len(result) != 2 {
		t.Fatalf("length mismatch: got %d, want 2", len(result))
	}

	if result[0].(uint32) != 42 {
		t.Errorf("element 0: got %v, want 42", result[0])
	}
	if result[1].(string) != "hello" {
		t.Errorf("element 1: got %v, want hello", result[1])
	}
}

// Test enum encoding/decoding via memory
func TestEncodeDecode_Enum_Memory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Enum with 3 cases
	enumType := &wit.TypeDef{
		Kind: &wit.Enum{Cases: []wit.EnumCase{
			{Name: "red"},
			{Name: "green"},
			{Name: "blue"},
		}},
	}

	// Test each case using integer discriminants
	for i, name := range []string{"red", "green", "blue"} {
		flat, err := enc.EncodeParams([]wit.Type{enumType}, []any{uint32(i)}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed for %s: %v", name, err)
		}

		results, err := dec.DecodeResults([]wit.Type{enumType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed for %s: %v", name, err)
		}

		result := results[0].(uint32)
		if result != uint32(i) {
			t.Errorf("case %d: got %d, want %d", i, result, i)
		}
	}
}

// Test flags encoding/decoding via memory
func TestEncodeDecode_Flags_Memory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Flags with 3 flags
	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{Flags: []wit.Flag{
			{Name: "read"},
			{Name: "write"},
			{Name: "execute"},
		}},
	}

	// Flags are encoded as bits: read=bit0, write=bit1, execute=bit2
	// So read=true, write=false, execute=true = 0b101 = 5
	input := uint32(0b101)

	flat, err := enc.EncodeParams([]wit.Type{flagsType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{flagsType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].(uint64)
	if result != 0b101 {
		t.Errorf("got flags %b, want %b", result, 0b101)
	}
}

// Test result encoding/decoding via memory
func TestEncodeDecode_Result_Memory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Result<u32, string>
	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	// Test Ok case - format is {"ok": value}
	okInput := map[string]any{"ok": uint32(42)}
	flat, err := enc.EncodeParams([]wit.Type{resultType}, []any{okInput}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams (ok) failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults (ok) failed: %v", err)
	}

	result := results[0].(map[string]any)
	okVal, hasOk := result["ok"]
	if !hasOk {
		t.Fatal("expected ok key in result")
	}
	if okVal.(uint32) != 42 {
		t.Errorf("ok value: got %v, want 42", okVal)
	}

	// Test Err case - format is {"err": value}
	errInput := map[string]any{"err": "error message"}
	flat, err = enc.EncodeParams([]wit.Type{resultType}, []any{errInput}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams (err) failed: %v", err)
	}

	results, err = dec.DecodeResults([]wit.Type{resultType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults (err) failed: %v", err)
	}

	result = results[0].(map[string]any)
	errVal, hasErr := result["err"]
	if !hasErr {
		t.Fatal("expected err key in result")
	}
	if errVal.(string) != "error message" {
		t.Errorf("err value: got %v, want 'error message'", errVal)
	}
}

// Test variant encoding/decoding via memory
func TestEncodeDecode_Variant_Memory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Variant with different cases
	variantType := &wit.TypeDef{
		Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "none"},
			{Name: "some-int", Type: wit.U32{}},
			{Name: "some-str", Type: wit.String{}},
		}},
	}

	// Test none case (unit variant) - format is {"none": nil}
	noneInput := map[string]any{"none": nil}
	flat, err := enc.EncodeParams([]wit.Type{variantType}, []any{noneInput}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams (none) failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{variantType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults (none) failed: %v", err)
	}

	result := results[0].(map[string]any)
	_, hasNone := result["none"]
	if !hasNone {
		t.Error("expected none key in result")
	}

	// Test some-int case - format is {"some-int": value}
	intInput := map[string]any{"some-int": uint32(123)}
	flat, err = enc.EncodeParams([]wit.Type{variantType}, []any{intInput}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams (some-int) failed: %v", err)
	}

	results, err = dec.DecodeResults([]wit.Type{variantType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults (some-int) failed: %v", err)
	}

	result = results[0].(map[string]any)
	intVal, hasInt := result["some-int"]
	if !hasInt {
		t.Fatal("expected some-int key in result")
	}
	if intVal.(uint32) != 123 {
		t.Errorf("some-int value: got %v, want 123", intVal)
	}

	// Test some-str case - format is {"some-str": value}
	strInput := map[string]any{"some-str": "hello"}
	flat, err = enc.EncodeParams([]wit.Type{variantType}, []any{strInput}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams (some-str) failed: %v", err)
	}

	results, err = dec.DecodeResults([]wit.Type{variantType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults (some-str) failed: %v", err)
	}

	result = results[0].(map[string]any)
	strVal, hasStr := result["some-str"]
	if !hasStr {
		t.Fatal("expected some-str key in result")
	}
	if strVal.(string) != "hello" {
		t.Errorf("some-str value: got %v, want hello", strVal)
	}
}

// Test stack primitives encoding/decoding
func TestStack_Primitives(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	tests := []struct {
		witType wit.Type
		goType  reflect.Type
		value   any
		name    string
	}{
		{wit.Bool{}, reflect.TypeOf(true), true, "bool_true"},
		{wit.Bool{}, reflect.TypeOf(false), false, "bool_false"},
		{wit.U8{}, reflect.TypeOf(uint8(0)), uint8(255), "u8"},
		{wit.S8{}, reflect.TypeOf(int8(0)), int8(-128), "s8"},
		{wit.U16{}, reflect.TypeOf(uint16(0)), uint16(65535), "u16"},
		{wit.S16{}, reflect.TypeOf(int16(0)), int16(-32768), "s16"},
		{wit.U32{}, reflect.TypeOf(uint32(0)), uint32(0xFFFFFFFF), "u32"},
		{wit.S32{}, reflect.TypeOf(int32(0)), int32(-2147483648), "s32"},
		{wit.U64{}, reflect.TypeOf(uint64(0)), uint64(0xFFFFFFFFFFFFFFFF), "u64"},
		{wit.S64{}, reflect.TypeOf(int64(0)), int64(-9223372036854775808), "s64"},
		{wit.F32{}, reflect.TypeOf(float32(0)), float32(3.14), "f32"},
		{wit.F64{}, reflect.TypeOf(float64(0)), float64(2.718281828), "f64"},
		{wit.Char{}, reflect.TypeOf(rune(0)), rune('A'), "char"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := enc.compiler.Compile(tt.witType, tt.goType)
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			stack := make([]uint64, compiled.FlatCount)

			// Get pointer to value
			val := reflect.New(tt.goType)
			val.Elem().Set(reflect.ValueOf(tt.value))

			// Lower to stack
			_, err = enc.LowerToStack(compiled, unsafe.Pointer(val.Pointer()), stack, mem, alloc)
			if err != nil {
				t.Fatalf("LowerToStack failed: %v", err)
			}

			// Lift from stack
			output := reflect.New(tt.goType)
			_, err = dec.LiftFromStack(compiled, stack, unsafe.Pointer(output.Pointer()), mem)
			if err != nil {
				t.Fatalf("LiftFromStack failed: %v", err)
			}

			if !reflect.DeepEqual(output.Elem().Interface(), tt.value) {
				t.Errorf("got %v, want %v", output.Elem().Interface(), tt.value)
			}
		})
	}
}

// Test large flags (> 32 flags)
func TestEncodeDecode_LargeFlags(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Create 40 flags
	flags := make([]wit.Flag, 40)
	for i := range flags {
		flags[i] = wit.Flag{Name: "flag" + string(rune('a'+i))}
	}

	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{Flags: flags},
	}

	// Set every other flag (bits 0, 2, 4, etc.)
	var input uint64
	for i := range flags {
		if i%2 == 0 {
			input |= 1 << i
		}
	}

	flat, err := enc.EncodeParams([]wit.Type{flagsType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{flagsType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].(uint64)
	if result != input {
		t.Errorf("got %b, want %b", result, input)
	}
}

// Test large enum (> 256 cases)
func TestEncodeDecode_LargeEnum(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Create 300 cases (requires 2-byte discriminant)
	cases := make([]wit.EnumCase, 300)
	for i := range cases {
		cases[i] = wit.EnumCase{Name: "case" + string(rune('a'+i%26)) + string(rune('0'+i/26))}
	}

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{Cases: cases},
	}

	// Test case 0, 150, and 299 using integer discriminants
	for _, idx := range []int{0, 150, 299} {
		flat, err := enc.EncodeParams([]wit.Type{enumType}, []any{uint32(idx)}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed for index %d: %v", idx, err)
		}

		results, err := dec.DecodeResults([]wit.Type{enumType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed for index %d: %v", idx, err)
		}

		result := results[0].(uint32)
		if result != uint32(idx) {
			t.Errorf("case %d: got %d, want %d", idx, result, idx)
		}
	}
}

// Test LoadValue for primitive types
func TestLoadValue_Primitives(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	tests := []struct {
		witType wit.Type
		want    any
		setup   func()
		name    string
	}{
		{wit.Bool{}, true, func() { mem.WriteU8(0, 1) }, "bool_true"},
		{wit.Bool{}, false, func() { mem.WriteU8(0, 0) }, "bool_false"},
		{wit.U8{}, uint8(42), func() { mem.WriteU8(0, 42) }, "u8"},
		{wit.S8{}, int8(-1), func() { mem.WriteU8(0, 0xFF) }, "s8"},
		{wit.U16{}, uint16(0x1234), func() { mem.WriteU16(0, 0x1234) }, "u16"},
		{wit.S16{}, int16(-1), func() { mem.WriteU16(0, 0xFFFF) }, "s16"},
		{wit.U32{}, uint32(0x12345678), func() { mem.WriteU32(0, 0x12345678) }, "u32"},
		{wit.S32{}, int32(-1), func() { mem.WriteU32(0, 0xFFFFFFFF) }, "s32"},
		{wit.U64{}, uint64(0x123456789ABCDEF0), func() { mem.WriteU64(0, 0x123456789ABCDEF0) }, "u64"},
		{wit.S64{}, int64(-1), func() { mem.WriteU64(0, 0xFFFFFFFFFFFFFFFF) }, "s64"},
		{wit.F32{}, float32(3.1415927), func() { mem.WriteU32(0, 0x40490FDB) }, "f32"},
		{wit.F64{}, float64(3.141592653589793), func() { mem.WriteU64(0, 0x400921FB54442D18) }, "f64"},
		{wit.Char{}, rune(0x1F600), func() { mem.WriteU32(0, 0x1F600) }, "char"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			result, err := dec.LoadValue(tt.witType, 0, mem)
			if err != nil {
				t.Fatalf("LoadValue failed: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %v, want %v", result, tt.want)
			}
		})
	}
}

// Test LoadValue for list type
func TestLoadValue_List(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	// Write list header: dataAddr=100, len=3
	mem.WriteU32(0, 100)
	mem.WriteU32(4, 3)
	// Write list elements at address 100
	mem.WriteU32(100, 10)
	mem.WriteU32(104, 20)
	mem.WriteU32(108, 30)

	result, err := dec.LoadValue(listType, 0, mem)
	if err != nil {
		t.Fatalf("LoadValue failed: %v", err)
	}
	list := result.([]any)
	if len(list) != 3 {
		t.Fatalf("got %d elements, want 3", len(list))
	}
	for i, want := range []uint32{10, 20, 30} {
		if list[i] != want {
			t.Errorf("list[%d]: got %v, want %d", i, list[i], want)
		}
	}
}

// Test LoadValue for tuple type
func TestLoadValue_Tuple(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U32{}, wit.U8{}, wit.Bool{}}},
	}

	// Write tuple: u32 at 0, u8 at 4, bool at 5
	mem.WriteU32(0, 123)
	mem.WriteU8(4, 45)
	mem.WriteU8(5, 1)

	result, err := dec.LoadValue(tupleType, 0, mem)
	if err != nil {
		t.Fatalf("LoadValue failed: %v", err)
	}
	tuple := result.([]any)
	if len(tuple) != 3 {
		t.Fatalf("got %d elements, want 3", len(tuple))
	}
	if tuple[0] != uint32(123) || tuple[1] != uint8(45) || tuple[2] != true {
		t.Errorf("got %+v, want [123, 45, true]", tuple)
	}
}

// Test LoadValue for option type
func TestLoadValue_Option(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	t.Run("none", func(t *testing.T) {
		mem.WriteU8(0, 0)
		result, err := dec.LoadValue(optionType, 0, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		if result != nil {
			t.Errorf("got %v, want nil", result)
		}
	})

	t.Run("some", func(t *testing.T) {
		mem.WriteU8(16, 1)
		mem.WriteU32(20, 42)
		result, err := dec.LoadValue(optionType, 16, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		if result != uint32(42) {
			t.Errorf("got %v, want 42", result)
		}
	})
}

// Test LoadValue for result type with memory paths
func TestLoadValue_ResultMemory(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	t.Run("ok", func(t *testing.T) {
		mem.WriteU8(0, 0)
		mem.WriteU32(4, 100)
		result, err := dec.LoadValue(resultType, 0, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["ok"] != uint32(100) {
			t.Errorf("ok: got %v, want 100", m["ok"])
		}
	})

	t.Run("err", func(t *testing.T) {
		mem.WriteU8(32, 1)
		// Write string: ptr=200, len=5
		mem.WriteU32(36, 200)
		mem.WriteU32(40, 5)
		copy(mem.data[200:], []byte("error"))
		result, err := dec.LoadValue(resultType, 32, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["err"] != "error" {
			t.Errorf("err: got %v, want 'error'", m["err"])
		}
	})
}

// Test LoadValue for variant type with memory paths
func TestLoadValue_VariantMemory(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "none"},
			{Name: "int-val", Type: wit.U32{}},
			{Name: "str-val", Type: wit.String{}},
		}},
	}

	t.Run("unit_case", func(t *testing.T) {
		mem.WriteU8(0, 0)
		result, err := dec.LoadValue(variantType, 0, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if _, ok := m["none"]; !ok {
			t.Errorf("expected 'none' key in result, got %v", m)
		}
	})

	t.Run("int_case", func(t *testing.T) {
		mem.WriteU8(16, 1)
		mem.WriteU32(20, 789)
		result, err := dec.LoadValue(variantType, 16, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["int-val"] != uint32(789) {
			t.Errorf("int-val: got %v, want 789", m["int-val"])
		}
	})
}

// Test LoadValue for record type
func TestLoadValue_Record(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	recordType := &wit.TypeDef{
		Kind: &wit.Record{Fields: []wit.Field{
			{Name: "id", Type: wit.U32{}},
			{Name: "count", Type: wit.U16{}},
			{Name: "active", Type: wit.Bool{}},
		}},
	}

	// Write record: u32 at 0, u16 at 4, bool at 6
	mem.WriteU32(0, 12345)
	mem.WriteU16(4, 678)
	mem.WriteU8(6, 1)

	result, err := dec.LoadValue(recordType, 0, mem)
	if err != nil {
		t.Fatalf("LoadValue failed: %v", err)
	}
	rec := result.(map[string]any)
	if rec["id"] != uint32(12345) {
		t.Errorf("id: got %v, want 12345", rec["id"])
	}
	if rec["count"] != uint16(678) {
		t.Errorf("count: got %v, want 678", rec["count"])
	}
	if rec["active"] != true {
		t.Errorf("active: got %v, want true", rec["active"])
	}
}

// Test Result with mismatched ok/err alignments
// This tests the bug where payload offset was calculated incorrectly
// when ok and err have different alignments.
// Result<u32, u64> has ok_align=4, err_align=8
// Payload should always be at offset 8 (max align), not 4 for ok case
func TestLoadValue_Result_MismatchedAlignment(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	// Result<u32, u64> - ok has align 4, err has align 8
	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.U64{},
		},
	}

	// Test ok case - payload should be at offset 8 (max of 4, 8)
	// Layout: [disc(1 byte), padding(7 bytes), u32 payload]
	t.Run("ok_aligned", func(t *testing.T) {
		mem.WriteU8(0, 0)           // discriminant = 0 (ok)
		mem.WriteU32(8, 0x12345678) // payload at offset 8, not 4!
		result, err := dec.LoadValue(resultType, 0, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["ok"] != uint32(0x12345678) {
			t.Errorf("ok value: got %v, want 0x12345678", m["ok"])
		}
	})

	// Test err case - payload should be at offset 8
	t.Run("err_aligned", func(t *testing.T) {
		mem.WriteU8(32, 1)                   // discriminant = 1 (err)
		mem.WriteU64(40, 0xDEADBEEFCAFEBABE) // payload at offset 8
		result, err := dec.LoadValue(resultType, 32, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["err"] != uint64(0xDEADBEEFCAFEBABE) {
			t.Errorf("err value: got %v, want 0xDEADBEEFCAFEBABE", m["err"])
		}
	})
}

// Test encode/decode round-trip for Result with mismatched alignments
func TestEncodeDecode_Result_MismatchedAlignment(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	// Result<u32, u64>
	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.U64{},
		},
	}

	t.Run("ok_roundtrip", func(t *testing.T) {
		input := map[string]any{"ok": uint32(42)}
		flat, err := enc.EncodeParams([]wit.Type{resultType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		m := results[0].(map[string]any)
		if m["ok"] != uint32(42) {
			t.Errorf("ok: got %v, want 42", m["ok"])
		}
	})

	t.Run("err_roundtrip", func(t *testing.T) {
		input := map[string]any{"err": uint64(0xDEADBEEF)}
		flat, err := enc.EncodeParams([]wit.Type{resultType}, []any{input}, mem, alloc, allocList)
		if err != nil {
			t.Fatalf("EncodeParams failed: %v", err)
		}

		results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
		if err != nil {
			t.Fatalf("DecodeResults failed: %v", err)
		}

		m := results[0].(map[string]any)
		if m["err"] != uint64(0xDEADBEEF) {
			t.Errorf("err: got %v, want 0xDEADBEEF", m["err"])
		}
	})
}

// Test Variant with mismatched case alignments
func TestLoadValue_Variant_MismatchedAlignment(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(1024)

	// Variant with cases of different alignments
	variantType := &wit.TypeDef{
		Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "small", Type: wit.U8{}},   // align 1
			{Name: "medium", Type: wit.U32{}}, // align 4
			{Name: "large", Type: wit.U64{}},  // align 8
		}},
	}

	// All payloads should be at offset 8 (max align)
	t.Run("small_case", func(t *testing.T) {
		mem.WriteU8(0, 0)  // discriminant = 0 (small)
		mem.WriteU8(8, 42) // payload at offset 8
		result, err := dec.LoadValue(variantType, 0, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["small"] != uint8(42) {
			t.Errorf("small: got %v, want 42", m["small"])
		}
	})

	t.Run("medium_case", func(t *testing.T) {
		mem.WriteU8(16, 1)           // discriminant = 1 (medium)
		mem.WriteU32(24, 0x12345678) // payload at offset 8
		result, err := dec.LoadValue(variantType, 16, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["medium"] != uint32(0x12345678) {
			t.Errorf("medium: got %v, want 0x12345678", m["medium"])
		}
	})

	t.Run("large_case", func(t *testing.T) {
		mem.WriteU8(32, 2)                   // discriminant = 2 (large)
		mem.WriteU64(40, 0xDEADBEEFCAFEBABE) // payload at offset 8
		result, err := dec.LoadValue(variantType, 32, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}
		m := result.(map[string]any)
		if m["large"] != uint64(0xDEADBEEFCAFEBABE) {
			t.Errorf("large: got %v, want 0xDEADBEEFCAFEBABE", m["large"])
		}
	})
}

// Test list<s32> stack encoding (covers lowerListToStack Int32 path)
func TestEncoder_ListS32Stack(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.S32{}},
	}
	compiled, err := compiler.Compile(listType, reflect.TypeOf([]int32{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	data := []int32{-1, 0, 1, 2147483647, -2147483648}
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&data), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 stack values, got %d", n)
	}
	if stack[1] != 5 {
		t.Errorf("expected length 5, got %d", stack[1])
	}

	var result []int32
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&result), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}
	if len(result) != len(data) {
		t.Errorf("length mismatch: got %d, want %d", len(result), len(data))
	}
	for i := range data {
		if result[i] != data[i] {
			t.Errorf("element %d: got %d, want %d", i, result[i], data[i])
		}
	}
}

// Test list<s64> stack encoding (covers lowerListToStack Int64 path)
func TestEncoder_ListS64Stack(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.S64{}},
	}
	compiled, err := compiler.Compile(listType, reflect.TypeOf([]int64{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	data := []int64{-1, 0, 1, 9223372036854775807, -9223372036854775808}
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&data), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 stack values, got %d", n)
	}

	var result []int64
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&result), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}
	for i := range data {
		if result[i] != data[i] {
			t.Errorf("element %d: got %d, want %d", i, result[i], data[i])
		}
	}
}

// Test list<f32> stack encoding (covers lowerListToStack Float32 path)
func TestEncoder_ListF32Stack(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.F32{}},
	}
	compiled, err := compiler.Compile(listType, reflect.TypeOf([]float32{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	data := []float32{-1.5, 0.0, 1.5, 3.14159, -2.71828}
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&data), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 stack values, got %d", n)
	}

	var result []float32
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&result), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}
	for i := range data {
		if result[i] != data[i] {
			t.Errorf("element %d: got %f, want %f", i, result[i], data[i])
		}
	}
}

// Test list<f64> stack encoding (covers lowerListToStack Float64 path)
func TestEncoder_ListF64Stack(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.F64{}},
	}
	compiled, err := compiler.Compile(listType, reflect.TypeOf([]float64{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	data := []float64{-1.5, 0.0, 1.5, 3.141592653589793, -2.718281828459045}
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&data), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 stack values, got %d", n)
	}

	var result []float64
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&result), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}
	for i := range data {
		if result[i] != data[i] {
			t.Errorf("element %d: got %f, want %f", i, result[i], data[i])
		}
	}
}

// Test list<string> stack encoding (covers lowerListToStack String path)
func TestEncoder_ListStringStack(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(8192)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.String{}},
	}
	compiled, err := compiler.Compile(listType, reflect.TypeOf([]string{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	data := []string{"hello", "", "world", "test"}
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&data), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 stack values, got %d", n)
	}

	var result []string
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&result), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}
	if len(result) != len(data) {
		t.Errorf("length mismatch: got %d, want %d", len(result), len(data))
	}
	for i := range data {
		if result[i] != data[i] {
			t.Errorf("element %d: got %q, want %q", i, result[i], data[i])
		}
	}
}

// Test empty list stack encoding
func TestEncoder_EmptyListStack(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(1024)
	alloc := newMockAllocator(mem)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}
	compiled, err := compiler.Compile(listType, reflect.TypeOf([]uint32{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	var data []uint32
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&data), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 stack values, got %d", n)
	}
	if stack[0] != 0 || stack[1] != 0 {
		t.Errorf("expected (0, 0), got (%d, %d)", stack[0], stack[1])
	}

	var result []uint32
	_, err = dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&result), mem)
	if err != nil {
		t.Fatalf("LiftFromStack failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got length %d", len(result))
	}
}

// Test result flat padding - critical spec compliance test
// When result<list<u8>, u8> is flattened, both branches must use same slot count
// list = 2 slots (ptr, len), u8 = 1 slot, so err case must be padded to 2 slots
func TestResult_FlatPadding(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	// result<list<u8>, u8> - ok=2 slots, err=1 slot (must pad to 2)
	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}},
			Err: wit.U8{},
		},
	}

	type testResult struct {
		Ok  *[]uint8
		Err *uint8
	}

	compiled, err := compiler.Compile(resultType, reflect.TypeOf(testResult{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Test 1: Encode err case, verify flat count
	errVal := uint8(42)
	inputErr := testResult{Err: &errVal}
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&inputErr), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack err case failed: %v", err)
	}
	// Should be 1 (discriminant) + 2 (max payload = list slots) = 3
	if n != 3 {
		t.Errorf("expected 3 flat values for err case (1 disc + 2 padded payload), got %d", n)
	}

	// Test 2: Decode and verify consumed matches encoded
	var outputErr testResult
	consumed, err := dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&outputErr), mem)
	if err != nil {
		t.Fatalf("LiftFromStack err case failed: %v", err)
	}
	if consumed != n {
		t.Errorf("decoder consumed %d flat values but encoder produced %d", consumed, n)
	}
	if outputErr.Err == nil || *outputErr.Err != 42 {
		t.Errorf("err value mismatch: got %v", outputErr.Err)
	}

	// Test 3: Multiple results in sequence - this is where padding bugs show
	okData := []uint8{1, 2, 3}
	inputOk := testResult{Ok: &okData}
	n1, err := enc.LowerToStack(compiled, unsafe.Pointer(&inputOk), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack ok case failed: %v", err)
	}
	n2, err := enc.LowerToStack(compiled, unsafe.Pointer(&inputErr), stack[n1:], mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack second err case failed: %v", err)
	}

	// Decode both in sequence
	var out1, out2 testResult
	c1, err := dec.LiftFromStack(compiled, stack[:n1], unsafe.Pointer(&out1), mem)
	if err != nil {
		t.Fatalf("LiftFromStack first failed: %v", err)
	}
	c2, err := dec.LiftFromStack(compiled, stack[c1:c1+n2], unsafe.Pointer(&out2), mem)
	if err != nil {
		t.Fatalf("LiftFromStack second failed: %v", err)
	}

	if c1 != n1 {
		t.Errorf("first decode consumed %d, expected %d", c1, n1)
	}
	if c2 != n2 {
		t.Errorf("second decode consumed %d, expected %d", c2, n2)
	}
}

// Test variant flat padding - same issue as result
func TestVariant_FlatPadding(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	// variant { small: u8, large: u64 } - small=1 slot, large=1 slot (u64 is 1 slot)
	// Better test: variant { small: u8, large: string } - small=1 slot, large=2 slots
	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "small", Type: wit.U8{}},
				{Name: "large", Type: wit.String{}},
			},
		},
	}

	type testVariant struct {
		Small *uint8
		Large *string
	}

	compiled, err := compiler.Compile(variantType, reflect.TypeOf(testVariant{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Encode small case
	smallVal := uint8(99)
	inputSmall := testVariant{Small: &smallVal}
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&inputSmall), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack small case failed: %v", err)
	}
	// Should be 1 (discriminant) + 2 (max payload = string slots) = 3
	if n != 3 {
		t.Errorf("expected 3 flat values for small case (1 disc + 2 padded payload), got %d", n)
	}

	// Decode and verify consumed matches
	var output testVariant
	consumed, err := dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("LiftFromStack small case failed: %v", err)
	}
	if consumed != n {
		t.Errorf("decoder consumed %d flat values but encoder produced %d", consumed, n)
	}
}

// Test result flat padding using raw WIT type path (DecodeResults API)
// This tests the decoder.go:liftResult path, not stack.go
func TestResult_FlatPadding_RawPath(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	// result<list<u8>, u8> - ok=2 slots, err=1 slot
	listType := &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}
	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  listType,
			Err: wit.U8{},
		},
	}

	// Encode err case using flattenValue
	errValue := map[string]any{"err": uint8(42)}
	flat := make([]uint64, 0, 16)
	err := enc.flattenValue(resultType, errValue, mem, alloc, nil, &flat, nil)
	if err != nil {
		t.Fatalf("flattenValue failed: %v", err)
	}

	// Encoder should produce 3 values: 1 disc + 2 padded (max of list=2, u8=1)
	if len(flat) != 3 {
		t.Errorf("encoder produced %d flat values, expected 3", len(flat))
	}

	// Now decode using DecodeResults (goes through liftResult in decoder.go)
	results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	// Verify correct decoding
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	resultMap, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result is not map[string]any: %T", results[0])
	}
	if resultMap["err"] != uint8(42) {
		t.Errorf("err value mismatch: got %v", resultMap["err"])
	}

	// Test THREE sequential results - bug shows on third when padding is wrong
	// If decoder returns 2 for err case instead of 3, the third value reads wrong offset
	errValue2 := map[string]any{"err": uint8(42)}
	okValue2 := map[string]any{"ok": []uint8{7, 8, 9}}

	flat3 := make([]uint64, 0, 24)
	// err (expects 3 slots: disc + 1 payload + 1 padding)
	err = enc.flattenValue(resultType, errValue2, mem, alloc, nil, &flat3, nil)
	if err != nil {
		t.Fatalf("flattenValue err failed: %v", err)
	}
	t.Logf("After err: flat3 = %v", flat3)

	// ok (expects 3 slots: disc + 2 payload)
	err = enc.flattenValue(resultType, okValue2, mem, alloc, nil, &flat3, nil)
	if err != nil {
		t.Fatalf("flattenValue ok failed: %v", err)
	}
	t.Logf("After ok: flat3 = %v", flat3)

	// another err (expects 3 slots)
	errValue3 := map[string]any{"err": uint8(99)}
	err = enc.flattenValue(resultType, errValue3, mem, alloc, nil, &flat3, nil)
	if err != nil {
		t.Fatalf("flattenValue err3 failed: %v", err)
	}
	t.Logf("After err3: flat3 = %v", flat3)

	// Should be 9 values: 3 + 3 + 3
	if len(flat3) != 9 {
		t.Errorf("encoder produced %d flat values for three results, expected 9", len(flat3))
	}

	// Decode all three
	results3, err := dec.DecodeResults([]wit.Type{resultType, resultType, resultType}, flat3, mem)
	if err != nil {
		t.Fatalf("DecodeResults three results failed: %v", err)
	}
	if len(results3) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results3))
	}

	// Verify all three decoded results
	r1, ok := results3[0].(map[string]any)
	if !ok {
		t.Fatalf("result[0] is not map: %T", results3[0])
	}
	if r1["err"] != uint8(42) {
		t.Errorf("result[0] should be err=42, got: %v", r1)
	}

	r2, ok := results3[1].(map[string]any)
	if !ok {
		t.Fatalf("result[1] is not map: %T", results3[1])
	}
	if r2["ok"] == nil {
		t.Errorf("result[1] should be ok with list, got: %v", r2)
	}

	r3, ok := results3[2].(map[string]any)
	if !ok {
		t.Fatalf("result[2] is not map: %T", results3[2])
	}
	// THIS IS THE CRITICAL CHECK - if decoder returned 2 for first err instead of 3,
	// the third result would be read from wrong offset
	if r3["err"] != uint8(99) {
		t.Errorf("result[2] should be err=99, got: %v (this indicates padding bug!)", r3)
	}
}

// Test option flat padding
func TestOption_FlatPadding(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	// option<string> - none=0 payload slots, some=2 slots
	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.String{}},
	}

	compiled, err := compiler.Compile(optionType, reflect.TypeOf((*string)(nil)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Encode none case
	var inputNone *string
	stack := make([]uint64, 16)
	n, err := enc.LowerToStack(compiled, unsafe.Pointer(&inputNone), stack, mem, alloc)
	if err != nil {
		t.Fatalf("LowerToStack none case failed: %v", err)
	}
	// Should be 1 (discriminant) + 2 (max payload = string slots) = 3
	if n != 3 {
		t.Errorf("expected 3 flat values for none case (1 disc + 2 padded payload), got %d", n)
	}

	// Decode and verify consumed matches
	var output *string
	consumed, err := dec.LiftFromStack(compiled, stack[:n], unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("LiftFromStack none case failed: %v", err)
	}
	if consumed != n {
		t.Errorf("decoder consumed %d flat values but encoder produced %d", consumed, n)
	}
}

// TestOwn_Compile tests own<T> type compilation
func TestOwn_Compile(t *testing.T) {
	c := NewCompiler()

	ownType := &wit.TypeDef{
		Kind: &wit.Own{Type: nil}, // own<resource>
	}

	// Test with uint32 (handle representation)
	compiled, err := c.Compile(ownType, reflect.TypeOf(uint32(0)))
	if err != nil {
		t.Fatalf("Compile own<T> with uint32 failed: %v", err)
	}

	if compiled.Kind != KindOwn {
		t.Errorf("expected KindOwn, got %v", compiled.Kind)
	}
	if compiled.FlatCount != 1 {
		t.Errorf("expected FlatCount 1, got %d", compiled.FlatCount)
	}
	if compiled.WitSize != 4 {
		t.Errorf("expected WitSize 4, got %d", compiled.WitSize)
	}
}

// TestBorrow_Compile tests borrow<T> type compilation
func TestBorrow_Compile(t *testing.T) {
	c := NewCompiler()

	borrowType := &wit.TypeDef{
		Kind: &wit.Borrow{Type: nil}, // borrow<resource>
	}

	// Test with uint32 (handle representation)
	compiled, err := c.Compile(borrowType, reflect.TypeOf(uint32(0)))
	if err != nil {
		t.Fatalf("Compile borrow<T> with uint32 failed: %v", err)
	}

	if compiled.Kind != KindBorrow {
		t.Errorf("expected KindBorrow, got %v", compiled.Kind)
	}
	if compiled.FlatCount != 1 {
		t.Errorf("expected FlatCount 1, got %d", compiled.FlatCount)
	}
	if compiled.WitSize != 4 {
		t.Errorf("expected WitSize 4, got %d", compiled.WitSize)
	}
}

// TestAllocationList_Free tests allocation cleanup
func TestAllocationList_Free(t *testing.T) {
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	list := NewAllocationList()

	// Add some allocations
	ptr1, _ := alloc.Alloc(100, 4)
	list.Add(ptr1, 100, 4)

	ptr2, _ := alloc.Alloc(200, 8)
	list.Add(ptr2, 200, 8)

	if list.Count() != 2 {
		t.Errorf("expected 2 allocations, got %d", list.Count())
	}

	// Free all allocations
	list.Free(alloc)

	// Verify Free with nil allocator doesn't panic
	list.Free(nil)
}

// TestEncoder_EnumToMemory tests enum encoding to memory
func TestEncoder_EnumToMemory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "red"},
				{Name: "green"},
				{Name: "blue"},
			},
		},
	}

	// Compile for memory operations
	type Color uint8
	compiled, err := enc.compiler.Compile(enumType, reflect.TypeOf(Color(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Encode enum value to memory
	var val Color = 1 // green
	err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&val), mem, nil, nil, nil)
	if err != nil {
		t.Fatalf("encodeFieldToMemory failed: %v", err)
	}

	// Verify the discriminant was written
	disc, _ := mem.ReadU8(0)
	if disc != 1 {
		t.Errorf("expected discriminant 1, got %d", disc)
	}

	// Decode and verify
	var decoded Color
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&decoded), mem, nil)
	if err != nil {
		t.Fatalf("decodeFieldFromMemory failed: %v", err)
	}
	if decoded != val {
		t.Errorf("expected %d, got %d", val, decoded)
	}
}

// TestEncoder_FlagsToMemory tests flags encoding to memory
func TestEncoder_FlagsToMemory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)

	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: []wit.Flag{
				{Name: "read"},
				{Name: "write"},
				{Name: "execute"},
			},
		},
	}

	// Compile for memory operations
	type Permissions uint8
	compiled, err := enc.compiler.Compile(flagsType, reflect.TypeOf(Permissions(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Encode flags value to memory (read + execute = 0b101 = 5)
	var val Permissions = 5
	err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&val), mem, nil, nil, nil)
	if err != nil {
		t.Fatalf("encodeFieldToMemory failed: %v", err)
	}

	// Verify the flags were written
	bits, _ := mem.ReadU8(0)
	if bits != 5 {
		t.Errorf("expected flags 5, got %d", bits)
	}

	// Decode and verify
	var decoded Permissions
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&decoded), mem, nil)
	if err != nil {
		t.Fatalf("decodeFieldFromMemory failed: %v", err)
	}
	if decoded != val {
		t.Errorf("expected %d, got %d", val, decoded)
	}
}

// TestEncoder_TupleToMemory tests tuple encoding to memory
func TestEncoder_TupleToMemory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)

	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U8{}},
		},
	}

	type Pair struct {
		A uint32
		B uint8
	}

	compiled, err := enc.compiler.Compile(tupleType, reflect.TypeOf(Pair{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	input := Pair{A: 12345, B: 42}
	err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&input), mem, nil, nil, nil)
	if err != nil {
		t.Fatalf("encodeFieldToMemory failed: %v", err)
	}

	// Decode and verify
	var output Pair
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&output), mem, nil)
	if err != nil {
		t.Fatalf("decodeFieldFromMemory failed: %v", err)
	}
	if output != input {
		t.Errorf("expected %+v, got %+v", input, output)
	}
}

// TestEncoder_VariantToMemory tests variant encoding to memory
func TestEncoder_VariantToMemory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "none", Type: nil},
				{Name: "some", Type: wit.U32{}},
			},
		},
	}

	type Optional struct {
		None *struct{}
		Some *uint32
	}

	compiled, err := enc.compiler.Compile(variantType, reflect.TypeOf(Optional{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Test some case
	val := uint32(42)
	input := Optional{Some: &val}
	err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&input), mem, nil, nil, nil)
	if err != nil {
		t.Fatalf("encodeFieldToMemory failed: %v", err)
	}

	// Decode and verify
	var output Optional
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&output), mem, nil)
	if err != nil {
		t.Fatalf("decodeFieldFromMemory failed: %v", err)
	}
	if output.Some == nil || *output.Some != val {
		t.Errorf("expected Some(42), got %+v", output)
	}
}

// TestEncoder_ResultToMemory tests result encoding to memory
func TestEncoder_ResultToMemory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.U8{},
		},
	}

	type Result struct {
		Ok  *uint32
		Err *uint8
	}

	compiled, err := enc.compiler.Compile(resultType, reflect.TypeOf(Result{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Test ok case
	val := uint32(100)
	input := Result{Ok: &val}
	err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&input), mem, nil, nil, nil)
	if err != nil {
		t.Fatalf("encodeFieldToMemory failed: %v", err)
	}

	// Decode and verify
	var output Result
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&output), mem, nil)
	if err != nil {
		t.Fatalf("decodeFieldFromMemory failed: %v", err)
	}
	if output.Ok == nil || *output.Ok != val {
		t.Errorf("expected Ok(100), got %+v", output)
	}
}

// TestEncoder_StoreTypeDef_Record tests dynamic record encoding
func TestEncoder_StoreTypeDef_Record(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "id", Type: wit.U32{}},
				{Name: "count", Type: wit.U8{}},
			},
		},
	}

	value := map[string]any{
		"id":    uint32(42),
		"count": uint8(5),
	}

	err := enc.storeTypeDef(recordType, value, 0, mem, alloc, nil, nil)
	if err != nil {
		t.Fatalf("storeTypeDef failed: %v", err)
	}

	// Verify values in memory
	id, _ := mem.ReadU32(0)
	if id != 42 {
		t.Errorf("expected id=42, got %d", id)
	}
	count, _ := mem.ReadU8(4)
	if count != 5 {
		t.Errorf("expected count=5, got %d", count)
	}
}

// TestEncoder_StoreTypeDef_List tests dynamic list encoding
func TestEncoder_StoreTypeDef_List(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	value := []any{uint32(1), uint32(2), uint32(3)}

	err := enc.storeTypeDef(listType, value, 0, mem, alloc, allocList, nil)
	if err != nil {
		t.Fatalf("storeTypeDef failed: %v", err)
	}

	// Verify list pointer and length
	dataAddr, _ := mem.ReadU32(0)
	length, _ := mem.ReadU32(4)
	if length != 3 {
		t.Errorf("expected length=3, got %d", length)
	}

	// Verify data elements
	for i := uint32(0); i < length; i++ {
		val, _ := mem.ReadU32(dataAddr + i*4)
		expected := i + 1
		if val != expected {
			t.Errorf("element %d: expected %d, got %d", i, expected, val)
		}
	}
}

// TestEncoder_StoreTypeDef_Tuple tests dynamic tuple encoding
func TestEncoder_StoreTypeDef_Tuple(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{wit.U32{}, wit.U8{}},
		},
	}

	value := []any{uint32(100), uint8(10)}

	err := enc.storeTypeDef(tupleType, value, 0, mem, alloc, nil, nil)
	if err != nil {
		t.Fatalf("storeTypeDef failed: %v", err)
	}

	// Verify values in memory
	v1, _ := mem.ReadU32(0)
	if v1 != 100 {
		t.Errorf("expected element 0 = 100, got %d", v1)
	}
	v2, _ := mem.ReadU8(4)
	if v2 != 10 {
		t.Errorf("expected element 1 = 10, got %d", v2)
	}
}

// TestDecoder_ListFromMemory_Primitives tests list decoding for primitive types
func TestDecoder_ListFromMemory_Primitives(t *testing.T) {
	enc := NewEncoder()

	tests := []struct {
		name     string
		elemType wit.Type
		input    []any
	}{
		{"list<u32>", wit.U32{}, []any{uint32(1), uint32(2), uint32(3)}},
		{"list<u64>", wit.U64{}, []any{uint64(100), uint64(200)}},
		{"list<s32>", wit.S32{}, []any{int32(-1), int32(0), int32(1)}},
		{"list<f32>", wit.F32{}, []any{float32(1.5), float32(2.5)}},
		{"list<f64>", wit.F64{}, []any{float64(1.5), float64(2.5)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMockMemory(4096)
			alloc := newMockAllocator(mem)
			listType := &wit.TypeDef{Kind: &wit.List{Type: tt.elemType}}

			// Encode list to memory using storeTypeDef
			err := enc.storeTypeDef(listType, tt.input, 0, mem, alloc, nil, nil)
			if err != nil {
				t.Fatalf("storeTypeDef failed: %v", err)
			}

			// Read list pointer and length
			dataAddr, _ := mem.ReadU32(0)
			length, _ := mem.ReadU32(4)

			if int(length) != len(tt.input) {
				t.Errorf("length mismatch: got %d, want %d", length, len(tt.input))
			}

			// Verify the dataAddr is valid
			if dataAddr == 0 && length > 0 {
				t.Errorf("dataAddr should not be 0 for non-empty list")
			}
		})
	}
}

// TestDecoder_EmptyList tests empty list handling
func TestDecoder_EmptyList(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	// Set up empty list (addr=0, len=0)
	mem.WriteU32(0, 0)
	mem.WriteU32(4, 0)

	listType := &wit.TypeDef{Kind: &wit.List{Type: wit.U32{}}}

	compiled, err := dec.compiler.Compile(listType, reflect.TypeOf([]uint32{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	var items []uint32
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&items), mem, nil)
	if err != nil {
		t.Fatalf("decodeFieldFromMemory failed: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("expected empty list, got %d elements", len(items))
	}
}

// TestEncoder_FlagsToMemory_MultipleWidths tests flags with different widths
func TestEncoder_FlagsToMemory_MultipleWidths(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()

	tests := []struct {
		goType   reflect.Type
		value    any
		name     string
		numFlags int
	}{
		{reflect.TypeOf(uint8(0)), uint8(0xFF), "8 flags (u8)", 8},
		{reflect.TypeOf(uint16(0)), uint16(0xFFFF), "16 flags (u16)", 16},
		{reflect.TypeOf(uint32(0)), uint32(0xFFFFFFFF), "32 flags (u32)", 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMockMemory(4096)
			flags := make([]wit.Flag, tt.numFlags)
			for i := 0; i < tt.numFlags; i++ {
				flags[i] = wit.Flag{Name: "flag" + string(rune('a'+i))}
			}
			flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}

			compiled, err := enc.compiler.Compile(flagsType, tt.goType)
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}

			// Use reflect to get pointer to value
			val := reflect.New(tt.goType)
			val.Elem().Set(reflect.ValueOf(tt.value))

			err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(val.Pointer()), mem, nil, nil, nil)
			if err != nil {
				t.Fatalf("encodeFieldToMemory failed: %v", err)
			}

			// Decode and verify
			out := reflect.New(tt.goType)
			err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(out.Pointer()), mem, nil)
			if err != nil {
				t.Fatalf("decodeFieldFromMemory failed: %v", err)
			}

			if out.Elem().Interface() != tt.value {
				t.Errorf("expected %v, got %v", tt.value, out.Elem().Interface())
			}
		})
	}
}

// TestEncoder_LargeEnum tests enum with more than 256 cases (u16 discriminant)
func TestEncoder_LargeEnum(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)

	// Create enum with 300 cases (requires u16 discriminant)
	cases := make([]wit.EnumCase, 300)
	for i := 0; i < 300; i++ {
		cases[i] = wit.EnumCase{Name: "case" + string(rune('a'+i%26))}
	}
	enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}

	type BigEnum uint16
	compiled, err := enc.compiler.Compile(enumType, reflect.TypeOf(BigEnum(0)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	val := BigEnum(299) // Last case
	err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&val), mem, nil, nil, nil)
	if err != nil {
		t.Fatalf("encodeFieldToMemory failed: %v", err)
	}

	// Verify discriminant
	disc, _ := mem.ReadU16(0)
	if disc != 299 {
		t.Errorf("expected discriminant 299, got %d", disc)
	}

	// Decode and verify
	var decoded BigEnum
	err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&decoded), mem, nil)
	if err != nil {
		t.Fatalf("decodeFieldFromMemory failed: %v", err)
	}
	if decoded != val {
		t.Errorf("expected %d, got %d", val, decoded)
	}
}

// TestEncoder_OptionToMemory tests option encoding to memory
func TestEncoder_OptionToMemory(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()

	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	compiled, err := enc.compiler.Compile(optionType, reflect.TypeOf((*uint32)(nil)))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Test none case
	t.Run("none", func(t *testing.T) {
		mem := newMockMemory(4096)
		var none *uint32
		err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&none), mem, nil, nil, nil)
		if err != nil {
			t.Fatalf("encodeFieldToMemory failed: %v", err)
		}

		disc, _ := mem.ReadU8(0)
		if disc != 0 {
			t.Errorf("expected discriminant 0, got %d", disc)
		}

		var decoded *uint32
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&decoded), mem, nil)
		if err != nil {
			t.Fatalf("decodeFieldFromMemory failed: %v", err)
		}
		if decoded != nil {
			t.Errorf("expected nil, got %v", *decoded)
		}
	})

	// Test some case
	t.Run("some", func(t *testing.T) {
		mem := newMockMemory(4096)
		val := uint32(42)
		some := &val
		err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&some), mem, nil, nil, nil)
		if err != nil {
			t.Fatalf("encodeFieldToMemory failed: %v", err)
		}

		disc, _ := mem.ReadU8(0)
		if disc != 1 {
			t.Errorf("expected discriminant 1, got %d", disc)
		}

		var decoded *uint32
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&decoded), mem, nil)
		if err != nil {
			t.Fatalf("decodeFieldFromMemory failed: %v", err)
		}
		if decoded == nil || *decoded != 42 {
			t.Errorf("expected 42, got %v", decoded)
		}
	})
}

// TestEncoder_ResultToMemory_BothCases tests result encoding with both ok and err cases
func TestEncoder_ResultToMemory_BothCases(t *testing.T) {
	enc := NewEncoder()

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	type MyResult struct {
		Ok  *uint32
		Err *string
	}

	compiled, err := enc.compiler.Compile(resultType, reflect.TypeOf(MyResult{}))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Test ok case
	t.Run("ok", func(t *testing.T) {
		mem := newMockMemory(4096)
		val := uint32(100)
		input := MyResult{Ok: &val}
		err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&input), mem, nil, nil, nil)
		if err != nil {
			t.Fatalf("encodeFieldToMemory failed: %v", err)
		}

		disc, _ := mem.ReadU8(0)
		if disc != 0 {
			t.Errorf("expected discriminant 0 for ok, got %d", disc)
		}
	})

	// Test err case
	t.Run("err", func(t *testing.T) {
		mem := newMockMemory(4096)
		errMsg := "error"
		input := MyResult{Err: &errMsg}
		alloc := newMockAllocator(mem)
		err = enc.encodeFieldToMemory(0, compiled, unsafe.Pointer(&input), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("encodeFieldToMemory failed: %v", err)
		}

		disc, _ := mem.ReadU8(0)
		if disc != 1 {
			t.Errorf("expected discriminant 1 for err, got %d", disc)
		}
	})
}

// TestDecoder_ListFromMemory_AllPrimitives tests list decoding for all primitive types
func TestDecoder_ListFromMemory_AllPrimitives(t *testing.T) {
	dec := NewDecoder()

	t.Run("[]byte", func(t *testing.T) {
		mem := newMockMemory(4096)
		// List header at addr 0: data at 100, length 5
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 5)
		// Data
		mem.data[100] = 1
		mem.data[101] = 2
		mem.data[102] = 3
		mem.data[103] = 4
		mem.data[104] = 5

		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}
		compiled, _ := dec.compiler.Compile(listType, reflect.TypeOf([]byte{}))

		var result []byte
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(result) != 5 || result[0] != 1 || result[4] != 5 {
			t.Errorf("got %v, want [1 2 3 4 5]", result)
		}
	})

	t.Run("[]int32", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 3)
		mem.WriteU32(100, 0xFFFFFFFF) // -1
		mem.WriteU32(104, 0x7FFFFFFF) // max int32
		mem.WriteU32(108, 0x80000000) // min int32

		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.S32{}}}
		compiled, _ := dec.compiler.Compile(listType, reflect.TypeOf([]int32{}))

		var result []int32
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(result) != 3 || result[0] != -1 || result[1] != 0x7FFFFFFF || result[2] != -0x80000000 {
			t.Errorf("got %v", result)
		}
	})

	t.Run("[]int64", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 2)
		mem.WriteU64(100, 0xFFFFFFFFFFFFFFFF) // -1
		mem.WriteU64(108, 0x7FFFFFFFFFFFFFFF) // max int64

		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.S64{}}}
		compiled, _ := dec.compiler.Compile(listType, reflect.TypeOf([]int64{}))

		var result []int64
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(result) != 2 || result[0] != -1 {
			t.Errorf("got %v", result)
		}
	})

	t.Run("[]float32", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 2)
		mem.WriteU32(100, math.Float32bits(1.5))
		mem.WriteU32(104, math.Float32bits(-2.5))

		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.F32{}}}
		compiled, _ := dec.compiler.Compile(listType, reflect.TypeOf([]float32{}))

		var result []float32
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(result) != 2 || result[0] != 1.5 || result[1] != -2.5 {
			t.Errorf("got %v", result)
		}
	})

	t.Run("[]float64", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 2)
		mem.WriteU64(100, math.Float64bits(3.14159))
		mem.WriteU64(108, math.Float64bits(-2.71828))

		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.F64{}}}
		compiled, _ := dec.compiler.Compile(listType, reflect.TypeOf([]float64{}))

		var result []float64
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(result) != 2 || result[0] != 3.14159 || result[1] != -2.71828 {
			t.Errorf("got %v", result)
		}
	})

	t.Run("list overflow protection", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 0xFFFFFFFF) // huge length

		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.U32{}}}
		compiled, _ := dec.compiler.Compile(listType, reflect.TypeOf([]uint32{}))

		var result []uint32
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err == nil {
			t.Fatal("expected overflow error")
		}
	})

	t.Run("complex element type", func(t *testing.T) {
		mem := newMockMemory(4096)
		// List of records: data at 100, length 2
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 2)
		// Record 0: {x: 10, y: 20}
		mem.WriteU32(100, 10)
		mem.WriteU32(104, 20)
		// Record 1: {x: 30, y: 40}
		mem.WriteU32(108, 30)
		mem.WriteU32(112, 40)

		type Point struct {
			X uint32 `wit:"x"`
			Y uint32 `wit:"y"`
		}
		recordType := &wit.TypeDef{Kind: &wit.Record{Fields: []wit.Field{
			{Name: "x", Type: wit.U32{}},
			{Name: "y", Type: wit.U32{}},
		}}}
		listType := &wit.TypeDef{Kind: &wit.List{Type: recordType}}

		compiled, err := dec.compiler.Compile(listType, reflect.TypeOf([]Point{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result []Point
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if len(result) != 2 || result[0].X != 10 || result[1].Y != 40 {
			t.Errorf("got %v", result)
		}
	})
}

// TestDecoder_FlagsFromMemory_AllWidths tests flags decoding for all width cases
func TestDecoder_FlagsFromMemory_AllWidths(t *testing.T) {
	dec := NewDecoder()

	t.Run("1-8 flags (u8)", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 0b10101010

		flags := make([]wit.Flag, 8)
		for i := 0; i < 8; i++ {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		compiled, _ := dec.compiler.Compile(flagsType, reflect.TypeOf(uint8(0)))

		var result uint8
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != 0b10101010 {
			t.Errorf("got %08b, want 10101010", result)
		}
	})

	t.Run("9-16 flags (u16)", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU16(0, 0xABCD)

		flags := make([]wit.Flag, 16)
		for i := 0; i < 16; i++ {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		compiled, _ := dec.compiler.Compile(flagsType, reflect.TypeOf(uint16(0)))

		var result uint16
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != 0xABCD {
			t.Errorf("got %04x, want abcd", result)
		}
	})

	t.Run("17-32 flags (u32)", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 0xDEADBEEF)

		flags := make([]wit.Flag, 32)
		for i := 0; i < 32; i++ {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		compiled, _ := dec.compiler.Compile(flagsType, reflect.TypeOf(uint32(0)))

		var result uint32
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != 0xDEADBEEF {
			t.Errorf("got %08x, want deadbeef", result)
		}
	})

	t.Run("33-64 flags (u64)", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU64(0, 0xDEADBEEFCAFEBABE)

		flags := make([]wit.Flag, 64)
		for i := 0; i < 64; i++ {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		compiled, _ := dec.compiler.Compile(flagsType, reflect.TypeOf(uint64(0)))

		var result uint64
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != 0xDEADBEEFCAFEBABE {
			t.Errorf("got %016x", result)
		}
	})

	t.Run(">64 flags rejected by compiler", func(t *testing.T) {
		// Compiler explicitly rejects >64 flags
		flags := make([]wit.Flag, 96)
		for i := 0; i < 96; i++ {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{Kind: &wit.Flags{Flags: flags}}
		_, err := dec.compiler.Compile(flagsType, reflect.TypeOf([3]uint32{}))
		if err == nil {
			t.Fatal("expected compiler to reject >64 flags")
		}
	})
}

// TestDecoder_EnumFromMemory_AllSizes tests enum decoding for different discriminant sizes
func TestDecoder_EnumFromMemory_AllSizes(t *testing.T) {
	dec := NewDecoder()

	t.Run("u8 discriminant (<=256 cases)", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 5

		cases := make([]wit.EnumCase, 10)
		for i := 0; i < 10; i++ {
			cases[i] = wit.EnumCase{Name: "case" + strconv.Itoa(i)}
		}
		enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
		compiled, _ := dec.compiler.Compile(enumType, reflect.TypeOf(uint8(0)))

		var result uint8
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != 5 {
			t.Errorf("got %d, want 5", result)
		}
	})

	t.Run("u16 discriminant (>256 cases)", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU16(0, 300)

		cases := make([]wit.EnumCase, 400)
		for i := 0; i < 400; i++ {
			cases[i] = wit.EnumCase{Name: "c" + strconv.Itoa(i)}
		}
		enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
		compiled, _ := dec.compiler.Compile(enumType, reflect.TypeOf(uint16(0)))

		var result uint16
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != 300 {
			t.Errorf("got %d, want 300", result)
		}
	})

	t.Run("u32 discriminant (>65536 cases)", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 70000)

		cases := make([]wit.EnumCase, 70001)
		for i := 0; i <= 70000; i++ {
			cases[i] = wit.EnumCase{Name: "c" + strconv.Itoa(i)}
		}
		enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
		compiled, _ := dec.compiler.Compile(enumType, reflect.TypeOf(uint32(0)))

		var result uint32
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != 70000 {
			t.Errorf("got %d, want 70000", result)
		}
	})

	t.Run("invalid discriminant", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 99 // out of bounds

		cases := make([]wit.EnumCase, 3)
		for i := 0; i < 3; i++ {
			cases[i] = wit.EnumCase{Name: "c" + strconv.Itoa(i)}
		}
		enumType := &wit.TypeDef{Kind: &wit.Enum{Cases: cases}}
		compiled, _ := dec.compiler.Compile(enumType, reflect.TypeOf(uint8(0)))

		var result uint8
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err == nil {
			t.Fatal("expected error for invalid discriminant")
		}
	})
}

// TestDecoder_VariantFromMemory tests variant decoding
func TestDecoder_VariantFromMemory(t *testing.T) {
	dec := NewDecoder()

	// Variant struct uses pointer fields - active case is non-nil
	type MyVariant struct {
		Num  *int32
		Text *string
	}

	t.Run("case with int32 payload", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 0 // case 0: num
		mem.WriteU32(4, 42)

		variantType := &wit.TypeDef{Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "num", Type: wit.S32{}},
			{Name: "text", Type: wit.String{}},
		}}}
		compiled, err := dec.compiler.Compile(variantType, reflect.TypeOf(MyVariant{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result MyVariant
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result.Num == nil || *result.Num != 42 || result.Text != nil {
			t.Errorf("got %+v", result)
		}
	})

	t.Run("case with string payload", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 1 // case 1: text
		mem.WriteU32(4, 100)
		mem.WriteU32(8, 5)
		copy(mem.data[100:], "hello")

		variantType := &wit.TypeDef{Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "num", Type: wit.S32{}},
			{Name: "text", Type: wit.String{}},
		}}}
		compiled, err := dec.compiler.Compile(variantType, reflect.TypeOf(MyVariant{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result MyVariant
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result.Text == nil || *result.Text != "hello" || result.Num != nil {
			t.Errorf("got %+v", result)
		}
	})

	t.Run("case without payload", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 0 // case 0: none (no payload)

		// For nil-payload cases, use unsafe.Pointer fields
		type NilVariant struct {
			None unsafe.Pointer
			Some *uint32
		}

		variantType := &wit.TypeDef{Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "none", Type: nil},
			{Name: "some", Type: wit.U32{}},
		}}}
		compiled, err := dec.compiler.Compile(variantType, reflect.TypeOf(NilVariant{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result NilVariant
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		// None should be set to UnitPtr, Some should be nil
		if result.None == nil || result.Some != nil {
			t.Errorf("got None=%v, Some=%v", result.None, result.Some)
		}
	})

	t.Run("invalid discriminant", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 99 // invalid

		type V struct{ A unsafe.Pointer }
		variantType := &wit.TypeDef{Kind: &wit.Variant{Cases: []wit.Case{
			{Name: "a", Type: nil},
		}}}
		compiled, _ := dec.compiler.Compile(variantType, reflect.TypeOf(V{}))

		var result V
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err == nil {
			t.Fatal("expected error for invalid discriminant")
		}
	})
}

// TestDecoder_ResultFromMemory tests result decoding with different payload types
func TestDecoder_ResultFromMemory(t *testing.T) {
	dec := NewDecoder()

	t.Run("ok with i32 payload", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 0 // ok
		mem.WriteU32(4, 123)

		// Result struct uses *T fields where T is the payload type
		type IntStrResult struct {
			Ok  *int32
			Err *string
		}

		resultType := &wit.TypeDef{Kind: &wit.Result{
			OK:  wit.S32{},
			Err: wit.String{},
		}}
		compiled, err := dec.compiler.Compile(resultType, reflect.TypeOf(IntStrResult{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result IntStrResult
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result.Ok == nil || *result.Ok != 123 {
			t.Errorf("got Ok=%v", result.Ok)
		}
		if result.Err != nil {
			t.Errorf("Err should be nil")
		}
	})

	t.Run("err with string payload", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 1 // err
		mem.WriteU32(4, 100)
		mem.WriteU32(8, 5)
		copy(mem.data[100:], "error")

		type IntStrResult struct {
			Ok  *int32
			Err *string
		}

		resultType := &wit.TypeDef{Kind: &wit.Result{
			OK:  wit.S32{},
			Err: wit.String{},
		}}
		compiled, err := dec.compiler.Compile(resultType, reflect.TypeOf(IntStrResult{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result IntStrResult
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result.Err == nil || *result.Err != "error" {
			t.Errorf("got Err=%v", result.Err)
		}
		if result.Ok != nil {
			t.Errorf("Ok should be nil")
		}
	})

	t.Run("result with no payloads - ok", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 0 // ok (no payload)

		// For nil payloads, decoder uses UnitPtr via unsafe.Pointer
		type EmptyResult struct {
			Ok  unsafe.Pointer
			Err unsafe.Pointer
		}

		resultType := &wit.TypeDef{Kind: &wit.Result{
			OK:  nil,
			Err: nil,
		}}
		compiled, err := dec.compiler.Compile(resultType, reflect.TypeOf(EmptyResult{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result EmptyResult
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		// Ok should be UnitPtr (non-nil), Err should be nil
		if result.Ok == nil || result.Err != nil {
			t.Errorf("got Ok=%v, Err=%v", result.Ok, result.Err)
		}
	})

	t.Run("result with no payloads - err", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 1 // err (no payload)

		type EmptyResult struct {
			Ok  unsafe.Pointer
			Err unsafe.Pointer
		}

		resultType := &wit.TypeDef{Kind: &wit.Result{
			OK:  nil,
			Err: nil,
		}}
		compiled, err := dec.compiler.Compile(resultType, reflect.TypeOf(EmptyResult{}))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		var result EmptyResult
		err = dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		// Ok should be nil, Err should be UnitPtr (non-nil)
		if result.Ok != nil || result.Err == nil {
			t.Errorf("got Ok=%v, Err=%v", result.Ok, result.Err)
		}
	})

	t.Run("invalid discriminant", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.data[0] = 5 // invalid

		type EmptyResult struct {
			Ok  unsafe.Pointer
			Err unsafe.Pointer
		}
		resultType := &wit.TypeDef{Kind: &wit.Result{OK: nil, Err: nil}}
		compiled, _ := dec.compiler.Compile(resultType, reflect.TypeOf(EmptyResult{}))

		var result EmptyResult
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err == nil {
			t.Fatal("expected error for invalid discriminant")
		}
	})
}

// TestDecoder_StringFromMemory tests string decoding edge cases
func TestDecoder_StringFromMemory(t *testing.T) {
	dec := NewDecoder()

	t.Run("valid UTF-8", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 12)
		copy(mem.data[100:], "hello world!")

		stringType := wit.String{}
		compiled, _ := dec.compiler.Compile(stringType, reflect.TypeOf(""))

		var result string
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != "hello world!" {
			t.Errorf("got %q", result)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 0)
		mem.WriteU32(4, 0)

		compiled, _ := dec.compiler.Compile(wit.String{}, reflect.TypeOf(""))

		var result string
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != "" {
			t.Errorf("got %q, want empty", result)
		}
	})

	t.Run("unicode string", func(t *testing.T) {
		mem := newMockMemory(4096)
		s := "hello"
		mem.WriteU32(0, 100)
		mem.WriteU32(4, uint32(len(s)))
		copy(mem.data[100:], s)

		compiled, _ := dec.compiler.Compile(wit.String{}, reflect.TypeOf(""))

		var result string
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if result != s {
			t.Errorf("got %q", result)
		}
	})

	t.Run("invalid UTF-8", func(t *testing.T) {
		mem := newMockMemory(4096)
		mem.WriteU32(0, 100)
		mem.WriteU32(4, 3)
		mem.data[100] = 0xFF // invalid UTF-8 byte
		mem.data[101] = 0xFE
		mem.data[102] = 0xFD

		compiled, _ := dec.compiler.Compile(wit.String{}, reflect.TypeOf(""))

		var result string
		err := dec.decodeFieldFromMemory(0, compiled, unsafe.Pointer(&result), mem, nil)
		if err == nil {
			t.Fatal("expected error for invalid UTF-8")
		}
	})
}

// TestCompiler_FindGoField tests field lookup with various edge cases
func TestCompiler_FindGoField(t *testing.T) {
	c := NewCompiler()

	t.Run("unexported field skipped", func(t *testing.T) {
		type StructWithUnexported struct {
			_        string // unexported field for testing
			Exported string
		}

		goType := reflect.TypeOf(StructWithUnexported{})
		field, found := c.findGoField(goType, "_")
		if found {
			t.Error("unexported field should not be found")
		}
		_ = field
	})

	t.Run("wit tag dash skips field", func(t *testing.T) {
		type StructWithDash struct {
			Skipped  string `wit:"-"`
			Included string
		}

		goType := reflect.TypeOf(StructWithDash{})
		field, found := c.findGoField(goType, "skipped")
		if found {
			t.Error("field with wit:\"-\" should not be found")
		}
		_ = field
	})

	t.Run("wit tag name match", func(t *testing.T) {
		type StructWithTag struct {
			MyField string `wit:"custom-name"`
		}

		goType := reflect.TypeOf(StructWithTag{})
		field, found := c.findGoField(goType, "custom-name")
		if !found {
			t.Fatal("field with wit tag should be found")
		}
		if field.Name != "MyField" {
			t.Errorf("wrong field: %s", field.Name)
		}
	})

	t.Run("case insensitive match", func(t *testing.T) {
		type StructCaseInsensitive struct {
			MyFieldName string
		}

		goType := reflect.TypeOf(StructCaseInsensitive{})
		field, found := c.findGoField(goType, "myfieldname")
		if !found {
			t.Fatal("case insensitive match should work")
		}
		if field.Name != "MyFieldName" {
			t.Errorf("wrong field: %s", field.Name)
		}
	})

	t.Run("kebab case match", func(t *testing.T) {
		type StructKebab struct {
			MyFieldName string
		}

		goType := reflect.TypeOf(StructKebab{})
		field, found := c.findGoField(goType, "my-field-name")
		if !found {
			t.Fatal("kebab case match should work")
		}
		if field.Name != "MyFieldName" {
			t.Errorf("wrong field: %s", field.Name)
		}
	})

	t.Run("field not found", func(t *testing.T) {
		type StructNoMatch struct {
			SomeField string
		}

		goType := reflect.TypeOf(StructNoMatch{})
		_, found := c.findGoField(goType, "nonexistent")
		if found {
			t.Error("nonexistent field should not be found")
		}
	})
}

// TestCompiler_Own tests Own type compilation
func TestCompiler_Own(t *testing.T) {
	c := NewCompiler()

	t.Run("uint32 type", func(t *testing.T) {
		ownType := &wit.TypeDef{
			Kind: &wit.Own{},
		}
		compiled, err := c.Compile(ownType, reflect.TypeOf(uint32(0)))
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if compiled.Kind != KindOwn {
			t.Errorf("wrong kind: %v", compiled.Kind)
		}
		if compiled.WitSize != 4 {
			t.Errorf("wrong size: %d", compiled.WitSize)
		}
	})

	t.Run("struct type (Own wrapper)", func(t *testing.T) {
		type OwnResource struct {
			Handle uint32
		}
		ownType := &wit.TypeDef{
			Kind: &wit.Own{},
		}
		compiled, err := c.Compile(ownType, reflect.TypeOf(OwnResource{}))
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if compiled.Kind != KindOwn {
			t.Errorf("wrong kind: %v", compiled.Kind)
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		ownType := &wit.TypeDef{
			Kind: &wit.Own{},
		}
		_, err := c.Compile(ownType, reflect.TypeOf("string"))
		if err == nil {
			t.Fatal("expected error for invalid type")
		}
	})
}

// TestCompiler_Borrow tests Borrow type compilation
func TestCompiler_Borrow(t *testing.T) {
	c := NewCompiler()

	t.Run("uint32 type", func(t *testing.T) {
		borrowType := &wit.TypeDef{
			Kind: &wit.Borrow{},
		}
		compiled, err := c.Compile(borrowType, reflect.TypeOf(uint32(0)))
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if compiled.Kind != KindBorrow {
			t.Errorf("wrong kind: %v", compiled.Kind)
		}
		if compiled.WitSize != 4 {
			t.Errorf("wrong size: %d", compiled.WitSize)
		}
	})

	t.Run("struct type (Borrow wrapper)", func(t *testing.T) {
		type BorrowResource struct {
			Handle uint32
		}
		borrowType := &wit.TypeDef{
			Kind: &wit.Borrow{},
		}
		compiled, err := c.Compile(borrowType, reflect.TypeOf(BorrowResource{}))
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if compiled.Kind != KindBorrow {
			t.Errorf("wrong kind: %v", compiled.Kind)
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		borrowType := &wit.TypeDef{
			Kind: &wit.Borrow{},
		}
		_, err := c.Compile(borrowType, reflect.TypeOf("string"))
		if err == nil {
			t.Fatal("expected error for invalid type")
		}
	})
}

// TestEncoder_TryFastEncode tests the fast path for typed structs/slices
func TestEncoder_TryFastEncode(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	t.Run("typed struct record", func(t *testing.T) {
		type Point struct {
			X int32
			Y int32
		}
		recordType := &wit.TypeDef{
			Kind: &wit.Record{
				Fields: []wit.Field{
					{Name: "x", Type: wit.S32{}},
					{Name: "y", Type: wit.S32{}},
				},
			},
		}

		flat := make([]uint64, 0, 16)
		result := enc.tryFastEncode(recordType, Point{X: 10, Y: 20}, &flat, mem, alloc, nil)
		if !result {
			t.Error("tryFastEncode should succeed for typed struct")
		}
		if len(flat) != 2 {
			t.Errorf("expected 2 flat values, got %d", len(flat))
		}
	})

	t.Run("typed slice list", func(t *testing.T) {
		listType := &wit.TypeDef{
			Kind: &wit.List{Type: wit.S32{}},
		}

		flat := make([]uint64, 0, 16)
		result := enc.tryFastEncode(listType, []int32{1, 2, 3}, &flat, mem, alloc, nil)
		if !result {
			t.Error("tryFastEncode should succeed for typed slice")
		}
		if len(flat) != 2 {
			t.Errorf("expected 2 flat values (ptr+len), got %d", len(flat))
		}
	})

	t.Run("non-typedef returns false", func(t *testing.T) {
		flat := make([]uint64, 0, 16)
		result := enc.tryFastEncode(wit.S32{}, int32(42), &flat, mem, alloc, nil)
		if result {
			t.Error("tryFastEncode should return false for non-typedef")
		}
	})

	t.Run("wrong kind returns false", func(t *testing.T) {
		recordType := &wit.TypeDef{
			Kind: &wit.Record{
				Fields: []wit.Field{
					{Name: "x", Type: wit.S32{}},
				},
			},
		}

		flat := make([]uint64, 0, 16)
		result := enc.tryFastEncode(recordType, []int32{1, 2, 3}, &flat, mem, alloc, nil)
		if result {
			t.Error("tryFastEncode should return false for mismatched kind")
		}
	})
}

// TestEncoder_EncodeFlagsToMemory_AllWidths tests flags encoding for all bit widths
func TestEncoder_EncodeFlagsToMemory(t *testing.T) {
	enc := NewEncoder()
	mem := newMockMemory(4096)

	t.Run("8-bit flags", func(t *testing.T) {
		flagsType := &wit.TypeDef{
			Kind: &wit.Flags{
				Flags: []wit.Flag{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			},
		}
		compiled, _ := enc.compiler.Compile(flagsType, reflect.TypeOf(uint8(0)))

		var val uint8 = 0b101
		err := enc.encodeFlagsToMemory(0, compiled, unsafe.Pointer(&val), mem)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		v, _ := mem.ReadU8(0)
		if v != 0b101 {
			t.Errorf("got %d, want 5", v)
		}
	})

	t.Run("16-bit flags", func(t *testing.T) {
		flags := make([]wit.Flag, 12)
		for i := range flags {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{
			Kind: &wit.Flags{Flags: flags},
		}
		compiled, _ := enc.compiler.Compile(flagsType, reflect.TypeOf(uint16(0)))

		var val uint16 = 0xABCD
		err := enc.encodeFlagsToMemory(0, compiled, unsafe.Pointer(&val), mem)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		v, _ := mem.ReadU16(0)
		if v != 0xABCD {
			t.Errorf("got %x, want 0xABCD", v)
		}
	})

	t.Run("32-bit flags", func(t *testing.T) {
		flags := make([]wit.Flag, 25)
		for i := range flags {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{
			Kind: &wit.Flags{Flags: flags},
		}
		compiled, _ := enc.compiler.Compile(flagsType, reflect.TypeOf(uint32(0)))

		var val uint32 = 0xDEADBEEF
		err := enc.encodeFlagsToMemory(0, compiled, unsafe.Pointer(&val), mem)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		v, _ := mem.ReadU32(0)
		if v != 0xDEADBEEF {
			t.Errorf("got %x, want 0xDEADBEEF", v)
		}
	})

	t.Run("64-bit flags", func(t *testing.T) {
		flags := make([]wit.Flag, 50)
		for i := range flags {
			flags[i] = wit.Flag{Name: "f" + strconv.Itoa(i)}
		}
		flagsType := &wit.TypeDef{
			Kind: &wit.Flags{Flags: flags},
		}
		compiled, _ := enc.compiler.Compile(flagsType, reflect.TypeOf(uint64(0)))

		var val uint64 = 0xCAFEBABEDEADBEEF
		err := enc.encodeFlagsToMemory(0, compiled, unsafe.Pointer(&val), mem)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		v, _ := mem.ReadU64(0)
		if v != 0xCAFEBABEDEADBEEF {
			t.Errorf("got %x, want 0xCAFEBABEDEADBEEF", v)
		}
	})
}

// TestEncoder_EncodeListToMemory_AllTypes tests list encoding for various element types
func TestEncoder_EncodeListToMemory(t *testing.T) {
	enc := NewEncoder()

	t.Run("empty list", func(t *testing.T) {
		mem := newMockMemory(4096)
		alloc := newMockAllocator(mem)
		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.S32{}}}
		compiled, _ := enc.compiler.Compile(listType, reflect.TypeOf([]int32{}))

		data := []int32{}
		err := enc.encodeListToMemory(0, compiled, unsafe.Pointer(&data), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		ptr, _ := mem.ReadU32(0)
		length, _ := mem.ReadU32(4)
		if ptr != 0 || length != 0 {
			t.Errorf("empty list: ptr=%d, len=%d", ptr, length)
		}
	})

	t.Run("int64 list", func(t *testing.T) {
		mem := newMockMemory(4096)
		alloc := newMockAllocator(mem)
		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.S64{}}}
		compiled, _ := enc.compiler.Compile(listType, reflect.TypeOf([]int64{}))

		data := []int64{100, 200, 300}
		err := enc.encodeListToMemory(0, compiled, unsafe.Pointer(&data), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		length, _ := mem.ReadU32(4)
		if length != 3 {
			t.Errorf("expected length 3, got %d", length)
		}
	})

	t.Run("float32 list with NaN", func(t *testing.T) {
		mem := newMockMemory(4096)
		alloc := newMockAllocator(mem)
		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.F32{}}}
		compiled, _ := enc.compiler.Compile(listType, reflect.TypeOf([]float32{}))

		nan := math.Float32frombits(0x7FC00001)
		data := []float32{1.0, nan, 3.0}
		err := enc.encodeListToMemory(0, compiled, unsafe.Pointer(&data), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		ptr, _ := mem.ReadU32(0)
		bits, _ := mem.ReadU32(ptr + 4)
		if bits != 0x7fc00000 {
			t.Errorf("NaN not canonicalized: got %x", bits)
		}
	})

	t.Run("float64 list with NaN", func(t *testing.T) {
		mem := newMockMemory(4096)
		alloc := newMockAllocator(mem)
		listType := &wit.TypeDef{Kind: &wit.List{Type: wit.F64{}}}
		compiled, _ := enc.compiler.Compile(listType, reflect.TypeOf([]float64{}))

		nan := math.Float64frombits(0x7FF8000000000001)
		data := []float64{1.0, nan, 3.0}
		err := enc.encodeListToMemory(0, compiled, unsafe.Pointer(&data), mem, alloc, nil, nil)
		if err != nil {
			t.Fatalf("encode failed: %v", err)
		}
		ptr, _ := mem.ReadU32(0)
		bits, _ := mem.ReadU64(ptr + 8)
		if bits != 0x7ff8000000000000 {
			t.Errorf("NaN not canonicalized: got %x", bits)
		}
	})
}
