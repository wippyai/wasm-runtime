package transcoder

import (
	"math"
	"reflect"
	"strconv"
	"unicode/utf8"
	"unsafe"

	"github.com/wippyai/wasm-runtime/errors"
	"github.com/wippyai/wasm-runtime/transcoder/internal/abi"
	"go.bytecodealliance.org/wit"
)

// Canonical ABI limits for flat encoding
const (
	MaxFlatParams  = 16
	MaxFlatResults = 1
)

// Safety limits to prevent DoS attacks and memory exhaustion.
const (
	MaxStringSize = abi.MaxStringSize // Maximum string size (16 MB)
	MaxListLength = abi.MaxListLength // Maximum list length (1M elements)
	MaxAlloc      = abi.MaxAlloc      // Maximum allocation size (1 GB)
)

// Local wrappers for abi package functions - kept for internal use
var (
	safeMulU32     = abi.SafeMulU32
	typeName       = abi.TypeName
	coerceToUint32 = abi.CoerceToUint32
	coerceToInt32  = abi.CoerceToInt32
	coerceToUint64 = abi.CoerceToUint64
	coerceToInt64  = abi.CoerceToInt64
)

type Encoder struct {
	compiler *Compiler
}

func NewEncoder() *Encoder {
	return &Encoder{
		compiler: NewCompiler(),
	}
}

func NewEncoderWithCompiler(c *Compiler) *Encoder {
	return &Encoder{compiler: c}
}

// EncodeParams uses compiled fast path for structs and typed slices when possible.
func (e *Encoder) EncodeParams(paramTypes []wit.Type, values []any, mem Memory, alloc Allocator, allocList *AllocationList) ([]uint64, error) {
	if len(paramTypes) != len(values) {
		return nil, errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Detail("parameter count mismatch: expected %d, got %d", len(paramTypes), len(values)).
			Build()
	}

	flat := getBuf64()
	defer putBuf64(flat)

	for i, paramType := range paramTypes {
		// Try compiled fast path for typed values
		if e.tryFastEncode(paramType, values[i], flat, mem, alloc, allocList) {
			continue
		}

		// Fall back to dynamic path
		if err := e.flattenValue(paramType, values[i], mem, alloc, allocList, flat, nil); err != nil {
			return nil, errors.New(errors.PhaseEncode, errors.KindInvalidData).
				Path("param["+strconv.Itoa(i)+"]").
				Detail("failed to encode parameter %d: %v", i, err).
				Build()
		}
	}

	if len(*flat) > MaxFlatParams {
		return nil, errors.New(errors.PhaseEncode, errors.KindOverflow).
			Detail("flattened parameters exceed MAX_FLAT_PARAMS (%d > %d)", len(*flat), MaxFlatParams).
			Build()
	}

	// Return a copy since we're returning the buffer to pool
	result := make([]uint64, len(*flat))
	copy(result, *flat)
	return result, nil
}

// tryFastEncode returns true if handled via compiled path, false to fall back to dynamic path.
func (e *Encoder) tryFastEncode(witType wit.Type, value any, flat *[]uint64, mem Memory, alloc Allocator, allocList *AllocationList) bool {
	typeDef, ok := witType.(*wit.TypeDef)
	if !ok {
		return false
	}

	val := reflect.ValueOf(value)

	// Structs
	if _, isRecord := typeDef.Kind.(*wit.Record); isRecord && val.Kind() == reflect.Struct {
		compiled, err := e.compiler.Compile(typeDef, val.Type())
		if err == nil {
			// Get pointer from interface
			valueInterface := (*[2]unsafe.Pointer)(unsafe.Pointer(&value))
			ptr := valueInterface[1]

			// Lower directly to flat buffer, tracking allocations
			consumed, err := e.LowerToStackTracked(compiled, ptr, (*flat)[len(*flat):cap(*flat)], mem, alloc, allocList)
			if err == nil {
				*flat = (*flat)[:len(*flat)+consumed]
				return true
			}
		}
	}

	// Typed slices ([]int32, []byte, etc)
	if _, isList := typeDef.Kind.(*wit.List); isList && val.Kind() == reflect.Slice {
		compiled, err := e.compiler.Compile(typeDef, val.Type())
		if err == nil {
			valueInterface := (*[2]unsafe.Pointer)(unsafe.Pointer(&value))
			ptr := valueInterface[1]

			consumed, err := e.LowerToStackTracked(compiled, ptr, (*flat)[len(*flat):cap(*flat)], mem, alloc, allocList)
			if err == nil {
				*flat = (*flat)[:len(*flat)+consumed]
				return true
			}
		}
	}

	return false
}

func (e *Encoder) encodeFieldToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	switch ct.Kind {
	case KindBool:
		v := *(*bool)(ptr)
		var b uint8
		if v {
			b = 1
		}
		return mem.WriteU8(addr, b)

	case KindU8:
		return mem.WriteU8(addr, *(*uint8)(ptr))

	case KindS8:
		return mem.WriteU8(addr, uint8(*(*int8)(ptr)))

	case KindU16:
		return mem.WriteU16(addr, *(*uint16)(ptr))

	case KindS16:
		return mem.WriteU16(addr, uint16(*(*int16)(ptr)))

	case KindU32:
		return mem.WriteU32(addr, *(*uint32)(ptr))

	case KindS32:
		return mem.WriteU32(addr, uint32(*(*int32)(ptr)))

	case KindU64:
		return mem.WriteU64(addr, *(*uint64)(ptr))

	case KindS64:
		return mem.WriteU64(addr, uint64(*(*int64)(ptr)))

	case KindF32:
		bits := math.Float32bits(*(*float32)(ptr))
		return mem.WriteU32(addr, abi.CanonicalizeF32(bits))

	case KindF64:
		bits := math.Float64bits(*(*float64)(ptr))
		return mem.WriteU64(addr, abi.CanonicalizeF64(bits))

	case KindChar:
		r := *(*rune)(ptr)
		if !abi.ValidateChar(r) {
			return errors.New(errors.PhaseEncode, errors.KindInvalidData).
				Path(path...).
				Detail("invalid Unicode scalar value: 0x%X", r).
				Build()
		}
		return mem.WriteU32(addr, uint32(r))

	case KindString:
		s := *(*string)(ptr)
		return e.encodeStringToMemory(addr, s, mem, alloc, allocList, path)

	case KindRecord:
		return e.encodeRecordToMemory(addr, ct, ptr, mem, alloc, allocList, path)

	case KindList:
		return e.encodeListToMemory(addr, ct, ptr, mem, alloc, allocList, path)

	case KindOption:
		return e.encodeOptionToMemory(addr, ct, ptr, mem, alloc, allocList, path)

	case KindResult:
		return e.encodeResultToMemory(addr, ct, ptr, mem, alloc, allocList, path)

	case KindTuple:
		return e.encodeTupleToMemory(addr, ct, ptr, mem, alloc, allocList, path)

	case KindVariant:
		return e.encodeVariantToMemory(addr, ct, ptr, mem, alloc, allocList, path)

	case KindEnum:
		return e.encodeEnumToMemory(addr, ct, ptr, mem)

	case KindFlags:
		return e.encodeFlagsToMemory(addr, ct, ptr, mem)

	case KindOwn, KindBorrow:
		handle := *(*uint32)(ptr)
		return mem.WriteU32(addr, handle)

	default:
		return errors.Unsupported(errors.PhaseEncode, "type kind: "+ct.Kind.String())
	}
}

func (e *Encoder) encodeStringToMemory(addr uint32, s string, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	if !utf8.ValidString(s) {
		return errors.InvalidUTF8(errors.PhaseEncode, path, []byte(s))
	}

	dataLen := uint32(len(s))
	if dataLen > MaxStringSize {
		return errors.New(errors.PhaseEncode, errors.KindOverflow).
			Path(path...).
			Detail("string size %d exceeds maximum %d", dataLen, MaxStringSize).
			Build()
	}

	if dataLen == 0 {
		// Empty string: ptr=0, len=0
		if err := mem.WriteU32(addr, 0); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, 0)
	}

	// Allocate space for string data
	dataAddr, err := alloc.Alloc(dataLen, 1)
	if err != nil {
		return errors.New(errors.PhaseEncode, errors.KindAllocation).
			Path(path...).
			Detail("failed to allocate %d bytes for string data", dataLen).
			Build()
	}
	if allocList != nil {
		allocList.Add(dataAddr, dataLen, 1)
	}

	// Write string bytes without allocation
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	if err := mem.Write(dataAddr, data); err != nil {
		return err
	}

	// Write ptr and len
	if err := mem.WriteU32(addr, dataAddr); err != nil {
		return err
	}
	return mem.WriteU32(addr+4, dataLen)
}

func (e *Encoder) encodeRecordToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	for _, field := range ct.Fields {
		fieldPtr := unsafe.Add(ptr, field.GoOffset)
		if err := e.encodeFieldToMemory(addr+field.WitOffset, field.Type, fieldPtr, mem, alloc, allocList, nil); err != nil {
			if path != nil {
				fieldPath := append(append([]string{}, path...), field.WitName)
				return errors.New(errors.PhaseEncode, errors.KindInvalidData).
					Path(fieldPath...).
					Detail("failed to encode field %s: %v", field.WitName, err).
					Build()
			}
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeListToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	// Get slice info using reflect (avoids deprecated SliceHeader)
	sliceVal := reflect.NewAt(reflect.SliceOf(ct.ElemType.GoType), ptr).Elem()
	length := uint32(sliceVal.Len())

	if length > MaxListLength {
		return errors.New(errors.PhaseEncode, errors.KindOverflow).
			Path(path...).
			Detail("list length %d exceeds maximum %d", length, MaxListLength).
			Build()
	}

	if length == 0 {
		// Empty list: ptr=0, len=0
		if err := mem.WriteU32(addr, 0); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, 0)
	}

	// Allocate space for list data
	elemSize := ct.ElemType.WitSize
	dataSize, ok := safeMulU32(length, elemSize)
	if !ok || dataSize > MaxAlloc {
		return errors.New(errors.PhaseEncode, errors.KindOverflow).
			Path(path...).
			Detail("list data size overflow: %d * %d", length, elemSize).
			Build()
	}
	dataAddr, err := alloc.Alloc(dataSize, ct.ElemType.WitAlign)
	if err != nil {
		return errors.New(errors.PhaseEncode, errors.KindAllocation).
			Path(path...).
			Detail("failed to allocate %d bytes (align %d) for list data", dataSize, ct.ElemType.WitAlign).
			Build()
	}
	if allocList != nil {
		allocList.Add(dataAddr, dataSize, ct.ElemType.WitAlign)
	}

	// Fast path for primitive types - direct memory copy
	switch ct.ElemType.GoType.Kind() {
	case reflect.Uint8:
		// []byte - single bulk write
		src := unsafe.Slice((*byte)(unsafe.Pointer(sliceVal.Index(0).UnsafeAddr())), length)
		if err := mem.Write(dataAddr, src); err != nil {
			return err
		}
		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, length)
	case reflect.Int32, reflect.Uint32:
		src := unsafe.Slice((*byte)(unsafe.Pointer(sliceVal.Index(0).UnsafeAddr())), length*4)
		if err := mem.Write(dataAddr, src); err != nil {
			return err
		}
		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, length)
	case reflect.Int64, reflect.Uint64:
		src := unsafe.Slice((*byte)(unsafe.Pointer(sliceVal.Index(0).UnsafeAddr())), length*8)
		if err := mem.Write(dataAddr, src); err != nil {
			return err
		}
		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, length)
	case reflect.Float32:
		// Canonicalize NaN values per spec - cannot use fast path
		for i := uint32(0); i < length; i++ {
			bits := math.Float32bits(*(*float32)(unsafe.Pointer(sliceVal.Index(int(i)).UnsafeAddr())))
			if err := mem.WriteU32(dataAddr+i*4, abi.CanonicalizeF32(bits)); err != nil {
				return err
			}
		}
		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, length)
	case reflect.Float64:
		// Canonicalize NaN values per spec - cannot use fast path
		for i := uint32(0); i < length; i++ {
			bits := math.Float64bits(*(*float64)(unsafe.Pointer(sliceVal.Index(int(i)).UnsafeAddr())))
			if err := mem.WriteU64(dataAddr+i*8, abi.CanonicalizeF64(bits)); err != nil {
				return err
			}
		}
		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, length)
	}

	// Slow path for complex types - encode each element
	for i := uint32(0); i < length; i++ {
		elemPtr := unsafe.Pointer(sliceVal.Index(int(i)).UnsafeAddr())
		var elemPath []string
		if path != nil {
			elemPath = append(append([]string{}, path...), "["+strconv.FormatUint(uint64(i), 10)+"]")
		}
		if err := e.encodeFieldToMemory(dataAddr+i*elemSize, ct.ElemType, elemPtr, mem, alloc, allocList, elemPath); err != nil {
			return err
		}
	}

	// Write ptr and len
	if err := mem.WriteU32(addr, dataAddr); err != nil {
		return err
	}
	return mem.WriteU32(addr+4, length)
}

func (e *Encoder) encodeOptionToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	// ptr points to a Go pointer - check if nil
	goPtr := *(*unsafe.Pointer)(ptr)

	if goPtr == nil {
		// None: discriminant = 0
		return mem.WriteU8(addr, 0)
	}

	// Some: discriminant = 1
	if err := mem.WriteU8(addr, 1); err != nil {
		return err
	}

	// Payload offset
	payloadOffset := alignTo(1, ct.ElemType.WitAlign)
	if err := e.encodeFieldToMemory(addr+payloadOffset, ct.ElemType, goPtr, mem, alloc, allocList, nil); err != nil {
		if path != nil {
			somePath := append(append([]string{}, path...), "[some]")
			return errors.New(errors.PhaseEncode, errors.KindInvalidData).
				Path(somePath...).
				Detail("failed to encode option value: %v", err).
				Build()
		}
		return err
	}
	return nil
}

func (e *Encoder) encodeResultToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	// Result is represented as struct: { Ok *T; Err *E }
	type resultLayout struct {
		Ok  unsafe.Pointer
		Err unsafe.Pointer
	}
	r := (*resultLayout)(ptr)

	// Determine max alignment of payloads
	maxAlign := uint32(1)
	if ct.OkType != nil && ct.OkType.WitAlign > maxAlign {
		maxAlign = ct.OkType.WitAlign
	}
	if ct.ErrType != nil && ct.ErrType.WitAlign > maxAlign {
		maxAlign = ct.ErrType.WitAlign
	}
	payloadOffset := alignTo(1, maxAlign)

	if r.Ok != nil {
		if err := mem.WriteU8(addr, 0); err != nil {
			return err
		}
		if ct.OkType != nil {
			return e.encodeFieldToMemory(addr+payloadOffset, ct.OkType, r.Ok, mem, alloc, allocList, path)
		}
		return nil
	}

	if err := mem.WriteU8(addr, 1); err != nil {
		return err
	}
	if ct.ErrType != nil && r.Err != nil {
		return e.encodeFieldToMemory(addr+payloadOffset, ct.ErrType, r.Err, mem, alloc, allocList, path)
	}
	return nil
}

func (e *Encoder) encodeTupleToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	for _, field := range ct.Fields {
		fieldPtr := unsafe.Add(ptr, field.GoOffset)
		if err := e.encodeFieldToMemory(addr+field.WitOffset, field.Type, fieldPtr, mem, alloc, allocList, path); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeVariantToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	// Variant is represented as struct with pointer fields for each case
	discSize := abi.DiscriminantSize(len(ct.Cases))

	// Determine max alignment for payload offset
	maxAlign := discSize
	for _, c := range ct.Cases {
		if c.Type != nil && c.Type.WitAlign > maxAlign {
			maxAlign = c.Type.WitAlign
		}
	}
	payloadOffset := alignTo(discSize, maxAlign)

	// Find which case is active (non-nil)
	for i, c := range ct.Cases {
		caseField := (*unsafe.Pointer)(unsafe.Add(ptr, c.GoOffset))
		if *caseField != nil {
			// Write discriminant
			switch discSize {
			case 1:
				if err := mem.WriteU8(addr, uint8(i)); err != nil {
					return err
				}
			case 2:
				if err := mem.WriteU16(addr, uint16(i)); err != nil {
					return err
				}
			case 4:
				if err := mem.WriteU32(addr, uint32(i)); err != nil {
					return err
				}
			}
			// Write payload if not unit type
			if c.Type != nil {
				return e.encodeFieldToMemory(addr+payloadOffset, c.Type, *caseField, mem, alloc, allocList, path)
			}
			return nil
		}
	}
	return errors.New(errors.PhaseEncode, errors.KindInvalidData).
		Path(path...).
		Detail("variant has no active case").
		Build()
}

func (e *Encoder) encodeEnumToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory) error {
	// Enum is stored as an integer discriminant
	discSize := abi.DiscriminantSize(len(ct.Cases))

	// Read discriminant based on Go type size
	var disc uint32
	switch ct.GoSize {
	case 1:
		disc = uint32(*(*uint8)(ptr))
	case 2:
		disc = uint32(*(*uint16)(ptr))
	case 4:
		disc = *(*uint32)(ptr)
	case 8:
		disc = uint32(*(*uint64)(ptr))
	default:
		disc = *(*uint32)(ptr)
	}

	// Validate discriminant is in bounds
	if disc >= uint32(len(ct.Cases)) {
		return errors.InvalidDiscriminant(errors.PhaseEncode, nil, disc, uint32(len(ct.Cases)-1))
	}

	switch discSize {
	case 1:
		return mem.WriteU8(addr, uint8(disc))
	case 2:
		return mem.WriteU16(addr, uint16(disc))
	default:
		return mem.WriteU32(addr, disc)
	}
}

func (e *Encoder) encodeFlagsToMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory) error {
	numFlags := len(ct.Cases)

	if numFlags <= 8 {
		return mem.WriteU8(addr, *(*uint8)(ptr))
	} else if numFlags <= 16 {
		return mem.WriteU16(addr, *(*uint16)(ptr))
	} else if numFlags <= 32 {
		return mem.WriteU32(addr, *(*uint32)(ptr))
	} else if numFlags <= 64 {
		return mem.WriteU64(addr, *(*uint64)(ptr))
	}

	// >64 flags: multiple u32s per Canonical ABI spec
	numU32s := (numFlags + 31) / 32
	u32Ptr := (*uint32)(ptr)
	for i := 0; i < numU32s; i++ {
		word := *(*uint32)(unsafe.Add(unsafe.Pointer(u32Ptr), i*4))
		if err := mem.WriteU32(addr+uint32(i*4), word); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) flattenValue(witType wit.Type, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	switch t := witType.(type) {
	case wit.Bool:
		return e.flattenBool(value, flat, path)
	case wit.U8, wit.S8:
		return e.flattenU8(value, flat, path)
	case wit.U16, wit.S16:
		return e.flattenU16(value, flat, path)
	case wit.U32, wit.S32:
		return e.flattenU32(value, flat, path)
	case wit.U64, wit.S64:
		return e.flattenU64(value, flat, path)
	case wit.F32:
		return e.flattenF32(value, flat, path)
	case wit.F64:
		return e.flattenF64(value, flat, path)
	case wit.Char:
		return e.flattenChar(value, flat, path)
	case wit.String:
		return e.flattenString(value, mem, alloc, allocList, flat, path)
	case *wit.TypeDef:
		return e.flattenTypeDef(t, value, mem, alloc, allocList, flat, path)
	default:
		return errors.Unsupported(errors.PhaseEncode, "WIT type")
	}
}

func (e *Encoder) flattenBool(value any, flat *[]uint64, path []string) error {
	v, ok := value.(bool)
	if !ok {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "bool")
	}
	if v {
		*flat = append(*flat, 1)
	} else {
		*flat = append(*flat, 0)
	}
	return nil
}

func (e *Encoder) flattenU8(value any, flat *[]uint64, path []string) error {
	var v uint8
	switch val := value.(type) {
	case uint8:
		v = val
	case int8:
		v = uint8(val)
	default:
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint8/int8")
	}
	*flat = append(*flat, uint64(v))
	return nil
}

func (e *Encoder) flattenU16(value any, flat *[]uint64, path []string) error {
	var v uint16
	switch val := value.(type) {
	case uint16:
		v = val
	case int16:
		v = uint16(val)
	default:
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint16/int16")
	}
	*flat = append(*flat, uint64(v))
	return nil
}

func (e *Encoder) flattenU32(value any, flat *[]uint64, path []string) error {
	var v uint32
	switch val := value.(type) {
	case uint32:
		v = val
	case int32:
		v = uint32(val)
	case int64:
		v = uint32(val)
	case uint64:
		v = uint32(val)
	case int:
		v = uint32(val)
	case float64:
		v = uint32(val)
	default:
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint32/int32")
	}
	*flat = append(*flat, uint64(v))
	return nil
}

func (e *Encoder) flattenU64(value any, flat *[]uint64, path []string) error {
	var v uint64
	switch val := value.(type) {
	case uint64:
		v = val
	case int64:
		v = uint64(val)
	case uint32:
		v = uint64(val)
	case int32:
		v = uint64(val)
	case int:
		v = uint64(val)
	case float64:
		v = uint64(val)
	default:
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint64/int64")
	}
	*flat = append(*flat, v)
	return nil
}

func (e *Encoder) flattenF32(value any, flat *[]uint64, path []string) error {
	var v float32
	switch val := value.(type) {
	case float32:
		v = val
	case float64:
		v = float32(val)
	case int64:
		v = float32(val)
	case int:
		v = float32(val)
	default:
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "float32")
	}
	bits := abi.CanonicalizeF32(math.Float32bits(v))
	*flat = append(*flat, uint64(bits))
	return nil
}

func (e *Encoder) flattenF64(value any, flat *[]uint64, path []string) error {
	var v float64
	switch val := value.(type) {
	case float64:
		v = val
	case float32:
		v = float64(val)
	case int64:
		v = float64(val)
	case int:
		v = float64(val)
	default:
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "float64")
	}
	bits := abi.CanonicalizeF64(math.Float64bits(v))
	*flat = append(*flat, bits)
	return nil
}

func (e *Encoder) flattenChar(value any, flat *[]uint64, path []string) error {
	var r rune
	switch val := value.(type) {
	case rune: // rune is int32
		r = val
	case string:
		if len(val) == 0 {
			return errors.New(errors.PhaseEncode, errors.KindInvalidData).
				Path(path...).
				Detail("empty string cannot be converted to char").
				Build()
		}
		r, _ = utf8.DecodeRuneInString(val)
	default:
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "rune/int32")
	}
	if !abi.ValidateChar(r) {
		return errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Path(path...).
			Detail("invalid Unicode scalar value: 0x%X", r).
			Build()
	}
	*flat = append(*flat, uint64(r))
	return nil
}

func (e *Encoder) flattenString(value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	s, ok := value.(string)
	if !ok {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "string")
	}

	if !utf8.ValidString(s) {
		return errors.InvalidUTF8(errors.PhaseEncode, path, []byte(s))
	}

	dataLen := uint32(len(s))
	if dataLen > MaxStringSize {
		return errors.New(errors.PhaseEncode, errors.KindOverflow).
			Path(path...).
			Detail("string size %d exceeds maximum %d", dataLen, MaxStringSize).
			Build()
	}

	if dataLen == 0 {
		*flat = append(*flat, 0, 0) // ptr=0, len=0
		return nil
	}

	dataAddr, err := alloc.Alloc(dataLen, 1)
	if err != nil {
		return errors.New(errors.PhaseEncode, errors.KindAllocation).
			Path(path...).
			Detail("failed to allocate %d bytes for string data", dataLen).
			Build()
	}
	if allocList != nil {
		allocList.Add(dataAddr, dataLen, 1)
	}

	if err := mem.Write(dataAddr, []byte(s)); err != nil {
		return err
	}

	*flat = append(*flat, uint64(dataAddr), uint64(dataLen))
	return nil
}

func (e *Encoder) flattenTypeDef(t *wit.TypeDef, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	switch kind := t.Kind.(type) {
	case *wit.Record:
		return e.flattenRecord(kind, value, mem, alloc, allocList, flat, path)
	case *wit.List:
		return e.flattenList(kind, value, mem, alloc, allocList, flat, path)
	case *wit.Option:
		return e.flattenOption(kind, value, mem, alloc, allocList, flat, path)
	case *wit.Tuple:
		return e.flattenTuple(kind, value, mem, alloc, allocList, flat, path)
	case *wit.Enum:
		return e.flattenEnum(kind, value, flat, path)
	case *wit.Flags:
		return e.flattenFlags(kind, value, flat, path)
	case *wit.Result:
		return e.flattenResult(kind, value, mem, alloc, allocList, flat, path)
	case *wit.Variant:
		return e.flattenVariant(kind, value, mem, alloc, allocList, flat, path)
	case wit.Type:
		return e.flattenValue(kind, value, mem, alloc, allocList, flat, path)
	default:
		return errors.Unsupported(errors.PhaseEncode, "TypeDef kind")
	}
}

func (e *Encoder) flattenRecord(r *wit.Record, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	m, ok := value.(map[string]any)
	if !ok {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "map[string]any")
	}

	for _, field := range r.Fields {
		fieldVal, exists := m[field.Name]
		if !exists {
			return errors.FieldMissing(errors.PhaseEncode, path, field.Name)
		}
		if err := e.flattenValue(field.Type, fieldVal, mem, alloc, allocList, flat, nil); err != nil {
			if path != nil {
				fieldPath := append(append([]string{}, path...), field.Name)
				return errors.New(errors.PhaseEncode, errors.KindInvalidData).
					Path(fieldPath...).
					Detail("failed to encode field %s: %v", field.Name, err).
					Build()
			}
			return err
		}
	}
	return nil
}

func (e *Encoder) flattenList(l *wit.List, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "slice")
	}

	length := uint32(rv.Len())
	if length > MaxListLength {
		return errors.New(errors.PhaseEncode, errors.KindOverflow).
			Path(path...).
			Detail("list length %d exceeds maximum %d", length, MaxListLength).
			Build()
	}

	if length == 0 {
		*flat = append(*flat, 0, 0)
		return nil
	}

	// Calculate element layout
	lc := e.compiler.layout
	elemLayout := lc.Calculate(l.Type)
	dataSize, ok := safeMulU32(length, elemLayout.Size)
	if !ok || dataSize > MaxAlloc {
		return errors.New(errors.PhaseEncode, errors.KindOverflow).
			Path(path...).
			Detail("list data size overflow: %d * %d", length, elemLayout.Size).
			Build()
	}

	dataAddr, err := alloc.Alloc(dataSize, elemLayout.Align)
	if err != nil {
		return errors.New(errors.PhaseEncode, errors.KindAllocation).
			Path(path...).
			Detail("failed to allocate %d bytes (align %d) for list data", dataSize, elemLayout.Align).
			Build()
	}
	if allocList != nil {
		allocList.Add(dataAddr, dataSize, elemLayout.Align)
	}

	// Fast path for primitive slices to avoid boxing
	if length > 0 {
		switch l.Type.(type) {
		case wit.U8:
			if rv.Type().Elem().Kind() == reflect.Uint8 {
				ptr := unsafe.Pointer(rv.Index(0).UnsafeAddr())
				src := unsafe.Slice((*uint8)(ptr), length)
				if err := mem.Write(dataAddr, src); err != nil {
					return err
				}
				*flat = append(*flat, uint64(dataAddr), uint64(length))
				return nil
			}
		case wit.S32:
			if rv.Type().Elem().Kind() == reflect.Int32 {
				ptr := unsafe.Pointer(rv.Index(0).UnsafeAddr())
				src := unsafe.Slice((*byte)(ptr), length*4)
				if err := mem.Write(dataAddr, src); err != nil {
					return err
				}
				*flat = append(*flat, uint64(dataAddr), uint64(length))
				return nil
			}
		case wit.U32:
			if rv.Type().Elem().Kind() == reflect.Uint32 {
				ptr := unsafe.Pointer(rv.Index(0).UnsafeAddr())
				src := unsafe.Slice((*byte)(ptr), length*4)
				if err := mem.Write(dataAddr, src); err != nil {
					return err
				}
				*flat = append(*flat, uint64(dataAddr), uint64(length))
				return nil
			}
		case wit.S64:
			if rv.Type().Elem().Kind() == reflect.Int64 {
				ptr := unsafe.Pointer(rv.Index(0).UnsafeAddr())
				src := unsafe.Slice((*byte)(ptr), length*8)
				if err := mem.Write(dataAddr, src); err != nil {
					return err
				}
				*flat = append(*flat, uint64(dataAddr), uint64(length))
				return nil
			}
		case wit.U64:
			if rv.Type().Elem().Kind() == reflect.Uint64 {
				ptr := unsafe.Pointer(rv.Index(0).UnsafeAddr())
				src := unsafe.Slice((*byte)(ptr), length*8)
				if err := mem.Write(dataAddr, src); err != nil {
					return err
				}
				*flat = append(*flat, uint64(dataAddr), uint64(length))
				return nil
			}
		case wit.F32:
			if rv.Type().Elem().Kind() == reflect.Float32 {
				// Canonicalize NaN values per spec
				for i := uint32(0); i < length; i++ {
					bits := math.Float32bits(rv.Index(int(i)).Interface().(float32))
					if err := mem.WriteU32(dataAddr+i*4, abi.CanonicalizeF32(bits)); err != nil {
						return err
					}
				}
				*flat = append(*flat, uint64(dataAddr), uint64(length))
				return nil
			}
		case wit.F64:
			if rv.Type().Elem().Kind() == reflect.Float64 {
				// Canonicalize NaN values per spec
				for i := uint32(0); i < length; i++ {
					bits := math.Float64bits(rv.Index(int(i)).Interface().(float64))
					if err := mem.WriteU64(dataAddr+i*8, abi.CanonicalizeF64(bits)); err != nil {
						return err
					}
				}
				*flat = append(*flat, uint64(dataAddr), uint64(length))
				return nil
			}
		}
	}

	// Slow path: box each element
	for i := uint32(0); i < length; i++ {
		elemVal := rv.Index(int(i)).Interface()
		if err := e.storeValue(l.Type, elemVal, dataAddr+i*elemLayout.Size, mem, alloc, allocList, path); err != nil {
			return err
		}
	}

	*flat = append(*flat, uint64(dataAddr), uint64(length))
	return nil
}

func (e *Encoder) flattenOption(o *wit.Option, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	if value == nil {
		*flat = append(*flat, 0) // None
		// Add zero padding for inner type
		flatCount := GetFlatCount(o.Type)
		for i := 0; i < flatCount; i++ {
			*flat = append(*flat, 0)
		}
		return nil
	}

	*flat = append(*flat, 1) // Some
	if err := e.flattenValue(o.Type, value, mem, alloc, allocList, flat, nil); err != nil {
		if path != nil {
			somePath := append(append([]string{}, path...), "[some]")
			return errors.New(errors.PhaseEncode, errors.KindInvalidData).
				Path(somePath...).
				Detail("failed to encode option value: %v", err).
				Build()
		}
		return err
	}
	return nil
}

func (e *Encoder) flattenTuple(t *wit.Tuple, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array && rv.Kind() != reflect.Struct {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "slice/array/struct")
	}

	var getElem func(i int) any
	var length int

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		length = rv.Len()
		getElem = func(i int) any { return rv.Index(i).Interface() }
	case reflect.Struct:
		length = rv.NumField()
		getElem = func(i int) any { return rv.Field(i).Interface() }
	}

	if length != len(t.Types) {
		return errors.New(errors.PhaseEncode, errors.KindTypeMismatch).
			Path(path...).
			Detail("tuple has %d elements, value has %d", len(t.Types), length).
			Build()
	}

	for i, elemType := range t.Types {
		if err := e.flattenValue(elemType, getElem(i), mem, alloc, allocList, flat, nil); err != nil {
			if path != nil {
				elemPath := append(append([]string{}, path...), "["+strconv.Itoa(i)+"]")
				return errors.New(errors.PhaseEncode, errors.KindInvalidData).
					Path(elemPath...).
					Detail("failed to encode tuple element %d: %v", i, err).
					Build()
			}
			return err
		}
	}
	return nil
}

func (e *Encoder) flattenEnum(en *wit.Enum, value any, flat *[]uint64, path []string) error {
	rv := reflect.ValueOf(value)
	if !rv.CanInt() && !rv.CanUint() {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "integer")
	}

	var disc uint64
	if rv.CanInt() {
		disc = uint64(rv.Int())
	} else {
		disc = rv.Uint()
	}

	if disc >= uint64(len(en.Cases)) {
		return errors.InvalidDiscriminant(errors.PhaseEncode, path, uint32(disc), uint32(len(en.Cases)-1))
	}

	*flat = append(*flat, disc)
	return nil
}

func (e *Encoder) flattenFlags(f *wit.Flags, value any, flat *[]uint64, path []string) error {
	rv := reflect.ValueOf(value)
	if !rv.CanUint() {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "unsigned integer")
	}
	*flat = append(*flat, rv.Uint())
	return nil
}

func (e *Encoder) flattenResult(r *wit.Result, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	m, ok := value.(map[string]any)
	if !ok {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "map[string]any")
	}

	// Find max payload size for padding
	maxPayloadFlat := 0
	if r.OK != nil {
		okFlat := GetFlatCount(r.OK)
		if okFlat > maxPayloadFlat {
			maxPayloadFlat = okFlat
		}
	}
	if r.Err != nil {
		errFlat := GetFlatCount(r.Err)
		if errFlat > maxPayloadFlat {
			maxPayloadFlat = errFlat
		}
	}

	if okVal, hasOk := m["ok"]; hasOk {
		startLen := len(*flat)
		*flat = append(*flat, 0) // Ok discriminant
		if r.OK != nil {
			if err := e.flattenValue(r.OK, okVal, mem, alloc, allocList, flat, nil); err != nil {
				if path != nil {
					okPath := append(append([]string{}, path...), "[ok]")
					return errors.New(errors.PhaseEncode, errors.KindInvalidData).
						Path(okPath...).
						Detail("failed to encode ok value: %v", err).
						Build()
				}
				return err
			}
		}
		// Pad with zeros to max payload size
		actualPayloadFlat := len(*flat) - startLen - 1
		for actualPayloadFlat < maxPayloadFlat {
			*flat = append(*flat, 0)
			actualPayloadFlat++
		}
	} else if errVal, hasErr := m["err"]; hasErr {
		startLen := len(*flat)
		*flat = append(*flat, 1) // Err discriminant
		if r.Err != nil {
			if err := e.flattenValue(r.Err, errVal, mem, alloc, allocList, flat, nil); err != nil {
				if path != nil {
					errPath := append(append([]string{}, path...), "[err]")
					return errors.New(errors.PhaseEncode, errors.KindInvalidData).
						Path(errPath...).
						Detail("failed to encode err value: %v", err).
						Build()
				}
				return err
			}
		}
		// Pad with zeros to max payload size
		actualPayloadFlat := len(*flat) - startLen - 1
		for actualPayloadFlat < maxPayloadFlat {
			*flat = append(*flat, 0)
			actualPayloadFlat++
		}
	} else {
		return errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Path(path...).
			Detail("result must have either 'ok' or 'err' key").
			Build()
	}
	return nil
}

func (e *Encoder) flattenVariant(v *wit.Variant, value any, mem Memory, alloc Allocator, allocList *AllocationList, flat *[]uint64, path []string) error {
	m, ok := value.(map[string]any)
	if !ok {
		return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "map[string]any")
	}

	// Find max payload size for padding
	maxPayloadFlat := 0
	for _, c := range v.Cases {
		if c.Type != nil {
			flatCount := GetFlatCount(c.Type)
			if flatCount > maxPayloadFlat {
				maxPayloadFlat = flatCount
			}
		}
	}

	for i, c := range v.Cases {
		if payload, found := m[c.Name]; found {
			startLen := len(*flat)
			*flat = append(*flat, uint64(i))
			if c.Type != nil {
				if err := e.flattenValue(c.Type, payload, mem, alloc, allocList, flat, nil); err != nil {
					if path != nil {
						casePath := append(append([]string{}, path...), c.Name)
						return errors.New(errors.PhaseEncode, errors.KindInvalidData).
							Path(casePath...).
							Detail("failed to encode variant case %s: %v", c.Name, err).
							Build()
					}
					return err
				}
			}
			// Pad with zeros to max payload size
			actualPayloadFlat := len(*flat) - startLen - 1
			for actualPayloadFlat < maxPayloadFlat {
				*flat = append(*flat, 0)
				actualPayloadFlat++
			}
			return nil
		}
	}

	return errors.New(errors.PhaseEncode, errors.KindInvalidData).
		Path(path...).
		Detail("variant value must contain one of the case names").
		Build()
}

func (e *Encoder) storeValue(witType wit.Type, value any, addr uint32, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	switch t := witType.(type) {
	case wit.Bool:
		v, ok := value.(bool)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "bool")
		}
		var b uint8
		if v {
			b = 1
		}
		return mem.WriteU8(addr, b)

	case wit.U8:
		v, ok := value.(uint8)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint8")
		}
		return mem.WriteU8(addr, v)

	case wit.S8:
		v, ok := value.(int8)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "int8")
		}
		return mem.WriteU8(addr, uint8(v))

	case wit.U16:
		v, ok := value.(uint16)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint16")
		}
		return mem.WriteU16(addr, v)

	case wit.S16:
		v, ok := value.(int16)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "int16")
		}
		return mem.WriteU16(addr, uint16(v))

	case wit.U32:
		v, ok := coerceToUint32(value)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint32")
		}
		return mem.WriteU32(addr, v)

	case wit.S32:
		v, ok := coerceToInt32(value)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "int32")
		}
		return mem.WriteU32(addr, uint32(v))

	case wit.U64:
		v, ok := coerceToUint64(value)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "uint64")
		}
		return mem.WriteU64(addr, v)

	case wit.S64:
		v, ok := coerceToInt64(value)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "int64")
		}
		return mem.WriteU64(addr, uint64(v))

	case wit.F32:
		v, ok := value.(float32)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "float32")
		}
		return mem.WriteU32(addr, abi.CanonicalizeF32(math.Float32bits(v)))

	case wit.F64:
		v, ok := value.(float64)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "float64")
		}
		return mem.WriteU64(addr, abi.CanonicalizeF64(math.Float64bits(v)))

	case wit.Char:
		var r rune
		switch v := value.(type) {
		case rune: // rune is int32
			r = v
		default:
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "rune")
		}
		if !abi.ValidateChar(r) {
			return errors.New(errors.PhaseEncode, errors.KindInvalidData).
				Path(path...).
				Detail("invalid Unicode scalar value: 0x%X", r).
				Build()
		}
		return mem.WriteU32(addr, uint32(r))

	case wit.String:
		s, ok := value.(string)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "string")
		}
		if !utf8.ValidString(s) {
			return errors.InvalidUTF8(errors.PhaseEncode, path, []byte(s))
		}
		// Store string as ptr+len
		dataLen := uint32(len(s))
		if dataLen == 0 {
			if err := mem.WriteU32(addr, 0); err != nil {
				return err
			}
			return mem.WriteU32(addr+4, 0)
		}
		dataAddr, err := alloc.Alloc(dataLen, 1)
		if err != nil {
			return errors.New(errors.PhaseEncode, errors.KindAllocation).
				Path(path...).
				Detail("failed to allocate %d bytes for string data", dataLen).
				Build()
		}
		if allocList != nil {
			allocList.Add(dataAddr, dataLen, 1)
		}
		if err := mem.Write(dataAddr, []byte(s)); err != nil {
			return err
		}
		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, dataLen)

	case *wit.TypeDef:
		return e.storeTypeDef(t, value, addr, mem, alloc, allocList, path)

	default:
		return errors.Unsupported(errors.PhaseEncode, "WIT type for store")
	}
}

func (e *Encoder) storeTypeDef(t *wit.TypeDef, value any, addr uint32, mem Memory, alloc Allocator, allocList *AllocationList, path []string) error {
	switch kind := t.Kind.(type) {
	case *wit.Record:
		m, ok := value.(map[string]any)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "map[string]any")
		}
		lc := e.compiler.layout
		recordLayout := lc.Calculate(t)
		for _, field := range kind.Fields {
			fieldVal, exists := m[field.Name]
			if !exists {
				return errors.FieldMissing(errors.PhaseEncode, path, field.Name)
			}
			fieldAddr := addr + recordLayout.FieldOffs[field.Name]
			if err := e.storeValue(field.Type, fieldVal, fieldAddr, mem, alloc, allocList, nil); err != nil {
				if path != nil {
					fieldPath := append(append([]string{}, path...), field.Name)
					return errors.New(errors.PhaseEncode, errors.KindInvalidData).
						Path(fieldPath...).
						Detail("failed to store field %s: %v", field.Name, err).
						Build()
				}
				return err
			}
		}
		return nil

	case *wit.List:
		rv := reflect.ValueOf(value)
		if rv.Kind() != reflect.Slice {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "slice")
		}

		length := uint32(rv.Len())
		if length > MaxListLength {
			return errors.New(errors.PhaseEncode, errors.KindOverflow).
				Path(path...).
				Detail("list length %d exceeds maximum %d", length, MaxListLength).
				Build()
		}

		if length == 0 {
			if err := mem.WriteU32(addr, 0); err != nil {
				return err
			}
			return mem.WriteU32(addr+4, 0)
		}

		lc := e.compiler.layout
		elemLayout := lc.Calculate(kind.Type)
		dataSize, ok := safeMulU32(length, elemLayout.Size)
		if !ok || dataSize > MaxAlloc {
			return errors.New(errors.PhaseEncode, errors.KindOverflow).
				Path(path...).
				Detail("list data size overflow: %d * %d", length, elemLayout.Size).
				Build()
		}

		dataAddr, err := alloc.Alloc(dataSize, elemLayout.Align)
		if err != nil {
			return errors.New(errors.PhaseEncode, errors.KindAllocation).
				Path(path...).
				Detail("failed to allocate %d bytes (align %d) for list data", dataSize, elemLayout.Align).
				Build()
		}
		if allocList != nil {
			allocList.Add(dataAddr, dataSize, elemLayout.Align)
		}

		for i := uint32(0); i < length; i++ {
			elemVal := rv.Index(int(i)).Interface()
			var elemPath []string
			if path != nil {
				elemPath = append(append([]string{}, path...), "["+strconv.FormatUint(uint64(i), 10)+"]")
			}
			if err := e.storeValue(kind.Type, elemVal, dataAddr+i*elemLayout.Size, mem, alloc, allocList, elemPath); err != nil {
				return err
			}
		}

		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		return mem.WriteU32(addr+4, length)

	case *wit.Tuple:
		rv := reflect.ValueOf(value)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "slice/array")
		}

		length := rv.Len()
		if length != len(kind.Types) {
			return errors.New(errors.PhaseEncode, errors.KindTypeMismatch).
				Path(path...).
				Detail("tuple has %d elements, value has %d", len(kind.Types), length).
				Build()
		}

		lc := e.compiler.layout
		witOffset := uint32(0)
		for i, elemType := range kind.Types {
			elemLayout := lc.Calculate(elemType)
			witOffset = alignTo(witOffset, elemLayout.Align)
			elemVal := rv.Index(i).Interface()
			if err := e.storeValue(elemType, elemVal, addr+witOffset, mem, alloc, allocList, path); err != nil {
				return err
			}
			witOffset += elemLayout.Size
		}
		return nil

	case *wit.Option:
		if value == nil {
			return mem.WriteU8(addr, 0)
		}
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Ptr && rv.IsNil() {
			return mem.WriteU8(addr, 0)
		}
		if err := mem.WriteU8(addr, 1); err != nil {
			return err
		}
		innerLayout := e.compiler.layout.Calculate(kind.Type)
		payloadOffset := alignTo(1, innerLayout.Align)
		innerVal := value
		if rv.Kind() == reflect.Ptr {
			innerVal = rv.Elem().Interface()
		}
		return e.storeValue(kind.Type, innerVal, addr+payloadOffset, mem, alloc, allocList, path)

	case *wit.Result:
		m, ok := value.(map[string]any)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "map[string]any with ok/err")
		}
		lc := e.compiler.layout
		resultLayout := lc.Calculate(t)
		payloadAddr := addr + alignTo(1, resultLayout.Align)
		okVal, hasOk := m["ok"]
		_, hasErr := m["err"]
		if hasOk && !hasErr {
			if err := mem.WriteU8(addr, 0); err != nil {
				return err
			}
			if kind.OK != nil && okVal != nil {
				return e.storeValue(kind.OK, okVal, payloadAddr, mem, alloc, allocList, path)
			}
			return nil
		}
		if hasErr {
			errVal := m["err"]
			if err := mem.WriteU8(addr, 1); err != nil {
				return err
			}
			if kind.Err != nil && errVal != nil {
				return e.storeValue(kind.Err, errVal, payloadAddr, mem, alloc, allocList, path)
			}
			return nil
		}
		return errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Path(path...).
			Detail("result value must have either 'ok' or 'err' key").
			Build()

	case *wit.Variant:
		m, ok := value.(map[string]any)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "map[string]any")
		}
		lc := e.compiler.layout
		variantLayout := lc.Calculate(t)
		discSize := abi.DiscriminantSize(len(kind.Cases))
		for i, c := range kind.Cases {
			caseVal, exists := m[c.Name]
			if !exists {
				continue
			}
			switch discSize {
			case 1:
				if err := mem.WriteU8(addr, uint8(i)); err != nil {
					return err
				}
			case 2:
				if err := mem.WriteU16(addr, uint16(i)); err != nil {
					return err
				}
			case 4:
				if err := mem.WriteU32(addr, uint32(i)); err != nil {
					return err
				}
			}
			if c.Type != nil && caseVal != nil {
				payloadAddr := addr + alignTo(discSize, variantLayout.Align)
				return e.storeValue(c.Type, caseVal, payloadAddr, mem, alloc, allocList, path)
			}
			return nil
		}
		return errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Path(path...).
			Detail("variant value must have one of the case names as key").
			Build()

	case *wit.Enum:
		caseName, ok := value.(string)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "string")
		}
		discSize := abi.DiscriminantSize(len(kind.Cases))
		for i, c := range kind.Cases {
			if c.Name == caseName {
				switch discSize {
				case 1:
					return mem.WriteU8(addr, uint8(i))
				case 2:
					return mem.WriteU16(addr, uint16(i))
				case 4:
					return mem.WriteU32(addr, uint32(i))
				}
			}
		}
		return errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Path(path...).
			Detail("enum case '%s' not found", caseName).
			Build()

	case *wit.Flags:
		m, ok := value.(map[string]bool)
		if !ok {
			return errors.TypeMismatch(errors.PhaseEncode, path, typeName(value), "map[string]bool")
		}
		numFlags := len(kind.Flags)
		var bits uint64
		for i, flag := range kind.Flags {
			if m[flag.Name] {
				bits |= 1 << uint(i)
			}
		}
		if numFlags <= 8 {
			return mem.WriteU8(addr, uint8(bits))
		} else if numFlags <= 16 {
			return mem.WriteU16(addr, uint16(bits))
		} else if numFlags <= 32 {
			return mem.WriteU32(addr, uint32(bits))
		} else if numFlags <= 64 {
			return mem.WriteU64(addr, bits)
		}

		// >64 flags: multiple u32s per Canonical ABI spec
		numU32s := (numFlags + 31) / 32
		for i := 0; i < numU32s; i++ {
			word := uint32((bits >> (i * 32)) & 0xFFFFFFFF)
			if err := mem.WriteU32(addr+uint32(i*4), word); err != nil {
				return err
			}
		}
		return nil

	case wit.Type:
		return e.storeValue(kind, value, addr, mem, alloc, allocList, path)

	default:
		return errors.Unsupported(errors.PhaseEncode, "TypeDef kind for store")
	}
}

var GetFlatCount = abi.GetFlatCount
