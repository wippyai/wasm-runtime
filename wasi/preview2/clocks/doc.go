// Package clocks implements WASI clock interfaces for time operations.
//
// Implements:
//   - wasi:clocks/monotonic-clock@0.2.8 - Monotonic time measurements
//   - wasi:clocks/wall-clock@0.2.8 - Wall clock time
//
// Monotonic clocks provide nanosecond precision relative to an arbitrary
// start time. Wall clocks provide real-world time with timezone awareness.
package clocks
