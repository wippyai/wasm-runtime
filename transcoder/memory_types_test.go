package transcoder

import (
	"reflect"
	"testing"
	"unsafe"

	"go.bytecodealliance.org/wit"
)

func TestEncodeDecode_Result_Ok(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	input := map[string]any{"ok": uint32(42)}

	flat, err := enc.EncodeParams([]wit.Type{resultType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].(map[string]any)
	if result["ok"] != uint32(42) {
		t.Errorf("ok value: got %v, want 42", result["ok"])
	}
}

func TestEncodeDecode_Result_Err(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	input := map[string]any{"err": "error message"}

	flat, err := enc.EncodeParams([]wit.Type{resultType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{resultType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].(map[string]any)
	if result["err"] != "error message" {
		t.Errorf("err value: got %v, want 'error message'", result["err"])
	}
}

func TestEncodeDecode_Variant(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "num", Type: wit.U32{}},
				{Name: "str", Type: wit.String{}},
				{Name: "none", Type: nil},
			},
		},
	}

	tests := []struct {
		input map[string]any
		name  string
	}{
		{map[string]any{"num": uint32(42)}, "case0_num"},
		{map[string]any{"str": "hello"}, "case1_str"},
		{map[string]any{"none": nil}, "case2_none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flat, err := enc.EncodeParams([]wit.Type{variantType}, []any{tt.input}, mem, alloc, allocList)
			if err != nil {
				t.Fatalf("EncodeParams failed: %v", err)
			}

			results, err := dec.DecodeResults([]wit.Type{variantType}, flat, mem)
			if err != nil {
				t.Fatalf("DecodeResults failed: %v", err)
			}

			result := results[0].(map[string]any)
			for k, v := range tt.input {
				if result[k] != v {
					t.Errorf("case %s: got %v, want %v", k, result[k], v)
				}
			}
		})
	}
}

func TestEncodeDecode_Enum(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{
			Cases: []wit.EnumCase{
				{Name: "red"},
				{Name: "green"},
				{Name: "blue"},
			},
		},
	}

	tests := []struct {
		name  string
		index uint32
	}{
		{"red", 0},
		{"green", 1},
		{"blue", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flat, err := enc.EncodeParams([]wit.Type{enumType}, []any{tt.index}, mem, alloc, allocList)
			if err != nil {
				t.Fatalf("EncodeParams failed: %v", err)
			}

			results, err := dec.DecodeResults([]wit.Type{enumType}, flat, mem)
			if err != nil {
				t.Fatalf("DecodeResults failed: %v", err)
			}

			// Dynamic path returns uint32 discriminant
			result := results[0].(uint32)
			if result != tt.index {
				t.Errorf("got %d, want %d", result, tt.index)
			}
		})
	}
}

func TestEncodeDecode_Flags(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{
			Flags: []wit.Flag{
				{Name: "read"},
				{Name: "write"},
				{Name: "execute"},
			},
		},
	}

	// read=bit0, execute=bit2 -> 0b101 = 5
	input := uint64(5)

	flat, err := enc.EncodeParams([]wit.Type{flagsType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{flagsType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	// Dynamic path returns uint64 bitfield
	result := results[0].(uint64)
	if result != input {
		t.Errorf("got %d, want %d", result, input)
	}
}

func TestEncodeDecode_Tuple(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)
	allocList := NewAllocationList()

	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{
			Types: []wit.Type{
				wit.U32{},
				wit.String{},
				wit.Bool{},
			},
		},
	}

	input := []any{uint32(42), "hello", true}

	flat, err := enc.EncodeParams([]wit.Type{tupleType}, []any{input}, mem, alloc, allocList)
	if err != nil {
		t.Fatalf("EncodeParams failed: %v", err)
	}

	results, err := dec.DecodeResults([]wit.Type{tupleType}, flat, mem)
	if err != nil {
		t.Fatalf("DecodeResults failed: %v", err)
	}

	result := results[0].([]any)
	if result[0] != uint32(42) {
		t.Errorf("elem 0: got %v, want 42", result[0])
	}
	if result[1] != "hello" {
		t.Errorf("elem 1: got %v, want 'hello'", result[1])
	}
	if result[2] != true {
		t.Errorf("elem 2: got %v, want true", result[2])
	}
}

func TestUnitPtr_Stability(t *testing.T) {
	ptr1 := UnitPtr()
	ptr2 := UnitPtr()

	if ptr1 != ptr2 {
		t.Error("UnitPtr should return the same pointer")
	}

	if ptr1 == nil {
		t.Error("UnitPtr should not be nil")
	}
}

func TestCompiledPath_Result(t *testing.T) {
	type MyResult struct {
		Ok  *uint32
		Err *string
	}

	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	t.Run("ok", func(t *testing.T) {
		val := uint32(42)
		input := MyResult{Ok: &val, Err: nil}

		compiled, err := enc.compiler.Compile(resultType, reflect.TypeOf(input))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		stack := make([]uint64, compiled.FlatCount)
		ptr := unsafe.Pointer(&input)
		consumed, err := enc.LowerToStack(compiled, ptr, stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var output MyResult
		outPtr := unsafe.Pointer(&output)
		_, err = dec.LiftFromStack(compiled, stack[:consumed], outPtr, mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}

		if output.Ok == nil || *output.Ok != 42 {
			t.Errorf("Ok: got %v, want 42", output.Ok)
		}
		if output.Err != nil {
			t.Errorf("Err should be nil")
		}
	})

	t.Run("err", func(t *testing.T) {
		errMsg := "error"
		input := MyResult{Ok: nil, Err: &errMsg}

		compiled, err := enc.compiler.Compile(resultType, reflect.TypeOf(input))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		stack := make([]uint64, compiled.FlatCount)
		ptr := unsafe.Pointer(&input)
		consumed, err := enc.LowerToStack(compiled, ptr, stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var output MyResult
		outPtr := unsafe.Pointer(&output)
		_, err = dec.LiftFromStack(compiled, stack[:consumed], outPtr, mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}

		if output.Ok != nil {
			t.Errorf("Ok should be nil")
		}
		if output.Err == nil || *output.Err != "error" {
			t.Errorf("Err: got %v, want 'error'", output.Err)
		}
	})
}

func TestCompiledPath_Variant(t *testing.T) {
	type MyVariant struct {
		Num *uint32
		Str *string
	}

	enc := NewEncoder()
	dec := NewDecoder()
	mem := newMockMemory(4096)
	alloc := newMockAllocator(mem)

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "num", Type: wit.U32{}},
				{Name: "str", Type: wit.String{}},
			},
		},
	}

	t.Run("case0_num", func(t *testing.T) {
		val := uint32(99)
		input := MyVariant{Num: &val, Str: nil}

		compiled, err := enc.compiler.Compile(variantType, reflect.TypeOf(input))
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		stack := make([]uint64, compiled.FlatCount)
		ptr := unsafe.Pointer(&input)
		consumed, err := enc.LowerToStack(compiled, ptr, stack, mem, alloc)
		if err != nil {
			t.Fatalf("LowerToStack failed: %v", err)
		}

		var output MyVariant
		outPtr := unsafe.Pointer(&output)
		_, err = dec.LiftFromStack(compiled, stack[:consumed], outPtr, mem)
		if err != nil {
			t.Fatalf("LiftFromStack failed: %v", err)
		}

		if output.Num == nil || *output.Num != 99 {
			t.Errorf("Num: got %v, want 99", output.Num)
		}
		if output.Str != nil {
			t.Errorf("Str should be nil")
		}
	})
}

func TestLoadValue_Result(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	resultType := &wit.TypeDef{
		Kind: &wit.Result{
			OK:  wit.U32{},
			Err: wit.String{},
		},
	}

	t.Run("ok_value", func(t *testing.T) {
		addr := uint32(100)
		mem.WriteU8(addr, 0)
		mem.WriteU32(addr+4, 42)

		result, err := dec.LoadValue(resultType, addr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		m := result.(map[string]any)
		if m["ok"] != uint32(42) {
			t.Errorf("ok value: got %v, want 42", m["ok"])
		}
	})

	t.Run("err_value", func(t *testing.T) {
		addr := uint32(200)
		mem.WriteU8(addr, 1)
		strAddr := uint32(300)
		mem.Write(strAddr, []byte("error"))
		mem.WriteU32(addr+4, strAddr)
		mem.WriteU32(addr+8, 5)

		result, err := dec.LoadValue(resultType, addr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		m := result.(map[string]any)
		if m["err"] != "error" {
			t.Errorf("err value: got %v, want 'error'", m["err"])
		}
	})
}

func TestLoadValue_Variant(t *testing.T) {
	dec := NewDecoder()
	mem := newMockMemory(4096)

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.String{}},
			},
		},
	}

	t.Run("case_a", func(t *testing.T) {
		addr := uint32(100)
		mem.WriteU8(addr, 0)
		mem.WriteU32(addr+4, 123)

		result, err := dec.LoadValue(variantType, addr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		m := result.(map[string]any)
		if m["a"] != uint32(123) {
			t.Errorf("variant a: got %v, want 123", m["a"])
		}
	})

	t.Run("case_b", func(t *testing.T) {
		addr := uint32(200)
		mem.WriteU8(addr, 1)
		strAddr := uint32(300)
		mem.Write(strAddr, []byte("hello"))
		mem.WriteU32(addr+4, strAddr)
		mem.WriteU32(addr+8, 5)

		result, err := dec.LoadValue(variantType, addr, mem)
		if err != nil {
			t.Fatalf("LoadValue failed: %v", err)
		}

		m := result.(map[string]any)
		if m["b"] != "hello" {
			t.Errorf("variant b: got %v, want 'hello'", m["b"])
		}
	})
}
