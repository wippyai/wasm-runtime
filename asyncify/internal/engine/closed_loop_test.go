package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func closedIncrementLoop() []wasm.Instruction {
	return []wasm.Instruction{
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32LtU},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 1}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
	}
}

func transformYieldingBody(t *testing.T, locals []wasm.LocalEntry, body []wasm.Instruction) []wasm.Instruction {
	t.Helper()
	encoded := append(append([]wasm.Instruction{}, body...), wasm.Instruction{Opcode: wasm.OpEnd})
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{},
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Module: "env", Name: "yield", Desc: wasm.ImportDesc{Kind: 0, TypeIdx: 0}},
		},
		Funcs:    []uint32{1},
		Memories: []wasm.MemoryType{{Limits: wasm.Limits{Min: 1}}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}},
			{Type: wasm.GlobalType{ValType: wasm.ValI32, Mutable: true}},
		},
		Code: []wasm.FuncBody{{
			Locals: locals,
			Code:   wasm.EncodeInstructions(encoded),
		}},
	}
	ft := NewFunctionTransformer(DefaultRegistry(), m, GlobalIndices{StateGlobal: 0, DataGlobal: 1}, 0, false)
	if err := ft.Transform(1, &m.Code[0], map[uint32]bool{0: true}); err != nil {
		t.Fatal(err)
	}
	instrs, err := wasm.DecodeInstructions(m.Code[0].Code)
	if err != nil {
		t.Fatal(err)
	}
	return instrs
}

func matchLoopEnd(instrs []wasm.Instruction, start int) (int, bool) {
	if start < 0 || start >= len(instrs) || instrs[start].Opcode != wasm.OpLoop {
		return 0, false
	}
	depth := 0
	for i := start; i < len(instrs); i++ {
		switch instrs[i].Opcode {
		case wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpTry, wasm.OpTryTable:
			depth++
		case wasm.OpEnd:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func firstLoopSpan(t *testing.T, instrs []wasm.Instruction) (int, int) {
	t.Helper()
	for i := range instrs {
		if instrs[i].Opcode == wasm.OpLoop {
			end, ok := matchLoopEnd(instrs, i)
			if !ok {
				t.Fatal("loop without matching end")
			}
			return i, end
		}
	}
	t.Fatal("no loop in transformed body")
	return 0, 0
}

func stateGetsIn(instrs []wasm.Instruction, start, end int, globalIdx uint32) int {
	n := 0
	for i := start; i <= end && i < len(instrs); i++ {
		if instrs[i].Opcode != wasm.OpGlobalGet {
			continue
		}
		imm, ok := instrs[i].Imm.(wasm.GlobalImm)
		if ok && imm.GlobalIdx == globalIdx {
			n++
		}
	}
	return n
}

func TestTransform_PreservesClosedVoidLoopBeforeYield(t *testing.T) {
	body := append(closedIncrementLoop(),
		wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
	)
	instrs := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)
	start, end := firstLoopSpan(t, instrs)
	if got := stateGetsIn(instrs, start, end, 0); got != 0 {
		t.Fatalf("preserved loop contains %d state global.gets, want 0", got)
	}
	if end-start+1 != len(closedIncrementLoop()) {
		t.Fatalf("preserved loop span %d instructions, want %d", end-start+1, len(closedIncrementLoop()))
	}
}

func TestTransform_PreservesClosedVoidLoopAfterYield(t *testing.T) {
	body := []wasm.Instruction{{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}}}
	body = append(body, closedIncrementLoop()...)
	body = append(body, wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}})
	instrs := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)
	start, end := firstLoopSpan(t, instrs)
	if got := stateGetsIn(instrs, start, end, 0); got != 0 {
		t.Fatalf("preserved loop contains %d state global.gets, want 0", got)
	}
	if end-start+1 != len(closedIncrementLoop()) {
		t.Fatalf("preserved loop span %d instructions, want %d", end-start+1, len(closedIncrementLoop()))
	}
}

func TestTransform_PreservesClosedVoidLoopWithTee(t *testing.T) {
	body := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32LtU},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpLocalTee, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpDrop},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 1}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
	}
	instrs := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)
	start, end := firstLoopSpan(t, instrs)
	if got := stateGetsIn(instrs, start, end, 0); got != 0 {
		t.Fatalf("preserved tee loop contains %d state global.gets, want 0", got)
	}
	hasTee := false
	for i := start; i <= end; i++ {
		if instrs[i].Opcode == wasm.OpLocalTee {
			hasTee = true
			break
		}
	}
	if !hasTee {
		t.Fatal("preserved loop dropped original local.tee")
	}
}

func TestTransform_TempLiveAcrossClosedLoop(t *testing.T) {
	body := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 5}},
	}
	body = append(body, closedIncrementLoop()...)
	body = append(body,
		wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		wasm.Instruction{Opcode: wasm.OpI32Add},
	)
	instrs := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)
	start, end := firstLoopSpan(t, instrs)
	if got := stateGetsIn(instrs, start, end, 0); got != 0 {
		t.Fatalf("preserved loop contains %d state global.gets, want 0", got)
	}
}

func TestTransform_PreservesLoopExitWithinClosedBlock(t *testing.T) {
	body := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32GeU},
		{Opcode: wasm.OpBrIf, Imm: wasm.BranchImm{LabelIdx: 1}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
	}
	instrs := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)
	start, end := firstLoopSpan(t, instrs)
	if got := stateGetsIn(instrs, start, end, 0); got != 0 {
		t.Fatal("loop exit confined to copied block acquired state guards")
	}
}

func TestTransform_RejectsAsyncInsideLoop(t *testing.T) {
	body := []wasm.Instruction{
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32LtU},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpI32Const, Imm: wasm.I32Imm{Value: 1}},
		{Opcode: wasm.OpI32Add},
		{Opcode: wasm.OpLocalSet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 1}},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpEnd},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
	}
	instrs := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)
	start, end := firstLoopSpan(t, instrs)
	if got := stateGetsIn(instrs, start, end, 0); got == 0 {
		t.Fatal("async loop was preserved")
	}
}

func TestTransform_PreservesInnerClosedLoopInsideAsyncLoop(t *testing.T) {
	body := []wasm.Instruction{
		{Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
		{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 0}},
		{Opcode: wasm.OpI32GeU},
		{Opcode: wasm.OpIf, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 2}},
		{Opcode: wasm.OpEnd},
	}
	body = append(body, closedIncrementLoop()...)
	body = append(body,
		wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		wasm.Instruction{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 0}},
		wasm.Instruction{Opcode: wasm.OpEnd},
		wasm.Instruction{Opcode: wasm.OpEnd},
		wasm.Instruction{Opcode: wasm.OpLocalGet, Imm: wasm.LocalImm{LocalIdx: 1}},
	)
	instrs := transformYieldingBody(t, []wasm.LocalEntry{{Count: 1, ValType: wasm.ValI32}}, body)

	var innerPreserved bool
	var outerHasState bool
	for i := 0; i < len(instrs); i++ {
		if instrs[i].Opcode != wasm.OpLoop {
			continue
		}
		end, ok := matchLoopEnd(instrs, i)
		if !ok {
			continue
		}
		gets := stateGetsIn(instrs, i, end, 0)
		if gets == 0 {
			if end-i+1 == len(closedIncrementLoop()) {
				innerPreserved = true
			}
		} else {
			outerHasState = true
		}
	}
	if !innerPreserved {
		t.Fatal("inner closed loop was not preserved")
	}
	if !outerHasState {
		t.Fatal("outer async loop was preserved")
	}
}

func TestClosedVoidLoopSpans_Direct(t *testing.T) {
	ft := NewFunctionTransformer(DefaultRegistry(), &wasm.Module{}, GlobalIndices{}, 0, false)
	loop := closedIncrementLoop()
	asyncAt := map[int]bool{}
	spans := ft.closedVoidRegionSpans(loop, asyncAt)
	if len(spans) != 1 {
		t.Fatalf("eligible loop spans = %v, want 1", spans)
	}
	if end, ok := spans[0]; !ok || end != len(loop)-1 {
		t.Fatalf("span = %v, want [0]=%d", spans, len(loop)-1)
	}

	escaping := []wasm.Instruction{
		{Opcode: wasm.OpLoop, Imm: wasm.BlockImm{Type: wasm.BlockTypeVoid}},
		{Opcode: wasm.OpBr, Imm: wasm.BranchImm{LabelIdx: 1}},
		{Opcode: wasm.OpEnd},
	}
	if got := ft.closedVoidRegionSpans(escaping, asyncAt); len(got) != 0 {
		t.Fatalf("escaping branch spans = %v, want none", got)
	}

	withAsync := append([]wasm.Instruction{}, loop...)
	asyncAt[6] = true
	if got := ft.closedVoidRegionSpans(withAsync, asyncAt); len(got) != 0 {
		t.Fatalf("async-inside spans = %v, want none", got)
	}
}
