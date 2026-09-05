package engine

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
	"go.bytecodealliance.org/wit"
	"go.uber.org/zap"

	wasmruntime "github.com/wippyai/wasm-runtime"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/transcoder"
)

// CheckedHostFunction validates raw Canonical ABI arguments before lifting can
// allocate host memory. Validation must be read-only and must not retain stack.
type CheckedHostFunction struct {
	Handler  any
	Validate func(context.Context, api.Module, []uint64) error
}

// LowerWrapper wraps a Go function for Canonical ABI lowering.
type LowerWrapper struct {
	argsPool            sync.Pool
	callMemoryPool      sync.Pool
	typedInvoke         typedHostInvoker
	typedResume         typedHostInvoker
	paramSlots          int
	validateRaw         func(context.Context, api.Module, []uint64) error
	handlerIf           any
	handlerTyp          reflect.Type
	compiler            *transcoder.Compiler
	encoder             *transcoder.Encoder
	decoder             *transcoder.Decoder
	def                 *component.LowerDef
	handler             reflect.Value
	paramTypes          []*transcoder.CompiledType
	resultTypes         []*transcoder.CompiledType
	resultOffsets       []uint32
	resultSuccessType   *transcoder.CompiledType
	resultErrType       wit.Type
	resultPayloadOffset uint32
	resultAreaSize      uint32
	argTypes            []reflect.Type
	numIn               int
	goParamStart        int
	hasCtx              bool
}

func (w *LowerWrapper) Name() string {
	return w.def.Name
}

func NewLowerWrapper(def *component.LowerDef, handler any) (*LowerWrapper, error) {
	var validateRaw func(context.Context, api.Module, []uint64) error
	if checked, ok := handler.(CheckedHostFunction); ok {
		handler, validateRaw = checked.Handler, checked.Validate
	}
	var direct *typedHostFunction
	if typed, ok := handler.(typedHostFunction); ok {
		direct = &typed
		handler = typed.handler
	}
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
		def:            def,
		validateRaw:    validateRaw,
		handler:        handlerVal,
		handlerTyp:     handlerType,
		handlerIf:      handler,
		encoder:        transcoder.NewEncoder(),
		decoder:        transcoder.NewDecoder(),
		compiler:       transcoder.NewCompiler(),
		numIn:          numIn,
		hasCtx:         hasCtx,
		goParamStart:   goParamStart,
		argTypes:       argTypes,
		callMemoryPool: sync.Pool{New: func() any { return new(lowerCallMemory) }},
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
	if direct != nil && w.hasCtx && w.hasResultType() && handlerType.NumOut() == 2 && len(def.Params) == numIn-1 {
		compatible := true
		for i, ct := range w.paramTypes {
			if ct == nil || ct.GoType != handlerType.In(i+1) {
				compatible = false
				break
			}
			w.paramSlots += ct.FlatCount
		}
		if compatible {
			w.typedInvoke = direct.invoke
			w.typedResume = direct.resume
		}
	}

	return w, nil
}

func (w *LowerWrapper) compileTypes() error {
	handlerType := w.handlerTyp
	// Result layout is required even when a parameter uses dynamic lifting.
	w.resultOffsets = make([]uint32, len(w.def.Results))
	var offset uint32
	for i, witType := range w.def.Results {
		w.resultOffsets[i] = offset
		offset += resultSize(witType)
	}
	w.resultAreaSize = offset
	numOut := handlerType.NumOut()
	w.resultTypes = make([]*transcoder.CompiledType, len(w.def.Results))
	isResult := w.hasResultType() && numOut == 2
	if isResult {
		r := w.def.Results[0].(*wit.TypeDef).Kind.(*wit.Result)
		w.resultErrType = r.Err
		layout := transcoder.NewLayoutCalculator().Calculate(w.def.Results[0])
		w.resultPayloadOffset = alignTo(1, layout.Align)
		if r.OK != nil {
			// Unsupported Go representations retain the dynamic encoder.
			w.resultSuccessType, _ = w.compiler.Compile(r.OK, handlerType.Out(0))
		}
	}
	w.paramTypes = make([]*transcoder.CompiledType, len(w.def.Params))
	for i, witType := range w.def.Params {
		goIdx := w.goParamStart + i
		if goIdx >= w.numIn {
			break
		}
		ct, err := w.compiler.Compile(witType, handlerType.In(goIdx))
		if err != nil {
			return fmt.Errorf("param %d: %w", i, err)
		}
		w.paramTypes[i] = ct
	}
	if !isResult {
		for i, witType := range w.def.Results {
			if i >= numOut {
				break
			}
			ct, err := w.compiler.Compile(witType, handlerType.Out(i))
			if err != nil {
				return fmt.Errorf("result %d: %w", i, err)
			}
			w.resultTypes[i] = ct
		}
	}
	return nil
}

func alignTo(offset, align uint32) uint32 {
	if align <= 1 {
		return offset
	}
	return (offset + align - 1) &^ (align - 1)
}

func (w *LowerWrapper) BuildRawFunc() api.GoModuleFunc {
	if fastFn := w.tryBuildFastFunc(); w.validateRaw == nil && fastFn != nil {
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
	// A wasi:cli/exit host call unwinds here as wazero's sys.ExitError. Apply the
	// exit code via CloseWithExitCode (the module has already stopped) and stop;
	// any other panic re-throws so genuine faults still surface.
	defer func() {
		if r := recover(); r != nil {
			if exitErr, ok := r.(*sys.ExitError); ok {
				_ = mod.CloseWithExitCode(ctx, exitErr.ExitCode())
				return
			}
			panic(r)
		}
	}()

	if mod == nil {
		panic(fmt.Errorf("canonical host %s: module is nil", w.def.Name))
	}
	if mod.Memory() == nil {
		panic(fmt.Errorf("canonical host %s: module has no memory", w.def.Name))
	}
	allocFunc := mod.ExportedFunction(CabiRealloc)
	if allocFunc == nil {
		panic(fmt.Errorf("canonical host %s: cabi_realloc not found", w.def.Name))
	}
	callMemory := w.callMemoryPool.Get().(*lowerCallMemory)
	callMemory.memory.mem = mod.Memory()
	callMemory.allocator.ctx = ctx
	callMemory.allocator.allocFunc = allocFunc
	defer func() {
		// A call has finished even when Asyncify parks the guest. No encoder or
		// decoder retains these wrappers; clear instance/context references.
		callMemory.memory.mem = nil
		callMemory.allocator.ctx = nil
		callMemory.allocator.allocFunc = nil
		w.callMemoryPool.Put(callMemory)
	}()
	mem := &callMemory.memory
	alloc := &callMemory.allocator

	async := GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		stack = restoreHostArgs(async.TakeHostArgs(), stack)
	}

	if w.validateRaw != nil {
		if err := w.validateRaw(ctx, mod, stack); err != nil {
			trapCanon(w.def.Name, "validate arguments", err)
		}
	}
	var args []reflect.Value
	flatIdx := w.paramSlots
	if w.typedInvoke == nil {
		argsPtr := w.argsPool.Get().(*[]reflect.Value)
		args = *argsPtr
		defer func() {
			// Clear slice elements before returning to pool to avoid retaining references
			var zero reflect.Value
			for i := range args {
				args[i] = zero
			}
			w.argsPool.Put(argsPtr)
		}()
		flatIdx = 0
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
					trapCanon(w.def.Name, "lift", err)
				}
				args[i] = goValPtr.Elem()
				flatIdx += consumed
				paramIdx++
			} else if paramIdx < len(w.def.Params) {
				witType := w.def.Params[paramIdx]
				goArg, consumed, err := w.liftArg(witType, stack[flatIdx:], mem, paramType)
				if err != nil {
					trapCanon(w.def.Name, "lift", err)
				}
				args[i] = goArg
				flatIdx += consumed
				paramIdx++
			} else {
				args[i] = reflect.Zero(paramType)
			}
		}

	}

	var retptr uint32
	if w.usesRetptr() {
		if flatIdx >= len(stack) {
			trapCanon(w.def.Name, "lift", fmt.Errorf("missing result pointer"))
		}
		retptr = uint32(stack[flatIdx])
		if _, err := mem.Read(retptr, w.resultAreaSize); err != nil {
			trapCanon(w.def.Name, "result pointer", err)
		}
	}

	// Calling an Asyncify export from the host can reuse wazero's stack.
	// Preserve arguments before invoking the handler, not after it suspends.
	var inlineArgs [32]uint64
	var originalArgs []uint64
	if async != nil {
		if len(stack) <= len(inlineArgs) {
			originalArgs = inlineArgs[:len(stack)]
		} else {
			originalArgs = make([]uint64, len(stack))
		}
		copy(originalArgs, stack)
	}
	var directResults [2]reflect.Value
	var results []reflect.Value
	if w.typedInvoke != nil {
		invoke := w.typedInvoke
		if w.typedResume != nil && async != nil && async.IsRewinding(ctx) {
			invoke = w.typedResume
		}
		value, hostErr, liftErr := invoke(ctx, w.decoder, w.paramTypes, stack, mem)
		if liftErr != nil {
			trapCanon(w.def.Name, "lift", liftErr)
		}
		directResults[0] = value
		directResults[1] = nilHostError
		if hostErr != nil {
			directResults[1] = reflect.ValueOf(hostErr)
		}
		results = directResults[:]
	} else {
		results = w.handler.Call(args)
	}
	if async != nil && async.IsUnwinding(ctx) {
		async.ParkHostArgs(originalArgs)
		return
	}

	if w.usesRetptr() {
		if w.hasResultType() && len(results) == 2 {
			errVal := results[1].Interface()
			if isNilHandlerError(errVal) {
				if err := mem.WriteU8(retptr+w.resultOffsets[0], 0); err != nil {
					trapCanon(w.def.Name, "store", err)
				}
				r := w.def.Results[0].(*wit.TypeDef).Kind.(*wit.Result)
				if r.OK != nil {
					payloadAddr := retptr + w.resultOffsets[0] + w.resultPayloadOffset
					if w.resultSuccessType != nil {
						val := results[0]
						if val.Kind() == reflect.Pointer && w.resultSuccessType.Kind != transcoder.KindOption {
							if val.IsNil() {
								trapCanon(w.def.Name, "store", fmt.Errorf("nil result pointer"))
							}
							val = val.Elem()
						}
						valHolder := val
						if !val.CanAddr() {
							valHolder = reflect.New(val.Type()).Elem()
							valHolder.Set(val)
						}
						ptr := unsafe.Pointer(valHolder.Addr().Pointer())
						allocList := transcoder.NewAllocationList()
						defer allocList.Release()
						if err := w.encoder.StoreCompiledToMemory(payloadAddr, w.resultSuccessType, ptr, mem, alloc, allocList); err != nil {
							runtime.KeepAlive(valHolder)
							trapCanon(w.def.Name, "store", err)
						}
						runtime.KeepAlive(valHolder)
					} else {
						if err := w.storeResultToMemoryWithAlloc(r.OK, results[0].Interface(), payloadAddr, mem, alloc); err != nil {
							trapCanon(w.def.Name, "store", err)
						}
					}
				}
			} else {
				if err := mem.WriteU8(retptr+w.resultOffsets[0], 1); err != nil {
					trapCanon(w.def.Name, "store", err)
				}
				if w.resultErrType != nil {
					payloadAddr := retptr + w.resultOffsets[0] + w.resultPayloadOffset
					errPayload := handlerErrPayload(errVal, w.resultErrType)
					if err := w.storeResultToMemoryWithAlloc(w.resultErrType, errPayload, payloadAddr, mem, alloc); err != nil {
						trapCanon(w.def.Name, "store", err)
					}
				}
			}
			return
		}

		for i, result := range results {
			if i < len(w.def.Results) {
				witType := w.def.Results[i]
				offset := w.resultOffsets[i]
				if err := w.storeResultToMemoryWithAlloc(witType, result.Interface(), retptr+offset, mem, alloc); err != nil {
					trapCanon(w.def.Name, "store", err)
				}
			}
		}
	} else {
		// Host handlers follow the Go (value, error) convention, but the Canonical ABI
		// encoder represents a result<T,E> as map[string]any{"ok"/"err"}. Fold the two
		// returns into that representation before lowering; otherwise the raw ok value
		// fails to encode and the result area is left uninitialized.
		if w.hasResultType() && len(results) == 2 {
			var errType wit.Type
			if len(w.def.Results) == 1 {
				errType = resultErrType(w.def.Results[0])
			}
			folded := foldResultValue(results[0].Interface(), results[1].Interface(), errType)
			results = []reflect.Value{reflect.ValueOf(folded)}
		}
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
					trapCanon(w.def.Name, "store", err)
				}
				resultIdx += consumed
			} else if i < len(w.def.Results) && resultIdx < len(stack) {
				witType := w.def.Results[i]
				flat, err := w.lowerResultWithAlloc(witType, result.Interface(), mem, alloc)
				if err != nil {
					trapCanon(w.def.Name, "store", err)
				}
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

// foldResultValue maps a host handler's (value, error) returns onto the
// map[string]any{"ok"/"err"} representation the Canonical ABI encoder expects for
// a result<T,E>. On success the ok payload is the value; on failure the err
// payload is the host error's WIT representation.
func foldResultValue(okVal, errVal any, errType wit.Type) any {
	if isNilHandlerError(errVal) {
		return map[string]any{"ok": okVal}
	}
	return map[string]any{"err": handlerErrPayload(errVal, errType)}
}

func isNilHandlerError(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

func resultErrType(t wit.Type) wit.Type {
	td, ok := t.(*wit.TypeDef)
	if !ok {
		return nil
	}
	r, ok := td.Kind.(*wit.Result)
	if !ok {
		return nil
	}
	return r.Err
}

func isWITString(t wit.Type) bool {
	_, ok := t.(wit.String)
	return ok
}

// handlerErrPayload extracts the WIT error payload from a host error value.
// result<_, string> uses the error message; result<_, error-code> uses a Code
// field or integer discriminant.
func handlerErrPayload(errVal any, errType wit.Type) any {
	// Host error types may expose their exact WIT error payload (e.g. a variant
	// like stream-error). Prefer that over structural reflection.
	if p, ok := errVal.(interface{ WITErrorPayload() any }); ok {
		return p.WITErrorPayload()
	}
	if isWITString(errType) {
		switch e := errVal.(type) {
		case string:
			return e
		case error:
			return e.Error()
		}
		return fmt.Sprint(errVal)
	}
	rv := reflect.ValueOf(errVal)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return uint32(0)
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		if f := rv.FieldByName("Code"); f.IsValid() {
			if f.CanUint() {
				return f.Uint()
			}
			if f.CanInt() {
				return uint64(f.Int())
			}
		}
	}
	if rv.CanUint() {
		return rv.Uint()
	}
	if rv.CanInt() {
		return uint64(rv.Int())
	}
	if err, ok := errVal.(error); ok {
		return err.Error()
	}
	return uint32(0)
}

func trapCanon(name, op string, err error) {
	panic(fmt.Errorf("%s: %s: %w", name, op, err))
}

func restoreHostArgs(saved, stack []uint64) []uint64 {
	if len(saved) == 0 {
		return stack
	}
	if len(stack) < len(saved) {
		out := make([]uint64, len(saved))
		copy(out, saved)
		return out
	}
	copy(stack, saved)
	return stack
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
		allocList := transcoder.NewAllocationList()
		defer allocList.Release() // allocations are owned by the WASM caller
		return w.encoder.StoreToMemory(witType, value, addr, mem, alloc, allocList)
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

// lowerCallMemory is borrowed only for one synchronous host invocation.
// Nested calls acquire separate entries; no entry stays borrowed across a yield.
type lowerCallMemory struct {
	memory    WazeroMemory
	allocator moduleAllocator
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
