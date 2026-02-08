package asyncify

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// Edge case tests to find gaps in asyncify implementation

func TestEdge_EmptyStackBeforeCall(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (param i32) (result i32)))
		(func (export "test") (result i32)
			(call $async (i32.const 42)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_CallIndirectAsync(t *testing.T) {
	watSrc := `(module
		(type $sig (func (result i32)))
		(import "env" "async" (func $async (type $sig)))
		(table 1 funcref)
		(elem (i32.const 0) $async)
		(func (export "test") (result i32)
			(call_indirect (type $sig) (i32.const 0)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_NestedAsyncCalls(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (param i32) (result i32)))
		(func (export "test") (result i32)
			(call $async (call $async (i32.const 1))))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_AsyncInSelect(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (result i32)
			(select
				(call $async)
				(call $async)
				(call $async)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_AsyncReturnDiscarded(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (result i32)
			(block (result i32)
				(call $async)
				(drop)
				(i32.const 99)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_MultipleReturnsEarlyExit(t *testing.T) {
	watSrc := `(module
		(type $pair (func (result i32 i32)))
		(import "env" "async" (func $async (type $pair)))
		(func (export "test") (param $x i32) (result i32 i32)
			(if (local.get $x)
				(then
					(call $async)
					(return)))
			(i32.const 1)
			(i32.const 2))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_BrTableWithAsync(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $idx i32) (result i32)
			(local $r i32)
			(block $b2
				(block $b1
					(block $b0
						(local.set $r (call $async))
						(br_table $b0 $b1 $b2 (local.get $idx)))
					(local.set $r (i32.add (local.get $r) (i32.const 10))))
				(local.set $r (i32.add (local.get $r) (i32.const 20))))
			(i32.add (local.get $r) (i32.const 30)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_LoopWithMultipleAsyncCalls(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $n i32) (result i32)
			(local $sum i32)
			(local $i i32)
			(block $exit
				(loop $loop
					(local.set $sum (i32.add (local.get $sum) (call $async)))
					(local.set $sum (i32.add (local.get $sum) (call $async)))
					(local.set $i (i32.add (local.get $i) (i32.const 1)))
					(br_if $exit (i32.ge_u (local.get $i) (local.get $n)))
					(br $loop)))
			(local.get $sum))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_DeepNesting(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $a i32) (param $b i32) (result i32)
			(if (result i32) (local.get $a)
				(then
					(if (result i32) (local.get $b)
						(then (call $async))
						(else (i32.const 1))))
				(else
					(if (result i32) (local.get $b)
						(then (i32.const 2))
						(else (call $async))))))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_MemoryOpsWithAsync(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (result i32)
			(i32.store (call $async) (call $async))
			(i32.load (call $async)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_GlobalOpsWithAsync(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(global $g (mut i32) (i32.const 0))
		(func (export "test") (result i32)
			(global.set $g (call $async))
			(i32.add (global.get $g) (call $async)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_AllTypesParams(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (param i32 i64 f32 f64) (result i32)))
		(func (export "test") (result i32)
			(call $async
				(i32.const 1)
				(i64.const 2)
				(f32.const 3.0)
				(f64.const 4.0)))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_AllTypesReturns(t *testing.T) {
	watSrc := `(module
		(import "env" "async_i32" (func $async_i32 (result i32)))
		(import "env" "async_i64" (func $async_i64 (result i64)))
		(import "env" "async_f32" (func $async_f32 (result f32)))
		(import "env" "async_f64" (func $async_f64 (result f64)))
		(func (export "test") (result i32)
			(drop (call $async_i64))
			(drop (call $async_f32))
			(drop (call $async_f64))
			(call $async_i32))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.*")
}

func TestEdge_VoidAsyncCall(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async))
		(func (export "test") (result i32)
			(call $async)
			(call $async)
			(i32.const 42))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_AsyncAfterUnreachable(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $x i32) (result i32)
			(if (local.get $x)
				(then (return (call $async))))
			(unreachable))
		(memory 1))`
	runEdgeTest(t, watSrc, "env.async")
}

func TestEdge_BrIfWithValueToResultBlock(t *testing.T) {
	watSrc := `(module
		(func (export "test") (param $cond i32) (result i32)
			(block (result i32)
				(i32.const 42)
				(local.get $cond)
				(br_if 0)
				(drop)
				(i32.const 99)))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"dummy.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}

func TestEdge_BrIfWithAsyncInBranch(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $cond i32) (result i32)
			(block (result i32)
				(call $async)
				(local.get $cond)
				(br_if 0)
				(drop)
				(i32.const 99)))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}

func TestEdge_LoopWithAsyncAndBrIf(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (result i32)
			(local $sum i32)
			(block (result i32)
				(loop
					(local.set $sum (i32.add (local.get $sum) (call $async)))
					(local.get $sum)
					(local.get $sum)
					(i32.const 100)
					(i32.ge_u)
					(br_if 1)
					(drop)
					(br 0))
				(local.get $sum)))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}

func TestEdge_NestedLoopsWithAsync(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (result i32)
			(local $i i32)
			(local $j i32)
			(local $sum i32)
			(block (result i32)
				(loop
					(local.set $j (i32.const 0))
					(loop
						(local.set $sum (i32.add (local.get $sum) (call $async)))
						(local.set $j (i32.add (local.get $j) (i32.const 1)))
						(br_if 0 (i32.lt_u (local.get $j) (i32.const 3))))
					(local.set $i (i32.add (local.get $i) (i32.const 1)))
					(br_if 0 (i32.lt_u (local.get $i) (i32.const 3))))
				(local.get $sum)))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}

func TestEdge_BrIfFallthroughUsesValue(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $cond i32) (result i32)
			(block (result i32)
				(call $async)
				(local.get $cond)
				(br_if 0)
				(i32.const 1)
				(i32.add)))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	out, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	instrs, err := wasm.DecodeInstructions(out.Code[0].Code)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	hasBrIf := false
	hasAdd := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpBrIf {
			hasBrIf = true
		}
		if instr.Opcode == wasm.OpI32Add {
			hasAdd = true
		}
	}

	if !hasBrIf {
		t.Error("Expected br_if in output")
	}
	if !hasAdd {
		t.Error("Expected i32.add in output")
	}
}

func TestEdge_BlockWithParamsAndAsync(t *testing.T) {
	watSrc := `(module
		(type $block_t (func (param i32) (result i32)))
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (result i32)
			(i32.const 10)
			(block (type $block_t)
				(call $async)
				(i32.add)))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}

func TestEdge_BrTableWithAsyncResultBlocks(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $idx i32) (result i32)
			(block (result i32)
				(block (result i32)
					(block (result i32)
						(call $async)
						(local.get $idx)
						(br_table 0 1 2))
					(i32.const 10)
					(i32.add))
				(i32.const 100)
				(i32.add)))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}

func TestEdge_NonAsyncCallValueAcrossAsync(t *testing.T) {
	watSrc := `(module
		(import "env" "get_value" (func $get_value (result i32)))
		(import "env" "async" (func $async))
		(func (export "test") (result i32)
			(call $get_value)
			(call $async)
			(i32.const 1)
			(i32.add))
		(memory 1))`

	wasmData, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	transformed, err := Transform(wasmData, Config{
		Matcher: NewExactMatcher([]string{"env.async"}),
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(transformed); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	out, err := wasm.ParseModule(transformed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	instrs, err := wasm.DecodeInstructions(out.Code[0].Code)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	hasStoreOffset4 := false
	for _, instr := range instrs {
		if instr.Opcode == wasm.OpI32Store {
			if imm, ok := instr.Imm.(wasm.MemoryImm); ok && imm.Offset == 4 {
				hasStoreOffset4 = true
				break
			}
		}
	}
	if !hasStoreOffset4 {
		t.Error("Expected save path to include local storage at offset 4")
	}
}

func runEdgeTest(t *testing.T, watSrc string, pattern string) {
	t.Helper()

	wasmBytes, err := wat.Compile(watSrc)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	cfg := Config{
		Matcher: NewWildcardMatcher([]string{pattern}),
	}
	output, err := Transform(wasmBytes, cfg)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if _, err := wasm.ParseModule(output); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}
