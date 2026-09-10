package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestAutomaticAsyncifyDefaultOwnsStorage(t *testing.T) {
	// The fixture allocator starts at 4096; two pages cover the default region.
	source := strings.Replace(ownedAsyncifyFixture, `(memory (export "memory") 1)`, `(memory (export "memory") 2)`, 1)
	for _, automatic := range []bool{true, false} {
		inst := instantiateOwnedAsyncifyFixture(t, source, &InstanceConfig{EnableAsyncify: automatic})
		if !automatic {
			if err := inst.EnableAsyncify(AsyncifyConfig{}); err != nil {
				t.Fatal(err)
			}
		}
		a := inst.Asyncify()
		if a == nil || a.dataAddr != 4096 || a.stackSize != DefaultAsyncifyStackBytes {
			t.Fatalf("automatic=%v controller=%+v", automatic, a)
		}
		if got, _ := inst.instance.Memory().ReadUint32Le(AsyncifyDataAddr); got != 0 {
			t.Fatalf("automatic initialization wrote legacy location: %d", got)
		}
		if err := inst.EnableAsyncify(AsyncifyConfig{}); err != nil {
			t.Fatal(err)
		}
		if got := inst.instance.ExportedGlobal("allocations").Get(); got != 1 {
			t.Fatalf("reconfiguration leaked reservation: %d allocations", got)
		}
	}
}

func TestAutomaticAsyncifyRejectsUnownedGuestMemory(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	raw, err := wat.Compile(unallocatableAsyncifyFixture)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := eng.LoadModule(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err == nil || !strings.Contains(err.Error(), "no local/canonical allocator") || inst != nil {
		t.Fatalf("unowned startup: instance=%v err=%v", inst, err)
	}
}

func TestAutomaticAsyncifyOwnsRawTransformMemory(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	raw, err := wat.Compile(`(module
      (import "env" "pause" (func $pause))
      (func (export "run") (call $pause)))`)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := eng.LoadModule(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.RegisterHostFuncRaw("env", "pause", nil, nil, func(context.Context, api.Module, []uint64) {}, true); err != nil {
		t.Fatal(err)
	}
	if err := mod.Compile(ctx, &CompileConfig{EnableAsyncify: true}); err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	if !mod.asyncifyAddedMemory || inst.Asyncify() == nil || inst.Asyncify().dataAddr != 0 {
		t.Fatal("raw memoryless core did not receive a proven owned reservation")
	}
	if inst.Asyncify().stackSize != DefaultAsyncifyStackBytes {
		t.Fatal("wrong default stack size")
	}
}

func TestStandaloneAsyncifyRequiresExplicitReservation(t *testing.T) {
	inst := instantiateOwnedAsyncifyFixture(t, unallocatableAsyncifyFixture, nil)
	const sentinel = 0xfacefeed
	if !inst.instance.Memory().WriteUint32Le(AsyncifyDataAddr, sentinel) {
		t.Fatal("sentinel write")
	}
	a := NewAsyncify()
	if err := a.Init(inst.instance); err == nil || !strings.Contains(err.Error(), "SetDataAddr") {
		t.Fatalf("implicit storage accepted: %v", err)
	}
	if got, _ := inst.instance.Memory().ReadUint32Le(AsyncifyDataAddr); got != sentinel {
		t.Fatal("rejected initialization wrote guest memory")
	}
	// This synthetic guest has no data/heap and explicitly lends [0, 12).
	a.SetDataAddr(0)
	a.SetStackSize(4)
	if err := a.Init(inst.instance); err != nil {
		t.Fatal(err)
	}
}
