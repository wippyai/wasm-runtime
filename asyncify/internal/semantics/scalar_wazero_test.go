package semantics

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestScalarSignaturesValidateIndependently(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)
	// The core binary grammar assigns these 128 consecutive bytes to base
	// numeric instructions. No operand/result type is inferred from that range.
	for opcode := wasm.OpI32Eqz; opcode <= wasm.OpI64Extend32S; opcode++ {
		inputs, outputs, handled := ScalarSignature(opcode)
		if !handled {
			t.Fatalf("missing numeric opcode 0x%x", opcode)
		}
		instructions := make([]wasm.Instruction, 0, len(inputs)+2)
		for i := range inputs {
			instructions = append(instructions, wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: uint32(i)}})
		}
		instructions = append(instructions, wasm.Instruction{Opcode: opcode}, wasm.Instruction{Opcode: wasm.OpEnd})
		module := &wasm.Module{Types: []wasm.FuncType{{Params: inputs, Results: outputs}}, Funcs: []uint32{0}, Code: []wasm.FuncBody{{Code: wasm.EncodeInstructions(instructions)}}}
		compiled, err := runtime.CompileModule(ctx, module.Encode())
		if err != nil {
			t.Fatalf("signature of opcode 0x%x disagrees with independent validation: %v", opcode, err)
		}
		compiled.Close(ctx)
	}
}
