package bridge

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
)

const (
	// valueTypeV128 is the WebAssembly 128-bit vector type (0x7b).
	// In wazero stack representation, each v128 value takes 2 uint64 slots.
	valueTypeV128 api.ValueType = 0x7b
)

// stackSlots computes the number of uint64 stack slots required for a sequence of types.
func stackSlots(types []api.ValueType) int {
	slots := 0
	for _, t := range types {
		slots++
		if t == valueTypeV128 {
			slots++
		}
	}
	return slots
}

// ForwardingWrapper creates a GoModuleFunc that forwards calls to a source function.
// It has no core-controller knowledge and is retained for standalone bridges.
func ForwardingWrapper(sourceFn api.Function, paramCount int) api.GoModuleFunc {
	return forwardingWrapper(sourceFn, paramCount, nil, nil)
}

// ForwardingWrapperWithAsyncify forwards a real core bridge while preserving a
// per-core Asyncify continuation. lookup is dynamic because linker bridges are
// built before engine configuration publishes their controllers. tracked reports
// whether sourceFn belongs to an embedded-transformed core; a missing controller
// for such a core is rejected rather than silently borrowing the caller state.
func ForwardingWrapperWithAsyncify(sourceFn api.Function, paramCount int, lookup func() asyncify.RuntimeController, tracked func() bool) api.GoModuleFunc {
	return forwardingWrapper(sourceFn, paramCount, lookup, tracked)
}

func forwardingWrapper(sourceFn api.Function, paramCount int, lookup func() asyncify.RuntimeController, tracked func() bool) api.GoModuleFunc {
	if sourceFn == nil {
		return nil
	}
	boundary := &asyncify.RuntimeCallBoundary{}
	invokeBoundary := func(ctx context.Context, invoke func(context.Context) error) error {
		return forwardAcrossAsyncifyBoundary(ctx, boundary, lookup, tracked, invoke)
	}
	fail := func(ctx context.Context, caller api.Module, err error) {
		if caller != nil {
			_ = caller.CloseWithExitCode(ctx, 1)
		}
		panic(err)
	}

	def := sourceFn.Definition()
	if def == nil {
		// Fallback for custom api.Function implementations lacking definitions.
		return func(ctx context.Context, caller api.Module, stack []uint64) {
			if err := ctx.Err(); err != nil {
				fail(ctx, caller, err)
			}
			if paramCount < 0 || paramCount > len(stack) {
				if caller != nil {
					_ = caller.CloseWithExitCode(ctx, 1)
				}
				return
			}
			overflow := false
			err := invokeBoundary(ctx, func(callCtx context.Context) error {
				results, err := sourceFn.Call(callCtx, stack[:paramCount]...)
				if err != nil {
					return err
				}
				if len(results) > len(stack) {
					overflow = true
					return nil
				}
				copy(stack, results)
				return nil
			})
			if err != nil {
				fail(ctx, caller, err)
			}
			if overflow && caller != nil {
				_ = caller.CloseWithExitCode(ctx, 1)
			}
		}
	}

	paramTypes := def.ParamTypes()
	if paramCount < 0 || paramCount != len(paramTypes) {
		// Arity mismatch or negative paramCount is invalid state.
		// Guest side effects must not be invoked.
		return func(ctx context.Context, caller api.Module, _ []uint64) {
			if caller != nil {
				_ = caller.CloseWithExitCode(ctx, 1)
			}
		}
	}

	paramSlots := stackSlots(paramTypes)
	resultSlots := stackSlots(def.ResultTypes())
	requiredSlots := paramSlots
	if resultSlots > requiredSlots {
		requiredSlots = resultSlots
	}

	return func(ctx context.Context, caller api.Module, stack []uint64) {
		if err := ctx.Err(); err != nil {
			fail(ctx, caller, err)
		}
		if len(stack) < requiredSlots {
			// Insufficient stack for parameters or results before invocation.
			// Guest side effects must not be invoked.
			if caller != nil {
				_ = caller.CloseWithExitCode(ctx, 1)
			}
			return
		}
		if err := invokeBoundary(ctx, func(callCtx context.Context) error {
			return sourceFn.CallWithStack(callCtx, stack)
		}); err != nil {
			fail(ctx, caller, err)
		}
	}
}

// forwardAcrossAsyncifyBoundary coordinates one P->C bridge edge. It does not
// own a scheduler: the root session scheduler remains in ctx while only the
// selected core controller changes for C.
func forwardAcrossAsyncifyBoundary(ctx context.Context, boundary *asyncify.RuntimeCallBoundary, lookup func() asyncify.RuntimeController, tracked func() bool, invoke func(context.Context) error) error {
	if lookup == nil {
		return invoke(ctx)
	}
	if asyncify.RuntimeBoundaryActive(ctx, boundary) {
		return fmt.Errorf("asyncify bridge: recursive invocation of an active core function boundary")
	}
	parent := asyncify.RuntimeControllerFromContext(ctx)
	child := lookup()
	if parent == nil {
		// The component is executing synchronously; no Asyncify state exists to
		// transfer. A later async host call will fail closed in MakeAsyncHandler.
		return invoke(asyncify.WithRuntimeBoundary(ctx, nil, boundary))
	}
	if child == nil {
		if tracked != nil && tracked() {
			return fmt.Errorf("asyncify bridge: transformed child core has no registered controller")
		}
		// An executable core without a controller may run synchronous code, but
		// it must never inherit the caller controller and suspend on its behalf.
		// MakeAsyncHandler observes this explicit empty selection and traps if
		// the untracked core reaches an async lower. Host and synthetic bridges
		// do not use this real-core wrapper path.
		return invoke(asyncify.WithRuntimeBoundary(ctx, nil, boundary))
	}
	if !parent.IsNormal(ctx) && !parent.IsRewinding(ctx) {
		return fmt.Errorf("asyncify bridge: parent controller is neither normal nor rewinding")
	}
	// An alias back into the currently selected core has no ownership handoff:
	// ordinary Asyncify recursion already saves both frames on that core's
	// stack. Track the function activation but let its outer boundary stop the
	// unwind, without changing controller state at this alias.
	if asyncify.SameRuntimeController(parent, child) {
		return invoke(asyncify.WithRuntimeBoundary(ctx, child, boundary))
	}
	reentrantChild := asyncify.RuntimeControllerActive(ctx, child)

	replaying := parent.IsRewinding(ctx)
	if replaying {
		if child.IsNormal(ctx) {
			if err := child.StartRewind(ctx); err != nil {
				return fmt.Errorf("asyncify bridge: start child rewind: %w", err)
			}
		} else if !reentrantChild || !child.IsRewinding(ctx) {
			return fmt.Errorf("asyncify bridge: child controller is not normal before rewind")
		}
	}

	childCtx := asyncify.WithRuntimeBoundary(ctx, child, boundary)
	if err := invoke(childCtx); err != nil {
		child.ClearHostArgs()
		return err
	}

	switch {
	case child.IsUnwinding(ctx):
		// C yielded. In an A->B->A path, both B activations unwind into
		// B's one Asyncify stack. The inner boundary must leave B unwinding
		// so the outer activation can append its frames before an outer edge
		// stops that controller.
		if !reentrantChild {
			if err := child.StopUnwind(ctx); err != nil {
				child.ClearHostArgs()
				return fmt.Errorf("asyncify bridge: stop child unwind: %w", err)
			}
		}
		if replaying && parent.IsRewinding(ctx) {
			// C consumed the previous result and yielded again before returning
			// to P. Finish P's old rewind at this import boundary before saving
			// a fresh continuation for the new pending operation.
			if err := parent.StopRewind(ctx); err != nil {
				child.ClearHostArgs()
				return fmt.Errorf("asyncify bridge: stop parent rewind for resuspension: %w", err)
			}
		}
		if parent.IsNormal(ctx) {
			if err := parent.StartUnwind(ctx); err != nil {
				child.ClearHostArgs()
				return fmt.Errorf("asyncify bridge: start parent unwind: %w", err)
			}
		} else if !parent.IsUnwinding(ctx) {
			child.ClearHostArgs()
			return fmt.Errorf("asyncify bridge: parent did not enter unwind after child yield")
		}
		return nil
	case child.IsNormal(ctx):
		if replaying {
			if parent.IsRewinding(ctx) {
				if err := parent.StopRewind(ctx); err != nil {
					child.ClearHostArgs()
					return fmt.Errorf("asyncify bridge: stop parent rewind: %w", err)
				}
			} else if !parent.IsNormal(ctx) {
				child.ClearHostArgs()
				return fmt.Errorf("asyncify bridge: parent left rewind in invalid state")
			}
		}
		return nil
	default:
		child.ClearHostArgs()
		return fmt.Errorf("asyncify bridge: child controller entered invalid state")
	}
}

// Collector gathers function exports from various sources for bridge creation.
type Collector struct {
	// SafeGetFunc retrieves a function, returning nil on panic (wazevo re-export issue)
	SafeGetFunc func(mod api.Module, name string) api.Function
}

// NewCollector creates a new export collector.
func NewCollector() *Collector {
	return &Collector{
		SafeGetFunc: safeGetExportedFunction,
	}
}

// safeGetExportedFunction wraps ExportedFunction with panic recovery.
// This handles wazero wazevo engine issues with re-exported imports.
// Returns nil if the function cannot be accessed (panic recovered).
func safeGetExportedFunction(mod api.Module, name string) (fn api.Function) {
	defer func() {
		if r := recover(); r != nil {
			// Function not accessible - likely a re-exported import issue in wazevo.
			// This is expected for some modules and not an error condition.
			// Note: Using Debug level since this is an expected condition for some modules.
			fn = nil
		}
	}()
	return mod.ExportedFunction(name)
}

// FromModule collects function exports from a real wazero module.
func (c *Collector) FromModule(mod api.Module) []Export {
	if mod == nil {
		return nil
	}
	var exports []Export
	defs := mod.ExportedFunctionDefinitions()
	if defs == nil {
		return nil
	}

	for funcName, def := range defs {
		if def == nil {
			continue
		}
		fn := c.SafeGetFunc(mod, funcName)
		if fn == nil {
			continue
		}
		wrapper := ForwardingWrapper(fn, len(def.ParamTypes()))
		if wrapper == nil {
			continue
		}
		exports = append(exports, Export{
			Name:        funcName,
			Fn:          wrapper,
			ParamTypes:  def.ParamTypes(),
			ResultTypes: def.ResultTypes(),
		})
	}
	return exports
}

// MergeBindings adds host function exports from bindings to existing exports.
func (c *Collector) MergeBindings(exports []Export, bindings []HostBinding) []Export {
	if len(bindings) == 0 {
		return exports
	}

	existing := make(map[string]bool)
	for _, exp := range exports {
		existing[exp.Name] = true
	}

	for _, binding := range bindings {
		if existing[binding.ImportName] {
			continue
		}
		existing[binding.ImportName] = true

		if binding.IsTrap {
			exports = append(exports, Export{
				Name:        binding.ImportName,
				Fn:          TrapHandler,
				ParamTypes:  binding.ParamTypes,
				ResultTypes: binding.ResultTypes,
			})
		} else if binding.Handler != nil {
			exports = append(exports, Export{
				Name:        binding.ImportName,
				Fn:          binding.Handler,
				ParamTypes:  binding.ParamTypes,
				ResultTypes: binding.ResultTypes,
			})
		}
	}

	return exports
}

// TrapHandler is a function that closes the module when called.
// TrapHandler is for unresolved imports that should trap on invocation.
var TrapHandler = api.GoModuleFunc(func(ctx context.Context, mod api.Module, _ []uint64) {
	if mod != nil {
		_ = mod.CloseWithExitCode(ctx, 1)
	}
})
