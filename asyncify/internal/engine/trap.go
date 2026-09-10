package engine

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
)

// emitSourceTrap ends execution, while lexical validation retains the source
// operands below the active frame. The caller owns normal/rewind guarding.
func emitSourceTrap(ctx *handler.Context, operation ir.TrapOperation) error {
	prefix, err := retainedPrefix(operation.PrefixCount(), operation.PrefixOperand, ctx.Stack.Snapshot())
	if err != nil {
		return err
	}
	ctx.Emit.Unreachable()
	ctx.Stack.Restore(prefix)
	return ctx.Emit.Err()
}
