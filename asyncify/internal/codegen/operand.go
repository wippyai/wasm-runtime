package codegen

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
)

// Operand reads a typed value through its explicit location kind. Invalid
// operands record an emission error without manufacturing a local-zero read.
func (e *Emitter) Operand(value semantics.Operand) *Emitter {
	if e.err != nil {
		return e
	}
	if value.ValidationOnly() {
		// A validation-only value never authorizes a fabricated runtime read.
		return e.Unreachable()
	}
	if local, ok := value.LocalIndex(); ok {
		return e.LocalGet(local)
	}
	if literal, ok := value.Literal(); ok {
		instruction, err := literal.Instruction()
		if err != nil {
			e.err = err
			return e
		}
		return e.EmitInstr(instruction)
	}
	e.err = fmt.Errorf("asyncify: cannot emit absent operand")
	return e
}
func (e *Emitter) Err() error { return e.err }
