package cli

import (
	"context"
	"os"
	"sync/atomic"

	"golang.org/x/term"
)

const (
	terminalStdinHandle  uint32 = 0x7FFE0001
	terminalStdoutHandle uint32 = 0x7FFE0002
	terminalStderrHandle uint32 = 0x7FFE0003
)

var (
	stdinIsTerminal  int32 = -1 // -1 = unchecked, 0 = no, 1 = yes
	stdoutIsTerminal int32 = -1
	stderrIsTerminal int32 = -1
)

func isTerminal(fd int, cached *int32) bool {
	if v := atomic.LoadInt32(cached); v >= 0 {
		return v == 1
	}
	result := term.IsTerminal(fd)
	if result {
		atomic.StoreInt32(cached, 1)
	} else {
		atomic.StoreInt32(cached, 0)
	}
	return result
}

type TerminalStdinHost struct{}

func NewTerminalStdinHost() *TerminalStdinHost {
	return &TerminalStdinHost{}
}

func (h *TerminalStdinHost) Namespace() string {
	return "wasi:cli/terminal-stdin@0.2.3"
}

func (h *TerminalStdinHost) GetTerminalStdin(_ context.Context) *uint32 {
	if isTerminal(int(os.Stdin.Fd()), &stdinIsTerminal) {
		handle := terminalStdinHandle
		return &handle
	}
	return nil
}

type TerminalStdoutHost struct{}

func NewTerminalStdoutHost() *TerminalStdoutHost {
	return &TerminalStdoutHost{}
}

func (h *TerminalStdoutHost) Namespace() string {
	return "wasi:cli/terminal-stdout@0.2.3"
}

func (h *TerminalStdoutHost) GetTerminalStdout(_ context.Context) *uint32 {
	if isTerminal(int(os.Stdout.Fd()), &stdoutIsTerminal) {
		handle := terminalStdoutHandle
		return &handle
	}
	return nil
}

type TerminalStderrHost struct{}

func NewTerminalStderrHost() *TerminalStderrHost {
	return &TerminalStderrHost{}
}

func (h *TerminalStderrHost) Namespace() string {
	return "wasi:cli/terminal-stderr@0.2.3"
}

func (h *TerminalStderrHost) GetTerminalStderr(_ context.Context) *uint32 {
	if isTerminal(int(os.Stderr.Fd()), &stderrIsTerminal) {
		handle := terminalStderrHandle
		return &handle
	}
	return nil
}
