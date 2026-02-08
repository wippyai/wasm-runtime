// Package codegen provides WASM bytecode emission for asyncify transformation.
//
// This package generates the instrumented WebAssembly bytecode that implements
// asyncify's stack switching mechanism. It emits the control flow and local
// variable management code needed to suspend and resume execution.
//
// # Responsibilities
//
//   - Emit state machine dispatch tables
//   - Generate local variable save/restore sequences
//   - Produce control flow wrappers around async call sites
//
// This package is internal to the asyncify transformer.
package codegen
