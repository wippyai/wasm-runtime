// Package handler provides instruction-level handlers for asyncify transformation.
//
// Handler categories:
//   - Passthrough: emit unchanged (arithmetic, comparison)
//   - Stack-tracking: track simulated stack (local.get, local.set)
//   - Control flow: manage block depth and branching
//   - Memory: handle load/store with type tracking
package handler
