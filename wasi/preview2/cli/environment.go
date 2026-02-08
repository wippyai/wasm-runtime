package cli

import (
	"context"
)

type EnvironmentHost struct {
	env  map[string]string
	cwd  string
	args []string
}

func NewEnvironmentHost(env map[string]string, args []string, cwd string) *EnvironmentHost {
	if env == nil {
		env = make(map[string]string)
	}
	if cwd == "" {
		cwd = "/"
	}
	return &EnvironmentHost{
		env:  env,
		args: args,
		cwd:  cwd,
	}
}

func (h *EnvironmentHost) Namespace() string {
	return "wasi:cli/environment@0.2.3"
}

func (h *EnvironmentHost) GetEnvironment(_ context.Context) [][2]string {
	result := make([][2]string, 0, len(h.env))
	for k, v := range h.env {
		result = append(result, [2]string{k, v})
	}
	return result
}

func (h *EnvironmentHost) GetArguments(_ context.Context) []string {
	return h.args
}

func (h *EnvironmentHost) InitialCwd(_ context.Context) *string {
	return &h.cwd
}
