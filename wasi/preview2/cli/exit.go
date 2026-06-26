package cli

import (
	"context"
	"fmt"
)

type ExitHost struct{}

func NewExitHost() *ExitHost {
	return &ExitHost{}
}

func (h *ExitHost) Namespace() string {
	return "wasi:cli/exit@0.2.3"
}

type ExitError struct {
	Code uint32
}

func (e ExitError) Error() string {
	return fmt.Sprintf("wasi:cli/exit called with code %d", e.Code)
}

func (h *ExitHost) Exit(_ context.Context, status uint32) {
	panic(ExitError{Code: status})
}
