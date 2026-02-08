package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestGetStackEffect_Constants(t *testing.T) {
	tests := []struct {
		wantPops int
		op       byte
		wantPush wasm.ValType
	}{
		{0, wasm.OpI32Const, wasm.ValI32},
		{0, wasm.OpI64Const, wasm.ValI64},
		{0, wasm.OpF32Const, wasm.ValF32},
		{0, wasm.OpF64Const, wasm.ValF64},
	}
	for _, tc := range tests {
		eff := GetStackEffect(tc.op, wasm.Instruction{Opcode: tc.op}, nil)
		if eff == nil {
			t.Errorf("op %#x: expected stack effect, got nil", tc.op)
			continue
		}
		if eff.Pops != tc.wantPops {
			t.Errorf("op %#x: pops = %d, want %d", tc.op, eff.Pops, tc.wantPops)
		}
		if len(eff.Pushes) != 1 || eff.Pushes[0] != tc.wantPush {
			t.Errorf("op %#x: pushes = %v, want [%v]", tc.op, eff.Pushes, tc.wantPush)
		}
	}
}

func TestGetStackEffect_Drop(t *testing.T) {
	eff := GetStackEffect(wasm.OpDrop, wasm.Instruction{Opcode: wasm.OpDrop}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for drop")
	}
	if eff.Pops != 1 {
		t.Errorf("drop pops = %d, want 1", eff.Pops)
	}
	if len(eff.Pushes) != 0 {
		t.Errorf("drop pushes = %v, want empty", eff.Pushes)
	}
}

func TestGetStackEffect_Conversions(t *testing.T) {
	tests := []struct {
		name     string
		op       byte
		wantPush wasm.ValType
	}{
		{"i32.wrap_i64", wasm.OpI32WrapI64, wasm.ValI32},
		{"i64.extend_i32_s", wasm.OpI64ExtendI32S, wasm.ValI64},
		{"i64.extend_i32_u", wasm.OpI64ExtendI32U, wasm.ValI64},
		{"i32.trunc_f32_s", wasm.OpI32TruncF32S, wasm.ValI32},
		{"i32.trunc_f32_u", wasm.OpI32TruncF32U, wasm.ValI32},
		{"i32.trunc_f64_s", wasm.OpI32TruncF64S, wasm.ValI32},
		{"i32.trunc_f64_u", wasm.OpI32TruncF64U, wasm.ValI32},
		{"i64.trunc_f32_s", wasm.OpI64TruncF32S, wasm.ValI64},
		{"i64.trunc_f32_u", wasm.OpI64TruncF32U, wasm.ValI64},
		{"i64.trunc_f64_s", wasm.OpI64TruncF64S, wasm.ValI64},
		{"i64.trunc_f64_u", wasm.OpI64TruncF64U, wasm.ValI64},
		{"f32.convert_i32_s", wasm.OpF32ConvertI32S, wasm.ValF32},
		{"f32.convert_i32_u", wasm.OpF32ConvertI32U, wasm.ValF32},
		{"f32.convert_i64_s", wasm.OpF32ConvertI64S, wasm.ValF32},
		{"f32.convert_i64_u", wasm.OpF32ConvertI64U, wasm.ValF32},
		{"f64.convert_i32_s", wasm.OpF64ConvertI32S, wasm.ValF64},
		{"f64.convert_i32_u", wasm.OpF64ConvertI32U, wasm.ValF64},
		{"f64.convert_i64_s", wasm.OpF64ConvertI64S, wasm.ValF64},
		{"f64.convert_i64_u", wasm.OpF64ConvertI64U, wasm.ValF64},
		{"f32.demote_f64", wasm.OpF32DemoteF64, wasm.ValF32},
		{"f64.promote_f32", wasm.OpF64PromoteF32, wasm.ValF64},
		{"i32.reinterpret_f32", wasm.OpI32ReinterpretF32, wasm.ValI32},
		{"i64.reinterpret_f64", wasm.OpI64ReinterpretF64, wasm.ValI64},
		{"f32.reinterpret_i32", wasm.OpF32ReinterpretI32, wasm.ValF32},
		{"f64.reinterpret_i64", wasm.OpF64ReinterpretI64, wasm.ValF64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eff := GetStackEffect(tc.op, wasm.Instruction{Opcode: tc.op}, nil)
			if eff == nil {
				t.Fatal("expected stack effect")
			}
			if eff.Pops != 1 {
				t.Errorf("pops = %d, want 1", eff.Pops)
			}
			if len(eff.Pushes) != 1 || eff.Pushes[0] != tc.wantPush {
				t.Errorf("pushes = %v, want [%v]", eff.Pushes, tc.wantPush)
			}
		})
	}
}

func TestGetStackEffect_SignExtension(t *testing.T) {
	tests := []struct {
		op       byte
		wantPush wasm.ValType
	}{
		{wasm.OpI32Extend8S, wasm.ValI32},
		{wasm.OpI32Extend16S, wasm.ValI32},
		{wasm.OpI64Extend8S, wasm.ValI64},
		{wasm.OpI64Extend16S, wasm.ValI64},
		{wasm.OpI64Extend32S, wasm.ValI64},
	}
	for _, tc := range tests {
		eff := GetStackEffect(tc.op, wasm.Instruction{Opcode: tc.op}, nil)
		if eff == nil {
			t.Errorf("op %#x: expected stack effect", tc.op)
			continue
		}
		if eff.Pops != 1 {
			t.Errorf("op %#x: pops = %d, want 1", tc.op, eff.Pops)
		}
		if len(eff.Pushes) != 1 || eff.Pushes[0] != tc.wantPush {
			t.Errorf("op %#x: pushes = %v, want [%v]", tc.op, eff.Pushes, tc.wantPush)
		}
	}
}

func TestGetStackEffect_MemoryLoads(t *testing.T) {
	tests := []struct {
		op       byte
		wantPush wasm.ValType
	}{
		{wasm.OpI32Load, wasm.ValI32},
		{wasm.OpI32Load8S, wasm.ValI32},
		{wasm.OpI32Load8U, wasm.ValI32},
		{wasm.OpI32Load16S, wasm.ValI32},
		{wasm.OpI32Load16U, wasm.ValI32},
		{wasm.OpI64Load, wasm.ValI64},
		{wasm.OpI64Load8S, wasm.ValI64},
		{wasm.OpI64Load8U, wasm.ValI64},
		{wasm.OpI64Load16S, wasm.ValI64},
		{wasm.OpI64Load16U, wasm.ValI64},
		{wasm.OpI64Load32S, wasm.ValI64},
		{wasm.OpI64Load32U, wasm.ValI64},
		{wasm.OpF32Load, wasm.ValF32},
		{wasm.OpF64Load, wasm.ValF64},
	}
	for _, tc := range tests {
		eff := GetStackEffect(tc.op, wasm.Instruction{Opcode: tc.op}, nil)
		if eff == nil {
			t.Errorf("op %#x: expected stack effect", tc.op)
			continue
		}
		if eff.Pops != 1 {
			t.Errorf("op %#x: pops = %d, want 1", tc.op, eff.Pops)
		}
		if len(eff.Pushes) != 1 || eff.Pushes[0] != tc.wantPush {
			t.Errorf("op %#x: pushes = %v, want [%v]", tc.op, eff.Pushes, tc.wantPush)
		}
	}
}

func TestGetStackEffect_MemoryStores(t *testing.T) {
	stores := []byte{
		wasm.OpI32Store, wasm.OpI32Store8, wasm.OpI32Store16,
		wasm.OpI64Store, wasm.OpI64Store8, wasm.OpI64Store16, wasm.OpI64Store32,
		wasm.OpF32Store, wasm.OpF64Store,
	}
	for _, op := range stores {
		eff := GetStackEffect(op, wasm.Instruction{Opcode: op}, nil)
		if eff == nil {
			t.Errorf("op %#x: expected stack effect", op)
			continue
		}
		if eff.Pops != 2 {
			t.Errorf("op %#x: pops = %d, want 2", op, eff.Pops)
		}
		if len(eff.Pushes) != 0 {
			t.Errorf("op %#x: pushes = %v, want empty", op, eff.Pushes)
		}
	}
}

func TestGetStackEffect_MemorySizeGrow(t *testing.T) {
	// memory.size
	eff := GetStackEffect(wasm.OpMemorySize, wasm.Instruction{Opcode: wasm.OpMemorySize}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for memory.size")
	}
	if eff.Pops != 0 {
		t.Errorf("memory.size pops = %d, want 0", eff.Pops)
	}
	if len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
		t.Errorf("memory.size pushes = %v, want [i32]", eff.Pushes)
	}

	// memory.grow
	eff = GetStackEffect(wasm.OpMemoryGrow, wasm.Instruction{Opcode: wasm.OpMemoryGrow}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for memory.grow")
	}
	if eff.Pops != 1 {
		t.Errorf("memory.grow pops = %d, want 1", eff.Pops)
	}
	if len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
		t.Errorf("memory.grow pushes = %v, want [i32]", eff.Pushes)
	}
}

func TestGetStackEffect_ReferenceTypes(t *testing.T) {
	// ref.is_null
	eff := GetStackEffect(wasm.OpRefIsNull, wasm.Instruction{Opcode: wasm.OpRefIsNull}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for ref.is_null")
	}
	if eff.Pops != 1 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
		t.Errorf("ref.is_null: pops=%d pushes=%v, want pops=1 pushes=[i32]", eff.Pops, eff.Pushes)
	}

	// ref.func
	eff = GetStackEffect(wasm.OpRefFunc, wasm.Instruction{Opcode: wasm.OpRefFunc}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for ref.func")
	}
	if eff.Pops != 0 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValFuncRef {
		t.Errorf("ref.func: pops=%d pushes=%v, want pops=0 pushes=[funcref]", eff.Pops, eff.Pushes)
	}

	// ref.eq
	eff = GetStackEffect(wasm.OpRefEq, wasm.Instruction{Opcode: wasm.OpRefEq}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for ref.eq")
	}
	if eff.Pops != 2 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
		t.Errorf("ref.eq: pops=%d pushes=%v, want pops=2 pushes=[i32]", eff.Pops, eff.Pushes)
	}
}

func TestGetStackEffect_TableOps(t *testing.T) {
	// table.get
	eff := GetStackEffect(wasm.OpTableGet, wasm.Instruction{Opcode: wasm.OpTableGet}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for table.get")
	}
	if eff.Pops != 1 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValFuncRef {
		t.Errorf("table.get: pops=%d pushes=%v, want pops=1 pushes=[funcref]", eff.Pops, eff.Pushes)
	}

	// table.set
	eff = GetStackEffect(wasm.OpTableSet, wasm.Instruction{Opcode: wasm.OpTableSet}, nil)
	if eff == nil {
		t.Fatal("expected stack effect for table.set")
	}
	if eff.Pops != 2 || len(eff.Pushes) != 0 {
		t.Errorf("table.set: pops=%d pushes=%v, want pops=2 pushes=[]", eff.Pops, eff.Pushes)
	}
}

func TestGetStackEffect_Unknown(t *testing.T) {
	// Control flow and unknown ops return nil
	nilOps := []byte{
		wasm.OpNop,    // not in table
		wasm.OpBlock,  // control flow
		wasm.OpLoop,   // control flow
		wasm.OpIf,     // control flow
		wasm.OpBr,     // control flow
		wasm.OpBrIf,   // control flow
		wasm.OpReturn, // control flow
		wasm.OpCall,   // dynamic
		wasm.OpSelect, // handled by registry
		wasm.OpI32Add, // handled by BinaryOpHandler
		wasm.OpI32Eqz, // handled by UnaryOpHandler
	}
	for _, op := range nilOps {
		eff := GetStackEffect(op, wasm.Instruction{Opcode: op}, nil)
		if eff != nil {
			t.Errorf("op %#x: expected nil, got %+v", op, eff)
		}
	}
}

func TestGetStackEffectFromRegistry(t *testing.T) {
	reg := DefaultRegistry()

	// Test that i32.add comes from handler
	instr := wasm.Instruction{Opcode: wasm.OpI32Add}
	eff := GetStackEffectFromRegistry(reg, wasm.OpI32Add, instr, nil)
	if eff == nil {
		t.Fatal("expected stack effect for i32.add")
	}
	if eff.Pops != 2 || len(eff.Pushes) != 1 || eff.Pushes[0] != wasm.ValI32 {
		t.Errorf("i32.add: pops=%d pushes=%v", eff.Pops, eff.Pushes)
	}

	// Test that i32.const falls back to static table
	instr = wasm.Instruction{Opcode: wasm.OpI32Const}
	eff = GetStackEffectFromRegistry(reg, wasm.OpI32Const, instr, nil)
	if eff == nil {
		t.Fatal("expected stack effect for i32.const")
	}
	if eff.Pops != 0 || len(eff.Pushes) != 1 {
		t.Errorf("i32.const: pops=%d pushes=%v", eff.Pops, eff.Pushes)
	}
}
