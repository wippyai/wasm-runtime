// Package invoke handles function invocation for WebAssembly Component Model.
//
// It provides utilities for calling component functions with proper
// Canonical ABI encoding/decoding and type coercion.
package invoke

import (
	"go.bytecodealliance.org/wit"
)

// FlatCount returns the number of flat (i32/i64/f32/f64) values needed for a WIT type.
// FlatCount is used to determine if the result uses the retptr pattern.
func FlatCount(t wit.Type) int {
	if t == nil {
		return 0
	}
	switch t := t.(type) {
	case wit.Bool, wit.U8, wit.S8, wit.U16, wit.S16, wit.U32, wit.S32, wit.Char:
		return 1
	case wit.U64, wit.S64:
		return 1
	case wit.F32, wit.F64:
		return 1
	case wit.String:
		return 2 // ptr + len
	case *wit.TypeDef:
		if t == nil {
			return 0
		}
		return flatCountForTypeDef(t)
	default:
		return 1
	}
}

func flatCountForTypeDef(t *wit.TypeDef) int {
	if t.Kind == nil {
		return 0
	}
	switch k := t.Kind.(type) {
	case *wit.Record:
		if k == nil {
			return 0
		}
		count := 0
		for _, f := range k.Fields {
			count += FlatCount(f.Type)
		}
		return count
	case *wit.List:
		return 2 // ptr + len
	case *wit.Tuple:
		if k == nil {
			return 0
		}
		count := 0
		for _, tt := range k.Types {
			count += FlatCount(tt)
		}
		return count
	case *wit.Option:
		if k == nil {
			return 1
		}
		return 1 + FlatCount(k.Type)
	case *wit.Result:
		if k == nil {
			return 1
		}
		maxPayload := 0
		if k.OK != nil {
			if c := FlatCount(k.OK); c > maxPayload {
				maxPayload = c
			}
		}
		if k.Err != nil {
			if c := FlatCount(k.Err); c > maxPayload {
				maxPayload = c
			}
		}
		return 1 + maxPayload
	case *wit.Variant:
		if k == nil {
			return 1
		}
		maxPayload := 0
		for _, c := range k.Cases {
			if c.Type != nil {
				if count := FlatCount(c.Type); count > maxPayload {
					maxPayload = count
				}
			}
		}
		return 1 + maxPayload
	case *wit.Enum:
		return 1
	case *wit.Flags:
		// Flags with >32 flags need 2 i32 values
		if k != nil && len(k.Flags) > 32 {
			return 2
		}
		return 1
	case wit.Type:
		return FlatCount(k)
	default:
		return 1
	}
}

// TotalFlatCount returns the total flat count for multiple types.
func TotalFlatCount(types []wit.Type) int {
	count := 0
	for _, t := range types {
		count += FlatCount(t)
	}
	return count
}

// UsesRetptr returns true if the result types require the retptr pattern.
// UsesRetptr indicates the retptr pattern is used when the total flat count exceeds 1.
func UsesRetptr(resultTypes []wit.Type) bool {
	return TotalFlatCount(resultTypes) > 1
}
