package asyncify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestPolymorphicExitPreservesValidationAndTrap(t *testing.T) {
	for _, result := range []struct{ types, zero string }{
		{"i32", "i32.const 0"}, {"i64", "i64.const 0"}, {"f32", "f32.const 0"}, {"f64", "f64.const 0"},
	} {
		for _, boundary := range []struct{ name, body string }{
			{"block", `block (result ` + result.types + `) call $yield unreachable select end call $yield`},
			{"loop", `loop (result ` + result.types + `) call $yield unreachable select end call $yield`},
			{"then", `i32.const 1 if (result ` + result.types + `) call $yield unreachable select else ` + result.zero + ` end call $yield`},
			{"else", `i32.const 0 if (result ` + result.types + `) ` + result.zero + ` else call $yield unreachable select end call $yield`},
			{"function", `call $yield unreachable select`},
			{"explicit_return", `call $yield unreachable select return`},
		} {
			t.Run(result.types+"/"+boundary.name, func(t *testing.T) { checkPolymorphicExit(t, result.types, boundary.body) })
		}
	}
	t.Run("mixed_known_unknown", func(t *testing.T) {
		checkPolymorphicExit(t, "i64 i32", `block (result i64 i32) call $yield unreachable select i32.const 7 end call $yield`)
	})
}

func checkPolymorphicExit(t *testing.T, result, body string) {
	t.Helper()
	raw, err := wat.Compile(`(module (import "env" "yield" (func $yield)) (memory 1) (func (export "run") (result ` + result + `) ` + body + `))`)
	if err != nil {
		t.Fatal(err)
	}
	checkPolymorphicModule(t, raw)
}

func checkPolymorphicModule(t *testing.T, raw []byte) {
	t.Helper()
	check := func(t *testing.T, code []byte, backend string) {
		t.Helper()
		ctx := context.Background()
		cfg := wazero.NewRuntimeConfigCompiler()
		if backend == "interpreter" {
			cfg = wazero.NewRuntimeConfigInterpreter()
		}
		rt := wazero.NewRuntimeWithConfig(ctx, cfg)
		defer rt.Close(ctx)
		calls := 0
		if _, err := rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func() { calls++ }).Export("yield").Instantiate(ctx); err != nil {
			t.Fatal(err)
		}
		mod, err := rt.Instantiate(ctx, code)
		if err != nil {
			t.Fatalf("valid source must remain valid: %v", err)
		}
		if _, err := mod.ExportedFunction("run").Call(ctx); err == nil || !strings.Contains(err.Error(), "unreachable") {
			t.Fatalf("expected source trap, got %v", err)
		}
		if calls != 1 {
			t.Fatalf("calls=%d, want 1", calls)
		}
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend+"/source", func(t *testing.T) { check(t, raw, backend) })
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.yield"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend+"/transformed", func(t *testing.T) { check(t, transformed, backend) })
	}
}

// A function-label branch can reach materialized root results even when the
// lexical root exit is polymorphic. It must not be mistaken for an unreachable
// direct completion and replaced with a trap.
func TestPolymorphicRootExitPreservesFunctionBranch(t *testing.T) {
	runYieldMatrix(t, `(func (export "run") (result i64) i64.const 42 call $yield br 0 unreachable select)`, nil, 42)
}

// Build vector signatures directly because the repository WAT parser does not
// yet support v128 types. Both Wazero engines still validate the original Wasm.
func TestPolymorphicVectorExitPreservesValidation(t *testing.T) {
	for _, boundary := range []string{"block", "loop", "then", "else", "function", "explicit_return", "br", "br_if", "br_table"} {
		t.Run(boundary, func(t *testing.T) {
			yield := wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}
			trap := []wasm.Instruction{yield, {Opcode: wasm.OpUnreachable}, {Opcode: wasm.OpSelect}}
			zero := wasm.Instruction{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: make([]byte, 16)}}
			var code []wasm.Instruction
			switch boundary {
			case "function":
				code = append(code, trap...)
			case "br", "br_if", "br_table":
				code = append(code, trap...)
				branch := wasm.Instruction{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}}
				if boundary == "br_if" {
					branch.Opcode = wasm.OpBrIf
				}
				if boundary == "br_table" {
					branch = wasm.Instruction{Opcode: wasm.OpBrTable, Imm: wasm.BrTableImm{Labels: []uint32{0}, Default: 0}}
				}
				code = append(code, branch)
			case "explicit_return":
				code = append(code, trap...)
				code = append(code, wasm.Instruction{Opcode: wasm.OpReturn})
			case "block", "loop":
				op := wasm.OpBlock
				if boundary == "loop" {
					op = wasm.OpLoop
				}
				code = append(code, wasm.Instruction{Opcode: op, Imm: wasm.BlockImm{Type: wasm.BlockTypeV128}})
				code = append(code, trap...)
				code = append(code, wasm.Instruction{Opcode: wasm.OpEnd}, yield)
			case "then", "else":
				cond := int32(1)
				if boundary == "else" {
					cond = 0
				}
				code = append(code, wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: cond}}, wasm.Instruction{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeV128}})
				if boundary == "then" {
					code = append(code, trap...)
				} else {
					code = append(code, zero)
				}
				code = append(code, wasm.Instruction{Opcode: wasm.OpElse})
				if boundary == "else" {
					code = append(code, trap...)
				} else {
					code = append(code, zero)
				}
				code = append(code, wasm.Instruction{Opcode: wasm.OpEnd}, yield)
			}
			code = append(code, wasm.Instruction{Opcode: wasm.OpEnd})
			module := &wasm.Module{
				Types:   []wasm.FuncType{{}, {Results: []wasm.ValType{wasm.ValV128}}},
				Imports: []wasm.Import{{Module: "env", Name: "yield", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}}},
				Funcs:   []uint32{1}, Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
				Exports: []wasm.Export{{Name: "run", Kind: wasm.KindFunc, Idx: 1}},
				Code:    []wasm.FuncBody{{Code: wasm.EncodeInstructions(code)}},
			}
			checkPolymorphicModule(t, module.Encode())
		})
	}
}
