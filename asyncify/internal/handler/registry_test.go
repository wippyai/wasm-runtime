package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/wasm"
)

func requireStoredOperand(t *testing.T, operand StackEntry, wantIndex uint32, wantType wasm.ValType) {
	t.Helper()
	gotIndex, stored := operand.LocalIndex()
	if !stored || gotIndex != wantIndex || operand.Type() != wantType {
		t.Errorf("operand = {%d, stored=%t, %#x}, want {%d, stored=true, %#x}", gotIndex, stored, operand.Type(), wantIndex, wantType)
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	called := false
	h := Func(func(ctx *Context, instr wasm.Instruction) error {
		called = true
		return nil
	})

	r.Register(wasm.OpNop, h, "nop")

	if !r.Has(wasm.OpNop) {
		t.Error("Has should return true for registered opcode")
	}
	if r.Has(wasm.OpEnd) {
		t.Error("Has should return false for unregistered opcode")
	}

	got := r.Get(wasm.OpNop)
	if got == nil {
		t.Fatal("Get should return handler for registered opcode")
	}

	ctx := &Context{Emit: codegen.NewEmitter()}
	_ = got.Handle(ctx, wasm.Instruction{Opcode: wasm.OpNop})

	if !called {
		t.Error("handler should have been called")
	}
}

func TestRegistry_Name(t *testing.T) {
	r := NewRegistry()

	r.Register(wasm.OpNop, Func(nil), "nop_handler")

	if r.Name(wasm.OpNop) != "nop_handler" {
		t.Errorf("Name = %q, want %q", r.Name(wasm.OpNop), "nop_handler")
	}
	if r.Name(wasm.OpEnd) != "" {
		t.Errorf("Name for unregistered should be empty, got %q", r.Name(wasm.OpEnd))
	}
}

func TestRegistry_RegisterFunc(t *testing.T) {
	r := NewRegistry()

	called := false
	r.RegisterFunc(wasm.OpNop, func(ctx *Context, instr wasm.Instruction) error {
		called = true
		return nil
	}, "nop")

	got := r.Get(wasm.OpNop)
	if got == nil {
		t.Fatal("RegisterFunc should register handler")
	}

	ctx := &Context{Emit: codegen.NewEmitter()}
	_ = got.Handle(ctx, wasm.Instruction{})

	if !called {
		t.Error("handler should have been called")
	}
}

func TestRegistry_RegisterBulk(t *testing.T) {
	r := NewRegistry()

	h := Func(func(ctx *Context, instr wasm.Instruction) error {
		return nil
	})

	opcodes := []byte{wasm.OpI32Add, wasm.OpI32Sub, wasm.OpI32Mul}
	r.RegisterBulk(opcodes, h, "arithmetic")

	for _, op := range opcodes {
		if !r.Has(op) {
			t.Errorf("opcode %#x should be registered", op)
		}
		if r.Name(op) != "arithmetic" {
			t.Errorf("Name(%#x) = %q, want %q", op, r.Name(op), "arithmetic")
		}
	}
}

func TestRegistry_MissingHandlers(t *testing.T) {
	r := NewRegistry()

	r.Register(wasm.OpNop, Func(nil), "nop")
	r.Register(wasm.OpEnd, Func(nil), "end")

	check := []byte{wasm.OpNop, wasm.OpEnd, wasm.OpBlock, wasm.OpLoop}
	missing := r.MissingHandlers(check)

	if len(missing) != 2 {
		t.Errorf("expected 2 missing, got %d", len(missing))
	}

	hasMissing := func(op byte) bool {
		for _, m := range missing {
			if m == op {
				return true
			}
		}
		return false
	}

	if !hasMissing(wasm.OpBlock) {
		t.Error("block should be in missing")
	}
	if !hasMissing(wasm.OpLoop) {
		t.Error("loop should be in missing")
	}
}

func TestStack_PushPop(t *testing.T) {
	s := NewStack(99)

	if !s.IsEmpty() {
		t.Error("new stack should be empty")
	}

	s.Push(10, wasm.ValI32)
	s.Push(20, wasm.ValI64)
	s.Push(30, wasm.ValF32)

	if s.Len() != 3 {
		t.Errorf("Len = %d, want 3", s.Len())
	}

	// Pop in LIFO order
	requireStoredOperand(t, s.Pop(), 30, wasm.ValF32)
	requireStoredOperand(t, s.Pop(), 20, wasm.ValI64)
	requireStoredOperand(t, s.Pop(), 10, wasm.ValI32)

	// Pop from empty returns fallback
	requireStoredOperand(t, s.Pop(), 99, wasm.ValI32)
}

func TestStack_PopTyped(t *testing.T) {
	s := NewStack(99)

	s.Push(10, wasm.ValI32)
	s.Push(20, wasm.ValI64)

	e := s.Pop()
	requireStoredOperand(t, e, 20, wasm.ValI64)

	e = s.Pop()
	requireStoredOperand(t, e, 10, wasm.ValI32)

	// From empty
	e = s.Pop()
	requireStoredOperand(t, e, 99, wasm.ValI32)
}

func TestStack_Peek(t *testing.T) {
	s := NewStack(99)

	// Peek on empty
	e := s.Peek()
	requireStoredOperand(t, e, 99, wasm.ValI32)

	s.Push(42, wasm.ValF64)
	e = s.Peek()
	requireStoredOperand(t, e, 42, wasm.ValF64)

	// Peek doesn't remove
	if s.Len() != 1 {
		t.Errorf("after Peek, Len = %d, want 1", s.Len())
	}
}

func TestStack_Clear(t *testing.T) {
	s := NewStack(0)

	s.Push(1, wasm.ValI32)
	s.Push(2, wasm.ValI32)
	s.Push(3, wasm.ValI32)

	s.Clear()

	if !s.IsEmpty() {
		t.Error("stack should be empty after Clear")
	}
}

func TestStack_PushI32(t *testing.T) {
	s := NewStack(0)
	s.PushI32(42)

	e := s.Pop()
	requireStoredOperand(t, e, 42, wasm.ValI32)
}

func TestLocals_Alloc(t *testing.T) {
	body := &wasm.FuncBody{}
	initial := []wasm.ValType{wasm.ValI32, wasm.ValI32}
	l := NewLocals(2, body, initial)

	idx1 := l.Alloc(wasm.ValI64)
	if idx1 != 2 {
		t.Errorf("first Alloc = %d, want 2", idx1)
	}

	idx2 := l.AllocI32()
	if idx2 != 3 {
		t.Errorf("second Alloc = %d, want 3", idx2)
	}

	idx3 := l.Alloc(wasm.ValF64)
	if idx3 != 4 {
		t.Errorf("third Alloc = %d, want 4", idx3)
	}

	if l.NextIdx() != 5 {
		t.Errorf("NextIdx = %d, want 5", l.NextIdx())
	}

	// Check body.Locals was updated
	if len(body.Locals) != 3 {
		t.Errorf("len(body.Locals) = %d, want 3", len(body.Locals))
	}
}

func TestLocals_Alloc_PredeclaredTypeMismatchAllocatesFresh(t *testing.T) {
	body := &wasm.FuncBody{}
	// Pretend we pre-declared a temp i32 slot at index 1.
	initial := []wasm.ValType{wasm.ValI32, wasm.ValI32}
	l := NewLocals(1, body, initial)

	// Request i64 at index 1; allocator must not reuse mismatched i32 slot.
	idx := l.Alloc(wasm.ValI64)
	if idx != 2 {
		t.Fatalf("Alloc(i64) = %d, want fresh index 2", idx)
	}
	if got := l.TypeOf(idx); got != wasm.ValI64 {
		t.Fatalf("TypeOf(%d) = %#x, want i64", idx, got)
	}
	if len(body.Locals) != 1 {
		t.Fatalf("len(body.Locals) = %d, want 1 newly declared local", len(body.Locals))
	}
}

func TestLocals_TypeOf(t *testing.T) {
	body := &wasm.FuncBody{}
	initial := []wasm.ValType{wasm.ValI32, wasm.ValI64, wasm.ValF32}
	l := NewLocals(3, body, initial)

	if l.TypeOf(0) != wasm.ValI32 {
		t.Errorf("TypeOf(0) = %#x, want i32", l.TypeOf(0))
	}
	if l.TypeOf(1) != wasm.ValI64 {
		t.Errorf("TypeOf(1) = %#x, want i64", l.TypeOf(1))
	}
	if l.TypeOf(2) != wasm.ValF32 {
		t.Errorf("TypeOf(2) = %#x, want f32", l.TypeOf(2))
	}

	// Allocate new local
	idx := l.Alloc(wasm.ValF64)
	if l.TypeOf(idx) != wasm.ValF64 {
		t.Errorf("TypeOf(%d) = %#x, want f64", idx, l.TypeOf(idx))
	}

	// Out of range returns i32
	if l.TypeOf(100) != wasm.ValI32 {
		t.Errorf("TypeOf(100) = %#x, want i32", l.TypeOf(100))
	}
}

func TestContext_Creation(t *testing.T) {
	emit := codegen.NewEmitter()
	stack := NewStack(99)
	body := &wasm.FuncBody{}
	locals := NewLocals(0, body, nil)

	ctx := NewContext(emit, stack, locals, 0, 1)

	if ctx.Emit != emit {
		t.Error("Emit not set correctly")
	}
	if ctx.Stack != stack {
		t.Error("Stack not set correctly")
	}
	if ctx.Locals != locals {
		t.Error("Locals not set correctly")
	}
	if ctx.StateGlobal != 0 {
		t.Errorf("StateGlobal = %d, want 0", ctx.StateGlobal)
	}
	if ctx.DataGlobal != 1 {
		t.Errorf("DataGlobal = %d, want 1", ctx.DataGlobal)
	}
}

func TestContext_AllocTemp(t *testing.T) {
	emit := codegen.NewEmitter()
	stack := NewStack(99)
	body := &wasm.FuncBody{}
	locals := NewLocals(0, body, nil)
	ctx := NewContext(emit, stack, locals, 0, 1)

	idx := ctx.AllocTemp(wasm.ValI64)
	if idx != 0 {
		t.Errorf("AllocTemp = %d, want 0", idx)
	}

	if ctx.TypeOf(idx) != wasm.ValI64 {
		t.Errorf("TypeOf(%d) = %#x, want i64", idx, ctx.TypeOf(idx))
	}
}

func TestContext_PushPopResult(t *testing.T) {
	emit := codegen.NewEmitter()
	stack := NewStack(99)
	body := &wasm.FuncBody{}
	locals := NewLocals(0, body, nil)
	ctx := NewContext(emit, stack, locals, 0, 1)

	ctx.PushResult(wasm.ValF32, 42)

	if stack.Len() != 1 {
		t.Errorf("stack Len = %d, want 1", stack.Len())
	}

	requireStoredOperand(t, ctx.PopArg(), 42, wasm.ValF32)
}

func TestFunc_Nil(t *testing.T) {
	// Ensure nil Func doesn't panic when not called
	r := NewRegistry()
	r.Register(wasm.OpNop, nil, "nil")

	if r.Has(wasm.OpNop) {
		t.Error("nil handler should still show as not registered")
	}
}

func TestRegistry_Replace(t *testing.T) {
	r := NewRegistry()

	callCount := 0
	h1 := Func(func(ctx *Context, instr wasm.Instruction) error {
		callCount = 1
		return nil
	})
	h2 := Func(func(ctx *Context, instr wasm.Instruction) error {
		callCount = 2
		return nil
	})

	r.Register(wasm.OpNop, h1, "first")
	r.Register(wasm.OpNop, h2, "second")

	ctx := &Context{Emit: codegen.NewEmitter()}
	_ = r.Get(wasm.OpNop).Handle(ctx, wasm.Instruction{})

	if callCount != 2 {
		t.Errorf("second handler should be called, got callCount = %d", callCount)
	}
	if r.Name(wasm.OpNop) != "second" {
		t.Errorf("Name = %q, want %q", r.Name(wasm.OpNop), "second")
	}
}
