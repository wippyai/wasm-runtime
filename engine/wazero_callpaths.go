package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"unsafe"

	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/transcoder"
)

// Call path implementations for WazeroInstance.
// Contains fast paths, compiled paths, and general transcoder-based calling.

func bindingMemoryInterface(b *exportBinding) transcoder.Memory {
	if b != nil && b.memory != nil && b.memory.mem != nil {
		return b.memory
	}
	return nil
}

func bindingAllocatorInterface(b *exportBinding) transcoder.Allocator {
	if b != nil && b.alloc != nil && b.alloc.allocFn != nil {
		return b.alloc
	}
	return nil
}

// tryCallStringInto handles (string) -> string, copying returned guest bytes.
func (i *WazeroInstance) tryCallStringInto(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) (bool, error) {
	// Check signature: (string) -> string
	if len(paramTypes) != 1 || len(resultTypes) != 1 {
		return false, nil
	}
	if _, ok := paramTypes[0].(wit.String); !ok {
		return false, nil
	}
	if _, ok := resultTypes[0].(wit.String); !ok {
		return false, nil
	}
	if len(params) != 1 {
		return false, nil
	}

	resultPtr, ok := result.(*string)
	if !ok {
		return false, nil
	}

	input, ok := params[0].(string)
	if !ok {
		return false, nil
	}

	alloc := b.alloc
	memObj := b.memory
	if alloc == nil || alloc.allocFn == nil || memObj == nil || memObj.mem == nil {
		return true, fmt.Errorf("canonical memory or allocator not available for export %q", b.name)
	}

	mem := memObj.mem

	// Track allocations for cleanup
	allocList := transcoder.NewAllocationList()
	defer allocList.Release()
	if !b.isCanonical() {
		defer allocList.Free(alloc)
	}

	// Allocate and write input string
	var inputPtr uint32
	inputLen := uint32(len(input))
	if inputLen > 0 {
		i.stackBuf[0] = 0
		i.stackBuf[1] = 0
		i.stackBuf[2] = 1 // align
		i.stackBuf[3] = uint64(inputLen)
		if err := alloc.allocFn.CallWithStack(ctx, i.stackBuf[:4]); err != nil {
			return true, err
		}
		inputPtr = uint32(i.stackBuf[0])
		allocList.Add(inputPtr, inputLen, 1)
		if !mem.WriteString(inputPtr, input) {
			if b.isCanonical() {
				allocList.Free(alloc)
			}
			return true, fmt.Errorf("write input string to memory at 0x%x: out of bounds", inputPtr)
		}
	}

	// Call: (ptr, len) -> retptr
	i.stackBuf[0] = uint64(inputPtr)
	i.stackBuf[1] = uint64(inputLen)
	if err := b.fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
		return true, err
	}

	// Decode result directly into caller's pointer
	retptr := uint32(i.stackBuf[0])
	resultDataPtr, ok := mem.ReadUint32Le(retptr)
	if !ok {
		return true, fmt.Errorf("read result pointer at 0x%x: out of bounds", retptr)
	}
	resultDataLen, ok := mem.ReadUint32Le(retptr + 4)
	if !ok {
		return true, fmt.Errorf("read result length at 0x%x: out of bounds", retptr+4)
	}

	if resultDataLen == 0 {
		*resultPtr = ""
	} else {
		resultData, ok := mem.Read(resultDataPtr, resultDataLen)
		if !ok {
			return true, fmt.Errorf("read result data at 0x%x (len %d): out of bounds", resultDataPtr, resultDataLen)
		}
		// Go strings must outlive subsequent guest calls, even without post-return.
		*resultPtr = string(resultData)
	}

	if b.postReturn != nil {
		callCtx := i.prepareCallContext(ctx)
		if _, err := b.postReturn.Call(callCtx, uint64(retptr)); err != nil {
			return true, fmt.Errorf("post-return: %w", err)
		}
	}

	return true, nil
}

// tryCallPrimitiveInto handles primitive signatures with zero allocations
func (i *WazeroInstance) tryCallPrimitiveInto(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) (bool, error) {
	// Only handle single u32 result for now
	if len(resultTypes) != 1 {
		return false, nil
	}

	// Check result type matches
	if _, ok := resultTypes[0].(wit.U32); ok {
		resultPtr, ok := result.(*uint32)
		if !ok {
			return false, nil
		}

		// Handle (u32, u32) -> u32
		if len(paramTypes) == 2 {
			if _, ok := paramTypes[0].(wit.U32); !ok {
				return false, nil
			}
			if _, ok := paramTypes[1].(wit.U32); !ok {
				return false, nil
			}
			if len(params) != 2 {
				return false, nil
			}

			a, ok1 := params[0].(uint32)
			bParam, ok2 := params[1].(uint32)
			if !ok1 || !ok2 {
				return false, nil
			}

			i.stackBuf[0] = uint64(a)
			i.stackBuf[1] = uint64(bParam)
			if err := b.fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
				return true, err
			}
			*resultPtr = uint32(i.stackBuf[0])
			if b.postReturn != nil {
				callCtx := i.prepareCallContext(ctx)
				if _, err := b.postReturn.Call(callCtx, i.stackBuf[0]); err != nil {
					return true, fmt.Errorf("post-return: %w", err)
				}
			}
			return true, nil
		}

		// Handle (u32) -> u32
		if len(paramTypes) == 1 {
			if _, ok := paramTypes[0].(wit.U32); !ok {
				return false, nil
			}
			if len(params) != 1 {
				return false, nil
			}

			a, ok := params[0].(uint32)
			if !ok {
				return false, nil
			}

			i.stackBuf[0] = uint64(a)
			if err := b.fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
				return true, err
			}
			*resultPtr = uint32(i.stackBuf[0])
			if b.postReturn != nil {
				callCtx := i.prepareCallContext(ctx)
				if _, err := b.postReturn.Call(callCtx, i.stackBuf[0]); err != nil {
					return true, fmt.Errorf("post-return: %w", err)
				}
			}
			return true, nil
		}

		// Handle () -> u32
		if len(paramTypes) == 0 {
			if err := b.fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
				return true, err
			}
			*resultPtr = uint32(i.stackBuf[0])
			if b.postReturn != nil {
				callCtx := i.prepareCallContext(ctx)
				if _, err := b.postReturn.Call(callCtx, i.stackBuf[0]); err != nil {
					return true, fmt.Errorf("post-return: %w", err)
				}
			}
			return true, nil
		}
	}

	return false, nil
}

// tryCallCompiled handles typed calls using compiled transcoder (allocates result, returns it)
// Supports records (structs) and lists (typed slices)
func (i *WazeroInstance) tryCallCompiled(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, bool, error) {
	// Check signature: single param -> single result
	if len(paramTypes) != 1 || len(resultTypes) != 1 || len(params) != 1 {
		return nil, false, nil
	}

	// Both must be TypeDef
	paramTypeDef, paramOk := paramTypes[0].(*wit.TypeDef)
	resultTypeDef, resultOk := resultTypes[0].(*wit.TypeDef)
	if !paramOk || !resultOk {
		return nil, false, nil
	}

	// Try to compile param type
	paramVal := reflect.ValueOf(params[0])
	paramCompiled, err := i.compiler.Compile(paramTypeDef, paramVal.Type())
	if err != nil {
		return nil, false, nil
	}

	// Try to compile result type - allocate result based on WIT type
	var resultGo reflect.Value
	switch resultTypeDef.Kind.(type) {
	case *wit.Record:
		resultGo = reflect.New(paramVal.Type()).Elem()
	case *wit.List:
		elemType := paramVal.Type().Elem()
		resultGo = reflect.MakeSlice(reflect.SliceOf(elemType), 0, 0)
	default:
		return nil, false, nil
	}

	resultCompiled, err := i.compiler.Compile(resultTypeDef, resultGo.Type())
	if err != nil {
		return nil, false, nil
	}

	alloc := b.alloc
	mem := b.memory
	if (alloc == nil || alloc.allocFn == nil) && paramsRequireAlloc(paramTypes) {
		return nil, true, fmt.Errorf("canonical allocator not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && paramsRequireMemory(paramTypes) {
		return nil, true, fmt.Errorf("canonical memory not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && resultsRequireMemory(resultTypes) {
		return nil, true, fmt.Errorf("canonical memory not available for export %q", b.name)
	}

	memInterface := bindingMemoryInterface(b)
	allocInterface := bindingAllocatorInterface(b)
	if alloc != nil {
		alloc.setContext(ctx)
	}

	allocList := transcoder.NewAllocationList()
	defer allocList.Release()
	if !b.isCanonical() && allocInterface != nil {
		defer allocList.Free(allocInterface)
	}

	// Lower param to stack - get pointer to param data
	paramInterface := (*[2]unsafe.Pointer)(unsafe.Pointer(&params[0]))
	paramPtr := paramInterface[1]

	stackSize, err := i.encoder.LowerToStackTracked(paramCompiled, paramPtr, i.stackBuf, memInterface, allocInterface, allocList)
	if err != nil {
		if b.isCanonical() && allocInterface != nil {
			allocList.Free(allocInterface)
		}
		return nil, true, err
	}

	// Call WASM function
	if err := b.fn.CallWithStack(ctx, i.stackBuf[:stackSize]); err != nil {
		return nil, true, err
	}

	// Lift result from stack - need pointer to the value
	resultPtrVal := reflect.New(resultGo.Type())
	resultPtrVal.Elem().Set(resultGo)
	resultPtr := resultPtrVal.UnsafePointer()

	_, err = i.decoder.LiftFromStack(resultCompiled, i.stackBuf, resultPtr, memInterface)
	if err != nil {
		return nil, true, err
	}

	if b.postReturn != nil {
		callCtx := i.prepareCallContext(ctx)
		rawResults := []uint64{i.stackBuf[0]}
		if _, err := b.postReturn.Call(callCtx, rawResults...); err != nil {
			return nil, true, fmt.Errorf("post-return: %w", err)
		}
	}

	return resultPtrVal.Elem().Interface(), true, nil
}

// tryCallCompiledInto handles typed calls using compiled transcoder (zero-alloc fast path)
// Supports records (structs) and lists (typed slices)
func (i *WazeroInstance) tryCallCompiledInto(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) (bool, error) {
	// Check signature: single param -> single result
	if len(paramTypes) != 1 || len(resultTypes) != 1 || len(params) != 1 {
		return false, nil
	}

	// Both must be TypeDef
	paramTypeDef, paramOk := paramTypes[0].(*wit.TypeDef)
	resultTypeDef, resultOk := resultTypes[0].(*wit.TypeDef)
	if !paramOk || !resultOk {
		return false, nil
	}

	// Result must be a pointer
	rv := reflect.ValueOf(result)
	if rv.Kind() != reflect.Pointer {
		return false, nil
	}

	// Try to compile both types - if compilation succeeds, we can use fast path
	paramVal := reflect.ValueOf(params[0])
	paramCompiled, err := i.compiler.Compile(paramTypeDef, paramVal.Type())
	if err != nil {
		return false, nil
	}

	resultCompiled, err := i.compiler.Compile(resultTypeDef, rv.Elem().Type())
	if err != nil {
		return false, nil
	}

	alloc := b.alloc
	mem := b.memory
	if (alloc == nil || alloc.allocFn == nil) && paramsRequireAlloc(paramTypes) {
		return true, fmt.Errorf("canonical allocator not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && paramsRequireMemory(paramTypes) {
		return true, fmt.Errorf("canonical memory not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && resultsRequireMemory(resultTypes) {
		return true, fmt.Errorf("canonical memory not available for export %q", b.name)
	}

	memInterface := bindingMemoryInterface(b)
	allocInterface := bindingAllocatorInterface(b)
	if alloc != nil {
		alloc.setContext(ctx)
	}

	allocList := transcoder.NewAllocationList()
	defer allocList.Release()
	if !b.isCanonical() && allocInterface != nil {
		defer allocList.Free(allocInterface)
	}

	// Lower param to stack - get pointer to param data
	paramInterface := (*[2]unsafe.Pointer)(unsafe.Pointer(&params[0]))
	paramPtr := paramInterface[1]

	stackSize, err := i.encoder.LowerToStackTracked(paramCompiled, paramPtr, i.stackBuf, memInterface, allocInterface, allocList)
	if err != nil {
		if b.isCanonical() && allocInterface != nil {
			allocList.Free(allocInterface)
		}
		return true, err
	}

	// Call WASM function
	if err := b.fn.CallWithStack(ctx, i.stackBuf[:stackSize]); err != nil {
		return true, err
	}

	// Check if result uses retptr (indirect return)
	usesRetptr := usesRetptr(resultTypes)

	// Lift result from flat values
	resultPtr := unsafe.Pointer(rv.Pointer())

	if usesRetptr {
		// Result is returned via pointer - read actual result data from memory
		retptr := uint32(i.stackBuf[0])
		resultSize := resultSize(resultTypes[0])

		if mem == nil || mem.mem == nil {
			return true, fmt.Errorf("read retptr result: memory not available")
		}
		resultData, err := mem.Read(retptr, resultSize)
		if err != nil {
			return true, fmt.Errorf("read retptr result: %w", err)
		}

		// Convert bytes to uint64 flat values directly into stackBuf (reuse allocation)
		flatCount := resultSize / 4
		if uint32(len(resultData)) < flatCount*4 {
			return true, fmt.Errorf("malformed result data: expected %d bytes, got %d", flatCount*4, len(resultData))
		}
		for j := uint32(0); j < flatCount; j++ {
			offset := j * 4
			i.stackBuf[j] = uint64(binary.LittleEndian.Uint32(resultData[offset:]))
		}
		_, err = i.decoder.LiftFromStack(resultCompiled, i.stackBuf[:flatCount], resultPtr, memInterface)
		if err != nil {
			return true, err
		}

		if b.postReturn != nil {
			callCtx := i.prepareCallContext(ctx)
			if _, err := b.postReturn.Call(callCtx, uint64(retptr)); err != nil {
				return true, fmt.Errorf("post-return: %w", err)
			}
		}
		return true, nil
	}

	// Result is returned directly on stack
	rawRet := i.stackBuf[0]
	_, err = i.decoder.LiftFromStack(resultCompiled, i.stackBuf, resultPtr, memInterface)
	if err != nil {
		return true, err
	}

	if b.postReturn != nil {
		callCtx := i.prepareCallContext(ctx)
		if _, err := b.postReturn.Call(callCtx, rawRet); err != nil {
			return true, fmt.Errorf("post-return: %w", err)
		}
	}

	return true, nil
}

// callGeneralInto is the general path using transcoder with DecodeInto
func (i *WazeroInstance) callGeneralInto(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) error {
	alloc := b.alloc
	mem := b.memory
	if (alloc == nil || alloc.allocFn == nil) && paramsRequireAlloc(paramTypes) {
		return fmt.Errorf("canonical allocator not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && paramsRequireMemory(paramTypes) {
		return fmt.Errorf("canonical memory not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && resultsRequireMemory(resultTypes) {
		return fmt.Errorf("canonical memory not available for export %q", b.name)
	}

	memInterface := bindingMemoryInterface(b)
	allocInterface := bindingAllocatorInterface(b)
	if alloc != nil {
		alloc.setContext(ctx)
	}

	allocList := transcoder.NewAllocationList()
	defer allocList.Release()
	if !b.isCanonical() && allocInterface != nil {
		defer allocList.Free(allocInterface)
	}

	// Encode parameters - encoder internally uses compiled fast path when possible
	flatParams, err := i.encoder.EncodeParams(paramTypes, params, memInterface, allocInterface, allocList)
	if err != nil {
		if b.isCanonical() && allocInterface != nil {
			allocList.Free(allocInterface)
		}
		return fmt.Errorf("encode params: %w", err)
	}

	// Call WASM function
	copy(i.stackBuf, flatParams)
	if err := b.fn.CallWithStack(ctx, i.stackBuf[:len(flatParams)]); err != nil {
		return fmt.Errorf("wasm call failed: %w", err)
	}

	// Handle void return
	if result == nil || len(resultTypes) == 0 {
		if b.postReturn != nil {
			callCtx := i.prepareCallContext(ctx)
			if _, err := b.postReturn.Call(callCtx); err != nil {
				return fmt.Errorf("post-return: %w", err)
			}
		}
		return nil
	}

	// Check if result uses retptr (indirect return)
	usesRetptr := usesRetptr(resultTypes)
	var retptr uint32

	// If retptr, read actual result from memory into stackBuf
	if usesRetptr {
		retptr = uint32(i.stackBuf[0])
		resultSize := resultSize(resultTypes[0])

		if mem == nil || mem.mem == nil {
			return fmt.Errorf("read retptr result: memory not available")
		}
		resultData, err := mem.Read(retptr, resultSize)
		if err != nil {
			return fmt.Errorf("read retptr result: %w", err)
		}

		// Convert bytes to uint64 flat values in stackBuf
		flatCount := resultSize / 4
		if uint32(len(resultData)) < flatCount*4 {
			return fmt.Errorf("malformed result data: expected %d bytes, got %d", flatCount*4, len(resultData))
		}
		for j := uint32(0); j < flatCount; j++ {
			i.stackBuf[j] = uint64(binary.LittleEndian.Uint32(resultData[j*4:]))
		}
	}

	// Lift result using compiled type if possible
	if len(resultTypes) == 1 {
		if typeDef, ok := resultTypes[0].(*wit.TypeDef); ok {
			rv := reflect.ValueOf(result)
			if rv.Kind() == reflect.Pointer {
				// Try to compile the result type with the Go type
				goType := rv.Elem().Type()
				compiled, err := i.compiler.Compile(typeDef, goType)
				if err == nil {
					// Use compiled fast path for records, lists, etc.
					resultPtr := unsafe.Pointer(rv.Pointer())
					_, err = i.decoder.LiftFromStack(compiled, i.stackBuf, resultPtr, memInterface)
					if err != nil {
						return err
					}
					if b.postReturn != nil {
						callCtx := i.prepareCallContext(ctx)
						var raw []uint64
						if usesRetptr {
							raw = []uint64{uint64(retptr)}
						} else {
							raw = []uint64{i.stackBuf[0]}
						}
						if _, err := b.postReturn.Call(callCtx, raw...); err != nil {
							return fmt.Errorf("post-return: %w", err)
						}
					}
					return nil
				}
			}
		}
	}

	// Fall back to DecodeInto for other types - results are in stackBuf
	if err := i.decoder.DecodeInto(resultTypes, i.stackBuf, memInterface, result); err != nil {
		return err
	}

	if b.postReturn != nil {
		callCtx := i.prepareCallContext(ctx)
		var raw []uint64
		if usesRetptr {
			raw = []uint64{uint64(retptr)}
		} else {
			count := flatResultCount(resultTypes)
			raw = make([]uint64, count)
			copy(raw, i.stackBuf[:count])
		}
		if _, err := b.postReturn.Call(callCtx, raw...); err != nil {
			return fmt.Errorf("post-return: %w", err)
		}
	}

	return nil
}

// tryFastCall attempts direct call for primitive signatures
func (i *WazeroInstance) tryFastCall(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, bool, error) {
	// Try string fast path first
	if result, ok, err := i.tryFastStringCall(ctx, b, paramTypes, resultTypes, params); ok {
		return result, ok, err
	}

	// Handle single result primitives
	if len(resultTypes) != 1 {
		return nil, false, nil
	}

	// Determine result type and converter
	var convertResult func(uint64) any
	switch resultTypes[0].(type) {
	case wit.U32:
		convertResult = func(v uint64) any { return uint32(v) }
	case wit.S32:
		convertResult = func(v uint64) any { return int32(v) }
	case wit.U64:
		convertResult = func(v uint64) any { return v }
	case wit.S64:
		convertResult = func(v uint64) any { return int64(v) }
	case wit.Bool:
		convertResult = func(v uint64) any { return v != 0 }
	default:
		return nil, false, nil
	}

	// Handle (T, T) -> R for 32-bit types
	if len(paramTypes) == 2 && len(params) == 2 {
		var a, bVal uint64
		switch p := paramTypes[0].(type) {
		case wit.U32:
			if v, ok := params[0].(uint32); ok {
				a = uint64(v)
			} else {
				return nil, false, nil
			}
		case wit.S32:
			if v, ok := params[0].(int32); ok {
				a = uint64(uint32(v))
			} else {
				return nil, false, nil
			}
		default:
			_ = p
			return nil, false, nil
		}
		switch p := paramTypes[1].(type) {
		case wit.U32:
			if v, ok := params[1].(uint32); ok {
				bVal = uint64(v)
			} else {
				return nil, false, nil
			}
		case wit.S32:
			if v, ok := params[1].(int32); ok {
				bVal = uint64(uint32(v))
			} else {
				return nil, false, nil
			}
		default:
			_ = p
			return nil, false, nil
		}

		i.stackBuf[0] = a
		i.stackBuf[1] = bVal
		if err := b.fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
			return nil, true, fmt.Errorf("wasm call failed: %w", err)
		}
		rawRet := i.stackBuf[0]
		if b.postReturn != nil {
			callCtx := i.prepareCallContext(ctx)
			if _, err := b.postReturn.Call(callCtx, rawRet); err != nil {
				return nil, true, fmt.Errorf("post-return: %w", err)
			}
		}
		return convertResult(rawRet), true, nil
	}

	// Handle (T) -> R for 32/64-bit types
	if len(paramTypes) == 1 && len(params) == 1 {
		var a uint64
		switch p := paramTypes[0].(type) {
		case wit.U32:
			if v, ok := params[0].(uint32); ok {
				a = uint64(v)
			} else {
				return nil, false, nil
			}
		case wit.S32:
			if v, ok := params[0].(int32); ok {
				a = uint64(uint32(v))
			} else {
				return nil, false, nil
			}
		case wit.U64:
			if v, ok := params[0].(uint64); ok {
				a = v
			} else {
				return nil, false, nil
			}
		case wit.S64:
			if v, ok := params[0].(int64); ok {
				a = uint64(v)
			} else {
				return nil, false, nil
			}
		default:
			_ = p
			return nil, false, nil
		}

		i.stackBuf[0] = a
		if err := b.fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
			return nil, true, fmt.Errorf("wasm call failed: %w", err)
		}
		rawRet := i.stackBuf[0]
		if b.postReturn != nil {
			callCtx := i.prepareCallContext(ctx)
			if _, err := b.postReturn.Call(callCtx, rawRet); err != nil {
				return nil, true, fmt.Errorf("post-return: %w", err)
			}
		}
		return convertResult(rawRet), true, nil
	}

	// Handle () -> R
	if len(paramTypes) == 0 {
		if err := b.fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
			return nil, true, fmt.Errorf("wasm call failed: %w", err)
		}
		rawRet := i.stackBuf[0]
		if b.postReturn != nil {
			callCtx := i.prepareCallContext(ctx)
			if _, err := b.postReturn.Call(callCtx, rawRet); err != nil {
				return nil, true, fmt.Errorf("post-return: %w", err)
			}
		}
		return convertResult(rawRet), true, nil
	}

	return nil, false, nil
}

// tryFastStringCall handles string parameter/result signatures
func (i *WazeroInstance) tryFastStringCall(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, bool, error) {
	// Handle (string) -> string (e.g., echo, process)
	// Canonical ABI: function takes (ptr, len) and returns retptr to (resultPtr, resultLen)
	if len(paramTypes) == 1 && len(resultTypes) == 1 {
		if _, ok := paramTypes[0].(wit.String); !ok {
			return nil, false, nil
		}
		if _, ok := resultTypes[0].(wit.String); !ok {
			return nil, false, nil
		}
		if len(params) != 1 {
			return nil, false, nil
		}

		s, ok := params[0].(string)
		if !ok {
			return nil, false, nil
		}

		alloc := b.alloc
		memObj := b.memory
		if alloc == nil || alloc.allocFn == nil || memObj == nil || memObj.mem == nil {
			return nil, true, fmt.Errorf("canonical memory or allocator not available for export %q", b.name)
		}

		// Track allocations for cleanup
		allocList := transcoder.NewAllocationList()
		defer allocList.Release()
		if !b.isCanonical() {
			defer allocList.Free(alloc)
		}

		mem := memObj.mem

		// Allocate and write input string
		var inputPtr uint32
		inputLen := uint32(len(s))
		if inputLen > 0 {
			i.stackBuf[0] = 0
			i.stackBuf[1] = 0
			i.stackBuf[2] = 1 // align
			i.stackBuf[3] = uint64(inputLen)
			if err := alloc.allocFn.CallWithStack(ctx, i.stackBuf[:4]); err != nil {
				return nil, true, err
			}
			inputPtr = uint32(i.stackBuf[0])
			allocList.Add(inputPtr, inputLen, 1)
			if !mem.WriteString(inputPtr, s) {
				if b.isCanonical() {
					allocList.Free(alloc)
				}
				return nil, true, fmt.Errorf("write input string to memory at 0x%x: out of bounds", inputPtr)
			}
		}

		// Call: (ptr, len) -> retptr
		i.stackBuf[0] = uint64(inputPtr)
		i.stackBuf[1] = uint64(inputLen)
		if err := b.fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
			return nil, true, fmt.Errorf("wasm call failed: %w", err)
		}

		// Function returns retptr in stackBuf[0]
		retptr := uint32(i.stackBuf[0])

		// Read result from retptr
		resultPtr, ok := mem.ReadUint32Le(retptr)
		if !ok {
			return nil, true, fmt.Errorf("read result pointer at 0x%x: out of bounds", retptr)
		}
		resultLen, ok := mem.ReadUint32Le(retptr + 4)
		if !ok {
			return nil, true, fmt.Errorf("read result length at 0x%x: out of bounds", retptr+4)
		}
		if resultLen == 0 {
			if b.postReturn != nil {
				callCtx := i.prepareCallContext(ctx)
				if _, err := b.postReturn.Call(callCtx, uint64(retptr)); err != nil {
					return nil, true, fmt.Errorf("post-return: %w", err)
				}
			}
			return "", true, nil
		}
		resultData, ok := mem.Read(resultPtr, resultLen)
		if !ok {
			return nil, true, fmt.Errorf("read result data at 0x%x (len %d): out of bounds", resultPtr, resultLen)
		}

		// The guest may reuse this memory on its next call.
		res := string(resultData)
		if b.postReturn != nil {
			callCtx := i.prepareCallContext(ctx)
			if _, err := b.postReturn.Call(callCtx, uint64(retptr)); err != nil {
				return nil, true, fmt.Errorf("post-return: %w", err)
			}
		}
		return res, true, nil
	}

	return nil, false, nil
}

// callGeneral is the general-purpose call path using transcoder
func (i *WazeroInstance) callGeneral(ctx context.Context, b *exportBinding, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, error) {
	alloc := b.alloc
	mem := b.memory
	if (alloc == nil || alloc.allocFn == nil) && paramsRequireAlloc(paramTypes) {
		return nil, fmt.Errorf("canonical allocator not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && paramsRequireMemory(paramTypes) {
		return nil, fmt.Errorf("canonical memory not available for export %q", b.name)
	}
	if (mem == nil || mem.mem == nil) && resultsRequireMemory(resultTypes) {
		return nil, fmt.Errorf("canonical memory not available for export %q", b.name)
	}

	memInterface := bindingMemoryInterface(b)
	allocInterface := bindingAllocatorInterface(b)
	if alloc != nil {
		alloc.setContext(ctx)
	}

	allocList := transcoder.NewAllocationList()
	defer allocList.Release()
	if !b.isCanonical() && allocInterface != nil {
		defer allocList.Free(allocInterface)
	}

	// Encode parameters - encoder internally uses compiled fast path when possible
	flatParams, err := i.encoder.EncodeParams(paramTypes, params, memInterface, allocInterface, allocList)
	if err != nil {
		if b.isCanonical() && allocInterface != nil {
			allocList.Free(allocInterface)
		}
		return nil, fmt.Errorf("encode params: %w", err)
	}

	// Call WASM function - stack needs space for max(params, results)
	// For lifted exports: callee allocates return buffer and returns pointer
	copy(i.stackBuf, flatParams)
	stackSize := len(flatParams)

	// Check if results use indirect return (callee returns pointer to result struct)
	usesIndirectReturn := usesRetptr(resultTypes)

	// For direct returns, ensure stack has space for results
	if len(resultTypes) > 0 && !usesIndirectReturn {
		resultSlots := flatResultCount(resultTypes)
		if resultSlots > stackSize {
			stackSize = resultSlots
		}
	}

	// For indirect returns, function returns 1 value (pointer)
	if usesIndirectReturn && stackSize < 1 {
		stackSize = 1
	}

	if err := b.fn.CallWithStack(ctx, i.stackBuf[:stackSize]); err != nil {
		return nil, fmt.Errorf("wasm call failed: %w", err)
	}

	var goResults []any
	var rawResults []uint64
	if usesIndirectReturn {
		// Callee allocated return buffer and returned pointer to it in stackBuf[0]
		retptr := uint32(i.stackBuf[0])
		rawResults = []uint64{uint64(retptr)}
		if mem == nil || mem.mem == nil {
			return nil, fmt.Errorf("decode results: memory not available")
		}
		if len(resultTypes) == 1 {
			if _, isString := resultTypes[0].(wit.String); isString {
				// Fast path for string results
				ptr, err := mem.ReadU32(retptr)
				if err != nil {
					return nil, fmt.Errorf("read string pointer at 0x%x: %w", retptr, err)
				}
				length, err := mem.ReadU32(retptr + 4)
				if err != nil {
					return nil, fmt.Errorf("read string length at 0x%x: %w", retptr+4, err)
				}
				if length > 0 {
					data, err := mem.Read(ptr, length)
					if err != nil {
						return nil, fmt.Errorf("read string data at 0x%x (len %d): %w", ptr, length, err)
					}
					goResults = []any{string(data)}
				} else {
					goResults = []any{""}
				}
			} else {
				// Load value directly from memory at retptr address
				val, err := i.decoder.LoadValue(resultTypes[0], retptr, memInterface)
				if err != nil {
					return nil, fmt.Errorf("load indirect result: %w", err)
				}
				goResults = []any{val}
			}
		} else {
			// Multiple results via retptr - load each from memory
			goResults = make([]any, len(resultTypes))
			offset := uint32(0)
			for idx, rt := range resultTypes {
				val, err := i.decoder.LoadValue(rt, retptr+offset, memInterface)
				if err != nil {
					return nil, fmt.Errorf("load indirect result[%d]: %w", idx, err)
				}
				goResults[idx] = val
				offset += resultSize(rt)
			}
		}
	} else if len(resultTypes) > 0 {
		// Decode results from flat return values in stackBuf
		count := flatResultCount(resultTypes)
		rawResults = make([]uint64, count)
		copy(rawResults, i.stackBuf[:count])
		var err error
		goResults, err = i.decoder.DecodeResults(resultTypes, i.stackBuf, memInterface)
		if err != nil {
			return nil, fmt.Errorf("decode results: %w", err)
		}
	}

	// Post-return cleanup
	if b.postReturn != nil {
		callCtx := i.prepareCallContext(ctx)
		if _, err := b.postReturn.Call(callCtx, rawResults...); err != nil {
			return nil, fmt.Errorf("post-return: %w", err)
		}
	}

	// Return single value if only one result, otherwise return slice
	if len(goResults) == 1 {
		return goResults[0], nil
	}
	return goResults, nil
}
