package semantics

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestBlockSignatureUsesOriginalFlatTypeSpace(t *testing.T) {
	first := &wasm.FuncType{Params: []wasm.ValType{wasm.ValI64}, Results: []wasm.ValType{wasm.ValF32}}
	second := &wasm.FuncType{Results: []wasm.ValType{wasm.ValF64}}
	module := &wasm.Module{
		Types: []wasm.FuncType{*first, *second},
		TypeDefs: []wasm.TypeDef{
			{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{Types: []wasm.SubType{
				{CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &wasm.StructType{}}},
				{CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: first}},
			}}},
			{Kind: wasm.TypeDefKindFunc, Func: second},
		},
	}
	if _, err := ResolveBlockType(0, module); err == nil {
		t.Fatal("accepted struct as block signature")
	}
	sig, err := ResolveBlockType(1, module)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig.Params) != 1 || sig.Params[0] != wasm.ValI64 || len(sig.Results) != 1 || sig.Results[0] != wasm.ValF32 {
		t.Fatalf("wrong flat signature: %+v", sig)
	}
	sig.Params[0] = wasm.ValI32
	if first.Params[0] != wasm.ValI64 {
		t.Fatal("signature aliases module")
	}
	tail, err := ResolveBlockType(2, module)
	if err != nil || len(tail.Results) != 1 || tail.Results[0] != wasm.ValF64 {
		t.Fatalf("lost type after recursive group: %+v %v", tail, err)
	}
	for _, bad := range []int32{3, 2147483647, -6, -128} {
		if _, err := ResolveBlockType(bad, module); err == nil {
			t.Fatalf("accepted invalid type %d", bad)
		}
	}
}

func TestBlockSignatureCompactReferences(t *testing.T) {
	for _, tc := range []struct {
		encoded  int32
		expected wasm.ValType
	}{
		{-13, wasm.ValNullFuncRef}, {-14, wasm.ValNullExternRef}, {-15, wasm.ValNullRef},
		{-16, wasm.ValFuncRef}, {-17, wasm.ValExtern}, {-18, wasm.ValAnyRef},
		{-19, wasm.ValEqRef}, {-20, wasm.ValI31Ref}, {-21, wasm.ValStructRef}, {-22, wasm.ValArrayRef},
	} {
		signature, err := ResolveBlockType(tc.encoded, nil)
		if err != nil || len(signature.Params) != 0 || len(signature.Results) != 1 || signature.Results[0] != tc.expected {
			t.Fatalf("compact type %d: %+v %v", tc.encoded, signature, err)
		}
	}
}
