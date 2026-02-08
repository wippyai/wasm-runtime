package encoder

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
)

func TestBufferAppendByte(t *testing.T) {
	b := &Buffer{}
	b.AppendByte(0x42)
	if len(b.Bytes) != 1 || b.Bytes[0] != 0x42 {
		t.Errorf("AppendByte failed: got %v", b.Bytes)
	}
}

func TestBufferWriteBytes(t *testing.T) {
	b := &Buffer{}
	b.WriteBytes([]byte{0x01, 0x02, 0x03})
	if !bytes.Equal(b.Bytes, []byte{0x01, 0x02, 0x03}) {
		t.Errorf("WriteBytes failed: got %v", b.Bytes)
	}
}

func TestBufferWriteU32(t *testing.T) {
	tests := []struct {
		want []byte
		val  uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7F}, 127},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xFF, 0x01}, 255},
		{[]byte{0x80, 0x02}, 256},
		{[]byte{0xFF, 0x7F}, 16383},
		{[]byte{0x80, 0x80, 0x01}, 16384},
		{[]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F}, 0xFFFFFFFF},
	}

	for _, tt := range tests {
		b := &Buffer{}
		b.WriteU32(tt.val)
		if !bytes.Equal(b.Bytes, tt.want) {
			t.Errorf("WriteU32(%d) = %v, want %v", tt.val, b.Bytes, tt.want)
		}
	}
}

func TestBufferWriteI32(t *testing.T) {
	tests := []struct {
		want []byte
		val  int32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7F}, -1},
		{[]byte{0x3F}, 63},
		{[]byte{0xC0, 0x00}, 64},
		{[]byte{0x40}, -64},
		{[]byte{0xBF, 0x7F}, -65},
		{[]byte{0xFF, 0x00}, 127},
		{[]byte{0x80, 0x7F}, -128},
		{[]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x07}, 2147483647},
		{[]byte{0x80, 0x80, 0x80, 0x80, 0x78}, -2147483648},
	}

	for _, tt := range tests {
		b := &Buffer{}
		b.WriteI32(tt.val)
		if !bytes.Equal(b.Bytes, tt.want) {
			t.Errorf("WriteI32(%d) = %v, want %v", tt.val, b.Bytes, tt.want)
		}
	}
}

func TestBufferWriteI64(t *testing.T) {
	tests := []struct {
		want []byte
		val  int64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7F}, -1},
		{[]byte{0xFF, 0x00}, 127},
		{[]byte{0x80, 0x7F}, -128},
	}

	for _, tt := range tests {
		b := &Buffer{}
		b.WriteI64(tt.val)
		if !bytes.Equal(b.Bytes, tt.want) {
			t.Errorf("WriteI64(%d) = %v, want %v", tt.val, b.Bytes, tt.want)
		}
	}
}

func TestBufferWriteF32(t *testing.T) {
	b := &Buffer{}
	b.WriteF32(1.0)
	if len(b.Bytes) != 4 {
		t.Errorf("WriteF32 should write 4 bytes, got %d", len(b.Bytes))
	}
	// 1.0 as IEEE 754 float32 little-endian
	want := []byte{0x00, 0x00, 0x80, 0x3F}
	if !bytes.Equal(b.Bytes, want) {
		t.Errorf("WriteF32(1.0) = %v, want %v", b.Bytes, want)
	}
}

func TestBufferWriteF64(t *testing.T) {
	b := &Buffer{}
	b.WriteF64(1.0)
	if len(b.Bytes) != 8 {
		t.Errorf("WriteF64 should write 8 bytes, got %d", len(b.Bytes))
	}
	// 1.0 as IEEE 754 float64 little-endian
	want := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F}
	if !bytes.Equal(b.Bytes, want) {
		t.Errorf("WriteF64(1.0) = %v, want %v", b.Bytes, want)
	}
}

func TestBufferWriteString(t *testing.T) {
	b := &Buffer{}
	b.WriteString("hello")
	want := []byte{0x05, 'h', 'e', 'l', 'l', 'o'}
	if !bytes.Equal(b.Bytes, want) {
		t.Errorf("WriteString = %v, want %v", b.Bytes, want)
	}
}

func TestBufferWriteLimits(t *testing.T) {
	tests := []struct {
		max  *uint32
		name string
		want []byte
		min  uint32
	}{
		{nil, "no_max", []byte{0x00, 0x01}, 1},
		{ptr(10), "with_max", []byte{0x01, 0x01, 0x0A}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Buffer{}
			b.WriteLimits(tt.min, tt.max)
			if !bytes.Equal(b.Bytes, tt.want) {
				t.Errorf("WriteLimits = %v, want %v", b.Bytes, tt.want)
			}
		})
	}
}

func ptr(v uint32) *uint32 { return &v }

func TestEncodeEmptyModule(t *testing.T) {
	m := &ast.Module{}
	wasm := Encode(m)

	// Magic + version
	want := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(wasm, want) {
		t.Errorf("Encode empty module = %v, want %v", wasm, want)
	}
}

func TestEncodeModuleWithType(t *testing.T) {
	m := &ast.Module{
		Types: []ast.FuncType{
			{Params: []ast.ValType{ast.ValTypeI32}, Results: []ast.ValType{ast.ValTypeI32}},
		},
	}
	wasm := Encode(m)

	// Should start with magic + version
	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
	if !bytes.Equal(wasm[:8], []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}) {
		t.Error("invalid magic/version")
	}

	// Section ID 1 (type section) should follow
	if wasm[8] != 0x01 {
		t.Errorf("expected type section ID (0x01), got 0x%02X", wasm[8])
	}
}

func TestEncodeInstr(t *testing.T) {
	tests := []struct {
		name  string
		instr ast.Instr
		want  []byte
	}{
		{
			"nop",
			ast.Instr{Opcode: ast.OpNop},
			[]byte{0x01},
		},
		{
			"unreachable",
			ast.Instr{Opcode: ast.OpUnreachable},
			[]byte{0x00},
		},
		{
			"i32.const_0",
			ast.Instr{Opcode: ast.OpI32Const, Imm: int32(0)},
			[]byte{0x41, 0x00},
		},
		{
			"i32.const_42",
			ast.Instr{Opcode: ast.OpI32Const, Imm: int32(42)},
			[]byte{0x41, 0x2A},
		},
		{
			"i32.const_neg1",
			ast.Instr{Opcode: ast.OpI32Const, Imm: int32(-1)},
			[]byte{0x41, 0x7F},
		},
		{
			"i64.const",
			ast.Instr{Opcode: ast.OpI64Const, Imm: int64(100)},
			[]byte{0x42, 0xE4, 0x00}, // 100 in signed LEB128
		},
		{
			"local.get_0",
			ast.Instr{Opcode: ast.OpLocalGet, Imm: uint32(0)},
			[]byte{0x20, 0x00},
		},
		{
			"br_0",
			ast.Instr{Opcode: ast.OpBr, Imm: uint32(0)},
			[]byte{0x0C, 0x00},
		},
		{
			"end",
			ast.Instr{Opcode: ast.OpEnd},
			[]byte{0x0B},
		},
		{
			"drop",
			ast.Instr{Opcode: ast.OpDrop},
			[]byte{0x1A},
		},
		{
			"select",
			ast.Instr{Opcode: ast.OpSelect},
			[]byte{0x1B},
		},
		{
			"ref.null_func",
			ast.Instr{Opcode: ast.OpRefNull, Imm: ast.RefTypeFuncref},
			[]byte{0xD0, 0x70},
		},
		{
			"ref.null_extern",
			ast.Instr{Opcode: ast.OpRefNull, Imm: ast.RefTypeExternref},
			[]byte{0xD0, 0x6F},
		},
		{
			"ref.func",
			ast.Instr{Opcode: ast.OpRefFunc, Imm: uint32(0)},
			[]byte{0xD2, 0x00},
		},
		{
			"memory.size_nil",
			ast.Instr{Opcode: ast.OpMemorySize, Imm: nil},
			[]byte{0x3F, 0x00},
		},
		{
			"memory.size_0",
			ast.Instr{Opcode: ast.OpMemorySize, Imm: uint32(0)},
			[]byte{0x3F, 0x00},
		},
		{
			"memory.grow_nil",
			ast.Instr{Opcode: ast.OpMemoryGrow, Imm: nil},
			[]byte{0x40, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Buffer{}
			EncodeInstr(b, tt.instr)
			if !bytes.Equal(b.Bytes, tt.want) {
				t.Errorf("EncodeInstr = %v, want %v", b.Bytes, tt.want)
			}
		})
	}
}

func TestEncodeBlockType(t *testing.T) {
	tests := []struct {
		name string
		want []byte
		bt   ast.BlockType
	}{
		{
			"empty",
			[]byte{0x02, 0x40},
			ast.BlockType{Simple: ast.BlockTypeEmpty, TypeIdx: -1},
		},
		{
			"i32_result",
			[]byte{0x02, 0x7F},
			ast.BlockType{Simple: byte(ast.ValTypeI32), TypeIdx: -1},
		},
		{
			"type_index_0",
			[]byte{0x02, 0x00},
			ast.BlockType{TypeIdx: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: ast.OpBlock, Imm: tt.bt})
			if !bytes.Equal(b.Bytes, tt.want) {
				t.Errorf("Encode block = %v, want %v", b.Bytes, tt.want)
			}
		})
	}
}

func TestEncodeMemarg(t *testing.T) {
	tests := []struct {
		name string
		want []byte
		ma   ast.Memarg
	}{
		{
			"align0_offset0",
			[]byte{0x28, 0x00, 0x00},
			ast.Memarg{Align: 0, Offset: 0},
		},
		{
			"align2_offset4",
			[]byte{0x28, 0x02, 0x04},
			ast.Memarg{Align: 2, Offset: 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: ast.OpI32Load, Imm: tt.ma})
			if !bytes.Equal(b.Bytes, tt.want) {
				t.Errorf("Encode i32.load = %v, want %v", b.Bytes, tt.want)
			}
		})
	}
}

func TestEncodeBrTable(t *testing.T) {
	b := &Buffer{}
	EncodeInstr(b, ast.Instr{
		Opcode: ast.OpBrTable,
		Imm:    []uint32{0, 1, 2, 0},
	})
	// br_table with 3 labels + default
	want := []byte{0x0E, 0x03, 0x00, 0x01, 0x02, 0x00}
	if !bytes.Equal(b.Bytes, want) {
		t.Errorf("Encode br_table = %v, want %v", b.Bytes, want)
	}
}

func TestEncodeCallIndirect(t *testing.T) {
	b := &Buffer{}
	EncodeInstr(b, ast.Instr{
		Opcode: ast.OpCallIndirect,
		Imm:    []uint32{0, 0}, // type 0, table 0
	})
	want := []byte{0x11, 0x00, 0x00}
	if !bytes.Equal(b.Bytes, want) {
		t.Errorf("Encode call_indirect = %v, want %v", b.Bytes, want)
	}
}

func TestEncodeSelectTyped(t *testing.T) {
	b := &Buffer{}
	EncodeInstr(b, ast.Instr{
		Opcode: ast.OpSelectTyped,
		Imm:    []ast.ValType{ast.ValTypeI32},
	})
	want := []byte{0x1C, 0x01, 0x7F}
	if !bytes.Equal(b.Bytes, want) {
		t.Errorf("Encode select typed = %v, want %v", b.Bytes, want)
	}
}

// Extended encoder tests covering additional module sections and instruction types

func TestEncodeModule_Imports(t *testing.T) {
	max := uint32(10)
	m := &ast.Module{
		Types: []ast.FuncType{
			{Params: []ast.ValType{ast.ValTypeI32}, Results: []ast.ValType{}},
		},
		Imports: []ast.Import{
			{
				Module: "env",
				Name:   "log",
				Desc:   ast.ImportDesc{Kind: ast.KindFunc, TypeIdx: 0},
			},
			{
				Module: "env",
				Name:   "memory",
				Desc:   ast.ImportDesc{Kind: ast.KindMemory, MemLimits: &ast.Limits{Min: 1, Max: &max}},
			},
			{
				Module: "env",
				Name:   "table",
				Desc:   ast.ImportDesc{Kind: ast.KindTable, TableTyp: &ast.Table{Limits: ast.Limits{Min: 1}, ElemType: ast.RefTypeFuncref}},
			},
			{
				Module: "env",
				Name:   "global",
				Desc:   ast.ImportDesc{Kind: ast.KindGlobal, GlobalTyp: &ast.GlobalType{ValType: ast.ValTypeI32, Mutable: false}},
			},
		},
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}

	// Check magic and version
	if !bytes.Equal(wasm[:8], []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}) {
		t.Error("invalid magic/version")
	}
}

func TestEncodeModule_Functions(t *testing.T) {
	m := &ast.Module{
		Types: []ast.FuncType{
			{Params: []ast.ValType{ast.ValTypeI32, ast.ValTypeI32}, Results: []ast.ValType{ast.ValTypeI32}},
		},
		Funcs: []ast.FuncEntry{
			{TypeIdx: 0},
		},
		Code: []ast.FuncBody{
			{
				Locals: nil,
				Code: []ast.Instr{
					{Opcode: ast.OpLocalGet, Imm: uint32(0)},
					{Opcode: ast.OpLocalGet, Imm: uint32(1)},
					{Opcode: 0x6A}, // i32.add
					{Opcode: ast.OpEnd},
				},
			},
		},
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
}

func TestEncodeModule_Tables(t *testing.T) {
	max := uint32(20)
	m := &ast.Module{
		Tables: []ast.Table{
			{
				ElemType: ast.RefTypeFuncref,
				Limits:   ast.Limits{Min: 10, Max: &max},
			},
		},
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
}

func TestEncodeModule_Memories(t *testing.T) {
	max := uint32(100)
	m := &ast.Module{
		Memories: []ast.Memory{
			{Limits: ast.Limits{Min: 1, Max: &max}},
		},
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
}

func TestEncodeModule_Globals(t *testing.T) {
	m := &ast.Module{
		Globals: []ast.Global{
			{
				Type: ast.GlobalType{ValType: ast.ValTypeI32, Mutable: true},
				Init: []ast.Instr{
					{Opcode: ast.OpI32Const, Imm: int32(42)},
					{Opcode: ast.OpEnd},
				},
			},
			{
				Type: ast.GlobalType{ValType: ast.ValTypeF64, Mutable: false},
				Init: []ast.Instr{
					{Opcode: ast.OpF64Const, Imm: float64(3.14)},
					{Opcode: ast.OpEnd},
				},
			},
		},
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
}

func TestEncodeModule_Exports(t *testing.T) {
	m := &ast.Module{
		Types: []ast.FuncType{
			{Params: nil, Results: []ast.ValType{ast.ValTypeI32}},
		},
		Funcs: []ast.FuncEntry{{TypeIdx: 0}},
		Code: []ast.FuncBody{
			{Code: []ast.Instr{{Opcode: ast.OpI32Const, Imm: int32(0)}, {Opcode: ast.OpEnd}}},
		},
		Memories: []ast.Memory{{Limits: ast.Limits{Min: 1}}},
		Tables:   []ast.Table{{ElemType: ast.RefTypeFuncref, Limits: ast.Limits{Min: 1}}},
		Globals: []ast.Global{
			{Type: ast.GlobalType{ValType: ast.ValTypeI32}, Init: []ast.Instr{{Opcode: ast.OpI32Const, Imm: int32(0)}, {Opcode: ast.OpEnd}}},
		},
		Exports: []ast.Export{
			{Name: "myFunc", Kind: ast.KindFunc, Idx: 0},
			{Name: "memory", Kind: ast.KindMemory, Idx: 0},
			{Name: "table", Kind: ast.KindTable, Idx: 0},
			{Name: "global", Kind: ast.KindGlobal, Idx: 0},
		},
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
}

func TestEncodeModule_Start(t *testing.T) {
	start := uint32(0)
	m := &ast.Module{
		Types: []ast.FuncType{{Params: nil, Results: nil}},
		Funcs: []ast.FuncEntry{{TypeIdx: 0}},
		Code:  []ast.FuncBody{{Code: []ast.Instr{{Opcode: ast.OpEnd}}}},
		Start: &start,
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
}

func TestEncodeModule_Elements(t *testing.T) {
	t.Run("ActiveElements", func(t *testing.T) {
		m := &ast.Module{
			Types:  []ast.FuncType{{Params: nil, Results: nil}},
			Funcs:  []ast.FuncEntry{{TypeIdx: 0}},
			Code:   []ast.FuncBody{{Code: []ast.Instr{{Opcode: ast.OpEnd}}}},
			Tables: []ast.Table{{ElemType: ast.RefTypeFuncref, Limits: ast.Limits{Min: 10}}},
			Elems: []ast.Elem{
				{
					Mode:     ast.ElemModeActive,
					TableIdx: 0,
					Offset:   []ast.Instr{{Opcode: ast.OpI32Const, Imm: int32(0)}, {Opcode: ast.OpEnd}},
					Init:     []uint32{0},
					RefType:  ast.RefTypeFuncref,
				},
			},
		}
		wasm := Encode(m)

		if len(wasm) < 8 {
			t.Fatal("wasm too short")
		}
	})

	t.Run("PassiveElements", func(t *testing.T) {
		m := &ast.Module{
			Types:  []ast.FuncType{{Params: nil, Results: nil}},
			Funcs:  []ast.FuncEntry{{TypeIdx: 0}},
			Code:   []ast.FuncBody{{Code: []ast.Instr{{Opcode: ast.OpEnd}}}},
			Tables: []ast.Table{{ElemType: ast.RefTypeFuncref, Limits: ast.Limits{Min: 10}}},
			Elems: []ast.Elem{
				{
					Mode:    ast.ElemModePassive,
					Init:    []uint32{0},
					RefType: ast.RefTypeFuncref,
				},
			},
		}
		wasm := Encode(m)

		if len(wasm) < 8 {
			t.Fatal("wasm too short")
		}
	})

	t.Run("DeclarativeElements", func(t *testing.T) {
		m := &ast.Module{
			Types:  []ast.FuncType{{Params: nil, Results: nil}},
			Funcs:  []ast.FuncEntry{{TypeIdx: 0}},
			Code:   []ast.FuncBody{{Code: []ast.Instr{{Opcode: ast.OpEnd}}}},
			Tables: []ast.Table{{ElemType: ast.RefTypeFuncref, Limits: ast.Limits{Min: 10}}},
			Elems: []ast.Elem{
				{
					Mode:    ast.ElemModeDeclarative,
					Init:    []uint32{0},
					RefType: ast.RefTypeFuncref,
				},
			},
		}
		wasm := Encode(m)

		if len(wasm) < 8 {
			t.Fatal("wasm too short")
		}
	})

	t.Run("ExprInit", func(t *testing.T) {
		m := &ast.Module{
			Types:  []ast.FuncType{{Params: nil, Results: nil}},
			Funcs:  []ast.FuncEntry{{TypeIdx: 0}},
			Code:   []ast.FuncBody{{Code: []ast.Instr{{Opcode: ast.OpEnd}}}},
			Tables: []ast.Table{{ElemType: ast.RefTypeFuncref, Limits: ast.Limits{Min: 10}}},
			Elems: []ast.Elem{
				{
					Mode:    ast.ElemModePassive,
					RefType: ast.RefTypeFuncref,
					Exprs: [][]ast.Instr{
						{{Opcode: ast.OpRefFunc, Imm: uint32(0)}, {Opcode: ast.OpEnd}},
					},
				},
			},
		}
		wasm := Encode(m)

		if len(wasm) < 8 {
			t.Fatal("wasm too short")
		}
	})
}

func TestEncodeModule_Data(t *testing.T) {
	t.Run("ActiveData", func(t *testing.T) {
		m := &ast.Module{
			Memories: []ast.Memory{{Limits: ast.Limits{Min: 1}}},
			Data: []ast.DataSegment{
				{
					Passive: false,
					MemIdx:  0,
					Offset:  []ast.Instr{{Opcode: ast.OpI32Const, Imm: int32(0)}, {Opcode: ast.OpEnd}},
					Init:    []byte("hello"),
				},
			},
		}
		wasm := Encode(m)

		if len(wasm) < 8 {
			t.Fatal("wasm too short")
		}
	})

	t.Run("PassiveData", func(t *testing.T) {
		m := &ast.Module{
			Types:    []ast.FuncType{{Params: nil, Results: nil}},
			Funcs:    []ast.FuncEntry{{TypeIdx: 0}},
			Code:     []ast.FuncBody{{Code: []ast.Instr{{Opcode: ast.OpEnd}}}},
			Memories: []ast.Memory{{Limits: ast.Limits{Min: 1}}},
			Data: []ast.DataSegment{
				{
					Passive: true,
					Init:    []byte("hello"),
				},
			},
		}
		wasm := Encode(m)

		if len(wasm) < 8 {
			t.Fatal("wasm too short")
		}
	})
}

func TestEncodeModule_Locals(t *testing.T) {
	m := &ast.Module{
		Types: []ast.FuncType{
			{Params: nil, Results: []ast.ValType{ast.ValTypeI32}},
		},
		Funcs: []ast.FuncEntry{{TypeIdx: 0}},
		Code: []ast.FuncBody{
			{
				Locals: []ast.ValType{
					ast.ValTypeI32, ast.ValTypeI32, ast.ValTypeI32,
					ast.ValTypeF64, ast.ValTypeF64,
				},
				Code: []ast.Instr{
					{Opcode: ast.OpLocalGet, Imm: uint32(0)},
					{Opcode: ast.OpEnd},
				},
			},
		},
	}
	wasm := Encode(m)

	if len(wasm) < 8 {
		t.Fatal("wasm too short")
	}
}

func TestEncodeInstr_RefOps(t *testing.T) {
	t.Run("RefIsNull", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpRefIsNull})
		want := []byte{0xD1}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode ref.is_null = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("TableGet", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpTableGet, Imm: uint32(0)})
		want := []byte{0x25, 0x00}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode table.get = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("TableSet", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpTableSet, Imm: uint32(0)})
		want := []byte{0x26, 0x00}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode table.set = %v, want %v", b.Bytes, want)
		}
	})
}

func TestEncodeInstr_LoadStore(t *testing.T) {
	loadOps := []struct {
		name   string
		opcode byte
	}{
		{"i32.load", ast.OpI32Load},
		{"i64.load", ast.OpI64Load},
		{"f32.load", ast.OpF32Load},
		{"f64.load", ast.OpF64Load},
		{"i32.load8_s", ast.OpI32Load8S},
		{"i32.load8_u", ast.OpI32Load8U},
		{"i32.load16_s", ast.OpI32Load16S},
		{"i32.load16_u", ast.OpI32Load16U},
		{"i64.load8_s", ast.OpI64Load8S},
		{"i64.load8_u", ast.OpI64Load8U},
		{"i64.load16_s", ast.OpI64Load16S},
		{"i64.load16_u", ast.OpI64Load16U},
		{"i64.load32_s", ast.OpI64Load32S},
		{"i64.load32_u", ast.OpI64Load32U},
	}

	for _, tt := range loadOps {
		t.Run(tt.name, func(t *testing.T) {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: tt.opcode, Imm: ast.Memarg{Align: 2, Offset: 4}})
			if len(b.Bytes) < 3 {
				t.Errorf("Encode %s too short: %v", tt.name, b.Bytes)
			}
		})
	}

	storeOps := []struct {
		name   string
		opcode byte
	}{
		{"i32.store", ast.OpI32Store},
		{"i64.store", ast.OpI64Store},
		{"f32.store", ast.OpF32Store},
		{"f64.store", ast.OpF64Store},
		{"i32.store8", ast.OpI32Store8},
		{"i32.store16", ast.OpI32Store16},
		{"i64.store8", ast.OpI64Store8},
		{"i64.store16", ast.OpI64Store16},
		{"i64.store32", ast.OpI64Store32},
	}

	for _, tt := range storeOps {
		t.Run(tt.name, func(t *testing.T) {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: tt.opcode, Imm: ast.Memarg{Align: 2, Offset: 4}})
			if len(b.Bytes) < 3 {
				t.Errorf("Encode %s too short: %v", tt.name, b.Bytes)
			}
		})
	}
}

func TestEncodeInstr_FloatConst(t *testing.T) {
	t.Run("F32Const", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpF32Const, Imm: float32(3.14)})
		if len(b.Bytes) != 5 {
			t.Errorf("Encode f32.const should be 5 bytes, got %d", len(b.Bytes))
		}
	})

	t.Run("F64Const", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpF64Const, Imm: float64(3.14159)})
		if len(b.Bytes) != 9 {
			t.Errorf("Encode f64.const should be 9 bytes, got %d", len(b.Bytes))
		}
	})
}

func TestEncodeInstr_ControlFlow(t *testing.T) {
	t.Run("Loop", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{
			Opcode: ast.OpLoop,
			Imm:    ast.BlockType{Simple: ast.BlockTypeEmpty, TypeIdx: -1},
		})
		want := []byte{0x03, 0x40}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode loop = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("If", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{
			Opcode: ast.OpIf,
			Imm:    ast.BlockType{Simple: byte(ast.ValTypeI32), TypeIdx: -1},
		})
		want := []byte{0x04, 0x7F}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode if = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("Else", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpElse})
		want := []byte{0x05}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode else = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("BrIf", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpBrIf, Imm: uint32(0)})
		want := []byte{0x0D, 0x00}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode br_if = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("Return", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpReturn})
		want := []byte{0x0F}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode return = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("Call", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpCall, Imm: uint32(5)})
		want := []byte{0x10, 0x05}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("Encode call = %v, want %v", b.Bytes, want)
		}
	})
}

func TestEncodeInstr_LocalGlobal(t *testing.T) {
	t.Run("LocalSet", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpLocalSet, Imm: uint32(0)})
		if !bytes.Equal(b.Bytes, []byte{0x21, 0x00}) {
			t.Errorf("Encode local.set = %v", b.Bytes)
		}
	})

	t.Run("LocalTee", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpLocalTee, Imm: uint32(0)})
		if !bytes.Equal(b.Bytes, []byte{0x22, 0x00}) {
			t.Errorf("Encode local.tee = %v", b.Bytes)
		}
	})

	t.Run("GlobalGet", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpGlobalGet, Imm: uint32(0)})
		if !bytes.Equal(b.Bytes, []byte{0x23, 0x00}) {
			t.Errorf("Encode global.get = %v", b.Bytes)
		}
	})

	t.Run("GlobalSet", func(t *testing.T) {
		b := &Buffer{}
		EncodeInstr(b, ast.Instr{Opcode: ast.OpGlobalSet, Imm: uint32(0)})
		if !bytes.Equal(b.Bytes, []byte{0x24, 0x00}) {
			t.Errorf("Encode global.set = %v", b.Bytes)
		}
	})
}

func TestEncodeBuffer_WriteI33(t *testing.T) {
	t.Run("Negative1", func(t *testing.T) {
		b := &Buffer{}
		b.WriteI33(-1)
		want := []byte{0x7F}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("WriteI33(-1) = %v, want %v", b.Bytes, want)
		}
	})

	t.Run("Positive64", func(t *testing.T) {
		b := &Buffer{}
		b.WriteI33(0x40)
		want := []byte{0xC0, 0x00}
		if !bytes.Equal(b.Bytes, want) {
			t.Errorf("WriteI33(0x40) = %v, want %v", b.Bytes, want)
		}
	})
}

func TestEncodeInstr_NumericOps(t *testing.T) {
	t.Run("BinaryOps", func(t *testing.T) {
		ops := []byte{
			0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, 0x70, 0x71,
			0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		}

		for _, op := range ops {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: op})
			if len(b.Bytes) != 1 || b.Bytes[0] != op {
				t.Errorf("Encode op 0x%02X failed: got %v", op, b.Bytes)
			}
		}
	})

	t.Run("UnaryOps", func(t *testing.T) {
		ops := []byte{0x67, 0x68, 0x69, 0x45}
		for _, op := range ops {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: op})
			if len(b.Bytes) != 1 {
				t.Errorf("Encode op 0x%02X should be 1 byte, got %d", op, len(b.Bytes))
			}
		}
	})

	t.Run("ComparisonOps", func(t *testing.T) {
		ops := []byte{0x46, 0x47, 0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F}
		for _, op := range ops {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: op})
			if len(b.Bytes) != 1 {
				t.Errorf("Encode op 0x%02X should be 1 byte, got %d", op, len(b.Bytes))
			}
		}
	})

	t.Run("ConversionOps", func(t *testing.T) {
		ops := []byte{
			0xA7, 0xA8, 0xA9, 0xAA, 0xAB, 0xAC, 0xAD,
			0xB2, 0xB3, 0xB6, 0xB7, 0xBB, 0xBC, 0xBD, 0xBE, 0xBF,
		}
		for _, op := range ops {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: op})
			if len(b.Bytes) != 1 {
				t.Errorf("Encode op 0x%02X should be 1 byte, got %d", op, len(b.Bytes))
			}
		}
	})

	t.Run("F32Ops", func(t *testing.T) {
		ops := []byte{
			0x8B, 0x8C, 0x8D, 0x8E, 0x8F, 0x90, 0x91, 0x92, 0x93,
			0x94, 0x95, 0x96, 0x97, 0x98, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x60,
		}
		for _, op := range ops {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: op})
			if len(b.Bytes) != 1 {
				t.Errorf("Encode op 0x%02X should be 1 byte, got %d", op, len(b.Bytes))
			}
		}
	})

	t.Run("F64Ops", func(t *testing.T) {
		ops := []byte{
			0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F, 0xA0, 0xA1,
			0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66,
		}
		for _, op := range ops {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: op})
			if len(b.Bytes) != 1 {
				t.Errorf("Encode op 0x%02X should be 1 byte, got %d", op, len(b.Bytes))
			}
		}
	})

	t.Run("I64Ops", func(t *testing.T) {
		ops := []byte{
			0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57,
			0x58, 0x59, 0x5A, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E,
		}
		for _, op := range ops {
			b := &Buffer{}
			EncodeInstr(b, ast.Instr{Opcode: op})
			if len(b.Bytes) != 1 {
				t.Errorf("Encode op 0x%02X should be 1 byte, got %d", op, len(b.Bytes))
			}
		}
	})
}
