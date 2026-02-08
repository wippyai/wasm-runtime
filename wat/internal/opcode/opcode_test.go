package opcode

import (
	"testing"
)

func TestLookup(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		operands int
		immType  ImmKind
	}{
		// Control
		{"unreachable", 0x00, 0, ImmNone},
		{"nop", 0x01, 0, ImmNone},
		{"return", 0x0F, -1, ImmNone},
		{"br", 0x0C, -1, ImmU32},
		{"br_if", 0x0D, -1, ImmU32},
		{"call", 0x10, -1, ImmU32},

		// Variables
		{"local.get", 0x20, 0, ImmU32},
		{"local.set", 0x21, 1, ImmU32},
		{"local.tee", 0x22, 1, ImmU32},
		{"global.get", 0x23, 0, ImmU32},
		{"global.set", 0x24, 1, ImmU32},

		// Constants
		{"i32.const", 0x41, 0, ImmI32},
		{"i64.const", 0x42, 0, ImmI64},
		{"f32.const", 0x43, 0, ImmF32},
		{"f64.const", 0x44, 0, ImmF64},

		// Comparisons
		{"i32.eqz", 0x45, 1, ImmNone},
		{"i32.eq", 0x46, 2, ImmNone},
		{"i64.eqz", 0x50, 1, ImmNone},
		{"f32.eq", 0x5B, 2, ImmNone},
		{"f64.eq", 0x61, 2, ImmNone},

		// Arithmetic
		{"i32.add", 0x6A, 2, ImmNone},
		{"i64.add", 0x7C, 2, ImmNone},
		{"f32.add", 0x92, 2, ImmNone},
		{"f64.add", 0xA0, 2, ImmNone},

		// Conversions
		{"i32.wrap_i64", 0xA7, 1, ImmNone},
		{"i64.extend_i32_s", 0xAC, 1, ImmNone},
		{"f32.demote_f64", 0xB6, 1, ImmNone},
		{"f64.promote_f32", 0xBB, 1, ImmNone},

		// Sign extension
		{"i32.extend8_s", 0xC0, 1, ImmNone},
		{"i64.extend32_s", 0xC4, 1, ImmNone},

		// Parametric
		{"drop", 0x1A, 1, ImmNone},

		// Memory
		{"memory.size", 0x3F, 0, ImmMemIdx},
		{"memory.grow", 0x40, 1, ImmMemIdx},

		// Reference
		{"ref.is_null", 0xD1, 1, ImmNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := Lookup(tt.name)
			if !ok {
				t.Fatalf("Lookup(%q) not found", tt.name)
			}
			if info.Opcode != tt.opcode {
				t.Errorf("opcode = 0x%02X, want 0x%02X", info.Opcode, tt.opcode)
			}
			if info.Operands != tt.operands {
				t.Errorf("operands = %d, want %d", info.Operands, tt.operands)
			}
			if info.ImmType != tt.immType {
				t.Errorf("immType = %d, want %d", info.ImmType, tt.immType)
			}
		})
	}
}

func TestLookupNotFound(t *testing.T) {
	_, ok := Lookup("nonexistent")
	if ok {
		t.Error("Lookup should return false for unknown instruction")
	}
}

func TestLookupMemory(t *testing.T) {
	tests := []struct {
		name         string
		opcode       byte
		naturalAlign uint32
		operands     int
	}{
		// Loads
		{"i32.load", 0x28, 2, 1},
		{"i64.load", 0x29, 3, 1},
		{"f32.load", 0x2A, 2, 1},
		{"f64.load", 0x2B, 3, 1},
		{"i32.load8_s", 0x2C, 0, 1},
		{"i32.load8_u", 0x2D, 0, 1},
		{"i32.load16_s", 0x2E, 1, 1},
		{"i32.load16_u", 0x2F, 1, 1},
		{"i64.load8_s", 0x30, 0, 1},
		{"i64.load8_u", 0x31, 0, 1},
		{"i64.load16_s", 0x32, 1, 1},
		{"i64.load16_u", 0x33, 1, 1},
		{"i64.load32_s", 0x34, 2, 1},
		{"i64.load32_u", 0x35, 2, 1},

		// Stores
		{"i32.store", 0x36, 2, 2},
		{"i64.store", 0x37, 3, 2},
		{"f32.store", 0x38, 2, 2},
		{"f64.store", 0x39, 3, 2},
		{"i32.store8", 0x3A, 0, 2},
		{"i32.store16", 0x3B, 1, 2},
		{"i64.store8", 0x3C, 0, 2},
		{"i64.store16", 0x3D, 1, 2},
		{"i64.store32", 0x3E, 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, ok := LookupMemory(tt.name)
			if !ok {
				t.Fatalf("LookupMemory(%q) not found", tt.name)
			}
			if op.Opcode != tt.opcode {
				t.Errorf("opcode = 0x%02X, want 0x%02X", op.Opcode, tt.opcode)
			}
			if op.NaturalAlign != tt.naturalAlign {
				t.Errorf("naturalAlign = %d, want %d", op.NaturalAlign, tt.naturalAlign)
			}
			if op.Operands != tt.operands {
				t.Errorf("operands = %d, want %d", op.Operands, tt.operands)
			}
		})
	}
}

func TestLookupPrefixed(t *testing.T) {
	tests := []struct {
		name     string
		subop    uint32
		operands int
	}{
		// Saturating truncation
		{"i32.trunc_sat_f32_s", 0, 1},
		{"i32.trunc_sat_f32_u", 1, 1},
		{"i32.trunc_sat_f64_s", 2, 1},
		{"i32.trunc_sat_f64_u", 3, 1},
		{"i64.trunc_sat_f32_s", 4, 1},
		{"i64.trunc_sat_f32_u", 5, 1},
		{"i64.trunc_sat_f64_s", 6, 1},
		{"i64.trunc_sat_f64_u", 7, 1},

		// Bulk memory
		{"memory.init", 8, 3},
		{"data.drop", 9, 0},
		{"memory.copy", 10, 3},
		{"memory.fill", 11, 3},

		// Table operations
		{"table.init", 12, 3},
		{"elem.drop", 13, 0},
		{"table.copy", 14, 5},
		{"table.grow", 15, 2},
		{"table.size", 16, 0},
		{"table.fill", 17, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, ok := LookupPrefixed(tt.name)
			if !ok {
				t.Fatalf("LookupPrefixed(%q) not found", tt.name)
			}
			if op.Subop != tt.subop {
				t.Errorf("subop = %d, want %d", op.Subop, tt.subop)
			}
			if op.Operands != tt.operands {
				t.Errorf("operands = %d, want %d", op.Operands, tt.operands)
			}
		})
	}
}

func TestAllOpcodesCovered(t *testing.T) {
	// Core WASM 2.0 instructions that should be in the table
	coreInstructions := []string{
		// Control
		"unreachable", "nop", "return", "br", "br_if", "call",
		// Variables
		"local.get", "local.set", "local.tee", "global.get", "global.set",
		// Constants
		"i32.const", "i64.const", "f32.const", "f64.const",
		// i32 ops
		"i32.eqz", "i32.eq", "i32.ne", "i32.lt_s", "i32.lt_u",
		"i32.gt_s", "i32.gt_u", "i32.le_s", "i32.le_u", "i32.ge_s", "i32.ge_u",
		"i32.clz", "i32.ctz", "i32.popcnt",
		"i32.add", "i32.sub", "i32.mul", "i32.div_s", "i32.div_u",
		"i32.rem_s", "i32.rem_u", "i32.and", "i32.or", "i32.xor",
		"i32.shl", "i32.shr_s", "i32.shr_u", "i32.rotl", "i32.rotr",
		// i64 ops
		"i64.eqz", "i64.eq", "i64.ne", "i64.lt_s", "i64.lt_u",
		"i64.gt_s", "i64.gt_u", "i64.le_s", "i64.le_u", "i64.ge_s", "i64.ge_u",
		"i64.clz", "i64.ctz", "i64.popcnt",
		"i64.add", "i64.sub", "i64.mul", "i64.div_s", "i64.div_u",
		"i64.rem_s", "i64.rem_u", "i64.and", "i64.or", "i64.xor",
		"i64.shl", "i64.shr_s", "i64.shr_u", "i64.rotl", "i64.rotr",
		// f32 ops
		"f32.eq", "f32.ne", "f32.lt", "f32.gt", "f32.le", "f32.ge",
		"f32.abs", "f32.neg", "f32.ceil", "f32.floor", "f32.trunc", "f32.nearest", "f32.sqrt",
		"f32.add", "f32.sub", "f32.mul", "f32.div", "f32.min", "f32.max", "f32.copysign",
		// f64 ops
		"f64.eq", "f64.ne", "f64.lt", "f64.gt", "f64.le", "f64.ge",
		"f64.abs", "f64.neg", "f64.ceil", "f64.floor", "f64.trunc", "f64.nearest", "f64.sqrt",
		"f64.add", "f64.sub", "f64.mul", "f64.div", "f64.min", "f64.max", "f64.copysign",
		// Conversions
		"i32.wrap_i64",
		"i32.trunc_f32_s", "i32.trunc_f32_u", "i32.trunc_f64_s", "i32.trunc_f64_u",
		"i64.extend_i32_s", "i64.extend_i32_u",
		"i64.trunc_f32_s", "i64.trunc_f32_u", "i64.trunc_f64_s", "i64.trunc_f64_u",
		"f32.convert_i32_s", "f32.convert_i32_u", "f32.convert_i64_s", "f32.convert_i64_u",
		"f32.demote_f64",
		"f64.convert_i32_s", "f64.convert_i32_u", "f64.convert_i64_s", "f64.convert_i64_u",
		"f64.promote_f32",
		"i32.reinterpret_f32", "i64.reinterpret_f64",
		"f32.reinterpret_i32", "f64.reinterpret_i64",
		// Sign extension
		"i32.extend8_s", "i32.extend16_s",
		"i64.extend8_s", "i64.extend16_s", "i64.extend32_s",
		// Parametric
		"drop",
		// Memory
		"memory.size", "memory.grow",
		// Reference
		"ref.is_null",
	}

	for _, name := range coreInstructions {
		if _, ok := Lookup(name); !ok {
			t.Errorf("missing instruction: %s", name)
		}
	}
}
