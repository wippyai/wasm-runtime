package engine

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/tetratelabs/wazero/api"
	"go.bytecodealliance.org/wit"
	"go.uber.org/zap"

	wasmruntime "github.com/wippyai/wasm-runtime"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/transcoder"
)

// LowerWrapper wraps a Go function for Canonical ABI lowering.
type LowerWrapper struct {
	argsPool     sync.Pool
	handlerIf    any
	handlerTyp   reflect.Type
	compiler     *transcoder.Compiler
	encoder      *transcoder.Encoder
	decoder      *transcoder.Decoder
	def          *component.LowerDef
	handler      reflect.Value
	paramTypes   []*transcoder.CompiledType
	resultTypes  []*transcoder.CompiledType
	argTypes     []reflect.Type
	numIn        int
	goParamStart int
	hasCtx       bool
}

func (w *LowerWrapper) Name() string {
	return w.def.Name
}

func NewLowerWrapper(def *component.LowerDef, handler any) (*LowerWrapper, error) {
	handlerVal := reflect.ValueOf(handler)
	if handlerVal.Kind() != reflect.Func {
		return nil, fmt.Errorf("handler must be a function, got %T", handler)
	}

	handlerType := handlerVal.Type()
	numIn := handlerType.NumIn()
	hasCtx := numIn > 0 && handlerType.In(0) == reflect.TypeOf((*context.Context)(nil)).Elem()
	goParamStart := 0
	if hasCtx {
		goParamStart = 1
	}

	argTypes := make([]reflect.Type, numIn)
	for i := 0; i < numIn; i++ {
		argTypes[i] = handlerType.In(i)
	}

	w := &LowerWrapper{
		def:          def,
		handler:      handlerVal,
		handlerTyp:   handlerType,
		handlerIf:    handler,
		encoder:      transcoder.NewEncoder(),
		decoder:      transcoder.NewDecoder(),
		compiler:     transcoder.NewCompiler(),
		numIn:        numIn,
		hasCtx:       hasCtx,
		goParamStart: goParamStart,
		argTypes:     argTypes,
		argsPool: sync.Pool{
			New: func() any {
				s := make([]reflect.Value, numIn)
				return &s
			},
		},
	}

	if err := w.compileTypes(); err != nil {
		debugf("canon_lower: type compilation failed, using dynamic transcoding: %v", err)
	}

	return w, nil
}

func (w *LowerWrapper) compileTypes() error {
	handlerType := w.handlerTyp

	w.paramTypes = make([]*transcoder.CompiledType, len(w.def.Params))
	for i, witType := range w.def.Params {
		goIdx := w.goParamStart + i
		if goIdx >= w.numIn {
			break
		}
		goType := handlerType.In(goIdx)
		ct, err := w.compiler.Compile(witType, goType)
		if err != nil {
			return fmt.Errorf("param %d: %w", i, err)
		}
		w.paramTypes[i] = ct
	}

	numOut := handlerType.NumOut()
	w.resultTypes = make([]*transcoder.CompiledType, len(w.def.Results))
	for i, witType := range w.def.Results {
		if i >= numOut {
			break
		}
		goType := handlerType.Out(i)
		ct, err := w.compiler.Compile(witType, goType)
		if err != nil {
			return fmt.Errorf("result %d: %w", i, err)
		}
		w.resultTypes[i] = ct
	}

	return nil
}

func (w *LowerWrapper) BuildRawFunc() api.GoModuleFunc {
	if fastFn := w.tryBuildFastFunc(); fastFn != nil {
		return fastFn
	}

	return func(ctx context.Context, mod api.Module, stack []uint64) {
		w.callHandler(ctx, mod, stack)
	}
}

func (w *LowerWrapper) tryBuildFastFunc() api.GoModuleFunc {
	paramCount := len(w.def.Params)
	resultCount := len(w.def.Results)

	if fn := w.tryBuildStringFastFunc(paramCount, resultCount); fn != nil {
		return fn
	}

	if fn := w.tryBuildBoolFastFunc(paramCount, resultCount); fn != nil {
		return fn
	}

	allU32Params := true
	for _, p := range w.def.Params {
		if _, ok := p.(wit.U32); !ok {
			allU32Params = false
			break
		}
	}

	allU32Results := true
	for _, r := range w.def.Results {
		if _, ok := r.(wit.U32); !ok {
			allU32Results = false
			break
		}
	}

	if !allU32Params || !allU32Results {
		return nil
	}

	if w.hasCtx && paramCount == 2 && resultCount == 1 {
		if fn, ok := w.handlerIf.(func(context.Context, uint32, uint32) uint32); ok {
			return func(ctx context.Context, _ api.Module, stack []uint64) {
				if len(stack) < 2 {
					return
				}
				stack[0] = uint64(fn(ctx, uint32(stack[0]), uint32(stack[1])))
			}
		}
	}

	if w.hasCtx && paramCount == 1 && resultCount == 1 {
		if fn, ok := w.handlerIf.(func(context.Context, uint32) uint32); ok {
			return func(ctx context.Context, _ api.Module, stack []uint64) {
				if len(stack) < 1 {
					return
				}
				stack[0] = uint64(fn(ctx, uint32(stack[0])))
			}
		}
	}

	if !w.hasCtx && paramCount == 2 && resultCount == 1 {
		if fn, ok := w.handlerIf.(func(uint32, uint32) uint32); ok {
			return func(_ context.Context, _ api.Module, stack []uint64) {
				if len(stack) < 2 {
					return
				}
				stack[0] = uint64(fn(uint32(stack[0]), uint32(stack[1])))
			}
		}
	}

	if w.hasCtx && paramCount == 0 && resultCount == 1 {
		if fn, ok := w.handlerIf.(func(context.Context) uint32); ok {
			return func(ctx context.Context, _ api.Module, stack []uint64) {
				if len(stack) < 1 {
					return
				}
				stack[0] = uint64(fn(ctx))
			}
		}
	}

	return nil
}

func (w *LowerWrapper) tryBuildStringFastFunc(paramCount, resultCount int) api.GoModuleFunc {
	// String points into WASM memory - only valid during function call
	if w.hasCtx && paramCount == 1 && resultCount == 0 {
		if _, ok := w.def.Params[0].(wit.String); ok {
			if fn, ok := w.handlerIf.(func(context.Context, string)); ok {
				return func(ctx context.Context, mod api.Module, stack []uint64) {
					if len(stack) < 2 {
						return
					}
					mem := mod.Memory()
					if mem == nil {
						return
					}
					ptr := uint32(stack[0])
					length := uint32(stack[1])
					data, ok := mem.Read(ptr, length)
					if !ok {
						return
					}
					// Zero-copy string - only valid during this call
					fn(ctx, unsafe.String(unsafe.SliceData(data), len(data)))
				}
			}
		}
	}

	if !w.hasCtx && paramCount == 1 && resultCount == 0 {
		if _, ok := w.def.Params[0].(wit.String); ok {
			if fn, ok := w.handlerIf.(func(string)); ok {
				return func(_ context.Context, mod api.Module, stack []uint64) {
					if len(stack) < 2 {
						return
					}
					mem := mod.Memory()
					if mem == nil {
						return
					}
					ptr := uint32(stack[0])
					length := uint32(stack[1])
					data, ok := mem.Read(ptr, length)
					if !ok {
						return
					}
					// Zero-copy string - only valid during this call
					fn(unsafe.String(unsafe.SliceData(data), len(data)))
				}
			}
		}
	}

	if w.hasCtx && paramCount == 2 && resultCount == 1 {
		if _, ok1 := w.def.Params[0].(wit.String); ok1 {
			if _, ok2 := w.def.Params[1].(wit.String); ok2 {
				if _, ok3 := w.def.Results[0].(wit.String); ok3 {
					if fn, ok := w.handlerIf.(func(context.Context, string, string) string); ok {
						var cachedAllocFunc atomic.Value
						return func(ctx context.Context, mod api.Module, stack []uint64) {
							if len(stack) < 5 {
								return
							}
							mem := mod.Memory()
							if mem == nil {
								return
							}
							ptr1, len1 := uint32(stack[0]), uint32(stack[1])
							ptr2, len2 := uint32(stack[2]), uint32(stack[3])
							retptr := uint32(stack[4])
							data1, ok1 := mem.Read(ptr1, len1)
							if !ok1 {
								return
							}
							data2, ok2 := mem.Read(ptr2, len2)
							if !ok2 {
								return
							}
							// Zero-copy strings - only valid during this call
							s1 := unsafe.String(unsafe.SliceData(data1), len(data1))
							s2 := unsafe.String(unsafe.SliceData(data2), len(data2))
							result := fn(ctx, s1, s2)
							var allocFunc api.Function
							if cached := cachedAllocFunc.Load(); cached != nil {
								if fn, ok := cached.(api.Function); ok {
									allocFunc = fn
								}
							}
							if allocFunc == nil {
								allocFunc = mod.ExportedFunction(CabiRealloc)
								if allocFunc != nil {
									cachedAllocFunc.Store(allocFunc)
								}
							}
							if allocFunc != nil && len(result) > 0 {
								resultLen := uint32(len(result))
								var allocStack [4]uint64
								allocStack[0] = 0
								allocStack[1] = 0
								allocStack[2] = 1
								allocStack[3] = uint64(resultLen)
								if err := allocFunc.CallWithStack(ctx, allocStack[:]); err != nil {
									Logger().Warn("string fast path: allocation failed",
										zap.Error(err))
									// Write zero-length result on allocation failure
									mem.WriteUint32Le(retptr, 0)
									mem.WriteUint32Le(retptr+4, 0)
									return
								}
								resultPtr := uint32(allocStack[0])
								if !mem.WriteString(resultPtr, result) {
									Logger().Warn("string fast path: failed to write result string",
										zap.Uint32("ptr", resultPtr),
										zap.Int("len", len(result)))
									return
								}
								if !mem.WriteUint32Le(retptr, resultPtr) || !mem.WriteUint32Le(retptr+4, resultLen) {
									Logger().Warn("string fast path: failed to write result pointer")
									return
								}
							} else if !mem.WriteUint32Le(retptr, 0) || !mem.WriteUint32Le(retptr+4, 0) {
								Logger().Warn("string fast path: failed to write zero result")
								return
							}
						}
					}
				}
			}
		}
	}

	if !w.hasCtx && paramCount == 2 && resultCount == 1 {
		if _, ok1 := w.def.Params[0].(wit.String); ok1 {
			if _, ok2 := w.def.Params[1].(wit.String); ok2 {
				if _, ok3 := w.def.Results[0].(wit.String); ok3 {
					if fn, ok := w.handlerIf.(func(string, string) string); ok {
						var cachedAllocFunc atomic.Value
						return func(ctx context.Context, mod api.Module, stack []uint64) {
							if len(stack) < 5 {
								return
							}
							mem := mod.Memory()
							if mem == nil {
								return
							}
							ptr1, len1 := uint32(stack[0]), uint32(stack[1])
							ptr2, len2 := uint32(stack[2]), uint32(stack[3])
							retptr := uint32(stack[4])
							data1, ok1 := mem.Read(ptr1, len1)
							if !ok1 {
								return
							}
							data2, ok2 := mem.Read(ptr2, len2)
							if !ok2 {
								return
							}
							// Zero-copy strings - only valid during this call
							s1 := unsafe.String(unsafe.SliceData(data1), len(data1))
							s2 := unsafe.String(unsafe.SliceData(data2), len(data2))
							result := fn(s1, s2)
							var allocFunc api.Function
							if cached := cachedAllocFunc.Load(); cached != nil {
								if fn, ok := cached.(api.Function); ok {
									allocFunc = fn
								}
							}
							if allocFunc == nil {
								allocFunc = mod.ExportedFunction(CabiRealloc)
								if allocFunc != nil {
									cachedAllocFunc.Store(allocFunc)
								}
							}
							if allocFunc != nil && len(result) > 0 {
								resultLen := uint32(len(result))
								var allocStack [4]uint64
								allocStack[0] = 0
								allocStack[1] = 0
								allocStack[2] = 1
								allocStack[3] = uint64(resultLen)
								if err := allocFunc.CallWithStack(ctx, allocStack[:]); err != nil {
									Logger().Warn("string2 fast path: allocation failed",
										zap.Error(err))
									mem.WriteUint32Le(retptr, 0)
									mem.WriteUint32Le(retptr+4, 0)
									return
								}
								resultPtr := uint32(allocStack[0])
								if !mem.WriteString(resultPtr, result) {
									Logger().Warn("string2 fast path: failed to write result string",
										zap.Uint32("ptr", resultPtr),
										zap.Int("len", len(result)))
									return
								}
								if !mem.WriteUint32Le(retptr, resultPtr) || !mem.WriteUint32Le(retptr+4, resultLen) {
									Logger().Warn("string2 fast path: failed to write result pointer")
									return
								}
							} else if !mem.WriteUint32Le(retptr, 0) || !mem.WriteUint32Le(retptr+4, 0) {
								Logger().Warn("string2 fast path: failed to write zero result")
								return
							}
						}
					}
				}
			}
		}
	}

	return nil
}

func (w *LowerWrapper) tryBuildBoolFastFunc(paramCount, resultCount int) api.GoModuleFunc {
	if w.hasCtx && paramCount == 1 && resultCount == 1 {
		if _, isU32 := w.def.Params[0].(wit.U32); isU32 {
			if _, isBool := w.def.Results[0].(wit.Bool); isBool {
				if fn, ok := w.handlerIf.(func(context.Context, uint32) bool); ok {
					return func(ctx context.Context, _ api.Module, stack []uint64) {
						if len(stack) < 1 {
							return
						}
						result := fn(ctx, uint32(stack[0]))
						if result {
							stack[0] = 1
						} else {
							stack[0] = 0
						}
					}
				}
			}
		}
	}

	if w.hasCtx && paramCount == 2 && resultCount == 1 {
		if _, isU32_1 := w.def.Params[0].(wit.U32); isU32_1 {
			if _, isU32_2 := w.def.Params[1].(wit.U32); isU32_2 {
				if _, isBool := w.def.Results[0].(wit.Bool); isBool {
					if fn, ok := w.handlerIf.(func(context.Context, uint32, uint32) bool); ok {
						return func(ctx context.Context, _ api.Module, stack []uint64) {
							if len(stack) < 2 {
								return
							}
							result := fn(ctx, uint32(stack[0]), uint32(stack[1]))
							if result {
								stack[0] = 1
							} else {
								stack[0] = 0
							}
						}
					}
				}
			}
		}
	}

	return nil
}

func (w *LowerWrapper) callHandler(ctx context.Context, mod api.Module, stack []uint64) {
	log := Logger()

	if mod == nil {
		log.Error("callHandler: module is nil", zap.String("func", w.def.Name))
		return
	}
	if mod.Memory() == nil {
		log.Error("callHandler: module has no memory", zap.String("func", w.def.Name))
		return
	}
	mem := &WazeroMemory{mem: mod.Memory()}

	allocFunc := mod.ExportedFunction(CabiRealloc)
	if allocFunc == nil {
		log.Error("callHandler: cabi_realloc not found", zap.String("func", w.def.Name))
		return
	}
	alloc := &moduleAllocator{ctx: ctx, allocFunc: allocFunc}

	argsPtr := w.argsPool.Get().(*[]reflect.Value)
	args := *argsPtr
	defer func() {
		// Clear slice elements before returning to pool to avoid retaining references
		var zero reflect.Value
		for i := range args {
			args[i] = zero
		}
		w.argsPool.Put(argsPtr)
	}()
	flatIdx := 0
	paramIdx := 0

	for i := 0; i < w.numIn; i++ {
		paramType := w.argTypes[i] // use pre-cached type

		if i == 0 && w.hasCtx {
			args[i] = reflect.ValueOf(ctx)
			continue
		}

		if paramIdx < len(w.paramTypes) && w.paramTypes[paramIdx] != nil {
			ct := w.paramTypes[paramIdx]
			goValPtr := reflect.New(paramType)
			ptr := unsafe.Pointer(goValPtr.Pointer())
			consumed, err := w.decoder.LiftFromStack(ct, stack[flatIdx:], ptr, mem)
			if err != nil {
				log.Warn("callHandler: LiftFromStack failed",
					zap.String("func", w.def.Name),
					zap.Int("param", paramIdx),
					zap.Error(err))
				args[i] = reflect.Zero(paramType)
			} else {
				args[i] = goValPtr.Elem()
				flatIdx += consumed
			}
			paramIdx++
		} else if paramIdx < len(w.def.Params) {
			witType := w.def.Params[paramIdx]
			goArg, consumed, err := w.liftArg(witType, stack[flatIdx:], mem, paramType)
			if err != nil {
				log.Warn("callHandler: liftArg failed",
					zap.String("func", w.def.Name),
					zap.Int("param", paramIdx),
					zap.Error(err))
				args[i] = reflect.Zero(paramType)
			} else {
				args[i] = goArg
				flatIdx += consumed
			}
			paramIdx++
		} else {
			args[i] = reflect.Zero(paramType)
		}
	}

	var retptr uint32
	if w.usesRetptr() && flatIdx < len(stack) {
		retptr = uint32(stack[flatIdx])
	}

	results := w.handler.Call(args)

	if w.usesRetptr() {
		offset := uint32(0)
		for i, result := range results {
			if i < len(w.def.Results) {
				witType := w.def.Results[i]
				if err := w.storeResultToMemoryWithAlloc(witType, result.Interface(), retptr+offset, mem, alloc); err != nil {
					log.Error("callHandler: storeResultToMemory failed",
						zap.String("func", w.def.Name),
						zap.Int("result", i),
						zap.Error(err))
					return
				}
				offset += resultSize(witType)
			}
		}
	} else {
		resultIdx := 0
		for i, result := range results {
			if i < len(w.resultTypes) && w.resultTypes[i] != nil {
				ct := w.resultTypes[i]
				val := result.Interface()
				rv := reflect.ValueOf(val)
				if rv.Kind() == reflect.Invalid {
					resultIdx += ct.FlatCount
					continue
				}
				tmp := reflect.New(rv.Type())
				tmp.Elem().Set(rv)
				ptr := unsafe.Pointer(tmp.Pointer())
				consumed, err := w.encoder.LowerToStack(ct, ptr, stack[resultIdx:], mem, alloc)
				if err != nil {
					log.Warn("callHandler: LowerToStack failed",
						zap.String("func", w.def.Name),
						zap.Int("result", i),
						zap.Error(err))
				} else {
					resultIdx += consumed
				}
			} else if i < len(w.def.Results) && resultIdx < len(stack) {
				witType := w.def.Results[i]
				flat, err := w.lowerResultWithAlloc(witType, result.Interface(), mem, alloc)
				if err != nil {
					log.Warn("callHandler: lowerResultWithAlloc failed",
						zap.String("func", w.def.Name),
						zap.Int("result", i),
						zap.Error(err))
				} else {
					for _, v := range flat {
						if resultIdx < len(stack) {
							stack[resultIdx] = v
							resultIdx++
						}
					}
				}
			}
		}
	}
}

func (w *LowerWrapper) storeResultToMemoryWithAlloc(witType wit.Type, value any, addr uint32, mem wasmruntime.Memory, alloc wasmruntime.Allocator) error {
	switch witType.(type) {
	case wit.String:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		dataLen := uint32(len(s))
		if dataLen == 0 {
			if err := mem.WriteU32(addr, 0); err != nil {
				return err
			}
			if err := mem.WriteU32(addr+4, 0); err != nil {
				return err
			}
			return nil
		}
		dataAddr, err := alloc.Alloc(dataLen, 1)
		if err != nil {
			return err
		}
		if err := mem.Write(dataAddr, []byte(s)); err != nil {
			return err
		}
		if err := mem.WriteU32(addr, dataAddr); err != nil {
			return err
		}
		if err := mem.WriteU32(addr+4, dataLen); err != nil {
			return err
		}
		return nil
	default:
		flat, err := w.encoder.EncodeParams([]wit.Type{witType}, []any{value}, mem, alloc, nil)
		if err != nil {
			return err
		}
		for i, v := range flat {
			if err := mem.WriteU32(addr+uint32(i*4), uint32(v)); err != nil {
				return err
			}
		}
		return nil
	}
}

func (w *LowerWrapper) liftArg(witType wit.Type, flat []uint64, mem wasmruntime.Memory, goType reflect.Type) (reflect.Value, int, error) {
	value, err := w.decoder.DecodeResults([]wit.Type{witType}, flat, mem)
	if err != nil {
		return reflect.Value{}, 0, err
	}

	if len(value) == 0 {
		return reflect.Zero(goType), 1, nil
	}

	consumed := flatCount(witType)
	return reflect.ValueOf(value[0]).Convert(goType), consumed, nil
}

func (w *LowerWrapper) lowerResultWithAlloc(witType wit.Type, value any, mem wasmruntime.Memory, alloc wasmruntime.Allocator) ([]uint64, error) {
	allocList := transcoder.NewAllocationList()
	defer allocList.Release() // allocations owned by WASM caller
	return w.encoder.EncodeParams([]wit.Type{witType}, []any{value}, mem, alloc, allocList)
}

type moduleAllocator struct {
	ctx       context.Context
	allocFunc api.Function
	stackBuf  [4]uint64 // pre-allocated for CallWithStack
}

func (a *moduleAllocator) Alloc(size, align uint32) (uint32, error) {
	if a.allocFunc == nil {
		return 0, fmt.Errorf("no allocator available")
	}
	a.stackBuf[0] = 0 // oldPtr
	a.stackBuf[1] = 0 // oldSize
	a.stackBuf[2] = uint64(align)
	a.stackBuf[3] = uint64(size)
	if err := a.allocFunc.CallWithStack(a.ctx, a.stackBuf[:]); err != nil {
		return 0, err
	}
	return uint32(a.stackBuf[0]), nil
}

func (a *moduleAllocator) Free(ptr, size, align uint32) {
	// Module-based allocator doesn't support free
}

// ValidateHandler checks if the Go handler matches the WIT signature.
// Returns nil if Params is nil (unknown types from failed component parsing).
func (w *LowerWrapper) ValidateHandler() error {
	if w.def.Params == nil {
		return nil
	}

	handlerType := w.handlerTyp
	numIn := handlerType.NumIn()
	numOut := handlerType.NumOut()

	ctxOffset := 0
	if numIn > 0 && handlerType.In(0) == reflect.TypeOf((*context.Context)(nil)).Elem() {
		ctxOffset = 1
	}

	expectedParams := len(w.def.Params)
	actualParams := numIn - ctxOffset

	if actualParams != expectedParams {
		return fmt.Errorf("param count mismatch: expected %d, got %d", expectedParams, actualParams)
	}

	if w.def.Results == nil {
		return nil
	}

	// WIT result<T, E> maps to Go (T, error)
	expectedResults := len(w.def.Results)
	if numOut != expectedResults {
		if expectedResults == 1 && numOut == 2 && w.hasResultType() {
		} else if expectedResults == 0 && numOut == 0 {
		} else {
			return fmt.Errorf("result count mismatch: expected %d, got %d", expectedResults, numOut)
		}
	}

	return nil
}

func (w *LowerWrapper) hasResultType() bool {
	if len(w.def.Results) != 1 {
		return false
	}
	switch r := w.def.Results[0].(type) {
	case *wit.TypeDef:
		_, ok := r.Kind.(*wit.Result)
		return ok
	default:
		return false
	}
}

func (w *LowerWrapper) FlatSignature() (paramCount, resultCount int) {
	for _, p := range w.def.Params {
		paramCount += flatCount(p)
	}
	for _, r := range w.def.Results {
		resultCount += flatCount(r)
	}
	return
}

func (w *LowerWrapper) usesRetptr() bool {
	return usesRetptr(w.def.Results)
}

func (w *LowerWrapper) FlatParamTypes() []api.ValueType {
	var types []api.ValueType
	for _, p := range w.def.Params {
		types = append(types, getFlatTypes(p)...)
	}
	// If results exceed MAX_FLAT_RESULTS, add retptr parameter
	if w.usesRetptr() {
		types = append(types, api.ValueTypeI32)
	}
	return types
}

func (w *LowerWrapper) FlatResultTypes() []api.ValueType {
	if w.usesRetptr() {
		return nil
	}
	var types []api.ValueType
	for _, r := range w.def.Results {
		flat := getFlatTypes(r)
		types = append(types, flat...)
	}
	return types
}

func getFlatTypes(witType wit.Type) []api.ValueType {
	switch t := witType.(type) {
	case wit.Bool, wit.U8, wit.S8, wit.U16, wit.S16, wit.U32, wit.S32, wit.Char:
		return []api.ValueType{api.ValueTypeI32}
	case wit.U64, wit.S64:
		return []api.ValueType{api.ValueTypeI64}
	case wit.F32:
		return []api.ValueType{api.ValueTypeF32}
	case wit.F64:
		return []api.ValueType{api.ValueTypeF64}
	case wit.String:
		return []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}
	case *wit.TypeDef:
		switch kind := t.Kind.(type) {
		case *wit.Record:
			var types []api.ValueType
			for _, f := range kind.Fields {
				types = append(types, getFlatTypes(f.Type)...)
			}
			return types
		case *wit.List:
			return []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}
		case *wit.Tuple:
			var types []api.ValueType
			for _, elem := range kind.Types {
				types = append(types, getFlatTypes(elem)...)
			}
			return types
		case *wit.Option:
			types := []api.ValueType{api.ValueTypeI32}
			types = append(types, getFlatTypes(kind.Type)...)
			return types
		case *wit.Result:
			maxPayload := []api.ValueType{}
			if kind.OK != nil {
				okTypes := getFlatTypes(kind.OK)
				if len(okTypes) > len(maxPayload) {
					maxPayload = okTypes
				}
			}
			if kind.Err != nil {
				errTypes := getFlatTypes(kind.Err)
				if len(errTypes) > len(maxPayload) {
					maxPayload = errTypes
				}
			}
			return append([]api.ValueType{api.ValueTypeI32}, maxPayload...)
		case *wit.Variant:
			maxPayload := []api.ValueType{}
			for _, c := range kind.Cases {
				if c.Type != nil {
					caseTypes := getFlatTypes(c.Type)
					if len(caseTypes) > len(maxPayload) {
						maxPayload = caseTypes
					}
				}
			}
			return append([]api.ValueType{api.ValueTypeI32}, maxPayload...)
		case *wit.Enum, *wit.Flags:
			return []api.ValueType{api.ValueTypeI32}
		}
	}
	return []api.ValueType{api.ValueTypeI32}
}
