package linker

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func TestExportName(t *testing.T) {
	if got := exportName(""); got != EmptyFieldName {
		t.Fatalf("exportName(\"\") = %q, want %q", got, EmptyFieldName)
	}
	if got := exportName("now"); got != "now" {
		t.Fatalf("exportName(%q) = %q, want unchanged", "now", got)
	}
	if EmptyFieldName == "" || EmptyModuleName == "" || RootModuleName == "" {
		t.Fatal("sentinels must be non-empty")
	}
	if EmptyModuleName == RootModuleName {
		t.Fatal("EmptyModuleName and RootModuleName must be distinct")
	}
}

// TestReactorStartShimRewriteResolves locks in the reactor start-shim fix: a core
// module importing ("", "") with a core (start) is rewritten to import
// (RootModuleName, EmptyFieldName) and resolves against a host module exporting
// EmptyFieldName, so the (start) function (the guest's _initialize) runs. Before
// the fix wazero rejected the empty-named import.
func TestReactorStartShimRewriteResolves(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer rt.Close(ctx)

	called := false
	if _, err := rt.NewHostModuleBuilder(RootModuleName).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(context.Context, api.Module, []uint64) { called = true }), nil, nil).
		Export(exportName("")).
		Instantiate(ctx); err != nil {
		t.Fatalf("host module instantiate: %v", err)
	}

	// (type () -> ()) (import "" "" (func type 0)) (start 0)
	guest := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
		0x02, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00,
		0x08, 0x01, 0x00,
	}
	rewritten := rewriteEmptyModuleNames(guest)
	if _, err := rt.InstantiateWithConfig(ctx, rewritten, wazero.NewModuleConfig().WithName("guest")); err != nil {
		t.Fatalf("start-shim guest failed to instantiate: %v", err)
	}
	if !called {
		t.Fatal("start function (reactor _initialize) was not invoked")
	}
}
