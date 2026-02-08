package wat

import (
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// Integration tests for the public Compile() API.
// Unit tests are in internal packages.

func TestCompile(t *testing.T) {
	t.Run("empty_module", func(t *testing.T) {
		wasm, err := Compile("(module)")
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		if len(wasm) != 8 {
			t.Errorf("expected 8 bytes, got %d", len(wasm))
		}
		// Check magic number
		if wasm[0] != 0x00 || wasm[1] != 0x61 || wasm[2] != 0x73 || wasm[3] != 0x6D {
			t.Error("invalid WASM magic")
		}
	})

	t.Run("simple_function", func(t *testing.T) {
		wasm, err := Compile(`(module
			(func (export "add") (param i32 i32) (result i32)
				(i32.add (local.get 0) (local.get 1))))`)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		if len(wasm) < 20 {
			t.Errorf("output too small: %d bytes", len(wasm))
		}
	})
}

func TestCompileErrors(t *testing.T) {
	tests := []struct {
		name, wat, wantErr string
	}{
		{"missing_module", "(func)", "expected 'module'"},
		{"unclosed", "(module", "unexpected end"},
		{"unknown_instr", "(module (func (bogus)))", "unknown instruction"},
		{"unknown_type", "(module (func (param bogus)))", "unknown value type"},
		{"unknown_label", "(module (func (block (br $x))))", "unknown label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(tt.wat)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q missing %q", err, tt.wantErr)
			}
		})
	}
}

// TestWasmValidation validates compiled output by parsing it back.
func TestWasmValidation(t *testing.T) {
	tests := []struct {
		name string
		wat  string
	}{
		// Module structure
		{"memory", "(module (memory 1 10))"},
		{"table", "(module (table 10 funcref))"},
		{"global", "(module (global (mut i32) (i32.const 0)))"},
		{"start", "(module (func $main) (start $main))"},
		{"multi_memory", "(module (memory $m0 1) (memory $m1 1))"},

		// Functions
		{"func_params", "(module (func (param i32 i64 f32 f64)))"},
		{"func_results", "(module (func (result i32 i32) (i32.const 1) (i32.const 2)))"},
		{"func_locals", "(module (func (local i32) (local.set 0 (i32.const 1))))"},
		{"func_mixed_params", "(module (func (param i32 i64 f32 f64 funcref externref)))"},

		// Imports/exports
		{"import_func", "(module (import \"m\" \"f\" (func)))"},
		{"import_memory", "(module (import \"m\" \"m\" (memory 1)))"},
		{"import_table", "(module (import \"m\" \"t\" (table 1 funcref)))"},
		{"import_global", "(module (import \"m\" \"g\" (global i32)))"},
		{"export_func", "(module (func $f) (export \"f\" (func $f)))"},
		{"inline_export", "(module (func (export \"f\")))"},

		// Control flow
		{"block", "(module (func (result i32) (block (result i32) (i32.const 1))))"},
		{"loop", "(module (func (loop $l (br $l))))"},
		{"if_else", "(module (func (result i32) (if (result i32) (i32.const 1) (then (i32.const 2)) (else (i32.const 3)))))"},
		{"br_table", "(module (func (param i32) (block $a (block $b (br_table $a $b (local.get 0))))))"},
		{"nested_blocks", "(module (func (block (block (block (nop))))))"},

		// Flat form
		{"flat_block", "(module (func block nop end))"},
		{"flat_if_else", "(module (func i32.const 1 if nop else nop end))"},
		{"flat_loop", "(module (func loop $l br $l end))"},

		// Calls
		{"call", "(module (func $f) (func (call $f)))"},
		{"call_indirect", "(module (type $t (func)) (table 1 funcref) (func (call_indirect (type $t) (i32.const 0))))"},
		{"return_call", "(module (func $f (return_call $f)))"},
		{"return_call_indirect", "(module (type $t (func)) (table 1 funcref) (func (return_call_indirect (type $t) (i32.const 0))))"},

		// Memory ops
		{"load_store", "(module (memory 1) (func (i32.store (i32.const 0) (i32.const 42))))"},
		{"memory_grow", "(module (memory 1) (func (result i32) (memory.grow (i32.const 1))))"},
		{"memory_fill", "(module (memory 1) (func (memory.fill (i32.const 0) (i32.const 0) (i32.const 10))))"},
		{"memory_copy", "(module (memory 1) (func (memory.copy (i32.const 0) (i32.const 10) (i32.const 5))))"},
		{"memory_init", "(module (memory 1) (data $d \"hello\") (func (memory.init $d (i32.const 0) (i32.const 0) (i32.const 5))))"},
		{"data_drop", "(module (memory 1) (data $d \"hello\") (func (data.drop $d)))"},
		{"load_offset_align", "(module (memory 1) (func (result i32) (i32.load offset=4 align=4 (i32.const 0))))"},

		// Table ops
		{"table_get", "(module (table 1 funcref) (func (result funcref) (table.get (i32.const 0))))"},
		{"table_set", "(module (table 1 funcref) (func (table.set (i32.const 0) (ref.null func))))"},
		{"table_grow", "(module (table 1 funcref) (func (result i32) (table.grow (ref.null func) (i32.const 1))))"},
		{"table_size", "(module (table 1 funcref) (func (result i32) (table.size)))"},
		{"table_fill", "(module (table 10 funcref) (func (table.fill (i32.const 0) (ref.null func) (i32.const 5))))"},
		{"table_init", "(module (table 10 funcref) (func $f) (elem $e func $f) (func (table.init $e (i32.const 0) (i32.const 0) (i32.const 1))))"},
		{"table_copy", "(module (table 10 funcref) (func (table.copy (i32.const 0) (i32.const 5) (i32.const 3))))"},
		{"elem_drop", "(module (func $f) (elem $e func $f) (func (elem.drop $e)))"},

		// Reference types
		{"ref_null_func", "(module (func (result funcref) (ref.null func)))"},
		{"ref_null_extern", "(module (func (result externref) (ref.null extern)))"},
		{"ref_is_null", "(module (func (param funcref) (result i32) (ref.is_null (local.get 0))))"},
		{"ref_func", "(module (func $f) (elem declare func $f) (func (result funcref) (ref.func $f)))"},

		// Select
		{"select", "(module (func (result i32) (select (i32.const 1) (i32.const 2) (i32.const 1))))"},
		{"select_typed", "(module (func (result i32) (select (result i32) (i32.const 1) (i32.const 2) (i32.const 1))))"},

		// Data/elem
		{"data_active", "(module (memory 1) (data (i32.const 0) \"hello\"))"},
		{"data_passive", "(module (memory 1) (data \"hello\"))"},
		{"elem_active", "(module (table 1 funcref) (func $f) (elem (i32.const 0) $f))"},
		{"elem_declare", "(module (func $f) (elem declare func $f))"},
		{"elem_expr", "(module (table 1 funcref) (elem (i32.const 0) funcref (ref.null func)))"},

		// Inline syntax
		{"inline_memory", "(module (memory (data \"test\")))"},
		{"inline_table", "(module (func $f) (table funcref (elem $f)))"},

		// Saturating truncation
		{"trunc_sat_f32_s", "(module (func (result i32) (i32.trunc_sat_f32_s (f32.const 1.5))))"},
		{"trunc_sat_f64_u", "(module (func (result i64) (i64.trunc_sat_f64_u (f64.const 1.5))))"},

		// Sign extension
		{"extend8_s", "(module (func (result i32) (i32.extend8_s (i32.const 255))))"},
		{"extend16_s", "(module (func (result i32) (i32.extend16_s (i32.const 65535))))"},
		{"i64_extend32_s", "(module (func (result i64) (i64.extend32_s (i64.const 0xFFFFFFFF))))"},

		// Numeric edge cases
		{"i32_max", "(module (func (drop (i32.const 2147483647))))"},
		{"i32_min", "(module (func (drop (i32.const -2147483648))))"},
		{"i64_min", "(module (func (drop (i64.const -9223372036854775808))))"},
		{"hex_numbers", "(module (func (drop (i32.const 0xFFFF_FFFF))))"},
		{"f32_nan", "(module (func (drop (f32.const nan))))"},
		{"f64_inf", "(module (func (drop (f64.const inf))))"},
		{"hex_float", "(module (func (drop (f32.const 0x1.0p0))))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, err := Compile(tt.wat)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if _, err := wasm.ParseModule(bin); err != nil {
				t.Errorf("ParseModule: %v", err)
			}
		})
	}
}
