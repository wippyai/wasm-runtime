package engine

import (
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// 1. Static pure table skips instrumentation.
func TestIndirectAnalysis_StaticPureTableSkipsInstrumentation(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table 1 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem (i32.const 0) $pure)
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}
	if len(marked) != 0 {
		t.Fatalf("pure table must skip instrumentation, got marked: %v", marked)
	}
}

// 2. Async import target direct + transitive + indirect cycle still suspends.
func TestIndirectAnalysis_AsyncImportTargetDirectTransitiveIndirectCycle(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (param i32) (result i32)))
  (import "env" "async" (func $async (type $t)))
  (table 1 funcref)
  (func $c (type $t) (param $n i32) (result i32)
    local.get $n
    i32.eqz
    if (result i32)
      local.get $n
      call $d
    else
      local.get $n
      i32.const 1
      i32.sub
      call $a
    end)
  (func $d (type $t) (param $n i32) (result i32)
    local.get $n
    call $async)
  (elem (i32.const 0) $c)
  (func $b (type $t) (param $n i32) (result i32)
    local.get $n
    i32.const 0
    call_indirect (type $t))
  (func $a (type $t) (param $n i32) (result i32)
    local.get $n
    call $b)
  (func (export "run") (type $t) (param $n i32) (result i32)
    local.get $n
    call $a)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// func 0 is async import
	// internal funcs are 1 ($c), 2 ($d), 3 ($b), 4 ($a), 5 (run)
	numImported := uint32(m.NumImportedFuncs())
	for i := uint32(1); i <= 5; i++ {
		funcIdx := numImported + (i - 1)
		if !marked[funcIdx] {
			t.Fatalf("cycle func %d (idx %d) must be marked, got marked: %v", i, funcIdx, marked)
		}
	}
}

// 3. Exported table fallback even with zero local async imports.
func TestIndirectAnalysis_ExportedTableFallback_ZeroLocalAsyncImports(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table (export "table") 1 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem (i32.const 0) $pure)
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// Funcs: 0 ($pure), 1 ($caller), 2 (run)
	if !marked[1] {
		t.Fatalf("caller of indirect call on exported table must be marked, got: %v", marked)
	}
	if !marked[2] {
		t.Fatalf("transitive caller of exported table call must be marked, got: %v", marked)
	}
}

// 4. Imported table fallback even with zero local async imports.
func TestIndirectAnalysis_ImportedTableFallback_ZeroLocalAsyncImports(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (import "env" "table" (table 1 funcref))
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// Funcs: 0 ($caller), 1 (run)
	if !marked[0] {
		t.Fatalf("caller of indirect call on imported table must be marked, got: %v", marked)
	}
	if !marked[1] {
		t.Fatalf("transitive caller of imported table call must be marked, got: %v", marked)
	}
}

// 5. Mutation fallback (table.set, table.grow, table.fill, table.copy).
func TestIndirectAnalysis_MutationFallback_TableSet(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table 2 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem (i32.const 0) $pure)
  (func $mutator
    i32.const 1
    ref.null func
    table.set 0)
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// Funcs: 0 ($pure), 1 ($mutator), 2 ($caller), 3 (run)
	if !marked[2] {
		t.Fatalf("caller on mutated table (table.set) must be marked, got: %v", marked)
	}
	if !marked[3] {
		t.Fatalf("transitive caller on mutated table must be marked, got: %v", marked)
	}
}

func TestIndirectAnalysis_MutationFallback_TableGrow(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table 1 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem (i32.const 0) $pure)
  (func $mutator
    (drop (table.grow 0 (ref.null func) (i32.const 1))))
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// Func 2 ($caller), Func 3 (run)
	if !marked[2] || !marked[3] {
		t.Fatalf("mutated table (table.grow) must mark indirect caller and transitive callers: %v", marked)
	}
}

func TestIndirectAnalysis_MutationFallback_TableFill(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table 2 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem (i32.const 0) $pure)
  (func $mutator
    (table.fill 0 (i32.const 0) (ref.null func) (i32.const 1)))
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	if !marked[2] || !marked[3] {
		t.Fatalf("mutated table (table.fill) must mark indirect caller and transitive callers: %v", marked)
	}
}

func TestIndirectAnalysis_MutationFallback_TableCopy(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table 2 funcref)
  (table 2 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem 0 (i32.const 0) $pure)
  (elem 1 (i32.const 0) $pure)
  (func $mutator
    (table.copy 0 1 (i32.const 0) (i32.const 0) (i32.const 1)))
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect 0 (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	if !marked[2] || !marked[3] {
		t.Fatalf("mutated table (table.copy) must mark dst indirect caller and transitive callers: %v", marked)
	}
}

// 6. Duplicate equivalent types: structurally identical type indices match.
func TestIndirectAnalysis_DuplicateEquivalentTypes(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t0 (func (param i32) (result i32)))
  (type $t1 (func (param i32) (result i32)))
  (import "env" "async" (func $async (type $t0)))
  (table 1 funcref)
  (func $worker (type $t0) (param $x i32) (result i32)
    local.get $x
    call $async)
  (elem (i32.const 0) $worker)
  (func $caller (type $t1) (param $x i32) (result i32)
    local.get $x
    i32.const 0
    call_indirect (type $t1))
  (func (export "run") (type $t1) (param $x i32) (result i32)
    local.get $x
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// func 0 is async import
	// internal func 1 is $worker, func 2 is $caller, func 3 is run
	if !marked[1] || !marked[2] || !marked[3] {
		t.Fatalf("duplicate equivalent type signatures must match indirect target: %v", marked)
	}
}

// 7. Table isolation across multiple tables.
func TestIndirectAnalysis_TableIsolation_Multi(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table 1 funcref)
  (table (export "tbl1") 1 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem 0 (i32.const 0) $pure)
  (elem 1 (i32.const 0) $pure)
  (func $call_tbl0 (result i32)
    i32.const 0
    call_indirect 0 (type $t))
  (func $call_tbl1 (result i32)
    i32.const 0
    call_indirect 1 (type $t))
  (func (export "run_pure") (result i32)
    call $call_tbl0)
  (func (export "run_open") (result i32)
    call $call_tbl1)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// Funcs: 0 ($pure), 1 ($call_tbl0), 2 ($call_tbl1), 3 (run_pure), 4 (run_open)
	// $call_tbl0 and run_pure target closed pure table 0 -> NOT marked
	if marked[1] || marked[3] {
		t.Fatalf("closed table calls must be isolated and not marked: %v", marked)
	}
	// $call_tbl1 and run_open target exported table 1 -> MUST be marked
	if !marked[2] || !marked[4] {
		t.Fatalf("exported table calls must be marked conservative: %v", marked)
	}
}

// 8. CallRef conservative fallback.
func TestIndirectAnalysis_CallRefConservativeFallback(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Funcs: []uint32{0, 0},
		Code: []wasm.FuncBody{
			// Func 0: contains call_ref
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
			// Func 1: caller of Func 0
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	e := New(Config{})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	if !marked[0] || !marked[1] {
		t.Fatalf("call_ref must trigger conservative transformation for caller and callers: %v", marked)
	}
}

// 9. AddList and OnlyList support without silently losing forced async roots.
func TestIndirectAnalysis_AddListAndOnlyList_PreservesForcedRoots(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (import "env" "async" (func $async (type $t)))
  (func (export "forced") (type $t) (result i32) i32.const 1)
  (func (export "target") (type $t) (result i32) call $async)
  (func (export "other") (type $t) (result i32) call $async)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	// forced is in AddList, target is in OnlyList
	// func 0 is async import
	// func 1 is "forced"
	// func 2 is "target"
	// func 3 is "other"
	e := New(Config{
		Matcher:  newExactMatcher([]string{"env.async"}),
		AddList:  &testFuncMatcher{names: map[string]bool{"forced": true}},
		OnlyList: &testFuncMatcher{names: map[string]bool{"target": true}},
	})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// "forced" (from AddList) must be preserved and NOT dropped by OnlyList!
	if !marked[1] {
		t.Errorf("forced root 'forced' was silently lost by OnlyList filter: %v", marked)
	}
	// "target" (from OnlyList) must be transformed
	if !marked[2] {
		t.Errorf("selected function 'target' must be marked: %v", marked)
	}
	// "other" (not in OnlyList) must NOT be transformed
	if marked[3] {
		t.Errorf("unselected function 'other' must NOT be marked: %v", marked)
	}
}

// 10. Mutation fallback: table.init
func TestIndirectAnalysis_MutationFallback_TableInit(t *testing.T) {
	data, err := wat.Compile(`(module
  (type $t (func (result i32)))
  (table 2 funcref)
  (func $pure (type $t) (result i32) i32.const 42)
  (elem $e func $pure)
  (func $mutator
    (table.init 0 $e (i32.const 0) (i32.const 0) (i32.const 1)))
  (func $caller (type $t) (result i32)
    i32.const 0
    call_indirect 0 (type $t))
  (func (export "run") (type $t) (result i32)
    call $caller)
)`)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}
	m, err := wasm.ParseModule(data)
	if err != nil {
		t.Fatalf("wasm.ParseModule: %v", err)
	}

	e := New(Config{Matcher: newExactMatcher([]string{"env.async"})})
	marked, err := e.findAsyncFuncs(m)
	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	// Funcs: 0 ($pure), 1 ($mutator), 2 ($caller), 3 (run)
	if !marked[2] {
		t.Fatalf("caller on table mutated by table.init must be marked conservative, got: %v", marked)
	}
	if !marked[3] {
		t.Fatalf("transitive caller on table mutated by table.init must be marked, got: %v", marked)
	}
}

// 11. Result-only extended type with ExtValKindRef triggers conservative fallback.
func TestIndirectAnalysis_ResultOnlyExtendedType_ConservativeFallback(t *testing.T) {
	// A function type with 0 params and a result of reference type with heap type.
	// ExtParams is empty, but ExtResults has ExtValKindRef.
	// Previous code missed this because it only checked len(ExtParams) > 0.
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{
				Params:    nil,
				Results:   []wasm.ValType{wasm.ValRefNull},
				ExtParams: nil,
				ExtResults: []wasm.ExtValType{
					{
						Kind:    wasm.ExtValKindRef,
						ValType: wasm.ValRefNull,
						RefType: wasm.RefType{Nullable: true, HeapType: 0},
					},
				},
			},
		},
		Tables: []wasm.TableType{
			{Limits: wasm.Limits{Min: 1}, ElemType: byte(wasm.ValFuncRef)},
		},
		Elements: []wasm.Element{
			{
				Flags:    0, // active table 0
				TableIdx: 0,
				FuncIdxs: []uint32{0},
			},
		},
		Funcs: []uint32{0, 0},
		Code: []wasm.FuncBody{
			// Func 0: dummy target
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
			// Func 1: caller with call_indirect expecting type 0
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
					{Opcode: wasm.OpDrop},
					{Opcode: wasm.OpRefNull, Imm: wasm.RefNullImm{HeapType: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	analysis, err := AnalyzeIndirectCalls(m, false)
	if err != nil {
		t.Fatalf("AnalyzeIndirectCalls: %v", err)
	}

	// Table 0 contains a function with unsupported ExtValKindRef, so it must NOT be closed!
	if analysis.Tables[0].IsClosed {
		t.Fatalf("table containing unsupported ExtValKindRef must not be closed")
	}

	// Caller (func 1) must be marked in conservative roots because table is not closed
	// and call site uses unsupported ExtValKindRef!
	if !analysis.ConservativeRoots[1] {
		t.Fatalf("caller with result-only extended type must be marked in ConservativeRoots, got: %v", analysis.ConservativeRoots)
	}
}

// 12. Declared subtyping (TypeDefKindSub with parents) triggers conservative fallback.
func TestIndirectAnalysis_UnsupportedGCTypes_SubParents_ConservativeFallback(t *testing.T) {
	ft := wasm.FuncType{
		Params:  []wasm.ValType{wasm.ValI32},
		Results: []wasm.ValType{wasm.ValI32},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					Parents: []uint32{0},
					Final:   false,
					CompType: wasm.CompType{
						Kind: wasm.CompKindFunc,
						Func: &ft,
					},
				},
			},
		},
		Tables: []wasm.TableType{
			{Limits: wasm.Limits{Min: 1}, ElemType: byte(wasm.ValFuncRef)},
		},
		Elements: []wasm.Element{
			{
				Flags:    0,
				TableIdx: 0,
				FuncIdxs: []uint32{0},
			},
		},
		Funcs: []uint32{0, 0},
		Code: []wasm.FuncBody{
			// Func 0
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
			// Func 1: caller with call_indirect
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	analysis, err := AnalyzeIndirectCalls(m, false)
	if err != nil {
		t.Fatalf("AnalyzeIndirectCalls: %v", err)
	}

	// Module with declared subtyping must NOT have closed tables
	if analysis.Tables[0].IsClosed {
		t.Fatalf("module with declared subtyping must fall back to open table")
	}
	if !analysis.ConservativeRoots[1] {
		t.Fatalf("caller of indirect call under subtyping must be conservative root")
	}
}

// 13. Recursive type groups (TypeDefKindRec) trigger conservative fallback.
func TestIndirectAnalysis_UnsupportedGCTypes_RecType_ConservativeFallback(t *testing.T) {
	ft := wasm.FuncType{
		Params:  []wasm.ValType{wasm.ValI32},
		Results: []wasm.ValType{wasm.ValI32},
	}
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindRec,
				Rec: &wasm.RecType{
					Types: []wasm.SubType{
						{
							Final: true,
							CompType: wasm.CompType{
								Kind: wasm.CompKindFunc,
								Func: &ft,
							},
						},
					},
				},
			},
		},
		Tables: []wasm.TableType{
			{Limits: wasm.Limits{Min: 1}, ElemType: byte(wasm.ValFuncRef)},
		},
		Elements: []wasm.Element{
			{
				Flags:    0,
				TableIdx: 0,
				FuncIdxs: []uint32{0},
			},
		},
		Funcs: []uint32{0, 0},
		Code: []wasm.FuncBody{
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
			{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}},
					{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			},
		},
	}

	analysis, err := AnalyzeIndirectCalls(m, false)
	if err != nil {
		t.Fatalf("AnalyzeIndirectCalls: %v", err)
	}

	if analysis.Tables[0].IsClosed {
		t.Fatalf("module with recursive type groups must fall back to open table")
	}
	if !analysis.ConservativeRoots[1] {
		t.Fatalf("caller of indirect call under recursive types must be conservative root")
	}
}

// 14. Synthetic scale: 1,000 table functions, 50 distinct signatures,
// 100 callers with 20 repeated indirect call sites each (2,000 indirect sites).
// Verifies bounded performance and O(callers + targets) group node scaling.
func TestIndirectAnalysis_SyntheticScale(t *testing.T) {
	const numSignatures = 50
	const numTableFuncs = 1000
	const numCallers = 100
	const numSitesPerCaller = 20

	// Define numSignatures distinct FuncTypes
	types := make([]wasm.FuncType, numSignatures)
	for s := 0; s < numSignatures; s++ {
		// Each signature has s dummy i32 params and 1 i32 result
		params := make([]wasm.ValType, s)
		for p := range params {
			params[p] = wasm.ValI32
		}
		types[s] = wasm.FuncType{
			Params:  params,
			Results: []wasm.ValType{wasm.ValI32},
		}
	}

	// 1 async import: imported func 0 (signature 0: () -> i32)
	imports := []wasm.Import{
		{
			Module: "env",
			Name:   "async",
			Desc:   wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0},
		},
	}

	totalInternal := numTableFuncs + numCallers
	funcs := make([]uint32, totalInternal)
	code := make([]wasm.FuncBody, totalInternal)

	// Table element indices: internal funcs 0..999 have funcIdx = 1 + i
	elemFuncIdxs := make([]uint32, numTableFuncs)
	for i := 0; i < numTableFuncs; i++ {
		funcIdx := uint32(1 + i)
		elemFuncIdxs[i] = funcIdx
		typeIdx := uint32(i % numSignatures)
		funcs[i] = typeIdx

		if i == 0 {
			// Calls async import (funcIdx 0)
			code[i] = wasm.FuncBody{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
					{Opcode: wasm.OpEnd},
				}),
			}
		} else {
			code[i] = wasm.FuncBody{
				Code: wasm.EncodeInstructions([]wasm.Instruction{
					{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 42}},
					{Opcode: wasm.OpEnd},
				}),
			}
		}
	}

	// Callers: internal funcs numTableFuncs .. totalInternal-1
	for c := 0; c < numCallers; c++ {
		localIdx := numTableFuncs + c
		// Caller 0 calls signature 0; others call signatures 1..49
		sig := uint32(0)
		if c > 0 {
			sig = uint32(1 + (c % (numSignatures - 1)))
		}
		funcs[localIdx] = 0 // caller itself has type 0

		// Generate numSitesPerCaller repeated indirect calls to the same signature
		var instrs []wasm.Instruction
		for site := 0; site < numSitesPerCaller; site++ {
			// Push dummy params for sig
			for p := uint32(0); p < sig; p++ {
				instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: int32(p)}})
			}
			// Push table index 0
			instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 0}})
			// call_indirect
			instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TableIdx: 0, TypeIdx: sig}})
			if site < numSitesPerCaller-1 {
				instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpDrop})
			}
		}
		instrs = append(instrs, wasm.Instruction{Opcode: wasm.OpEnd})
		code[localIdx] = wasm.FuncBody{Code: wasm.EncodeInstructions(instrs)}
	}

	m := &wasm.Module{
		Types:   types,
		Imports: imports,
		Tables: []wasm.TableType{
			{Limits: wasm.Limits{Min: uint64(numTableFuncs)}, ElemType: byte(wasm.ValFuncRef)},
		},
		Elements: []wasm.Element{
			{
				Flags:    0,
				TableIdx: 0,
				FuncIdxs: elemFuncIdxs,
			},
		},
		Funcs: funcs,
		Code:  code,
	}

	e := New(Config{
		Matcher: newExactMatcher([]string{"env.async"}),
	})

	start := time.Now()
	marked, err := e.findAsyncFuncs(m)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("findAsyncFuncs: %v", err)
	}

	t.Logf("Synthetic scale: 1,000 table funcs, %d callers, 2,000 indirect sites analyzed in %v",
		numCallers, elapsed)

	// Must complete well within 500ms (typically under 10ms with group nodes and map canonizer)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("analysis exceeded bounded scale time limit: took %v", elapsed)
	}

	// Verify correctness:
	// - Import 0 is marked
	// - Table func 0 (funcIdx 1) is marked (calls async import)
	// - Caller 0 (funcIdx 1 + numTableFuncs) is marked (calls signature 0)
	// - Other callers (caller 1..99) target pure signatures and MUST NOT be marked!
	if !marked[0] {
		t.Errorf("async import 0 must be marked")
	}
	if !marked[1] {
		t.Errorf("table func 0 (calls async import) must be marked")
	}
	caller0Idx := uint32(1 + numTableFuncs)
	if !marked[caller0Idx] {
		t.Errorf("caller 0 (targets signature 0) must be marked")
	}

	// Callers 1..99 must NOT be marked
	for c := 1; c < numCallers; c++ {
		callerIdx := uint32(1 + numTableFuncs + c)
		if marked[callerIdx] {
			t.Errorf("caller %d (targets pure signature) should NOT be marked", c)
		}
	}
}
