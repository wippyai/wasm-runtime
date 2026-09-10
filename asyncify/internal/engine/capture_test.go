package engine

import (
	"slices"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

type capturePlanState struct {
	transformer *FunctionTransformer
	program     *executionPlan
	sites       []ir.SuspensionSite
	locals      []wasm.ValType
}

func capturePlanFixture(t *testing.T, body string) capturePlanState {
	t.Helper()
	raw, err := wat.Compile(`(module (import "env" "yield" (func $yield)) (func (param i32) (result i32) ` + body + `))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.ParseModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	ft := NewFunctionTransformer(DefaultRegistry(), module, GlobalIndices{}, 0, false)
	control, err := ir.Prepare(module.Code[0].Code, module, semantics.NewCalls(module), func(semantics.CallOperation) bool { return true }, []wasm.ValType{wasm.ValI32})
	if err != nil {
		t.Fatal(err)
	}
	locals := []wasm.ValType{wasm.ValI32}
	values, err := ir.PlanValues(control, func(instr wasm.Instruction) (ir.OperandShape, error) { return ft.sourceOperandShape(instr, locals) })
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := ir.Linearize(values, &ir.LinearizeConfig{StateRewinding: 2, AllocLocal: func(vt wasm.ValType) uint32 { index := uint32(len(locals)); locals = append(locals, vt); return index }})
	if err != nil {
		t.Fatal(err)
	}
	actions, err := lowered.CopyActions()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := newExecutionPlan(actions)
	if err != nil {
		t.Fatal(err)
	}
	return capturePlanState{transformer: ft, program: plan, sites: lowered.SuspensionSites(), locals: locals}
}

func TestCapturePlanMapsCallsLivenessAndFollowingLoop(t *testing.T) {
	fixture := capturePlanFixture(t, `i32.const 7 i32.const 1
 if (param i32) (result i32) call $yield local.get 0 i32.add else end
 drop loop i32.const 9 local.set 0 end local.get 0`)
	ft, p, sites, locals := fixture.transformer, fixture.program, fixture.sites, fixture.locals
	if len(sites) != 1 {
		t.Fatalf("sites=%d", len(sites))
	}
	site := sites[0]
	step, err := p.sourceStep(site.ActionIndex)
	if err != nil {
		t.Fatal(err)
	}
	if step == site.ActionIndex || p.steps[step].Opcode != wasm.OpCall {
		t.Fatalf("call action %d mapped to step %d", site.ActionIndex, step)
	}
	live := NewLivenessAnalyzer(1, len(locals)-1).ComputeForCallSites(p.steps, []int{step})
	if !slices.Contains(live[step], uint32(0)) {
		t.Fatal("capture expansion lost live source parameter at call")
	}
	excluded := map[int]bool{site.ActionIndex: true}
	for index, action := range p.actions {
		if domain, ok := action.Domain(); ok && domain == semantics.RewindRouting {
			excluded[index] = true
		}
	}
	loops, err := ft.closedActionRegions(p, excluded)
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 1 {
		t.Fatalf("following guest loop spans=%v", loops)
	}
	for start, end := range loops {
		first, ok := p.actions[start].Primitive()
		last, lastOK := p.actions[end].Primitive()
		if !ok || !lastOK || first.Opcode != wasm.OpLoop || last.Opcode != wasm.OpEnd {
			t.Fatal("loop steps were used as action indices")
		}
		if p.ranges[start].start == start {
			t.Fatal("fixture did not shift following loop")
		}
	}
}

func TestCapturePlanRetainsZeroStepAction(t *testing.T) {
	fixture := capturePlanFixture(t, `unreachable if call $yield end i32.const 9`)
	p, sites := fixture.program, fixture.sites
	found := false
	for index, action := range p.actions {
		if action.Kind() != ir.AsyncIfEntryCapture {
			continue
		}
		found = true
		if p.ranges[index].start != p.ranges[index].end {
			t.Fatal("absent capture emitted analysis steps")
		}
		if _, err := p.sourceStep(index); err == nil {
			t.Fatal("capture impersonated source call")
		}
	}
	if !found || len(sites) != 1 {
		t.Fatal("missing zero-step capture or call")
	}
	if _, err := p.sourceStep(sites[0].ActionIndex); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureAllocatorProhibitsMaterialization(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		allocator := NewTempAllocator(3)
		allocator.SetCurrentInstr(4)
		allocator.ObserveCapture()
		if snapshot {
			allocator.SnapshotLocal(0, wasm.ValI32)
		} else {
			allocator.Alloc(wasm.ValI32)
		}
		if allocator.Err() == nil || len(allocator.Plan()) != 0 || allocator.MaxLocal() != 3 {
			t.Fatal("capture allocated temporary storage")
		}
	}
}

// This is a projection-policy test, not a valid-source lowering fixture. Insert
// an existing zero-step semantic action into a copyable loop's action interval;
// byte analysis alone cannot see it and must not authorize copying that span.
func TestCapturePlanExcludesInvisibleCompositeFromLoopCopy(t *testing.T) {
	fixture := capturePlanFixture(t, `call $yield loop i32.const 9 local.set 0 end local.get 0`)
	ft, p := fixture.transformer, fixture.program
	zero := capturePlanFixture(t, `unreachable if call $yield end i32.const 9`).program
	var capture ir.Action
	found := false
	for _, action := range zero.actions {
		if action.Kind() == ir.AsyncIfEntryCapture {
			capture, found = action, true
			break
		}
	}
	if !found {
		t.Fatal("no zero-step action")
	}
	insertion := -1
	for index, action := range p.actions {
		instruction, ok := action.Primitive()
		if ok && instruction.Opcode == wasm.OpLoop {
			insertion = index + 1
			break
		}
	}
	if insertion < 0 {
		t.Fatal("no loop")
	}
	actions := append([]ir.Action(nil), p.actions[:insertion]...)
	actions = append(actions, capture)
	actions = append(actions, p.actions[insertion:]...)
	projection, err := newExecutionPlan(actions)
	if err != nil {
		t.Fatal(err)
	}
	spans, err := ft.closedActionRegions(projection, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 0 {
		t.Fatalf("copied a span containing an invisible composite: %v", spans)
	}
}
