package transcoder

import (
	"math"
	"reflect"
	"unicode/utf8"
	"unsafe"

	"github.com/wippyai/wasm-runtime/errors"
	"github.com/wippyai/wasm-runtime/transcoder/internal/abi"
	"go.bytecodealliance.org/wit"
)

// DecodeInto decodes directly into result (must be pointer) without intermediate allocation.
// Strings point directly into WASM memory: only valid while instance is alive and memory unmodified.
func (d *Decoder) DecodeInto(resultTypes []wit.Type, flat []uint64, mem Memory, result any) error {
	if result == nil {
		return nil // void return
	}

	rv := reflect.ValueOf(result)
	if rv.Kind() != reflect.Pointer {
		return errors.New(errors.PhaseDecode, errors.KindTypeMismatch).
			Detail("result must be a pointer, got %T", result).
			Build()
	}

	// Extract pointer from interface correctly
	ptr := unsafe.Pointer(rv.Pointer())
	if ptr == nil {
		return errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("result pointer is nil").
			Build()
	}

	// Single result - most common case
	if len(resultTypes) == 1 {
		return d.decodeValueInto(resultTypes[0], flat, 0, mem, ptr)
	}

	// Multiple results - result should be a struct
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return errors.New(errors.PhaseDecode, errors.KindTypeMismatch).
			Detail("multiple results require struct pointer, got %T", result).
			Build()
	}

	offset := 0
	for i, rt := range resultTypes {
		if i >= elem.NumField() {
			break
		}
		fieldPtr := unsafe.Pointer(elem.Field(i).UnsafeAddr())
		consumed, err := d.decodeValueIntoWithCount(rt, flat, offset, mem, fieldPtr)
		if err != nil {
			return err
		}
		offset += consumed
	}

	return nil
}

func (d *Decoder) decodeValueInto(witType wit.Type, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) error {
	_, err := d.decodeValueIntoWithCount(witType, flat, offset, mem, ptr)
	return err
}

func (d *Decoder) decodeValueIntoWithCount(witType wit.Type, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	// Bounds check for primitives
	if offset >= len(flat) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient flat values at offset %d", offset).
			Build()
	}

	switch t := witType.(type) {
	case wit.Bool:
		*(*bool)(ptr) = flat[offset] != 0
		return 1, nil

	case wit.U8:
		*(*uint8)(ptr) = uint8(flat[offset])
		return 1, nil

	case wit.S8:
		*(*int8)(ptr) = int8(flat[offset])
		return 1, nil

	case wit.U16:
		*(*uint16)(ptr) = uint16(flat[offset])
		return 1, nil

	case wit.S16:
		*(*int16)(ptr) = int16(flat[offset])
		return 1, nil

	case wit.U32:
		*(*uint32)(ptr) = uint32(flat[offset])
		return 1, nil

	case wit.S32:
		*(*int32)(ptr) = int32(flat[offset])
		return 1, nil

	case wit.U64:
		*(*uint64)(ptr) = flat[offset]
		return 1, nil

	case wit.S64:
		*(*int64)(ptr) = int64(flat[offset])
		return 1, nil

	case wit.F32:
		*(*float32)(ptr) = math.Float32frombits(abi.CanonicalizeF32(uint32(flat[offset])))
		return 1, nil

	case wit.F64:
		*(*float64)(ptr) = math.Float64frombits(abi.CanonicalizeF64(flat[offset]))
		return 1, nil

	case wit.Char:
		r := rune(flat[offset])
		if !abi.ValidateChar(r) {
			return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
				Detail("invalid Unicode scalar value: 0x%X", r).
				Build()
		}
		*(*rune)(ptr) = r
		return 1, nil

	case wit.String:
		return d.decodeStringInto(flat, offset, mem, ptr)

	case *wit.TypeDef:
		return d.decodeTypeDefInto(t, flat, offset, mem, ptr)

	default:
		return 0, errors.Unsupported(errors.PhaseDecode, "WIT type")
	}
}

// decodeStringInto creates a string pointing directly into WASM memory (zero-copy).
func (d *Decoder) decodeStringInto(flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	if offset+1 >= len(flat) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient flat values for string: need %d, have %d", offset+2, len(flat)).
			Build()
	}
	dataAddr := uint32(flat[offset])
	dataLen := uint32(flat[offset+1])

	if dataLen == 0 {
		*(*string)(ptr) = ""
		return 2, nil
	}

	if dataLen > MaxStringSize {
		return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Detail("string size %d exceeds maximum %d", dataLen, MaxStringSize).
			Build()
	}

	// Get slice pointing into WASM memory
	data, err := mem.Read(dataAddr, dataLen)
	if err != nil {
		return 0, err
	}

	// Validate UTF-8 per spec
	if !utf8.Valid(data) {
		return 0, errors.InvalidUTF8(errors.PhaseDecode, nil, data)
	}

	// Zero-copy: create string header pointing directly into WASM memory
	*(*string)(ptr) = unsafe.String(unsafe.SliceData(data), len(data))
	return 2, nil
}

func (d *Decoder) decodeTypeDefInto(t *wit.TypeDef, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	switch kind := t.Kind.(type) {
	case *wit.Record:
		return d.decodeRecordInto(kind, flat, offset, mem, ptr)

	case *wit.List:
		return d.decodeListInto(kind, flat, offset, mem, ptr)

	case *wit.Option:
		return d.decodeOptionInto(kind, flat, offset, mem, ptr)

	case *wit.Tuple:
		return d.decodeTupleInto(kind, flat, offset, mem, ptr)

	case *wit.Enum:
		disc := uint32(flat[offset])
		if disc >= uint32(len(kind.Cases)) {
			return 0, errors.InvalidDiscriminant(errors.PhaseDecode, nil, disc, uint32(len(kind.Cases)-1))
		}
		*(*uint32)(ptr) = disc
		return 1, nil

	case *wit.Flags:
		*(*uint64)(ptr) = flat[offset]
		return 1, nil

	case *wit.Result:
		return d.decodeResultInto(kind, flat, offset, mem, ptr)

	case *wit.Variant:
		return d.decodeVariantInto(kind, flat, offset, mem, ptr)

	case wit.Type:
		return d.decodeValueIntoWithCount(kind, flat, offset, mem, ptr)

	default:
		return 0, errors.Unsupported(errors.PhaseDecode, "TypeDef kind")
	}
}

// decodeRecordInto decodes into map[string]any. For zero-alloc struct decoding, use compiled stack path.
func (d *Decoder) decodeRecordInto(r *wit.Record, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	result := make(map[string]any, len(r.Fields))
	consumed := 0

	for _, field := range r.Fields {
		val, count, err := d.liftValue(field.Type, flat[offset+consumed:], mem, []string{field.Name})
		if err != nil {
			return 0, err
		}
		result[field.Name] = val
		consumed += count
	}

	*(*map[string]any)(ptr) = result
	return consumed, nil
}

func (d *Decoder) decodeListInto(l *wit.List, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	if offset+1 >= len(flat) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient flat values for list: need %d, have %d", offset+2, len(flat)).
			Build()
	}
	dataAddr := uint32(flat[offset])
	length := uint32(flat[offset+1])

	if length == 0 {
		*(*[]any)(ptr) = nil
		return 2, nil
	}

	if length > MaxListLength {
		return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Detail("list length %d exceeds maximum %d", length, MaxListLength).
			Build()
	}

	lc := d.compiler.layout
	elemLayout := lc.Calculate(l.Type)

	// Check for address overflow
	if elemLayout.Size > 0 && length > 0 {
		maxOffset := uint64(length-1) * uint64(elemLayout.Size)
		if maxOffset > uint64(^uint32(0))-uint64(dataAddr) {
			return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Detail("list address overflow: dataAddr=%d, length=%d, elemSize=%d", dataAddr, length, elemLayout.Size).
				Build()
		}
	}

	result := make([]any, length)
	for i := uint32(0); i < length; i++ {
		val, err := d.loadValue(l.Type, dataAddr+i*elemLayout.Size, mem, nil)
		if err != nil {
			return 0, err
		}
		result[i] = val
	}

	*(*[]any)(ptr) = result
	return 2, nil
}

func (d *Decoder) decodeOptionInto(o *wit.Option, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	if offset >= len(flat) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient flat values for option").
			Build()
	}
	disc := flat[offset]
	innerFlatCount := GetFlatCount(o.Type)
	// Total consumed is always 1 (discriminant) + inner flat count (for padding)
	totalConsumed := 1 + innerFlatCount

	if disc == 0 {
		// None - set to nil/zero
		// ptr points to an any, set it to nil
		*(*any)(ptr) = nil
		return totalConsumed, nil
	}
	if disc != 1 {
		return 0, errors.InvalidDiscriminant(errors.PhaseDecode, nil, uint32(disc), 1)
	}

	// Some - decode inner value
	val, _, err := d.liftValue(o.Type, flat[offset+1:], mem, nil)
	if err != nil {
		return 0, err
	}
	*(*any)(ptr) = val
	return totalConsumed, nil
}

func (d *Decoder) decodeTupleInto(t *wit.Tuple, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	result := make([]any, len(t.Types))
	consumed := 0

	for i, elemType := range t.Types {
		val, count, err := d.liftValue(elemType, flat[offset+consumed:], mem, nil)
		if err != nil {
			return 0, err
		}
		result[i] = val
		consumed += count
	}

	*(*[]any)(ptr) = result
	return consumed, nil
}

func (d *Decoder) decodeResultInto(r *wit.Result, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	if offset >= len(flat) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient flat values for result").
			Build()
	}
	disc := flat[offset]
	result := make(map[string]any)

	// Calculate max payload flat count for proper padding
	okCount := 0
	if r.OK != nil {
		okCount = GetFlatCount(r.OK)
	}
	errCount := 0
	if r.Err != nil {
		errCount = GetFlatCount(r.Err)
	}
	payloadCount := okCount
	if errCount > payloadCount {
		payloadCount = errCount
	}
	// Total consumed is always 1 (discriminant) + max(okCount, errCount)
	totalConsumed := 1 + payloadCount

	if disc == 0 {
		// Ok
		if r.OK != nil {
			val, _, err := d.liftValue(r.OK, flat[offset+1:], mem, nil)
			if err != nil {
				return 0, err
			}
			result["ok"] = val
		} else {
			result["ok"] = nil
		}
	} else {
		// Err
		if r.Err != nil {
			val, _, err := d.liftValue(r.Err, flat[offset+1:], mem, nil)
			if err != nil {
				return 0, err
			}
			result["err"] = val
		} else {
			result["err"] = nil
		}
	}

	*(*map[string]any)(ptr) = result
	return totalConsumed, nil
}

func (d *Decoder) decodeVariantInto(v *wit.Variant, flat []uint64, offset int, mem Memory, ptr unsafe.Pointer) (int, error) {
	if offset >= len(flat) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient flat values for variant").
			Build()
	}
	disc := flat[offset]
	if disc >= uint64(len(v.Cases)) {
		return 0, errors.InvalidDiscriminant(errors.PhaseDecode, nil, uint32(disc), uint32(len(v.Cases)-1))
	}

	// Calculate max payload flat count for proper padding
	maxPayload := 0
	for _, c := range v.Cases {
		if c.Type != nil {
			count := GetFlatCount(c.Type)
			if count > maxPayload {
				maxPayload = count
			}
		}
	}
	// Total consumed is always 1 (discriminant) + max payload count
	totalConsumed := 1 + maxPayload

	c := v.Cases[disc]
	result := make(map[string]any)

	if c.Type != nil {
		val, _, err := d.liftValue(c.Type, flat[offset+1:], mem, nil)
		if err != nil {
			return 0, err
		}
		result[c.Name] = val
	} else {
		result[c.Name] = nil
	}

	*(*map[string]any)(ptr) = result
	return totalConsumed, nil
}
