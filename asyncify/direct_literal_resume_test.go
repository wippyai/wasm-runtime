package asyncify_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// TestDirectLiteralsSurviveRealResume keeps exact literal operands below an
// async host argument. The call deliberately returns a provisional value while
// unwinding, so replay must use the original argument and reconstruct every
// lower literal before the guest continues.
//
// The local.tee result is intentionally retained on the Wasm operand stack,
// then its guest local is overwritten. This distinguishes the immutable stack
// value from the mutable local read in the nested branch after rewind.
func TestDirectLiteralsSurviveRealResume(t *testing.T) {
	for _, bits := range []struct {
		name string
		f32  uint32
		f64  uint64
	}{
		{"negative-zero", 0x80000000, 0x8000000000000000},
		{"signaling-nan", 0x7f801234, 0x7ff0000000001234},
	} {
		t.Run(bits.name, func(t *testing.T) { checkDirectLiteralResume(t, bits.f32, bits.f64) })
	}
}

func checkDirectLiteralResume(t *testing.T, f32bits uint32, f64bits uint64) {
	t.Helper()
	transformed, err := asyncify.Transform(directLiteralTestModule(f32bits, f64bits).Encode(), asyncify.Config{AsyncImports: []string{"env.echo"}})
	if err != nil {
		t.Fatal(err)
	}

	for _, backend := range []string{"compiler", "interpreter"} {
		for _, resume := range []bool{false, true} {
			name := backend + "/normal"
			if resume {
				name = backend + "/unwind-rewind"
			}
			t.Run(name, func(t *testing.T) {
				result, calls, _, err := runDirectLiteralResumeCase(backend, transformed, resume)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.values) != 1 || result.values[0] != 141 {
					t.Fatalf("guest result = %v, want [141]", result.values)
				}
				if resume {
					if want := []uint32{5, 5}; !equalUint32(calls, want) {
						t.Fatalf("host replay arguments = %v, want %v", calls, want)
					}
				} else if want := []uint32{5}; !equalUint32(calls, want) {
					t.Fatalf("normal host arguments = %v, want %v", calls, want)
				}

				// Stores must preserve signed-zero and every vector lane bit; no
				// arithmetic or comparison is allowed to hide changed encodings.
				var expected [32]byte
				binary.LittleEndian.PutUint32(expected[0:4], f32bits)
				binary.LittleEndian.PutUint64(expected[8:16], f64bits)
				for i := 0; i < 16; i++ {
					expected[16+i] = byte(i)
				}
				if !bytes.Equal(result.memory, expected[:]) {
					t.Fatalf("literal store bits = %x, want %x", result.memory, expected)
				}
			})
		}
	}
}

// TestDirectLiteralOnlyFrameIsCallIdentifier isolates the frame invariant from
// guest-local snapshots. With no local effects before the call, literals below
// the host argument do not need continuation storage: only the call id remains.
func TestDirectLiteralOnlyFrameIsCallIdentifier(t *testing.T) {
	raw, err := wat.Compile(`(module
  (import "env" "echo" (func $echo (param i32) (result i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    i32.const 42
    i32.const 5
    call $echo
    drop))`)
	if err != nil {
		t.Fatal(err)
	}
	transformed, err := asyncify.Transform(raw, asyncify.Config{AsyncImports: []string{"env.echo"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			result, calls, peakFrameBytes, err := runDirectLiteralResumeCase(backend, transformed, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.values) != 1 || result.values[0] != 42 {
				t.Fatalf("guest result = %v, want [42]", result.values)
			}
			if want := []uint32{5, 5}; !equalUint32(calls, want) {
				t.Fatalf("host replay arguments = %v, want %v", calls, want)
			}
			if peakFrameBytes != 4 {
				t.Fatalf("literal operands occupied %d frame bytes, want only 4-byte call id", peakFrameBytes)
			}
		})
	}
}

func directLiteralTestModule(f32bits uint32, f64bits uint64) *wasm.Module {
	vector := make([]byte, 16)
	for index := range vector {
		vector[index] = byte(index)
	}
	return &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports:  []wasm.Import{{Module: "env", Name: "echo", Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}}},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Exports: []wasm.Export{
			{Name: "run", Kind: wasm.KindFunc, Idx: 1},
			{Name: "memory", Kind: wasm.KindMemory, Idx: 0},
		},
		Code: []wasm.FuncBody{{
			Locals: []wasm.LocalEntry{
				{Count: 1, ValType: wasm.ValI32}, {Count: 1, ValType: wasm.ValF32},
				{Count: 1, ValType: wasm.ValF64}, {Count: 1, ValType: wasm.ValV128},
			},
			Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
				{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpF32Const, Imm: wasm.F32Imm{Value: math.Float32frombits(f32bits)}},
				{Opcode: wasm.OpF64Const, Imm: wasm.F64Imm{Value: math.Float64frombits(f64bits)}},
				{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Const, V128Bytes: vector}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 5}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, {Opcode: wasm.OpDrop},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 3}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 2}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 99}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeI32}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeI32}},
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpElse}, {Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{}}, {Opcode: wasm.OpEnd},
				{Opcode: wasm.OpElse}, {Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{}}, {Opcode: wasm.OpEnd},
				{Opcode: wasm.OpI32Add},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{}}, {Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
				{Opcode: wasm.OpF32Store, Imm: wasm.MemoryImm{}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 8}}, {Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 2}},
				{Opcode: wasm.OpF64Store, Imm: wasm.MemoryImm{}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 16}}, {Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 3}},
				{Opcode: wasm.OpPrefixSIMD, Imm: wasm.SIMDImm{SubOpcode: wasm.SimdV128Store, MemArg: &wasm.MemoryImm{}}},
				{Opcode: wasm.OpEnd},
			}),
		}},
	}
}

type directLiteralResult struct {
	values []uint64
	memory []byte
}

func runDirectLiteralResumeCase(backend string, wasmBytes []byte, resume bool) (directLiteralResult, []uint32, uint32, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	config := wazero.NewRuntimeConfigCompiler()
	if backend == "interpreter" {
		config = wazero.NewRuntimeConfigInterpreter()
	}
	runtime := wazero.NewRuntimeWithConfig(ctx, config.WithCloseOnContextDone(true))
	defer runtime.Close(ctx)

	var calls []uint32
	if _, err := runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(func(ctx context.Context, module api.Module, argument uint32) uint32 {
		calls = append(calls, argument)
		if !resume {
			return argument
		}
		switch len(calls) {
		case 1:
			if !module.Memory().WriteUint32Le(1024, 1032) || !module.Memory().WriteUint32Le(1028, 8192) {
				panic("stack setup failed")
			}
			if _, err := module.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024); err != nil {
				panic(err)
			}
			// A real host can return anything while it starts unwinding. This
			// provisional result must not overwrite the rewind argument.
			return 0xdead
		case 2:
			if _, err := module.ExportedFunction("asyncify_stop_rewind").Call(ctx); err != nil {
				panic(err)
			}
			return argument
		default:
			panic("unexpected host replay")
		}
	}).Export("echo").Instantiate(ctx); err != nil {
		return directLiteralResult{}, nil, 0, fmt.Errorf("instantiate host: %w", err)
	}
	module, err := runtime.Instantiate(ctx, wasmBytes)
	if err != nil {
		return directLiteralResult{}, nil, 0, fmt.Errorf("instantiate guest: %w", err)
	}
	run := module.ExportedFunction("run")
	if run == nil {
		return directLiteralResult{}, nil, 0, fmt.Errorf("run export missing")
	}
	result, err := run.Call(ctx)
	if err != nil {
		return directLiteralResult{}, nil, 0, fmt.Errorf("initial run: %w", err)
	}
	if !resume {
		memory, ok := module.Memory().Read(0, 32)
		if !ok {
			return directLiteralResult{}, nil, 0, fmt.Errorf("read normal memory")
		}
		return directLiteralResult{values: result, memory: append([]byte(nil), memory...)}, calls, 0, nil
	}

	state, err := module.ExportedFunction("asyncify_get_state").Call(ctx)
	if err != nil || len(state) != 1 || state[0] != 1 {
		return directLiteralResult{}, nil, 0, fmt.Errorf("expected unwind state 1, got %v (%w)", state, err)
	}
	pointer, ok := module.Memory().ReadUint32Le(1024)
	if !ok || pointer < 1032 {
		return directLiteralResult{}, nil, 0, fmt.Errorf("invalid unwind frame pointer %d", pointer)
	}
	if _, err := module.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
		return directLiteralResult{}, nil, 0, fmt.Errorf("stop unwind: %w", err)
	}
	if _, err := module.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
		return directLiteralResult{}, nil, 0, fmt.Errorf("start rewind: %w", err)
	}
	result, err = run.Call(ctx)
	if err != nil {
		return directLiteralResult{}, nil, 0, fmt.Errorf("rewind run: %w", err)
	}
	state, err = module.ExportedFunction("asyncify_get_state").Call(ctx)
	if err != nil || len(state) != 1 || state[0] != 0 {
		return directLiteralResult{}, nil, 0, fmt.Errorf("expected completed state 0, got %v (%w)", state, err)
	}
	memory, ok := module.Memory().Read(0, 32)
	if !ok {
		return directLiteralResult{}, nil, 0, fmt.Errorf("read resumed memory")
	}
	return directLiteralResult{values: result, memory: append([]byte(nil), memory...)}, calls, pointer - 1032, nil
}

func equalUint32(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
