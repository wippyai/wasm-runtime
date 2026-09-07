package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/wasm"
)

// transferInput names source facts without conflating source identities with
// temporary storage definitions. Capture and scope transfers share this input
// check while retaining their different execution policies.
type transferInput interface {
	Value() ir.ValueID
	Present() bool
	Type() wasm.ValType
}

// transferInputs checks presence and concrete types, not exact source ValueID
// equality on the virtual stack; that remains the bound-stack phase.
func transferInputs[T transferInput](count int, at func(int) (T, bool), stack []stackEntry) ([]stackEntry, error) {
	present := 0
	for i := range count {
		operand, ok := at(i)
		if !ok {
			return nil, fmt.Errorf("invalid transfer operand %d", i)
		}
		if operand.Present() {
			present++
		}
	}
	if present > len(stack) {
		return nil, fmt.Errorf("transfer requires %d present operands, stack has %d", present, len(stack))
	}
	inputs := stack[len(stack)-present:]
	index := 0
	for i := range count {
		operand, _ := at(i)
		if !operand.Present() {
			continue
		}
		// Source zero is a validation-only polymorphic operand, never a
		// concrete runtime definition to reinterpret or write into a cell.
		if operand.Value() != 0 && inputs[index].Type() != operand.Type() {
			return nil, fmt.Errorf("transfer operand %d has type %v, want %v", i, inputs[index].Type(), operand.Type())
		}
		index++
	}
	return inputs, nil
}

func emitEntryCapture(ctx *handler.Context, capture ir.EntryCapture) error {
	inputs, err := transferInputs(capture.OperandCount(), capture.Operand, ctx.Stack.Snapshot())
	if err != nil {
		return err
	}
	// Virtual stack operands are consumed once for both execution paths. Only
	// normal execution reads them into the carrier cells. Rewind preserves the
	// cells restored by the prelude. This private guard is balanced locally.
	guardOpen := false
	input := len(inputs)
	for i := capture.OperandCount() - 1; i >= 0; i-- {
		operand, _ := capture.Operand(i)
		if !operand.Present() {
			continue
		}
		input--
		if operand.Value() != 0 {
			if !guardOpen {
				ctx.Emit.StateCheck(capture.StateGlobal(), capture.StateRewinding()).I32Eqz().If(codegen.BlockVoid)
				guardOpen = true
			}
			ctx.Emit.Operand(inputs[input]).LocalSet(operand.Local())
		}
		ctx.Stack.Pop() // transferInputs proved the complete present suffix.
	}
	if guardOpen {
		ctx.Emit.End()
	}
	return ctx.Emit.Err()
}

// retainedPrefix verifies an enclosing source validation prefix without
// manufacturing values. Exact source identity on stored operands is a separate
// bound-stack proof; this boundary checks count and concrete types.
func retainedPrefix[T interface {
	Value() ir.ValueID
	Type() wasm.ValType
}](count int, at func(int) (T, bool), stack []stackEntry) ([]stackEntry, error) {
	if count > len(stack) {
		return nil, fmt.Errorf("control prefix exceeds stack")
	}
	for i := 0; i < count; i++ {
		expected, ok := at(i)
		if !ok || (expected.Value() != 0 && expected.Type() != stack[i].Type()) {
			return nil, fmt.Errorf("control prefix %d type mismatch", i)
		}
	}
	return stack[:count], nil
}
