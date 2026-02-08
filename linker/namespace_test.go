package linker

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input  string
		want   Version
		wantOk bool
	}{
		{"0.2.0", Version{0, 2, 0}, true},
		{"1.0.0", Version{1, 0, 0}, true},
		{"0.2", Version{0, 2, 0}, true},
		{"1", Version{1, 0, 0}, true},
		{"0.2.1", Version{0, 2, 1}, true},
		{"10.20.30", Version{10, 20, 30}, true},
		{"", Version{}, false},
		{"abc", Version{}, false},
		{"1.2.3.4", Version{}, false},
		{"1.a.0", Version{}, false},
		{"4294967295", Version{4294967295, 0, 0}, true}, // max uint32
		{"4294967296", Version{}, false},                // overflow
		{"9999999999", Version{}, false},                // way over
		{"1..0", Version{}, false},                      // empty part
		{".1.0", Version{}, false},                      // leading dot
		{"1.0.", Version{}, false},                      // trailing dot
	}

	for _, tt := range tests {
		v, ok := ParseVersion(tt.input)
		if ok != tt.wantOk {
			t.Errorf("ParseVersion(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
		}
		if ok && v != tt.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", tt.input, v, tt.want)
		}
	}
}

func TestVersionCompatible(t *testing.T) {
	tests := []struct {
		have   Version
		want   Version
		compat bool
	}{
		{Version{0, 2, 0}, Version{0, 2, 0}, true},  // exact match
		{Version{0, 2, 1}, Version{0, 2, 0}, true},  // patch higher
		{Version{0, 3, 0}, Version{0, 2, 0}, true},  // minor higher
		{Version{0, 1, 0}, Version{0, 2, 0}, false}, // minor lower
		{Version{1, 0, 0}, Version{0, 2, 0}, false}, // major different
		{Version{0, 2, 0}, Version{0, 2, 1}, false}, // patch lower
	}

	for _, tt := range tests {
		got := tt.have.Compatible(tt.want)
		if got != tt.compat {
			t.Errorf("Version{%v}.Compatible(%v) = %v, want %v",
				tt.have, tt.want, got, tt.compat)
		}
	}
}

func TestVersionString(t *testing.T) {
	v := Version{1, 2, 3}
	if s := v.String(); s != "1.2.3" {
		t.Errorf("Version{1,2,3}.String() = %q, want %q", s, "1.2.3")
	}
}

func TestNamespaceInstance(t *testing.T) {
	ns := NewNamespace()

	child := ns.Instance("wasi:io")
	if child == nil {
		t.Fatal("Instance returned nil")
	}

	if child.Name() != "wasi:io" {
		t.Errorf("Name() = %q, want %q", child.Name(), "wasi:io")
	}

	// Same name should return same instance
	child2 := ns.Instance("wasi:io")
	if child2 != child {
		t.Error("Instance didn't return same child for same name")
	}
}

func TestNamespaceInstanceVersioned(t *testing.T) {
	ns := NewNamespace()

	child := ns.Instance("streams@0.2.0")
	if child == nil {
		t.Fatal("Instance returned nil")
	}

	if child.Name() != "streams" {
		t.Errorf("Name() = %q, want %q", child.Name(), "streams")
	}

	ver := child.Version()
	if ver == nil {
		t.Fatal("Version() returned nil")
	}

	if *ver != (Version{0, 2, 0}) {
		t.Errorf("Version() = %v, want {0,2,0}", *ver)
	}
}

func TestNamespaceInstanceVersioned_SecondCallReturnsSame(t *testing.T) {
	ns := NewNamespace()

	// First call creates versioned namespace
	child1 := ns.Instance("streams@0.2.0")
	if child1 == nil {
		t.Fatal("first Instance returned nil")
	}

	// Second call with same version should return same instance
	child2 := ns.Instance("streams@0.2.0")
	if child2 == nil {
		t.Fatal("second Instance returned nil")
	}

	if child1 != child2 {
		t.Errorf("second call returned different instance: %p vs %p", child1, child2)
	}
}

func TestNamespaceInstanceVersioned_DifferentVersions(t *testing.T) {
	ns := NewNamespace()

	// Create two different versions
	v1 := ns.Instance("streams@0.2.0")
	v2 := ns.Instance("streams@0.2.1")

	if v1 == v2 {
		t.Error("different versions should return different instances")
	}

	if v1.Version().Patch != 0 {
		t.Errorf("v1 patch = %d, want 0", v1.Version().Patch)
	}
	if v2.Version().Patch != 1 {
		t.Errorf("v2 patch = %d, want 1", v2.Version().Patch)
	}
}

func TestNamespaceInstance_InvalidVersion(t *testing.T) {
	ns := NewNamespace()

	// Name with @ but invalid version should still work (version nil, name includes @)
	child := ns.Instance("streams@abc")
	if child == nil {
		t.Fatal("Instance returned nil")
	}
	// When version parse fails, whole string becomes the name
	if child.Name() != "streams@abc" {
		t.Errorf("Name() = %q, want %q", child.Name(), "streams@abc")
	}
	if child.Version() != nil {
		t.Error("Version() should be nil for invalid version")
	}
}

func TestNamespaceDefineFunc(t *testing.T) {
	ns := NewNamespace()
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}

	ns.DefineFunc("test-func", handler, nil, nil)

	def := ns.GetFunc("test-func")
	if def == nil {
		t.Fatal("GetFunc returned nil")
	}

	if def.Name != "test-func" {
		t.Errorf("Name = %q, want %q", def.Name, "test-func")
	}
}

func TestNamespaceResolve(t *testing.T) {
	ns := NewNamespace()
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}

	wasiIO := ns.Instance("wasi:io")
	streams := wasiIO.Instance("streams@0.2.0")
	streams.DefineFunc("read", handler, nil, nil)

	def := ns.Resolve("wasi:io/streams@0.2.0#read")
	if def == nil {
		t.Fatal("Resolve returned nil")
	}

	if def.Name != "read" {
		t.Errorf("Name = %q, want %q", def.Name, "read")
	}
}

func TestNamespaceResolveSemver(t *testing.T) {
	ns := NewNamespace()
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}

	wasiIO := ns.Instance("wasi:io")
	streams := wasiIO.Instance("streams@0.2.1")
	streams.DefineFunc("read", handler, nil, nil)

	// Request 0.2.0, should match 0.2.1 (compatible)
	def := ns.Resolve("wasi:io/streams@0.2.0#read")
	if def == nil {
		t.Fatal("Resolve returned nil for semver compatible version")
	}
}

func TestNamespaceResolveNoMatch(t *testing.T) {
	ns := NewNamespace()

	def := ns.Resolve("nonexistent#func")
	if def != nil {
		t.Error("Resolve should return nil for nonexistent namespace")
	}

	def = ns.Resolve("invalid-path")
	if def != nil {
		t.Error("Resolve should return nil for path without #")
	}
}

func TestNamespaceFullPath(t *testing.T) {
	ns := NewNamespace()

	wasiIO := ns.Instance("wasi:io")
	streams := wasiIO.Instance("streams@0.2.0")

	path := streams.FullPath()
	if path != "wasi:io/streams@0.2.0" {
		t.Errorf("FullPath() = %q, want %q", path, "wasi:io/streams@0.2.0")
	}
}

func TestNamespaceAllFuncs(t *testing.T) {
	ns := NewNamespace()
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}

	ns.DefineFunc("func1", handler, nil, nil)
	ns.DefineFunc("func2", handler, nil, nil)

	funcs := ns.AllFuncs()
	if len(funcs) != 2 {
		t.Errorf("AllFuncs() returned %d funcs, want 2", len(funcs))
	}

	if funcs["func1"] == nil || funcs["func2"] == nil {
		t.Error("AllFuncs() missing expected functions")
	}
}

func TestNamespaceAllChildren(t *testing.T) {
	ns := NewNamespace()

	ns.Instance("child1")
	ns.Instance("child2")

	children := ns.AllChildren()
	if len(children) != 2 {
		t.Errorf("AllChildren() returned %d children, want 2", len(children))
	}
}

func TestNamespaceGetChild(t *testing.T) {
	ns := NewNamespace()

	// Create a child
	child := ns.Instance("mychild")
	if child == nil {
		t.Fatal("Instance returned nil")
	}

	// GetChild should return it
	got := ns.GetChild("mychild")
	if got != child {
		t.Error("GetChild didn't return expected child")
	}

	// Nonexistent should return nil
	got = ns.GetChild("nonexistent")
	if got != nil {
		t.Error("GetChild should return nil for nonexistent")
	}
}

func TestNamespaceFullPath_NoParent(t *testing.T) {
	ns := NewNamespace()
	ns.name = "root"

	path := ns.FullPath()
	if path != "root" {
		t.Errorf("FullPath() = %q, want %q", path, "root")
	}
}

func TestNamespaceFullPath_EmptyParentPath(t *testing.T) {
	root := NewNamespace()
	child := root.Instance("child@1.0.0")

	path := child.FullPath()
	if path != "child@1.0.0" {
		t.Errorf("FullPath() = %q, want %q", path, "child@1.0.0")
	}
}

func TestNamespaceFullPath_NestedNoVersion(t *testing.T) {
	root := NewNamespace()
	wasiIO := root.Instance("wasi:io")
	streams := wasiIO.Instance("streams") // no version

	path := streams.FullPath()
	if path != "wasi:io/streams" {
		t.Errorf("FullPath() = %q, want %q", path, "wasi:io/streams")
	}
}

func TestNamespaceResolveExact(t *testing.T) {
	ns := NewNamespace()
	handler := func(ctx context.Context, mod api.Module, stack []uint64) {}

	// Define at 0.2.1
	wasiIO := ns.Instance("wasi:io")
	streams := wasiIO.Instance("streams@0.2.1")
	streams.DefineFunc("read", handler, nil, nil)

	// ResolveExact should NOT find 0.2.0 (only exact match)
	def := ns.ResolveExact("wasi:io/streams@0.2.0#read")
	if def != nil {
		t.Error("ResolveExact should not find compatible version, only exact")
	}

	// ResolveExact should find exact 0.2.1
	def = ns.ResolveExact("wasi:io/streams@0.2.1#read")
	if def == nil {
		t.Fatal("ResolveExact should find exact version")
	}
	if def.Name != "read" {
		t.Errorf("Name = %q, want %q", def.Name, "read")
	}

	// Regular Resolve should find both via semver
	def = ns.Resolve("wasi:io/streams@0.2.0#read")
	if def == nil {
		t.Fatal("Resolve should find compatible version")
	}
}
