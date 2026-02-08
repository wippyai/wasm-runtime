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

type Decoder struct {
	compiler *Compiler
}

func NewDecoder() *Decoder {
	return &Decoder{
		compiler: NewCompiler(),
	}
}

func NewDecoderWithCompiler(c *Compiler) *Decoder {
	return &Decoder{compiler: c}
}

func (d *Decoder) DecodeResults(resultTypes []wit.Type, flat []uint64, mem Memory) ([]any, error) {
	results := make([]any, 0, len(resultTypes))
	offset := 0

	for i, resultType := range resultTypes {
		result, consumed, err := d.liftValue(resultType, flat[offset:], mem, []string{"result[" + strconv.Itoa(i) + "]"})
		if err != nil {
			return nil, err
		}
		results = append(results, result)
		offset += consumed
	}

	return results, nil
}

func (d *Decoder) LoadValue(witType wit.Type, addr uint32, mem Memory) (any, error) {
	return d.loadValue(witType, addr, mem, nil)
}

func (d *Decoder) decodeFieldFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	switch ct.Kind {
	case KindBool:
		v, err := mem.ReadU8(addr)
		if err != nil {
			return err
		}
		*(*bool)(ptr) = v != 0
		return nil

	case KindU8:
		v, err := mem.ReadU8(addr)
		if err != nil {
			return err
		}
		*(*uint8)(ptr) = v
		return nil

	case KindS8:
		v, err := mem.ReadU8(addr)
		if err != nil {
			return err
		}
		*(*int8)(ptr) = int8(v)
		return nil

	case KindU16:
		v, err := mem.ReadU16(addr)
		if err != nil {
			return err
		}
		*(*uint16)(ptr) = v
		return nil

	case KindS16:
		v, err := mem.ReadU16(addr)
		if err != nil {
			return err
		}
		*(*int16)(ptr) = int16(v)
		return nil

	case KindU32:
		v, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		*(*uint32)(ptr) = v
		return nil

	case KindS32:
		v, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		*(*int32)(ptr) = int32(v)
		return nil

	case KindU64:
		v, err := mem.ReadU64(addr)
		if err != nil {
			return err
		}
		*(*uint64)(ptr) = v
		return nil

	case KindS64:
		v, err := mem.ReadU64(addr)
		if err != nil {
			return err
		}
		*(*int64)(ptr) = int64(v)
		return nil

	case KindF32:
		bits, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		// Canonicalize NaN per spec
		*(*float32)(ptr) = math.Float32frombits(abi.CanonicalizeF32(bits))
		return nil

	case KindF64:
		bits, err := mem.ReadU64(addr)
		if err != nil {
			return err
		}
		// Canonicalize NaN per spec
		*(*float64)(ptr) = math.Float64frombits(abi.CanonicalizeF64(bits))
		return nil

	case KindChar:
		v, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		r := rune(v)
		if !abi.ValidateChar(r) {
			return errors.New(errors.PhaseDecode, errors.KindInvalidData).
				Path(path...).
				Detail("invalid Unicode scalar value: 0x%X", v).
				Build()
		}
		*(*rune)(ptr) = r
		return nil

	case KindString:
		s, err := d.decodeStringFromMemory(addr, mem, path)
		if err != nil {
			return err
		}
		*(*string)(ptr) = s
		return nil

	case KindRecord:
		return d.decodeRecordFromMemory(addr, ct, ptr, mem, path)

	case KindList:
		return d.decodeListFromMemory(addr, ct, ptr, mem, path)

	case KindOption:
		return d.decodeOptionFromMemory(addr, ct, ptr, mem, path)

	case KindResult:
		return d.decodeResultFromMemory(addr, ct, ptr, mem, path)

	case KindTuple:
		return d.decodeTupleFromMemory(addr, ct, ptr, mem, path)

	case KindVariant:
		return d.decodeVariantFromMemory(addr, ct, ptr, mem, path)

	case KindEnum:
		return d.decodeEnumFromMemory(addr, ct, ptr, mem, path)

	case KindFlags:
		return d.decodeFlagsFromMemory(addr, ct, ptr, mem)

	case KindOwn, KindBorrow:
		handle, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		*(*uint32)(ptr) = handle
		return nil

	default:
		return errors.Unsupported(errors.PhaseDecode, "type kind: "+ct.Kind.String())
	}
}

func (d *Decoder) decodeStringFromMemory(addr uint32, mem Memory, path []string) (string, error) {
	dataAddr, err := mem.ReadU32(addr)
	if err != nil {
		return "", err
	}
	dataLen, err := mem.ReadU32(addr + 4)
	if err != nil {
		return "", err
	}

	if dataLen == 0 {
		return "", nil
	}

	if dataLen > MaxStringSize {
		return "", errors.New(errors.PhaseDecode, errors.KindOverflow).
			Path(path...).
			Detail("string size %d exceeds maximum %d", dataLen, MaxStringSize).
			Build()
	}

	data, err := mem.Read(dataAddr, dataLen)
	if err != nil {
		return "", err
	}

	if !utf8.Valid(data) {
		return "", errors.InvalidUTF8(errors.PhaseDecode, path, data)
	}

	return string(data), nil
}

func (d *Decoder) decodeRecordFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	for _, field := range ct.Fields {
		fieldPtr := unsafe.Add(ptr, field.GoOffset)
		fieldPath := append(append([]string{}, path...), field.WitName)
		if err := d.decodeFieldFromMemory(addr+field.WitOffset, field.Type, fieldPtr, mem, fieldPath); err != nil {
			return err
		}
	}
	return nil
}

func (d *Decoder) decodeListFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	dataAddr, err := mem.ReadU32(addr)
	if err != nil {
		return err
	}
	length, err := mem.ReadU32(addr + 4)
	if err != nil {
		return err
	}

	if length > MaxListLength {
		return errors.New(errors.PhaseDecode, errors.KindOverflow).
			Path(path...).
			Detail("list length %d exceeds maximum %d", length, MaxListLength).
			Build()
	}

	// Create slice via reflect - use cached SliceType when available
	sliceType := ct.ElemType.SliceType
	if sliceType == nil {
		sliceType = reflect.SliceOf(ct.ElemType.GoType)
	}
	slice := reflect.MakeSlice(sliceType, int(length), int(length))

	// Handle empty list early to avoid index panics
	if length == 0 {
		dstSlice := reflect.NewAt(sliceType, ptr).Elem()
		dstSlice.Set(slice)
		return nil
	}

	// Validate address range doesn't overflow
	totalSize, ok := abi.SafeMulU32(length, ct.ElemType.WitSize)
	if !ok {
		return errors.New(errors.PhaseDecode, errors.KindOverflow).
			Path(path...).
			Detail("list data size overflow").
			Build()
	}
	if _, ok := abi.SafeAddU32(dataAddr, totalSize); !ok {
		return errors.New(errors.PhaseDecode, errors.KindOverflow).
			Path(path...).
			Detail("list data address range overflow").
			Build()
	}

	// Fast path for primitive types - direct memory read
	switch ct.ElemType.GoType.Kind() {
	case reflect.Uint8:
		// []byte - single bulk read
		data, err := mem.Read(dataAddr, length)
		if err != nil {
			return err
		}
		dst := unsafe.Slice((*byte)(unsafe.Pointer(slice.Index(0).UnsafeAddr())), length)
		copy(dst, data)
		dstSlice := reflect.NewAt(sliceType, ptr).Elem()
		dstSlice.Set(slice)
		return nil
	case reflect.Int32, reflect.Uint32:
		dst := unsafe.Slice((*uint32)(unsafe.Pointer(slice.Index(0).UnsafeAddr())), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU32(dataAddr + i*4)
			if err != nil {
				return err
			}
			dst[i] = val
		}
		dstSlice := reflect.NewAt(sliceType, ptr).Elem()
		dstSlice.Set(slice)
		return nil
	case reflect.Int64, reflect.Uint64:
		dst := unsafe.Slice((*uint64)(unsafe.Pointer(slice.Index(0).UnsafeAddr())), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU64(dataAddr + i*8)
			if err != nil {
				return err
			}
			dst[i] = val
		}
		dstSlice := reflect.NewAt(sliceType, ptr).Elem()
		dstSlice.Set(slice)
		return nil
	case reflect.Float32:
		dst := unsafe.Slice((*float32)(unsafe.Pointer(slice.Index(0).UnsafeAddr())), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU32(dataAddr + i*4)
			if err != nil {
				return err
			}
			// Canonicalize NaN per spec
			dst[i] = math.Float32frombits(abi.CanonicalizeF32(val))
		}
		dstSlice := reflect.NewAt(sliceType, ptr).Elem()
		dstSlice.Set(slice)
		return nil
	case reflect.Float64:
		dst := unsafe.Slice((*float64)(unsafe.Pointer(slice.Index(0).UnsafeAddr())), length)
		for i := uint32(0); i < length; i++ {
			val, err := mem.ReadU64(dataAddr + i*8)
			if err != nil {
				return err
			}
			// Canonicalize NaN per spec
			dst[i] = math.Float64frombits(abi.CanonicalizeF64(val))
		}
		dstSlice := reflect.NewAt(sliceType, ptr).Elem()
		dstSlice.Set(slice)
		return nil
	}

	// Slow path for complex types - decode each element
	for i := uint32(0); i < length; i++ {
		elemPtr := unsafe.Pointer(slice.Index(int(i)).UnsafeAddr())
		elemPath := append(append([]string{}, path...), "["+strconv.FormatUint(uint64(i), 10)+"]")
		if err := d.decodeFieldFromMemory(dataAddr+i*ct.ElemType.WitSize, ct.ElemType, elemPtr, mem, elemPath); err != nil {
			return err
		}
	}

	// Set slice to destination using reflect.Value.Set
	dstSlice := reflect.NewAt(sliceType, ptr).Elem()
	dstSlice.Set(slice)
	return nil
}

func (d *Decoder) decodeOptionFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	disc, err := mem.ReadU8(addr)
	if err != nil {
		return err
	}

	if disc == 0 {
		// None: set pointer to nil
		*(*unsafe.Pointer)(ptr) = nil
		return nil
	}

	// Some: allocate and decode
	payloadOffset := alignTo(1, ct.ElemType.WitAlign)

	// Create new value via reflect
	elemVal := reflect.New(ct.ElemType.GoType)
	elemPtr := unsafe.Pointer(elemVal.Pointer())

	somePath := append(append([]string{}, path...), "[some]")
	if err := d.decodeFieldFromMemory(addr+payloadOffset, ct.ElemType, elemPtr, mem, somePath); err != nil {
		return err
	}

	*(*unsafe.Pointer)(ptr) = elemPtr
	return nil
}

func (d *Decoder) decodeResultFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	// Result is represented as struct: { Ok *T; Err *E }
	type resultLayout struct {
		Ok  unsafe.Pointer
		Err unsafe.Pointer
	}
	r := (*resultLayout)(ptr)

	disc, err := mem.ReadU8(addr)
	if err != nil {
		return err
	}
	if disc > 1 {
		return errors.InvalidDiscriminant(errors.PhaseDecode, path, uint32(disc), 1)
	}

	// Determine max alignment
	maxAlign := uint32(1)
	if ct.OkType != nil && ct.OkType.WitAlign > maxAlign {
		maxAlign = ct.OkType.WitAlign
	}
	if ct.ErrType != nil && ct.ErrType.WitAlign > maxAlign {
		maxAlign = ct.ErrType.WitAlign
	}
	payloadOffset := alignTo(1, maxAlign)

	if disc == 0 {
		r.Err = nil
		if ct.OkType != nil {
			okVal := reflect.New(ct.OkType.GoType)
			okPtr := unsafe.Pointer(okVal.Pointer())
			if err := d.decodeFieldFromMemory(addr+payloadOffset, ct.OkType, okPtr, mem, path); err != nil {
				return err
			}
			r.Ok = okPtr
		} else {
			r.Ok = UnitPtr()
		}
	} else {
		r.Ok = nil
		if ct.ErrType != nil {
			errVal := reflect.New(ct.ErrType.GoType)
			errPtr := unsafe.Pointer(errVal.Pointer())
			if err := d.decodeFieldFromMemory(addr+payloadOffset, ct.ErrType, errPtr, mem, path); err != nil {
				return err
			}
			r.Err = errPtr
		} else {
			r.Err = UnitPtr()
		}
	}
	return nil
}

func (d *Decoder) decodeTupleFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	for _, field := range ct.Fields {
		fieldPtr := unsafe.Add(ptr, field.GoOffset)
		if err := d.decodeFieldFromMemory(addr+field.WitOffset, field.Type, fieldPtr, mem, path); err != nil {
			return err
		}
	}
	return nil
}

func (d *Decoder) decodeVariantFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	discSize := abi.DiscriminantSize(len(ct.Cases))

	var disc uint32
	switch discSize {
	case 1:
		d8, err := mem.ReadU8(addr)
		if err != nil {
			return err
		}
		disc = uint32(d8)
	case 2:
		d16, err := mem.ReadU16(addr)
		if err != nil {
			return err
		}
		disc = uint32(d16)
	case 4:
		d32, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		disc = d32
	}

	if disc >= uint32(len(ct.Cases)) {
		return errors.InvalidDiscriminant(errors.PhaseDecode, path, disc, uint32(len(ct.Cases)-1))
	}

	// Clear all case fields
	for _, c := range ct.Cases {
		caseField := (*unsafe.Pointer)(unsafe.Add(ptr, c.GoOffset))
		*caseField = nil
	}

	// Determine max alignment for payload offset
	maxAlign := discSize
	for _, c := range ct.Cases {
		if c.Type != nil && c.Type.WitAlign > maxAlign {
			maxAlign = c.Type.WitAlign
		}
	}
	payloadOffset := alignTo(discSize, maxAlign)

	activeCase := ct.Cases[disc]
	caseField := (*unsafe.Pointer)(unsafe.Add(ptr, activeCase.GoOffset))

	if activeCase.Type != nil {
		caseVal := reflect.New(activeCase.Type.GoType)
		casePtr := unsafe.Pointer(caseVal.Pointer())
		if err := d.decodeFieldFromMemory(addr+payloadOffset, activeCase.Type, casePtr, mem, path); err != nil {
			return err
		}
		*caseField = casePtr
	} else {
		*caseField = UnitPtr()
	}

	return nil
}

func (d *Decoder) decodeEnumFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory, path []string) error {
	discSize := abi.DiscriminantSize(len(ct.Cases))

	var disc uint32
	switch discSize {
	case 1:
		d8, err := mem.ReadU8(addr)
		if err != nil {
			return err
		}
		disc = uint32(d8)
	case 2:
		d16, err := mem.ReadU16(addr)
		if err != nil {
			return err
		}
		disc = uint32(d16)
	case 4:
		d32, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		disc = d32
	}

	if disc >= uint32(len(ct.Cases)) {
		return errors.InvalidDiscriminant(errors.PhaseDecode, path, disc, uint32(len(ct.Cases)-1))
	}

	// Write discriminant based on Go type size
	switch ct.GoSize {
	case 1:
		*(*uint8)(ptr) = uint8(disc)
	case 2:
		*(*uint16)(ptr) = uint16(disc)
	case 4:
		*(*uint32)(ptr) = disc
	case 8:
		*(*uint64)(ptr) = uint64(disc)
	default:
		*(*uint32)(ptr) = disc
	}
	return nil
}

func (d *Decoder) decodeFlagsFromMemory(addr uint32, ct *CompiledType, ptr unsafe.Pointer, mem Memory) error {
	numFlags := len(ct.Cases)

	if numFlags <= 8 {
		v, err := mem.ReadU8(addr)
		if err != nil {
			return err
		}
		*(*uint8)(ptr) = v
	} else if numFlags <= 16 {
		v, err := mem.ReadU16(addr)
		if err != nil {
			return err
		}
		*(*uint16)(ptr) = v
	} else if numFlags <= 32 {
		v, err := mem.ReadU32(addr)
		if err != nil {
			return err
		}
		*(*uint32)(ptr) = v
	} else if numFlags <= 64 {
		v, err := mem.ReadU64(addr)
		if err != nil {
			return err
		}
		*(*uint64)(ptr) = v
	} else {
		// >64 flags: multiple u32s per Canonical ABI spec
		numU32s := (numFlags + 31) / 32
		u32Ptr := (*uint32)(ptr)
		for i := 0; i < numU32s; i++ {
			word, err := mem.ReadU32(addr + uint32(i*4))
			if err != nil {
				return err
			}
			*(*uint32)(unsafe.Add(unsafe.Pointer(u32Ptr), i*4)) = word
		}
	}
	return nil
}

func (d *Decoder) liftValue(witType wit.Type, flat []uint64, mem Memory, path []string) (any, int, error) {
	// Bounds check for primitives that access flat[0]
	if len(flat) < 1 {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values").
			Build()
	}

	switch t := witType.(type) {
	case wit.Bool:
		return flat[0] != 0, 1, nil
	case wit.U8:
		return uint8(flat[0]), 1, nil
	case wit.S8:
		return int8(flat[0]), 1, nil
	case wit.U16:
		return uint16(flat[0]), 1, nil
	case wit.S16:
		return int16(flat[0]), 1, nil
	case wit.U32:
		return uint32(flat[0]), 1, nil
	case wit.S32:
		return int32(flat[0]), 1, nil
	case wit.U64:
		return flat[0], 1, nil
	case wit.S64:
		return int64(flat[0]), 1, nil
	case wit.F32:
		return math.Float32frombits(abi.CanonicalizeF32(uint32(flat[0]))), 1, nil
	case wit.F64:
		return math.Float64frombits(abi.CanonicalizeF64(flat[0])), 1, nil
	case wit.Char:
		r := rune(flat[0])
		if !abi.ValidateChar(r) {
			return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
				Path(path...).
				Detail("invalid Unicode scalar value: 0x%X", flat[0]).
				Build()
		}
		return r, 1, nil
	case wit.String:
		return d.liftString(flat, mem, path)
	case *wit.TypeDef:
		return d.liftTypeDef(t, flat, mem, path)
	default:
		return nil, 0, errors.Unsupported(errors.PhaseDecode, "WIT type")
	}
}

func (d *Decoder) liftString(flat []uint64, mem Memory, path []string) (string, int, error) {
	if len(flat) < 2 {
		return "", 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values for string").
			Build()
	}

	dataAddr := uint32(flat[0])
	dataLen := uint32(flat[1])

	if dataLen == 0 {
		return "", 2, nil
	}

	if dataLen > MaxStringSize {
		return "", 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Path(path...).
			Detail("string size %d exceeds maximum %d", dataLen, MaxStringSize).
			Build()
	}

	data, err := mem.Read(dataAddr, dataLen)
	if err != nil {
		return "", 0, err
	}

	if !utf8.Valid(data) {
		return "", 0, errors.InvalidUTF8(errors.PhaseDecode, path, data)
	}

	return string(data), 2, nil
}

func (d *Decoder) liftTypeDef(t *wit.TypeDef, flat []uint64, mem Memory, path []string) (any, int, error) {
	switch kind := t.Kind.(type) {
	case *wit.Record:
		return d.liftRecord(kind, flat, mem, path)
	case *wit.List:
		return d.liftList(kind, flat, mem, path)
	case *wit.Option:
		return d.liftOption(kind, flat, mem, path)
	case *wit.Tuple:
		return d.liftTuple(kind, flat, mem, path)
	case *wit.Enum:
		return d.liftEnum(kind, flat, path)
	case *wit.Flags:
		return d.liftFlags(kind, flat, path)
	case *wit.Result:
		return d.liftResult(kind, flat, mem, path)
	case *wit.Variant:
		return d.liftVariant(kind, flat, mem, path)
	case wit.Type:
		return d.liftValue(kind, flat, mem, path)
	default:
		return nil, 0, errors.Unsupported(errors.PhaseDecode, "TypeDef kind")
	}
}

func (d *Decoder) liftRecord(r *wit.Record, flat []uint64, mem Memory, path []string) (map[string]any, int, error) {
	result := make(map[string]any, len(r.Fields))
	offset := 0

	for _, field := range r.Fields {
		fieldPath := append(append([]string{}, path...), field.Name)
		val, consumed, err := d.liftValue(field.Type, flat[offset:], mem, fieldPath)
		if err != nil {
			return nil, 0, err
		}
		result[field.Name] = val
		offset += consumed
	}

	return result, offset, nil
}

func (d *Decoder) liftList(l *wit.List, flat []uint64, mem Memory, path []string) (any, int, error) {
	if len(flat) < 2 {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values for list").
			Build()
	}

	dataAddr := uint32(flat[0])
	length := uint32(flat[1])

	if length == 0 {
		return d.liftEmptyList(l.Type), 2, nil
	}

	if length > MaxListLength {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Path(path...).
			Detail("list length %d exceeds maximum %d", length, MaxListLength).
			Build()
	}

	// Fast paths for primitive types
	switch l.Type.(type) {
	case wit.U8, wit.S8:
		return d.liftByteList(l.Type, dataAddr, length, mem)
	case wit.U16, wit.S16:
		return d.lift16BitList(l.Type, dataAddr, length, mem)
	case wit.U32, wit.S32, wit.F32:
		return d.lift32BitList(l.Type, dataAddr, length, mem)
	case wit.U64, wit.S64, wit.F64:
		return d.lift64BitList(l.Type, dataAddr, length, mem)
	case wit.String:
		return d.liftStringList(dataAddr, length, mem, path)
	}

	// Slow path for complex types
	lc := d.compiler.layout
	elemLayout := lc.Calculate(l.Type)
	result := make([]any, length)
	for i := uint32(0); i < length; i++ {
		val, err := d.loadValue(l.Type, dataAddr+i*elemLayout.Size, mem, nil)
		if err != nil {
			elemPath := append(append([]string{}, path...), "["+strconv.FormatUint(uint64(i), 10)+"]")
			return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
				Path(elemPath...).
				Detail("failed to load element: %v", err).
				Build()
		}
		result[i] = val
	}

	return result, 2, nil
}

func (d *Decoder) liftEmptyList(typ wit.Type) any {
	switch typ.(type) {
	case wit.U8:
		return []uint8(nil)
	case wit.S8:
		return []int8(nil)
	case wit.U16:
		return []uint16(nil)
	case wit.S16:
		return []int16(nil)
	case wit.U32:
		return []uint32(nil)
	case wit.S32:
		return []int32(nil)
	case wit.U64:
		return []uint64(nil)
	case wit.S64:
		return []int64(nil)
	case wit.F32:
		return []float32(nil)
	case wit.F64:
		return []float64(nil)
	case wit.String:
		return []string(nil)
	default:
		return []any(nil)
	}
}

func (d *Decoder) liftByteList(typ wit.Type, addr uint32, length uint32, mem Memory) (any, int, error) {
	data, err := mem.Read(addr, length)
	if err != nil {
		return nil, 0, err
	}

	switch typ.(type) {
	case wit.U8:
		result := make([]uint8, length)
		copy(result, data)
		return result, 2, nil
	case wit.S8:
		result := make([]int8, length)
		for i := uint32(0); i < length; i++ {
			result[i] = int8(data[i])
		}
		return result, 2, nil
	}
	return nil, 0, errors.Unsupported(errors.PhaseDecode, "byte list type")
}

func (d *Decoder) lift16BitList(typ wit.Type, addr uint32, length uint32, mem Memory) (any, int, error) {
	dataSize, ok := safeMulU32(length, 2)
	if !ok || dataSize > MaxAlloc {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Detail("list data size overflow: %d * 2", length).
			Build()
	}
	data, err := mem.Read(addr, dataSize)
	if err != nil {
		return nil, 0, err
	}

	switch typ.(type) {
	case wit.U16:
		result := make([]uint16, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*2), data)
		return result, 2, nil
	case wit.S16:
		result := make([]int16, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*2), data)
		return result, 2, nil
	}
	return nil, 0, errors.Unsupported(errors.PhaseDecode, "16-bit list type")
}

func (d *Decoder) lift32BitList(typ wit.Type, addr uint32, length uint32, mem Memory) (any, int, error) {
	dataSize, ok := safeMulU32(length, 4)
	if !ok || dataSize > MaxAlloc {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Detail("list data size overflow: %d * 4", length).
			Build()
	}
	data, err := mem.Read(addr, dataSize)
	if err != nil {
		return nil, 0, err
	}

	switch typ.(type) {
	case wit.U32:
		result := make([]uint32, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*4), data)
		return result, 2, nil
	case wit.S32:
		result := make([]int32, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*4), data)
		return result, 2, nil
	case wit.F32:
		result := make([]float32, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*4), data)
		// Canonicalize NaN values per spec
		for i := range result {
			bits := math.Float32bits(result[i])
			result[i] = math.Float32frombits(abi.CanonicalizeF32(bits))
		}
		return result, 2, nil
	}
	return nil, 0, errors.Unsupported(errors.PhaseDecode, "32-bit list type")
}

func (d *Decoder) lift64BitList(typ wit.Type, addr uint32, length uint32, mem Memory) (any, int, error) {
	dataSize, ok := safeMulU32(length, 8)
	if !ok || dataSize > MaxAlloc {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Detail("list data size overflow: %d * 8", length).
			Build()
	}
	data, err := mem.Read(addr, dataSize)
	if err != nil {
		return nil, 0, err
	}

	switch typ.(type) {
	case wit.U64:
		result := make([]uint64, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*8), data)
		return result, 2, nil
	case wit.S64:
		result := make([]int64, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*8), data)
		return result, 2, nil
	case wit.F64:
		result := make([]float64, length)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&result[0])), length*8), data)
		// Canonicalize NaN values per spec
		for i := range result {
			bits := math.Float64bits(result[i])
			result[i] = math.Float64frombits(abi.CanonicalizeF64(bits))
		}
		return result, 2, nil
	}
	return nil, 0, errors.Unsupported(errors.PhaseDecode, "64-bit list type")
}

func (d *Decoder) liftStringList(addr uint32, length uint32, mem Memory, path []string) (any, int, error) {
	result := make([]string, length)

	// Batch read all metadata (address + length pairs)
	metadataSize, ok := safeMulU32(length, 8)
	if !ok || metadataSize > MaxAlloc {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindOverflow).
			Path(path...).
			Detail("string list metadata overflow: %d * 8", length).
			Build()
	}
	metadata, err := mem.Read(addr, metadataSize)
	if err != nil {
		return nil, 0, err
	}
	if uint32(len(metadata)) != metadataSize {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("string list metadata short read: got %d, want %d", len(metadata), metadataSize).
			Build()
	}

	// Process each string
	for i := uint32(0); i < length; i++ {
		offset := i * 8
		strAddr := uint32(metadata[offset]) | uint32(metadata[offset+1])<<8 | uint32(metadata[offset+2])<<16 | uint32(metadata[offset+3])<<24
		strLen := uint32(metadata[offset+4]) | uint32(metadata[offset+5])<<8 | uint32(metadata[offset+6])<<16 | uint32(metadata[offset+7])<<24

		if strLen == 0 {
			result[i] = ""
			continue
		}

		data, err := mem.Read(strAddr, strLen)
		if err != nil {
			return nil, 0, err
		}

		if !utf8.Valid(data) {
			elemPath := append(append([]string{}, path...), "["+strconv.FormatUint(uint64(i), 10)+"]")
			return nil, 0, errors.InvalidUTF8(errors.PhaseDecode, elemPath, data)
		}

		result[i] = unsafe.String(unsafe.SliceData(data), len(data))
	}

	return result, 2, nil
}

func (d *Decoder) liftOption(o *wit.Option, flat []uint64, mem Memory, path []string) (any, int, error) {
	// Calculate total flat count (always 1 + inner type flat count)
	innerFlatCount := GetFlatCount(o.Type)
	totalFlat := 1 + innerFlatCount

	if len(flat) < totalFlat {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values for option: need %d, have %d", totalFlat, len(flat)).
			Build()
	}

	disc := flat[0]
	if disc == 0 {
		// None - return padded size
		return nil, totalFlat, nil
	}
	if disc != 1 {
		return nil, 0, errors.InvalidDiscriminant(errors.PhaseDecode, path, uint32(disc), 1)
	}

	// Some
	somePath := append(append([]string{}, path...), "[some]")
	val, _, err := d.liftValue(o.Type, flat[1:], mem, somePath)
	if err != nil {
		return nil, 0, err
	}
	// Return padded size (same as None case)
	return val, totalFlat, nil
}

func (d *Decoder) liftTuple(t *wit.Tuple, flat []uint64, mem Memory, path []string) ([]any, int, error) {
	result := make([]any, len(t.Types))
	offset := 0

	for i, elemType := range t.Types {
		elemPath := append(append([]string{}, path...), "["+strconv.Itoa(i)+"]")
		val, consumed, err := d.liftValue(elemType, flat[offset:], mem, elemPath)
		if err != nil {
			return nil, 0, err
		}
		result[i] = val
		offset += consumed
	}

	return result, offset, nil
}

func (d *Decoder) liftEnum(e *wit.Enum, flat []uint64, path []string) (uint32, int, error) {
	if len(flat) < 1 {
		return 0, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values for enum").
			Build()
	}
	disc := uint32(flat[0])
	if disc >= uint32(len(e.Cases)) {
		return 0, 0, errors.InvalidDiscriminant(errors.PhaseDecode, path, disc, uint32(len(e.Cases)-1))
	}
	return disc, 1, nil
}

func (d *Decoder) liftFlags(f *wit.Flags, flat []uint64, path []string) (uint64, int, error) {
	if len(flat) < 1 {
		return 0, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values for flags").
			Build()
	}
	return flat[0], 1, nil
}

func (d *Decoder) liftResult(r *wit.Result, flat []uint64, mem Memory, path []string) (map[string]any, int, error) {
	// Calculate max payload size for proper padding
	maxPayload := 0
	if r.OK != nil {
		okFlat := GetFlatCount(r.OK)
		if okFlat > maxPayload {
			maxPayload = okFlat
		}
	}
	if r.Err != nil {
		errFlat := GetFlatCount(r.Err)
		if errFlat > maxPayload {
			maxPayload = errFlat
		}
	}

	if len(flat) < 1+maxPayload {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values for result: need %d, have %d", 1+maxPayload, len(flat)).
			Build()
	}

	disc := flat[0]
	if disc > 1 {
		return nil, 0, errors.InvalidDiscriminant(errors.PhaseDecode, path, uint32(disc), 1)
	}
	result := make(map[string]any)

	if disc == 0 {
		// Ok
		if r.OK != nil {
			okPath := append(append([]string{}, path...), "[ok]")
			val, _, err := d.liftValue(r.OK, flat[1:], mem, okPath)
			if err != nil {
				return nil, 0, err
			}
			result["ok"] = val
		} else {
			result["ok"] = nil
		}
	} else {
		// Err
		if r.Err != nil {
			errPath := append(append([]string{}, path...), "[err]")
			val, _, err := d.liftValue(r.Err, flat[1:], mem, errPath)
			if err != nil {
				return nil, 0, err
			}
			result["err"] = val
		} else {
			result["err"] = nil
		}
	}

	// Return total consumed = 1 (discriminant) + maxPayload (padded)
	return result, 1 + maxPayload, nil
}

func (d *Decoder) liftVariant(v *wit.Variant, flat []uint64, mem Memory, path []string) (map[string]any, int, error) {
	// Calculate max payload size for proper padding
	maxPayload := 0
	for _, c := range v.Cases {
		if c.Type != nil {
			caseFlat := GetFlatCount(c.Type)
			if caseFlat > maxPayload {
				maxPayload = caseFlat
			}
		}
	}

	if len(flat) < 1+maxPayload {
		return nil, 0, errors.New(errors.PhaseDecode, errors.KindInvalidData).
			Path(path...).
			Detail("insufficient flat values for variant: need %d, have %d", 1+maxPayload, len(flat)).
			Build()
	}

	disc := flat[0]
	if disc >= uint64(len(v.Cases)) {
		return nil, 0, errors.InvalidDiscriminant(errors.PhaseDecode, path, uint32(disc), uint32(len(v.Cases)-1))
	}

	c := v.Cases[disc]
	result := make(map[string]any)

	if c.Type != nil {
		casePath := append(append([]string{}, path...), c.Name)
		val, _, err := d.liftValue(c.Type, flat[1:], mem, casePath)
		if err != nil {
			return nil, 0, err
		}
		result[c.Name] = val
	} else {
		result[c.Name] = nil
	}

	// Return total consumed = 1 (discriminant) + maxPayload (padded)
	return result, 1 + maxPayload, nil
}

func (d *Decoder) loadValue(witType wit.Type, addr uint32, mem Memory, path []string) (any, error) {
	switch t := witType.(type) {
	case wit.Bool:
		v, err := mem.ReadU8(addr)
		if err != nil {
			return nil, err
		}
		return v != 0, nil

	case wit.U8:
		return mem.ReadU8(addr)

	case wit.S8:
		v, err := mem.ReadU8(addr)
		if err != nil {
			return nil, err
		}
		return int8(v), nil

	case wit.U16:
		return mem.ReadU16(addr)

	case wit.S16:
		v, err := mem.ReadU16(addr)
		if err != nil {
			return nil, err
		}
		return int16(v), nil

	case wit.U32:
		return mem.ReadU32(addr)

	case wit.S32:
		v, err := mem.ReadU32(addr)
		if err != nil {
			return nil, err
		}
		return int32(v), nil

	case wit.U64:
		return mem.ReadU64(addr)

	case wit.S64:
		v, err := mem.ReadU64(addr)
		if err != nil {
			return nil, err
		}
		return int64(v), nil

	case wit.F32:
		bits, err := mem.ReadU32(addr)
		if err != nil {
			return nil, err
		}
		return math.Float32frombits(abi.CanonicalizeF32(bits)), nil

	case wit.F64:
		bits, err := mem.ReadU64(addr)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(abi.CanonicalizeF64(bits)), nil

	case wit.Char:
		v, err := mem.ReadU32(addr)
		if err != nil {
			return nil, err
		}
		r := rune(v)
		if !abi.ValidateChar(r) {
			return nil, errors.New(errors.PhaseDecode, errors.KindInvalidData).
				Path(path...).
				Detail("invalid Unicode scalar value: 0x%X", v).
				Build()
		}
		return r, nil

	case wit.String:
		return d.decodeStringFromMemory(addr, mem, path)

	case *wit.TypeDef:
		return d.loadTypeDef(t, addr, mem, path)

	default:
		return nil, errors.Unsupported(errors.PhaseDecode, "WIT type for load")
	}
}

func (d *Decoder) loadTypeDef(t *wit.TypeDef, addr uint32, mem Memory, path []string) (any, error) {
	switch kind := t.Kind.(type) {
	case *wit.Record:
		lc := d.compiler.layout
		recordLayout := lc.Calculate(t) // Use Calculate to leverage caching
		result := make(map[string]any, len(kind.Fields))
		for _, field := range kind.Fields {
			val, err := d.loadValue(field.Type, addr+recordLayout.FieldOffs[field.Name], mem, nil)
			if err != nil {
				// Only build path on error
				if path != nil {
					fieldPath := append(append([]string{}, path...), field.Name)
					return nil, errors.New(errors.PhaseDecode, errors.KindInvalidData).
						Path(fieldPath...).
						Detail("failed to load field %s: %v", field.Name, err).
						Build()
				}
				return nil, err
			}
			result[field.Name] = val
		}
		return result, nil

	case *wit.List:
		dataAddr, err := mem.ReadU32(addr)
		if err != nil {
			return nil, err
		}
		length, err := mem.ReadU32(addr + 4)
		if err != nil {
			return nil, err
		}

		if length == 0 {
			return []any{}, nil
		}

		if length > MaxListLength {
			return nil, errors.New(errors.PhaseDecode, errors.KindOverflow).
				Path(path...).
				Detail("list length %d exceeds maximum %d", length, MaxListLength).
				Build()
		}

		lc := d.compiler.layout
		elemLayout := lc.Calculate(kind.Type)

		// Dynamic path always returns []any for consistency
		result := make([]any, length)
		for i := uint32(0); i < length; i++ {
			val, err := d.loadValue(kind.Type, dataAddr+i*elemLayout.Size, mem, nil)
			if err != nil {
				// Only build path on error
				if path != nil {
					elemPath := append(append([]string{}, path...), "["+strconv.FormatUint(uint64(i), 10)+"]")
					return nil, errors.New(errors.PhaseDecode, errors.KindInvalidData).
						Path(elemPath...).
						Detail("failed to load element %d: %v", i, err).
						Build()
				}
				return nil, err
			}
			result[i] = val
		}
		return result, nil

	case *wit.Option:
		disc, err := mem.ReadU8(addr)
		if err != nil {
			return nil, err
		}
		if disc == 0 {
			return nil, nil
		}
		payloadOffset := alignTo(1, d.compiler.layout.Calculate(kind.Type).Align)
		return d.loadValue(kind.Type, addr+payloadOffset, mem, nil)

	case *wit.Variant:
		discSize := abi.DiscriminantSize(len(kind.Cases))
		var disc uint32
		switch discSize {
		case 1:
			d8, err := mem.ReadU8(addr)
			if err != nil {
				return nil, err
			}
			disc = uint32(d8)
		case 2:
			d16, err := mem.ReadU16(addr)
			if err != nil {
				return nil, err
			}
			disc = uint32(d16)
		case 4:
			d32, err := mem.ReadU32(addr)
			if err != nil {
				return nil, err
			}
			disc = d32
		}
		if disc >= uint32(len(kind.Cases)) {
			return nil, errors.InvalidDiscriminant(errors.PhaseDecode, path, disc, uint32(len(kind.Cases)-1))
		}
		c := kind.Cases[disc]
		result := make(map[string]any)
		if c.Type != nil {
			lc := d.compiler.layout
			variantLayout := lc.Calculate(t)
			payloadAddr := addr + alignTo(discSize, variantLayout.Align)
			val, err := d.loadValue(c.Type, payloadAddr, mem, nil)
			if err != nil {
				if path != nil {
					casePath := append(append([]string{}, path...), c.Name)
					return nil, errors.New(errors.PhaseDecode, errors.KindInvalidData).
						Path(casePath...).
						Detail("failed to load variant case %s: %v", c.Name, err).
						Build()
				}
				return nil, err
			}
			result[c.Name] = val
		} else {
			result[c.Name] = nil
		}
		return result, nil

	case *wit.Tuple:
		result := make([]any, len(kind.Types))
		lc := d.compiler.layout
		offset := uint32(0)
		for i, elemType := range kind.Types {
			elemLayout := lc.Calculate(elemType)
			offset = alignTo(offset, elemLayout.Align)
			val, err := d.loadValue(elemType, addr+offset, mem, nil)
			if err != nil {
				if path != nil {
					elemPath := append(append([]string{}, path...), "["+strconv.Itoa(i)+"]")
					return nil, errors.New(errors.PhaseDecode, errors.KindInvalidData).
						Path(elemPath...).
						Detail("failed to load tuple element %d: %v", i, err).
						Build()
				}
				return nil, err
			}
			result[i] = val
			offset += elemLayout.Size
		}
		return result, nil

	case *wit.Result:
		disc, err := mem.ReadU8(addr)
		if err != nil {
			return nil, err
		}
		result := make(map[string]any)
		lc := d.compiler.layout
		resultLayout := lc.Calculate(t)
		payloadAddr := addr + alignTo(1, resultLayout.Align)
		if disc == 0 {
			if kind.OK != nil {
				val, err := d.loadValue(kind.OK, payloadAddr, mem, nil)
				if err != nil {
					if path != nil {
						okPath := append(append([]string{}, path...), "[ok]")
						return nil, errors.New(errors.PhaseDecode, errors.KindInvalidData).
							Path(okPath...).
							Detail("failed to load ok value: %v", err).
							Build()
					}
					return nil, err
				}
				result["ok"] = val
			} else {
				result["ok"] = nil
			}
		} else {
			if kind.Err != nil {
				val, err := d.loadValue(kind.Err, payloadAddr, mem, nil)
				if err != nil {
					if path != nil {
						errPath := append(append([]string{}, path...), "[err]")
						return nil, errors.New(errors.PhaseDecode, errors.KindInvalidData).
							Path(errPath...).
							Detail("failed to load err value: %v", err).
							Build()
					}
					return nil, err
				}
				result["err"] = val
			} else {
				result["err"] = nil
			}
		}
		return result, nil

	case wit.Type:
		return d.loadValue(kind, addr, mem, path)

	default:
		return nil, errors.Unsupported(errors.PhaseDecode, "TypeDef kind for load")
	}
}
