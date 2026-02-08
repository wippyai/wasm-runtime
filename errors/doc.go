// Package errors provides structured error types for the wasm-runtime library.
//
// Errors are categorized by Phase (where the error occurred) and Kind (error category).
// The Error type includes rich context: field path, Go/WIT type names, and cause chain.
//
// Use the Builder for structured error construction:
//
//	err := errors.New(errors.PhaseEncode, errors.KindTypeMismatch).
//		Path("user", "age").
//		GoType("string").
//		WitType("u32").
//		Detail("cannot convert string to integer").
//		Build()
//
// Or use convenience constructors for common patterns:
//
//	err := errors.TypeMismatch(errors.PhaseEncode, path, "string", "u32")
//	err := errors.OutOfBounds(errors.PhaseDecode, path, 10, 5)
//
// All errors implement the standard error interface and support errors.Is/As.
package errors
