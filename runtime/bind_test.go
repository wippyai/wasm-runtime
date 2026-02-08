package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
	wasihttp "github.com/wippyai/wasm-runtime/wasi/preview2/http"
)

func TestBindHTTPHostToComponent(t *testing.T) {
	data, err := os.ReadFile("../testbed/hello_http.wasm")
	if err != nil {
		t.Skip("hello_http.wasm not found in testbed - this test requires an HTTP component")
	}

	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close(ctx)

	// Register WASI Preview2 hosts
	wasi := preview2.New()
	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI: %v", err)
	}

	// Register HTTP types host
	httpHost := wasihttp.NewTypesHost(wasi.Resources())
	if err := rt.RegisterHost(httpHost); err != nil {
		t.Fatalf("register HTTP host: %v", err)
	}

	t.Logf("Hosts registered, now loading component...")

	// Load component - this is where Bind happens
	module, err := rt.LoadComponent(ctx, data)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}

	t.Logf("Component loaded, now compiling...")

	// Try to compile
	if err := module.Compile(ctx); err != nil {
		t.Fatalf("compile component: %v", err)
	}

	t.Logf("Component compiled successfully!")
}
