package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestGlobalSimulationRejectsInvalidMetadata(t *testing.T) {
	for _, module := range []*wasm.Module{nil, {}, {Globals: []wasm.Global{{Type: wasm.GlobalType{ValType: wasm.ValI64}}}}} {
		ft := NewFunctionTransformer(DefaultRegistry(), module, GlobalIndices{}, 0, false)
		allocator := NewTempAllocator(1)
		allocator.SetCurrentInstr(0)
		stack := []stackEntry{semantics.StoredOperand(0, wasm.ValI64)}
		ft.simulateInstrStack(&stack, wasm.Instruction{Opcode: wasm.OpGlobalSet, Imm: wasm.GlobalImm{}}, allocator, nil)
		if allocator.Err() == nil {
			t.Fatal("invalid global write accepted")
		}
		if len(stack) != 1 {
			t.Fatal("rejection mutated operand stack")
		}
	}
}

func TestGlobalSimulationUsesImportedType(t *testing.T) {
	module := &wasm.Module{Imports: []wasm.Import{{Desc: wasm.ImportDesc{Kind: wasm.KindGlobal, Global: &wasm.GlobalType{ValType: wasm.ValF64}}}}}
	ft := NewFunctionTransformer(DefaultRegistry(), module, GlobalIndices{}, 0, false)
	allocator := NewTempAllocator(1)
	allocator.SetCurrentInstr(0)
	var stack []stackEntry
	ft.simulateInstrStack(&stack, wasm.Instruction{Opcode: wasm.OpGlobalGet, Imm: wasm.GlobalImm{}}, allocator, nil)
	if allocator.Err() != nil || len(stack) != 1 || stack[0].Type() != wasm.ValF64 {
		t.Fatalf("incorrect global snapshot: %+v, %v", stack, allocator.Err())
	}
}
