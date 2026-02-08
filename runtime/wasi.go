package runtime

import (
	"github.com/wippyai/wasm-runtime/errors"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"github.com/wippyai/wasm-runtime/wasi/preview2/cli"
	"github.com/wippyai/wasm-runtime/wasi/preview2/clocks"
	"github.com/wippyai/wasm-runtime/wasi/preview2/filesystem"
	"github.com/wippyai/wasm-runtime/wasi/preview2/http"
	"github.com/wippyai/wasm-runtime/wasi/preview2/io"
	"github.com/wippyai/wasm-runtime/wasi/preview2/random"
	"github.com/wippyai/wasm-runtime/wasi/preview2/sockets"
)

// RegisterWASI registers all WASI Preview2 host implementations.
func (r *Runtime) RegisterWASI(wasi *preview2.WASI) error {
	resources := wasi.Resources()

	// registerHost wraps RegisterHost with structured error
	registerHost := func(h Host, namespace string) error {
		if err := r.RegisterHost(h); err != nil {
			return errors.Registration(errors.PhaseHost, namespace, "host", err)
		}
		return nil
	}

	ioHost := io.NewHost(resources)
	if err := registerHost(ioHost.Error, "wasi:io/error"); err != nil {
		return err
	}
	if err := registerHost(ioHost.Poll, "wasi:io/poll"); err != nil {
		return err
	}
	if err := registerHost(ioHost.Streams, "wasi:io/streams"); err != nil {
		return err
	}
	if err := registerHost(clocks.NewMonotonicClockHost(resources), "wasi:clocks/monotonic-clock"); err != nil {
		return err
	}
	if err := registerHost(clocks.NewWallClockHost(), "wasi:clocks/wall-clock"); err != nil {
		return err
	}
	if err := registerHost(random.NewSecureRandomHost(), "wasi:random/random"); err != nil {
		return err
	}
	if err := registerHost(random.NewInsecureRandomHost(), "wasi:random/insecure"); err != nil {
		return err
	}
	if err := registerHost(random.NewInsecureSeedHost(), "wasi:random/insecure-seed"); err != nil {
		return err
	}
	if err := registerHost(cli.NewEnvironmentHost(wasi.Env(), wasi.Args(), wasi.Cwd()), "wasi:cli/environment"); err != nil {
		return err
	}
	if err := registerHost(cli.NewExitHost(), "wasi:cli/exit"); err != nil {
		return err
	}
	if err := registerHost(cli.NewStdioHost(resources, wasi.Stdin(), wasi.StdoutResource(), wasi.StderrResource()), "wasi:cli/stdin"); err != nil {
		return err
	}
	if err := registerHost(cli.NewStdoutHost(resources, wasi.StdoutResource()), "wasi:cli/stdout"); err != nil {
		return err
	}
	if err := registerHost(cli.NewStderrHost(resources, wasi.StderrResource()), "wasi:cli/stderr"); err != nil {
		return err
	}
	if err := registerHost(cli.NewTerminalStdinHost(), "wasi:cli/terminal-stdin"); err != nil {
		return err
	}
	if err := registerHost(cli.NewTerminalStdoutHost(), "wasi:cli/terminal-stdout"); err != nil {
		return err
	}
	if err := registerHost(cli.NewTerminalStderrHost(), "wasi:cli/terminal-stderr"); err != nil {
		return err
	}
	if err := registerHost(filesystem.NewTypesHost(resources), "wasi:filesystem/types"); err != nil {
		return err
	}
	if err := registerHost(filesystem.NewPreopensHost(resources, wasi.Preopens()), "wasi:filesystem/preopens"); err != nil {
		return err
	}
	if err := registerHost(sockets.NewInstanceNetworkHost(resources), "wasi:sockets/instance-network"); err != nil {
		return err
	}
	if err := registerHost(sockets.NewTCPCreateSocketHost(resources), "wasi:sockets/tcp-create-socket"); err != nil {
		return err
	}
	if err := registerHost(sockets.NewTCPHost(resources), "wasi:sockets/tcp"); err != nil {
		return err
	}
	if err := registerHost(sockets.NewUDPCreateSocketHost(resources), "wasi:sockets/udp-create-socket"); err != nil {
		return err
	}
	if err := registerHost(sockets.NewUDPHost(resources), "wasi:sockets/udp"); err != nil {
		return err
	}
	if err := registerHost(sockets.NewIPNameLookupHost(resources), "wasi:sockets/ip-name-lookup"); err != nil {
		return err
	}
	if err := registerHost(http.NewTypesHost(resources), "wasi:http/types"); err != nil {
		return err
	}
	if err := registerHost(http.NewOutgoingHandlerHost(resources), "wasi:http/outgoing-handler"); err != nil {
		return err
	}

	return nil
}
