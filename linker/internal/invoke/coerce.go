package invoke

import (
	"fmt"
	"math"
)

// CoerceToUint64 converts a Go value to uint64 for WASM function calls.
// Supports: all integer types, float64, float32, bool.
func CoerceToUint64(arg any) (uint64, error) {
	switch v := arg.(type) {
	case uint64:
		return v, nil
	case uint32:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case int32:
		return uint64(v), nil
	case int16:
		return uint64(v), nil
	case int8:
		return uint64(v), nil
	case int:
		return uint64(v), nil
	case float64:
		return math.Float64bits(v), nil
	case float32:
		return uint64(math.Float32bits(v)), nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("cannot coerce %T to uint64", arg)
	}
}

// CoerceArgs converts a slice of Go values to uint64 slice.
func CoerceArgs(args []any) ([]uint64, error) {
	result := make([]uint64, len(args))
	for i, arg := range args {
		v, err := CoerceToUint64(arg)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		result[i] = v
	}
	return result, nil
}

// ResultsToAny converts uint64 results to []any.
func ResultsToAny(results []uint64) []any {
	anyResults := make([]any, len(results))
	for i, r := range results {
		anyResults[i] = r
	}
	return anyResults
}
