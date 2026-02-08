// Package filesystem implements WASI filesystem interfaces for file I/O.
//
// Implements:
//   - wasi:filesystem/types@0.2.3 - Filesystem types and operations
//   - wasi:filesystem/preopens@0.2.3 - Pre-opened directories
//
// Provides sandboxed filesystem access with capability-based security.
// All paths are resolved relative to pre-opened directories.
package filesystem
