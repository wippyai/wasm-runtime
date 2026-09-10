package wat

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestCompileTypeUse(t *testing.T) {
	tests := []struct {
		name       string
		wat        string
		numTypes   int
		numParams  int
		numResults int
		wazero     bool
	}{
		{
			name:       "type_only",
			wat:        `(module (type (func (param i32) (result i32))) (func (export "f") (type 0) local.get 0))`,
			numTypes:   1,
			numParams:  1,
			numResults: 1,
			wazero:     true,
		},
		{
			name:       "inline_only",
			wat:        `(module (func (export "f") (param $n i32) (result i32) local.get $n))`,
			numTypes:   1,
			numParams:  1,
			numResults: 1,
			wazero:     true,
		},
		{
			name:       "matching_type_inline",
			wat:        `(module (type (func (param i32) (result i32))) (func (export "f") (type 0) (param $n i32) (result i32) local.get $n))`,
			numTypes:   1,
			numParams:  1,
			numResults: 1,
			wazero:     true,
		},
		{
			name:       "matching_named_type_ref",
			wat:        `(module (type $sig (func (param i32) (result i32))) (func (export "f") (type $sig) (param $n i32) (result i32) local.get $n))`,
			numTypes:   1,
			numParams:  1,
			numResults: 1,
			wazero:     true,
		},
		{
			name:       "type_only_named",
			wat:        `(module (type $sig (func (param i32) (result i32))) (func (export "f") (type $sig) local.get 0))`,
			numTypes:   1,
			numParams:  1,
			numResults: 1,
			wazero:     true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, err := Compile(tt.wat)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			mod, err := wasm.ParseModule(bin)
			if err != nil {
				t.Fatalf("ParseModule: %v", err)
			}
			if len(mod.Types) != tt.numTypes {
				t.Errorf("types = %d, want %d", len(mod.Types), tt.numTypes)
			}
			if len(mod.Funcs) != 1 {
				t.Fatalf("funcs = %d, want 1", len(mod.Funcs))
			}
			ft := mod.Types[mod.Funcs[0]]
			if len(ft.Params) != tt.numParams {
				t.Errorf("params = %d, want %d (%v)", len(ft.Params), tt.numParams, ft.Params)
			}
			if len(ft.Results) != tt.numResults {
				t.Errorf("results = %d, want %d (%v)", len(ft.Results), tt.numResults, ft.Results)
			}
			if !tt.wazero {
				return
			}
			rt := wazero.NewRuntime(ctx)
			t.Cleanup(func() { _ = rt.Close(ctx) })
			compiled, err := rt.CompileModule(ctx, bin)
			if err != nil {
				t.Fatalf("wazero CompileModule: %v", err)
			}
			inst, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
			if err != nil {
				t.Fatalf("wazero InstantiateModule: %v", err)
			}
			t.Cleanup(func() { _ = inst.Close(ctx) })
			fn := inst.ExportedFunction("f")
			if fn == nil {
				t.Fatal("missing export f")
			}
			results, err := fn.Call(ctx, 7)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(results) != 1 || results[0] != 7 {
				t.Errorf("f(7) = %v, want [7]", results)
			}
		})
	}
}

func TestCompileTypeUseMismatch(t *testing.T) {
	_, err := Compile(`(module (type (func (param i32) (result i32))) (func (type 0) (param i32 i32) (result i32) local.get 0))`)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}
