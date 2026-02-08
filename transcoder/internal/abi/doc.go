// Package abi provides internal utilities for Canonical ABI encoding/decoding.
//
// This package contains type coercion helpers, memory operations, and other
// low-level utilities used by the transcoder package. It implements the
// Canonical ABI specification's type lifting and lowering rules.
//
// # Contents
//
//   - coerce.go: Type coercion between Go types and WIT types
//   - count.go: Flat representation counting for function signatures
//   - helpers.go: Shared utilities for encoding/decoding operations
//
// This package is internal to the transcoder.
package abi
