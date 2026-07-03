package cli

import (
	"context"

	"github.com/tetratelabs/wazero/sys"
)

type ExitHost struct{}

func NewExitHost() *ExitHost {
	return &ExitHost{}
}

func (h *ExitHost) Namespace() string {
	return "wasi:cli/exit@0.2.3"
}

// Exit terminates the calling guest with status. It panics wazero's own
// sys.ExitError sentinel, which unwinds the guest call stack and is recovered at
// the engine call boundary (and by wazero itself) rather than escaping to the
// host. This mirrors wazero's native proc_exit: the panic is required because a
// return alone would let instructions a toolchain emits after exit (e.g. LLVM
// unreachable) keep running.
func (h *ExitHost) Exit(_ context.Context, status uint32) {
	panic(sys.NewExitError(status))
}
