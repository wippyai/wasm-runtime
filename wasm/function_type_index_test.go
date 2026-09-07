package wasm_test

import (
	"math"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestFunctionTypeIndexFlatTraversal(t *testing.T) {
	first := &wasm.FuncType{Params: []wasm.ValType{wasm.ValI64}}
	last := &wasm.FuncType{Results: []wasm.ValType{wasm.ValF64}}
	m := &wasm.Module{
		Types: []wasm.FuncType{*first, *last}, // Compatibility view excludes the struct.
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{Types: []wasm.SubType{
				{CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &wasm.StructType{}}},
				{CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: first}},
			}}},
			{Kind: wasm.TypeDefKindFunc, Func: last},
		},
		Funcs: []uint32{1, 2},
	}
	expected := []*wasm.FuncType{nil, first, last}
	count := 0
	m.RangeFunctionTypes(func(index uint32, signature *wasm.FuncType) bool {
		if index != uint32(count) || signature != expected[count] {
			t.Fatalf("wrong flat entry %d: %+v", index, signature)
		}
		count++
		return true
	})
	if count != len(expected) {
		t.Fatalf("visited %d entries", count)
	}
	for i, signature := range expected {
		if m.GetFuncTypeByTypeIndex(uint32(i)) != signature {
			t.Fatalf("wrong indexed lookup %d", i)
		}
	}
	if m.GetFuncType(0) != first || m.GetFuncType(1) != last {
		t.Fatal("function lookup disagrees with flat type traversal")
	}
	if m.GetFuncTypeByTypeIndex(math.MaxUint32) != nil || m.GetFuncType(math.MaxUint32) != nil {
		t.Fatal("out-of-range index accepted")
	}
	count = 0
	m.RangeFunctionTypes(func(uint32, *wasm.FuncType) bool { count++; return false })
	if count != 1 {
		t.Fatal("traversal ignored early stop")
	}
}
