package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/linker"
	"github.com/wippyai/wasm-runtime/resource"
)

type parentCtxKey struct{}

func newEngineCallInstance() *WazeroInstance {
	asyncify := NewAsyncify()
	return &WazeroInstance{
		resources: resource.NewTable(),
		asyncify:  asyncify,
		scheduler: NewScheduler(asyncify),
	}
}

func captureStepContext(parent context.Context, t *testing.T, inst *WazeroInstance) context.Context {
	t.Helper()
	var captured context.Context
	fn := &mockFunction{
		callFn: func(ctx context.Context, _ ...uint64) ([]uint64, error) {
			captured = ctx
			return nil, nil
		},
	}
	if err := inst.scheduler.Execute(parent, fn); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := (&CallSession{instance: inst}).Step(parent, nil); err != nil {
		t.Fatalf("step: %v", err)
	}
	if captured == nil {
		t.Fatal("guest call did not observe a context")
	}
	return captured
}

func assertEngineIdentity(ctx context.Context, t *testing.T, inst *WazeroInstance) {
	t.Helper()
	if got := ResourcesFromContext(ctx); got != inst.resources {
		t.Fatalf("resources = %p, want %p", got, inst.resources)
	}
	if got := GetAsyncify(ctx); got != inst.asyncify {
		t.Fatalf("asyncify = %p, want %p", got, inst.asyncify)
	}
	if got := GetScheduler(ctx); got != inst.scheduler {
		t.Fatalf("scheduler = %p, want %p", got, inst.scheduler)
	}
}

func TestCallSessionStep_CrossInstanceContextIdentity(t *testing.T) {
	parent := context.WithValue(context.Background(), parentCtxKey{}, "tenant")
	first := newEngineCallInstance()
	second := newEngineCallInstance()

	firstCtx := captureStepContext(parent, t, first)
	secondCtx := captureStepContext(parent, t, second)

	assertEngineIdentity(firstCtx, t, first)
	assertEngineIdentity(secondCtx, t, second)
	if first.resources == second.resources || first.asyncify == second.asyncify || first.scheduler == second.scheduler {
		t.Fatal("instances share engine-owned call state")
	}
	if firstCtx.Value(parentCtxKey{}) != "tenant" || secondCtx.Value(parentCtxKey{}) != "tenant" {
		t.Fatal("parent value missing from step context")
	}
}

func TestCallSessionStep_NestedCallPreservesIdentities(t *testing.T) {
	parent := context.WithValue(context.Background(), parentCtxKey{}, "outer-parent")
	outer := newEngineCallInstance()
	inner := newEngineCallInstance()

	var outerCtx, innerCtx context.Context
	outerFn := &mockFunction{
		callFn: func(ctx context.Context, _ ...uint64) ([]uint64, error) {
			outerCtx = ctx
			innerFn := &mockFunction{
				callFn: func(ctx context.Context, _ ...uint64) ([]uint64, error) {
					innerCtx = ctx
					return nil, nil
				},
			}
			if err := inner.scheduler.Execute(ctx, innerFn); err != nil {
				return nil, err
			}
			_, err := (&CallSession{instance: inner}).Step(ctx, nil)
			return nil, err
		},
	}
	if err := outer.scheduler.Execute(parent, outerFn); err != nil {
		t.Fatalf("outer execute: %v", err)
	}
	if _, err := (&CallSession{instance: outer}).Step(parent, nil); err != nil {
		t.Fatalf("outer step: %v", err)
	}

	assertEngineIdentity(outerCtx, t, outer)
	assertEngineIdentity(innerCtx, t, inner)
	if outerCtx.Value(parentCtxKey{}) != "outer-parent" || innerCtx.Value(parentCtxKey{}) != "outer-parent" {
		t.Fatal("nested step lost parent value")
	}
	assertEngineIdentity(outerCtx, t, outer)
}

func TestCallSessionStep_RetainedContextIsImmutable(t *testing.T) {
	inst := newEngineCallInstance()
	originalResources := inst.resources
	originalAsyncify := inst.asyncify
	originalScheduler := inst.scheduler

	retained := captureStepContext(context.Background(), t, inst)

	inst.resources = resource.NewTable()
	inst.asyncify = NewAsyncify()
	inst.scheduler = NewScheduler(inst.asyncify)
	next := captureStepContext(context.Background(), t, inst)

	if retained == next {
		t.Fatal("step reused the previous context wrapper")
	}
	if ResourcesFromContext(retained) != originalResources || GetAsyncify(retained) != originalAsyncify || GetScheduler(retained) != originalScheduler {
		t.Fatal("retained step context changed after a later step")
	}
	assertEngineIdentity(next, t, inst)
}

func TestCallSessionStep_ParentCancellationAndCause(t *testing.T) {
	cause := errors.New("guest canceled")
	parent, cancel := context.WithCancelCause(context.Background())
	inst := newEngineCallInstance()
	captured := captureStepContext(parent, t, inst)
	cancel(cause)

	if !errors.Is(captured.Err(), context.Canceled) {
		t.Fatalf("captured.Err() = %v, want context.Canceled", captured.Err())
	}
	if !errors.Is(context.Cause(captured), cause) {
		t.Fatalf("cause = %v, want %v", context.Cause(captured), cause)
	}
	if captured.Done() != parent.Done() {
		t.Fatal("Done channel is not the parent channel")
	}

	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	ready := newEngineCallInstance()
	if err := ready.scheduler.Execute(canceled, &mockFunction{}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	_, err := (&CallSession{instance: ready}).Step(canceled, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("step err = %v, want context.Canceled", err)
	}
}

func TestEngineCallContext_NilValuesShadowParent(t *testing.T) {
	parentRes := resource.NewTable()
	parentAsyncify := NewAsyncify()
	parentScheduler := NewScheduler(parentAsyncify)
	parent := context.WithValue(context.Background(), resourcesContextKey{}, parentRes)
	parent = WithAsyncify(parent, parentAsyncify)
	parent = WithScheduler(parent, parentScheduler)

	ctx := withEngineCallContext(parent, &WazeroInstance{})
	if got := ResourcesFromContext(ctx); got != nil {
		t.Fatalf("resources = %p, want nil", got)
	}
	if got := GetAsyncify(ctx); got != nil {
		t.Fatalf("asyncify = %p, want nil", got)
	}
	if got := GetScheduler(ctx); got != nil {
		t.Fatalf("scheduler = %p, want nil", got)
	}

	wantSched := ctx.Value(ctxKeyScheduler{})
	gotSched := context.WithValue(parent, ctxKeyScheduler{}, (*Scheduler)(nil)).Value(ctxKeyScheduler{})
	if (wantSched == nil) != (gotSched == nil) {
		t.Fatalf("nil scheduler Value() = %#v, WithValue nil = %#v", wantSched, gotSched)
	}
}

func TestEngineCallContext_DeadlineDoneErrDelegate(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	parent = context.WithValue(parent, parentCtxKey{}, 7)
	inst := newEngineCallInstance()
	ctx := withEngineCallContext(parent, inst)

	gotDeadline, ok := ctx.Deadline()
	if !ok || !gotDeadline.Equal(deadline) {
		t.Fatalf("deadline = %v ok=%v, want %v", gotDeadline, ok, deadline)
	}
	if ctx.Done() != parent.Done() {
		t.Fatal("Done channel is not the parent channel")
	}
	if ctx.Err() != nil {
		t.Fatalf("Err = %v, want nil", ctx.Err())
	}
	if ctx.Value(parentCtxKey{}) != 7 {
		t.Fatalf("parent value = %v, want 7", ctx.Value(parentCtxKey{}))
	}
	assertEngineIdentity(ctx, t, inst)
}

func TestCallSessionStep_LinkerInstanceWhenPresent(t *testing.T) {
	withoutLinker := newEngineCallInstance()
	plain := captureStepContext(context.Background(), t, withoutLinker)
	if got := linker.InstanceFromContext(plain); got != nil {
		t.Fatalf("linker instance = %p, want nil", got)
	}

	withLinker := newEngineCallInstance()
	withLinker.linkerInst = &linker.Instance{}
	wrapped := captureStepContext(context.Background(), t, withLinker)
	if got := linker.InstanceFromContext(wrapped); got != withLinker.linkerInst {
		t.Fatalf("linker instance = %p, want %p", got, withLinker.linkerInst)
	}
	assertEngineIdentity(wrapped, t, withLinker)
}

func TestPrepareCallContext_SyncPathUnchanged(t *testing.T) {
	inst := newEngineCallInstance()
	inst.linkerInst = &linker.Instance{}
	ctx := inst.prepareCallContext(context.Background())
	if got := ResourcesFromContext(ctx); got != inst.resources {
		t.Fatalf("resources = %p, want %p", got, inst.resources)
	}
	if got := linker.InstanceFromContext(ctx); got != inst.linkerInst {
		t.Fatalf("linker instance = %p, want %p", got, inst.linkerInst)
	}
	if GetAsyncify(ctx) != nil || GetScheduler(ctx) != nil {
		t.Fatal("prepareCallContext attached asyncify or scheduler")
	}
}

func TestCallSessionStep_ConcurrentInstancesStayIsolated(t *testing.T) {
	parent := context.WithValue(context.Background(), parentCtxKey{}, "shared")
	const n = 16
	type observed struct {
		inst *WazeroInstance
		ctx  context.Context
	}
	got := make([]observed, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			inst := newEngineCallInstance()
			var captured context.Context
			fn := &mockFunction{
				callFn: func(ctx context.Context, _ ...uint64) ([]uint64, error) {
					captured = ctx
					return nil, nil
				},
			}
			if err := inst.scheduler.Execute(parent, fn); err != nil {
				t.Errorf("execute: %v", err)
				return
			}
			if _, err := (&CallSession{instance: inst}).Step(parent, nil); err != nil {
				t.Errorf("step: %v", err)
				return
			}
			got[i] = observed{inst: inst, ctx: captured}
		}(i)
	}
	wg.Wait()

	for i, obs := range got {
		if obs.ctx == nil {
			t.Fatalf("goroutine %d missing captured context", i)
		}
		assertEngineIdentity(obs.ctx, t, obs.inst)
		if obs.ctx.Value(parentCtxKey{}) != "shared" {
			t.Fatalf("goroutine %d lost parent value", i)
		}
	}
}

func BenchmarkEngineCallContext(b *testing.B) {
	inst := newEngineCallInstance()
	parent := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := withEngineCallContext(parent, inst)
		if ResourcesFromContext(ctx) != inst.resources || GetAsyncify(ctx) != inst.asyncify || GetScheduler(ctx) != inst.scheduler {
			b.Fatal("engine keys missing")
		}
	}
}
