package semantics

import (
	"bytes"
	"context"
	encodingbinary "encoding/binary"
	"math"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestLiteralBitsThroughIndependentExecution(t *testing.T) {
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			config := wazero.NewRuntimeConfigCompiler()
			if backend == "interpreter" {
				config = wazero.NewRuntimeConfigInterpreter()
			}
			runtime := wazero.NewRuntimeWithConfig(ctx, config)
			defer runtime.Close(ctx)
			for _, tc := range []struct {
				name      string
				valueType wasm.ValType
				lo, hi    uint64
			}{
				{"i32-negative", wasm.ValI32, 0x87654321, 0}, {"i64-negative", wasm.ValI64, 0x87654321fedcba98, 0},
				{"f32-negative-zero", wasm.ValF32, 0x80000000, 0}, {"f32-nan-payload", wasm.ValF32, 0x7fc01234, 0}, {"f32-signaling-nan", wasm.ValF32, 0x7f801234, 0},
				{"f64-negative-zero", wasm.ValF64, 0x8000000000000000, 0}, {"f64-nan-payload", wasm.ValF64, 0x7ff8123456789abc, 0}, {"f64-signaling-nan", wasm.ValF64, 0x7ff0123456789abc, 0},
				{"vector-lanes", wasm.ValV128, 0x0123456789abcdef, 0xfedcba9876543210},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var expected [16]byte
					encodingbinary.LittleEndian.PutUint64(expected[:8], tc.lo)
					encodingbinary.LittleEndian.PutUint64(expected[8:], tc.hi)
					var instruction, store wasm.Instruction
					switch tc.valueType {
					case wasm.ValI32:
						instruction = wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: int32(tc.lo)}}
						store = wasm.Instruction{Opcode: wasm.OpI32Store, Imm: wasm.MemoryImm{}}
					case wasm.ValI64:
						instruction = wasm.Instruction{Opcode: wasm.OpI64Const, Imm: wasm.I64Imm{Value: int64(tc.lo)}}
						store = wasm.Instruction{Opcode: wasm.OpI64Store, Imm: wasm.MemoryImm{}}
					case wasm.ValF32:
						instruction = wasm.Instruction{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: math.Float32frombits(uint32(tc.lo))}}
						store = wasm.Instruction{Opcode: wasm.OpF32Store, Imm: wasm.MemoryImm{}}
					case wasm.ValF64:
						instruction = wasm.Instruction{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: math.Float64frombits(tc.lo)}}
						store = wasm.Instruction{Opcode: wasm.OpF64Store, Imm: wasm.MemoryImm{}}
					case wasm.ValV128:
						instruction = wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: append([]byte(nil), expected[:]...)}}
						store = wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Store, MemArg: &wasm.MemoryImm{}}}
					}
					literal, handled, err := ResolveLiteral(instruction)
					if err != nil || !handled || !literal.Valid() || literal.Type() != tc.valueType || literal.Bits() != expected {
						t.Fatalf("wrong literal %v %v", handled, err)
					}
					emitted, err := literal.Instruction()
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(wasm.EncodeInstructions([]wasm.Instruction{instruction}), wasm.EncodeInstructions([]wasm.Instruction{emitted})) {
						t.Fatal("literal encoding changed bits")
					}
					module := &wasm.Module{Types: []wasm.FuncType{{}}, Funcs: []uint32{0}, Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}}, Exports: []wasm.Export{{Name: "run", Kind: 0, Idx: 0}, {Name: "memory", Kind: 2, Idx: 0}}, Code: []wasm.FuncBody{{Code: wasm.EncodeInstructions([]wasm.Instruction{{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{}}, emitted, store, {Opcode: wasm.OpEnd}})}}}
					instance, err := runtime.Instantiate(ctx, module.Encode())
					if err != nil {
						t.Fatal(err)
					}
					defer instance.Close(ctx)
					if _, err := instance.ExportedFunction("run").Call(ctx); err != nil {
						t.Fatal(err)
					}
					actual, ok := instance.Memory().Read(0, 16)
					if !ok || !bytes.Equal(actual, expected[:]) {
						t.Fatalf("independent execution changed bits: %x != %x", actual, expected)
					}
				})
			}
		})
	}
}

func TestLiteralRejectsMalformedAndOwnsVectorBits(t *testing.T) {
	for _, instruction := range []wasm.Instruction{{Opcode: wasm.OpI32Const}, {Opcode: wasm.OpF64Const, Imm: wasm.I64Imm{}}, {Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: make([]byte, 15)}}} {
		literal, handled, err := ResolveLiteral(instruction)
		if !handled || err == nil || literal.Valid() {
			t.Fatal("accepted malformed literal")
		}
	}
	if _, handled, err := ResolveLiteral(wasm.Instruction{Opcode: wasm.OpI32Add}); handled || err != nil {
		t.Fatal("arithmetic was treated as literal")
	}
	input := make([]byte, 16)
	input[0] = 7
	literal, _, err := ResolveLiteral(wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: input}})
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 9
	emitted, err := literal.Instruction()
	if err != nil {
		t.Fatal(err)
	}
	emitted.Imm.(wasm.SIMDImm).V128Bytes[0] = 11
	if literal.Bits()[0] != 7 {
		t.Fatal("literal aliases input or output storage")
	}
	if _, err := (Literal{}).Instruction(); err == nil {
		t.Fatal("absent literal became a zero instruction")
	}
}
