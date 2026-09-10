package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestTransformRejectsCallTypeAliasingGeneratedType(t *testing.T) {
	// Original type 0 is () -> (). Helpers would add type 1 () -> i32
	// and type 2 (i32) -> (), making this invalid type index compile successfully.
	m := &wasm.Module{
		Types:  []wasm.FuncType{{}},
		Funcs:  []uint32{0},
		Tables: []wasm.TableType{{ElemType: byte(wasm.ValFuncRef), Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{{Code: wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
			{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 2}},
			{Opcode: wasm.OpEnd},
		})}},
	}
	raw := m.Encode()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	if _, err := rt.CompileModule(ctx, raw); err == nil {
		t.Fatal("raw invalid signature accepted")
	}
	out, err := asyncify.Transform(raw, asyncify.Config{IgnoreIndirect: true})
	if err == nil {
		if _, compileErr := rt.CompileModule(ctx, out); compileErr != nil {
			t.Fatalf("transform accepted invalid type; output also invalid: %v", compileErr)
		}
		t.Fatal("transform made invalid original call type alias a valid helper type")
	}
	if !strings.Contains(err.Error(), "input module identities:") {
		t.Fatalf("not rejected before mutation: %v", err)
	}
}
