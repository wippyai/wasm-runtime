package wasm

import (
	"github.com/tetratelabs/wazero/api"
)

// EncodeULEB128 encodes an unsigned value in LEB128 format.
func EncodeULEB128(v uint32) []byte {
	var result []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		result = append(result, b)
		if v == 0 {
			break
		}
	}
	return result
}

// EncodeSLEB128 encodes a signed value in LEB128 format.
func EncodeSLEB128[T int32 | int64](v T) []byte {
	var result []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			result = append(result, b)
			break
		}
		result = append(result, b|0x80)
	}
	return result
}

// DecodeULEB128 decodes an unsigned LEB128 value.
func DecodeULEB128(data []byte) (uint32, int) {
	var result uint32
	var shift uint32
	for i, b := range data {
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1
		}
		shift += 7
		if shift > 35 {
			return result, i + 1
		}
	}
	return result, len(data)
}

// ValTypeToWasm converts a wazero value type to WASM encoding.
func ValTypeToWasm(t api.ValueType) byte {
	switch t {
	case api.ValueTypeI32:
		return 0x7f
	case api.ValueTypeI64:
		return 0x7e
	case api.ValueTypeF32:
		return 0x7d
	case api.ValueTypeF64:
		return 0x7c
	default:
		return 0x7f
	}
}

// ParseValType converts a WASM encoding to wazero value type.
func ParseValType(b byte) api.ValueType {
	switch b {
	case 0x7F:
		return api.ValueTypeI32
	case 0x7E:
		return api.ValueTypeI64
	case 0x7D:
		return api.ValueTypeF32
	case 0x7C:
		return api.ValueTypeF64
	default:
		return api.ValueTypeI32
	}
}
