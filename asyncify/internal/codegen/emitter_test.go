package codegen

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestEmitter_NewAndBytes(t *testing.T) {
	e := NewEmitter()
	if e.Len() != 0 {
		t.Errorf("new emitter should be empty, got len %d", e.Len())
	}

	e.I32Const(42)
	if e.Len() == 0 {
		t.Error("emitter should have content after I32Const")
	}

	data := e.Bytes()
	if len(data) == 0 {
		t.Error("Bytes() should return non-empty slice")
	}
}

func TestEmitter_Reset(t *testing.T) {
	e := NewEmitter()
	e.I32Const(42).I32Const(100)

	originalLen := e.Len()
	if originalLen == 0 {
		t.Fatal("emitter should have content before reset")
	}

	e.Reset()
	if e.Len() != 0 {
		t.Errorf("emitter should be empty after reset, got len %d", e.Len())
	}
}

func TestEmitter_Copy(t *testing.T) {
	e := NewEmitter()
	e.I32Const(42)

	copy1 := e.Copy()
	e.I32Const(100)
	copy2 := e.Bytes()

	if len(copy1) == len(copy2) {
		t.Error("Copy should be independent of further emitter operations")
	}
}

func TestEmitter_ControlFlow(t *testing.T) {
	tests := []struct {
		emit   func(e *Emitter)
		verify func(t *testing.T, data []byte)
		name   string
	}{
		{
			name: "block void",
			emit: func(e *Emitter) {
				e.Block(BlockVoid).End()
			},
			verify: func(t *testing.T, data []byte) {
				instrs, err := wasm.DecodeInstructions(data)
				if err != nil {
					t.Fatalf("decode error: %v", err)
				}
				if len(instrs) != 2 {
					t.Errorf("expected 2 instrs, got %d", len(instrs))
				}
				if instrs[0].Opcode != wasm.OpBlock {
					t.Errorf("first opcode = %#x, want block", instrs[0].Opcode)
				}
				if instrs[1].Opcode != wasm.OpEnd {
					t.Errorf("second opcode = %#x, want end", instrs[1].Opcode)
				}
			},
		},
		{
			name: "block i32 result",
			emit: func(e *Emitter) {
				e.Block(BlockI32).I32Const(42).End()
			},
			verify: func(t *testing.T, data []byte) {
				instrs, err := wasm.DecodeInstructions(data)
				if err != nil {
					t.Fatalf("decode error: %v", err)
				}
				if len(instrs) != 3 {
					t.Errorf("expected 3 instrs, got %d", len(instrs))
				}
				imm := instrs[0].Imm.(wasm.BlockImm)
				if imm.Type != BlockI32 {
					t.Errorf("block type = %d, want %d", imm.Type, BlockI32)
				}
			},
		},
		{
			name: "loop",
			emit: func(e *Emitter) {
				e.Loop(BlockVoid).Br(0).End()
			},
			verify: func(t *testing.T, data []byte) {
				instrs, err := wasm.DecodeInstructions(data)
				if err != nil {
					t.Fatalf("decode error: %v", err)
				}
				if instrs[0].Opcode != wasm.OpLoop {
					t.Errorf("first opcode = %#x, want loop", instrs[0].Opcode)
				}
				if instrs[1].Opcode != wasm.OpBr {
					t.Errorf("second opcode = %#x, want br", instrs[1].Opcode)
				}
			},
		},
		{
			name: "if else",
			emit: func(e *Emitter) {
				e.I32Const(1).If(BlockVoid).Nop().Else().Unreachable().End()
			},
			verify: func(t *testing.T, data []byte) {
				instrs, err := wasm.DecodeInstructions(data)
				if err != nil {
					t.Fatalf("decode error: %v", err)
				}
				// i32.const, if, nop, else, unreachable, end
				if len(instrs) != 6 {
					t.Errorf("expected 6 instrs, got %d", len(instrs))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEmitter()
			tt.emit(e)
			tt.verify(t, e.Bytes())
		})
	}
}

func TestEmitter_Variables(t *testing.T) {
	e := NewEmitter()
	e.LocalGet(0).LocalSet(1).LocalTee(2)
	e.GlobalGet(0).GlobalSet(1)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	expected := []byte{
		wasm.OpLocalGet,
		wasm.OpLocalSet,
		wasm.OpLocalTee,
		wasm.OpGlobalGet,
		wasm.OpGlobalSet,
	}

	for i, op := range expected {
		if instrs[i].Opcode != op {
			t.Errorf("instr[%d] = %#x, want %#x", i, instrs[i].Opcode, op)
		}
	}
}

func TestEmitter_Constants(t *testing.T) {
	e := NewEmitter()
	e.I32Const(42).I64Const(100).F32Const(3.14).F64Const(2.718)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(instrs) != 4 {
		t.Fatalf("expected 4 instrs, got %d", len(instrs))
	}

	if instrs[0].Imm.(wasm.I32Imm).Value != 42 {
		t.Errorf("i32 value = %d, want 42", instrs[0].Imm.(wasm.I32Imm).Value)
	}
	if instrs[1].Imm.(wasm.I64Imm).Value != 100 {
		t.Errorf("i64 value = %d, want 100", instrs[1].Imm.(wasm.I64Imm).Value)
	}
}

func TestEmitter_Memory(t *testing.T) {
	e := NewEmitter()
	e.I32Const(0).I32Load(2, 0)
	e.I32Const(0).I32Const(42).I32Store(2, 0)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if instrs[1].Opcode != wasm.OpI32Load {
		t.Errorf("expected i32.load, got %#x", instrs[1].Opcode)
	}
	if instrs[4].Opcode != wasm.OpI32Store {
		t.Errorf("expected i32.store, got %#x", instrs[4].Opcode)
	}
}

func TestEmitter_Arithmetic(t *testing.T) {
	e := NewEmitter()
	e.I32Const(10).I32Const(20).I32Add()
	e.I32Const(5).I32Sub()
	e.I32Const(2).I32Mul()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Check arithmetic operations are present
	hasAdd := false
	hasSub := false
	hasMul := false
	for _, instr := range instrs {
		switch instr.Opcode {
		case wasm.OpI32Add:
			hasAdd = true
		case wasm.OpI32Sub:
			hasSub = true
		case wasm.OpI32Mul:
			hasMul = true
		}
	}

	if !hasAdd {
		t.Error("missing i32.add")
	}
	if !hasSub {
		t.Error("missing i32.sub")
	}
	if !hasMul {
		t.Error("missing i32.mul")
	}
}

func TestEmitter_Comparison(t *testing.T) {
	e := NewEmitter()
	e.I32Const(0).I32Eqz()
	e.I32Const(1).I32Const(1).I32Eq()
	e.I32Const(1).I32Const(2).I32Ne()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	hasEqz := false
	hasEq := false
	hasNe := false
	for _, instr := range instrs {
		switch instr.Opcode {
		case wasm.OpI32Eqz:
			hasEqz = true
		case wasm.OpI32Eq:
			hasEq = true
		case wasm.OpI32Ne:
			hasNe = true
		}
	}

	if !hasEqz {
		t.Error("missing i32.eqz")
	}
	if !hasEq {
		t.Error("missing i32.eq")
	}
	if !hasNe {
		t.Error("missing i32.ne")
	}
}

func TestEmitter_Calls(t *testing.T) {
	e := NewEmitter()
	e.Call(5)
	e.I32Const(0).CallIndirect(2, 0)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if instrs[0].Opcode != wasm.OpCall {
		t.Errorf("expected call, got %#x", instrs[0].Opcode)
	}
	callImm := instrs[0].Imm.(wasm.CallImm)
	if callImm.FuncIdx != 5 {
		t.Errorf("call target = %d, want 5", callImm.FuncIdx)
	}

	if instrs[2].Opcode != wasm.OpCallIndirect {
		t.Errorf("expected call_indirect, got %#x", instrs[2].Opcode)
	}
}

func TestEmitter_BrTable(t *testing.T) {
	e := NewEmitter()
	e.I32Const(1).BrTable([]uint32{0, 1, 2}, 3)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if instrs[1].Opcode != wasm.OpBrTable {
		t.Errorf("expected br_table, got %#x", instrs[1].Opcode)
	}

	imm := instrs[1].Imm.(wasm.BrTableImm)
	if len(imm.Labels) != 3 {
		t.Errorf("labels len = %d, want 3", len(imm.Labels))
	}
	if imm.Default != 3 {
		t.Errorf("default = %d, want 3", imm.Default)
	}
}

func TestEmitter_StateCheck(t *testing.T) {
	stateGlobal := uint32(0)
	expectedState := int32(0) // StateNormal

	e := NewEmitter()
	e.StateCheck(stateGlobal, expectedState)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Should emit: global.get, i32.const, i32.eq
	if len(instrs) != 3 {
		t.Fatalf("expected 3 instrs, got %d", len(instrs))
	}
	if instrs[0].Opcode != wasm.OpGlobalGet {
		t.Errorf("instr[0] = %#x, want global.get", instrs[0].Opcode)
	}
	if instrs[1].Opcode != wasm.OpI32Const {
		t.Errorf("instr[1] = %#x, want i32.const", instrs[1].Opcode)
	}
	if instrs[2].Opcode != wasm.OpI32Eq {
		t.Errorf("instr[2] = %#x, want i32.eq", instrs[2].Opcode)
	}
}

func TestEmitter_IfState(t *testing.T) {
	e := NewEmitter()
	e.IfState(0, 0, BlockVoid).Nop().End()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// global.get, i32.const, i32.eq, if, nop, end
	if len(instrs) != 6 {
		t.Fatalf("expected 6 instrs, got %d", len(instrs))
	}
	if instrs[3].Opcode != wasm.OpIf {
		t.Errorf("instr[3] = %#x, want if", instrs[3].Opcode)
	}
}

func TestEmitter_LoadStackPtr(t *testing.T) {
	e := NewEmitter()
	e.LoadStackPtr(1)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// global.get, i32.load
	if len(instrs) != 2 {
		t.Fatalf("expected 2 instrs, got %d", len(instrs))
	}
	if instrs[0].Opcode != wasm.OpGlobalGet {
		t.Errorf("instr[0] = %#x, want global.get", instrs[0].Opcode)
	}
	if instrs[1].Opcode != wasm.OpI32Load {
		t.Errorf("instr[1] = %#x, want i32.load", instrs[1].Opcode)
	}
}

func TestEmitter_IncrStackPtr(t *testing.T) {
	e := NewEmitter()
	e.IncrStackPtr(1, 4)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// global.get, global.get, i32.load, i32.const(4), i32.add, i32.store
	if len(instrs) != 6 {
		t.Fatalf("expected 6 instrs, got %d", len(instrs))
	}
	if instrs[4].Opcode != wasm.OpI32Add {
		t.Errorf("instr[4] = %#x, want i32.add", instrs[4].Opcode)
	}
	constImm := instrs[3].Imm.(wasm.I32Imm)
	if constImm.Value != 4 {
		t.Errorf("increment = %d, want 4", constImm.Value)
	}
}

func TestEmitter_Raw(t *testing.T) {
	e := NewEmitter()
	raw := []byte{0x01, 0x02, 0x03}
	e.Raw(raw)

	if !bytes.Equal(e.Bytes(), raw) {
		t.Errorf("Raw bytes = %v, want %v", e.Bytes(), raw)
	}
}

func TestEmitter_EmitInstr(t *testing.T) {
	e := NewEmitter()
	e.EmitInstr(wasm.Instruction{
		Opcode: wasm.OpI32Const,
		Imm:    wasm.I32Imm{Value: 123},
	})

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(instrs) != 1 {
		t.Fatalf("expected 1 instr, got %d", len(instrs))
	}
	if instrs[0].Imm.(wasm.I32Imm).Value != 123 {
		t.Errorf("value = %d, want 123", instrs[0].Imm.(wasm.I32Imm).Value)
	}
}

func TestEmitter_Chaining(t *testing.T) {
	e := NewEmitter()

	// Test that all methods return the emitter for chaining
	result := e.
		Block(BlockVoid).
		I32Const(1).
		LocalSet(0).
		LocalGet(0).
		I32Eqz().
		BrIf(0).
		Drop().
		End()

	if result != e {
		t.Error("chaining should return same emitter")
	}

	if e.Len() == 0 {
		t.Error("chained emitter should have content")
	}
}

func TestEmitter_AllBlockTypes(t *testing.T) {
	types := []struct {
		name      string
		blockType int32
	}{
		{"void", BlockVoid},
		{"i32", BlockI32},
		{"i64", BlockI64},
		{"f32", BlockF32},
		{"f64", BlockF64},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEmitter()
			e.Block(tt.blockType).End()

			instrs, err := wasm.DecodeInstructions(e.Bytes())
			if err != nil {
				t.Fatalf("decode error: %v", err)
			}

			imm := instrs[0].Imm.(wasm.BlockImm)
			if imm.Type != tt.blockType {
				t.Errorf("block type = %d, want %d", imm.Type, tt.blockType)
			}
		})
	}
}

func TestEmitter_AllMemoryOps(t *testing.T) {
	e := NewEmitter()

	// Test all typed loads/stores
	e.I32Const(0).I32Load(2, 0).Drop()
	e.I32Const(0).I64Load(3, 0).Drop()
	e.I32Const(0).F32Load(2, 0).Drop()
	e.I32Const(0).F64Load(3, 0).Drop()

	e.I32Const(0).I32Const(0).I32Store(2, 0)
	e.I32Const(0).I64Const(0).I64Store(3, 0)
	e.I32Const(0).F32Const(0).F32Store(2, 0)
	e.I32Const(0).F64Const(0).F64Store(3, 0)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Verify loads are present
	loadOps := map[byte]bool{
		wasm.OpI32Load: false,
		wasm.OpI64Load: false,
		wasm.OpF32Load: false,
		wasm.OpF64Load: false,
	}
	storeOps := map[byte]bool{
		wasm.OpI32Store: false,
		wasm.OpI64Store: false,
		wasm.OpF32Store: false,
		wasm.OpF64Store: false,
	}

	for _, instr := range instrs {
		if _, ok := loadOps[instr.Opcode]; ok {
			loadOps[instr.Opcode] = true
		}
		if _, ok := storeOps[instr.Opcode]; ok {
			storeOps[instr.Opcode] = true
		}
	}

	for op, found := range loadOps {
		if !found {
			t.Errorf("missing load op %#x", op)
		}
	}
	for op, found := range storeOps {
		if !found {
			t.Errorf("missing store op %#x", op)
		}
	}
}

func TestEmitter_I64Operations(t *testing.T) {
	e := NewEmitter()
	e.I64Const(100).I64Const(200).I64Add()
	e.I64Const(50).I64Sub()
	e.I64Const(2).I64Mul()
	e.I64Eqz().Drop()
	e.I64Const(1).I64Const(1).I64Eq().Drop()
	e.I64Const(1).I64Const(2).I64Ne().Drop()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	required := []byte{wasm.OpI64Add, wasm.OpI64Sub, wasm.OpI64Mul, wasm.OpI64Eqz, wasm.OpI64Eq, wasm.OpI64Ne}
	for _, op := range required {
		if !ops[op] {
			t.Errorf("missing i64 op %#x", op)
		}
	}
}

func TestEmitter_BitwiseOps(t *testing.T) {
	e := NewEmitter()
	e.I32Const(0xFF).I32Const(0x0F).I32And()
	e.I32Const(0xF0).I32Or()
	e.I32Const(0xFF).I32Xor()
	e.I32Const(4).I32Shl()
	e.I32Const(2).I32ShrS()
	e.I32Const(1).I32ShrU()

	e.I64Const(0xFF).I64Const(0x0F).I64And()
	e.I64Const(0xF0).I64Or()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	required := []byte{
		wasm.OpI32And, wasm.OpI32Or, wasm.OpI32Xor,
		wasm.OpI32Shl, wasm.OpI32ShrS, wasm.OpI32ShrU,
		wasm.OpI64And, wasm.OpI64Or,
	}
	for _, op := range required {
		if !ops[op] {
			t.Errorf("missing bitwise op %#x", op)
		}
	}
}

func TestEmitter_Conversions(t *testing.T) {
	e := NewEmitter()
	e.I64Const(100).I32WrapI64()
	e.I32Const(50).I64ExtendI32S()
	e.I32Const(50).I64ExtendI32U()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	required := []byte{wasm.OpI32WrapI64, wasm.OpI64ExtendI32S, wasm.OpI64ExtendI32U}
	for _, op := range required {
		if !ops[op] {
			t.Errorf("missing conversion op %#x", op)
		}
	}
}

func TestNewEmitterWithCapacity(t *testing.T) {
	e := NewEmitterWithCapacity(1024)
	if e == nil {
		t.Fatal("NewEmitterWithCapacity returned nil")
	}
	if e.Len() != 0 {
		t.Errorf("new emitter with capacity should be empty, got len %d", e.Len())
	}

	// Should work like normal emitter
	e.I32Const(42)
	if e.Len() == 0 {
		t.Error("emitter should have content after I32Const")
	}
}

func TestEmitterPool(t *testing.T) {
	// Get from pool
	e := GetEmitter()
	if e == nil {
		t.Fatal("GetEmitter returned nil")
	}

	// Use it
	e.I32Const(42)
	if len(e.Bytes()) == 0 {
		t.Error("emitter should have bytes")
	}

	// Return to pool
	PutEmitter(e)

	// Get again - should be reset
	e2 := GetEmitter()
	if len(e2.Bytes()) != 0 {
		t.Error("pooled emitter should be reset")
	}
	PutEmitter(e2)

	// Nil should not panic
	PutEmitter(nil)
}

func TestGetEmitterWithCapacity(t *testing.T) {
	e := GetEmitterWithCapacity(4096)
	if e == nil {
		t.Fatal("GetEmitterWithCapacity returned nil")
	}
	// Write some data
	for i := 0; i < 100; i++ {
		e.I32Const(int32(i))
	}
	PutEmitter(e)

	// Get again with smaller capacity - should reuse large buffer
	e2 := GetEmitterWithCapacity(100)
	if e2 == nil {
		t.Fatal("GetEmitterWithCapacity returned nil")
	}
	PutEmitter(e2)
}

// Tests for previously 0% coverage methods

func TestEmitter_Return(t *testing.T) {
	e := NewEmitter()
	e.I32Const(42).Return()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(instrs) != 2 {
		t.Fatalf("expected 2 instructions, got %d", len(instrs))
	}
	if instrs[1].Opcode != wasm.OpReturn {
		t.Errorf("expected return, got %#x", instrs[1].Opcode)
	}
}

func TestEmitter_Select(t *testing.T) {
	e := NewEmitter()
	e.I32Const(1).I32Const(2).I32Const(0).Select()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	hasSelect := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpSelect {
			hasSelect = true
		}
	}
	if !hasSelect {
		t.Error("missing select instruction")
	}
}

func TestEmitter_MemorySizeAndGrow(t *testing.T) {
	e := NewEmitter()
	e.MemorySize().I32Const(1).MemoryGrow().Drop()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	if !ops[wasm.OpMemorySize] {
		t.Error("missing memory.size")
	}
	if !ops[wasm.OpMemoryGrow] {
		t.Error("missing memory.grow")
	}
}

func TestEmitter_RefNull(t *testing.T) {
	e := NewEmitter()
	e.RefNullFunc().Drop()
	e.RefNullExtern().Drop()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	refNullCount := 0
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpRefNull {
			refNullCount++
		}
	}
	if refNullCount != 2 {
		t.Errorf("expected 2 ref.null, got %d", refNullCount)
	}
}

func TestEmitter_ComparisonOps(t *testing.T) {
	e := NewEmitter()
	// Test all comparison ops that were at 0%
	e.I32Const(1).I32Const(2).I32LtS().Drop()
	e.I32Const(1).I32Const(2).I32LtU().Drop()
	e.I32Const(1).I32Const(2).I32GtS().Drop()
	e.I32Const(1).I32Const(2).I32GtU().Drop()
	e.I32Const(1).I32Const(2).I32LeS().Drop()
	e.I32Const(1).I32Const(2).I32LeU().Drop()
	e.I32Const(1).I32Const(2).I32GeS().Drop()
	e.I32Const(1).I32Const(2).I32GeU().Drop()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	required := []byte{
		wasm.OpI32LtS, wasm.OpI32LtU,
		wasm.OpI32GtS, wasm.OpI32GtU,
		wasm.OpI32LeS, wasm.OpI32LeU,
		wasm.OpI32GeS, wasm.OpI32GeU,
	}
	for _, op := range required {
		if !ops[op] {
			t.Errorf("missing comparison op %#x", op)
		}
	}
}

func TestEmitter_DivOps(t *testing.T) {
	e := NewEmitter()
	e.I32Const(10).I32Const(2).I32DivS().Drop()
	e.I32Const(10).I32Const(2).I32DivU().Drop()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	if !ops[wasm.OpI32DivS] {
		t.Error("missing i32.div_s")
	}
	if !ops[wasm.OpI32DivU] {
		t.Error("missing i32.div_u")
	}
}

func TestEmitter_FloatAdd(t *testing.T) {
	e := NewEmitter()
	e.F32Const(1.0).F32Const(2.0).F32Add().Drop()
	e.F64Const(1.0).F64Const(2.0).F64Add().Drop()

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	if !ops[wasm.OpF32Add] {
		t.Error("missing f32.add")
	}
	if !ops[wasm.OpF64Add] {
		t.Error("missing f64.add")
	}
}

func TestEmitter_EmitInstrs(t *testing.T) {
	e := NewEmitter()
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 2}},
		{Opcode: wasm.OpI32Add},
	}
	e.EmitInstrs(instrs)

	decoded, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(decoded) != 3 {
		t.Errorf("expected 3 instructions, got %d", len(decoded))
	}
}

func TestEmitter_EmitRawOpcode(t *testing.T) {
	e := NewEmitter()
	e.EmitRawOpcode(wasm.OpNop)
	e.EmitRawOpcode(wasm.OpNop)

	instrs, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(instrs) != 2 {
		t.Errorf("expected 2 nops, got %d", len(instrs))
	}
	for _, instr := range instrs {
		if instr.Opcode != wasm.OpNop {
			t.Errorf("expected nop, got %#x", instr.Opcode)
		}
	}
}

func TestEmitter_EmitV128Const(t *testing.T) {
	e := NewEmitter()
	val := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	e.EmitV128Const(val)

	data := e.Bytes()
	if len(data) == 0 {
		t.Error("v128.const should emit bytes")
	}
	// First byte should be SIMD prefix
	if data[0] != wasm.OpPrefixSIMD {
		t.Errorf("expected SIMD prefix %#x, got %#x", wasm.OpPrefixSIMD, data[0])
	}
}

func TestEmitter_StoreStackPtr(t *testing.T) {
	e := NewEmitter()
	e.StoreStackPtr(0, 100) // global 0, offset 100

	data := e.Bytes()
	if len(data) == 0 {
		t.Error("StoreStackPtr should emit bytes")
	}

	instrs, err := wasm.DecodeInstructions(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Should emit: global.get, i32.store
	ops := make(map[byte]bool)
	for _, instr := range instrs {
		ops[instr.Opcode] = true
	}

	if !ops[wasm.OpGlobalGet] {
		t.Error("missing global.get in StoreStackPtr")
	}
	if !ops[wasm.OpI32Store] {
		t.Error("missing i32.store in StoreStackPtr")
	}
}
