package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/wasm"
)

func newTestContext() *Context {
	emit := codegen.NewEmitter()
	stack := NewStack(99)
	body := &wasm.FuncBody{}
	locals := NewLocals(0, body, nil)
	return NewContext(emit, stack, locals, 0, 1)
}

func TestPassthroughHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterPassthroughHandlers(r)

	t.Run("nop", func(t *testing.T) {
		ctx := newTestContext()
		h := r.Get(wasm.OpNop)
		if h == nil {
			t.Fatal("nop handler not registered")
		}
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpNop}); err != nil {
			t.Fatal(err)
		}
		if ctx.Emit.Len() == 0 {
			t.Error("nop should emit bytecode")
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		ctx := newTestContext()
		h := r.Get(wasm.OpUnreachable)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpUnreachable}); err != nil {
			t.Fatal(err)
		}
		if ctx.Emit.Len() == 0 {
			t.Error("unreachable should emit bytecode")
		}
	})

	t.Run("drop", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValI32)

		h := r.Get(wasm.OpDrop)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpDrop}); err != nil {
			t.Fatal(err)
		}
		if !ctx.Stack.IsEmpty() {
			t.Error("drop should pop from stack")
		}
	})
}

func TestVariableHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterVariableHandlers(r)

	t.Run("local.get", func(t *testing.T) {
		ctx := newTestContext()
		// Pretend we have a local of type i64 at index 0
		ctx.Locals = NewLocals(1, &wasm.FuncBody{}, []wasm.ValType{wasm.ValI64})

		h := r.Get(wasm.OpLocalGet)
		instr := wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI64 {
			t.Errorf("pushed type = %#x, want i64", e.Type)
		}
	})

	t.Run("local.set", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValI32)

		h := r.Get(wasm.OpLocalSet)
		instr := wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 5}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if !ctx.Stack.IsEmpty() {
			t.Error("local.set should pop from stack")
		}
	})

	t.Run("local.tee", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Locals = NewLocals(6, &wasm.FuncBody{}, []wasm.ValType{
			wasm.ValI32, wasm.ValI32, wasm.ValI32,
			wasm.ValI32, wasm.ValI32, wasm.ValF32,
		})
		ctx.Stack.Push(10, wasm.ValI32)

		h := r.Get(wasm.OpLocalTee)
		instr := wasm.Instruction{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 5}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1 (tee keeps value)", ctx.Stack.Len())
		}
	})
}

func TestConstantHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterConstantHandlers(r)

	tests := []struct {
		instr    wasm.Instruction
		name     string
		opcode   byte
		wantType wasm.ValType
	}{
		{
			wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
			"i32.const",
			wasm.OpI32Const,
			wasm.ValI32,
		},
		{
			wasm.Instruction{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: 100}},
			"i64.const",
			wasm.OpI64Const,
			wasm.ValI64,
		},
		{
			wasm.Instruction{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: 3.14}},
			"f32.const",
			wasm.OpF32Const,
			wasm.ValF32,
		},
		{
			wasm.Instruction{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: 2.718}},
			"f64.const",
			wasm.OpF64Const,
			wasm.ValF64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestContext()
			h := r.Get(tt.opcode)
			if h == nil {
				t.Fatalf("handler not registered for %s", tt.name)
			}

			if err := h.Handle(ctx, tt.instr); err != nil {
				t.Fatal(err)
			}

			if ctx.Stack.Len() != 1 {
				t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
			}
			e := ctx.Stack.PopTyped()
			if e.Type != tt.wantType {
				t.Errorf("type = %#x, want %#x", e.Type, tt.wantType)
			}
		})
	}
}

func TestArithmeticHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterArithmeticHandlers(r)

	t.Run("binary op i32.add", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValI32)
		ctx.Stack.Push(20, wasm.ValI32)

		h := r.Get(wasm.OpI32Add)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpI32Add}); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("result type = %#x, want i32", e.Type)
		}
	})

	t.Run("unary op i32.eqz", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValI32)

		h := r.Get(wasm.OpI32Eqz)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpI32Eqz}); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
	})

	t.Run("i64.eq produces i32", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValI64)
		ctx.Stack.Push(20, wasm.ValI64)

		h := r.Get(wasm.OpI64Eq)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpI64Eq}); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("i64.eq result type = %#x, want i32", e.Type)
		}
	})

	t.Run("select", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI64) // true val
		ctx.Stack.Push(200, wasm.ValI64) // false val
		ctx.Stack.Push(1, wasm.ValI32)   // condition

		h := r.Get(wasm.OpSelect)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpSelect}); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI64 {
			t.Errorf("select result type = %#x, want i64", e.Type)
		}
	})
}

func TestConversionHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterConversionHandlers(r)

	tests := []struct {
		name       string
		opcode     byte
		inputType  wasm.ValType
		outputType wasm.ValType
	}{
		{"i32.wrap_i64", wasm.OpI32WrapI64, wasm.ValI64, wasm.ValI32},
		{"i64.extend_i32_s", wasm.OpI64ExtendI32S, wasm.ValI32, wasm.ValI64},
		{"f32.demote_f64", wasm.OpF32DemoteF64, wasm.ValF64, wasm.ValF32},
		{"f64.promote_f32", wasm.OpF64PromoteF32, wasm.ValF32, wasm.ValF64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestContext()
			ctx.Stack.Push(10, tt.inputType)

			h := r.Get(tt.opcode)
			if h == nil {
				t.Fatalf("handler not registered for %s", tt.name)
			}

			if err := h.Handle(ctx, wasm.Instruction{Opcode: tt.opcode}); err != nil {
				t.Fatal(err)
			}

			e := ctx.Stack.PopTyped()
			if e.Type != tt.outputType {
				t.Errorf("result type = %#x, want %#x", e.Type, tt.outputType)
			}
		})
	}
}

func TestMemoryHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterMemoryHandlers(r)

	t.Run("i32.load", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI32) // address

		h := r.Get(wasm.OpI32Load)
		instr := wasm.Instruction{
			Opcode: wasm.OpI32Load,
			Imm:    wasm.MemoryImm{Align: 2, Offset: 0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("load result type = %#x, want i32", e.Type)
		}
	})

	t.Run("i64.load", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI32) // address

		h := r.Get(wasm.OpI64Load)
		instr := wasm.Instruction{
			Opcode: wasm.OpI64Load,
			Imm:    wasm.MemoryImm{Align: 3, Offset: 0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI64 {
			t.Errorf("load result type = %#x, want i64", e.Type)
		}
	})

	t.Run("i32.store", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI32) // address
		ctx.Stack.Push(42, wasm.ValI32)  // value

		h := r.Get(wasm.OpI32Store)
		instr := wasm.Instruction{
			Opcode: wasm.OpI32Store,
			Imm:    wasm.MemoryImm{Align: 2, Offset: 0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if !ctx.Stack.IsEmpty() {
			t.Error("store should consume both values")
		}
	})

	t.Run("memory.size", func(t *testing.T) {
		ctx := newTestContext()

		h := r.Get(wasm.OpMemorySize)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpMemorySize}); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
	})

	t.Run("memory.grow", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValI32) // delta

		h := r.Get(wasm.OpMemoryGrow)
		if err := h.Handle(ctx, wasm.Instruction{Opcode: wasm.OpMemoryGrow}); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
	})
}

func TestGlobalGetHandler_TypeLookup(t *testing.T) {
	// Test that GlobalGetHandler uses the correct type from module metadata
	r := NewRegistry()
	RegisterVariableHandlers(r)

	t.Run("i32_global", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Module = &wasm.Module{
			Globals: []wasm.Global{
				{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}},
			},
		}

		h := r.Get(wasm.OpGlobalGet)
		instr := wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("expected i32 type, got %#x", e.Type)
		}
	})

	t.Run("i64_global", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Module = &wasm.Module{
			Globals: []wasm.Global{
				{Type: wasm.GlobalType{ValType: wasm.ValI64, Mutable: true}},
			},
		}

		h := r.Get(wasm.OpGlobalGet)
		instr := wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI64 {
			t.Errorf("expected i64 type, got %#x", e.Type)
		}
	})

	t.Run("f64_global", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Module = &wasm.Module{
			Globals: []wasm.Global{
				{Type: wasm.GlobalType{ValType: wasm.ValF64, Mutable: true}},
			},
		}

		h := r.Get(wasm.OpGlobalGet)
		instr := wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValF64 {
			t.Errorf("expected f64 type, got %#x", e.Type)
		}
	})

	t.Run("imported_global", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Module = &wasm.Module{
			Imports: []wasm.Import{
				{Module: "env", Name: "counter", Desc: wasm.ImportDesc{Kind: 3, Global: &wasm.GlobalType{ValType: wasm.ValI64, Mutable: true}}},
			},
			Globals: []wasm.Global{
				{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: false}},
			},
		}

		h := r.Get(wasm.OpGlobalGet)
		// Global index 0 is the imported i64 global
		instr := wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI64 {
			t.Errorf("expected i64 type for imported global, got %#x", e.Type)
		}
	})
}

func TestGlobalSetHandler(t *testing.T) {
	r := NewRegistry()
	RegisterVariableHandlers(r)

	ctx := newTestContext()
	ctx.Stack.Push(42, wasm.ValI32)

	h := r.Get(wasm.OpGlobalSet)
	instr := wasm.Instruction{Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{GlobalIdx: 0}}
	if err := h.Handle(ctx, instr); err != nil {
		t.Fatal(err)
	}

	if !ctx.Stack.IsEmpty() {
		t.Error("global.set should pop from stack")
	}
}

func TestGlobalGetHandler_NilModule(t *testing.T) {
	r := NewRegistry()
	RegisterVariableHandlers(r)

	ctx := newTestContext()
	ctx.Module = nil // no module metadata

	h := r.Get(wasm.OpGlobalGet)
	instr := wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}}
	if err := h.Handle(ctx, instr); err != nil {
		t.Fatal(err)
	}

	// Should default to i32 when module is nil
	e := ctx.Stack.PopTyped()
	if e.Type != wasm.ValI32 {
		t.Errorf("expected i32 default type when module is nil, got %#x", e.Type)
	}
}

func TestGlobalGetHandler_NilGlobalDescriptor(t *testing.T) {
	r := NewRegistry()
	RegisterVariableHandlers(r)

	ctx := newTestContext()
	ctx.Module = &wasm.Module{
		Imports: []wasm.Import{
			{Module: "env", Name: "g", Desc: wasm.ImportDesc{Kind: 3, Global: nil}}, // nil Global descriptor
		},
	}

	h := r.Get(wasm.OpGlobalGet)
	instr := wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 0}}
	if err := h.Handle(ctx, instr); err != nil {
		t.Fatal(err)
	}

	// Should default to i32 when Global descriptor is nil
	e := ctx.Stack.PopTyped()
	if e.Type != wasm.ValI32 {
		t.Errorf("expected i32 default type when Global is nil, got %#x", e.Type)
	}
}

func TestGlobalGetHandler_OutOfRange(t *testing.T) {
	r := NewRegistry()
	RegisterVariableHandlers(r)

	ctx := newTestContext()
	ctx.Module = &wasm.Module{
		Imports: []wasm.Import{},
		Globals: []wasm.Global{},
	}

	h := r.Get(wasm.OpGlobalGet)
	instr := wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 999}} // out of range
	if err := h.Handle(ctx, instr); err != nil {
		t.Fatal(err)
	}

	// Should default to i32 when global index is out of range
	e := ctx.Stack.PopTyped()
	if e.Type != wasm.ValI32 {
		t.Errorf("expected i32 default type for out-of-range global, got %#x", e.Type)
	}
}

func TestPassthroughHandler_Direct(t *testing.T) {
	// Test PassthroughHandler directly
	h := PassthroughHandler{}
	ctx := newTestContext()

	instr := wasm.Instruction{Opcode: wasm.OpNop}
	if err := h.Handle(ctx, instr); err != nil {
		t.Fatal(err)
	}

	if ctx.Emit.Len() == 0 {
		t.Error("PassthroughHandler should emit instruction")
	}
}

func TestSelectTypeHandler(t *testing.T) {
	r := NewRegistry()
	RegisterArithmeticHandlers(r)

	t.Run("select_t with explicit type", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValF64) // true val
		ctx.Stack.Push(200, wasm.ValF64) // false val
		ctx.Stack.Push(1, wasm.ValI32)   // condition

		h := r.Get(wasm.OpSelectType)
		if h == nil {
			t.Fatal("select_t handler not registered")
		}

		instr := wasm.Instruction{
			Opcode: wasm.OpSelectType,
			Imm:    wasm.SelectTypeImm{Types: []wasm.ValType{wasm.ValF64}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValF64 {
			t.Errorf("select_t result type = %#x, want f64", e.Type)
		}
	})
}

func TestSaturatingTruncationHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterMemoryHandlers(r)

	tests := []struct {
		name       string
		subOpcode  uint32
		inputType  wasm.ValType
		outputType wasm.ValType
	}{
		{"i32.trunc_sat_f32_s", wasm.MiscI32TruncSatF32S, wasm.ValF32, wasm.ValI32},
		{"i32.trunc_sat_f32_u", wasm.MiscI32TruncSatF32U, wasm.ValF32, wasm.ValI32},
		{"i32.trunc_sat_f64_s", wasm.MiscI32TruncSatF64S, wasm.ValF64, wasm.ValI32},
		{"i32.trunc_sat_f64_u", wasm.MiscI32TruncSatF64U, wasm.ValF64, wasm.ValI32},
		{"i64.trunc_sat_f32_s", wasm.MiscI64TruncSatF32S, wasm.ValF32, wasm.ValI64},
		{"i64.trunc_sat_f32_u", wasm.MiscI64TruncSatF32U, wasm.ValF32, wasm.ValI64},
		{"i64.trunc_sat_f64_s", wasm.MiscI64TruncSatF64S, wasm.ValF64, wasm.ValI64},
		{"i64.trunc_sat_f64_u", wasm.MiscI64TruncSatF64U, wasm.ValF64, wasm.ValI64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestContext()
			ctx.Stack.Push(10, tt.inputType) // operand

			h := r.Get(wasm.OpPrefixMisc)
			if h == nil {
				t.Fatal("misc handler not registered")
			}

			instr := wasm.Instruction{
				Opcode: wasm.OpPrefixMisc,
				Imm:    wasm.MiscImm{SubOpcode: tt.subOpcode},
			}
			if err := h.Handle(ctx, instr); err != nil {
				t.Fatal(err)
			}

			if ctx.Stack.Len() != 1 {
				t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
			}
			e := ctx.Stack.PopTyped()
			if e.Type != tt.outputType {
				t.Errorf("result type = %#x, want %#x", e.Type, tt.outputType)
			}
		})
	}
}

func TestBulkMemoryHandler(t *testing.T) {
	r := NewRegistry()
	RegisterMemoryHandlers(r)

	t.Run("memory.copy", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValI32) // dst
		ctx.Stack.Push(2, wasm.ValI32) // src
		ctx.Stack.Push(3, wasm.ValI32) // len

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscMemoryCopy, Operands: []uint32{0, 0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})

	t.Run("memory.fill", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValI32) // dst
		ctx.Stack.Push(2, wasm.ValI32) // val
		ctx.Stack.Push(3, wasm.ValI32) // len

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscMemoryFill, Operands: []uint32{0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})

	t.Run("memory.init", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValI32) // dst
		ctx.Stack.Push(2, wasm.ValI32) // src
		ctx.Stack.Push(3, wasm.ValI32) // len

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscMemoryInit, Operands: []uint32{0, 0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})

	t.Run("data.drop", func(t *testing.T) {
		ctx := newTestContext()

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscDataDrop, Operands: []uint32{0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})

	t.Run("table.init", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValI32)
		ctx.Stack.Push(2, wasm.ValI32)
		ctx.Stack.Push(3, wasm.ValI32)

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscTableInit, Operands: []uint32{0, 0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})

	t.Run("elem.drop", func(t *testing.T) {
		ctx := newTestContext()

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscElemDrop, Operands: []uint32{0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})

	t.Run("table.copy", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValI32)
		ctx.Stack.Push(2, wasm.ValI32)
		ctx.Stack.Push(3, wasm.ValI32)

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscTableCopy, Operands: []uint32{0, 0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})

	t.Run("table.grow", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValFuncRef) // init
		ctx.Stack.Push(2, wasm.ValI32)     // n

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscTableGrow, Operands: []uint32{0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("type = %#x, want i32", e.Type)
		}
	})

	t.Run("table.fill", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(1, wasm.ValI32)
		ctx.Stack.Push(2, wasm.ValFuncRef)
		ctx.Stack.Push(3, wasm.ValI32)

		h := r.Get(wasm.OpPrefixMisc)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscTableFill, Operands: []uint32{0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Stack.Len() != 0 {
			t.Errorf("stack len = %d, want 0", ctx.Stack.Len())
		}
	})
}

func TestStackEffecter(t *testing.T) {
	t.Run("BinaryOpHandler", func(t *testing.T) {
		h := BinaryOpHandler{wasm.OpI32Add, wasm.ValI32}
		eff := h.StackEffect()
		if eff.Pops != 2 {
			t.Errorf("pops = %d, want 2", eff.Pops)
		}
		if len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
			t.Errorf("pushes = %v, want [i32]", eff.Pushes)
		}
	})

	t.Run("UnaryOpHandler", func(t *testing.T) {
		h := UnaryOpHandler{wasm.OpI64Clz, wasm.ValI64}
		eff := h.StackEffect()
		if eff.Pops != 1 {
			t.Errorf("pops = %d, want 1", eff.Pops)
		}
		if len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI64 {
			t.Errorf("pushes = %v, want [i64]", eff.Pushes)
		}
	})

	t.Run("i64.eqz produces i32", func(t *testing.T) {
		h := UnaryOpHandler{wasm.OpI64Eqz, wasm.ValI32}
		eff := h.StackEffect()
		if len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
			t.Errorf("i64.eqz should produce i32, got %v", eff.Pushes)
		}
	})
}

func TestReferenceHandlers(t *testing.T) {
	r := NewRegistry()
	RegisterReferenceHandlers(r)

	t.Run("table.get", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Module = &wasm.Module{
			Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
		}
		ctx.Stack.Push(0, wasm.ValI32) // index

		h := r.Get(wasm.OpTableGet)
		if h == nil {
			t.Fatal("table.get handler not registered")
		}

		instr := wasm.Instruction{Opcode: wasm.OpTableGet, Imm: wasm.TableImm{TableIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValFuncRef {
			t.Errorf("result type = %#x, want funcref", e.Type)
		}
	})

	t.Run("table.get with externref", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Module = &wasm.Module{
			Tables: []wasm.TableType{{ElemType: byte(wasm.ValExtern), Limits: wasm.Limits{Min: 1}}},
		}
		ctx.Stack.Push(0, wasm.ValI32) // index

		h := r.Get(wasm.OpTableGet)
		instr := wasm.Instruction{Opcode: wasm.OpTableGet, Imm: wasm.TableImm{TableIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValExtern {
			t.Errorf("result type = %#x, want externref", e.Type)
		}
	})

	t.Run("table.set", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(0, wasm.ValI32)      // index
		ctx.Stack.Push(10, wasm.ValFuncRef) // value

		h := r.Get(wasm.OpTableSet)
		instr := wasm.Instruction{Opcode: wasm.OpTableSet, Imm: wasm.TableImm{TableIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if !ctx.Stack.IsEmpty() {
			t.Error("table.set should consume both values")
		}
	})

	t.Run("ref.null funcref", func(t *testing.T) {
		ctx := newTestContext()

		h := r.Get(wasm.OpRefNull)
		instr := wasm.Instruction{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: wasm.HeapTypeFunc}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValFuncRef {
			t.Errorf("ref.null funcref type = %#x, want funcref", e.Type)
		}
	})

	t.Run("ref.null externref", func(t *testing.T) {
		ctx := newTestContext()

		h := r.Get(wasm.OpRefNull)
		instr := wasm.Instruction{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: wasm.HeapTypeExtern}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValExtern {
			t.Errorf("ref.null externref type = %#x, want externref", e.Type)
		}
	})

	t.Run("ref.is_null", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValFuncRef) // ref

		h := r.Get(wasm.OpRefIsNull)
		instr := wasm.Instruction{Opcode: wasm.OpRefIsNull}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("ref.is_null result = %#x, want i32", e.Type)
		}
	})

	t.Run("ref.func", func(t *testing.T) {
		ctx := newTestContext()

		h := r.Get(wasm.OpRefFunc)
		instr := wasm.Instruction{Opcode: wasm.OpRefFunc, Imm: wasm.RefFuncImm{FuncIdx: 0}}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValFuncRef {
			t.Errorf("ref.func type = %#x, want funcref", e.Type)
		}
	})

	t.Run("ref.as_non_null", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValFuncRef)

		h := r.Get(wasm.OpRefAsNonNull)
		instr := wasm.Instruction{Opcode: wasm.OpRefAsNonNull}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValFuncRef {
			t.Errorf("ref.as_non_null preserves type, got %#x", e.Type)
		}
	})

	t.Run("ref.eq", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(10, wasm.ValFuncRef)
		ctx.Stack.Push(20, wasm.ValFuncRef)

		h := r.Get(wasm.OpRefEq)
		instr := wasm.Instruction{Opcode: wasm.OpRefEq}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("ref.eq result = %#x, want i32", e.Type)
		}
	})
}

func TestSIMDHandler(t *testing.T) {
	r := NewRegistry()
	RegisterSIMDHandlers(r)

	lane0 := byte(0)
	lane1 := byte(1)

	t.Run("v128.const", func(t *testing.T) {
		ctx := newTestContext()

		h := r.Get(wasm.OpPrefixSIMD)
		if h == nil {
			t.Fatal("SIMD handler not registered")
		}

		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: make([]byte, 16)},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if ctx.Stack.Len() != 1 {
			t.Errorf("stack len = %d, want 1", ctx.Stack.Len())
		}
		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("v128.const type = %#x, want v128", e.Type)
		}
	})

	t.Run("v128.load", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI32) // address

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128Load, MemArg: &wasm.MemoryImm{Align: 4, Offset: 0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("v128.load type = %#x, want v128", e.Type)
		}
	})

	t.Run("v128.store", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI32)  // address
		ctx.Stack.Push(200, wasm.ValV128) // value

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128Store, MemArg: &wasm.MemoryImm{Align: 4, Offset: 0}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if !ctx.Stack.IsEmpty() {
			t.Error("v128.store should consume both values")
		}
	})

	t.Run("i8x16.splat", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(42, wasm.ValI32) // scalar

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI8x16Splat},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("i8x16.splat type = %#x, want v128", e.Type)
		}
	})

	t.Run("i32x4.extract_lane", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128) // vector

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI32x4ExtractLane, LaneIdx: &lane0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("i32x4.extract_lane type = %#x, want i32", e.Type)
		}
	})

	t.Run("f64x2.extract_lane", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128)

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdF64x2ExtractLane, LaneIdx: &lane1},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValF64 {
			t.Errorf("f64x2.extract_lane type = %#x, want f64", e.Type)
		}
	})

	t.Run("i8x16.shuffle", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128) // a
		ctx.Stack.Push(200, wasm.ValV128) // b

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI8x16Shuffle, V128Bytes: []byte{0, 1, 2, 3, 4, 5, 6, 7, 16, 17, 18, 19, 20, 21, 22, 23}},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("i8x16.shuffle type = %#x, want v128", e.Type)
		}
	})

	t.Run("v128.any_true", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128)

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128AnyTrue},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("v128.any_true type = %#x, want i32", e.Type)
		}
	})

	t.Run("v128.bitselect", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128) // a
		ctx.Stack.Push(200, wasm.ValV128) // b
		ctx.Stack.Push(300, wasm.ValV128) // mask

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128Bitselect},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("v128.bitselect type = %#x, want v128", e.Type)
		}
	})

	t.Run("i32x4.replace_lane", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128) // vector
		ctx.Stack.Push(42, wasm.ValI32)   // scalar

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI32x4ReplaceLane, LaneIdx: &lane0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("i32x4.replace_lane type = %#x, want v128", e.Type)
		}
	})

	t.Run("i64x2.extract_lane", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128)

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI64x2ExtractLane, LaneIdx: &lane0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI64 {
			t.Errorf("i64x2.extract_lane type = %#x, want i64", e.Type)
		}
	})

	t.Run("f32x4.extract_lane", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128)

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdF32x4ExtractLane, LaneIdx: &lane0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValF32 {
			t.Errorf("f32x4.extract_lane type = %#x, want f32", e.Type)
		}
	})

	t.Run("i8x16.swizzle", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128) // a
		ctx.Stack.Push(200, wasm.ValV128) // idx

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI8x16Swizzle},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("i8x16.swizzle type = %#x, want v128", e.Type)
		}
	})

	t.Run("i8x16.bitmask", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128)

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI8x16Bitmask},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValI32 {
			t.Errorf("i8x16.bitmask type = %#x, want i32", e.Type)
		}
	})

	t.Run("i8x16.add_binary", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128)
		ctx.Stack.Push(200, wasm.ValV128)

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI8x16Add},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("i8x16.add type = %#x, want v128", e.Type)
		}
	})

	t.Run("i8x16.neg_unary", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValV128)

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdI8x16Neg},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("i8x16.neg type = %#x, want v128", e.Type)
		}
	})

	t.Run("v128.load8_lane", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI32)  // address
		ctx.Stack.Push(200, wasm.ValV128) // vector

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128Load8Lane, MemArg: &wasm.MemoryImm{}, LaneIdx: &lane0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		e := ctx.Stack.PopTyped()
		if e.Type != wasm.ValV128 {
			t.Errorf("v128.load8_lane type = %#x, want v128", e.Type)
		}
	})

	t.Run("v128.store8_lane", func(t *testing.T) {
		ctx := newTestContext()
		ctx.Stack.Push(100, wasm.ValI32)  // address
		ctx.Stack.Push(200, wasm.ValV128) // vector

		h := r.Get(wasm.OpPrefixSIMD)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128Store8Lane, MemArg: &wasm.MemoryImm{}, LaneIdx: &lane0},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}

		if !ctx.Stack.IsEmpty() {
			t.Error("v128.store8_lane should consume values")
		}
	})
}

func TestAllHandlersRegistration(t *testing.T) {
	r := NewRegistry()
	RegisterPassthroughHandlers(r)
	RegisterVariableHandlers(r)
	RegisterConstantHandlers(r)
	RegisterArithmeticHandlers(r)
	RegisterConversionHandlers(r)
	RegisterMemoryHandlers(r)

	// Count registered handlers
	count := 0
	for i := 0; i < 256; i++ {
		if r.Has(byte(i)) {
			count++
		}
	}

	// Should have many handlers registered
	if count < 80 {
		t.Errorf("expected at least 80 handlers, got %d", count)
	}

	t.Logf("Registered %d handlers", count)

	// Verify select_t is registered
	if !r.Has(wasm.OpSelectType) {
		t.Error("select_t handler not registered")
	}
}

func TestGCHandler_StackEffectWith_AllOps(t *testing.T) {
	h := GCHandler{}
	tests := []struct {
		name      string
		subOpcode uint32
		pops      int
		pushLen   int
	}{
		{"struct.new", wasm.GCStructNew, 0, 1},
		{"struct.new_default", wasm.GCStructNewDefault, 0, 1},
		{"struct.get", wasm.GCStructGet, 1, 1},
		{"struct.get_s", wasm.GCStructGetS, 1, 1},
		{"struct.get_u", wasm.GCStructGetU, 1, 1},
		{"struct.set", wasm.GCStructSet, 2, 0},
		{"array.new", wasm.GCArrayNew, 2, 1},
		{"array.new_default", wasm.GCArrayNewDefault, 1, 1},
		{"array.new_fixed", wasm.GCArrayNewFixed, 0, 1},
		{"array.new_data", wasm.GCArrayNewData, 2, 1},
		{"array.new_elem", wasm.GCArrayNewElem, 2, 1},
		{"array.get", wasm.GCArrayGet, 2, 1},
		{"array.get_s", wasm.GCArrayGetS, 2, 1},
		{"array.get_u", wasm.GCArrayGetU, 2, 1},
		{"array.set", wasm.GCArraySet, 3, 0},
		{"array.len", wasm.GCArrayLen, 1, 1},
		{"array.fill", wasm.GCArrayFill, 4, 0},
		{"array.copy", wasm.GCArrayCopy, 5, 0},
		{"array.init_data", wasm.GCArrayInitData, 4, 0},
		{"array.init_elem", wasm.GCArrayInitElem, 4, 0},
		{"ref.test", wasm.GCRefTest, 1, 1},
		{"ref.test_null", wasm.GCRefTestNull, 1, 1},
		{"ref.cast", wasm.GCRefCast, 1, 1},
		{"ref.cast_null", wasm.GCRefCastNull, 1, 1},
		{"br_on_cast", wasm.GCBrOnCast, 1, 1},
		{"br_on_cast_fail", wasm.GCBrOnCastFail, 1, 1},
		{"any.convert_extern", wasm.GCAnyConvertExtern, 1, 1},
		{"extern.convert_any", wasm.GCExternConvertAny, 1, 1},
		{"ref.i31", wasm.GCRefI31, 1, 1},
		{"i31.get_s", wasm.GCI31GetS, 1, 1},
		{"i31.get_u", wasm.GCI31GetU, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instr := wasm.Instruction{
				Opcode: wasm.OpPrefixGC,
				Imm:    wasm.GCImm{SubOpcode: tc.subOpcode},
			}
			eff := h.StackEffectWith(instr)
			if eff == nil {
				t.Fatal("expected non-nil effect")
			}
			if eff.Pops != tc.pops {
				t.Errorf("pops = %d, want %d", eff.Pops, tc.pops)
			}
			if len(eff.Pushes) != tc.pushLen {
				t.Errorf("pushes len = %d, want %d", len(eff.Pushes), tc.pushLen)
			}
		})
	}
}

// TestGCHandler_WrongImmType tests GCHandler StackEffectWith with non-GCImm
func TestGCHandler_WrongImmType(t *testing.T) {
	h := GCHandler{}

	// StackEffectWith with wrong imm type should return nil
	instr := wasm.Instruction{Opcode: wasm.OpPrefixGC, Imm: wasm.CallImm{FuncIdx: 0}}
	if eff := h.StackEffectWith(instr); eff != nil {
		t.Error("StackEffectWith with wrong imm type should return nil")
	}
}

// TestMemoryHandler_WrongImmType tests BulkMemoryHandler StackEffectWith with non-BulkMemoryImm
func TestMemoryHandler_WrongImmType(t *testing.T) {
	h := BulkMemoryHandler{}

	// StackEffectWith with wrong imm type should return nil
	instr := wasm.Instruction{Opcode: wasm.OpPrefixMisc, Imm: wasm.CallImm{FuncIdx: 0}}
	if eff := h.StackEffectWith(instr); eff != nil {
		t.Error("StackEffectWith with wrong imm type should return nil")
	}
}

func TestSIMDHandler_StackEffectWith_MoreOps(t *testing.T) {
	h := SIMDHandler{}
	tests := []struct {
		name      string
		subOpcode uint32
		pops      int
		pushLen   int
	}{
		// Loads
		{"v128.load", wasm.SimdV128Load, 1, 1},
		{"v128.load32_zero", wasm.SimdV128Load32Zero, 1, 1},
		{"v128.load64_zero", wasm.SimdV128Load64Zero, 1, 1},
		// Store
		{"v128.store", wasm.SimdV128Store, 2, 0},
		// Const
		{"v128.const", wasm.SimdV128Const, 0, 1},
		// Shuffle/Swizzle
		{"i8x16.shuffle", wasm.SimdI8x16Shuffle, 2, 1},
		{"i8x16.swizzle", wasm.SimdI8x16Swizzle, 2, 1},
		// Splat
		{"i8x16.splat", wasm.SimdI8x16Splat, 1, 1},
		// Extract lanes
		{"i8x16.extract_lane_s", wasm.SimdI8x16ExtractLaneS, 1, 1},
		{"i64x2.extract_lane", wasm.SimdI64x2ExtractLane, 1, 1},
		{"f32x4.extract_lane", wasm.SimdF32x4ExtractLane, 1, 1},
		{"f64x2.extract_lane", wasm.SimdF64x2ExtractLane, 1, 1},
		// Replace lanes
		{"i8x16.replace_lane", wasm.SimdI8x16ReplaceLane, 2, 1},
		// Lane loads
		{"v128.load8_lane", wasm.SimdV128Load8Lane, 2, 1},
		// Lane stores
		{"v128.store8_lane", wasm.SimdV128Store8Lane, 2, 0},
		// Bitmask
		{"v128.any_true", wasm.SimdV128AnyTrue, 1, 1},
		{"i8x16.all_true", wasm.SimdI8x16AllTrue, 1, 1},
		{"i8x16.bitmask", wasm.SimdI8x16Bitmask, 1, 1},
		// Bitselect
		{"v128.bitselect", wasm.SimdV128Bitselect, 3, 1},
		// Binary operations - one from each range
		{"i8x16.eq", 0x23, 2, 1},  // i8x16 comparisons (0x23-0x28)
		{"i16x8.eq", 0x2D, 2, 1},  // i16x8 comparisons (0x2D-0x34)
		{"i32x4.eq", 0x37, 2, 1},  // i32x4 comparisons (0x37-0x40)
		{"f32x4.eq", 0x41, 2, 1},  // f32x4 comparisons (0x41-0x46)
		{"f64x2.eq", 0x47, 2, 1},  // f64x2 comparisons (0x47-0x4C)
		{"v128.and", 0x4E, 2, 1},  // v128 bitwise (0x4E-0x51)
		{"i16x8.add", 0x8E, 2, 1}, // i16x8 arithmetic (0x8D-0x9E)
		{"i32x4.add", 0xAE, 2, 1}, // i32x4 arithmetic (0xAB-0xBE)
		{"i64x2.add", 0xCE, 2, 1}, // i64x2 arithmetic (0xC5-0xD6)
		{"f32x4.add", 0xE4, 2, 1}, // f32x4 arithmetic (0xE4-0xEF)
		{"f64x2.add", 0xF0, 2, 1}, // f64x2 arithmetic (0xF0-0xFD)
		// Unary operations - one from each range
		{"v128.not", 0x4D, 1, 1},  // v128.not
		{"i16x8.abs", 0x80, 1, 1}, // i16x8 unary (0x80-0x81)
		{"i32x4.abs", 0xA0, 1, 1}, // i32x4 unary (0xA0-0xA1)
		{"i64x2.abs", 0xC0, 1, 1}, // i64x2 unary (0xC0-0xC1)
		{"f32x4.abs", 0x67, 1, 1}, // f32x4 abs/neg/sqrt (0x67-0x69)
		{"f32x4.neg", 0x68, 1, 1},
		{"f32x4.sqrt", 0x69, 1, 1},
		{"f32x4.ceil", 0xE0, 1, 1}, // f32x4 rounding (0xE0-0xE3)
		{"f64x2.ceil", 0x74, 1, 1}, // f64x2 rounding - all 4 ops
		{"f64x2.floor", 0x75, 1, 1},
		{"f64x2.trunc", 0x76, 1, 1},
		{"f64x2.nearest", 0x77, 1, 1},
		{"i16x8.extend_low", 0x5E, 1, 1},  // i16x8 extend low/high (0x5E-0x5F)
		{"i16x8.extend_high", 0x5F, 1, 1}, // i16x8 extend high
		{"i32x4.ext", 0x7C, 1, 1},         // i32x4 extend (0x7C-0x7F)
		{"i64x2.ext", 0x9C, 1, 1},         // i64x2 extend (0x9C-0x9F)
		{"f32x4.convert", 0xBC, 1, 1},     // convert ops (0xBC-0xBF) - was overlap
		{"f64x2.abs", 0xEC, 1, 1},         // f64x2 abs (0xEC) - was overlap
		{"f64x2.neg", 0xED, 1, 1},         // f64x2 neg (0xED) - was overlap
		{"f64x2.sqrt", 0xEF, 1, 1},        // f64x2 sqrt (0xEF) - was overlap
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instr := wasm.Instruction{
				Opcode: wasm.OpPrefixSIMD,
				Imm:    wasm.SIMDImm{SubOpcode: tc.subOpcode},
			}
			eff := h.StackEffectWith(instr)
			if eff == nil {
				t.Fatal("expected non-nil effect")
			}
			if eff.Pops != tc.pops {
				t.Errorf("pops = %d, want %d", eff.Pops, tc.pops)
			}
			if len(eff.Pushes) != tc.pushLen {
				t.Errorf("pushes len = %d, want %d", len(eff.Pushes), tc.pushLen)
			}
		})
	}
}

func TestBulkMemoryHandler_StackEffectWith_MoreOps(t *testing.T) {
	h := BulkMemoryHandler{}
	tests := []struct {
		name      string
		subOpcode uint32
		pops      int
		pushLen   int
	}{
		{"memory.init", wasm.MiscMemoryInit, 3, 0},
		{"data.drop", wasm.MiscDataDrop, 0, 0},
		{"memory.copy", wasm.MiscMemoryCopy, 3, 0},
		{"memory.fill", wasm.MiscMemoryFill, 3, 0},
		{"table.init", wasm.MiscTableInit, 3, 0},
		{"elem.drop", wasm.MiscElemDrop, 0, 0},
		{"table.copy", wasm.MiscTableCopy, 3, 0},
		{"table.grow", wasm.MiscTableGrow, 2, 1},
		{"table.size", wasm.MiscTableSize, 0, 1},
		{"table.fill", wasm.MiscTableFill, 3, 0},
		// Saturating trunc
		{"i32.trunc_sat_f32_s", wasm.MiscI32TruncSatF32S, 1, 1},
		{"i32.trunc_sat_f32_u", wasm.MiscI32TruncSatF32U, 1, 1},
		{"i32.trunc_sat_f64_s", wasm.MiscI32TruncSatF64S, 1, 1},
		{"i32.trunc_sat_f64_u", wasm.MiscI32TruncSatF64U, 1, 1},
		{"i64.trunc_sat_f32_s", wasm.MiscI64TruncSatF32S, 1, 1},
		{"i64.trunc_sat_f32_u", wasm.MiscI64TruncSatF32U, 1, 1},
		{"i64.trunc_sat_f64_s", wasm.MiscI64TruncSatF64S, 1, 1},
		{"i64.trunc_sat_f64_u", wasm.MiscI64TruncSatF64U, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instr := wasm.Instruction{
				Opcode: wasm.OpPrefixMisc,
				Imm:    wasm.MiscImm{SubOpcode: tc.subOpcode},
			}
			eff := h.StackEffectWith(instr)
			if eff == nil {
				t.Fatal("expected non-nil effect")
			}
			if eff.Pops != tc.pops {
				t.Errorf("pops = %d, want %d", eff.Pops, tc.pops)
			}
			if len(eff.Pushes) != tc.pushLen {
				t.Errorf("pushes len = %d, want %d", len(eff.Pushes), tc.pushLen)
			}
		})
	}
}

func TestStackEffectWith_FallbackChain(t *testing.T) {
	// Verify StackEffectWith returns correct values for known and unknown sub-opcodes

	t.Run("SIMD_known_subop", func(t *testing.T) {
		h := SIMDHandler{}
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: wasm.SimdV128Const},
		}
		eff := h.StackEffectWith(instr)
		if eff == nil {
			t.Fatal("expected non-nil effect for v128.const")
		}
		if eff.Pops != 0 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValV128 {
			t.Errorf("v128.const: got pops=%d pushes=%v, want pops=0 pushes=[v128]", eff.Pops, eff.Pushes)
		}
	})

	t.Run("SIMD_unknown_subop", func(t *testing.T) {
		h := SIMDHandler{}
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.SIMDImm{SubOpcode: 0xFFFF}, // Unknown
		}
		eff := h.StackEffectWith(instr)
		if eff == nil {
			t.Fatal("SIMD should return default effect for unknown")
		}
		// Default is binary (v128, v128) -> v128
		if eff.Pops != 2 {
			t.Errorf("unknown SIMD: got pops=%d, want 2", eff.Pops)
		}
	})

	t.Run("GC_known_subop", func(t *testing.T) {
		h := GCHandler{}
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixGC,
			Imm:    wasm.GCImm{SubOpcode: wasm.GCArrayLen},
		}
		eff := h.StackEffectWith(instr)
		if eff == nil {
			t.Fatal("expected non-nil effect for array.len")
		}
		if eff.Pops != 1 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
			t.Errorf("array.len: got pops=%d pushes=%v, want pops=1 pushes=[i32]", eff.Pops, eff.Pushes)
		}
	})

	t.Run("GC_unknown_subop", func(t *testing.T) {
		h := GCHandler{}
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixGC,
			Imm:    wasm.GCImm{SubOpcode: 0xFFFF}, // Unknown
		}
		eff := h.StackEffectWith(instr)
		if eff != nil {
			t.Errorf("unknown GC should return nil (passthrough), got %+v", eff)
		}
	})

	t.Run("Misc_known_subop", func(t *testing.T) {
		h := BulkMemoryHandler{}
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: wasm.MiscTableSize},
		}
		eff := h.StackEffectWith(instr)
		if eff == nil {
			t.Fatal("expected non-nil effect for table.size")
		}
		if eff.Pops != 0 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
			t.Errorf("table.size: got pops=%d pushes=%v, want pops=0 pushes=[i32]", eff.Pops, eff.Pushes)
		}
	})

	t.Run("Misc_unknown_subop", func(t *testing.T) {
		h := BulkMemoryHandler{}
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixMisc,
			Imm:    wasm.MiscImm{SubOpcode: 0xFFFF}, // Unknown
		}
		eff := h.StackEffectWith(instr)
		if eff != nil {
			t.Errorf("unknown Misc should return nil (passthrough), got %+v", eff)
		}
	})

	t.Run("SIMD_wrong_imm_type", func(t *testing.T) {
		h := SIMDHandler{}
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixSIMD,
			Imm:    wasm.BlockImm{}, // Wrong type
		}
		eff := h.StackEffectWith(instr)
		if eff != nil {
			t.Error("wrong imm type should return nil")
		}
	})
}

func TestGCHandler(t *testing.T) {
	r := NewRegistry()
	RegisterGCHandlers(r)

	tests := []struct {
		setup     func(*Context)
		name      string
		stackLen  int
		subOpcode uint32
	}{
		{nil, "struct.new", 1, wasm.GCStructNew},
		{nil, "struct.new_default", 1, wasm.GCStructNewDefault},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "struct.get", 1, wasm.GCStructGet},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "struct.get_s", 1, wasm.GCStructGetS},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "struct.get_u", 1, wasm.GCStructGetU},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValFuncRef)
			c.Stack.Push(2, wasm.ValI32)
		}, "struct.set", 0, wasm.GCStructSet},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValI32)
			c.Stack.Push(2, wasm.ValI32)
		}, "array.new", 1, wasm.GCArrayNew},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValI32) // length
		}, "array.new_default", 1, wasm.GCArrayNewDefault},
		{nil, "array.new_fixed", 1, wasm.GCArrayNewFixed},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValI32) // offset
			c.Stack.Push(2, wasm.ValI32) // size
		}, "array.new_data", 1, wasm.GCArrayNewData},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValI32) // offset
			c.Stack.Push(2, wasm.ValI32) // size
		}, "array.new_elem", 1, wasm.GCArrayNewElem},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValFuncRef) // array
			c.Stack.Push(2, wasm.ValI32)     // index
		}, "array.get", 1, wasm.GCArrayGet},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValFuncRef)
			c.Stack.Push(2, wasm.ValI32)
		}, "array.get_s", 1, wasm.GCArrayGetS},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValFuncRef)
			c.Stack.Push(2, wasm.ValI32)
		}, "array.get_u", 1, wasm.GCArrayGetU},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValFuncRef) // array
			c.Stack.Push(2, wasm.ValI32)     // index
			c.Stack.Push(3, wasm.ValI32)     // value
		}, "array.set", 0, wasm.GCArraySet},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "array.len", 1, wasm.GCArrayLen},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValFuncRef) // array
			c.Stack.Push(2, wasm.ValI32)     // offset
			c.Stack.Push(3, wasm.ValI32)     // value
			c.Stack.Push(4, wasm.ValI32)     // size
		}, "array.fill", 0, wasm.GCArrayFill},
		{func(c *Context) {
			c.Stack.Push(1, wasm.ValFuncRef) // dst
			c.Stack.Push(2, wasm.ValI32)     // dst_offset
			c.Stack.Push(3, wasm.ValFuncRef) // src
			c.Stack.Push(4, wasm.ValI32)     // src_offset
			c.Stack.Push(5, wasm.ValI32)     // size
		}, "array.copy", 0, wasm.GCArrayCopy},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "ref.test", 1, wasm.GCRefTest},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "ref.test_null", 1, wasm.GCRefTestNull},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "ref.cast", 1, wasm.GCRefCast},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "ref.cast_null", 1, wasm.GCRefCastNull},
		{func(c *Context) { c.Stack.Push(1, wasm.ValExtern) }, "any.convert_extern", 1, wasm.GCAnyConvertExtern},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "extern.convert_any", 1, wasm.GCExternConvertAny},
		{func(c *Context) { c.Stack.Push(1, wasm.ValI32) }, "ref.i31", 1, wasm.GCRefI31},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "i31.get_s", 1, wasm.GCI31GetS},
		{func(c *Context) { c.Stack.Push(1, wasm.ValFuncRef) }, "i31.get_u", 1, wasm.GCI31GetU},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestContext()
			if tt.setup != nil {
				tt.setup(ctx)
			}

			h := r.Get(wasm.OpPrefixGC)
			if h == nil {
				t.Fatal("gc handler not registered")
			}

			instr := wasm.Instruction{
				Opcode: wasm.OpPrefixGC,
				Imm:    wasm.GCImm{SubOpcode: tt.subOpcode},
			}
			if err := h.Handle(ctx, instr); err != nil {
				t.Fatal(err)
			}

			if ctx.Stack.Len() != tt.stackLen {
				t.Errorf("stack len = %d, want %d", ctx.Stack.Len(), tt.stackLen)
			}
		})
	}

	t.Run("unknown_passthrough", func(t *testing.T) {
		ctx := newTestContext()
		h := r.Get(wasm.OpPrefixGC)
		instr := wasm.Instruction{
			Opcode: wasm.OpPrefixGC,
			Imm:    wasm.GCImm{SubOpcode: 0xFFFF},
		}
		if err := h.Handle(ctx, instr); err != nil {
			t.Fatal(err)
		}
		if ctx.Emit.Len() == 0 {
			t.Error("unknown should emit instruction")
		}
	})
}
