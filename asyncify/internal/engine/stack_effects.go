package engine

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/wasm"
)

// StackEffect is an alias for handler.StackEffect.
type StackEffect = handler.StackEffect

// GetStackEffectFromRegistry queries the registry for handlers implementing StackEffecter
// or StackEffectWith, then falls back to the static table. This is the preferred function
// as it ensures consistency between simulation and code generation.
func GetStackEffectFromRegistry(reg *handler.Registry, op byte, instr wasm.Instruction, module *wasm.Module) *StackEffect {
	if h := reg.Get(op); h != nil {
		// Check for instruction-dependent stack effects first (prefix handlers)
		if sew, ok := h.(handler.StackEffectWith); ok {
			if eff := sew.StackEffectWith(instr); eff != nil {
				return eff
			}
		}
		// Check for static stack effects
		if se, ok := h.(handler.StackEffecter); ok {
			eff := se.StackEffect()
			return &eff
		}
	}
	return GetStackEffect(op, instr, module)
}

// GetStackEffect returns the stack effect for a given opcode using the static table.
// Returns nil for instructions with complex/dynamic stack effects (calls, control flow).
// NOTE: Arithmetic/comparison/unary ops are NOT here - they're handled by
// BinaryOpHandler/UnaryOpHandler which implement StackEffecter.
func GetStackEffect(op byte, instr wasm.Instruction, module *wasm.Module) *StackEffect {
	switch op {
	// Constants
	case wasm.OpI32Const:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpI64Const:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValI64}}
	case wasm.OpF32Const:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValF32}}
	case wasm.OpF64Const:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValF64}}

	// Drop
	case wasm.OpDrop:
		return &StackEffect{Pops: 1, Pushes: nil}

	// Conversions
	case wasm.OpI32WrapI64:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpI64ExtendI32S, wasm.OpI64ExtendI32U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI64}}
	case wasm.OpI32TruncF32S, wasm.OpI32TruncF32U, wasm.OpI32TruncF64S, wasm.OpI32TruncF64U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpI64TruncF32S, wasm.OpI64TruncF32U, wasm.OpI64TruncF64S, wasm.OpI64TruncF64U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI64}}
	case wasm.OpF32ConvertI32S, wasm.OpF32ConvertI32U, wasm.OpF32ConvertI64S, wasm.OpF32ConvertI64U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF32}}
	case wasm.OpF64ConvertI32S, wasm.OpF64ConvertI32U, wasm.OpF64ConvertI64S, wasm.OpF64ConvertI64U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF64}}
	case wasm.OpF32DemoteF64:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF32}}
	case wasm.OpF64PromoteF32:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF64}}
	case wasm.OpI32ReinterpretF32:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpI64ReinterpretF64:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI64}}
	case wasm.OpF32ReinterpretI32:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF32}}
	case wasm.OpF64ReinterpretI64:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF64}}

	// Sign extension
	case wasm.OpI32Extend8S, wasm.OpI32Extend16S:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpI64Extend8S, wasm.OpI64Extend16S, wasm.OpI64Extend32S:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI64}}

	// Memory loads
	case wasm.OpI32Load, wasm.OpI32Load8S, wasm.OpI32Load8U, wasm.OpI32Load16S, wasm.OpI32Load16U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpI64Load, wasm.OpI64Load8S, wasm.OpI64Load8U, wasm.OpI64Load16S, wasm.OpI64Load16U, wasm.OpI64Load32S, wasm.OpI64Load32U:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI64}}
	case wasm.OpF32Load:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF32}}
	case wasm.OpF64Load:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValF64}}

	// Memory stores
	case wasm.OpI32Store, wasm.OpI32Store8, wasm.OpI32Store16:
		return &StackEffect{Pops: 2, Pushes: nil}
	case wasm.OpI64Store, wasm.OpI64Store8, wasm.OpI64Store16, wasm.OpI64Store32:
		return &StackEffect{Pops: 2, Pushes: nil}
	case wasm.OpF32Store:
		return &StackEffect{Pops: 2, Pushes: nil}
	case wasm.OpF64Store:
		return &StackEffect{Pops: 2, Pushes: nil}

	// Memory size/grow
	case wasm.OpMemorySize:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpMemoryGrow:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}

	// Reference types
	case wasm.OpRefIsNull:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValI32}}
	case wasm.OpRefFunc:
		return &StackEffect{Pops: 0, Pushes: []wasm.ValType{wasm.ValFuncRef}}
	case wasm.OpRefEq:
		return &StackEffect{Pops: 2, Pushes: []wasm.ValType{wasm.ValI32}}

	// Table operations
	case wasm.OpTableGet:
		return &StackEffect{Pops: 1, Pushes: []wasm.ValType{wasm.ValFuncRef}}
	case wasm.OpTableSet:
		return &StackEffect{Pops: 2, Pushes: nil}
	}

	return nil // dynamic or control flow
}
