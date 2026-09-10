package linker

import (
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestAsyncifyBoundaryExecutionProfile(t *testing.T) {
	cases := []struct {
		name, source string
		reject       bool
	}{
		{"private table", `(module (table 1 funcref) (func $f) (elem (i32.const 0) $f))`, false},
		{"exported owned table", `(module (table (export "callbacks") 1 funcref) (func $f) (elem (i32.const 0) $f))`, false},
		{"canonical fixup without names", `(module
			(import "arbitrary" "callback" (func $callback))
			(import "arbitrary" "slots" (table 1 funcref))
			(elem (i32.const 0) $callback))`, false},
		{"indirect guest call", `(module
			(type $callback (func))
			(import "child" "callbacks" (table 1 funcref))
			(func (export "run") (call_indirect (type $callback) (i32.const 0))))`, true},
		{"apparently synchronous executable importer", `(module
			(import "child" "callbacks" (table 1 funcref))
			(func (export "run")))`, true},
		{"initializer with defined callback", `(module
			(import "child" "callbacks" (table 1 funcref))
			(func $callback) (elem (i32.const 0) $callback))`, true},
		{"returned function reference", `(module
			(import "child" "get-callback" (func (result funcref))))`, true},
		{"passed function reference", `(module
			(import "child" "set-callback" (func (param funcref))))`, true},
		{"reference global", `(module
			(import "child" "callback" (global funcref)))`, true},
		{"numeric boundary", `(module
			(import "child" "call" (func (param i32 i64 f32 f64) (result i32)))
			(import "child" "counter" (global (mut i64))))`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := wat.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			rt := wazero.NewRuntime(ctx)
			t.Cleanup(func() { _ = rt.Close(ctx) })
			// Each fixture is valid Wasm, including those outside our async
			// execution profile. Keep validation independent of our parser.
			compiled, err := rt.CompileModule(ctx, data)
			if err != nil {
				t.Fatalf("invalid fixture: %v", err)
			}
			_ = compiled.Close(ctx)
			c := &component.ValidatedComponent{Raw: &component.Component{CoreModules: [][]byte{data}}}
			for _, enabled := range []bool{false, true} {
				l := New(rt, Options{AsyncifyTransform: enabled})
				pre, err := l.Instantiate(ctx, c)
				if enabled && tc.reject {
					if err == nil || !strings.Contains(err.Error(), "unsupported cross-core continuation boundary") {
						if pre != nil {
							_ = pre.Close(ctx)
						}
						t.Fatalf("expected execution-profile rejection, got %v", err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("asyncify=%v: %v", enabled, err)
				}
				if err := pre.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
