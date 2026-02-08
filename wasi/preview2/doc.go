// Package preview2 implements the WASI Preview2 component model interfaces.
//
// This package provides resources and host implementations for the WASI Preview2
// specification, enabling WebAssembly components to interact with the host system.
//
// # Quick Start
//
// Create a WASI context and register it with a runtime:
//
//	wasi := preview2.New().
//	    WithEnv(map[string]string{"HOME": "/home/user"}).
//	    WithArgs([]string{"program", "--verbose"}).
//	    WithStdin([]byte("input data"))
//
//	rt.RegisterWASI(wasi)
//
// # Configuration Options
//
// The WASI context can be configured with various options:
//
//   - WithEnv: Set environment variables accessible to the component
//   - WithArgs: Set command-line arguments (argv)
//   - WithCwd: Set the current working directory
//   - WithStdin: Provide data for stdin reads
//   - WithPreopens: Map host directories to component paths
//
// # Resource Management
//
// WASI Preview2 uses a resource-oriented design where handles represent
// capabilities granted to components:
//
//   - ResourceTable: Manages handle lifecycle and ownership
//   - Resource: Interface for all WASI resources (streams, files, sockets)
//   - Pollable: Interface for resources that support async polling
//
// Resources are automatically cleaned up when the component exits or when
// handles are explicitly dropped.
//
// # Implemented Interfaces
//
// Sub-packages provide implementations of specific WASI interfaces:
//
//   - cli: Command-line environment (get-environment, get-arguments, exit)
//   - clocks: Wall clock and monotonic clock with subscription support
//   - filesystem: File and directory operations with capability-based access
//   - io: Input/output streams with blocking and non-blocking modes
//   - random: Cryptographic (get-random-bytes) and insecure random sources
//   - sockets: TCP and UDP networking with async operations
//   - http: HTTP client (outgoing-handler) and server (incoming-handler)
//
// # Capturing Output
//
// After component execution, capture stdout and stderr:
//
//	result, err := inst.Call(ctx, "run")
//	stdout := wasi.Stdout()  // captured stdout bytes
//	stderr := wasi.Stderr()  // captured stderr bytes
//
// # Thread Safety
//
// A single WASI context should be used with one component instance at a time.
// For concurrent component execution, create separate WASI contexts.
package preview2
