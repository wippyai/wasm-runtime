package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestInputIdentitiesVisitAllExpressionSites(t *testing.T) {
	invalidGlobal := wasm.EncodeInstructions([]wasm.Instruction{
		{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{GlobalIdx: 9}},
		{Opcode: wasm.OpEnd},
	})
	for _, tc := range []struct {
		place func(*wasm.Module, []byte)
		name  string
	}{
		{name: "func", place: func(m *wasm.Module, code []byte) {
			m.Types = []wasm.FuncType{{}}
			m.Funcs = []uint32{0}
			m.Code = []wasm.FuncBody{{Code: code}}
		}},
		{name: "global", place: func(m *wasm.Module, code []byte) {
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{ValType: wasm.ValI32}, Init: code}}
		}},
		{name: "table_initializer", place: func(m *wasm.Module, code []byte) {
			m.Tables = []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Init: code}}
		}},
		{name: "element_offset", place: func(m *wasm.Module, code []byte) {
			m.Tables = []wasm.TableType{{ElemType: byte(wasm.ValFuncRef)}}
			m.Elements = []wasm.Element{{Offset: code}}
		}},
		{name: "element_initializer", place: func(m *wasm.Module, code []byte) {
			m.Elements = []wasm.Element{{Flags: 5, Type: wasm.ValFuncRef, Exprs: [][]byte{code}}}
		}},
		{name: "data_offset", place: func(m *wasm.Module, code []byte) {
			m.Memories = []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}}
			m.Data = []wasm.DataSegment{{Offset: code}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &wasm.Module{}
			tc.place(m, invalidGlobal)
			before := m.Encode()
			err := validateInputIdentities(m)
			if err == nil || !strings.Contains(err.Error(), "global index 9 out of range") {
				t.Fatalf("missed expression identity: %v", err)
			}
			if !bytes.Equal(before, m.Encode()) {
				t.Fatal("preflight mutated module")
			}
		})
	}
}

func TestInputIdentitiesGlobalImportOverflow(t *testing.T) {
	source := `(module (import "env" "g" (global i32)) (func (result i32) global.get 4294967295))`
	raw, err := wat.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range []bool{false, true} {
		_, err := New(Config{ImportGlobals: imported}).Transform(raw)
		if err == nil || !strings.Contains(err.Error(), "global index 4294967295 out of range") {
			t.Fatalf("importGlobals=%v: %v", imported, err)
		}
	}
}

func TestInputIdentitiesFunctionExpressions(t *testing.T) {
	for _, instr := range []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 9}},
		{Opcode: wasm.OpReturnCall, Imm: wasm.CallImm{FuncIdx: 9}},
		{Opcode: wasm.OpRefFunc, Imm: wasm.RefFuncImm{FuncIdx: 9}},
	} {
		m := &wasm.Module{Types: []wasm.FuncType{{}}, Funcs: []uint32{0}, Code: []wasm.FuncBody{{Code: wasm.EncodeInstructions([]wasm.Instruction{instr, {Opcode: wasm.OpEnd}})}}}
		err := validateInputIdentities(m)
		if err == nil || !strings.Contains(err.Error(), "function index 9 out of range") {
			t.Fatalf("opcode %x: %v", instr.Opcode, err)
		}
	}
	// ref.func in element expressions must not escape the instruction walker.
	m := &wasm.Module{Elements: []wasm.Element{{Flags: 5, Type: wasm.ValFuncRef, Exprs: [][]byte{wasm.EncodeInstructions([]wasm.Instruction{
		{Opcode: wasm.OpRefFunc, Imm: wasm.RefFuncImm{FuncIdx: 9}}, {Opcode: wasm.OpEnd},
	})}}}}
	if err := validateInputIdentities(m); err == nil || !strings.Contains(err.Error(), "function index 9 out of range") {
		t.Fatalf("element ref.func: %v", err)
	}
}

func TestInputIdentitiesValidImportedAndDefined(t *testing.T) {
	raw, err := wat.Compile(`(module
  (import "env" "yield" (func $yield))
  (import "env" "g" (global $g i64))
  (global $local (mut i64) (i64.const 0))
  (func (export "run") global.get $g global.set $local call $yield))`)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	before := m.Encode()
	if err := validateInputIdentities(m); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, m.Encode()) {
		t.Fatal("successful preflight mutated module")
	}
}
