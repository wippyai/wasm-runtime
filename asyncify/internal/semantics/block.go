package semantics

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// BlockSignature owns a snapshot of the operand types of a structured control
// instruction. Params enter the body; Results leave it. A loop branch targets
// Params, while a block/if branch targets Results (Wasm core validation rules).
type BlockSignature struct {
	Params  []wasm.ValType
	Results []wasm.ValType
}

// ResolveBlockType resolves the blocktype representation, never treating an
// unknown type as void. Type indices refer to the flattened module type space,
// including recursive groups and non-function entries. Returned slices are owned
// by the caller and do not alias module metadata.
func ResolveBlockType(blockType int32, module *wasm.Module) (BlockSignature, error) {
	var vt wasm.ValType
	switch blockType {
	case -64:
		return BlockSignature{}, nil
	case -1:
		vt = wasm.ValI32
	case -2:
		vt = wasm.ValI64
	case -3:
		vt = wasm.ValF32
	case -4:
		vt = wasm.ValF64
	case -5:
		vt = wasm.ValV128
	case -16:
		vt = wasm.ValFuncRef
	case -17:
		vt = wasm.ValExtern
	case -13:
		vt = wasm.ValNullFuncRef
	case -14:
		vt = wasm.ValNullExternRef
	case -15:
		vt = wasm.ValNullRef
	case -18:
		vt = wasm.ValAnyRef
	case -19:
		vt = wasm.ValEqRef
	case -20:
		vt = wasm.ValI31Ref
	case -21:
		vt = wasm.ValStructRef
	case -22:
		vt = wasm.ValArrayRef
	default:
		if blockType < 0 || module == nil {
			return BlockSignature{}, fmt.Errorf("asyncify: invalid or unresolved block type %d", blockType)
		}
		ft := module.GetFuncTypeByTypeIndex(uint32(blockType))
		if ft == nil {
			return BlockSignature{}, fmt.Errorf("asyncify: block type %d is not a function signature", blockType)
		}
		return BlockSignature{Params: slices.Clone(ft.Params), Results: slices.Clone(ft.Results)}, nil
	}
	return BlockSignature{Results: []wasm.ValType{vt}}, nil
}
