package engine

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
)

// emitScopeTransfer runs under the caller's normal-execution policy. The
// complete input suffix is consumed once; validation-only source values have
// no runtime carrier write. Known values retain their exact operand encoding.
func emitScopeTransfer(ctx *handler.Context, transfer ir.ScopeTransfer) error {
	inputs, err := transferInputs(transfer.OperandCount(), transfer.Operand, ctx.Stack.Snapshot())
	if err != nil {
		return err
	}
	input := len(inputs)
	for index := transfer.OperandCount() - 1; index >= 0; index-- {
		operand, _ := transfer.Operand(index)
		if !operand.Present() {
			continue
		}
		input--
		if operand.Value() != 0 {
			ctx.Emit.Operand(inputs[input]).LocalSet(operand.Local())
		}
		ctx.Stack.Pop() // transferInputs proved the complete present suffix.
	}
	return ctx.Emit.Err()
}
