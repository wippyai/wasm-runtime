package semantics

import (
	"slices"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestScalarSignatureRepresentativeCases(t *testing.T) {
	tests := []struct {
		name       string
		wantInputs []wasm.ValType
		wantOutput []wasm.ValType
		opcode     byte
	}{
		// i64.eqz -> i32 (testop producing i32 from i64)
		{
			name:       "i64.eqz -> i32",
			opcode:     wasm.OpI64Eqz,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},

		// Float comparisons (binary relops producing i32)
		{
			name:       "f32.eq",
			opcode:     wasm.OpF32Eq,
			wantInputs: []wasm.ValType{wasm.ValF32, wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f32.ne",
			opcode:     wasm.OpF32Ne,
			wantInputs: []wasm.ValType{wasm.ValF32, wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f32.lt",
			opcode:     wasm.OpF32Lt,
			wantInputs: []wasm.ValType{wasm.ValF32, wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f32.gt",
			opcode:     wasm.OpF32Gt,
			wantInputs: []wasm.ValType{wasm.ValF32, wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f32.le",
			opcode:     wasm.OpF32Le,
			wantInputs: []wasm.ValType{wasm.ValF32, wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f32.ge",
			opcode:     wasm.OpF32Ge,
			wantInputs: []wasm.ValType{wasm.ValF32, wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f64.eq",
			opcode:     wasm.OpF64Eq,
			wantInputs: []wasm.ValType{wasm.ValF64, wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f64.ne",
			opcode:     wasm.OpF64Ne,
			wantInputs: []wasm.ValType{wasm.ValF64, wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f64.lt",
			opcode:     wasm.OpF64Lt,
			wantInputs: []wasm.ValType{wasm.ValF64, wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f64.gt",
			opcode:     wasm.OpF64Gt,
			wantInputs: []wasm.ValType{wasm.ValF64, wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f64.le",
			opcode:     wasm.OpF64Le,
			wantInputs: []wasm.ValType{wasm.ValF64, wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "f64.ge",
			opcode:     wasm.OpF64Ge,
			wantInputs: []wasm.ValType{wasm.ValF64, wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},

		// Every conversion direction between scalar types:
		// 1. i32 -> i64
		{
			name:       "i64.extend_i32_s",
			opcode:     wasm.OpI64ExtendI32S,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.extend_i32_u",
			opcode:     wasm.OpI64ExtendI32U,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		// 2. i32 -> f32
		{
			name:       "f32.convert_i32_s",
			opcode:     wasm.OpF32ConvertI32S,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValF32},
		},
		{
			name:       "f32.convert_i32_u",
			opcode:     wasm.OpF32ConvertI32U,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValF32},
		},
		{
			name:       "f32.reinterpret_i32",
			opcode:     wasm.OpF32ReinterpretI32,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValF32},
		},
		// 3. i32 -> f64
		{
			name:       "f64.convert_i32_s",
			opcode:     wasm.OpF64ConvertI32S,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValF64},
		},
		{
			name:       "f64.convert_i32_u",
			opcode:     wasm.OpF64ConvertI32U,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValF64},
		},
		// 4. i64 -> i32
		{
			name:       "i32.wrap_i64",
			opcode:     wasm.OpI32WrapI64,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		// 5. i64 -> f32
		{
			name:       "f32.convert_i64_s",
			opcode:     wasm.OpF32ConvertI64S,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValF32},
		},
		{
			name:       "f32.convert_i64_u",
			opcode:     wasm.OpF32ConvertI64U,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValF32},
		},
		// 6. i64 -> f64
		{
			name:       "f64.convert_i64_s",
			opcode:     wasm.OpF64ConvertI64S,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValF64},
		},
		{
			name:       "f64.convert_i64_u",
			opcode:     wasm.OpF64ConvertI64U,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValF64},
		},
		{
			name:       "f64.reinterpret_i64",
			opcode:     wasm.OpF64ReinterpretI64,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValF64},
		},
		// 7. f32 -> i32
		{
			name:       "i32.trunc_f32_s",
			opcode:     wasm.OpI32TruncF32S,
			wantInputs: []wasm.ValType{wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.trunc_f32_u",
			opcode:     wasm.OpI32TruncF32U,
			wantInputs: []wasm.ValType{wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.reinterpret_f32",
			opcode:     wasm.OpI32ReinterpretF32,
			wantInputs: []wasm.ValType{wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		// 8. f32 -> i64
		{
			name:       "i64.trunc_f32_s",
			opcode:     wasm.OpI64TruncF32S,
			wantInputs: []wasm.ValType{wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.trunc_f32_u",
			opcode:     wasm.OpI64TruncF32U,
			wantInputs: []wasm.ValType{wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		// 9. f32 -> f64
		{
			name:       "f64.promote_f32",
			opcode:     wasm.OpF64PromoteF32,
			wantInputs: []wasm.ValType{wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValF64},
		},
		// 10. f64 -> i32
		{
			name:       "i32.trunc_f64_s",
			opcode:     wasm.OpI32TruncF64S,
			wantInputs: []wasm.ValType{wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.trunc_f64_u",
			opcode:     wasm.OpI32TruncF64U,
			wantInputs: []wasm.ValType{wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		// 11. f64 -> i64
		{
			name:       "i64.trunc_f64_s",
			opcode:     wasm.OpI64TruncF64S,
			wantInputs: []wasm.ValType{wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.trunc_f64_u",
			opcode:     wasm.OpI64TruncF64U,
			wantInputs: []wasm.ValType{wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.reinterpret_f64",
			opcode:     wasm.OpI64ReinterpretF64,
			wantInputs: []wasm.ValType{wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		// 12. f64 -> f32
		{
			name:       "f32.demote_f64",
			opcode:     wasm.OpF32DemoteF64,
			wantInputs: []wasm.ValType{wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValF32},
		},

		// Division
		{
			name:       "i32.div_s",
			opcode:     wasm.OpI32DivS,
			wantInputs: []wasm.ValType{wasm.ValI32, wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.div_u",
			opcode:     wasm.OpI32DivU,
			wantInputs: []wasm.ValType{wasm.ValI32, wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i64.div_s",
			opcode:     wasm.OpI64DivS,
			wantInputs: []wasm.ValType{wasm.ValI64, wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.div_u",
			opcode:     wasm.OpI64DivU,
			wantInputs: []wasm.ValType{wasm.ValI64, wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "f32.div",
			opcode:     wasm.OpF32Div,
			wantInputs: []wasm.ValType{wasm.ValF32, wasm.ValF32},
			wantOutput: []wasm.ValType{wasm.ValF32},
		},
		{
			name:       "f64.div",
			opcode:     wasm.OpF64Div,
			wantInputs: []wasm.ValType{wasm.ValF64, wasm.ValF64},
			wantOutput: []wasm.ValType{wasm.ValF64},
		},

		// Shifts & Rotates (testing both i32 and i64; note i64 shifts take i64 shift count)
		{
			name:       "i32.shl",
			opcode:     wasm.OpI32Shl,
			wantInputs: []wasm.ValType{wasm.ValI32, wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.shr_s",
			opcode:     wasm.OpI32ShrS,
			wantInputs: []wasm.ValType{wasm.ValI32, wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.shr_u",
			opcode:     wasm.OpI32ShrU,
			wantInputs: []wasm.ValType{wasm.ValI32, wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.rotl",
			opcode:     wasm.OpI32Rotl,
			wantInputs: []wasm.ValType{wasm.ValI32, wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.rotr",
			opcode:     wasm.OpI32Rotr,
			wantInputs: []wasm.ValType{wasm.ValI32, wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i64.shl",
			opcode:     wasm.OpI64Shl,
			wantInputs: []wasm.ValType{wasm.ValI64, wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.shr_s",
			opcode:     wasm.OpI64ShrS,
			wantInputs: []wasm.ValType{wasm.ValI64, wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.shr_u",
			opcode:     wasm.OpI64ShrU,
			wantInputs: []wasm.ValType{wasm.ValI64, wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.rotl",
			opcode:     wasm.OpI64Rotl,
			wantInputs: []wasm.ValType{wasm.ValI64, wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.rotr",
			opcode:     wasm.OpI64Rotr,
			wantInputs: []wasm.ValType{wasm.ValI64, wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},

		// Sign extensions
		{
			name:       "i32.extend8_s",
			opcode:     wasm.OpI32Extend8S,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i32.extend16_s",
			opcode:     wasm.OpI32Extend16S,
			wantInputs: []wasm.ValType{wasm.ValI32},
			wantOutput: []wasm.ValType{wasm.ValI32},
		},
		{
			name:       "i64.extend8_s",
			opcode:     wasm.OpI64Extend8S,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.extend16_s",
			opcode:     wasm.OpI64Extend16S,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
		{
			name:       "i64.extend32_s",
			opcode:     wasm.OpI64Extend32S,
			wantInputs: []wasm.ValType{wasm.ValI64},
			wantOutput: []wasm.ValType{wasm.ValI64},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inputs, outputs, handled := ScalarSignature(tc.opcode)
			if !handled {
				t.Fatalf("expected opcode %#02x (%s) to be handled", tc.opcode, tc.name)
			}
			if !slices.Equal(inputs, tc.wantInputs) {
				t.Errorf("inputs mismatch for %s: got %v, want %v", tc.name, inputs, tc.wantInputs)
			}
			if !slices.Equal(outputs, tc.wantOutput) {
				t.Errorf("outputs mismatch for %s: got %v, want %v", tc.name, outputs, tc.wantOutput)
			}
		})
	}
}

func TestScalarSignatureReturnedSlicesDoNotAliasGlobalState(t *testing.T) {
	// Mutating the returned slices from one call must not alter subsequent results.
	inputs1, outputs1, handled := ScalarSignature(wasm.OpI32Add)
	if !handled {
		t.Fatalf("expected OpI32Add to be handled")
	}
	inputs1[0] = wasm.ValF64
	inputs1[1] = wasm.ValF32
	outputs1[0] = wasm.ValI64

	inputs2, outputs2, handled := ScalarSignature(wasm.OpI32Add)
	if !handled {
		t.Fatalf("expected OpI32Add to be handled on second call")
	}
	if inputs2[0] != wasm.ValI32 || inputs2[1] != wasm.ValI32 {
		t.Fatalf("inputs slice aliased mutable state: got %v", inputs2)
	}
	if outputs2[0] != wasm.ValI32 {
		t.Fatalf("outputs slice aliased mutable state: got %v", outputs2)
	}

	// Repeat check for unary conversions (e.g. i64.eqz)
	inEqz1, outEqz1, handled := ScalarSignature(wasm.OpI64Eqz)
	if !handled {
		t.Fatalf("expected OpI64Eqz to be handled")
	}
	inEqz1[0] = wasm.ValF64
	outEqz1[0] = wasm.ValF64

	inEqz2, outEqz2, handled := ScalarSignature(wasm.OpI64Eqz)
	if !handled {
		t.Fatalf("expected OpI64Eqz to be handled on second call")
	}
	if inEqz2[0] != wasm.ValI64 || outEqz2[0] != wasm.ValI32 {
		t.Fatalf("unary slices aliased mutable state: in=%v, out=%v", inEqz2, outEqz2)
	}
}

func TestScalarSignatureUnhandledInstructions(t *testing.T) {
	unhandled := []struct {
		name   string
		opcode byte
	}{
		// Constants (explicitly excluded)
		{"i32.const", wasm.OpI32Const},
		{"i64.const", wasm.OpI64Const},
		{"f32.const", wasm.OpF32Const},
		{"f64.const", wasm.OpF64Const},

		// Multi-byte prefixes (SIMD, prefixed saturation/misc, GC, atomics)
		{"prefix.simd", wasm.OpPrefixSIMD},
		{"prefix.misc", wasm.OpPrefixMisc},
		{"prefix.gc", wasm.OpPrefixGC},
		{"prefix.atomic", wasm.OpPrefixAtomic},

		// Parametric instructions
		{"drop", wasm.OpDrop},
		{"select", wasm.OpSelect},
		{"select_t", wasm.OpSelectType},

		// Variable instructions
		{"local.get", wasm.OpLocalGet},
		{"local.set", wasm.OpLocalSet},
		{"local.tee", wasm.OpLocalTee},
		{"global.get", wasm.OpGlobalGet},
		{"global.set", wasm.OpGlobalSet},

		// Memory instructions
		{"i32.load", wasm.OpI32Load},
		{"i64.load", wasm.OpI64Load},
		{"f32.load", wasm.OpF32Load},
		{"f64.load", wasm.OpF64Load},
		{"i32.store", wasm.OpI32Store},
		{"i64.store", wasm.OpI64Store},
		{"memory.size", wasm.OpMemorySize},
		{"memory.grow", wasm.OpMemoryGrow},

		// Control flow
		{"unreachable", wasm.OpUnreachable},
		{"nop", wasm.OpNop},
		{"block", wasm.OpBlock},
		{"loop", wasm.OpLoop},
		{"if", wasm.OpIf},
		{"else", wasm.OpElse},
		{"end", wasm.OpEnd},
		{"br", wasm.OpBr},
		{"br_if", wasm.OpBrIf},
		{"br_table", wasm.OpBrTable},
		{"return", wasm.OpReturn},
		{"call", wasm.OpCall},
		{"call_indirect", wasm.OpCallIndirect},

		// References
		{"ref.null", wasm.OpRefNull},
		{"ref.is_null", wasm.OpRefIsNull},
		{"ref.func", wasm.OpRefFunc},
	}

	for _, tc := range unhandled {
		t.Run(tc.name, func(t *testing.T) {
			inputs, outputs, handled := ScalarSignature(tc.opcode)
			if handled {
				t.Fatalf("expected opcode %#02x (%s) to NOT be handled as base scalar numeric, got inputs=%v, outputs=%v",
					tc.opcode, tc.name, inputs, outputs)
			}
			if inputs != nil || outputs != nil {
				t.Fatalf("expected nil slices for unhandled opcode %#02x, got inputs=%v, outputs=%v",
					tc.opcode, inputs, outputs)
			}
		})
	}
}

func TestScalarSignatureExhaustiveByteCoverage(t *testing.T) {
	// WebAssembly base scalar numeric opcodes span exactly 0x45 through 0xC4 (128 opcodes).
	var handledCount int
	for b := 0; b <= 0xFF; b++ {
		op := byte(b)
		inputs, outputs, handled := ScalarSignature(op)
		if handled {
			handledCount++
			if len(outputs) != 1 {
				t.Fatalf("opcode %#02x: expected exactly 1 output, got %d", op, len(outputs))
			}
			if len(inputs) < 1 || len(inputs) > 2 {
				t.Fatalf("opcode %#02x: expected 1 or 2 inputs, got %d", op, len(inputs))
			}
		} else if inputs != nil || outputs != nil {
			t.Fatalf("unhandled opcode %#02x returned non-nil slices: inputs=%v, outputs=%v", op, inputs, outputs)
		}
	}

	const wantHandledCount = 128
	if handledCount != wantHandledCount {
		t.Fatalf("expected %d handled scalar opcodes, got %d", wantHandledCount, handledCount)
	}
}
