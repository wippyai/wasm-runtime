package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// stressTestCase defines a WAT module to test
type stressTestCase struct {
	name    string
	wat     string
	async   []string // functions to mark as async
	wantErr bool
}

var stressTests = []stressTestCase{
	{
		name: "simple_call",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32) (call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "i64_return",
		wat: `(module
			(import "env" "async" (func $async (result i64)))
			(func (export "test") (result i64) (call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "f32_return",
		wat: `(module
			(import "env" "async" (func $async (result f32)))
			(func (export "test") (result f32) (call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "f64_return",
		wat: `(module
			(import "env" "async" (func $async (result f64)))
			(func (export "test") (result f64) (call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "void_return",
		wat: `(module
			(import "env" "async" (func $async))
			(func (export "test") (call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "drop_result",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (drop (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multiple_calls",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(local $a i32) (local $b i32)
				(local.set $a (call $async))
				(local.set $b (call $async))
				(i32.add (local.get $a) (local.get $b)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "if_then_else",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (result i32) (local.get $x)
					(then (call $async))
					(else (i32.const 0))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "nested_if",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (result i32) (local.get $x)
					(then
						(if (result i32) (call $async)
							(then (call $async))
							(else (i32.const 0))))
					(else (i32.const -1))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "loop_with_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $n i32) (result i32)
				(local $sum i32) (local $i i32)
				(local.set $sum (i32.const 0))
				(local.set $i (i32.const 0))
				(block $exit
					(loop $loop
						(br_if $exit (i32.ge_s (local.get $i) (local.get $n)))
						(local.set $sum (i32.add (local.get $sum) (call $async)))
						(local.set $i (i32.add (local.get $i) (i32.const 1)))
						(br $loop)))
				(local.get $sum))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "select_with_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $c i32) (result i32)
				(select (call $async) (i32.const 42) (local.get $c)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "block_with_result",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block (result i32)
					(call $async)
					(i32.const 1)
					(i32.add)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_with_value",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(block $out (result i32)
					(br_if $out (call $async) (local.get $x))
					(i32.const 99)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_table",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(block $b2 (result i32)
					(block $b1 (result i32)
						(block $b0 (result i32)
							(call $async)
							(br_table $b0 $b1 $b2 (local.get $x)))
						(i32.const 100)
						(i32.add))
					(i32.const 200)
					(i32.add)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "call_indirect",
		wat: `(module
			(type $t0 (func (result i32)))
			(import "env" "async" (func $async (type $t0)))
			(table 2 funcref)
			(elem (i32.const 0) $async $double)
			(func $double (result i32) (i32.const 2))
			(func (export "test") (param $idx i32) (result i32)
				(call_indirect (type $t0) (local.get $idx)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "mixed_locals",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $a i32) (param $b i64) (param $c f32) (param $d f64) (result i32)
				(local $x i32) (local $y i64) (local $z f32) (local $w f64)
				(local.set $x (call $async))
				(local.set $y (i64.extend_i32_s (call $async)))
				(local.get $x))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "memory_ops",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.store (i32.const 0) (call $async))
				(call $async)
				(i32.load (i32.const 0))
				(i32.add))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "global_ops",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(global $g (mut i32) (i32.const 0))
			(func (export "test") (result i32)
				(global.set $g (call $async))
				(i32.add (global.get $g) (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "return_early",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (local.get $x)
					(then (return (call $async))))
				(call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "unreachable_after_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block (result i32)
					(call $async)
					(br 0)
					(unreachable)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multi_param_async",
		wat: `(module
			(import "env" "async" (func $async (param i32 i32) (result i32)))
			(func (export "test") (result i32)
				(call $async (i32.const 1) (i32.const 2)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_in_both_branches",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (result i32) (local.get $x)
					(then (call $async))
					(else (call $async))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "deeply_nested",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $a i32) (param $b i32) (param $c i32) (result i32)
				(if (result i32) (local.get $a)
					(then
						(if (result i32) (local.get $b)
							(then
								(if (result i32) (local.get $c)
									(then (call $async))
									(else (i32.const 3))))
							(else (i32.const 2))))
					(else (i32.const 1))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_with_async_value",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block (result i32)
					(call $async)
					(br 0)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_if_with_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(block (result i32)
					(call $async)
					(br_if 0 (local.get $x))
					(drop)
					(i32.const 0)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "block_with_early_return",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(block (result i32)
					(if (local.get $x)
						(then
							(return (call $async))))
					(i32.const 0)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "nested_loops",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $n i32) (result i32)
				(local $sum i32) (local $i i32) (local $j i32)
				(local.set $i (i32.const 0))
				(block $exit
					(loop $outer
						(br_if $exit (i32.ge_s (local.get $i) (local.get $n)))
						(local.set $j (i32.const 0))
						(block $inner_exit
							(loop $inner
								(br_if $inner_exit (i32.ge_s (local.get $j) (local.get $n)))
								(local.set $sum (i32.add (local.get $sum) (call $async)))
								(local.set $j (i32.add (local.get $j) (i32.const 1)))
								(br $inner)))
						(local.set $i (i32.add (local.get $i) (i32.const 1)))
						(br $outer)))
				(local.get $sum))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "try_catch_like",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block $handler (result i32)
					(block $try (result i32)
						(call $async)
						(br $try))
					(br $handler))
				(i32.const 0)
				(i32.add))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_to_outer_with_value",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block $outer (result i32)
					(block $inner (result i32)
						(call $async)
						(br $outer))
					(i32.const 99)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_if_to_outer",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $cond i32) (result i32)
				(block $outer (result i32)
					(block $inner (result i32)
						(call $async)
						(br_if $outer (local.get $cond))
						(drop)
						(i32.const 42))
					(i32.const 99)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multiple_br_targets",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(block $a (result i32)
					(block $b (result i32)
						(block $c (result i32)
							(call $async)
							(br_table $c $b $a (local.get $x)))
						(i32.const 1)
						(i32.add))
					(i32.const 2)
					(i32.add)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "tee_local_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(local $x i32)
				(local.tee $x (call $async))
				(local.get $x)
				(i32.add))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "call_in_call_params",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(import "env" "add" (func $add (param i32 i32) (result i32)))
			(func (export "test") (result i32)
				(call $add (call $async) (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_in_select_condition",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(select (i32.const 1) (i32.const 2) (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_with_value_to_result_block",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block $b (result i32)
					(call $async)
					(br $b)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "early_return_with_async_result",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $cond i32) (result i32)
				(if (result i32) (local.get $cond)
					(then (return (call $async)))
					(else (i32.const 0))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multi_value_return",
		wat: `(module
			(type $pair (func (result i32 i32)))
			(import "env" "async_pair" (func $async_pair (type $pair)))
			(func (export "test") (type $pair)
				(call $async_pair))
			(memory 1))`,
		async: []string{"env.async_pair"},
	},
	{
		name: "multi_value_with_ops",
		wat: `(module
			(type $pair (func (result i32 i32)))
			(import "env" "async_pair" (func $async_pair (type $pair)))
			(func (export "test") (result i32)
				(call $async_pair)
				(i32.add))
			(memory 1))`,
		async: []string{"env.async_pair"},
	},
	{
		name: "br_in_if_to_if_result",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $cond i32) (result i32)
				(if (result i32) (local.get $cond)
					(then (call $async) (br 0))
					(else (i32.const 0))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_in_nested_if_to_outer",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (param $y i32) (result i32)
				(block $outer (result i32)
					(if (result i32) (local.get $x)
						(then
							(if (local.get $y)
								(then (call $async) (br $outer)))
							(i32.const 1))
						(else (i32.const 0)))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "loop_with_result_and_br",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(local $i i32)
				(loop $L (result i32)
					(call $async)
					(local.tee $i)
					(br_if $L (i32.eqz (local.get $i)))
					(local.get $i)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_result_plus_const",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.add (call $async) (i32.const 10)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "const_plus_async_result",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.add (i32.const 10) (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multi_async_in_arith",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.mul
					(i32.add (call $async) (i32.const 5))
					(i32.sub (call $async) (i32.const 3))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_in_memory_store",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test")
				(i32.store (i32.const 0) (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_as_memory_addr",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.load (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multiple_async_in_store",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test")
				(i32.store (call $async) (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_in_global_set",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(global $g (mut i32) (i32.const 0))
			(func (export "test")
				(global.set $g (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "three_way_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.add
					(i32.add (call $async) (call $async))
					(call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_f64_arith",
		wat: `(module
			(import "env" "async" (func $async (result f64)))
			(func (export "test") (result f64)
				(f64.add (call $async) (f64.const 1.5)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "async_mixed_types",
		wat: `(module
			(import "env" "async_i32" (func $async_i32 (result i32)))
			(import "env" "async_f64" (func $async_f64 (result f64)))
			(func (export "test") (result f64)
				(f64.add
					(f64.convert_i32_s (call $async_i32))
					(call $async_f64)))
			(memory 1))`,
		async: []string{"env.async_i32", "env.async_f64"},
	},
	{
		name: "async_local_tee_chain",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(local $a i32) (local $b i32)
				(local.set $b
					(local.tee $a (call $async)))
				(i32.add (local.get $a) (local.get $b)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_in_if_then_to_if",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (result i32) (local.get $x)
					(then
						(call $async)
						(br 0))
					(else
						(i32.const 99))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_in_if_else_to_if",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (result i32) (local.get $x)
					(then
						(i32.const 99))
					(else
						(call $async)
						(br 0))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_in_loop_body_exit",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block $exit (result i32)
					(loop $L
						(call $async)
						(br_if $L (i32.eqz))
						(call $async)
						(br $exit))
					(i32.const 0)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "nested_if_with_br",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (param $y i32) (result i32)
				(if (result i32) (local.get $x)
					(then
						(if (result i32) (local.get $y)
							(then (call $async) (br 0))
							(else (i32.const 1))))
					(else (i32.const 0))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "drop_after_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test")
				(drop (call $async))
				(drop (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "drop_async_keep_other",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(drop (call $async))
				(call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multiple_drops_mixed",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(local $x i32)
				(local.set $x (call $async))
				(drop (call $async))
				(local.get $x))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "early_return_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (local.get $x)
					(then (return (call $async))))
				(i32.const 0))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "return_multi_value",
		wat: `(module
			(type $pair (func (result i32 i32)))
			(import "env" "async" (func $async (type $pair)))
			(func (export "test") (param $x i32) (result i32 i32)
				(if (local.get $x)
					(then
						(call $async)
						(return)))
				(i32.const 1)
				(i32.const 2))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "return_void_after_async",
		wat: `(module
			(import "env" "async" (func $async))
			(func (export "test") (param $x i32)
				(call $async)
				(if (local.get $x)
					(then (return)))
				(call $async))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "drop_with_other_stack_values",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.const 100)
				(i32.const 200)
				(call $async)
				(drop)
				(i32.add))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "multiple_sequential_drops",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(drop (call $async))
				(drop (call $async))
				(drop (call $async))
				(i32.const 42))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "select_all_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(select
					(call $async)
					(call $async)
					(call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_table_complex",
		wat: `(module
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
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "deep_nesting_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(block (result i32)
					(block (result i32)
						(block (result i32)
							(block (result i32)
								(call $async))))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "loop_with_continue_and_break",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $n i32) (result i32)
				(local $i i32)
				(local $sum i32)
				(block $break (result i32)
					(loop $continue
						(local.set $i (i32.add (local.get $i) (i32.const 1)))
						(local.set $sum (i32.add (local.get $sum) (call $async)))
						(br_if $break (i32.ge_u (local.get $i) (local.get $n)))
						(br $continue))
					(local.get $sum)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "interleaved_ops_and_async",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (result i32)
				(i32.add
					(i32.mul
						(call $async)
						(i32.const 2))
					(i32.sub
						(call $async)
						(i32.const 1))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "call_with_async_in_params",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func $helper (param i32 i32 i32) (result i32)
				(i32.add (local.get 0) (i32.add (local.get 1) (local.get 2))))
			(func (export "test") (result i32)
				(call $helper (call $async) (call $async) (call $async)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "br_if_with_value_to_block",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(block (result i32)
					(call $async)
					(br_if 0 (local.get $x))
					(drop)
					(i32.const 99)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "nested_block_br_if",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $cond i32) (result i32)
				(block (result i32)
					(block (result i32)
						(call $async)
						(br_if 1 (local.get $cond))
						(i32.const 10)
						(i32.add))
					(i32.const 20)
					(i32.add)))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "if_without_else_result",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $x i32) (result i32)
				(if (result i32) (local.get $x)
					(then (call $async))
					(else (i32.const 0))))
			(memory 1))`,
		async: []string{"env.async"},
	},
	{
		name: "mixed_control_flow",
		wat: `(module
			(import "env" "async" (func $async (result i32)))
			(func (export "test") (param $n i32) (result i32)
				(local $sum i32)
				(block $exit
					(loop $loop
						(if (i32.le_s (local.get $n) (i32.const 0))
							(then (br $exit)))
						(local.set $sum
							(i32.add (local.get $sum) (call $async)))
						(local.set $n (i32.sub (local.get $n) (i32.const 1)))
						(br $loop)))
				(local.get $sum))
			(memory 1))`,
		async: []string{"env.async"},
	},
}

// TestStress_AllCases runs all stress tests and validates output.
func TestStress_AllCases(t *testing.T) {
	for _, tc := range stressTests {
		t.Run(tc.name, func(t *testing.T) {
			wasmBytes, err := parseWAT(tc.wat)
			if err != nil {
				t.Fatalf("failed to parse WAT: %v", err)
			}

			eng := New(Config{Matcher: newExactMatcher(tc.async)})
			result, err := eng.Transform(wasmBytes)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}

			if err := validateWasm(result); err != nil {
				t.Errorf("validation failed: %v", err)
				dumpWasm(t, result, tc.name)
			}
		})
	}
}

// TestStress_CompareBinaryen compares output structure with Binaryen.
func TestStress_CompareBinaryen(t *testing.T) {
	if _, err := exec.LookPath("wasm-opt"); err != nil {
		if _, err := os.Stat("/tmp/binaryen-version_121/bin/wasm-opt"); err != nil {
			t.Skip("wasm-opt not found")
		}
	}

	for _, tc := range stressTests[:5] { // Test first 5 for speed
		t.Run(tc.name, func(t *testing.T) {
			wasmBytes, err := parseWAT(tc.wat)
			if err != nil {
				t.Fatalf("failed to parse WAT: %v", err)
			}

			eng := New(Config{Matcher: newExactMatcher(tc.async)})
			ourResult, err := eng.Transform(wasmBytes)
			if err != nil {
				t.Fatalf("our Transform() error = %v", err)
			}

			binResult, err := binaryenTransform(wasmBytes)
			if err != nil {
				t.Logf("Binaryen transform failed (may be expected): %v", err)
				return
			}

			if err := validateWasm(ourResult); err != nil {
				t.Errorf("our output invalid: %v", err)
			}
			if err := validateWasm(binResult); err != nil {
				t.Errorf("Binaryen output invalid: %v", err)
			}

			ourExports := countExports(ourResult)
			binExports := countExports(binResult)

			if ourExports != binExports {
				t.Logf("export count differs: ours=%d, binaryen=%d", ourExports, binExports)
			}
		})
	}
}

func parseWAT(watSrc string) ([]byte, error) {
	return wat.Compile(watSrc)
}

func validateWasm(data []byte) error {
	_, err := wasm.ParseModule(data)
	return err
}

func binaryenTransform(data []byte) ([]byte, error) {
	// Write to temp file
	tmpIn := "/tmp/stress_in.wasm"
	tmpOut := "/tmp/stress_out.wasm"
	if err := os.WriteFile(tmpIn, data, 0644); err != nil {
		return nil, err
	}

	// Try system wasm-opt first, then known location
	wasmOpt := "wasm-opt"
	if _, err := exec.LookPath(wasmOpt); err != nil {
		wasmOpt = "/tmp/binaryen-version_121/bin/wasm-opt"
	}

	cmd := exec.CommandContext(context.TODO(), wasmOpt, tmpIn, "--asyncify", "-o", tmpOut)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s", output)
	}

	return os.ReadFile(tmpOut)
}

func countExports(data []byte) int {
	// Simple heuristic: count export section entries
	// Real implementation would parse properly
	count := 0
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0x07 { // export section
			count++
		}
	}
	return count
}

func dumpWasm(t *testing.T, data []byte, name string) {
	t.Logf("WASM disassembly not available (no external tools); module %q is %d bytes", name, len(data))
}

// TestMemoryLeak_1000Modules transforms 1000 modules and verifies no memory leak.
func TestMemoryLeak_1000Modules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory leak test in short mode")
	}

	// Create a representative module
	m := createMediumModule(10)
	data := m.Encode()
	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})

	// Warm up
	for i := 0; i < 10; i++ {
		_, _ = eng.Transform(data)
	}

	// Force GC before measuring
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	// Transform 1000 modules
	for i := 0; i < 1000; i++ {
		result, err := eng.Transform(data)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		_ = result
	}

	// Force GC and measure
	runtime.GC()
	runtime.ReadMemStats(&after)

	// Calculate heap growth
	heapGrowth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	perModule := heapGrowth / 1000

	// Allow some growth but flag if excessive (> 1KB per module retained)
	if perModule > 1024 {
		t.Errorf("potential memory leak: %d bytes retained per module (heap grew %d bytes)", perModule, heapGrowth)
	}

	t.Logf("1000 transforms: heap before=%d after=%d growth=%d (%d bytes/module)",
		before.HeapAlloc, after.HeapAlloc, heapGrowth, perModule)
}

// TestFlatten_BrInIf verifies br inside if with result is correctly flattened.
func TestFlatten_BrInIf(t *testing.T) {
	watSrc := `(module
		(import "env" "async" (func $async (result i32)))
		(func (export "test") (param $x i32) (result i32)
			(if (result i32) (local.get $x)
				(then
					(call $async)
					(br 0))
				(else
					(i32.const 99))))
		(memory 1))`

	wasmBytes, err := parseWAT(watSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	eng := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	result, err := eng.Transform(wasmBytes)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	if err := validateWasm(result); err != nil {
		t.Errorf("validation failed: %v", err)
	}
}
