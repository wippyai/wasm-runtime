package cli

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestEnvironmentHost_GetEnvironment(t *testing.T) {
	env := map[string]string{
		"USER":  "testuser",
		"HOME":  "/home/testuser",
		"SHELL": "/bin/bash",
	}
	host := NewEnvironmentHost(env, nil, "")
	ctx := context.Background()

	result := host.GetEnvironment(ctx)

	if len(result) != 3 {
		t.Errorf("expected 3 env vars, got %d", len(result))
	}

	envMap := make(map[string]string)
	for _, pair := range result {
		envMap[pair[0]] = pair[1]
	}

	if envMap["USER"] != "testuser" {
		t.Errorf("expected USER=testuser, got %s", envMap["USER"])
	}
	if envMap["HOME"] != "/home/testuser" {
		t.Errorf("expected HOME=/home/testuser, got %s", envMap["HOME"])
	}
	if envMap["SHELL"] != "/bin/bash" {
		t.Errorf("expected SHELL=/bin/bash, got %s", envMap["SHELL"])
	}
}

func TestEnvironmentHost_GetArguments(t *testing.T) {
	args := []string{"program", "--flag", "value"}
	host := NewEnvironmentHost(nil, args, "")
	ctx := context.Background()

	result := host.GetArguments(ctx)

	if len(result) != 3 {
		t.Errorf("expected 3 args, got %d", len(result))
	}
	if result[0] != "program" || result[1] != "--flag" || result[2] != "value" {
		t.Errorf("unexpected args: %v", result)
	}
}

func TestEnvironmentHost_InitialCwd(t *testing.T) {
	host := NewEnvironmentHost(nil, nil, "/home/user/project")
	ctx := context.Background()

	result := host.InitialCwd(ctx)

	if result == nil {
		t.Fatal("expected non-nil cwd")
	}
	if *result != "/home/user/project" {
		t.Errorf("expected /home/user/project, got %s", *result)
	}
}

func TestEnvironmentHost_Defaults(t *testing.T) {
	host := NewEnvironmentHost(nil, nil, "")
	ctx := context.Background()

	env := host.GetEnvironment(ctx)
	if len(env) != 0 {
		t.Errorf("expected empty env, got %d vars", len(env))
	}

	args := host.GetArguments(ctx)
	if len(args) != 0 {
		t.Errorf("expected empty args, got %d args", len(args))
	}

	cwd := host.InitialCwd(ctx)
	if cwd == nil || *cwd != "/" {
		t.Error("expected default cwd to be /")
	}
}

func TestEnvironmentHost_Namespace(t *testing.T) {
	host := NewEnvironmentHost(nil, nil, "")
	ns := host.Namespace()
	expected := "wasi:cli/environment@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestExitHost_Namespace(t *testing.T) {
	host := NewExitHost()
	ns := host.Namespace()
	expected := "wasi:cli/exit@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestStdioHost_GetStdin(t *testing.T) {
	resources := preview2.NewResourceTable()
	stdin := preview2.NewInputStreamResource([]byte("test input"))
	stdout := preview2.NewOutputStreamResource(nil)
	stderr := preview2.NewOutputStreamResource(nil)

	host := NewStdioHost(resources, stdin, stdout, stderr)
	ctx := context.Background()

	handle := host.GetStdin(ctx)

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("stdin not found in resource table")
	}
	if _, ok := r.(*preview2.InputStreamResource); !ok {
		t.Error("resource is not an InputStreamResource")
	}
}

func TestStdioHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	stdin := preview2.NewInputStreamResource(nil)
	stdout := preview2.NewOutputStreamResource(nil)
	stderr := preview2.NewOutputStreamResource(nil)

	host := NewStdioHost(resources, stdin, stdout, stderr)
	ns := host.Namespace()
	expected := "wasi:cli/stdin@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestStdoutHost_GetStdout(t *testing.T) {
	resources := preview2.NewResourceTable()
	stdout := preview2.NewOutputStreamResource(nil)

	host := NewStdoutHost(resources, stdout)
	ctx := context.Background()

	handle := host.GetStdout(ctx)

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("stdout not found in resource table")
	}
	if _, ok := r.(*preview2.OutputStreamResource); !ok {
		t.Error("resource is not an OutputStreamResource")
	}
}

func TestStdoutHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	stdout := preview2.NewOutputStreamResource(nil)

	host := NewStdoutHost(resources, stdout)
	ns := host.Namespace()
	expected := "wasi:cli/stdout@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestStderrHost_GetStderr(t *testing.T) {
	resources := preview2.NewResourceTable()
	stderr := preview2.NewOutputStreamResource(nil)

	host := NewStderrHost(resources, stderr)
	ctx := context.Background()

	handle := host.GetStderr(ctx)

	r, ok := resources.Get(handle)
	if !ok {
		t.Fatal("stderr not found in resource table")
	}
	if _, ok := r.(*preview2.OutputStreamResource); !ok {
		t.Error("resource is not an OutputStreamResource")
	}
}

func TestStderrHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	stderr := preview2.NewOutputStreamResource(nil)

	host := NewStderrHost(resources, stderr)
	ns := host.Namespace()
	expected := "wasi:cli/stderr@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestTerminalStdinHost_GetTerminalStdin(t *testing.T) {
	host := NewTerminalStdinHost()
	ctx := context.Background()

	result := host.GetTerminalStdin(ctx)
	if result != nil {
		t.Error("expected nil for terminal stdin (not a terminal)")
	}
}

func TestTerminalStdinHost_Namespace(t *testing.T) {
	host := NewTerminalStdinHost()
	ns := host.Namespace()
	expected := "wasi:cli/terminal-stdin@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestTerminalStdoutHost_GetTerminalStdout(t *testing.T) {
	host := NewTerminalStdoutHost()
	ctx := context.Background()

	result := host.GetTerminalStdout(ctx)
	if result != nil {
		t.Error("expected nil for terminal stdout (not a terminal)")
	}
}

func TestTerminalStdoutHost_Namespace(t *testing.T) {
	host := NewTerminalStdoutHost()
	ns := host.Namespace()
	expected := "wasi:cli/terminal-stdout@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestTerminalStderrHost_GetTerminalStderr(t *testing.T) {
	host := NewTerminalStderrHost()
	ctx := context.Background()

	result := host.GetTerminalStderr(ctx)
	if result != nil {
		t.Error("expected nil for terminal stderr (not a terminal)")
	}
}

func TestTerminalStderrHost_Namespace(t *testing.T) {
	host := NewTerminalStderrHost()
	ns := host.Namespace()
	expected := "wasi:cli/terminal-stderr@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestEnvironmentHost_EmptyStrings(t *testing.T) {
	env := map[string]string{
		"EMPTY": "",
		"SET":   "value",
	}
	host := NewEnvironmentHost(env, []string{}, "")
	ctx := context.Background()

	result := host.GetEnvironment(ctx)
	envMap := make(map[string]string)
	for _, pair := range result {
		envMap[pair[0]] = pair[1]
	}

	if v, ok := envMap["EMPTY"]; !ok || v != "" {
		t.Error("empty environment variable not preserved")
	}
}
