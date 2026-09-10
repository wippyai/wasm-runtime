package engine

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// componentFixture loads a checked-in binary keyed by its exact WAT source.
// Ordinary tests need only Go. To regenerate after editing a fixture, install
// wasm-tools and run WIPPY_UPDATE_COMPONENT_FIXTURES=1 go test ./engine.
func componentFixture(t *testing.T, source string) []byte {
	t.Helper()
	name := fmt.Sprintf("%x.wasm", sha256.Sum256([]byte(source)))
	path := filepath.Join("testdata", "canonical", name)
	if os.Getenv("WIPPY_UPDATE_COMPONENT_FIXTURES") == "1" {
		dir := t.TempDir()
		watPath := filepath.Join(dir, "fixture.wat")
		wasmPath := filepath.Join(dir, "fixture.wasm")
		if err := os.WriteFile(watPath, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "wasm-tools", "parse", watPath, "-o", wasmPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("compile component fixture: %v\n%s", err, out)
		}
		data, err := os.ReadFile(wasmPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return data
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("component fixture %s: %v; regenerate with WIPPY_UPDATE_COMPONENT_FIXTURES=1 go test ./engine", name, err)
	}
	return data
}
