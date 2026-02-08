// Package sockets implements WASI socket interfaces for network I/O.
//
// Implements:
//   - wasi:sockets/network@0.2.0 - Network instance
//   - wasi:sockets/tcp@0.2.0 - TCP sockets
//   - wasi:sockets/tcp-create-socket@0.2.0 - TCP socket creation
//   - wasi:sockets/udp@0.2.0 - UDP sockets
//   - wasi:sockets/udp-create-socket@0.2.0 - UDP socket creation
//   - wasi:sockets/ip-name-lookup@0.2.0 - DNS resolution
//
// Provides capability-based network access with async I/O support.
package sockets
