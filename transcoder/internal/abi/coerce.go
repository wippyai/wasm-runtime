package abi

import "math"

// CoerceToUint32 handles JSON decoded numbers (float64) and other numeric types.
func CoerceToUint32(value any) (uint32, bool) {
	switch v := value.(type) {
	case uint32:
		return v, true
	case uint8:
		return uint32(v), true
	case uint16:
		return uint32(v), true
	case int8:
		if v >= 0 {
			return uint32(v), true
		}
	case int16:
		if v >= 0 {
			return uint32(v), true
		}
	case float64:
		if v >= 0 && v <= math.MaxUint32 && v == float64(uint32(v)) {
			return uint32(v), true
		}
	case float32:
		if v >= 0 && v <= math.MaxUint32 && v == float32(uint32(v)) {
			return uint32(v), true
		}
	case int:
		if v >= 0 && v <= math.MaxUint32 {
			return uint32(v), true
		}
	case int64:
		if v >= 0 && v <= math.MaxUint32 {
			return uint32(v), true
		}
	case uint:
		if v <= math.MaxUint32 {
			return uint32(v), true
		}
	case uint64:
		if v <= math.MaxUint32 {
			return uint32(v), true
		}
	case int32:
		if v >= 0 {
			return uint32(v), true
		}
	}
	return 0, false
}

func CoerceToInt32(value any) (int32, bool) {
	switch v := value.(type) {
	case int32:
		return v, true
	case int8:
		return int32(v), true
	case int16:
		return int32(v), true
	case uint8:
		return int32(v), true
	case uint16:
		return int32(v), true
	case float64:
		if v >= math.MinInt32 && v <= math.MaxInt32 && v == float64(int32(v)) {
			return int32(v), true
		}
	case float32:
		if v >= math.MinInt32 && v <= math.MaxInt32 && v == float32(int32(v)) {
			return int32(v), true
		}
	case int:
		if v >= math.MinInt32 && v <= math.MaxInt32 {
			return int32(v), true
		}
	case int64:
		if v >= math.MinInt32 && v <= math.MaxInt32 {
			return int32(v), true
		}
	case uint:
		if v <= math.MaxInt32 {
			return int32(v), true
		}
	case uint32:
		if v <= math.MaxInt32 {
			return int32(v), true
		}
	}
	return 0, false
}

func CoerceToUint64(value any) (uint64, bool) {
	switch v := value.(type) {
	case uint64:
		return v, true
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint:
		return uint64(v), true
	case int8:
		if v >= 0 {
			return uint64(v), true
		}
	case int16:
		if v >= 0 {
			return uint64(v), true
		}
	case int32:
		if v >= 0 {
			return uint64(v), true
		}
	case float64:
		if v >= 0 && v <= float64(math.MaxUint64) && v == float64(uint64(v)) {
			return uint64(v), true
		}
	case float32:
		// Use float64 for range check to avoid precision loss
		if v >= 0 && float64(v) <= float64(math.MaxUint64) && v == float32(uint64(v)) {
			return uint64(v), true
		}
	case int:
		if v >= 0 {
			return uint64(v), true
		}
	case int64:
		if v >= 0 {
			return uint64(v), true
		}
	}
	return 0, false
}

func CoerceToInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint:
		if v <= math.MaxInt64 {
			return int64(v), true
		}
	case uint64:
		if v <= math.MaxInt64 {
			return int64(v), true
		}
	case float64:
		if v >= float64(math.MinInt64) && v <= float64(math.MaxInt64) && v == float64(int64(v)) {
			return int64(v), true
		}
	case float32:
		if v >= float32(math.MinInt64) && v <= float32(math.MaxInt64) && v == float32(int64(v)) {
			return int64(v), true
		}
	}
	return 0, false
}
