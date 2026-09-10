package parser

import (
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func parseModule(t *testing.T, input string) *ast.Module {
	t.Helper()
	p := New(token.Tokenize(input))
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return mod
}

func parseErr(t *testing.T, input string) error {
	t.Helper()
	p := New(token.Tokenize(input))
	_, err := p.Parse()
	return err
}

func TestTypeUseBlockParametersHaveNoNames(t *testing.T) {
	for _, input := range []string{
		`(module (func i32.const 1 (block (param $x i32) drop)))`,
		`(module (type (func (param i32))) (func i32.const 1 (block (type 0) (param $x i32) drop)))`,
	} {
		if err := parseErr(t, input); err == nil {
			t.Errorf("accepted named block parameter: %s", input)
		}
	}
	parseModule(t, `(module (func i32.const 1 (block (param i32) drop)))`)
	parseModule(t, `(module (import "env" "f" (func (param $x i32))))`)
}

func TestTypeUseMaximumIndexRejected(t *testing.T) {
	for _, input := range []string{
		`(module (func (type 4294967295)))`,
		`(module (func (block (type 4294967295))))`,
		`(module (import "env" "f" (func (type 4294967295))))`,
		`(module (table 1 funcref) (func i32.const 0 call_indirect (type 4294967295)))`,
	} {
		if err := parseErr(t, input); err == nil {
			t.Errorf("accepted maximum out-of-range type index: %s", input)
		}
	}
}

func funcType(t *testing.T, mod *ast.Module, funcIdx int) ast.FuncType {
	t.Helper()
	if funcIdx >= len(mod.Funcs) {
		t.Fatalf("func %d missing", funcIdx)
	}
	idx := mod.Funcs[funcIdx].TypeIdx
	if int(idx) >= len(mod.Types) {
		t.Fatalf("type index %d out of range (%d types)", idx, len(mod.Types))
	}
	return mod.Types[idx]
}

func localGetImm(t *testing.T, mod *ast.Module, codeIdx int) uint32 {
	t.Helper()
	if codeIdx >= len(mod.Code) {
		t.Fatalf("code %d missing", codeIdx)
	}
	for _, ins := range mod.Code[codeIdx].Code {
		if ins.Opcode == ast.OpLocalGet {
			switch v := ins.Imm.(type) {
			case uint32:
				return v
			case int:
				return uint32(v)
			default:
				t.Fatalf("local.get imm type %T", ins.Imm)
			}
		}
	}
	t.Fatal("no local.get")
	return 0
}

func TestTypeUseSignatures(t *testing.T) {
	tests := []struct {
		localGet   *uint32
		name       string
		input      string
		numTypes   int
		numParams  int
		numResults int
		typeIdx    uint32
		param0     ast.ValType
		result0    ast.ValType
	}{
		{
			name:       "type_only_numeric",
			input:      `(module (type (func (param i32) (result i32))) (func (type 0) local.get 0))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  1,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
		},
		{
			name:       "type_only_named",
			input:      `(module (type $sig (func (param i32) (result i32))) (func (type $sig) local.get 0))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  1,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
		},
		{
			name:       "inline_only",
			input:      `(module (func (param $n i32) (result i32) local.get $n))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  1,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
			localGet:   uint32Ptr(0),
		},
		{
			name:       "matching_type_inline_numeric",
			input:      `(module (type (func (param i32) (result i32))) (func (type 0) (param $n i32) (result i32) local.get $n))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  1,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
			localGet:   uint32Ptr(0),
		},
		{
			name:       "matching_type_inline_named_ref",
			input:      `(module (type $sig (func (param i32) (result i32))) (func (type $sig) (param $n i32) (result i32) local.get $n))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  1,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
			localGet:   uint32Ptr(0),
		},
		{
			name:       "inline_reuses_existing_type",
			input:      `(module (type (func (param i32) (result i32))) (func (param i32) (result i32) local.get 0))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  1,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
		},
		{
			name:       "matching_multi_param_names",
			input:      `(module (type $t (func (param i32 i64) (result i32))) (func (type $t) (param $a i32) (param $b i64) (result i32) local.get $b))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  2,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
			localGet:   uint32Ptr(1),
		},
		{
			name:       "empty_type_only",
			input:      `(module (type (func)) (func (type 0)))`,
			numTypes:   1,
			typeIdx:    0,
			numParams:  0,
			numResults: 0,
		},
		{
			name:       "second_type_index",
			input:      `(module (type (func)) (type (func (param i32) (result i32))) (func (type 1) (param $n i32) (result i32) local.get $n))`,
			numTypes:   2,
			typeIdx:    1,
			numParams:  1,
			numResults: 1,
			param0:     ast.ValTypeI32,
			result0:    ast.ValTypeI32,
			localGet:   uint32Ptr(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := parseModule(t, tt.input)
			if len(mod.Types) != tt.numTypes {
				t.Errorf("types = %d, want %d (signatures=%v)", len(mod.Types), tt.numTypes, mod.Types)
			}
			if len(mod.Funcs) != 1 {
				t.Fatalf("funcs = %d, want 1", len(mod.Funcs))
			}
			if mod.Funcs[0].TypeIdx != tt.typeIdx {
				t.Errorf("TypeIdx = %d, want %d", mod.Funcs[0].TypeIdx, tt.typeIdx)
			}
			ft := funcType(t, mod, 0)
			if len(ft.Params) != tt.numParams {
				t.Errorf("params = %d, want %d (%v)", len(ft.Params), tt.numParams, ft.Params)
			}
			if len(ft.Results) != tt.numResults {
				t.Errorf("results = %d, want %d (%v)", len(ft.Results), tt.numResults, ft.Results)
			}
			if tt.numParams > 0 && ft.Params[0] != tt.param0 {
				t.Errorf("param0 = %v, want %v", ft.Params[0], tt.param0)
			}
			if tt.numResults > 0 && ft.Results[0] != tt.result0 {
				t.Errorf("result0 = %v, want %v", ft.Results[0], tt.result0)
			}
			if tt.localGet != nil {
				got := localGetImm(t, mod, 0)
				if got != *tt.localGet {
					t.Errorf("local.get imm = %d, want %d", got, *tt.localGet)
				}
			}
		})
	}
}

func TestTypeUseMismatchRejected(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "param_count",
			input: `(module (type (func (param i32) (result i32))) (func (type 0) (param i32 i32) (result i32) local.get 0))`,
		},
		{
			name:  "param_type",
			input: `(module (type (func (param i32) (result i32))) (func (type 0) (param i64) (result i32) local.get 0))`,
		},
		{
			name:  "result_count",
			input: `(module (type (func (param i32) (result i32))) (func (type 0) (param i32) (result i32 i32) local.get 0 i32.const 0))`,
		},
		{
			name:  "result_type",
			input: `(module (type (func (param i32) (result i32))) (func (type 0) (param i32) (result i64) i64.extend_i32_s (local.get 0)))`,
		},
		{
			name:  "inline_params_missing_results",
			input: `(module (type (func (param i32) (result i32))) (func (type 0) (param i32) local.get 0))`,
		},
		{
			name:  "inline_results_missing_params",
			input: `(module (type (func (param i32) (result i32))) (func (type 0) (result i32) i32.const 0))`,
		},
		{
			name:  "named_type_mismatch",
			input: `(module (type $sig (func (param i32))) (func (type $sig) (param i32) (result i32) local.get 0))`,
		},
		{
			name:  "empty_inline_param_group",
			input: `(module (type (func (param i32))) (func (type 0) (param)))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseErr(t, tt.input)
			if err == nil {
				t.Fatal("expected mismatch error")
			}
			msg := err.Error()
			if !strings.Contains(msg, "match") && !strings.Contains(msg, "mismatch") && !strings.Contains(msg, "type") {
				t.Errorf("error %q does not describe a type-use mismatch", msg)
			}
		})
	}
}

func TestTypeUseImport(t *testing.T) {
	t.Run("type_only", func(t *testing.T) {
		mod := parseModule(t, `(module (type (func (param i32) (result i32))) (import "env" "f" (func (type 0))))`)
		if len(mod.Types) != 1 {
			t.Errorf("types = %d, want 1", len(mod.Types))
		}
		if len(mod.Imports) != 1 {
			t.Fatalf("imports = %d, want 1", len(mod.Imports))
		}
		if mod.Imports[0].Desc.TypeIdx != 0 {
			t.Errorf("TypeIdx = %d, want 0", mod.Imports[0].Desc.TypeIdx)
		}
		ft := *mod.Imports[0].Desc.Type
		if len(ft.Params) != 1 || len(ft.Results) != 1 {
			t.Errorf("import sig params=%d results=%d, want 1/1", len(ft.Params), len(ft.Results))
		}
	})

	t.Run("matching_type_inline", func(t *testing.T) {
		mod := parseModule(t, `(module (type (func (param i32) (result i32))) (import "env" "f" (func (type 0) (param i32) (result i32))))`)
		if len(mod.Types) != 1 {
			t.Errorf("types = %d, want 1 (%v)", len(mod.Types), mod.Types)
		}
		if mod.Imports[0].Desc.TypeIdx != 0 {
			t.Errorf("TypeIdx = %d, want 0", mod.Imports[0].Desc.TypeIdx)
		}
		ft := *mod.Imports[0].Desc.Type
		if len(ft.Params) != 1 || len(ft.Results) != 1 {
			t.Errorf("import sig params=%d results=%d, want 1/1", len(ft.Params), len(ft.Results))
		}
	})

	t.Run("inline_func_import_matching", func(t *testing.T) {
		mod := parseModule(t, `(module (type (func (param i32) (result i32))) (func (import "env" "f") (type 0) (param i32) (result i32)))`)
		if len(mod.Types) != 1 {
			t.Errorf("types = %d, want 1 (%v)", len(mod.Types), mod.Types)
		}
		if len(mod.Imports) != 1 {
			t.Fatalf("imports = %d, want 1", len(mod.Imports))
		}
		if mod.Imports[0].Desc.TypeIdx != 0 {
			t.Errorf("TypeIdx = %d, want 0", mod.Imports[0].Desc.TypeIdx)
		}
		ft := *mod.Imports[0].Desc.Type
		if len(ft.Params) != 1 || len(ft.Results) != 1 {
			t.Errorf("import sig params=%d results=%d, want 1/1", len(ft.Params), len(ft.Results))
		}
	})

	t.Run("mismatch_rejected", func(t *testing.T) {
		err := parseErr(t, `(module (type (func (param i32))) (import "env" "f" (func (type 0) (param i32 i32))))`)
		if err == nil {
			t.Fatal("expected mismatch error")
		}
	})
}

func TestTypeUseCallIndirect(t *testing.T) {
	t.Run("matching_type_inline", func(t *testing.T) {
		mod := parseModule(t, `(module
			(type $t (func (param i32) (result i32)))
			(table 1 funcref)
			(func (param i32) (result i32)
				(call_indirect (type $t) (param i32) (result i32) (local.get 0) (i32.const 0))))`)
		if len(mod.Types) != 1 {
			t.Errorf("types = %d, want 1 (%v)", len(mod.Types), mod.Types)
		}
		if mod.Funcs[0].TypeIdx != 0 {
			t.Errorf("func TypeIdx = %d, want 0", mod.Funcs[0].TypeIdx)
		}
	})

	t.Run("mismatch_rejected", func(t *testing.T) {
		err := parseErr(t, `(module
			(type $t (func (param i32)))
			(table 1 funcref)
			(func (call_indirect (type $t) (param i32) (result i32) (i32.const 0) (i32.const 0))))`)
		if err == nil {
			t.Fatal("expected mismatch error")
		}
	})
}

func TestTypeUseNamedLocalsWithExtraLocal(t *testing.T) {
	mod := parseModule(t, `(module
		(type (func (param i32) (result i32)))
		(func (type 0) (param $n i32) (result i32) (local $tmp i32)
			local.get $n))`)
	ft := funcType(t, mod, 0)
	if len(ft.Params) != 1 {
		t.Fatalf("params = %d, want 1", len(ft.Params))
	}
	if len(mod.Code[0].Locals) != 1 {
		t.Fatalf("locals = %d, want 1", len(mod.Code[0].Locals))
	}
	if got := localGetImm(t, mod, 0); got != 0 {
		t.Errorf("local.get $n = %d, want 0", got)
	}
}

func uint32Ptr(v uint32) *uint32 { return &v }
