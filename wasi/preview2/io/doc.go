// Package io implements WASI I/O interfaces for stream operations.
//
// Implements:
//   - wasi:io/streams@0.2.8 - Input and output streams
//   - wasi:io/poll@0.2.8 - Pollable resources
//   - wasi:io/error@0.2.8 - Stream errors
//
// Provides async-capable stream abstraction used by filesystem, sockets,
// and HTTP packages.
package io
