package testbed

import (
	"context"
	"os"
	"testing"

	"github.com/wippyai/wasm-runtime/runtime"
)

var jsCalcWasm []byte

func init() {
	data, err := os.ReadFile("js-calculator.wasm")
	if err == nil {
		jsCalcWasm = data
	}
}

func TestJS_Add(t *testing.T) {
	if jsCalcWasm == nil {
		t.Skip("js-calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	mod, err := rt.LoadComponent(ctx, jsCalcWasm)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	result, err := inst.Call(ctx, "add", int32(5), int32(3))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	t.Logf("add(5, 3) = %v", result)

	if result != int32(8) {
		t.Errorf("expected 8, got %v", result)
	}
}

func TestJS_Greet(t *testing.T) {
	if jsCalcWasm == nil {
		t.Skip("js-calculator.wasm not found")
	}

	ctx := context.Background()
	rt, err := runtime.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	mod, err := rt.LoadComponent(ctx, jsCalcWasm)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	result, err := inst.Call(ctx, "greet", "WebAssembly")
	if err != nil {
		t.Fatalf("greet: %v", err)
	}
	t.Logf("greet(\"WebAssembly\") = %v", result)

	if result != "Hello, WebAssembly!" {
		t.Errorf("expected 'Hello, WebAssembly!', got %v", result)
	}
}
