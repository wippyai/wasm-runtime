package engine

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify/internal/codegen"
	"github.com/wippyai/wasm-runtime/asyncify/internal/handler"
	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// Deliberately overlap sources with destination cells. The production allocator
// currently keeps them apart; the transfer itself must still implement a true
// parallel assignment so future storage changes cannot silently corrupt swaps.
func TestBranchTargetParallelTransferDoesNotDependOnStoragePartition(t *testing.T) {
	fixture := capturePlanFixture(t, `block (result i32 i32) call $yield i32.const 7 i32.const 8 br 0 end i32.add`)
	var target ir.BranchTarget
	found := false
	for _, action := range fixture.program.actions {
		if branch, ok := action.Branch(); ok {
			target, found = branch.Target(0)
			break
		}
	}
	if !found || target.PortCount() != 2 || target.Depth() != 0 {
		t.Fatal("fixture lacks two-cell target")
	}
	a, _ := target.Port(0)
	b, _ := target.Port(1)
	em := codegen.NewEmitter()
	em.I32Const(5).LocalSet(a.Local()).I32Const(9).LocalSet(b.Local()).Block(codegen.BlockVoid)
	ctx := &handler.Context{Emit: em}
	if err := emitBranchTarget(ctx, target, []stackEntry{semantics.StoredOperand(b.Local(), wasm.ValI32), semantics.StoredOperand(a.Local(), wasm.ValI32)}, 0); err != nil {
		t.Fatal(err)
	}
	em.End().LocalGet(a.Local()).I32Const(100).I32Mul().LocalGet(b.Local()).I32Add().End()
	module := wasm.Module{
		Types: []wasm.FuncType{{Results: []wasm.ValType{wasm.ValI32}}}, Funcs: []uint32{0},
		Exports: []wasm.Export{{Name: "run", Kind: wasm.KindFunc, Idx: 0}},
		Code:    []wasm.FuncBody{{Locals: []wasm.LocalEntry{{Count: uint32(len(fixture.locals)), ValType: wasm.ValI32}}, Code: em.Bytes()}},
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			cfg := wazero.NewRuntimeConfigCompiler()
			if backend == "interpreter" {
				cfg = wazero.NewRuntimeConfigInterpreter()
			}
			rt := wazero.NewRuntimeWithConfig(ctx, cfg)
			defer rt.Close(ctx)
			mod, err := rt.Instantiate(ctx, module.Encode())
			if err != nil {
				t.Fatal(err)
			}
			got, err := mod.ExportedFunction("run").Call(ctx)
			if err != nil || len(got) != 1 || got[0] != 905 {
				t.Fatalf("parallel swap got %v err=%v, want [905]", got, err)
			}
		})
	}
}
