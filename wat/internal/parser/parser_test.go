package parser

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wat/internal/ast"
	"github.com/wippyai/wasm-runtime/wat/internal/token"
)

func TestParseEmptyModule(t *testing.T) {
	tokens := token.Tokenize("(module)")
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if mod == nil {
		t.Fatal("module is nil")
	}
	if len(mod.Types) != 0 {
		t.Errorf("expected 0 types, got %d", len(mod.Types))
	}
	if len(mod.Funcs) != 0 {
		t.Errorf("expected 0 funcs, got %d", len(mod.Funcs))
	}
}

func TestParseModuleWithName(t *testing.T) {
	tokens := token.Tokenize("(module $mymodule)")
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseFunc(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		numTypes   int
		numFuncs   int
		numParams  int
		numResults int
	}{
		{
			"empty_func",
			"(module (func))",
			1, 1, 0, 0,
		},
		{
			"func_with_param",
			"(module (func (param i32)))",
			1, 1, 1, 0,
		},
		{
			"func_with_result",
			"(module (func (result i32) (i32.const 0)))",
			1, 1, 0, 1,
		},
		{
			"func_with_params_results",
			"(module (func (param i32 i64) (result f32 f64) (f32.const 0) (f64.const 0)))",
			1, 1, 2, 2,
		},
		{
			"func_with_name",
			"(module (func $add (param i32 i32) (result i32) (i32.add (local.get 0) (local.get 1))))",
			1, 1, 2, 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			mod, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(mod.Types) != tt.numTypes {
				t.Errorf("types = %d, want %d", len(mod.Types), tt.numTypes)
			}
			if len(mod.Funcs) != tt.numFuncs {
				t.Errorf("funcs = %d, want %d", len(mod.Funcs), tt.numFuncs)
			}
			if tt.numFuncs > 0 {
				ft := mod.Types[mod.Funcs[0].TypeIdx]
				if len(ft.Params) != tt.numParams {
					t.Errorf("params = %d, want %d", len(ft.Params), tt.numParams)
				}
				if len(ft.Results) != tt.numResults {
					t.Errorf("results = %d, want %d", len(ft.Results), tt.numResults)
				}
			}
		})
	}
}

func TestParseType(t *testing.T) {
	tokens := token.Tokenize("(module (type $sig (func (param i32) (result i64))))")
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(mod.Types))
	}
	ft := mod.Types[0]
	if len(ft.Params) != 1 || ft.Params[0] != ast.ValTypeI32 {
		t.Error("wrong param type")
	}
	if len(ft.Results) != 1 || ft.Results[0] != ast.ValTypeI64 {
		t.Error("wrong result type")
	}
}

func TestParseImport(t *testing.T) {
	tests := []struct {
		name  string
		input string
		kind  byte
	}{
		{"import_func", `(module (import "m" "f" (func)))`, ast.KindFunc},
		{"import_memory", `(module (import "m" "mem" (memory 1)))`, ast.KindMemory},
		{"import_table", `(module (import "m" "tab" (table 1 funcref)))`, ast.KindTable},
		{"import_global", `(module (import "m" "g" (global i32)))`, ast.KindGlobal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			mod, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(mod.Imports) != 1 {
				t.Fatalf("expected 1 import, got %d", len(mod.Imports))
			}
			if mod.Imports[0].Desc.Kind != tt.kind {
				t.Errorf("import kind = %d, want %d", mod.Imports[0].Desc.Kind, tt.kind)
			}
		})
	}
}

func TestParseExport(t *testing.T) {
	tokens := token.Tokenize(`(module (func $f) (export "myFunc" (func $f)))`)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Exports) != 1 {
		t.Fatalf("expected 1 export, got %d", len(mod.Exports))
	}
	if mod.Exports[0].Name != "myFunc" {
		t.Errorf("export name = %q, want %q", mod.Exports[0].Name, "myFunc")
	}
	if mod.Exports[0].Kind != ast.KindFunc {
		t.Errorf("export kind = %d, want %d", mod.Exports[0].Kind, ast.KindFunc)
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		min    uint32
		hasMax bool
		max    uint32
	}{
		{"min_only", "(module (memory 1))", 1, false, 0},
		{"min_max", "(module (memory 1 10))", 1, true, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			mod, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(mod.Memories) != 1 {
				t.Fatalf("expected 1 memory, got %d", len(mod.Memories))
			}
			mem := mod.Memories[0]
			if mem.Limits.Min != tt.min {
				t.Errorf("min = %d, want %d", mem.Limits.Min, tt.min)
			}
			if tt.hasMax {
				if mem.Limits.Max == nil || *mem.Limits.Max != tt.max {
					t.Errorf("max = %v, want %d", mem.Limits.Max, tt.max)
				}
			} else if mem.Limits.Max != nil {
				t.Error("expected no max")
			}
		})
	}
}

func TestParseTable(t *testing.T) {
	tokens := token.Tokenize("(module (table 10 20 funcref))")
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(mod.Tables))
	}
	tbl := mod.Tables[0]
	if tbl.ElemType != ast.RefTypeFuncref {
		t.Errorf("elem type = 0x%02X, want 0x%02X", tbl.ElemType, ast.RefTypeFuncref)
	}
	if tbl.Limits.Min != 10 {
		t.Errorf("min = %d, want 10", tbl.Limits.Min)
	}
	if tbl.Limits.Max == nil || *tbl.Limits.Max != 20 {
		t.Error("max should be 20")
	}
}

func TestParseGlobal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		valType ast.ValType
		mutable bool
	}{
		{"immut_i32", "(module (global i32 (i32.const 0)))", ast.ValTypeI32, false},
		{"mut_i64", "(module (global (mut i64) (i64.const 0)))", ast.ValTypeI64, true},
		{"immut_f32", "(module (global f32 (f32.const 0)))", ast.ValTypeF32, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			mod, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(mod.Globals) != 1 {
				t.Fatalf("expected 1 global, got %d", len(mod.Globals))
			}
			g := mod.Globals[0]
			if g.Type.ValType != tt.valType {
				t.Errorf("val type = %d, want %d", g.Type.ValType, tt.valType)
			}
			if g.Type.Mutable != tt.mutable {
				t.Errorf("mutable = %v, want %v", g.Type.Mutable, tt.mutable)
			}
		})
	}
}

func TestParseData(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		passive bool
	}{
		{"active", `(module (memory 1) (data (i32.const 0) "hello"))`, false},
		{"passive", `(module (memory 1) (data "hello"))`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			mod, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(mod.Data) != 1 {
				t.Fatalf("expected 1 data, got %d", len(mod.Data))
			}
			if mod.Data[0].Passive != tt.passive {
				t.Errorf("passive = %v, want %v", mod.Data[0].Passive, tt.passive)
			}
		})
	}
}

func TestParseElem(t *testing.T) {
	tokens := token.Tokenize("(module (table 10 funcref) (func $f) (elem (i32.const 0) $f))")
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Elems) != 1 {
		t.Fatalf("expected 1 elem, got %d", len(mod.Elems))
	}
	if len(mod.Elems[0].Init) != 1 {
		t.Errorf("expected 1 init, got %d", len(mod.Elems[0].Init))
	}
}

func TestParseStart(t *testing.T) {
	tokens := token.Tokenize("(module (func $main) (start $main))")
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if mod.Start == nil {
		t.Fatal("start is nil")
	}
	if *mod.Start != 0 {
		t.Errorf("start = %d, want 0", *mod.Start)
	}
}

func TestParseValType(t *testing.T) {
	tests := []struct {
		input   string
		valType ast.ValType
	}{
		{"i32", ast.ValTypeI32},
		{"i64", ast.ValTypeI64},
		{"f32", ast.ValTypeF32},
		{"f64", ast.ValTypeF64},
		{"funcref", ast.ValTypeFuncref},
		{"externref", ast.ValTypeExternref},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			vt, err := p.parseValType()
			if err != nil {
				t.Fatalf("parseValType failed: %v", err)
			}
			if vt != tt.valType {
				t.Errorf("got %d, want %d", vt, tt.valType)
			}
		})
	}
}

func TestParseValTypeError(t *testing.T) {
	tokens := token.Tokenize("invalid")
	p := New(tokens)
	_, err := p.parseValType()
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestParseU32(t *testing.T) {
	tests := []struct {
		input string
		want  uint32
	}{
		{"0", 0},
		{"42", 42},
		{"0xFF", 255},
		{"1_000", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			got, err := p.parseU32()
			if err != nil {
				t.Fatalf("parseU32 failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseF32(t *testing.T) {
	tokens := token.Tokenize("3.14")
	p := New(tokens)
	got, err := p.parseF32()
	if err != nil {
		t.Fatalf("parseF32 failed: %v", err)
	}
	if got < 3.13 || got > 3.15 {
		t.Errorf("got %f, want ~3.14", got)
	}
}

func TestParseF64(t *testing.T) {
	tokens := token.Tokenize("3.14159265359")
	p := New(tokens)
	got, err := p.parseF64()
	if err != nil {
		t.Fatalf("parseF64 failed: %v", err)
	}
	if got < 3.14159 || got > 3.14160 {
		t.Errorf("got %f, want ~3.14159", got)
	}
}

func TestDecodeStringLiteral(t *testing.T) {
	tests := []struct {
		input string
		want  []byte
	}{
		{"hello", []byte("hello")},
		{`hello\nworld`, []byte("hello\nworld")},
		{`\t\r\\\"`, []byte("\t\r\\\"")},
		{`\00`, []byte{0}},
		{`\41\42\43`, []byte("ABC")},
		{`\u{0041}`, []byte("A")},
		{`\u{1F600}`, []byte{0xF0, 0x9F, 0x98, 0x80}}, // grinning face emoji
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := DecodeStringLiteral(tt.input)
			if string(got) != string(tt.want) {
				t.Errorf("DecodeStringLiteral(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLabelResolution(t *testing.T) {
	p := New(nil)

	p.pushLabel("$outer")
	p.pushLabel("$inner")

	depth, ok := p.resolveLabel("$inner")
	if !ok || depth != 0 {
		t.Errorf("$inner depth = %d, want 0", depth)
	}

	depth, ok = p.resolveLabel("$outer")
	if !ok || depth != 1 {
		t.Errorf("$outer depth = %d, want 1", depth)
	}

	_, ok = p.resolveLabel("$unknown")
	if ok {
		t.Error("$unknown should not be found")
	}

	p.popLabel()
	depth, ok = p.resolveLabel("$outer")
	if !ok || depth != 0 {
		t.Errorf("after pop, $outer depth = %d, want 0", depth)
	}
}

func TestFindOrAddType(t *testing.T) {
	tokens := token.Tokenize("(module)")
	p := New(tokens)
	p.mod = &ast.Module{}

	// Add first type
	ft1 := ast.FuncType{Params: []ast.ValType{ast.ValTypeI32}}
	idx1 := p.findOrAddType(ft1)
	if idx1 != 0 {
		t.Errorf("first type index = %d, want 0", idx1)
	}

	// Add same type again - should return same index
	idx2 := p.findOrAddType(ft1)
	if idx2 != 0 {
		t.Errorf("same type index = %d, want 0", idx2)
	}

	// Add different type
	ft2 := ast.FuncType{Params: []ast.ValType{ast.ValTypeI64}}
	idx3 := p.findOrAddType(ft2)
	if idx3 != 1 {
		t.Errorf("different type index = %d, want 1", idx3)
	}

	if len(p.mod.Types) != 2 {
		t.Errorf("type count = %d, want 2", len(p.mod.Types))
	}
}

// Extended parser tests covering additional instruction and section types

func TestParseElem_DeclareFunc(t *testing.T) {
	input := "(module (table 10 funcref) (func $f) (elem declare func $f))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Elems) != 1 {
		t.Fatalf("expected 1 elem, got %d", len(mod.Elems))
	}
	if mod.Elems[0].Mode != ast.ElemModeDeclarative {
		t.Errorf("expected declarative mode, got %d", mod.Elems[0].Mode)
	}
}

func TestParseElem_PassiveWithRefNull(t *testing.T) {
	input := "(module (table 10 funcref) (elem funcref (ref.null func)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Elems) != 1 {
		t.Fatalf("expected 1 elem, got %d", len(mod.Elems))
	}
	if mod.Elems[0].Mode != ast.ElemModePassive {
		t.Errorf("expected passive mode, got %d", mod.Elems[0].Mode)
	}
}

func TestParseBlock(t *testing.T) {
	input := "(module (func (block (nop))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseBlockWithLabel(t *testing.T) {
	input := "(module (func (block $myblock (br $myblock))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseLoop(t *testing.T) {
	input := "(module (func (loop $l (br $l))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseIfElse(t *testing.T) {
	input := "(module (func (param i32) (result i32) (if (result i32) (local.get 0) (then (i32.const 1)) (else (i32.const 0)))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseIfWithoutElse(t *testing.T) {
	input := "(module (func (param i32) (if (local.get 0) (then (nop)))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseBrTable(t *testing.T) {
	input := "(module (func (param i32) (block $a (block $b (br_table 0 1 (local.get 0))))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseBrTableWithLabels(t *testing.T) {
	input := "(module (func (param i32) (block $a (block $b (br_table $b $a $a (local.get 0))))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseCallIndirect(t *testing.T) {
	input := "(module (table 1 funcref) (type $t (func)) (func (call_indirect (type $t) (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseSelectTyped(t *testing.T) {
	input := "(module (func (result i32) (select (result i32) (i32.const 1) (i32.const 2) (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseRefFunc(t *testing.T) {
	input := "(module (func $f) (elem declare func $f) (func (result funcref) (ref.func $f)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseRefNull(t *testing.T) {
	input := "(module (func (result funcref) (ref.null func)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseRefIsNull(t *testing.T) {
	input := "(module (func (result i32) (ref.is_null (ref.null func))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTableGet(t *testing.T) {
	input := "(module (table 1 funcref) (func (result funcref) (table.get 0 (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTableSet(t *testing.T) {
	input := "(module (table 1 funcref) (func (param funcref) (table.set 0 (i32.const 0) (local.get 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTableGrow(t *testing.T) {
	input := "(module (table 1 funcref) (func (result i32) (table.grow 0 (ref.null func) (i32.const 1))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTableSize(t *testing.T) {
	input := "(module (table 1 funcref) (func (result i32) (table.size 0)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTableFill(t *testing.T) {
	input := "(module (table 10 funcref) (func (table.fill 0 (i32.const 0) (ref.null func) (i32.const 5))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTableCopy(t *testing.T) {
	input := "(module (table 10 funcref) (func (table.copy 0 0 (i32.const 0) (i32.const 5) (i32.const 2))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTableInit(t *testing.T) {
	input := "(module (table 10 funcref) (func $f) (elem $e func $f) (func (table.init $e (i32.const 0) (i32.const 0) (i32.const 1))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseElemDrop(t *testing.T) {
	input := "(module (table 1 funcref) (func $f) (elem $e func $f) (func (elem.drop $e)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseMemorySize(t *testing.T) {
	input := "(module (memory 1) (func (result i32) (memory.size)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseMemoryGrow(t *testing.T) {
	input := "(module (memory 1) (func (result i32) (memory.grow (i32.const 1))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseMemoryCopy(t *testing.T) {
	input := "(module (memory 1) (func (memory.copy (i32.const 0) (i32.const 10) (i32.const 5))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseMemoryFill(t *testing.T) {
	input := "(module (memory 1) (func (memory.fill (i32.const 0) (i32.const 0xFF) (i32.const 10))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseDataDrop(t *testing.T) {
	input := `(module (memory 1) (data $d "hello") (func (data.drop $d)))`
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseMemoryInit(t *testing.T) {
	input := `(module (memory 1) (data $d "hello") (func (memory.init $d (i32.const 0) (i32.const 0) (i32.const 5))))`
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseLocalTee(t *testing.T) {
	input := "(module (func (local i32) (drop (local.tee 0 (i32.const 1)))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseGlobalGetSet(t *testing.T) {
	input := "(module (global $g (mut i32) (i32.const 0)) (func (global.set $g (i32.add (global.get $g) (i32.const 1)))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseLoadStore(t *testing.T) {
	input := "(module (memory 1) (func (i32.store (i32.const 0) (i32.load (i32.const 0)))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseLoadWithOffset(t *testing.T) {
	input := "(module (memory 1) (func (result i32) (i32.load offset=4 (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseLoadWithAlign(t *testing.T) {
	input := "(module (memory 1) (func (result i32) (i32.load align=2 (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseLoadWithOffsetAndAlign(t *testing.T) {
	input := "(module (memory 1) (func (result i32) (i32.load offset=4 align=4 (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseSignedLoads(t *testing.T) {
	tests := []string{
		"(module (memory 1) (func (result i32) (i32.load8_s (i32.const 0))))",
		"(module (memory 1) (func (result i32) (i32.load8_u (i32.const 0))))",
		"(module (memory 1) (func (result i32) (i32.load16_s (i32.const 0))))",
		"(module (memory 1) (func (result i32) (i32.load16_u (i32.const 0))))",
		"(module (memory 1) (func (result i64) (i64.load8_s (i32.const 0))))",
		"(module (memory 1) (func (result i64) (i64.load32_s (i32.const 0))))",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			tokens := token.Tokenize(input)
			p := New(tokens)
			_, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
		})
	}
}

func TestParseTruncSat(t *testing.T) {
	tests := []string{
		"(module (func (result i32) (i32.trunc_sat_f32_s (f32.const 0))))",
		"(module (func (result i32) (i32.trunc_sat_f32_u (f32.const 0))))",
		"(module (func (result i32) (i32.trunc_sat_f64_s (f64.const 0))))",
		"(module (func (result i32) (i32.trunc_sat_f64_u (f64.const 0))))",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			tokens := token.Tokenize(input)
			p := New(tokens)
			_, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
		})
	}
}

func TestParseF32Const(t *testing.T) {
	tests := []string{
		"(module (func (result f32) (f32.const 3.14)))",
		"(module (func (result f32) (f32.const -1.0)))",
		"(module (func (result f32) (f32.const 0x1.8p+0)))",
		"(module (func (result f32) (f32.const inf)))",
		"(module (func (result f32) (f32.const -inf)))",
		"(module (func (result f32) (f32.const nan)))",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			tokens := token.Tokenize(input)
			p := New(tokens)
			_, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
		})
	}
}

func TestParseF64Const(t *testing.T) {
	tests := []string{
		"(module (func (result f64) (f64.const 3.14159)))",
		"(module (func (result f64) (f64.const -1.0)))",
		"(module (func (result f64) (f64.const 0x1.8p+0)))",
		"(module (func (result f64) (f64.const inf)))",
		"(module (func (result f64) (f64.const -inf)))",
		"(module (func (result f64) (f64.const nan)))",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			tokens := token.Tokenize(input)
			p := New(tokens)
			_, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
		})
	}
}

func TestParseMultipleFuncs(t *testing.T) {
	input := `(module
		(func $add (param i32 i32) (result i32)
			(i32.add (local.get 0) (local.get 1)))
		(func $sub (param i32 i32) (result i32)
			(i32.sub (local.get 0) (local.get 1)))
		(func $main (result i32)
			(call $add (call $sub (i32.const 10) (i32.const 3)) (i32.const 5)))
	)`
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Funcs) != 3 {
		t.Errorf("expected 3 funcs, got %d", len(mod.Funcs))
	}
}

func TestParseDataWithMemoryIndex(t *testing.T) {
	input := `(module (memory 1) (data (memory 0) (i32.const 0) "hello"))`
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Data) != 1 {
		t.Fatalf("expected 1 data, got %d", len(mod.Data))
	}
}

func TestParseInlineExport(t *testing.T) {
	input := `(module (func (export "test")))`
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Exports) != 1 {
		t.Fatalf("expected 1 export, got %d", len(mod.Exports))
	}
	if mod.Exports[0].Name != "test" {
		t.Errorf("export name = %q, want %q", mod.Exports[0].Name, "test")
	}
}

func TestParseInlineImport(t *testing.T) {
	input := `(module (func (import "env" "log") (param i32)))`
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Module != "env" {
		t.Errorf("import module = %q, want %q", mod.Imports[0].Module, "env")
	}
}

func TestParseMultipleMemories(t *testing.T) {
	input := "(module (memory $m1 1) (memory $m2 2))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Memories) != 2 {
		t.Errorf("expected 2 memories, got %d", len(mod.Memories))
	}
}

func TestParseMultipleTables(t *testing.T) {
	input := "(module (table $t1 10 funcref) (table $t2 20 externref))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(mod.Tables))
	}
}

func TestParseExternref(t *testing.T) {
	input := "(module (func (param externref) (result externref) (local.get 0)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseDrop(t *testing.T) {
	input := "(module (func (drop (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseUnreachable(t *testing.T) {
	input := "(module (func (unreachable)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseReturn(t *testing.T) {
	input := "(module (func (result i32) (return (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseNumericOps(t *testing.T) {
	ops := []string{
		"i32.clz", "i32.ctz", "i32.popcnt",
		"i32.add", "i32.sub", "i32.mul", "i32.div_s", "i32.div_u",
		"i32.rem_s", "i32.rem_u", "i32.and", "i32.or", "i32.xor",
		"i32.shl", "i32.shr_s", "i32.shr_u", "i32.rotl", "i32.rotr",
		"i32.eq", "i32.ne", "i32.lt_s", "i32.lt_u", "i32.gt_s", "i32.gt_u",
		"i32.le_s", "i32.le_u", "i32.ge_s", "i32.ge_u", "i32.eqz",
	}

	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			var input string
			if op == "i32.eqz" || op == "i32.clz" || op == "i32.ctz" || op == "i32.popcnt" {
				input = "(module (func (result i32) (" + op + " (i32.const 1))))"
			} else {
				input = "(module (func (result i32) (" + op + " (i32.const 1) (i32.const 2))))"
			}
			tokens := token.Tokenize(input)
			p := New(tokens)
			_, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse %s failed: %v", op, err)
			}
		})
	}
}

func TestParseF32Ops(t *testing.T) {
	ops := []string{
		"f32.abs", "f32.neg", "f32.ceil", "f32.floor", "f32.trunc",
		"f32.nearest", "f32.sqrt",
		"f32.add", "f32.sub", "f32.mul", "f32.div",
		"f32.min", "f32.max", "f32.copysign",
		"f32.eq", "f32.ne", "f32.lt", "f32.gt", "f32.le", "f32.ge",
	}

	unary := map[string]bool{
		"f32.abs": true, "f32.neg": true, "f32.ceil": true,
		"f32.floor": true, "f32.trunc": true, "f32.nearest": true,
		"f32.sqrt": true,
	}

	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			var input string
			if unary[op] {
				input = "(module (func (result f32) (" + op + " (f32.const 1.0))))"
			} else if op == "f32.eq" || op == "f32.ne" || op == "f32.lt" || op == "f32.gt" || op == "f32.le" || op == "f32.ge" {
				input = "(module (func (result i32) (" + op + " (f32.const 1.0) (f32.const 2.0))))"
			} else {
				input = "(module (func (result f32) (" + op + " (f32.const 1.0) (f32.const 2.0))))"
			}
			tokens := token.Tokenize(input)
			p := New(tokens)
			_, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse %s failed: %v", op, err)
			}
		})
	}
}

func TestParseConversions(t *testing.T) {
	tests := []string{
		"(module (func (result i64) (i64.extend_i32_s (i32.const 0))))",
		"(module (func (result i64) (i64.extend_i32_u (i32.const 0))))",
		"(module (func (result i32) (i32.wrap_i64 (i64.const 0))))",
		"(module (func (result f32) (f32.convert_i32_s (i32.const 0))))",
		"(module (func (result f32) (f32.convert_i32_u (i32.const 0))))",
		"(module (func (result f64) (f64.promote_f32 (f32.const 0))))",
		"(module (func (result f32) (f32.demote_f64 (f64.const 0))))",
		"(module (func (result i32) (i32.reinterpret_f32 (f32.const 0))))",
		"(module (func (result f32) (f32.reinterpret_i32 (i32.const 0))))",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			tokens := token.Tokenize(input)
			p := New(tokens)
			_, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
		})
	}
}

func TestParseNestedBlocks(t *testing.T) {
	input := `(module (func
		(block $outer
			(block $inner
				(br_if $inner (i32.const 0))
				(br $outer)
			)
		)
	))`
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseNamedLocals(t *testing.T) {
	input := "(module (func (param $x i32) (local $y i32) (local.set $y (local.get $x))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}

func TestParseTypeUse(t *testing.T) {
	input := "(module (type $sig (func (param i32) (result i32))) (func (type $sig) (local.get 0)))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(mod.Types) != 1 {
		t.Errorf("expected 1 type, got %d", len(mod.Types))
	}
}

func TestParseExportKinds(t *testing.T) {
	tests := []struct {
		input string
		kind  byte
	}{
		{`(module (func $f) (export "f" (func $f)))`, ast.KindFunc},
		{`(module (memory 1) (export "mem" (memory 0)))`, ast.KindMemory},
		{`(module (table 1 funcref) (export "tab" (table 0)))`, ast.KindTable},
		{`(module (global i32 (i32.const 0)) (export "g" (global 0)))`, ast.KindGlobal},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens := token.Tokenize(tt.input)
			p := New(tokens)
			mod, err := p.Parse()
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if len(mod.Exports) != 1 {
				t.Fatalf("expected 1 export, got %d", len(mod.Exports))
			}
			if mod.Exports[0].Kind != tt.kind {
				t.Errorf("kind = %d, want %d", mod.Exports[0].Kind, tt.kind)
			}
		})
	}
}

func TestParseEmptyBlockWithResult(t *testing.T) {
	input := "(module (func (result i32) (block (result i32) (i32.const 0))))"
	tokens := token.Tokenize(input)
	p := New(tokens)
	_, err := p.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
}
