package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

// BenchmarkTransform_Small benchmarks transformation of a small module.
func BenchmarkTransform_Small(b *testing.B) {
	m := createSmallModule()
	data := m.Encode()

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := eng.Transform(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTransform_Medium benchmarks transformation of a medium module.
func BenchmarkTransform_Medium(b *testing.B) {
	m := createMediumModule(10) // 10 functions
	data := m.Encode()

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := eng.Transform(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTransform_Large benchmarks transformation of a large module.
func BenchmarkTransform_Large(b *testing.B) {
	m := createMediumModule(100) // 100 functions
	data := m.Encode()

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := eng.Transform(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTransform_ComplexFunction benchmarks a single complex function.
func BenchmarkTransform_ComplexFunction(b *testing.B) {
	m := createComplexFunctionModule()
	data := m.Encode()

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := eng.Transform(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParse benchmarks just parsing (baseline).
func BenchmarkParse(b *testing.B) {
	m := createMediumModule(50)
	data := m.Encode()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := wasm.ParseModule(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncode benchmarks just encoding (baseline).
func BenchmarkEncode(b *testing.B) {
	m := createMediumModule(50)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = m.Encode()
	}
}

// BenchmarkDecodeInstructions benchmarks instruction decoding.
func BenchmarkDecodeInstructions(b *testing.B) {
	m := createComplexFunctionModule()
	code := m.Code[0].Code

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := wasm.DecodeInstructions(code)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCallGraph benchmarks call graph construction.
func BenchmarkCallGraph(b *testing.B) {
	m := createMediumModule(50)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = BuildCallGraph(m)
	}
}

// Helper functions to create test modules

func createSmallModule() *wasm.Module {
	return &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 2, ValType: wasm.ValI32}},
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
					{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}
}

func createMediumModule(numFuncs int) *wasm.Module {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI32}},
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    make([]uint32, numFuncs),
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code:     make([]wasm.FuncBody, numFuncs),
	}

	for i := 0; i < numFuncs; i++ {
		m.Funcs[i] = uint32(i % 3)
		m.Code[i] = wasm.FuncBody{
			Locals: []wasm.LocalEntry{{Count: 3, ValType: wasm.ValI32}},
			Code: wasm.EncodeInstructions([]wasm.Instruction{
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: int32(i)}},
				{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async call
				{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
				{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
				{Opcode: wasm.OpI32Add},
				{Opcode: wasm.OpEnd},
			}),
		}
	}

	return m
}

func createComplexFunctionModule() *wasm.Module {
	// Create a function with many instructions and multiple async calls
	instrs := []wasm.Instruction{
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
	}

	// Add 20 async calls with surrounding code
	for i := 0; i < 20; i++ {
		instrs = append(instrs,
			wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: int32(i)}},
			wasm.Instruction{Opcode: wasm.OpI32Add},
			wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
			wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}, // async
			wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
			wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
			wasm.Instruction{Opcode: wasm.OpI32Add},
			wasm.Instruction{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 0}},
		)
	}

	instrs = append(instrs,
		wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		wasm.Instruction{Opcode: wasm.OpEnd},
	)

	return &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "async", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Code: []wasm.FuncBody{
			{
				Locals: []wasm.LocalEntry{{Count: 5, ValType: wasm.ValI32}},
				Code:   wasm.EncodeInstructions(instrs),
			},
		},
	}
}
