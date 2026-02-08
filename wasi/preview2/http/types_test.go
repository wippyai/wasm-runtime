package http_test

import (
	"context"
	"os"
	"testing"

	"github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"github.com/wippyai/wasm-runtime/wasi/preview2/http"
)

var helloHTTPWasm []byte

func init() {
	// Try to load from testbed
	data, err := os.ReadFile("../../../testbed/hello_http.wasm")
	if err == nil {
		helloHTTPWasm = data
	}
}

func TestHTTPComponent(t *testing.T) {
	if helloHTTPWasm == nil {
		t.Skip("hello_http.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	// Register WASI
	wasi := preview2.New().
		WithEnv(map[string]string{}).
		WithArgs([]string{"test.wasm"}).
		WithCwd("/")

	if err := rt.RegisterWASI(wasi); err != nil {
		t.Fatalf("register WASI: %v", err)
	}

	// Register HTTP types host
	httpHost := http.NewTypesHost(wasi.Resources())
	if err := rt.RegisterHost(httpHost); err != nil {
		t.Fatalf("register HTTP host: %v", err)
	}

	// Load the component
	mod, err := rt.LoadComponent(ctx, helloHTTPWasm)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	t.Log("HTTP component loaded and instantiated successfully")

	// Log available exports
	for _, exp := range mod.Exports() {
		t.Logf("Export: %s", exp.Name)
	}

	// Try to call the handler
	// The handle function takes (request: incoming-request, response-out: response-outparam)
	// Both are resource handles (u32)

	// Create dummy handles - the HTTP types host will handle the actual resources
	requestHandle := uint32(1)
	responseOutHandle := uint32(2)

	// Call the exported handle function
	// The function name in components follows the pattern: namespace#name
	_, err = inst.Call(ctx, "wasi:http/incoming-handler@0.2.8#handle", requestHandle, responseOutHandle)
	if err != nil {
		t.Fatalf("handle call error: %v", err)
	}
	t.Log("handle call succeeded")

	// Check if response was set
	resp := httpHost.GetResponse()
	if resp != nil {
		t.Logf("Response: status=%d, body=%q", resp.StatusCode, string(resp.Body))
	} else {
		t.Log("No response captured (expected without full HTTP resource implementation)")
	}
}
