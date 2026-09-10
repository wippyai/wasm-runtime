package engine

import (
	"context"
	"errors"
	"testing"
)

func TestReconfigureFailureDoesNotPartiallyWriteHeader(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()
	mem := inst.instance.Memory()
	addr := mem.Size() - 4
	const sentinel = 0xfeedface
	if !mem.WriteUint32Le(addr, sentinel) {
		t.Fatal("sentinel write")
	}
	previous := inst.Asyncify()
	if err := inst.EnableAsyncify(AsyncifyConfig{DataAddr: addr, StackSize: 1024}); err == nil {
		t.Fatal("expected invalid header rejection")
	}
	if got, ok := mem.ReadUint32Le(addr); !ok || got != sentinel {
		t.Errorf("failed reconfiguration changed guest memory: %#x", got)
	}
	if inst.Asyncify() != previous {
		t.Error("failed reconfiguration published controls")
	}
}

func TestReconfiguredControlsCannotResetFormerStack(t *testing.T) {
	_, inst, cleanup := setupPostReturnInstance(t)
	defer cleanup()
	previous := inst.Asyncify()
	if previous == nil {
		t.Fatal("missing initial controls")
	}
	oldAddr := previous.dataAddr
	if err := inst.EnableAsyncify(AsyncifyConfig{DataAddr: 4096, StackSize: 1024}); err != nil {
		t.Fatal(err)
	}
	mem := inst.instance.Memory()
	const sentinel = 0xfeedface
	if !mem.WriteUint32Le(oldAddr, sentinel) {
		t.Fatal("sentinel write")
	}
	if err := previous.ResetStackContext(context.Background()); err == nil {
		t.Error("superseded controls accepted reset")
	}
	if got, ok := mem.ReadUint32Le(oldAddr); !ok || got != sentinel {
		t.Errorf("superseded controls changed guest memory: %#x", got)
	}
}

func TestGenerationRevocationPrecedesBorrowedLifetime(t *testing.T) {
	for _, direct := range []bool{false, true} {
		a, mod := reviewAsyncifyControls(t, direct)
		a.lifetime = newExecutionLifetime()
		a.generation = &asyncifyGeneration{}
		ctx, leave, err := a.lifetime.enter(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		a.generation.revoked.Store(true)
		for _, control := range []func(context.Context) error{a.StartUnwind, a.StopUnwind, a.StartRewind, a.StopRewind, a.ResetStackContext} {
			if err := control(ctx); !errors.Is(err, ErrAsyncifySuperseded) {
				t.Fatalf("stale control: %v", err)
			}
		}
		scheduler := NewScheduler(a)
		if err := scheduler.Execute(ctx, nil); !errors.Is(err, ErrAsyncifySuperseded) {
			t.Fatalf("stale Execute: %v", err)
		}
		if _, err := scheduler.Step(ctx, nil); !errors.Is(err, ErrAsyncifySuperseded) {
			t.Fatalf("stale Step: %v", err)
		}
		if got := mod.ExportedGlobal("asyncify_state").Get(); got != 0 {
			t.Fatalf("state mutated: %d", got)
		}
		leave()
	}
}

func TestReconfigurePreparesEveryCoreBeforeCommit(t *testing.T) {
	ctx := context.Background()
	eng, mod := loadTwoCoreModule(t)
	defer eng.Close(ctx)
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	// Both fixture heaps start above 70000; reserve this region in each core.
	if err := inst.EnableAsyncify(AsyncifyConfig{DataAddr: 32768, StackSize: 1024}); err != nil {
		t.Fatal(err)
	}
	first, err := inst.getExportBinding("func1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := inst.getExportBinding("func2")
	if err != nil {
		t.Fatal(err)
	}
	initial := inst.Asyncify()
	initialMemory := initial.module.Memory()
	otherMemory := first.coreMod.Memory()
	if first.coreMod == initial.module {
		otherMemory = second.coreMod.Memory()
	}
	if initialMemory == otherMemory {
		t.Fatal("fixture requires distinct memories")
	}
	addr := otherMemory.Size() - 4
	if initialMemory.Size() <= otherMemory.Size() {
		pages := (otherMemory.Size()-initialMemory.Size())/65536 + 1
		if _, ok := initialMemory.Grow(pages); !ok {
			t.Fatal("grow initial core")
		}
	}
	const sentinel = 0xfacefeed
	if !initialMemory.WriteUint32Le(addr, sentinel) {
		t.Fatal("sentinel write")
	}
	if err := inst.EnableAsyncify(AsyncifyConfig{DataAddr: addr, StackSize: 1024}); err == nil {
		t.Fatal("expected second core header rejection")
	}
	if got, _ := initialMemory.ReadUint32Le(addr); got != sentinel {
		t.Fatalf("first core was committed before second validated: %#x", got)
	}
	if inst.Asyncify() != initial {
		t.Fatal("failed transaction published")
	}
	if err := initial.ResetStackContext(ctx); err != nil {
		t.Fatalf("failed transaction revoked previous configuration: %v", err)
	}
}

func TestAsyncifyHeaderRejectsOverflowWithoutWrite(t *testing.T) {
	a, mod := reviewAsyncifyControls(t, false)
	addr := a.dataAddr
	const sentinel = 0xabcdef12
	mod.Memory().WriteUint32Le(addr, sentinel)
	a.SetStackSize(^uint32(0))
	if err := a.Init(mod); err == nil {
		t.Fatal("accepted overflowing stack")
	}
	if got, _ := mod.Memory().ReadUint32Le(addr); got != sentinel {
		t.Fatalf("overflow wrote header: %#x", got)
	}
}
