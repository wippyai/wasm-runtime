package engine

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
)

// returnPrefix checks the retained validation prefix without manufacturing
// values. Exact source identities on stored operands remain a separate proof.
func returnPrefix(operation ir.ReturnOperation, stack []stackEntry) ([]stackEntry, error) {
	return retainedPrefix(operation.PrefixCount(), operation.PrefixOperand, stack)
}

// emitSourceReturn uses the source function's result suffix, not the entire
// virtual stack. The caller owns normal/rewind guarding. A validation-unreachable
// source return cannot execute: emit a trap instead of reading fabricated values.
func emitSourceReturn(ctx *handler.Context, operation ir.ReturnOperation) error {
	stack := ctx.Stack.Snapshot()
	prefix, err := returnPrefix(operation, stack)
	if err != nil {
		return err
	}

	if !operation.ValidationReachable() {
		ctx.Emit.Unreachable()
		ctx.Stack.Restore(prefix)
		return ctx.Emit.Err()
	}
	results, err := transferInputs(operation.OperandCount(), operation.Operand, stack[len(prefix):])
	if err != nil {
		return err
	}
	for _, value := range results {
		ctx.Emit.Operand(value)
	}
	ctx.Emit.Return()
	ctx.Stack.Restore(prefix)
	return ctx.Emit.Err()
}
