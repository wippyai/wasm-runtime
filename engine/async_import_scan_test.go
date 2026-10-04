package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestAsyncImportScanPreservesCallGraph(t *testing.T) {
	code := []byte{
		wasm.OpCall, 1, wasm.OpCall, 1, wasm.OpReturnCall, 2,
		wasm.OpCallIndirect, 0, 0, wasm.OpEnd,
	}
	data := (&wasm.Module{
		Types: []wasm.FuncType{{}}, Funcs: []uint32{0, 0, 0},
		Code: []wasm.FuncBody{{Code: code}, {Code: []byte{wasm.OpReturnCallIndirect, 0, 0, wasm.OpEnd}},
			{Code: []byte{wasm.OpCallRef, 0, wasm.OpReturnCallRef, 0, wasm.OpEnd}}},
	}).Encode()
	meta, err := parseCoreModuleMeta(0, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(meta.callees[0], []uint32{1, 2}) {
		t.Fatalf("direct call deduplication/order changed: %v", meta.callees)
	}
	for i := range uint32(3) {
		if !meta.hasIndirect[i] {
			t.Fatalf("indirect call in function %d was missed", i)
		}
	}
}

func TestAsyncImportScanKeepsMalformedBytecodeErrorPrecedence(t *testing.T) {
	data := (&wasm.Module{
		Types: []wasm.FuncType{{}}, Funcs: []uint32{0},
		Code: []wasm.FuncBody{{Code: []byte{wasm.OpCall, 100, 0xff, wasm.OpEnd}}},
	}).Encode()
	_, err := parseCoreModuleMeta(0, data)
	if err == nil || !strings.Contains(err.Error(), "decode instructions") {
		t.Fatalf("malformed-bytecode error was hidden by call bounds: %v", err)
	}
}
