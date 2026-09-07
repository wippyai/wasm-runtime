package codegen

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestEmitterOperandReadsStoredLocalZero(t *testing.T) {
	e := NewEmitter()
	e.Operand(semantics.StoredOperand(0, wasm.ValI32))
	if err := e.Err(); err != nil {
		t.Fatalf("emit stored local zero: %v", err)
	}

	instructions, err := wasm.DecodeInstructions(e.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(instructions) != 1 || instructions[0].Opcode != wasm.OpLocalGet {
		t.Fatalf("stored local zero emitted %#v, want one local.get", instructions)
	}
	if index := instructions[0].Imm.(wasm.LocalImm).LocalIdx; index != 0 {
		t.Fatalf("local.get index = %d, want 0", index)
	}
}

func TestEmitterOperandPreservesExactLiteralEncoding(t *testing.T) {
	vector := []byte{0x00, 0xff, 0x10, 0x80, 0x55, 0xaa, 0x12, 0x21, 0x34, 0x43, 0x56, 0x65, 0x78, 0x87, 0x9a, 0xa9}
	for _, source := range []wasm.Instruction{
		{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: float32FromBits(0x7f801234)}},         // signaling NaN payload
		{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: float64FromBits(0x8000000000000000)}}, // negative zero
		{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: vector}},
	} {
		t.Run(sourceName(source), func(t *testing.T) {
			literal, handled, err := semantics.ResolveLiteral(source)
			if err != nil || !handled {
				t.Fatalf("resolve source literal: handled=%t err=%v", handled, err)
			}
			e := NewEmitter()
			e.Operand(semantics.LiteralOperand(literal))
			if err := e.Err(); err != nil {
				t.Fatalf("emit literal operand: %v", err)
			}
			want := wasm.EncodeInstructions([]wasm.Instruction{source})
			if got := e.Bytes(); !bytes.Equal(got, want) {
				t.Fatalf("literal encoding changed\n got: %x\nwant: %x", got, want)
			}
		})
	}
}

func TestEmitterOperandAbsentLatchesErrorWithoutFabricatingLocalRead(t *testing.T) {
	e := NewEmitter()
	e.Operand(semantics.Operand{}).Operand(semantics.StoredOperand(0, wasm.ValI32))
	if err := e.Err(); err == nil || !strings.Contains(err.Error(), "absent operand") {
		t.Fatalf("absent operand error = %v", err)
	}
	if got := e.Bytes(); len(got) != 0 {
		t.Fatalf("absent operand emitted bytes %x", got)
	}

	e.Reset().Operand(semantics.StoredOperand(0, wasm.ValI32))
	if err := e.Err(); err != nil {
		t.Fatalf("reset did not clear operand error: %v", err)
	}
	if got := e.Bytes(); !bytes.Equal(got, wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}}})) {
		t.Fatalf("reset did not restore emission: %x", got)
	}
}

func TestEmitterPoolClearsOperandErrorAndBytes(t *testing.T) {
	e := GetEmitter()
	e.Operand(semantics.Operand{})
	if e.Err() == nil {
		t.Fatal("absent operand did not set error before pool return")
	}
	PutEmitter(e)

	next := GetEmitter()
	defer PutEmitter(next)
	if err := next.Err(); err != nil {
		t.Fatalf("pooled emitter retained operand error: %v", err)
	}
	if got := next.Bytes(); len(got) != 0 {
		t.Fatalf("pooled emitter retained bytes: %x", got)
	}
}

// Keeping these bit conversions local makes the test's exact bit patterns
// visible without routing the values through arithmetic or formatting.
func float32FromBits(bits uint32) float32 { return math.Float32frombits(bits) }
func float64FromBits(bits uint64) float64 { return math.Float64frombits(bits) }

func sourceName(instruction wasm.Instruction) string {
	switch instruction.Opcode {
	case wasm.OpF32Const:
		return "f32-signaling-nan"
	case wasm.OpF64Const:
		return "f64-negative-zero"
	default:
		return "v128-lanes"
	}
}

func TestValidationOperandHasNoRuntimeStorageRead(t *testing.T) {
	for _, vt := range []wasm.ValType{wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64, wasm.ValV128} {
		value := semantics.ValidationOperand(vt)
		if value.Type() != vt || !value.ValidationOnly() {
			t.Fatal("lost validation type")
		}
		if _, stored := value.LocalIndex(); stored {
			t.Fatal("validation value acquired storage")
		}
		if _, literal := value.Literal(); literal {
			t.Fatal("validation value became a fabricated literal")
		}
		e := NewEmitter()
		e.Operand(value)
		if e.Err() != nil {
			t.Fatal(e.Err())
		}
		code, err := wasm.DecodeInstructions(e.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 1 || code[0].Opcode != wasm.OpUnreachable {
			t.Fatalf("validation read must trap: %#v", code)
		}
	}
}
