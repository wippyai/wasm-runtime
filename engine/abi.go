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

// callStackSize returns the wazero stack length for a call with paramSlots
// flat parameters: the stack carries the parameters in and the flat results,
// or the return-area pointer, out.
func callStackSize(paramSlots int, resultTypes []wit.Type) int {
	resultSlots := 0
	switch {
	case usesRetptr(resultTypes):
		resultSlots = 1
	case len(resultTypes) > 0:
		resultSlots = flatResultCount(resultTypes)
	}
	return max(paramSlots, resultSlots)
}
