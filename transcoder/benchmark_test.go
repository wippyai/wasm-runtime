package transcoder

import (
	"encoding/binary"
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

// benchMemory implements Memory for benchmarks
type benchMemory struct {
	data []byte
}

func newBenchMemory(size int) *benchMemory {
	return &benchMemory{data: make([]byte, size)}
}

func (m *benchMemory) Read(offset uint32, length uint32) ([]byte, error) {
	return m.data[offset : offset+length], nil
}

func (m *benchMemory) Write(offset uint32, data []byte) error {
	copy(m.data[offset:], data)
	return nil
}

func (m *benchMemory) ReadU8(offset uint32) (uint8, error) {
	return m.data[offset], nil
}

func (m *benchMemory) ReadU16(offset uint32) (uint16, error) {
	return binary.LittleEndian.Uint16(m.data[offset:]), nil
}

func (m *benchMemory) ReadU32(offset uint32) (uint32, error) {
	return binary.LittleEndian.Uint32(m.data[offset:]), nil
}

func (m *benchMemory) ReadU64(offset uint32) (uint64, error) {
	return binary.LittleEndian.Uint64(m.data[offset:]), nil
}

func (m *benchMemory) WriteU8(offset uint32, value uint8) error {
	m.data[offset] = value
	return nil
}

func (m *benchMemory) WriteU16(offset uint32, value uint16) error {
	binary.LittleEndian.PutUint16(m.data[offset:], value)
	return nil
}

func (m *benchMemory) WriteU32(offset uint32, value uint32) error {
	binary.LittleEndian.PutUint32(m.data[offset:], value)
	return nil
}

func (m *benchMemory) WriteU64(offset uint32, value uint64) error {
	binary.LittleEndian.PutUint64(m.data[offset:], value)
	return nil
}

// benchAllocator implements Allocator for benchmarks
type benchAllocator struct {
	offset uint32
}

func (a *benchAllocator) Alloc(size, align uint32) (uint32, error) {
	a.offset = alignTo(a.offset, align)
	ptr := a.offset
	a.offset += size
	return ptr, nil
}

func (a *benchAllocator) Free(ptr, size, align uint32) {}

func (a *benchAllocator) Reset() {
	a.offset = 1024
}

// Benchmark primitives
func BenchmarkEncode_U32(b *testing.B) {
	enc := NewEncoder()
	mem := newBenchMemory(4096)
	alloc := &benchAllocator{offset: 1024}
	allocList := NewAllocationList()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		allocList.Reset()
		_, _ = enc.EncodeParams([]wit.Type{wit.U32{}}, []any{uint32(42)}, mem, alloc, allocList)
	}
}

func BenchmarkEncode_String_Small(b *testing.B) {
	enc := NewEncoder()
	mem := newBenchMemory(4096)
	alloc := &benchAllocator{offset: 1024}
	allocList := NewAllocationList()
	s := "hello"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		allocList.Reset()
		_, _ = enc.EncodeParams([]wit.Type{wit.String{}}, []any{s}, mem, alloc, allocList)
	}
}

func BenchmarkEncode_String_Large(b *testing.B) {
	enc := NewEncoder()
	mem := newBenchMemory(65536)
	alloc := &benchAllocator{offset: 1024}
	allocList := NewAllocationList()
	s := string(make([]byte, 10000))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		allocList.Reset()
		_, _ = enc.EncodeParams([]wit.Type{wit.String{}}, []any{s}, mem, alloc, allocList)
	}
}

func BenchmarkDecode_U32(b *testing.B) {
	dec := NewDecoder()
	mem := newBenchMemory(4096)
	flat := []uint64{42}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.DecodeResults([]wit.Type{wit.U32{}}, flat, mem)
	}
}

func BenchmarkDecode_String_Small(b *testing.B) {
	dec := NewDecoder()
	mem := newBenchMemory(4096)

	// Set up string
	s := "hello"
	copy(mem.data[1024:], s)
	flat := []uint64{1024, uint64(len(s))}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.DecodeResults([]wit.Type{wit.String{}}, flat, mem)
	}
}

// Benchmark records
func BenchmarkEncode_Record_Map(b *testing.B) {
	enc := NewEncoder()
	mem := newBenchMemory(4096)
	alloc := &benchAllocator{offset: 1024}
	allocList := NewAllocationList()

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	input := map[string]any{
		"x": uint32(100),
		"y": uint32(200),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		allocList.Reset()
		_, _ = enc.EncodeParams([]wit.Type{recordType}, []any{input}, mem, alloc, allocList)
	}
}

func BenchmarkEncode_Record_Struct_Unsafe(b *testing.B) {
	type Point struct {
		X uint32
		Y uint32
	}

	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	mem := newBenchMemory(4096)
	alloc := &benchAllocator{offset: 1024}

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiled, _ := compiler.Compile(recordType, reflect.TypeOf(Point{}))
	input := Point{X: 100, Y: 200}
	stack := make([]uint64, 16)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		_, _ = enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, mem, alloc)
	}
}

func BenchmarkEncode_Record_Struct_ZeroAlloc(b *testing.B) {
	type Point struct {
		X uint32
		Y uint32
	}

	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)
	stack := make([]uint64, 16)

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiled, _ := compiler.Compile(recordType, reflect.TypeOf(Point{}))
	input := Point{X: 100, Y: 200}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enc.LowerToStack(compiled, unsafe.Pointer(&input), stack, nil, nil)
	}
}

func BenchmarkDecode_Record_ToMap(b *testing.B) {
	dec := NewDecoder()
	mem := newBenchMemory(4096)

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	flat := []uint64{100, 200}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.DecodeResults([]wit.Type{recordType}, flat, mem)
	}
}

func BenchmarkDecode_Record_Struct_Unsafe(b *testing.B) {
	type Point struct {
		X uint32
		Y uint32
	}

	compiler := NewCompiler()
	dec := NewDecoderWithCompiler(compiler)
	mem := newBenchMemory(4096)

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiled, _ := compiler.Compile(recordType, reflect.TypeOf(Point{}))

	stack := []uint64{100, 200}
	var output Point

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.LiftFromStack(compiled, stack, unsafe.Pointer(&output), mem)
	}
}

// Benchmark lists
func BenchmarkEncode_List_U32_100(b *testing.B) {
	enc := NewEncoder()
	mem := newBenchMemory(65536)
	alloc := &benchAllocator{offset: 1024}
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	input := make([]any, 100)
	for i := range input {
		input[i] = uint32(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		allocList.Reset()
		_, _ = enc.EncodeParams([]wit.Type{listType}, []any{input}, mem, alloc, allocList)
	}
}

func BenchmarkEncode_List_U32_1000(b *testing.B) {
	enc := NewEncoder()
	mem := newBenchMemory(65536)
	alloc := &benchAllocator{offset: 1024}
	allocList := NewAllocationList()

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	input := make([]any, 1000)
	for i := range input {
		input[i] = uint32(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		allocList.Reset()
		_, _ = enc.EncodeParams([]wit.Type{listType}, []any{input}, mem, alloc, allocList)
	}
}

func BenchmarkDecode_List_U32_100(b *testing.B) {
	dec := NewDecoder()
	mem := newBenchMemory(65536)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	// Set up list data
	for i := 0; i < 100; i++ {
		binary.LittleEndian.PutUint32(mem.data[1024+i*4:], uint32(i))
	}
	flat := []uint64{1024, 100}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.DecodeResults([]wit.Type{listType}, flat, mem)
	}
}

func BenchmarkDecode_List_U32_1000(b *testing.B) {
	dec := NewDecoder()
	mem := newBenchMemory(65536)

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U32{}},
	}

	// Set up list data
	for i := 0; i < 1000; i++ {
		binary.LittleEndian.PutUint32(mem.data[1024+i*4:], uint32(i))
	}
	flat := []uint64{1024, 1000}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.DecodeResults([]wit.Type{listType}, flat, mem)
	}
}

// Benchmark type compilation
func BenchmarkCompile_Record(b *testing.B) {
	type Point struct {
		X uint32
		Y uint32
	}

	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiler := NewCompiler() // fresh compiler each time to test non-cached
		_, _ = compiler.Compile(recordType, reflect.TypeOf(Point{}))
	}
}

func BenchmarkCompile_Record_Cached(b *testing.B) {
	type Point struct {
		X uint32
		Y uint32
	}

	compiler := NewCompiler()
	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	// Pre-compile
	_, _ = compiler.Compile(recordType, reflect.TypeOf(Point{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiler.Compile(recordType, reflect.TypeOf(Point{}))
	}
}

// Benchmark complex nested type
func BenchmarkEncode_NestedRecord(b *testing.B) {
	enc := NewEncoder()
	mem := newBenchMemory(65536)
	alloc := &benchAllocator{offset: 1024}
	allocList := NewAllocationList()

	innerRecordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	outerRecordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "name", Type: wit.String{}},
				{Name: "point", Type: innerRecordType},
				{Name: "id", Type: wit.U64{}},
			},
		},
	}

	input := map[string]any{
		"name": "test",
		"point": map[string]any{
			"x": uint32(100),
			"y": uint32(200),
		},
		"id": uint64(12345),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.Reset()
		allocList.Reset()
		_, _ = enc.EncodeParams([]wit.Type{outerRecordType}, []any{input}, mem, alloc, allocList)
	}
}
