package transcoder

import (
	"encoding/binary"
	"math"
	"reflect"
	"unicode/utf8"
	"unsafe"

	"github.com/wippyai/wasm-runtime/errors"
	"github.com/wippyai/wasm-runtime/transcoder/internal/abi"
)

// unitSentinel provides a stable non-nil pointer for unit types (Result<(), _>, unit Variant cases).
// Avoids use-after-free from stack-allocated sentinels.
var unitSentinel byte

func UnitPtr() unsafe.Pointer {
	return unsafe.Pointer(&unitSentinel)
}

// LowerToStack performs "lower" (Go -> WASM). Returns stack slots consumed.
func (e *Encoder) LowerToStack(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, mem Memory, alloc Allocator) (int, error) {
	return e.lowerToStack(ct, ptr, stack, 0, mem, alloc, nil)
}

// LowerToStackTracked tracks allocations for cleanup on error.
func (e *Encoder) LowerToStackTracked(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	return e.lowerToStack(ct, ptr, stack, 0, mem, alloc, allocList)
}

func (e *Encoder) lowerToStack(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, offset int, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	if offset+ct.FlatCount > len(stack) {
		return 0, errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Detail("stack too small: need %d slots at offset %d, have %d", ct.FlatCount, offset, len(stack)).
			Build()
	}

	switch ct.Kind {
	case KindBool:
		if *(*bool)(ptr) {
			stack[offset] = 1
		} else {
			stack[offset] = 0
		}
		return 1, nil

	case KindU8:
		stack[offset] = uint64(*(*uint8)(ptr))
		return 1, nil

	case KindS8:
		stack[offset] = uint64(int64(*(*int8)(ptr)))
		return 1, nil

	case KindU16:
		stack[offset] = uint64(*(*uint16)(ptr))
		return 1, nil

	case KindS16:
		stack[offset] = uint64(int64(*(*int16)(ptr)))
		return 1, nil

	case KindU32:
		stack[offset] = uint64(*(*uint32)(ptr))
		return 1, nil

	case KindS32:
		stack[offset] = uint64(int64(*(*int32)(ptr)))
		return 1, nil

	case KindU64:
		stack[offset] = *(*uint64)(ptr)
		return 1, nil

	case KindS64:
		stack[offset] = uint64(*(*int64)(ptr))
		return 1, nil

	case KindF32:
		stack[offset] = uint64(abi.CanonicalizeF32(math.Float32bits(*(*float32)(ptr))))
		return 1, nil

	case KindF64:
		stack[offset] = abi.CanonicalizeF64(math.Float64bits(*(*float64)(ptr)))
		return 1, nil

	case KindChar:
		r := *(*rune)(ptr)
		if !abi.ValidateChar(r) {
			return 0, errors.New(errors.PhaseEncode, errors.KindInvalidData).
				Detail("invalid Unicode scalar value: 0x%X", r).
				Build()
		}
		stack[offset] = uint64(r)
		return 1, nil

	case KindString:
		return e.lowerStringToStack(*(*string)(ptr), stack, offset, mem, alloc, allocList)

	case KindRecord, KindTuple:
		return e.lowerRecordToStack(ct, ptr, stack, offset, mem, alloc, allocList)

	case KindList:
		return e.lowerListToStack(ct, ptr, stack, offset, mem, alloc, allocList)

	case KindOption:
		return e.lowerOptionToStack(ct, ptr, stack, offset, mem, alloc, allocList)

	case KindResult:
		return e.lowerResultToStack(ct, ptr, stack, offset, mem, alloc, allocList)

	case KindVariant:
		return e.lowerVariantToStack(ct, ptr, stack, offset, mem, alloc, allocList)

	case KindEnum:
		rv := reflect.NewAt(ct.GoType, ptr).Elem()
		var disc uint64
		if rv.CanUint() {
			disc = rv.Uint()
		} else {
			disc = uint64(rv.Int())
		}
		if disc >= uint64(len(ct.Cases)) {
			return 0, errors.InvalidDiscriminant(errors.PhaseEncode, nil, uint32(disc), uint32(len(ct.Cases)-1))
		}
		stack[offset] = disc
		return 1, nil

	case KindFlags:
		// Flags are stored as integers without bounds check
		rv := reflect.NewAt(ct.GoType, ptr).Elem()
		if rv.CanUint() {
			stack[offset] = rv.Uint()
		} else {
			stack[offset] = uint64(rv.Int())
		}
		return 1, nil

	case KindOwn, KindBorrow:
		// Resource handles are u32 on the stack
		// Go type is either uint32 or a struct with Handle field
		if ct.GoType.Kind() == reflect.Uint32 {
			stack[offset] = uint64(*(*uint32)(ptr))
		} else {
			// Struct with Handle field (Own[T] or Borrow[T])
			rv := reflect.NewAt(ct.GoType, ptr).Elem()
			handleField := rv.FieldByName("Handle")
			if handleField.IsValid() {
				stack[offset] = handleField.Uint()
			} else {
				stack[offset] = 0
			}
		}
		return 1, nil

	default:
		return 0, errors.Unsupported(errors.PhaseEncode, "type kind for stack: "+ct.Kind.String())
	}
}

func (e *Encoder) lowerStringToStack(s string, stack []uint64, offset int, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	if !utf8.ValidString(s) {
		return 0, errors.InvalidUTF8(errors.PhaseEncode, nil, []byte(s))
	}

	dataLen := uint32(len(s))
	if dataLen > MaxStringSize {
		return 0, errors.New(errors.PhaseEncode, errors.KindOverflow).
			Detail("string size %d exceeds maximum %d", dataLen, MaxStringSize).
			Build()
	}

	if dataLen == 0 {
		stack[offset] = 0
		stack[offset+1] = 0
		return 2, nil
	}

	dataAddr, err := alloc.Alloc(dataLen, 1)
	if err != nil {
		return 0, err
	}
	if allocList != nil {
		allocList.Add(dataAddr, dataLen, 1)
	}

	if err := mem.Write(dataAddr, []byte(s)); err != nil {
		return 0, err
	}

	stack[offset] = uint64(dataAddr)
	stack[offset+1] = uint64(dataLen)
	return 2, nil
}

func (e *Encoder) lowerRecordToStack(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, offset int, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	consumed := 0
	for i := range ct.Fields {
		field := &ct.Fields[i]
		fieldPtr := unsafe.Add(ptr, field.GoOffset)
		n, err := e.lowerToStack(field.Type, fieldPtr, stack, offset+consumed, mem, alloc, allocList)
		if err != nil {
			return 0, err
		}
		consumed += n
	}
	return consumed, nil
}

func (e *Encoder) lowerListToStack(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, offset int, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	type sliceHeader struct {
		Data unsafe.Pointer
		Len  int
		Cap  int
	}
	slice := (*sliceHeader)(ptr)
	length := uint32(slice.Len)

	if length == 0 {
		stack[offset] = 0
		stack[offset+1] = 0
		return 2, nil
	}

	if slice.Data == nil {
		return 0, errors.New(errors.PhaseEncode, errors.KindInvalidData).
			Detail("list has length %d but nil data pointer", length).
			Build()
	}

	elemSize := ct.ElemType.WitSize
	elemAlign := ct.ElemType.WitAlign
	dataSize, ok := abi.SafeMulU32(length, elemSize)
	if !ok || dataSize > MaxAlloc {
		return 0, errors.New(errors.PhaseEncode, errors.KindOverflow).
			Detail("list data size overflow: %d * %d", length, elemSize).
			Build()
	}
	dataAddr, err := alloc.Alloc(dataSize, elemAlign)
	if err != nil {
		return 0, err
	}
	if allocList != nil {
		allocList.Add(dataAddr, dataSize, elemAlign)
	}

	// Fast path for primitives - direct memory write
	switch ct.ElemType.GoType.Kind() {
	case reflect.Int32, reflect.Uint32:
		src := unsafe.Slice((*uint32)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			if err := mem.WriteU32(dataAddr+i*4, src[i]); err != nil {
				return 0, err
			}
		}
		stack[offset] = uint64(dataAddr)
		stack[offset+1] = uint64(length)
		return 2, nil

	case reflect.Int64, reflect.Uint64:
		src := unsafe.Slice((*uint64)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			if err := mem.WriteU64(dataAddr+i*8, src[i]); err != nil {
				return 0, err
			}
		}
		stack[offset] = uint64(dataAddr)
		stack[offset+1] = uint64(length)
		return 2, nil

	case reflect.Float32:
		src := unsafe.Slice((*float32)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			if err := mem.WriteU32(dataAddr+i*4, abi.CanonicalizeF32(math.Float32bits(src[i]))); err != nil {
				return 0, err
			}
		}
		stack[offset] = uint64(dataAddr)
		stack[offset+1] = uint64(length)
		return 2, nil

	case reflect.Float64:
		src := unsafe.Slice((*float64)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			if err := mem.WriteU64(dataAddr+i*8, abi.CanonicalizeF64(math.Float64bits(src[i]))); err != nil {
				return 0, err
			}
		}
		stack[offset] = uint64(dataAddr)
		stack[offset+1] = uint64(length)
		return 2, nil

	case reflect.Uint8:
		src := unsafe.Slice((*byte)(slice.Data), length)
		if err := mem.Write(dataAddr, src); err != nil {
			return 0, err
		}
		stack[offset] = uint64(dataAddr)
		stack[offset+1] = uint64(length)
		return 2, nil

	case reflect.String:
		// Fast path for string lists
		src := unsafe.Slice((*string)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			s := src[i]
			strLen := uint32(len(s))

			// Write string metadata (ptr + len)
			metaAddr := dataAddr + i*8
			if strLen == 0 {
				if err := mem.WriteU32(metaAddr, 0); err != nil {
					return 0, err
				}
				if err := mem.WriteU32(metaAddr+4, 0); err != nil {
					return 0, err
				}
				continue
			}

			// Allocate and write string data
			strAddr, err := alloc.Alloc(strLen, 1)
			if err != nil {
				return 0, err
			}
			if allocList != nil {
				allocList.Add(strAddr, strLen, 1)
			}
			data := unsafe.Slice(unsafe.StringData(s), len(s))
			if err := mem.Write(strAddr, data); err != nil {
				return 0, err
			}

			// Write metadata
			if err := mem.WriteU32(metaAddr, strAddr); err != nil {
				return 0, err
			}
			if err := mem.WriteU32(metaAddr+4, strLen); err != nil {
				return 0, err
			}
		}
		stack[offset] = uint64(dataAddr)
		stack[offset+1] = uint64(length)
		return 2, nil

	case reflect.Struct:
		// Fast path for record lists
		if ct.ElemType.Kind != KindRecord {
			break
		}

		elemGoSize := ct.ElemType.GoSize
		recordSize := ct.ElemType.WitSize
		var metaBuf []byte

		// Small stack buffer for one record (typically 24 bytes)
		var recordStackBuf [128]byte

		for i := uint32(0); i < length; i++ {
			elemPtr := unsafe.Add(slice.Data, uintptr(i)*elemGoSize)
			recordAddr := dataAddr + i*recordSize

			// Use stack buffer if record fits, otherwise allocate
			var recordBuf []byte
			if recordSize <= 128 {
				recordBuf = recordStackBuf[:recordSize]
			} else {
				recordBuf = make([]byte, recordSize)
			}
			// Zero the buffer
			for j := range recordBuf {
				recordBuf[j] = 0
			}

			for _, field := range ct.ElemType.Fields {
				fieldPtr := unsafe.Add(elemPtr, field.GoOffset)

				switch field.Type.Kind {
				case KindU32:
					val := *(*uint32)(fieldPtr)
					binary.LittleEndian.PutUint32(recordBuf[field.WitOffset:], val)
				case KindBool:
					if *(*bool)(fieldPtr) {
						recordBuf[field.WitOffset] = 1
					}
				case KindString:
					s := *(*string)(fieldPtr)
					strLen := uint32(len(s))
					if strLen > 0 {
						strAddr, err := alloc.Alloc(strLen, 1)
						if err != nil {
							return 0, err
						}
						if allocList != nil {
							allocList.Add(strAddr, strLen, 1)
						}
						data := unsafe.Slice(unsafe.StringData(s), strLen)
						if err := mem.Write(strAddr, data); err != nil {
							return 0, err
						}
						binary.LittleEndian.PutUint32(recordBuf[field.WitOffset:], strAddr)
						binary.LittleEndian.PutUint32(recordBuf[field.WitOffset+4:], strLen)
					}
				case KindList:
					if field.Type.ElemType != nil && field.Type.ElemType.Kind == KindString {
						fieldSlice := (*sliceHeader)(fieldPtr)
						listLen := uint32(fieldSlice.Len)

						if listLen > 0 {
							metaSize := listLen * 8
							metaAddr, err := alloc.Alloc(metaSize, 4)
							if err != nil {
								return 0, err
							}
							if allocList != nil {
								allocList.Add(metaAddr, metaSize, 4)
							}

							strings := unsafe.Slice((*string)(fieldSlice.Data), listLen)

							if cap(metaBuf) < int(metaSize) {
								metaBuf = make([]byte, metaSize)
							} else {
								metaBuf = metaBuf[:metaSize]
							}
							for j := range metaBuf {
								metaBuf[j] = 0
							}

							for j := uint32(0); j < listLen; j++ {
								s := strings[j]
								sLen := uint32(len(s))
								off := j * 8

								if sLen > 0 {
									sAddr, err := alloc.Alloc(sLen, 1)
									if err != nil {
										return 0, err
									}
									if allocList != nil {
										allocList.Add(sAddr, sLen, 1)
									}
									sData := unsafe.Slice(unsafe.StringData(s), sLen)
									if err := mem.Write(sAddr, sData); err != nil {
										return 0, err
									}

									binary.LittleEndian.PutUint32(metaBuf[off:], sAddr)
									binary.LittleEndian.PutUint32(metaBuf[off+4:], sLen)
								}
							}
							if err := mem.Write(metaAddr, metaBuf); err != nil {
								return 0, err
							}

							binary.LittleEndian.PutUint32(recordBuf[field.WitOffset:], metaAddr)
							binary.LittleEndian.PutUint32(recordBuf[field.WitOffset+4:], listLen)
						}
					} else {
						var tempStack [2]uint64
						if _, err := e.lowerListToStack(field.Type, fieldPtr, tempStack[:], 0, mem, alloc, allocList); err != nil {
							return 0, err
						}
						addr := uint32(tempStack[0])
						length := uint32(tempStack[1])
						binary.LittleEndian.PutUint32(recordBuf[field.WitOffset:], addr)
						binary.LittleEndian.PutUint32(recordBuf[field.WitOffset+4:], length)
					}
				}
			}

			// Write the record
			if err := mem.Write(recordAddr, recordBuf); err != nil {
				return 0, err
			}
		}

		stack[offset] = uint64(dataAddr)
		stack[offset+1] = uint64(length)
		return 2, nil
	}

	// Slow path for complex types
	elemType := ct.ElemType
	elemGoSize := elemType.GoSize
	for i := uint32(0); i < length; i++ {
		elemPtr := unsafe.Add(slice.Data, uintptr(i)*elemGoSize)
		if err := e.encodeFieldToMemory(dataAddr+i*elemSize, elemType, elemPtr, mem, alloc, allocList, nil); err != nil {
			return 0, err
		}
	}

	stack[offset] = uint64(dataAddr)
	stack[offset+1] = uint64(length)
	return 2, nil
}

func (e *Encoder) lowerOptionToStack(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, offset int, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	goPtr := *(*unsafe.Pointer)(ptr)

	innerCount := ct.ElemType.FlatCount
	if goPtr == nil {
		stack[offset] = 0
		for i := 1; i <= innerCount; i++ {
			stack[offset+i] = 0
		}
		return 1 + innerCount, nil
	}

	stack[offset] = 1
	n, err := e.lowerToStack(ct.ElemType, goPtr, stack, offset+1, mem, alloc, allocList)
	if err != nil {
		return 0, err
	}
	return 1 + n, nil
}

func (e *Encoder) lowerResultToStack(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, offset int, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	// Result[T, E] is represented as Go struct: { Ok *T; Err *E }
	// Only one field should be non-nil at a time
	//
	// Stack layout: [discriminant, payload...]
	// discriminant: 0 = Ok, 1 = Err
	// payload: flattened Ok or Err value (padded to max of both)

	type resultLayout struct {
		Ok  unsafe.Pointer
		Err unsafe.Pointer
	}
	r := (*resultLayout)(ptr)

	okCount := 0
	if ct.OkType != nil {
		okCount = ct.OkType.FlatCount
	}
	errCount := 0
	if ct.ErrType != nil {
		errCount = ct.ErrType.FlatCount
	}
	payloadCount := okCount
	if errCount > payloadCount {
		payloadCount = errCount
	}

	// Zero the payload slots first
	for i := 0; i < payloadCount; i++ {
		stack[offset+1+i] = 0
	}

	if r.Ok != nil {
		stack[offset] = 0 // Ok discriminant
		if ct.OkType != nil {
			_, err := e.lowerToStack(ct.OkType, r.Ok, stack, offset+1, mem, alloc, allocList)
			if err != nil {
				return 0, err
			}
		}
	} else {
		stack[offset] = 1 // Err discriminant
		if ct.ErrType != nil && r.Err != nil {
			_, err := e.lowerToStack(ct.ErrType, r.Err, stack, offset+1, mem, alloc, allocList)
			if err != nil {
				return 0, err
			}
		}
	}

	return 1 + payloadCount, nil
}

func (e *Encoder) lowerVariantToStack(ct *CompiledType, ptr unsafe.Pointer, stack []uint64, offset int, mem Memory, alloc Allocator, allocList *AllocationList) (int, error) {
	// Variant is represented as Go struct with pointer fields for each case.
	// We find the non-nil case to determine the discriminant.
	//
	// Example:
	//   type MyVariant struct {
	//       Case1 *Type1  // non-nil if discriminant == 0
	//       Case2 *Type2  // non-nil if discriminant == 1
	//   }

	// Calculate max payload flat count
	maxPayload := 0
	for _, c := range ct.Cases {
		if c.Type != nil && c.Type.FlatCount > maxPayload {
			maxPayload = c.Type.FlatCount
		}
	}

	// Zero the payload slots
	for i := 0; i < maxPayload; i++ {
		stack[offset+1+i] = 0
	}

	// Find the active case (non-nil pointer field)
	for i, c := range ct.Cases {
		casePtr := *(*unsafe.Pointer)(unsafe.Add(ptr, c.GoOffset))
		if casePtr != nil {
			stack[offset] = uint64(i) // discriminant
			if c.Type != nil {
				_, err := e.lowerToStack(c.Type, casePtr, stack, offset+1, mem, alloc, allocList)
				if err != nil {
					return 0, err
				}
			}
			return 1 + maxPayload, nil
		}
	}

	// No case was active - error
	return 0, errors.New(errors.PhaseEncode, errors.KindInvalidData).
		Detail("variant has no active case").
		Build()
}

// LiftFromStack performs "lift" (WASM -> Go). Returns stack slots consumed.
func (d *Decoder) LiftFromStack(ct *CompiledType, stack []uint64, ptr unsafe.Pointer, mem Memory) (int, error) {
	return d.liftFromStack(ct, stack, 0, ptr, mem)
}

func (d *Decoder) liftFromStack(ct *CompiledType, stack []uint64, offset int, ptr unsafe.Pointer, mem Memory) (int, error) {
	if offset+ct.FlatCount > len(stack) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("stack too small: need %d slots at offset %d, have %d", ct.FlatCount, offset, len(stack)).
			Build()
	}

	switch ct.Kind {
	case KindBool:
		*(*bool)(ptr) = stack[offset] != 0
		return 1, nil

	case KindU8:
		*(*uint8)(ptr) = uint8(stack[offset])
		return 1, nil

	case KindS8:
		*(*int8)(ptr) = int8(stack[offset])
		return 1, nil

	case KindU16:
		*(*uint16)(ptr) = uint16(stack[offset])
		return 1, nil

	case KindS16:
		*(*int16)(ptr) = int16(stack[offset])
		return 1, nil

	case KindU32:
		*(*uint32)(ptr) = uint32(stack[offset])
		return 1, nil

	case KindS32:
		*(*int32)(ptr) = int32(stack[offset])
		return 1, nil

	case KindU64:
		*(*uint64)(ptr) = stack[offset]
		return 1, nil

	case KindS64:
		*(*int64)(ptr) = int64(stack[offset])
		return 1, nil

	case KindF32:
		// Canonicalize NaN per spec
		bits := abi.CanonicalizeF32(uint32(stack[offset]))
		*(*float32)(ptr) = math.Float32frombits(bits)
		return 1, nil

	case KindF64:
		// Canonicalize NaN per spec
		bits := abi.CanonicalizeF64(stack[offset])
		*(*float64)(ptr) = math.Float64frombits(bits)
		return 1, nil

	case KindChar:
		r := rune(stack[offset])
		if !abi.ValidateChar(r) {
			return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
				Detail("invalid Unicode scalar value: 0x%X", stack[offset]).
				Build()
		}
		*(*rune)(ptr) = r
		return 1, nil

	case KindString:
		return d.liftStringFromStack(stack, offset, ptr, mem)

	case KindRecord, KindTuple:
		return d.liftRecordFromStack(ct, stack, offset, ptr, mem)

	case KindList:
		return d.liftListFromStack(ct, stack, offset, ptr, mem)

	case KindOption:
		return d.liftOptionFromStack(ct, stack, offset, ptr, mem)

	case KindResult:
		return d.liftResultFromStack(ct, stack, offset, ptr, mem)

	case KindVariant:
		return d.liftVariantFromStack(ct, stack, offset, ptr, mem)

	case KindEnum:
		disc := stack[offset]
		if disc >= uint64(len(ct.Cases)) {
			return 0, errors.InvalidDiscriminant(errors.PhaseDecode, nil, uint32(disc), uint32(len(ct.Cases)-1))
		}
		rv := reflect.NewAt(ct.GoType, ptr).Elem()
		if rv.CanUint() {
			rv.SetUint(disc)
		} else {
			rv.SetInt(int64(disc))
		}
		return 1, nil

	case KindFlags:
		rv := reflect.NewAt(ct.GoType, ptr).Elem()
		if rv.CanUint() {
			rv.SetUint(stack[offset])
		} else {
			rv.SetInt(int64(stack[offset]))
		}
		return 1, nil

	case KindOwn, KindBorrow:
		// Resource handles are u32 on the stack
		handle := uint32(stack[offset])
		if ct.GoType.Kind() == reflect.Uint32 {
			*(*uint32)(ptr) = handle
		} else {
			// Struct with Handle field (Own[T] or Borrow[T])
			rv := reflect.NewAt(ct.GoType, ptr).Elem()
			handleField := rv.FieldByName("Handle")
			if handleField.IsValid() && handleField.CanSet() {
				handleField.SetUint(uint64(handle))
			}
		}
		return 1, nil

	default:
		return 0, errors.Unsupported(errors.PhaseDecode, "type kind for stack: "+ct.Kind.String())
	}
}

func (d *Decoder) liftStringFromStack(stack []uint64, offset int, ptr unsafe.Pointer, mem Memory) (int, error) {
	if offset+1 >= len(stack) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient stack values for string: need %d, have %d", offset+2, len(stack)).
			Build()
	}

	dataAddr := uint32(stack[offset])
	dataLen := uint32(stack[offset+1])

	if dataLen == 0 {
		*(*string)(ptr) = ""
		return 2, nil
	}

	data, err := mem.Read(dataAddr, dataLen)
	if err != nil {
		return 0, err
	}

	if !utf8.Valid(data) {
		return 0, errors.InvalidUTF8(errors.PhaseDecode, nil, data)
	}

	*(*string)(ptr) = string(data)
	return 2, nil
}

func (d *Decoder) liftRecordFromStack(ct *CompiledType, stack []uint64, offset int, ptr unsafe.Pointer, mem Memory) (int, error) {
	consumed := 0
	for i := range ct.Fields {
		field := &ct.Fields[i]
		fieldPtr := unsafe.Add(ptr, field.GoOffset)
		n, err := d.liftFromStack(field.Type, stack, offset+consumed, fieldPtr, mem)
		if err != nil {
			return 0, err
		}
		consumed += n
	}
	return consumed, nil
}

func (d *Decoder) liftListFromStack(ct *CompiledType, stack []uint64, offset int, ptr unsafe.Pointer, mem Memory) (int, error) {
	if offset+1 >= len(stack) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient stack values for list: need %d, have %d", offset+2, len(stack)).
			Build()
	}

	dataAddr := uint32(stack[offset])
	length := uint32(stack[offset+1])

	type sliceHeader struct {
		Data unsafe.Pointer
		Len  int
		Cap  int
	}
	slice := (*sliceHeader)(ptr)

	// Check for allocation size overflow
	allocSize, ok := abi.SafeMulU32(length, uint32(ct.ElemType.GoSize))
	if !ok {
		return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Detail("list allocation size overflow: %d * %d", length, ct.ElemType.GoSize).
			Build()
	}

	if int(length) != slice.Len || slice.Data == nil {
		*slice = sliceHeader{
			Data: unsafe.Pointer(unsafe.SliceData(make([]byte, allocSize))),
			Len:  int(length),
			Cap:  int(length),
		}
	}

	switch ct.ElemType.GoType.Kind() {
	case reflect.Int32, reflect.Uint32:
		totalSize, ok := abi.SafeMulU32(length, 4)
		if !ok || dataAddr > math.MaxUint32-totalSize {
			return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Detail("list memory range overflow").
				Build()
		}
		dst := unsafe.Slice((*uint32)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU32(dataAddr + i*4)
			if err != nil {
				return 0, err
			}
			dst[i] = val
		}
		return 2, nil

	case reflect.Int64, reflect.Uint64:
		totalSize, ok := abi.SafeMulU32(length, 8)
		if !ok || dataAddr > math.MaxUint32-totalSize {
			return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Detail("list memory range overflow").
				Build()
		}
		dst := unsafe.Slice((*uint64)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU64(dataAddr + i*8)
			if err != nil {
				return 0, err
			}
			dst[i] = val
		}
		return 2, nil

	case reflect.Float32:
		totalSize, ok := abi.SafeMulU32(length, 4)
		if !ok || dataAddr > math.MaxUint32-totalSize {
			return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Detail("list memory range overflow").
				Build()
		}
		dst := unsafe.Slice((*float32)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU32(dataAddr + i*4)
			if err != nil {
				return 0, err
			}
			// Canonicalize NaN per spec
			dst[i] = math.Float32frombits(abi.CanonicalizeF32(val))
		}
		return 2, nil

	case reflect.Float64:
		totalSize, ok := abi.SafeMulU32(length, 8)
		if !ok || dataAddr > math.MaxUint32-totalSize {
			return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Detail("list memory range overflow").
				Build()
		}
		dst := unsafe.Slice((*float64)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU64(dataAddr + i*8)
			if err != nil {
				return 0, err
			}
			// Canonicalize NaN per spec
			dst[i] = math.Float64frombits(abi.CanonicalizeF64(val))
		}
		return 2, nil

	case reflect.Uint8:
		data, err := mem.Read(dataAddr, length)
		if err != nil {
			return 0, err
		}
		dst := unsafe.Slice((*byte)(slice.Data), length)
		copy(dst, data)
		return 2, nil

	case reflect.String:
		// Fast path for string lists - batch read metadata
		metadataSize, ok := abi.SafeMulU32(length, 8)
		if !ok {
			return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Detail("string list metadata size overflow: %d * 8", length).
				Build()
		}
		metadata, err := mem.Read(dataAddr, metadataSize)
		if err != nil {
			return 0, err
		}

		dst := unsafe.Slice((*string)(slice.Data), length)
		for i := uint32(0); i < length; i++ {
			off := i * 8
			strAddr := uint32(metadata[off]) | uint32(metadata[off+1])<<8 | uint32(metadata[off+2])<<16 | uint32(metadata[off+3])<<24
			strLen := uint32(metadata[off+4]) | uint32(metadata[off+5])<<8 | uint32(metadata[off+6])<<16 | uint32(metadata[off+7])<<24

			if strLen == 0 {
				dst[i] = ""
				continue
			}

			data, err := mem.Read(strAddr, strLen)
			if err != nil {
				return 0, err
			}

			if !utf8.Valid(data) {
				return 0, errors.InvalidUTF8(errors.PhaseDecode, nil, data)
			}

			dst[i] = string(data)
		}
		return 2, nil

	case reflect.Struct:
		// Fast path for record lists
		if ct.ElemType.Kind != KindRecord {
			break
		}

		recordSize := ct.ElemType.WitSize
		totalSize, ok := abi.SafeMulU32(length, recordSize)
		if !ok {
			return 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Detail("record list total size overflow: %d * %d", length, recordSize).
				Build()
		}
		elemGoSize := ct.ElemType.GoSize

		// Batch read all record metadata
		allRecords, err := mem.Read(dataAddr, totalSize)
		if err != nil {
			return 0, err
		}
		if uint32(len(allRecords)) != totalSize {
			return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
				Detail("record list short read: got %d, want %d", len(allRecords), totalSize).
				Build()
		}

		type stringRef struct {
			target *string
			addr   uint32
			length uint32
		}
		type listMetaRef struct {
			strSlice *[]string
			strRefs  *[]stringRef
			addr     uint32
			length   uint32
		}

		stringRefs := make([]stringRef, 0, int(length)*4)
		listMetaRefs := make([]listMetaRef, 0, int(length))

		if int(length) != slice.Len {
			*slice = sliceHeader{
				Data: unsafe.Pointer(unsafe.SliceData(make([]byte, allocSize))),
				Len:  int(length),
				Cap:  int(length),
			}
		}

		// First pass: collect all addresses and parse primitives
		for i := uint32(0); i < length; i++ {
			elemPtr := unsafe.Add(slice.Data, uintptr(i)*elemGoSize)
			recordData := allRecords[i*recordSize : (i+1)*recordSize]

			for _, field := range ct.ElemType.Fields {
				fieldPtr := unsafe.Add(elemPtr, field.GoOffset)
				fieldData := recordData[field.WitOffset:]

				switch field.Type.Kind {
				case KindU32:
					val := uint32(fieldData[0]) | uint32(fieldData[1])<<8 | uint32(fieldData[2])<<16 | uint32(fieldData[3])<<24
					*(*uint32)(fieldPtr) = val
				case KindBool:
					*(*bool)(fieldPtr) = fieldData[0] != 0
				case KindString:
					strAddr := uint32(fieldData[0]) | uint32(fieldData[1])<<8 | uint32(fieldData[2])<<16 | uint32(fieldData[3])<<24
					strLen := uint32(fieldData[4]) | uint32(fieldData[5])<<8 | uint32(fieldData[6])<<16 | uint32(fieldData[7])<<24

					if strLen == 0 {
						*(*string)(fieldPtr) = ""
					} else {
						stringRefs = append(stringRefs, stringRef{
							addr:   strAddr,
							length: strLen,
							target: (*string)(fieldPtr),
						})
					}
				case KindList:
					listAddr := uint32(fieldData[0]) | uint32(fieldData[1])<<8 | uint32(fieldData[2])<<16 | uint32(fieldData[3])<<24
					listLen := uint32(fieldData[4]) | uint32(fieldData[5])<<8 | uint32(fieldData[6])<<16 | uint32(fieldData[7])<<24

					if listLen == 0 {
						fieldSlice := (*sliceHeader)(fieldPtr)
						*fieldSlice = sliceHeader{Data: nil, Len: 0, Cap: 0}
					} else if field.Type.ElemType != nil && field.Type.ElemType.Kind == KindString {
						// Collect list<string> metadata address for batch reading
						strings := make([]string, listLen)
						strRefs := make([]stringRef, 0, listLen)
						listMetaRefs = append(listMetaRefs, listMetaRef{
							addr:     listAddr,
							length:   listLen * 8,
							strSlice: &strings,
							strRefs:  &strRefs,
						})
						fieldSlice := (*sliceHeader)(fieldPtr)
						*fieldSlice = *(*sliceHeader)(unsafe.Pointer(&strings))
					} else {
						var tempStack [2]uint64
						tempStack[0] = uint64(listAddr)
						tempStack[1] = uint64(listLen)
						if _, err := d.liftListFromStack(field.Type, tempStack[:], 0, fieldPtr, mem); err != nil {
							return 0, err
						}
					}
				}
			}
		}

		// Batch read all list<string> metadata
		if len(listMetaRefs) > 0 {
			for _, ref := range listMetaRefs {
				listMeta, err := mem.Read(ref.addr, ref.length)
				if err != nil {
					return 0, err
				}
				if uint32(len(listMeta)) != ref.length {
					return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
						Detail("list<string> metadata short read: got %d, want %d", len(listMeta), ref.length).
						Build()
				}

				listLen := ref.length / 8
				for j := uint32(0); j < listLen; j++ {
					metaOff := j * 8
					sAddr := uint32(listMeta[metaOff]) | uint32(listMeta[metaOff+1])<<8 | uint32(listMeta[metaOff+2])<<16 | uint32(listMeta[metaOff+3])<<24
					sLen := uint32(listMeta[metaOff+4]) | uint32(listMeta[metaOff+5])<<8 | uint32(listMeta[metaOff+6])<<16 | uint32(listMeta[metaOff+7])<<24

					if sLen == 0 {
						(*ref.strSlice)[j] = ""
					} else {
						*ref.strRefs = append(*ref.strRefs, stringRef{
							addr:   sAddr,
							length: sLen,
							target: &(*ref.strSlice)[j],
						})
					}
				}
				stringRefs = append(stringRefs, *ref.strRefs...)
			}
		}

		// If no strings to read, we're done
		if len(stringRefs) == 0 {
			return 2, nil
		}

		// Find min and max addresses to determine memory span
		minAddr, maxAddr := stringRefs[0].addr, stringRefs[0].addr+stringRefs[0].length
		for _, ref := range stringRefs[1:] {
			if ref.addr < minAddr {
				minAddr = ref.addr
			}
			end := ref.addr + ref.length
			if end > maxAddr {
				maxAddr = end
			}
		}

		// Read entire memory region containing all strings
		totalSpan := maxAddr - minAddr
		const maxBatchSize = 1024 * 1024 // 1MB max batch

		if totalSpan <= maxBatchSize {
			// Read all strings in one batch
			batchData, err := mem.Read(minAddr, totalSpan)
			if err != nil {
				return 0, err
			}

			for _, ref := range stringRefs {
				off := ref.addr - minAddr
				strData := batchData[off : off+ref.length]
				if !utf8.Valid(strData) {
					return 0, errors.InvalidUTF8(errors.PhaseDecode, nil, strData)
				}
				*ref.target = string(strData)
			}
		} else {
			// Fallback: read strings individually
			for _, ref := range stringRefs {
				data, err := mem.Read(ref.addr, ref.length)
				if err != nil {
					return 0, err
				}
				if !utf8.Valid(data) {
					return 0, errors.InvalidUTF8(errors.PhaseDecode, nil, data)
				}
				*ref.target = string(data)
			}
		}

		return 2, nil
	}

	elemType := ct.ElemType
	elemGoSize := elemType.GoSize
	for i := uint32(0); i < length; i++ {
		elemPtr := unsafe.Add(slice.Data, uintptr(i)*elemGoSize)
		if err := d.decodeFieldFromMemory(dataAddr+i*elemType.WitSize, elemType, elemPtr, mem, nil); err != nil {
			return 0, err
		}
	}

	return 2, nil
}

func (d *Decoder) liftOptionFromStack(ct *CompiledType, stack []uint64, offset int, ptr unsafe.Pointer, mem Memory) (int, error) {
	if offset >= len(stack) {
		return 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Detail("insufficient stack values for option").
			Build()
	}
	disc := stack[offset]
	innerCount := ct.ElemType.FlatCount

	if disc == 0 {
		*(*unsafe.Pointer)(ptr) = nil
		return 1 + innerCount, nil
	}
	if disc != 1 {
		return 0, errors.InvalidDiscriminant(errors.PhaseDecode, nil, uint32(disc), 1)
	}

	// Allocate and decode
	elemVal := reflect.New(ct.ElemType.GoType)
	elemPtr := unsafe.Pointer(elemVal.Pointer())

	n, err := d.liftFromStack(ct.ElemType, stack, offset+1, elemPtr, mem)
	if err != nil {
		return 0, err
	}

	*(*unsafe.Pointer)(ptr) = elemPtr
	return 1 + n, nil
}

func (d *Decoder) liftResultFromStack(ct *CompiledType, stack []uint64, offset int, ptr unsafe.Pointer, mem Memory) (int, error) {
	// Result[T, E] is represented as Go struct: { Ok *T; Err *E }
	// Stack layout: [discriminant, payload...]

	type resultLayout struct {
		Ok  unsafe.Pointer
		Err unsafe.Pointer
	}
	r := (*resultLayout)(ptr)

	disc := stack[offset]

	okCount := 0
	if ct.OkType != nil {
		okCount = ct.OkType.FlatCount
	}
	errCount := 0
	if ct.ErrType != nil {
		errCount = ct.ErrType.FlatCount
	}
	payloadCount := okCount
	if errCount > payloadCount {
		payloadCount = errCount
	}

	if disc == 0 {
		// Ok
		r.Err = nil
		if ct.OkType != nil {
			okVal := reflect.New(ct.OkType.GoType)
			okPtr := unsafe.Pointer(okVal.Pointer())
			_, err := d.liftFromStack(ct.OkType, stack, offset+1, okPtr, mem)
			if err != nil {
				return 0, err
			}
			r.Ok = okPtr
		} else {
			// Unit Ok - use package-level sentinel for stable pointer
			r.Ok = UnitPtr()
		}
	} else {
		// Err
		r.Ok = nil
		if ct.ErrType != nil {
			errVal := reflect.New(ct.ErrType.GoType)
			errPtr := unsafe.Pointer(errVal.Pointer())
			_, err := d.liftFromStack(ct.ErrType, stack, offset+1, errPtr, mem)
			if err != nil {
				return 0, err
			}
			r.Err = errPtr
		} else {
			// Unit Err - use package-level sentinel for stable pointer
			r.Err = UnitPtr()
		}
	}

	return 1 + payloadCount, nil
}

func (d *Decoder) liftVariantFromStack(ct *CompiledType, stack []uint64, offset int, ptr unsafe.Pointer, mem Memory) (int, error) {
	// Variant is represented as Go struct with pointer fields for each case.
	// We set the discriminant's case field to non-nil, others to nil.

	disc := int(stack[offset])

	// Calculate max payload flat count
	maxPayload := 0
	for _, c := range ct.Cases {
		if c.Type != nil && c.Type.FlatCount > maxPayload {
			maxPayload = c.Type.FlatCount
		}
	}

	// Validate discriminant
	if disc < 0 || disc >= len(ct.Cases) {
		return 0, errors.InvalidDiscriminant(errors.PhaseDecode, nil, uint32(disc), uint32(len(ct.Cases)-1))
	}

	// Clear all case fields first
	for _, c := range ct.Cases {
		caseField := (*unsafe.Pointer)(unsafe.Add(ptr, c.GoOffset))
		*caseField = nil
	}

	// Set the active case
	activeCase := ct.Cases[disc]
	if activeCase.Type != nil {
		// Allocate and decode the payload
		caseVal := reflect.New(activeCase.Type.GoType)
		casePtr := unsafe.Pointer(caseVal.Pointer())
		_, err := d.liftFromStack(activeCase.Type, stack, offset+1, casePtr, mem)
		if err != nil {
			return 0, err
		}
		caseField := (*unsafe.Pointer)(unsafe.Add(ptr, activeCase.GoOffset))
		*caseField = casePtr
	} else {
		// Unit case - use package-level sentinel for stable pointer
		caseField := (*unsafe.Pointer)(unsafe.Add(ptr, activeCase.GoOffset))
		*caseField = UnitPtr()
	}

	return 1 + maxPayload, nil
}
