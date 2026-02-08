// Package wat provides WebAssembly Text format parsing.
//
// This package compiles WAT (WebAssembly Text) format into binary WASM,
// enabling human-readable module definitions for testing and examples.
//
// Basic usage:
//
//	wasm, err := wat.Compile(`(module
//		(func (export "add") (param i32 i32) (result i32)
//			(i32.add (local.get 0) (local.get 1)))
//	)`)
//
// Supported WASM 2.0 features:
//   - Functions with params, results, locals (named and indexed)
//   - Multi-value returns and block parameters
//   - Memory, global, table declarations with imports/exports
//   - Control flow: if/then/else, loop, block, br, br_if, br_table, return
//   - call, call_indirect with type references
//   - Integer ops: i32/i64 arithmetic, comparisons, bitwise, shifts, rotations
//   - Float ops: f32/f64 arithmetic, comparisons, math functions
//   - Memory: load/store for all types with offset/align
//   - Bulk memory: memory.copy, memory.fill, memory.init, data.drop
//   - Table ops: table.get/set/grow/size/fill/copy/init, elem.drop
//   - Reference types: funcref, externref, ref.null, ref.func, ref.is_null
//   - Saturating truncations: i32/i64.trunc_sat_f32/f64_s/u
//   - Sign extension: i32.extend8_s, i32.extend16_s, i64.extend*_s
//   - Select with type annotation
//   - Data and elem sections (active, passive, declarative)
//   - Comments: line (;;) and block (; ;)
//
// Not supported: SIMD (v128), threads/atomics, exception handling, GC types.
package wat
