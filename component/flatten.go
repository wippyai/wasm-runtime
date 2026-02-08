package component

import (
	"strings"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component/internal/arena"
	"github.com/wippyai/wasm-runtime/transcoder"
	"go.bytecodealliance.org/wit"
)

// Canonical ABI flattening limits
const (
	MaxFlatParams  = transcoder.MaxFlatParams
	MaxFlatResults = transcoder.MaxFlatResults
)

// CoreValType is a core wasm value type
type CoreValType = api.ValueType

// FlattenTypes flattens WIT types to core wasm types
func FlattenTypes(types []wit.Type) []CoreValType {
	var result []CoreValType
	for _, t := range types {
		result = append(result, FlattenType(t)...)
	}
	return result
}

// FlattenFuncTypeData flattens a FuncTypeData, applying MAX_FLAT_PARAMS/RESULTS limits
func FlattenFuncTypeData(a *arena.TypeArena, ft *arena.FuncTypeData, context string) (flatParams []CoreValType, flatResults []CoreValType, err error) {
	// Flatten params
	for _, p := range ft.Params {
		resolved, err := a.ResolveValType(p.Type)
		if err != nil {
			return nil, nil, err
		}
		flat := flattenArenaType(resolved)
		flatParams = append(flatParams, flat...)
	}

	// Flatten result
	if ft.Result != nil {
		resolved, err := a.ResolveValType(*ft.Result)
		if err != nil {
			return nil, nil, err
		}
		flatResults = flattenArenaType(resolved)
	}

	if len(flatParams) > MaxFlatParams {
		flatParams = []CoreValType{api.ValueTypeI32}
	}

	if len(flatResults) > MaxFlatResults {
		if strings.HasPrefix(context, "lift") {
			flatResults = []CoreValType{api.ValueTypeI32}
		} else if strings.HasPrefix(context, "lower") {
			flatParams = append(flatParams, api.ValueTypeI32)
			flatResults = nil
		}
	}

	return flatParams, flatResults, nil
}

// FlattenType flattens a WIT type to core wasm types
func FlattenType(t wit.Type) []CoreValType {
	if t == nil {
		return nil
	}

	switch v := t.(type) {
	// Primitive types
	case wit.Bool, wit.U8, wit.U16, wit.U32, wit.S8, wit.S16, wit.S32, wit.Char:
		return []CoreValType{api.ValueTypeI32}
	case wit.U64, wit.S64:
		return []CoreValType{api.ValueTypeI64}
	case wit.F32:
		return []CoreValType{api.ValueTypeF32}
	case wit.F64:
		return []CoreValType{api.ValueTypeF64}
	case wit.String:
		return []CoreValType{api.ValueTypeI32, api.ValueTypeI32} // ptr, len

	case *wit.TypeDef:
		return flattenTypeDef(v)

	default:
		return flattenArenaType(t)
	}
}

// flattenArenaType handles arena-resolved types
func flattenArenaType(t interface{}) []CoreValType {
	switch v := t.(type) {
	case arena.ResolvedBool, arena.ResolvedU8, arena.ResolvedU16, arena.ResolvedU32, arena.ResolvedS8, arena.ResolvedS16, arena.ResolvedS32, arena.ResolvedChar:
		return []CoreValType{api.ValueTypeI32}
	case arena.ResolvedU64, arena.ResolvedS64:
		return []CoreValType{api.ValueTypeI64}
	case arena.ResolvedF32:
		return []CoreValType{api.ValueTypeF32}
	case arena.ResolvedF64:
		return []CoreValType{api.ValueTypeF64}
	case arena.ResolvedString:
		return []CoreValType{api.ValueTypeI32, api.ValueTypeI32}

	case arena.Record:
		var flat []CoreValType
		for _, field := range v.Fields {
			flat = append(flat, flattenArenaType(field.Type)...)
		}
		return flat

	case arena.List:
		return []CoreValType{api.ValueTypeI32, api.ValueTypeI32} // ptr, len

	case arena.Tuple:
		var flat []CoreValType
		for _, elem := range v.Types {
			flat = append(flat, flattenArenaType(elem)...)
		}
		return flat

	case arena.Option:
		discrim := []CoreValType{api.ValueTypeI32}
		if v.Type != nil {
			return append(discrim, flattenArenaType(v.Type)...)
		}
		return discrim

	case arena.Result:
		discrim := []CoreValType{api.ValueTypeI32}
		var payload []CoreValType
		if v.OK != nil {
			payload = flattenArenaType(v.OK)
		}
		if v.Err != nil {
			errFlat := flattenArenaType(v.Err)
			for i, ft := range errFlat {
				if i < len(payload) {
					payload[i] = joinTypes(payload[i], ft)
				} else {
					payload = append(payload, ft)
				}
			}
		}
		return append(discrim, payload...)

	case arena.Flags:
		if v.Count > 32 {
			return []CoreValType{api.ValueTypeI64}
		}
		return []CoreValType{api.ValueTypeI32}

	case arena.Enum:
		return []CoreValType{api.ValueTypeI32}

	case arena.Variant:
		discrim := discriminantType(len(v.Cases))
		var payload []CoreValType
		for _, c := range v.Cases {
			if c.Type != nil {
				caseFlat := flattenArenaType(c.Type)
				for i, ft := range caseFlat {
					if i < len(payload) {
						payload[i] = joinTypes(payload[i], ft)
					} else {
						payload = append(payload, ft)
					}
				}
			}
		}
		return append(discrim, payload...)

	default:
		return []CoreValType{api.ValueTypeI32}
	}
}

// flattenTypeDef flattens a TypeDef
func flattenTypeDef(td *wit.TypeDef) []CoreValType {
	if td == nil || td.Kind == nil {
		return []CoreValType{api.ValueTypeI32}
	}

	switch kind := td.Kind.(type) {
	case *wit.Record:
		return flattenRecord(kind)
	case *wit.List:
		return flattenList(kind)
	case *wit.Tuple:
		return flattenTuple(kind)
	case *wit.Variant:
		return flattenVariant(kind)
	case *wit.Enum:
		return []CoreValType{api.ValueTypeI32} // discriminant only
	case *wit.Option:
		return flattenOption(kind)
	case *wit.Result:
		return flattenResult(kind)
	case *wit.Flags:
		return flattenFlags(kind)
	case *wit.Own, *wit.Borrow:
		return []CoreValType{api.ValueTypeI32} // resource handle
	case wit.String:
		return []CoreValType{api.ValueTypeI32, api.ValueTypeI32}
	// Primitives wrapped in TypeDef
	case wit.Bool, wit.U8, wit.U16, wit.U32, wit.S8, wit.S16, wit.S32, wit.Char:
		return []CoreValType{api.ValueTypeI32}
	case wit.U64, wit.S64:
		return []CoreValType{api.ValueTypeI64}
	case wit.F32:
		return []CoreValType{api.ValueTypeF32}
	case wit.F64:
		return []CoreValType{api.ValueTypeF64}
	default:
		return []CoreValType{api.ValueTypeI32}
	}
}

// flattenRecord flattens record fields
func flattenRecord(r *wit.Record) []CoreValType {
	var flat []CoreValType
	for _, field := range r.Fields {
		flat = append(flat, FlattenType(field.Type)...)
	}
	return flat
}

// flattenList returns (ptr, len) for dynamic lists
func flattenList(l *wit.List) []CoreValType {
	return []CoreValType{api.ValueTypeI32, api.ValueTypeI32}
}

// flattenTuple flattens tuple elements
func flattenTuple(t *wit.Tuple) []CoreValType {
	var flat []CoreValType
	for _, elem := range t.Types {
		flat = append(flat, FlattenType(elem)...)
	}
	return flat
}

// flattenVariant flattens to discriminant + union of case payloads
func flattenVariant(v *wit.Variant) []CoreValType {
	discrim := discriminantType(len(v.Cases))
	var payload []CoreValType
	for _, c := range v.Cases {
		if c.Type != nil {
			caseFlat := FlattenType(c.Type)
			for i, ft := range caseFlat {
				if i < len(payload) {
					payload[i] = joinTypes(payload[i], ft)
				} else {
					payload = append(payload, ft)
				}
			}
		}
	}

	return append(discrim, payload...)
}

// flattenOption flattens option<T> as discriminant + T
func flattenOption(o *wit.Option) []CoreValType {
	discrim := []CoreValType{api.ValueTypeI32}
	if o.Type != nil {
		return append(discrim, FlattenType(o.Type)...)
	}
	return discrim
}

// flattenResult flattens result<T, E> as discriminant + union(T, E)
func flattenResult(r *wit.Result) []CoreValType {
	discrim := []CoreValType{api.ValueTypeI32}
	var payload []CoreValType
	if r.OK != nil {
		payload = FlattenType(r.OK)
	}
	if r.Err != nil {
		errFlat := FlattenType(r.Err)
		for i, ft := range errFlat {
			if i < len(payload) {
				payload[i] = joinTypes(payload[i], ft)
			} else {
				payload = append(payload, ft)
			}
		}
	}

	return append(discrim, payload...)
}

// flattenFlags flattens to i32 (<=32 flags) or i64 (>32 flags)
func flattenFlags(f *wit.Flags) []CoreValType {
	if len(f.Flags) > 32 {
		return []CoreValType{api.ValueTypeI64}
	}
	return []CoreValType{api.ValueTypeI32}
}

// discriminantType returns i32 for any discriminant (spec requires at most u32)
func discriminantType(numCases int) []CoreValType {
	// All discriminants fit in i32 per canonical ABI
	return []CoreValType{api.ValueTypeI32}
}

// joinTypes unions two core types for variant payloads
func joinTypes(a, b CoreValType) CoreValType {
	if a == b {
		return a
	}
	// 32-bit types can share storage
	if (a == api.ValueTypeI32 && b == api.ValueTypeF32) ||
		(a == api.ValueTypeF32 && b == api.ValueTypeI32) {
		return api.ValueTypeI32
	}
	// Different sizes require i64
	return api.ValueTypeI64
}
