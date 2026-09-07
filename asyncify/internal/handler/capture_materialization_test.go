package handler

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestCaptureMaterializationRejectsDefinitions(t *testing.T) {
	for _, planned := range []bool{false, true} {
		plan := map[int][]semantics.Materialization{}
		if planned {
			plan[2] = []semantics.Materialization{{Domain: semantics.GuestExecution}}
		}
		verifier := newMaterializationVerifier(plan, []wasm.ValType{wasm.ValI32})
		verifier.beginNoMaterialization(2)
		if _, ok := verifier.consume(wasm.ValI32, nil); ok {
			t.Fatal("capture accepted materialization")
		}
		if verifier.finish() == nil {
			t.Fatal("capture definition error was lost")
		}
	}
}

func TestCaptureMaterializationCanReturnToPrimitivePolicy(t *testing.T) {
	verifier := newMaterializationVerifier(map[int][]semantics.Materialization{}, nil)
	verifier.beginNoMaterialization(2)
	verifier.beginInDomain(3, semantics.GuestExecution, semantics.PreserveLocals)
	if err := verifier.finish(); err != nil {
		t.Fatal(err)
	}
}
