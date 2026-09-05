package engine

import (
	"context"
	"fmt"
	"reflect"
	"unsafe"

	wasmruntime "github.com/wippyai/wasm-runtime"
	"github.com/wippyai/wasm-runtime/transcoder"
)

// typedHostFunction wraps a typed host handler with a direct invocation callback.
var nilHostError = reflect.Zero(reflect.TypeFor[error]())

type typedHostInvoker = func(context.Context, *transcoder.Decoder, []*transcoder.CompiledType, []uint64, wasmruntime.Memory) (reflect.Value, error, error)

type typedHostFunction struct {
	handler any
	invoke  typedHostInvoker
	resume  typedHostInvoker
}

// BindResult0 binds a 0-argument host function returning (R, error) into a typedHostFunction.
// Canonical ABI semantics remain identical to dynamic lowering while eliminating reflect.Call.
func BindResult0[R any](fn func(context.Context) (R, error)) typedHostFunction {
	return typedHostFunction{
		handler: fn,
		invoke: func(ctx context.Context, _ *transcoder.Decoder, _ []*transcoder.CompiledType, _ []uint64, _ wasmruntime.Memory) (reflect.Value, error, error) {
			if fn == nil {
				return reflect.Value{}, nil, fmt.Errorf("typed host function is nil")
			}
			result, hostErr := fn(ctx)
			return reflect.ValueOf(&result).Elem(), hostErr, nil
		},
	}
}

// BindResult1 binds a 1-argument host function returning (R, error) into a typedHostFunction.
// The parameter is lifted via Decoder.LiftFromStack. Lifting failure aborts before calling fn.
func BindResult1[A, R any](fn func(context.Context, A) (R, error)) typedHostFunction {
	return typedHostFunction{
		handler: fn,
		invoke: func(ctx context.Context, dec *transcoder.Decoder, paramTypes []*transcoder.CompiledType, stack []uint64, mem wasmruntime.Memory) (reflect.Value, error, error) {
			if fn == nil {
				return reflect.Value{}, nil, fmt.Errorf("typed host function is nil")
			}
			if dec == nil {
				return reflect.Value{}, nil, fmt.Errorf("transcoder decoder is nil")
			}
			if len(paramTypes) < 1 || paramTypes[0] == nil {
				return reflect.Value{}, nil, fmt.Errorf("insufficient or nil compiled param types: got %d", len(paramTypes))
			}

			var a A
			_, err := dec.LiftFromStack(paramTypes[0], stack, unsafe.Pointer(&a), mem)
			if err != nil {
				return reflect.Value{}, nil, err
			}

			result, hostErr := fn(ctx, a)
			return reflect.ValueOf(&result).Elem(), hostErr, nil
		},
	}
}

// BindResult2 binds a 2-argument host function returning (R, error) into a typedHostFunction.
// Parameters are lifted sequentially into typed local variables via
// Decoder.LiftFromStack, advancing the stack offset.
// If any lifting operation fails, invocation aborts before calling fn and returns the lift error.
func BindResult2[A, B, R any](fn func(context.Context, A, B) (R, error)) typedHostFunction {
	return typedHostFunction{
		handler: fn,
		invoke: func(ctx context.Context, dec *transcoder.Decoder, paramTypes []*transcoder.CompiledType, stack []uint64, mem wasmruntime.Memory) (reflect.Value, error, error) {
			if fn == nil {
				return reflect.Value{}, nil, fmt.Errorf("typed host function is nil")
			}
			if dec == nil {
				return reflect.Value{}, nil, fmt.Errorf("transcoder decoder is nil")
			}
			if len(paramTypes) < 2 || paramTypes[0] == nil || paramTypes[1] == nil {
				return reflect.Value{}, nil, fmt.Errorf("insufficient or nil compiled param types: got %d", len(paramTypes))
			}

			var (
				a      A
				b      B
				offset int
			)

			n, err := dec.LiftFromStack(paramTypes[0], stack[offset:], unsafe.Pointer(&a), mem)
			if err != nil {
				return reflect.Value{}, nil, err
			}
			offset += n

			if offset > len(stack) {
				return reflect.Value{}, nil, fmt.Errorf("stack offset out of bounds")
			}
			_, err = dec.LiftFromStack(paramTypes[1], stack[offset:], unsafe.Pointer(&b), mem)
			if err != nil {
				return reflect.Value{}, nil, err
			}

			result, hostErr := fn(ctx, a, b)
			return reflect.ValueOf(&result).Elem(), hostErr, nil
		},
	}
}

// BindResult3 binds a 3-argument host function returning (R, error) into a typedHostFunction.
// Parameters are lifted sequentially into typed local variables via
// Decoder.LiftFromStack, advancing the stack offset.
// If any lifting operation fails, invocation aborts before calling fn and returns the lift error.
func BindResult3[A, B, C, R any](fn func(context.Context, A, B, C) (R, error)) typedHostFunction {
	return typedHostFunction{
		handler: fn,
		invoke: func(ctx context.Context, dec *transcoder.Decoder, paramTypes []*transcoder.CompiledType, stack []uint64, mem wasmruntime.Memory) (reflect.Value, error, error) {
			if fn == nil {
				return reflect.Value{}, nil, fmt.Errorf("typed host function is nil")
			}
			if dec == nil {
				return reflect.Value{}, nil, fmt.Errorf("transcoder decoder is nil")
			}
			if len(paramTypes) < 3 || paramTypes[0] == nil || paramTypes[1] == nil || paramTypes[2] == nil {
				return reflect.Value{}, nil, fmt.Errorf("insufficient or nil compiled param types: got %d", len(paramTypes))
			}

			var (
				a      A
				b      B
				c      C
				offset int
			)

			n, err := dec.LiftFromStack(paramTypes[0], stack[offset:], unsafe.Pointer(&a), mem)
			if err != nil {
				return reflect.Value{}, nil, err
			}
			offset += n

			if offset > len(stack) {
				return reflect.Value{}, nil, fmt.Errorf("stack offset out of bounds")
			}
			n, err = dec.LiftFromStack(paramTypes[1], stack[offset:], unsafe.Pointer(&b), mem)
			if err != nil {
				return reflect.Value{}, nil, err
			}
			offset += n

			if offset > len(stack) {
				return reflect.Value{}, nil, fmt.Errorf("stack offset out of bounds")
			}
			_, err = dec.LiftFromStack(paramTypes[2], stack[offset:], unsafe.Pointer(&c), mem)
			if err != nil {
				return reflect.Value{}, nil, err
			}

			result, hostErr := fn(ctx, a, b, c)
			return reflect.ValueOf(&result).Elem(), hostErr, nil
		},
	}
}

// BindResult3WithResume supplies a continuation that does not need its original
// arguments. The runtime still restores the canonical result pointer and runs
// raw validation before resuming. Only hosts whose continuation is independent
// of the original arguments should use this adapter.
func BindResult3WithResume[A, B, C, R any](fn func(context.Context, A, B, C) (R, error), resume func(context.Context) (R, error)) typedHostFunction {
	host := BindResult3(fn)
	host.resume = BindResult0(resume).invoke
	return host
}
