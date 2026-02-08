// Package bridge creates and manages bridge modules for component instantiation.
//
// Bridge modules are synthetic WebAssembly modules that connect component imports
// to their providers (host functions, adapter exports, or other component exports).
// The package handles the complexity of wiring up the component model's import
// resolution at the wazero runtime level.
//
// # Components
//
//   - Builder: Creates bridge modules for a component's import requirements
//   - SynthBuilder: Generates synthetic WASM modules with stub functions
//   - Collector: Gathers canonical lower definitions from component sections
//
// # Usage
//
// Bridge is used internally by the linker during component instantiation:
//
//	builder, err := bridge.NewBuilder(runtime)
//	if err != nil {
//	    return err
//	}
//	err = builder.Build(ctx, imports, resolver)
//
// This package is internal to the linker and should not be used directly.
package bridge
