package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

type testLinearizeConfig struct {
	AsyncFuncs     map[uint32]bool
	Module         *wasm.Module
	AllocLocal     func(wasm.ValType) uint32
	StateGlobal    uint32
	StateRewinding int32
}

func prepareFixture(t *testing.T, instructions []wasm.Instruction, config *testLinearizeConfig) *Analysis {
	t.Helper()
	// These unit fixtures use functions 0 and 1 and table/type 0. Their tests
	// concern routing structure; full operand execution is tested differentially.
	calls := semantics.NewCalls(&wasm.Module{
		Types: []wasm.FuncType{{Results: []wasm.ValType{wasm.ValI32}}}, Funcs: []uint32{0, 0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
	})
	analysis, err := Prepare(wasm.EncodeInstructions(instructions), config.Module, calls, func(call semantics.CallOperation) bool {
		return call.Kind != semantics.DirectCall || config.AsyncFuncs[call.TargetIndex]
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return analysis
}

func mustLinearize(t *testing.T, instructions []wasm.Instruction, config *testLinearizeConfig) []wasm.Instruction {
	t.Helper()
	analysis := prepareFixture(t, instructions, config)
	result, err := linearizeControl(analysis, &LinearizeConfig{AllocLocal: config.AllocLocal, StateGlobal: config.StateGlobal, StateRewinding: config.StateRewinding})
	if err != nil {
		t.Fatal(err)
	}
	instructions, err = result.CopyInstructions()
	if err != nil {
		t.Fatal(err)
	}
	return instructions
}
