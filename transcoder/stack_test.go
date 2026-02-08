package transcoder

import (
	"encoding/binary"
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

// test helpers

type testMemory struct {
	data []byte
}

func newTestMemory(size int) *testMemory {
	return &testMemory{data: make([]byte, size)}
}

func (m *testMemory) Read(offset uint32, length uint32) ([]byte, error) {
	return m.data[offset : offset+length], nil
}

func (m *testMemory) Write(offset uint32, data []byte) error {
	copy(m.data[offset:], data)
	return nil
}

func (m *testMemory) ReadU8(offset uint32) (uint8, error) {
	return m.data[offset], nil
}

func (m *testMemory) ReadU16(offset uint32) (uint16, error) {
	return binary.LittleEndian.Uint16(m.data[offset:]), nil
}

func (m *testMemory) ReadU32(offset uint32) (uint32, error) {
	return binary.LittleEndian.Uint32(m.data[offset:]), nil
}

func (m *testMemory) ReadU64(offset uint32) (uint64, error) {
	return binary.LittleEndian.Uint64(m.data[offset:]), nil
}

func (m *testMemory) WriteU8(offset uint32, value uint8) error {
	m.data[offset] = value
	return nil
}

func (m *testMemory) WriteU16(offset uint32, value uint16) error {
	binary.LittleEndian.PutUint16(m.data[offset:], value)
	return nil
}

func (m *testMemory) WriteU32(offset uint32, value uint32) error {
	binary.LittleEndian.PutUint32(m.data[offset:], value)
	return nil
}

func (m *testMemory) WriteU64(offset uint32, value uint64) error {
	binary.LittleEndian.PutUint64(m.data[offset:], value)
	return nil
}

type testAllocator struct {
	data   []byte
	offset uint32
}

func newTestAllocator(data []byte, offset uint32) *testAllocator {
	return &testAllocator{data: data, offset: offset}
}

func (a *testAllocator) Alloc(size, align uint32) (uint32, error) {
	// Align offset
	a.offset = (a.offset + align - 1) &^ (align - 1)
	addr := a.offset
	a.offset += size
	return addr, nil
}

func (a *testAllocator) Free(addr, size, align uint32) {
}

func TestLowerToStack_Primitives(t *testing.T) {
	compiler := NewCompiler()
	enc := NewEncoderWithCompiler(compiler)

	tests := []struct {
		name     string
		witType  wit.Type
		goType   reflect.Type
		value    any
		expected []uint64
	}{
		{"u32", wit.U32{}, reflect.TypeOf(uint32(0)), uint32(42), []uint64{42}},
		{"s32", wit.S32{}, reflect.TypeOf(int32(0)), int32(-1), []uint64{^uint64(0)}},
		{"u64", wit.U64{}, reflect.TypeOf(uint64(0)), uint64(0xDEADBEEF), []uint64{0xDEADBEEF}},
		{"bool_true", wit.Bool{}, reflect.TypeOf(false), true, []uint64{1}},
		{"bool_false", wit.Bool{}, reflect.TypeOf(false), false, []uint64{0}},
		{"f32", wit.F32{}, reflect.TypeOf(float32(0)), float32(3.14), []uint64{0x4048f5c3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, err := compiler.Compile(tt.witType, tt.goType)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}

			stack := make([]uint64, 16)

			// Create value and get pointer
			val := reflect.New(tt.goType)
			val.Elem().Set(reflect.ValueOf(tt.value))
			ptr := unsafe.Pointer(val.Pointer())

			n, err := enc.LowerToStack(ct, ptr, stack, nil, nil)
			if err != nil {
				t.Fatalf("lower: %v", err)
			}

			if n != len(tt.expected) {
				t.Errorf("consumed %d slots, expected %d", n, len(tt.expected))
			}

			for i, exp := range tt.expected {
				if stack[i] != exp {
					t.Errorf("stack[%d] = %d, expected %d", i, stack[i], exp)
				}
			}
		})
	}
}

func TestLiftFromStack_Primitives(t *testing.T) {
	compiler := NewCompiler()
	dec := NewDecoderWithCompiler(compiler)

	tests := []struct {
		witType  wit.Type
		goType   reflect.Type
		expected any
		name     string
		stack    []uint64
	}{
		{wit.U32{}, reflect.TypeOf(uint32(0)), uint32(42), "u32", []uint64{42}},
		{wit.S32{}, reflect.TypeOf(int32(0)), int32(-1), "s32", []uint64{^uint64(0)}},
		{wit.U64{}, reflect.TypeOf(uint64(0)), uint64(0xDEADBEEF), "u64", []uint64{0xDEADBEEF}},
		{wit.Bool{}, reflect.TypeOf(false), true, "bool_true", []uint64{1}},
		{wit.Bool{}, reflect.TypeOf(false), false, "bool_false", []uint64{0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, err := compiler.Compile(tt.witType, tt.goType)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}

			val := reflect.New(tt.goType)
			ptr := unsafe.Pointer(val.Pointer())

			n, err := dec.LiftFromStack(ct, tt.stack, ptr, nil)
			if err != nil {
				t.Fatalf("lift: %v", err)
			}

			if n != len(tt.stack) {
				t.Errorf("consumed %d slots, expected %d", n, len(tt.stack))
			}

			got := val.Elem().Interface()
			if got != tt.expected {
				t.Errorf("got %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestLowerLift_Record(t *testing.T) {
	type Point struct {
		X uint32 `wit:"x"`
		Y uint32 `wit:"y"`
	}

	witType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiler := NewCompiler()
	ct, err := compiler.Compile(witType, reflect.TypeOf(Point{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)

	// Lower
	input := Point{X: 100, Y: 200}
	stack := make([]uint64, 16)

	n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, nil, nil)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if n != 2 {
		t.Errorf("lower consumed %d slots, expected 2", n)
	}
	if stack[0] != 100 || stack[1] != 200 {
		t.Errorf("stack = [%d, %d], expected [100, 200]", stack[0], stack[1])
	}

	// Lift
	var output Point
	n, err = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), nil)
	if err != nil {
		t.Fatalf("lift: %v", err)
	}
	if n != 2 {
		t.Errorf("lift consumed %d slots, expected 2", n)
	}
	if output != input {
		t.Errorf("got %+v, expected %+v", output, input)
	}
}

func TestLowerLift_String(t *testing.T) {
	compiler := NewCompiler()
	ct, err := compiler.Compile(wit.String{}, reflect.TypeOf(""))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)

	mem := newTestMemory(1024)
	alloc := newTestAllocator(mem.data, 0)

	// Lower
	input := "hello world"
	stack := make([]uint64, 16)

	n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if n != 2 {
		t.Errorf("lower consumed %d slots, expected 2", n)
	}

	addr := uint32(stack[0])
	length := uint32(stack[1])
	if length != 11 {
		t.Errorf("length = %d, expected 11", length)
	}

	// Verify string in memory
	data, _ := mem.Read(addr, length)
	if string(data) != input {
		t.Errorf("memory contains %q, expected %q", string(data), input)
	}

	// Lift
	var output string
	n, err = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("lift: %v", err)
	}
	if n != 2 {
		t.Errorf("lift consumed %d slots, expected 2", n)
	}
	if output != input {
		t.Errorf("got %q, expected %q", output, input)
	}
}

func TestLowerLift_RecordWithString(t *testing.T) {
	type Person struct {
		Name string `wit:"name"`
		Age  uint32 `wit:"age"`
	}

	witType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "name", Type: wit.String{}},
				{Name: "age", Type: wit.U32{}},
			},
		},
	}

	compiler := NewCompiler()
	ct, err := compiler.Compile(witType, reflect.TypeOf(Person{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)

	mem := newTestMemory(1024)
	alloc := newTestAllocator(mem.data, 0)

	// Lower
	input := Person{Name: "Alice", Age: 30}
	stack := make([]uint64, 16)

	n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if n != 3 { // ptr, len, age
		t.Errorf("lower consumed %d slots, expected 3", n)
	}

	// Lift
	var output Person
	n, err = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), mem)
	if err != nil {
		t.Fatalf("lift: %v", err)
	}
	if n != 3 {
		t.Errorf("lift consumed %d slots, expected 3", n)
	}
	if output != input {
		t.Errorf("got %+v, expected %+v", output, input)
	}
}

func TestLowerLift_Option(t *testing.T) {
	witType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}

	compiler := NewCompiler()
	ct, err := compiler.Compile(witType, reflect.TypeOf((*uint32)(nil)))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)

	t.Run("some", func(t *testing.T) {
		val := uint32(42)
		input := &val
		stack := make([]uint64, 16)

		n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, nil, nil)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if n != 2 { // discriminant + value
			t.Errorf("lower consumed %d slots, expected 2", n)
		}
		if stack[0] != 1 || stack[1] != 42 {
			t.Errorf("stack = [%d, %d], expected [1, 42]", stack[0], stack[1])
		}

		var output *uint32
		_, err = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), nil)
		if err != nil {
			t.Fatalf("lift: %v", err)
		}
		if output == nil || *output != 42 {
			t.Errorf("got %v, expected &42", output)
		}
	})

	t.Run("none", func(t *testing.T) {
		var input *uint32
		stack := make([]uint64, 16)

		n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, nil, nil)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if n != 2 {
			t.Errorf("lower consumed %d slots, expected 2", n)
		}
		if stack[0] != 0 {
			t.Errorf("discriminant = %d, expected 0", stack[0])
		}

		var output *uint32
		_, err = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), nil)
		if err != nil {
			t.Fatalf("lift: %v", err)
		}
		if output != nil {
			t.Errorf("got %v, expected nil", output)
		}
	})
}

func TestLowerLift_Variant(t *testing.T) {
	// Variant with two cases: case1 has u32, case2 has string
	type MyVariant struct {
		Num *uint32 `wit:"num"`
		Str *string `wit:"str"`
	}

	witType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "num", Type: wit.U32{}},
				{Name: "str", Type: wit.String{}},
			},
		},
	}

	compiler := NewCompiler()
	ct, err := compiler.Compile(witType, reflect.TypeOf(MyVariant{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)

	mem := newTestMemory(1024)
	alloc := newTestAllocator(mem.data, 0)

	t.Run("case0_num", func(t *testing.T) {
		val := uint32(42)
		input := MyVariant{Num: &val}
		stack := make([]uint64, 16)

		n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if stack[0] != 0 { // discriminant for "num"
			t.Errorf("discriminant = %d, expected 0", stack[0])
		}
		if stack[1] != 42 {
			t.Errorf("payload = %d, expected 42", stack[1])
		}

		var output MyVariant
		n2, err := dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("lift: %v", err)
		}
		if n != n2 {
			t.Errorf("lower consumed %d, lift consumed %d", n, n2)
		}
		if output.Num == nil || *output.Num != 42 {
			t.Errorf("output.Num = %v, expected &42", output.Num)
		}
		if output.Str != nil {
			t.Errorf("output.Str should be nil")
		}
	})

	t.Run("case1_str", func(t *testing.T) {
		alloc.offset = 0 // reset allocator
		val := "hello"
		input := MyVariant{Str: &val}
		stack := make([]uint64, 16)

		n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if stack[0] != 1 { // discriminant for "str"
			t.Errorf("discriminant = %d, expected 1", stack[0])
		}

		var output MyVariant
		_, err = dec.LiftFromStack(ct, stack[:n], unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("lift: %v", err)
		}
		if output.Str == nil || *output.Str != "hello" {
			t.Errorf("output.Str = %v, expected &hello", output.Str)
		}
		if output.Num != nil {
			t.Errorf("output.Num should be nil")
		}
	})
}

func TestLowerLift_Result(t *testing.T) {
	// Result with Ok=u32, Err=string
	type MyResult struct {
		Ok  *uint32
		Err *string
	}

	witType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	compiler := NewCompiler()
	ct, err := compiler.Compile(witType, reflect.TypeOf(MyResult{}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	enc := NewEncoderWithCompiler(compiler)
	dec := NewDecoderWithCompiler(compiler)

	mem := newTestMemory(1024)
	alloc := newTestAllocator(mem.data, 0)

	t.Run("ok", func(t *testing.T) {
		val := uint32(42)
		input := MyResult{Ok: &val}
		stack := make([]uint64, 16)

		n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if stack[0] != 0 { // Ok discriminant
			t.Errorf("discriminant = %d, expected 0", stack[0])
		}

		var output MyResult
		_, err = dec.LiftFromStack(ct, stack[:n], unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("lift: %v", err)
		}
		if output.Ok == nil || *output.Ok != 42 {
			t.Errorf("output.Ok = %v, expected &42", output.Ok)
		}
	})

	t.Run("err", func(t *testing.T) {
		alloc.offset = 0
		val := "error message"
		input := MyResult{Err: &val}
		stack := make([]uint64, 16)

		n, err := enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
		if err != nil {
			t.Fatalf("lower: %v", err)
		}
		if stack[0] != 1 { // Err discriminant
			t.Errorf("discriminant = %d, expected 1", stack[0])
		}

		var output MyResult
		_, err = dec.LiftFromStack(ct, stack[:n], unsafe.Pointer(&output), mem)
		if err != nil {
			t.Fatalf("lift: %v", err)
		}
		if output.Err == nil || *output.Err != "error message" {
			t.Errorf("output.Err = %v, expected &error message", output.Err)
		}
	})
}

// Benchmarks

func BenchmarkLowerToStack_U32(b *testing.B) {
	compiler := NewCompiler()
	ct, _ := compiler.Compile(wit.U32{}, reflect.TypeOf(uint32(0)))
	enc := NewEncoderWithCompiler(compiler)

	stack := make([]uint64, 16)
	input := uint32(42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enc.LowerToStack(ct, unsafe.Pointer(&input), stack, nil, nil)
	}
}

func BenchmarkLiftFromStack_U32(b *testing.B) {
	compiler := NewCompiler()
	ct, _ := compiler.Compile(wit.U32{}, reflect.TypeOf(uint32(0)))
	dec := NewDecoderWithCompiler(compiler)

	stack := []uint64{42}
	var output uint32

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), nil)
	}
}

func BenchmarkLowerToStack_Record(b *testing.B) {
	type Point struct {
		X uint32 `wit:"x"`
		Y uint32 `wit:"y"`
	}

	witType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiler := NewCompiler()
	ct, _ := compiler.Compile(witType, reflect.TypeOf(Point{}))
	enc := NewEncoderWithCompiler(compiler)

	stack := make([]uint64, 16)
	input := Point{X: 100, Y: 200}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enc.LowerToStack(ct, unsafe.Pointer(&input), stack, nil, nil)
	}
}

func BenchmarkLiftFromStack_Record(b *testing.B) {
	type Point struct {
		X uint32 `wit:"x"`
		Y uint32 `wit:"y"`
	}

	witType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.U32{}},
				{Name: "y", Type: wit.U32{}},
			},
		},
	}

	compiler := NewCompiler()
	ct, _ := compiler.Compile(witType, reflect.TypeOf(Point{}))
	dec := NewDecoderWithCompiler(compiler)

	stack := []uint64{100, 200}
	var output Point

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), nil)
	}
}

func BenchmarkLowerToStack_String(b *testing.B) {
	compiler := NewCompiler()
	ct, _ := compiler.Compile(wit.String{}, reflect.TypeOf(""))
	enc := NewEncoderWithCompiler(compiler)

	mem := newTestMemory(1024 * 1024)
	alloc := newTestAllocator(mem.data, 0)

	stack := make([]uint64, 16)
	input := "hello world"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.offset = 0 // reset allocator
		_, _ = enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
	}
}

func BenchmarkLiftFromStack_String(b *testing.B) {
	compiler := NewCompiler()
	ct, _ := compiler.Compile(wit.String{}, reflect.TypeOf(""))
	dec := NewDecoderWithCompiler(compiler)

	mem := newTestMemory(1024)
	copy(mem.data[0:11], "hello world")

	stack := []uint64{0, 11}
	var output string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = dec.LiftFromStack(ct, stack, unsafe.Pointer(&output), mem)
	}
}

func BenchmarkLowerToStack_RecordWithString(b *testing.B) {
	type Person struct {
		Name string `wit:"name"`
		Age  uint32 `wit:"age"`
	}

	witType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "name", Type: wit.String{}},
				{Name: "age", Type: wit.U32{}},
			},
		},
	}

	compiler := NewCompiler()
	ct, _ := compiler.Compile(witType, reflect.TypeOf(Person{}))
	enc := NewEncoderWithCompiler(compiler)

	mem := newTestMemory(1024 * 1024)
	alloc := newTestAllocator(mem.data, 0)

	stack := make([]uint64, 16)
	input := Person{Name: "Alice", Age: 30}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alloc.offset = 0
		_, _ = enc.LowerToStack(ct, unsafe.Pointer(&input), stack, mem, alloc)
	}
}
