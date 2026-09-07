package linker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/memory/budget"
	"github.com/wippyai/wasm-runtime/wat"
)

func admissionFixture(t *testing.T, sources []string, starts *int) *InstancePre {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	t.Cleanup(func() { _ = rt.Close(ctx) })
	l := NewWithDefaults(rt)
	if err := l.DefineFunc("env#mark", api.GoModuleFunc(func(context.Context, api.Module, []uint64) { *starts++ }), nil, nil); err != nil {
		t.Fatal(err)
	}
	comp := &component.ValidatedComponent{Raw: &component.Component{}}
	for i, source := range sources {
		data, err := wat.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		comp.Raw.CoreModules = append(comp.Raw.CoreModules, data)
		comp.Raw.CoreInstances = append(comp.Raw.CoreInstances, component.CoreInstance{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: uint32(i)}})
	}
	pre, err := l.Instantiate(ctx, comp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pre.Close(ctx) })
	return pre
}

func testAdmission(parent context.Context, t *testing.T, pre *InstancePre, b *budget.Budget) (*MemoryAdmission, context.Context, context.Context) {
	t.Helper()
	domain, cancel := context.WithCancel(parent)
	t.Cleanup(cancel)
	a, err := pre.AdmitMemory(b, cancel)
	if err != nil {
		t.Fatal(err)
	}
	return a, domain, a.WithAllocator(domain)
}

const admittedCore = `(module (import "env" "mark" (func $mark)) (memory (export "memory") 1 3) (func $start (call $mark)) (start $start) (func (export "grow") (result i32) (memory.grow (i32.const 1))))`

func TestMemoryAdmissionRejectsBeforeGuestStart(t *testing.T) {
	starts := 0
	pre := admissionFixture(t, []string{admittedCore, admittedCore}, &starts)
	b := budget.New(65536)
	a, err := pre.AdmitMemory(b, nil)
	if !errors.Is(err, budget.ErrLimit) || a != nil || starts != 0 || b.Usage().Used != 0 {
		t.Fatalf("admission=%v err=%v starts=%d usage=%+v", a, err, starts, b.Usage())
	}
}
func TestMemoryAdmissionSharesBudgetAndDeniesGrowth(t *testing.T) {
	starts := 0
	pre := admissionFixture(t, []string{admittedCore, admittedCore}, &starts)
	b := budget.New(4 * 65536)
	host, err := b.Reserve(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Release()
	a, _, coreCtx := testAdmission(context.Background(), t, pre, b)
	inst, err := pre.NewInstance(coreCtx)
	if err != nil {
		a.ReleaseAfterModulesClosed()
		t.Fatal(err)
	}
	defer func() { _ = inst.Close(context.Background()); a.ReleaseAfterModulesClosed() }()
	if starts != 2 {
		t.Fatalf("starts=%d", starts)
	}
	for i, want := range []uint64{1, 0xffffffff} {
		got, e := inst.modules[i].ExportedFunction("grow").Call(coreCtx)
		if e != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("core%d grow=%v err=%v want=%d", i, got, e, want)
		}
	}
	if b.Usage().Used != 4*65536 {
		t.Fatalf("usage=%+v", b.Usage())
	}
	if err = inst.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.ReleaseAfterModulesClosed()
	a.ReleaseAfterModulesClosed()
	if b.Usage().Used != 65536 {
		t.Fatal("guest release lost independent host charge")
	}
}
func TestMemoryAdmissionReleasesFailedStart(t *testing.T) {
	starts := 0
	bad := `(module (import "env" "mark" (func $mark)) (memory 1) (func $start (call $mark) unreachable) (start $start))`
	pre := admissionFixture(t, []string{admittedCore, bad}, &starts)
	b := budget.New(2 * 65536)
	a, _, coreCtx := testAdmission(context.Background(), t, pre, b)
	inst, err := pre.NewInstance(coreCtx)
	if inst != nil {
		inst.Close(context.Background())
	}
	if err == nil {
		a.ReleaseAfterModulesClosed()
		t.Fatal("start trap missing")
	}
	if starts != 2 {
		t.Fatalf("starts=%d want2", starts)
	}
	a.ReleaseAfterModulesClosed()
	if b.Usage().Used != 0 {
		t.Fatalf("failed startup retained charge: %+v", b.Usage())
	}
	for _, m := range a.slots {
		if m.data != nil {
			t.Fatal("failed startup retained buffer")
		}
	}
}

func TestMemoryAdmissionImportedAliasClosesDomain(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)
	comp := &component.ValidatedComponent{Raw: &component.Component{}}
	for _, source := range []string{
		`(module (memory (export "memory") 1 3) (func (export "grow") (result i32) (memory.grow (i32.const 1))))`,
		`(module (import "owner" "memory" (memory 1 3)))`,
	} {
		data, err := wat.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		comp.Raw.CoreModules = append(comp.Raw.CoreModules, data)
	}
	comp.Raw.CoreInstances = []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: 0}},
		{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: 1, Args: []component.CoreInstanceArg{{Name: "owner", Kind: component.CoreInstantiateInstance, InstanceIndex: 0}}}},
	}
	pre, err := NewWithDefaults(rt).Instantiate(ctx, comp)
	if err != nil {
		t.Fatal(err)
	}
	defer pre.Close(ctx)
	b := budget.New(65536)
	a, domain, coreCtx := testAdmission(ctx, t, pre, b)
	inst, err := pre.NewInstance(coreCtx)
	if err != nil {
		a.ReleaseAfterModulesClosed()
		t.Fatal(err)
	}
	defer func() { _ = inst.Close(ctx); a.ReleaseAfterModulesClosed() }()
	if len(a.slots) != 1 || a.next != 1 || len(inst.modules) != 2 {
		t.Fatalf("slots=%d assigned=%d cores=%d", len(a.slots), a.next, len(inst.modules))
	}
	if err = inst.modules[1].Close(ctx); err != nil {
		t.Fatal(err)
	}
	if domain.Err() != context.Canceled {
		t.Fatal("alias close left admission domain active")
	}
	if _, err = inst.modules[0].ExportedFunction("grow").Call(coreCtx); err == nil {
		t.Fatal("owner executed after alias detached allocator")
	}
	if b.Usage().Used != 65536 {
		t.Fatal("alias prematurely released charge")
	}
	if err = inst.Close(ctx); err != nil {
		t.Fatal(err)
	}
	a.ReleaseAfterModulesClosed()
	if b.Usage().Used != 0 {
		t.Fatal("owner retained charge after teardown")
	}
}

func TestMemoryAdmissionStopAllowsReservedConstructionButDeniesGrowth(t *testing.T) {
	b := budget.New(3 * wasmPageSize)
	var stops atomic.Int32
	a, err := AdmitMemoryPlan([]OwnedMemory{{MinimumPages: 1}, {MinimumPages: 1}}, b, func() {
		stops.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	first := a.Allocate(wasmPageSize, 2*wasmPageSize)
	// A core's host callback can stop the domain before Wazero allocates the
	// following core. All minima were charged by admission, and Allocate has no
	// error result, so both plan slots must complete their minimum initialization.
	a.Stop()
	a.Stop()
	if stops.Load() != 1 {
		t.Fatalf("stop calls=%d", stops.Load())
	}
	second := a.Allocate(wasmPageSize, wasmPageSize)
	if got := first.Reallocate(wasmPageSize); len(got) != wasmPageSize {
		t.Fatalf("first initial bytes=%d", len(got))
	}
	if got := second.Reallocate(wasmPageSize); len(got) != wasmPageSize {
		t.Fatalf("second initial bytes=%d", len(got))
	}
	if got := first.Reallocate(2 * wasmPageSize); got != nil {
		t.Fatal("stopped admission permitted growth")
	}
	a.ReleaseAfterModulesClosed()
	a.ReleaseAfterModulesClosed()
	if b.Usage().Used != 0 {
		t.Fatalf("released usage=%+v", b.Usage())
	}
}

func TestMemoryAdmissionWithAllocatorDoesNotOwnCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := AdmitMemoryPlan(nil, budget.New(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := a.WithAllocator(parent)
	a.Stop()
	if ctx.Err() != nil || parent.Err() != nil {
		t.Fatal("admission changed execution context cancellation")
	}
	a.ReleaseAfterModulesClosed()
}

func TestAdmitMemoryPlanRejectsInsufficientBudget(t *testing.T) {
	plan := []OwnedMemory{{MinimumPages: 2}}
	b := budget.New(wasmPageSize)
	a, err := AdmitMemoryPlan(plan, b, nil)
	if !errors.Is(err, budget.ErrLimit) || a != nil || b.Usage().Used != 0 {
		t.Fatalf("admission=%v err=%v usage=%+v", a, err, b.Usage())
	}
}
