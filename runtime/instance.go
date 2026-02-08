package runtime

import (
	"context"

	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/errors"
)

type Instance struct {
	module         *Module
	wazeroInstance *engine.WazeroInstance
}

// Call invokes an exported function with automatic type inference.
// Requires either a component or WIT definitions; use CallWithTypes for
// native WASM without WIT.
func (i *Instance) Call(ctx context.Context, name string, args ...any) (any, error) {
	if i.module == nil {
		return nil, errors.NotInitialized(errors.PhaseRuntime, "module")
	}
	if i.module.isComponent {
		return i.wazeroInstance.CallWithLift(ctx, name, args...)
	}

	if i.module.witText != "" {
		params, results, err := i.module.GetFunctionTypes(name)
		if err != nil {
			return nil, errors.Wrap(errors.PhaseRuntime, errors.KindNotFound, err, "get function types from WIT")
		}
		return i.wazeroInstance.CallWithTypes(ctx, name, params, results, args...)
	}

	return nil, errors.InvalidInput(errors.PhaseRuntime, "Call() requires a component or WIT definitions; use CallWithTypes() for native WASM without WIT")
}

// CallWithTypes invokes an exported function with explicit WIT types.
func (i *Instance) CallWithTypes(ctx context.Context, name string, params, results []wit.Type, args ...any) (any, error) {
	return i.wazeroInstance.CallWithTypes(ctx, name, params, results, args...)
}

// CallInto decodes results directly into result without intermediate allocation.
// result must be a pointer. For strings, the result references WASM memory and
// is only valid while the instance is alive.
func (i *Instance) CallInto(ctx context.Context, name string, params, results []wit.Type, result any, args ...any) error {
	return i.wazeroInstance.CallInto(ctx, name, params, results, result, args...)
}

func (i *Instance) Close(ctx context.Context) error {
	return i.wazeroInstance.Close(ctx)
}

// EnableAsyncify initializes asyncify support.
// The module must have been compiled with wasm-opt --asyncify.
func (i *Instance) EnableAsyncify(config engine.AsyncifyConfig) error {
	return i.wazeroInstance.EnableAsyncify(config)
}

func (i *Instance) Asyncify() *engine.Asyncify {
	return i.wazeroInstance.Asyncify()
}

func (i *Instance) Scheduler() *engine.Scheduler {
	return i.wazeroInstance.Scheduler()
}

// StartCall creates a step-based call session for async scheduler integration.
func (i *Instance) StartCall(ctx context.Context, name string, args ...any) (*CallSession, error) {
	session, err := i.wazeroInstance.StartCall(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	return &CallSession{session: session}, nil
}

// RunAsync executes a function with asyncify event loop support.
func (i *Instance) RunAsync(ctx context.Context, name string, args ...uint64) ([]uint64, error) {
	return i.wazeroInstance.RunAsync(ctx, name, args...)
}

// GetExportedFunction returns the raw wazero api.Function, or nil if not found.
func (i *Instance) GetExportedFunction(name string) any {
	fn := i.wazeroInstance.GetExportedFunction(name)
	if fn == nil {
		return nil
	}
	return fn
}
