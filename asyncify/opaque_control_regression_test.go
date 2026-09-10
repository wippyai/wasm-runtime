package asyncify

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/wippyai/wasm-runtime/wasm"
)

// br_on_null does not transfer its tested null reference. The i32 below it is
// the declared result on the taken edge. Keep this executable source fixture:
// the historical lowering silently changed its result from 42 to zero.
func TestTransformRejectsUnmodeledReferenceBranchControl(t *testing.T) {
	raw := referenceBranchFixture()

	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCoreFeatures(
		api.CoreFeaturesV2|experimental.CoreFeaturesTypedFunctionReferences,
	))
	defer runtime.Close(ctx)
	if _, err := runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func() {}).Export("async").Instantiate(ctx); err != nil {
		t.Fatal(err)
	}
	module, err := runtime.Instantiate(ctx, raw)
	if err != nil {
		t.Fatalf("source fixture must execute with typed references enabled: %v", err)
	}
	got, err := module.ExportedFunction("run").Call(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 42 {
		t.Fatalf("source returned %v, want [42]", got)
	}

	transformed, err := Transform(raw, Config{AsyncImports: []string{"env.async"}})
	if transformed != nil || err == nil || !strings.Contains(err.Error(), "unsupported unmodeled control transfer") {
		t.Fatalf("Transform() = (%d bytes, %v), want no output and unsupported-control error", len(transformed), err)
	}

	// A module with no selected async import never reaches production lowering.
	// Its original function body remains byte-for-byte intact even when it has
	// an operation reserved for future typed control-transfer support.
	withoutAsync, err := Transform(raw, Config{})
	if err != nil {
		t.Fatalf("no-async Transform() error = %v", err)
	}
	originalModule, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	withoutAsyncModule, err := wasm.ParseModule(withoutAsync)
	if err != nil {
		t.Fatal(err)
	}
	if len(originalModule.Code) != 1 || len(withoutAsyncModule.Code) < 1 || !bytes.Equal(originalModule.Code[0].Code, withoutAsyncModule.Code[0].Code) {
		t.Fatal("no-async path changed the original function body")
	}
	unchanged, err := runtime.InstantiateWithConfig(ctx, withoutAsync, wazero.NewModuleConfig().WithName("without-async"))
	if err != nil {
		t.Fatalf("no-async module must still instantiate: %v", err)
	}
	got, err = unchanged.ExportedFunction("run").Call(ctx)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("no-async execution = (%v, %v), want [42]", got, err)
	}
}

func referenceBranchFixture() []byte {
	return (&wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}}},
		Funcs:   []uint32{1},
		Exports: []wasm.Export{{Name: "run", Kind: wasm.KindFunc, Idx: 1}},
		Code: []wasm.FuncBody{{Code: wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: wasm.BlockTypeI32}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
			{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: wasm.HeapTypeExtern}},
			{Opcode: wasm.OpBrOnNull, Imm: wasm.BranchImm{LabelIdx: 0}},
			{Opcode: wasm.OpDrop},
			{Opcode: wasm.OpEnd},
			{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			{Opcode: wasm.OpEnd},
		})}},
	}).Encode()
}
