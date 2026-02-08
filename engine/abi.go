package engine

import (
	"github.com/wippyai/wasm-runtime/transcoder"
	"go.bytecodealliance.org/wit"
)

const (
	MaxFlatResults = transcoder.MaxFlatResults

	CabiRealloc = "cabi_realloc"
	CabiFree    = "cabi_free"

	// Legacy names from pre-standardization component model implementations
	legacyRealloc = "canonical_abi_realloc"
	legacyAlloc   = "allocate"
	simpleAlloc   = "alloc"
	legacyDealloc = "deallocate"
	simpleFree    = "free"
)

var flatCount = transcoder.GetFlatCount

func flatResultCount(resultTypes []wit.Type) int {
	count := 0
	for _, rt := range resultTypes {
		count += flatCount(rt)
	}
	return count
}

func usesRetptr(resultTypes []wit.Type) bool {
	return flatResultCount(resultTypes) > MaxFlatResults
}

func resultSize(t wit.Type) uint32 {
	switch v := t.(type) {
	case wit.String:
		return 8 // ptr + len
	case wit.U8, wit.S8, wit.Bool:
		return 1
	case wit.U16, wit.S16:
		return 2
	case wit.U32, wit.S32, wit.F32, wit.Char:
		return 4
	case wit.U64, wit.S64, wit.F64:
		return 8
	case *wit.TypeDef:
		lc := transcoder.NewLayoutCalculator()
		layout := lc.Calculate(v)
		return layout.Size
	default:
		return 8
	}
}
