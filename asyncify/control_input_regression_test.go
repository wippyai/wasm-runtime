package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
)

func TestTransformRejectsBlockTypeAliasingGeneratedType(t *testing.T) {
	// The original module has only type 0. A generated Asyncify control helper
	// would add () -> i32 as type 1, repairing this invalid guest block type.
	module := &wasm.Module{
		Types: []wasm.FuncType{{}}, Funcs: []uint32{0},
		Code: []wasm.FuncBody{{Code: wasm.EncodeInstructions([]wasm.Instruction{
			{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: 1}},
			{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 7}},
			{Opcode: wasm.OpEnd}, {Opcode: wasm.OpDrop}, {Opcode: wasm.OpEnd},
		})}},
	}
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	raw := module.Encode()
	if _, err := rt.CompileModule(ctx, raw); err == nil {
		t.Fatal("expected invalid original block type")
	}
	out, err := asyncify.Transform(raw, asyncify.Config{})
	if err == nil {
		if _, compileErr := rt.CompileModule(ctx, out); compileErr != nil {
			t.Fatalf("accepted invalid input, also invalid output: %v", compileErr)
		}
		t.Fatal("generated helper type repaired invalid original block type")
	}
	if !strings.Contains(err.Error(), "input module identities:") {
		t.Fatalf("not rejected before module mutation: %v", err)
	}
}

func TestTransformRejectsMalformedFunctionControl(t *testing.T) {
	call := wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}
	end := wasm.Instruction{Opcode: wasm.OpEnd}
	for _, tc := range []struct {
		name string
		code []wasm.Instruction
	}{
		{"trailing-instructions", []wasm.Instruction{call, end, {Opcode: wasm.OpNop}, end}},
		{"root-else", []wasm.Instruction{call, {Opcode: wasm.OpElse}, end}},
		{"missing-function-end", []wasm.Instruction{call}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := &wasm.Module{Types: []wasm.FuncType{{}}, Imports: []wasm.Import{{Module: "env", Name: "yield", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}}}, Funcs: []uint32{0}, Code: []wasm.FuncBody{{Code: wasm.EncodeInstructions(tc.code)}}}
			ctx := context.Background()
			rt := wazero.NewRuntime(ctx)
			defer rt.Close(ctx)
			raw := module.Encode()
			if _, err := rt.CompileModule(ctx, raw); err == nil {
				t.Fatal("original unexpectedly valid")
			}
			if out, err := asyncify.Transform(raw, asyncify.Config{Matcher: asyncify.NewExactMatcher([]string{"env.yield"})}); err == nil {
				if _, compileErr := rt.CompileModule(ctx, out); compileErr != nil {
					t.Fatalf("accepted malformed control (invalid output: %v)", compileErr)
				}
				t.Fatal("transform repaired malformed control")
			}
		})
	}
}
