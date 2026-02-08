package cli

import (
	"context"
	"os"
)

type ExitHost struct{}

func NewExitHost() *ExitHost {
	return &ExitHost{}
}

func (h *ExitHost) Namespace() string {
	return "wasi:cli/exit@0.2.3"
}

func (h *ExitHost) Exit(_ context.Context, status uint32) {
	os.Exit(int(status))
}
