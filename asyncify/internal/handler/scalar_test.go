package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestScalarBindingRejectsMismatchesBeforeUsingContext(t *testing.T) {
	for _, tc := range []struct {
		handler Handler
		opcode  byte
	}{
		{UnaryOpHandler{Opcode: wasm.OpI64Eqz, ResultType: wasm.ValI64}, wasm.OpI64Eqz},
		{BinaryOpHandler{Opcode: wasm.OpI32Add, ResultType: wasm.ValI32}, wasm.OpI32Sub},
		{BinaryOpHandler{Opcode: wasm.OpI32Clz, ResultType: wasm.ValI32}, wasm.OpI32Clz},
	} {
		if err := tc.handler.Handle(nil, wasm.Instruction{Opcode: tc.opcode}); err == nil {
			t.Fatal("accepted inconsistent scalar registry binding")
		}
	}
}
