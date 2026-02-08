// Package linker implements WebAssembly Component Model instantiation.
//
// # Main Types
//
//   - Linker: manages host function definitions and namespaces
//   - InstancePre: pre-compiled component ready for instantiation
//   - Instance: running component with callable exports
//
// # Thread Safety
//
// Linker and InstancePre are safe for concurrent use.
// Instance is NOT safe for concurrent use.
//
// # Import Resolution Order
//
//  1. Resolver (VirtualInstance or pre-instantiated Module)
//  2. Linker namespace bindings
//  3. Error on unresolved imports
//
// # Example
//
//	linker := NewWithDefaults(runtime)
//	pre, _ := linker.Instantiate(ctx, component)
//	inst, _ := pre.NewInstance(ctx)
//	defer inst.Close(ctx)
//	result, _ := inst.Call(ctx, "process", input)
package linker
