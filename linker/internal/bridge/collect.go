package bridge

import (
	"context"

	"github.com/tetratelabs/wazero/api"
)

// ForwardingWrapper creates a GoModuleFunc that forwards calls to a source function.
// ForwardingWrapper is for bridge modules that re-export functions from other modules.
// It returns nil if sourceFn is nil.
func ForwardingWrapper(sourceFn api.Function, paramCount int) api.GoModuleFunc {
	if sourceFn == nil {
		return nil
	}
	return func(ctx context.Context, caller api.Module, stack []uint64) {
		if paramCount > len(stack) {
			// Invalid state - close module with error code
			if caller != nil {
				_ = caller.CloseWithExitCode(ctx, 1)
			}
			return
		}
		results, err := sourceFn.Call(ctx, stack[:paramCount]...)
		if err != nil {
			// Propagate error by closing the module
			if caller != nil {
				_ = caller.CloseWithExitCode(ctx, 1)
			}
			return
		}
		if len(results) > len(stack) {
			if caller != nil {
				_ = caller.CloseWithExitCode(ctx, 1)
			}
			return
		}
		copy(stack, results)
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
