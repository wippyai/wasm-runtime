package wasm_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestCloneInstructionOwnsMutableImmediates(t *testing.T) {
	lane := byte(1)
	for _, tc := range []struct {
		immediate any
		mutate    func(any)
		name      string
	}{
		{name: "branch-table", immediate: wasm.BrTableImm{Labels: []uint32{1}}, mutate: func(imm any) { imm.(wasm.BrTableImm).Labels[0] = 9 }},
		{name: "misc", immediate: wasm.MiscImm{Operands: []uint32{1}}, mutate: func(imm any) { imm.(wasm.MiscImm).Operands[0] = 9 }},
		{name: "select", immediate: wasm.SelectTypeImm{Types: []wasm.ValType{wasm.ValI32}, ExtTypes: []wasm.ExtValType{{ValType: wasm.ValI32}}}, mutate: func(imm any) {
			s := imm.(wasm.SelectTypeImm)
			s.Types[0] = wasm.ValI64
			s.ExtTypes[0].ValType = wasm.ValI64
		}},
		{name: "simd", immediate: wasm.SIMDImm{MemArg: &wasm.MemoryImm{Offset: 1}, LaneIdx: &lane, V128Bytes: []byte{1}}, mutate: func(imm any) { s := imm.(wasm.SIMDImm); s.MemArg.Offset = 9; *s.LaneIdx = 9; s.V128Bytes[0] = 9 }},
		{name: "atomic", immediate: wasm.AtomicImm{MemArg: &wasm.MemoryImm{Offset: 1}}, mutate: func(imm any) { imm.(wasm.AtomicImm).MemArg.Offset = 9 }},
		{name: "try-table", immediate: wasm.TryTableImm{Catches: []wasm.CatchClause{{LabelIdx: 1}}}, mutate: func(imm any) { imm.(wasm.TryTableImm).Catches[0].LabelIdx = 9 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := wasm.Instruction{Opcode: 42, Synthetic: true, Imm: tc.immediate}
			before, err := json.Marshal(source)
			if err != nil {
				t.Fatal(err)
			}
			clone, err := wasm.CloneInstruction(source)
			if err != nil {
				t.Fatal(err)
			}
			copied, err := json.Marshal(clone)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, copied) {
				t.Fatal("clone changed instruction")
			}
			tc.mutate(clone.Imm)
			after, err := json.Marshal(source)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("clone mutation changed source")
			}
		})
	}
	if _, err := wasm.CloneInstruction(wasm.Instruction{Imm: &lane}); err == nil {
		t.Fatal("unknown immediate did not require ownership contract")
	}
}
