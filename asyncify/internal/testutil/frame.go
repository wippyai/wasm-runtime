// Package testutil provides structural assertions for Asyncify tests.
package testutil

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// RequireContinuationFrame checks generated state/data access and a frame write.
// It derives global identities from the public control ABI, not scratch-local
// counts. This structural assertion complements actual suspend/rewind tests.
func RequireContinuationFrame(t testing.TB, m *wasm.Module, body wasm.FuncBody) {
	t.Helper()
	controlGlobals := func(name string, opcode byte) []uint32 {
		t.Helper()
		for _, exp := range m.Exports {
			if exp.Kind != wasm.KindFunc || exp.Name != name {
				continue
			}
			imported := uint32(m.NumImportedFuncs())
			if exp.Idx < imported || uint64(exp.Idx-imported) >= uint64(len(m.Code)) {
				t.Fatalf("invalid control export %s", name)
			}
			instrs, err := wasm.DecodeInstructions(m.Code[exp.Idx-imported].Code)
			if err != nil {
				t.Fatal(err)
			}
			var ids []uint32
			for _, instr := range instrs {
				if instr.Opcode == opcode {
					ids = append(ids, instr.Imm.(wasm.GlobalImm).GlobalIdx)
				}
			}
			return ids
		}
		t.Fatalf("missing control export %s", name)
		return nil
	}
	state := controlGlobals("asyncify_get_state", wasm.OpGlobalGet)
	if len(state) != 1 {
		t.Fatal("get_state must identify exactly one state global")
	}
	var data uint32
	foundData := false
	for _, id := range controlGlobals("asyncify_start_unwind", wasm.OpGlobalSet) {
		if id != state[0] {
			data = id
			foundData = true
		}
	}
	if !foundData {
		t.Fatal("start_unwind did not identify continuation data global")
	}
	instrs, err := wasm.DecodeInstructions(body.Code)
	if err != nil {
		t.Fatal(err)
	}
	var readsState, readsData, writesFrame bool
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpGlobalGet {
			id := instr.Imm.(wasm.GlobalImm).GlobalIdx
			readsState = readsState || id == state[0]
			readsData = readsData || id == data
		}
		writesFrame = writesFrame || instr.Opcode == wasm.OpI32Store
	}
	if !readsState || !readsData || !writesFrame {
		t.Fatalf("missing continuation frame: state=%v data=%v store=%v", readsState, readsData, writesFrame)
	}
}
