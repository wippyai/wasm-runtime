package handler

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// checkScalarBinding prevents a registry entry from disagreeing with the source
// signature. Failure precedes any operand consumption or instruction emission.
func checkScalarBinding(instruction wasm.Instruction, opcode byte, result wasm.ValType, arity int) error {
	inputs, outputs, handled := semantics.ScalarSignature(instruction.Opcode)
	if !handled || opcode != instruction.Opcode || len(inputs) != arity || len(outputs) != 1 || outputs[0] != result {
		return fmt.Errorf("asyncify: scalar handler binding disagrees with opcode 0x%x", instruction.Opcode)
	}
	return nil
}

func scalarStackEffect(opcode byte) StackEffect {
	inputs, outputs, _ := semantics.ScalarSignature(opcode)
	return StackEffect{Pops: len(inputs), Pushes: outputs}
}
