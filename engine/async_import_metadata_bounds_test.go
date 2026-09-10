package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestAsyncImportMetadataRejectsInvalidFunctionSpace(t *testing.T) {
	cases := map[string][]byte{
		"empty module bytes":            nil,
		"missing body":                  (&wasm.Module{Types: []wasm.FuncType{{}}, Funcs: []uint32{0}}).Encode(),
		"extra body":                    (&wasm.Module{Code: []wasm.FuncBody{{Code: []byte{wasm.OpEnd}}}}).Encode(),
		"export outside function space": (&wasm.Module{Exports: []wasm.Export{{Name: "bad", Kind: wasm.KindFunc, Idx: 100}}}).Encode(),
		"call outside function space":   (&wasm.Module{Types: []wasm.FuncType{{}}, Funcs: []uint32{0}, Code: []wasm.FuncBody{{Code: []byte{wasm.OpCall, 100, wasm.OpEnd}}}}).Encode(),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCoreModuleMeta(0, data); err == nil {
				t.Fatal("invalid function metadata accepted")
			}
		})
	}
	if _, err := parseCoreModuleMeta(0, (&wasm.Module{}).Encode()); err != nil {
		t.Fatalf("valid empty core module rejected: %v", err)
	}
}
