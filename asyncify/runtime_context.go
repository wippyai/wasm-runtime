package asyncify

import (
	"context"
	"reflect"
)

// RuntimeController is the per-core part of the Asyncify protocol. A component
// call has one scheduler and pending operation, while every transformed core
// owns a distinct controller and saved stack.
type RuntimeController interface {
	IsNormal(context.Context) bool
	IsUnwinding(context.Context) bool
	IsRewinding(context.Context) bool
	StartUnwind(context.Context) error
	StopUnwind(context.Context) error
	StartRewind(context.Context) error
	StopRewind(context.Context) error
	ClearHostArgs()
}

// RuntimeControllerContextKey lets allocation-free engine contexts carry the
// protocol through Value, including ordinary context wrappers.
type RuntimeControllerContextKey struct{}

// RuntimeControllerContext is one immutable activation. Parent links to the
// enclosing activation; each bridge retains constant space, rather than copying
// the complete path. A nil Controller explicitly masks the caller's controller.
type RuntimeControllerContext struct {
	Controller RuntimeController
	Parent     RuntimeControllerStateProvider
	Boundary   *RuntimeCallBoundary
}

// RuntimeCallBoundary identifies one cached host-to-core function wrapper.
// Its identity is distinct from the core controller: different exports of one
// core may nest, but wazero forbids re-entering the same api.Function object.
type RuntimeCallBoundary struct{ _ byte }

// RuntimeControllerStateProvider supplies activation state without allocating
// a new context value on each host-call lookup.
type RuntimeControllerStateProvider interface {
	AsyncifyRuntimeControllerState() RuntimeControllerContext
}

type runtimeControllerContext struct {
	context.Context
	state RuntimeControllerContext
}

func (c *runtimeControllerContext) AsyncifyRuntimeControllerState() RuntimeControllerContext {
	return c.state
}

func (c *runtimeControllerContext) Value(key any) any {
	if _, ok := key.(RuntimeControllerContextKey); ok {
		return c
	}
	return c.Context.Value(key)
}

// WithRuntimeController selects the child while retaining the scheduler and
// cancellation in ctx. Activations are never mutated or pooled: host code may
// retain a context after the bridge returns.
func WithRuntimeController(ctx context.Context, controller RuntimeController) context.Context {
	return WithRuntimeBoundary(ctx, controller, nil)
}

// WithRuntimeBoundary records both controller ownership and a cached function
// invocation. This prevents executable cycles from re-entering an active call.
func WithRuntimeBoundary(ctx context.Context, controller RuntimeController, boundary *RuntimeCallBoundary) context.Context {
	if ctx == nil {
		panic("cannot create context from nil parent")
	}
	parent, _ := ctx.Value(RuntimeControllerContextKey{}).(RuntimeControllerStateProvider)
	return &runtimeControllerContext{Context: ctx, state: RuntimeControllerContext{
		Controller: controller,
		Parent:     parent,
		Boundary:   boundary,
	}}
}

func RuntimeBoundaryActive(ctx context.Context, boundary *RuntimeCallBoundary) bool {
	if boundary == nil {
		return false
	}
	for state, ok := RuntimeControllerStateFromContext(ctx); ok; state, ok = ParentRuntimeController(state) {
		if state.Boundary == boundary {
			return true
		}
	}
	return false
}

func RuntimeControllerFromContext(ctx context.Context) RuntimeController {
	state, _ := RuntimeControllerStateFromContext(ctx)
	return state.Controller
}

// RuntimeControllerStateFromContext distinguishes an absent protocol from an
// explicit nil selection, which must not fall back to a legacy root controller.
func RuntimeControllerStateFromContext(ctx context.Context) (RuntimeControllerContext, bool) {
	if ctx != nil {
		if provider, ok := ctx.Value(RuntimeControllerContextKey{}).(RuntimeControllerStateProvider); ok {
			return provider.AsyncifyRuntimeControllerState(), true
		}
	}
	return RuntimeControllerContext{}, false
}

// ParentRuntimeController advances outward through immutable activations.
func ParentRuntimeController(state RuntimeControllerContext) (RuntimeControllerContext, bool) {
	if state.Parent == nil {
		return RuntimeControllerContext{}, false
	}
	return state.Parent.AsyncifyRuntimeControllerState(), true
}

// RuntimeControllerActive reports whether controller has an enclosing activation.
func RuntimeControllerActive(ctx context.Context, controller RuntimeController) bool {
	for state, ok := RuntimeControllerStateFromContext(ctx); ok; state, ok = ParentRuntimeController(state) {
		if SameRuntimeController(state.Controller, controller) {
			return true
		}
	}
	return false
}

// SameRuntimeController compares implementations without panicking if a future
// implementation uses a non-comparable concrete type.
func SameRuntimeController(a, b RuntimeController) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	return ta == tb && ta.Comparable() && a == b
}
