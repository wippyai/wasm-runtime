package preview2

import (
	"testing"
)

func TestWASI_Environment(t *testing.T) {
	wasi := New().WithEnv(map[string]string{
		"USER": "testuser",
		"HOME": "/home/testuser",
		"LANG": "en_US.UTF-8",
	})
	defer wasi.Close()

	env := wasi.Env()

	if len(env) != 3 {
		t.Errorf("expected 3 env vars, got %d", len(env))
	}

	if env["USER"] != "testuser" {
		t.Errorf("expected USER=testuser, got %s", env["USER"])
	}
	if env["HOME"] != "/home/testuser" {
		t.Errorf("expected HOME=/home/testuser, got %s", env["HOME"])
	}
	if env["LANG"] != "en_US.UTF-8" {
		t.Errorf("expected LANG=en_US.UTF-8, got %s", env["LANG"])
	}
}

func TestWASI_Arguments(t *testing.T) {
	wasi := New().WithArgs([]string{"program", "arg1", "arg2", "--flag"})
	defer wasi.Close()

	args := wasi.Args()

	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}

	expected := []string{"program", "arg1", "arg2", "--flag"}
	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("args[%d]: expected %s, got %s", i, expected[i], arg)
		}
	}
}

func TestWASI_WorkingDirectory(t *testing.T) {
	wasi := New().WithCwd("/workspace/project")
	defer wasi.Close()

	cwd := wasi.Cwd()

	if cwd != "/workspace/project" {
		t.Errorf("expected /workspace/project, got %s", cwd)
	}
}

func TestWASI_Stdin(t *testing.T) {
	input := []byte("test input data")
	wasi := New().WithStdin(input)
	defer wasi.Close()

	// Stdin is set up - actual reading tested in integration
	if wasi.stdin == nil {
		t.Error("stdin should not be nil")
	}
}

func TestWASI_StdoutStderr(t *testing.T) {
	wasi := New()
	defer wasi.Close()

	stdout := wasi.Stdout()
	stderr := wasi.Stderr()

	if len(stdout) != 0 {
		t.Error("stdout should be empty initially")
	}
	if len(stderr) != 0 {
		t.Error("stderr should be empty initially")
	}
}

func TestWASI_EmptyDefaults(t *testing.T) {
	wasi := New()
	defer wasi.Close()

	env := wasi.Env()
	if len(env) != 0 {
		t.Errorf("expected empty env, got %d entries", len(env))
	}

	args := wasi.Args()
	if len(args) != 0 {
		t.Errorf("expected empty args, got %d entries", len(args))
	}

	cwd := wasi.Cwd()
	if cwd != "/" {
		t.Errorf("expected default cwd /, got %s", cwd)
	}
}

func TestWASI_ChainedConfiguration(t *testing.T) {
	wasi := New().
		WithEnv(map[string]string{"KEY": "value"}).
		WithArgs([]string{"app"}).
		WithCwd("/test")

	defer wasi.Close()

	env := wasi.Env()
	if len(env) != 1 {
		t.Errorf("expected 1 env var, got %d", len(env))
	}

	args := wasi.Args()
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}

	cwd := wasi.Cwd()
	if cwd != "/test" {
		t.Errorf("expected /test, got %s", cwd)
	}
}

func TestWASI_Resources(t *testing.T) {
	wasi := New()
	defer wasi.Close()

	resources := wasi.Resources()
	if resources == nil {
		t.Fatal("resources should not be nil")
	}
}
