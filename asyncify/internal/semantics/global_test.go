package semantics

import (
	"math"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestResolveGlobalIndexSpace(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{
			{Desc: wasm.ImportDesc{Kind: wasm.KindFunc}},
			{Desc: wasm.ImportDesc{Kind: wasm.KindGlobal, Global: &wasm.GlobalType{ValType: wasm.ValI64, Mutable: true}}},
			{Desc: wasm.ImportDesc{Kind: wasm.KindFunc}},
		},
		Globals: []wasm.Global{{Type: wasm.GlobalType{ValType: wasm.ValF64}}},
	}
	for _, tc := range []struct {
		index   uint32
		opcode  byte
		typ     wasm.ValType
		invalid bool
	}{
		{0, wasm.OpGlobalGet, wasm.ValI64, false},
		{0, wasm.OpGlobalSet, wasm.ValI64, false},
		{1, wasm.OpGlobalGet, wasm.ValF64, false},
		{1, wasm.OpGlobalSet, 0, true},
		{2, wasm.OpGlobalGet, 0, true},
		{math.MaxUint32, wasm.OpGlobalGet, 0, true},
	} {
		op, handled, err := ResolveGlobal(wasm.Instruction{Opcode: tc.opcode, Imm: wasm.GlobalImm{GlobalIdx: tc.index}}, m)
		if !handled || (err != nil) != tc.invalid {
			t.Fatalf("index %d opcode %x: handled=%v err=%v", tc.index, tc.opcode, handled, err)
		}
		if !tc.invalid && (op.Index != tc.index || op.Type != tc.typ || op.Write != (tc.opcode == wasm.OpGlobalSet)) {
			t.Fatalf("wrong resolved operation: %+v", op)
		}
	}
	op, _, err := ResolveGlobal(wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{}}, m)
	if err != nil {
		t.Fatal(err)
	}
	m.Imports[1].Desc.Global.ValType = wasm.ValI32
	if op.Type != wasm.ValI64 {
		t.Fatal("resolved operation borrowed mutable metadata")
	}
}

func TestResolveGlobalMalformedAndUnrelated(t *testing.T) {
	for _, opcode := range []byte{wasm.OpGlobalGet, wasm.OpGlobalSet} {
		for _, imm := range []wasm.Instruction{
			{Opcode: opcode},
			{Opcode: opcode, Imm: wasm.LocalImm{}},
			{Opcode: opcode, Imm: wasm.GlobalImm{}},
		} {
			if _, handled, err := ResolveGlobal(imm, nil); !handled || err == nil {
				t.Fatalf("malformed instruction accepted: %+v", imm)
			}
		}
	}
	if _, handled, err := ResolveGlobal(wasm.Instruction{Opcode: wasm.OpNop}, nil); handled || err != nil {
		t.Fatal("unrelated opcode classified as global")
	}
}
