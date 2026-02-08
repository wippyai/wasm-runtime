package graph

import (
	"os"
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/component"
)

func TestBuildFromPythonComponent(t *testing.T) {
	// Load the Python component
	data, err := os.ReadFile("../../../testbed/python.wasm")
	if err != nil {
		t.Skipf("python.wasm not found: %v", err)
	}

	validated, err := component.DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	g := Build(validated)

	// Check we found import namespaces
	namespaces := g.ImportNamespaces()
	t.Logf("Found %d import namespaces", len(namespaces))
	for _, ns := range namespaces {
		t.Logf("  Import: %s", ns)
	}

	if len(namespaces) == 0 {
		t.Error("expected to find import namespaces")
	}

	// Check required host functions
	required := g.RequiredHostFunctions()
	t.Logf("Found %d required host functions", len(required))

	// Sample some expected WASI functions
	expectedFuncs := []string{
		"wasi:filesystem/types@0.2.0#[method]descriptor.is-same-object",
		"wasi:io/streams@0.2.0#[method]input-stream.read",
	}
	for _, fn := range expectedFuncs {
		if required[fn] {
			t.Logf("  Required: %s", fn)
		}
	}

	// Check adapter-provided functions
	provided := g.ProvidedByAdapter()
	t.Logf("Found %d adapter-provided functions", len(provided))
}

func TestIsRequiredFromHost(t *testing.T) {
	data, err := os.ReadFile("../../../testbed/python.wasm")
	if err != nil {
		t.Skipf("python.wasm not found: %v", err)
	}

	validated, err := component.DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	g := Build(validated)

	// Verify graph was built successfully
	if g == nil {
		t.Fatal("Build returned nil graph")
	}

	// Verify required host functions can be retrieved
	required := g.RequiredHostFunctions()
	if required == nil {
		t.Fatal("RequiredHostFunctions returned nil")
	}

	// Python component should require some filesystem functions
	var filesystemFuncs []string
	for k := range required {
		if strings.Contains(k, "filesystem/types") {
			filesystemFuncs = append(filesystemFuncs, k)
		}
	}

	// Python component should have filesystem dependencies
	if len(filesystemFuncs) == 0 {
		t.Error("expected python.wasm to require filesystem/types functions")
	}
}
