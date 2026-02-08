package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/tetratelabs/wazero/api"
	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/transcoder"
)

// Call path implementations for WazeroInstance.
// Contains fast paths, compiled paths, and general transcoder-based calling.

// tryCallStringInto handles (string) -> string with zero allocations
func (i *WazeroInstance) tryCallStringInto(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) (bool, error) {
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

	if i.allocFn == nil || i.memory == nil {
		return false, fmt.Errorf("no allocator or memory available")
	}

	mem := i.memory.mem

	// Allocate and write input string
	var inputPtr uint32
	inputLen := uint32(len(input))
	if inputLen > 0 {
		i.stackBuf[0] = 0
		i.stackBuf[1] = 0
		i.stackBuf[2] = 1 // align
		i.stackBuf[3] = uint64(inputLen)
		if err := i.allocFn.CallWithStack(ctx, i.stackBuf[:4]); err != nil {
			return true, err
		}
		inputPtr = uint32(i.stackBuf[0])
		if !mem.WriteString(inputPtr, input) {
			return true, fmt.Errorf("write input string to memory at 0x%x: out of bounds", inputPtr)
		}
	}

	// Call: (ptr, len) -> retptr
	i.stackBuf[0] = uint64(inputPtr)
	i.stackBuf[1] = uint64(inputLen)
	if err := fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
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
		*resultPtr = unsafe.String(unsafe.SliceData(resultData), len(resultData))
	}

	return true, nil
}

// tryCallPrimitiveInto handles primitive signatures with zero allocations
func (i *WazeroInstance) tryCallPrimitiveInto(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) (bool, error) {
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
			b, ok2 := params[1].(uint32)
			if !ok1 || !ok2 {
				return false, nil
			}

			i.stackBuf[0] = uint64(a)
			i.stackBuf[1] = uint64(b)
			if err := fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
				return true, err
			}
			*resultPtr = uint32(i.stackBuf[0])
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
			if err := fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
				return true, err
			}
			*resultPtr = uint32(i.stackBuf[0])
			return true, nil
		}

		// Handle () -> u32
		if len(paramTypes) == 0 {
			if err := fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
				return true, err
			}
			*resultPtr = uint32(i.stackBuf[0])
			return true, nil
		}
	}

	return false, nil
}

// tryCallCompiled handles typed calls using compiled transcoder (allocates result, returns it)
// Supports records (structs) and lists (typed slices)
func (i *WazeroInstance) tryCallCompiled(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, bool, error) {
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

	// Lower param to stack - get pointer to param data
	// Go interface layout: [type ptr, data ptr]. For non-pointer types larger than
	// a word, data ptr points to the actual data. This is faster than reflect.
	paramInterface := (*[2]unsafe.Pointer)(unsafe.Pointer(&params[0]))
	paramPtr := paramInterface[1]

	i.alloc.setContext(ctx)

	stackSize, err := i.encoder.LowerToStack(paramCompiled, paramPtr, i.stackBuf, i.memory, i.alloc)
	if err != nil {
		return nil, true, err
	}

	// Call WASM function
	if err := fn.CallWithStack(ctx, i.stackBuf[:stackSize]); err != nil {
		return nil, true, err
	}

	// Lift result from stack - need pointer to the value
	resultPtrVal := reflect.New(resultGo.Type())
	resultPtrVal.Elem().Set(resultGo)
	resultPtr := resultPtrVal.UnsafePointer()

	_, err = i.decoder.LiftFromStack(resultCompiled, i.stackBuf, resultPtr, i.memory)
	if err != nil {
		return nil, true, err
	}

	return resultPtrVal.Elem().Interface(), true, nil
}

// tryCallCompiledInto handles typed calls using compiled transcoder (zero-alloc fast path)
// Supports records (structs) and lists (typed slices)
func (i *WazeroInstance) tryCallCompiledInto(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) (bool, error) {
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

	// Lower param to stack - get pointer to param data
	// Go interface layout: [type ptr, data ptr]. For non-pointer types larger than
	// a word, data ptr points to the actual data. This is faster than reflect.
	paramInterface := (*[2]unsafe.Pointer)(unsafe.Pointer(&params[0]))
	paramPtr := paramInterface[1]

	// Set allocator context
	i.alloc.setContext(ctx)

	stackSize, err := i.encoder.LowerToStack(paramCompiled, paramPtr, i.stackBuf, i.memory, i.alloc)
	if err != nil {
		return true, err
	}

	// Call WASM function
	if err := fn.CallWithStack(ctx, i.stackBuf[:stackSize]); err != nil {
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

		resultData, err := i.memory.Read(retptr, resultSize)
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
		_, err = i.decoder.LiftFromStack(resultCompiled, i.stackBuf[:flatCount], resultPtr, i.memory)
		return true, err
	}

	// Result is returned directly on stack
	_, err = i.decoder.LiftFromStack(resultCompiled, i.stackBuf, resultPtr, i.memory)
	return true, err
}

// callGeneralInto is the general path using transcoder with DecodeInto
func (i *WazeroInstance) callGeneralInto(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, result any, params []any) error {
	i.alloc.setContext(ctx)

	allocList := transcoder.NewAllocationList()
	defer allocList.FreeAndRelease(i.alloc)

	// Encode parameters - encoder internally uses compiled fast path when possible
	flatParams, err := i.encoder.EncodeParams(paramTypes, params, i.memory, i.alloc, allocList)
	if err != nil {
		return fmt.Errorf("encode params: %w", err)
	}

	// Call WASM function
	copy(i.stackBuf, flatParams)
	if err := fn.CallWithStack(ctx, i.stackBuf[:len(flatParams)]); err != nil {
		return fmt.Errorf("wasm call failed: %w", err)
	}

	// Handle void return
	if result == nil || len(resultTypes) == 0 {
		return nil
	}

	// Check if result uses retptr (indirect return)
	usesRetptr := usesRetptr(resultTypes)

	// If retptr, read actual result from memory into stackBuf
	if usesRetptr {
		retptr := uint32(i.stackBuf[0])
		resultSize := resultSize(resultTypes[0])

		resultData, err := i.memory.Read(retptr, resultSize)
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
					_, err = i.decoder.LiftFromStack(compiled, i.stackBuf, resultPtr, i.memory)
					return err
				}
			}
		}
	}

	// Fall back to DecodeInto for other types - results are in stackBuf
	return i.decoder.DecodeInto(resultTypes, i.stackBuf, i.memory, result)
}

// tryFastCall attempts direct call for primitive signatures
func (i *WazeroInstance) tryFastCall(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, bool, error) {
	// Try string fast path first
	if result, ok, err := i.tryFastStringCall(ctx, fn, paramTypes, resultTypes, params); ok {
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
		var a, b uint64
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
				b = uint64(v)
			} else {
				return nil, false, nil
			}
		case wit.S32:
			if v, ok := params[1].(int32); ok {
				b = uint64(uint32(v))
			} else {
				return nil, false, nil
			}
		default:
			_ = p
			return nil, false, nil
		}

		i.stackBuf[0] = a
		i.stackBuf[1] = b
		if err := fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
			return nil, true, fmt.Errorf("wasm call failed: %w", err)
		}
		return convertResult(i.stackBuf[0]), true, nil
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
		if err := fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
			return nil, true, fmt.Errorf("wasm call failed: %w", err)
		}
		return convertResult(i.stackBuf[0]), true, nil
	}

	// Handle () -> R
	if len(paramTypes) == 0 {
		if err := fn.CallWithStack(ctx, i.stackBuf[:1]); err != nil {
			return nil, true, fmt.Errorf("wasm call failed: %w", err)
		}
		return convertResult(i.stackBuf[0]), true, nil
	}

	return nil, false, nil
}

// tryFastStringCall handles string parameter/result signatures
func (i *WazeroInstance) tryFastStringCall(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, bool, error) {
	// Need allocator and memory for strings
	if i.allocFn == nil || i.memory == nil {
		return nil, false, nil
	}

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

		// Track allocations for cleanup
		allocList := transcoder.NewAllocationList()
		defer allocList.FreeAndRelease(i.alloc)

		mem := i.memory.mem

		// Allocate and write input string
		var inputPtr uint32
		inputLen := uint32(len(s))
		if inputLen > 0 {
			i.stackBuf[0] = 0
			i.stackBuf[1] = 0
			i.stackBuf[2] = 1 // align
			i.stackBuf[3] = uint64(inputLen)
			if err := i.allocFn.CallWithStack(ctx, i.stackBuf[:4]); err != nil {
				return nil, true, err
			}
			inputPtr = uint32(i.stackBuf[0])
			allocList.Add(inputPtr, inputLen, 1)
			mem.WriteString(inputPtr, s) // avoids []byte(s) allocation
		}

		// Call: (ptr, len) -> retptr
		i.stackBuf[0] = uint64(inputPtr)
		i.stackBuf[1] = uint64(inputLen)
		if err := fn.CallWithStack(ctx, i.stackBuf[:2]); err != nil {
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
			return "", true, nil
		}
		resultData, ok := mem.Read(resultPtr, resultLen)
		if !ok {
			return nil, true, fmt.Errorf("read result data at 0x%x (len %d): out of bounds", resultPtr, resultLen)
		}
		// Use unsafe to avoid string copy - resultData is a view into wasm memory
		return unsafe.String(unsafe.SliceData(resultData), len(resultData)), true, nil
	}

	return nil, false, nil
}

// callGeneral is the general-purpose call path using transcoder
func (i *WazeroInstance) callGeneral(ctx context.Context, fn api.Function, paramTypes []wit.Type, resultTypes []wit.Type, params []any) (any, error) {
	// Update allocator context
	i.alloc.setContext(ctx)

	allocList := transcoder.NewAllocationList()
	defer allocList.FreeAndRelease(i.alloc)

	// Encode parameters - encoder internally uses compiled fast path when possible
	flatParams, err := i.encoder.EncodeParams(paramTypes, params, i.memory, i.alloc, allocList)
	if err != nil {
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

	if err := fn.CallWithStack(ctx, i.stackBuf[:stackSize]); err != nil {
		return nil, fmt.Errorf("wasm call failed: %w", err)
	}

	var goResults []any
	if usesIndirectReturn {
		// Callee allocated return buffer and returned pointer to it in stackBuf[0]
		retptr := uint32(i.stackBuf[0])
		if len(resultTypes) == 1 {
			if _, isString := resultTypes[0].(wit.String); isString {
				// Fast path for string results
				ptr, err := i.memory.ReadU32(retptr)
				if err != nil {
					return nil, fmt.Errorf("read string pointer at 0x%x: %w", retptr, err)
				}
				length, err := i.memory.ReadU32(retptr + 4)
				if err != nil {
					return nil, fmt.Errorf("read string length at 0x%x: %w", retptr+4, err)
				}
				if length > 0 {
					data, err := i.memory.Read(ptr, length)
					if err != nil {
						return nil, fmt.Errorf("read string data at 0x%x (len %d): %w", ptr, length, err)
					}
					goResults = []any{string(data)}
				} else {
					goResults = []any{""}
				}
			} else {
				// Load value directly from memory at retptr address
				val, err := i.decoder.LoadValue(resultTypes[0], retptr, i.memory)
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
				val, err := i.decoder.LoadValue(rt, retptr+offset, i.memory)
				if err != nil {
					return nil, fmt.Errorf("load indirect result[%d]: %w", idx, err)
				}
				goResults[idx] = val
				offset += resultSize(rt)
			}
		}
	} else {
		// Decode results from flat return values in stackBuf
		var err error
		goResults, err = i.decoder.DecodeResults(resultTypes, i.stackBuf, i.memory)
		if err != nil {
			return nil, fmt.Errorf("decode results: %w", err)
		}
	}

	// Return single value if only one result, otherwise return slice
	if len(goResults) == 1 {
		return goResults[0], nil
	}
	return goResults, nil
}
