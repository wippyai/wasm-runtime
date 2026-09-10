package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
)

// verifyContinuationOperands bridges the source operand contract to materialized
// storage. Every saved operand must retain its exact source identity, type,
// literal (when applicable), and guest execution storage domain.
func verifyContinuationOperands(source *ir.Continuation, length int, at func(int) (stackEntry, bool), allocator *TempAllocator) error {
	if source == nil || source.OperandCount() != length {
		return fmt.Errorf("operand count disagrees with source")
	}
	for index := 0; index < length; index++ {
		entry, exists := at(index)
		if !exists {
			return fmt.Errorf("source operand %d is missing", index)
		}
		identity, bound := entry.Binding()
		if !bound || identity != uint64(source.OperandValue(index)) {
			return fmt.Errorf("operand %d source identity disagrees with continuation", index)
		}
		expected := source.OperandType(index)
		if expected != 0 && expected != entry.Type() {
			return fmt.Errorf("operand %d type %v disagrees with source %v", index, entry.Type(), expected)
		}
		if local, stored := entry.LocalIndex(); stored {
			if domain, ok := allocator.storage.domains[local]; ok && domain != semantics.GuestExecution {
				return fmt.Errorf("source operand %d occupies rewind routing storage", index)
			}
		} else if literal, ok := entry.Literal(); ok {
			expectedLiteral, known := source.OperandLiteral(index)
			if !known || literal != expectedLiteral {
				return fmt.Errorf("operand %d literal disagrees with source", index)
			}
		} else if !entry.ValidationOnly() {
			return fmt.Errorf("source operand %d is absent", index)
		}
	}
	return nil
}
