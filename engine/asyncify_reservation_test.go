package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/memory/budget"
	"github.com/wippyai/wasm-runtime/wat"
)

const ownedAsyncifyFixture = `(module
  (memory (export "memory") 1)
  (global $next (mut i32) (i32.const 4096))
  (global $allocations (export "allocations") (mut i32) (i32.const 0))
  (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
    (local $addr i32)
    (local.set $addr (global.get $next))
    (global.set $next (i32.add (local.get $addr) (local.get 3)))
    (global.set $allocations (i32.add (global.get $allocations) (i32.const 1)))
    (local.get $addr))
  (func (export "asyncify_get_state") (result i32) i32.const 0)
  (func (export "asyncify_start_unwind") (param i32))
  (func (export "asyncify_stop_unwind"))
  (func (export "asyncify_start_rewind") (param i32))
  (func (export "asyncify_stop_rewind"))
)`

const unallocatableAsyncifyFixture = `(module
  (memory (export "memory") 1)
  (func (export "asyncify_get_state") (result i32) i32.const 0)
  (func (export "asyncify_start_unwind") (param i32))
  (func (export "asyncify_stop_unwind"))
  (func (export "asyncify_start_rewind") (param i32))
  (func (export "asyncify_stop_rewind"))
)`

func instantiateOwnedAsyncifyFixture(t *testing.T, source string, cfg *InstanceConfig) *WazeroInstance {
	t.Helper()
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close(ctx) })
	raw, err := wat.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := eng.LoadModule(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inst.Close(ctx) })
	return inst
}

func TestOwnedAsyncifyStackUsesLocalAllocationAndReusesIt(t *testing.T) {
	const stackBytes = 96
	inst := instantiateOwnedAsyncifyFixture(t, ownedAsyncifyFixture, &InstanceConfig{
		EnableAsyncify:     true,
		AsyncifyStackBytes: stackBytes,
	})
	reservation, ok := inst.asyncifyReservations[inst.instance]
	if !ok {
		t.Fatal("missing owned reservation")
	}
	if reservation.dataAddr != 4096 || reservation.bytes != asyncifyHeaderBytes+stackBytes {
		t.Fatalf("reservation = %#v, want data=4096 bytes=%d", reservation, asyncifyHeaderBytes+stackBytes)
	}
	if inst.Asyncify().dataAddr != reservation.dataAddr || inst.Asyncify().stackSize != stackBytes {
		t.Fatalf("controller = data=%d stack=%d, reservation=%#v", inst.Asyncify().dataAddr, inst.Asyncify().stackSize, reservation)
	}
	mem := inst.instance.Memory()
	if ptr, ok := mem.ReadUint32Le(reservation.dataAddr); !ok || ptr != reservation.dataAddr+asyncifyHeaderBytes {
		t.Fatalf("stack pointer = %d, ok=%v", ptr, ok)
	}
	if end, ok := mem.ReadUint32Le(reservation.dataAddr + 4); !ok || end != reservation.dataAddr+asyncifyHeaderBytes+stackBytes {
		t.Fatalf("stack end = %d, ok=%v", end, ok)
	}
	if ptr, ok := mem.ReadUint32Le(AsyncifyDataAddr); !ok || ptr != 0 {
		t.Fatalf("legacy low address was written: %d, ok=%v", ptr, ok)
	}
	if got := inst.instance.ExportedGlobal("allocations").Get(); got != 1 {
		t.Fatalf("allocator calls = %d, want 1", got)
	}
	if err := inst.enableOwnedAsyncify(context.Background(), stackBytes); err != nil {
		t.Fatalf("same owned reconfiguration: %v", err)
	}
	if got := inst.instance.ExportedGlobal("allocations").Get(); got != 1 {
		t.Fatalf("same reconfiguration allocated again: %d", got)
	}
	if err := inst.enableOwnedAsyncify(context.Background(), stackBytes+1); err == nil || !strings.Contains(err.Error(), "cannot change") {
		t.Fatalf("different owned size error = %v", err)
	}
}

func TestOwnedAsyncifyStackRejectsMissingAllocatorBeforeHeaderWrite(t *testing.T) {
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
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	const sentinel = 0xfacefeed
	if !inst.instance.Memory().WriteUint32Le(AsyncifyDataAddr, sentinel) {
		t.Fatal("write sentinel")
	}
	err = inst.enableOwnedAsyncify(context.Background(), 64)
	if err == nil || !strings.Contains(err.Error(), "no local/canonical allocator") {
		t.Fatalf("error = %v", err)
	}
	if got, ok := inst.instance.Memory().ReadUint32Le(AsyncifyDataAddr); !ok || got != sentinel {
		t.Fatalf("missing allocator wrote header: %#x ok=%v", got, ok)
	}
	if len(inst.asyncifyReservations) != 0 {
		t.Fatalf("missing allocator retained reservations: %#v", inst.asyncifyReservations)
	}
}

func TestAsyncifyPrepareInitRejectsRegionWhoseHeaderFits(t *testing.T) {
	a, mod := reviewAsyncifyControls(t, false)
	addr := mod.Memory().Size() - asyncifyHeaderBytes
	const sentinel = 0xfacefeed
	if !mod.Memory().WriteUint32Le(addr, sentinel) {
		t.Fatal("write sentinel")
	}
	a.SetDataAddr(addr)
	a.SetStackSize(1)
	if err := a.Init(mod); err == nil || !strings.Contains(err.Error(), "stack region outside memory") {
		t.Fatalf("error = %v", err)
	}
	if got, ok := mod.Memory().ReadUint32Le(addr); !ok || got != sentinel {
		t.Fatalf("failed full-region validation wrote header: %#x ok=%v", got, ok)
	}
}

const sharedMemoryAsyncifyComponentWAT = `(component
  (core module $owner
    (memory (export "memory") 1)
    (global $next (mut i32) (i32.const 4096))
    (global $allocations (export "allocations") (mut i32) (i32.const 0))
    (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
      (local $addr i32)
      (local.set $addr (global.get $next))
      (global.set $next (i32.add (local.get $addr) (local.get 3)))
      (global.set $allocations (i32.add (global.get $allocations) (i32.const 1)))
      (local.get $addr))
    (func (export "asyncify_get_state") (result i32) i32.const 0)
    (func (export "asyncify_start_unwind") (param i32))
    (func (export "asyncify_stop_unwind"))
    (func (export "asyncify_start_rewind") (param i32))
    (func (export "asyncify_stop_rewind"))
  )
  (core module $adapter
    (import "shared" "memory" (memory 1))
    (func (export "asyncify_get_state") (result i32) i32.const 0)
    (func (export "asyncify_start_unwind") (param i32))
    (func (export "asyncify_stop_unwind"))
    (func (export "asyncify_start_rewind") (param i32))
    (func (export "asyncify_stop_rewind"))
    (func (export "run") (result i32) i32.const 7)
  )
  (core instance $owner_inst (instantiate $owner))
  (alias core export $owner_inst "memory" (core memory $memory))
  (core instance $shared (export "memory" (memory $memory)))
  (core instance $adapter_inst (instantiate $adapter (with "shared" (instance $shared))))
  (alias core export $adapter_inst "run" (core func $run))
  (type $run_type (func (result u32)))
  (func $lift_run (type $run_type) (canon lift (core func $run)))
  (export "run" (func $lift_run))
)`

func TestOwnedAsyncifyStackUsesSameMemoryAllocatorForAdapter(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	mod, err := eng.LoadModule(ctx, componentFixture(t, sharedMemoryAsyncifyComponentWAT))
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true, AsyncifyStackBytes: 64, EntryExport: "run"})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	if len(inst.asyncifyReservations) != 2 {
		t.Fatalf("reservations = %#v, want both shared-memory cores", inst.asyncifyReservations)
	}
	var first, second asyncifyStackReservation
	for _, reservation := range inst.asyncifyReservations {
		if first.memory == nil {
			first = reservation
		} else {
			second = reservation
		}
	}
	if first.memory != second.memory {
		t.Fatal("fixture does not share memory")
	}
	if first.dataAddr == second.dataAddr {
		t.Fatalf("shared allocator reused one stack region: %#v %#v", first, second)
	}
	if got := inst.linkerInst.GetModule(0).ExportedGlobal("allocations").Get(); got != 2 {
		t.Fatalf("allocator calls = %d, want 2", got)
	}
}

func TestOwnedAsyncifyStackRejectsAllocatorFromDifferentCoreMemory(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	// Keep the allocator core intact, but give the adapter its own memory. The
	// conventional cabi_realloc name must not make that foreign heap eligible.
	source := strings.ReplaceAll(sharedMemoryAsyncifyComponentWAT,
		`(import "shared" "memory" (memory 1))`, `(memory (export "memory") 1)`)
	source = strings.ReplaceAll(source,
		`(core instance $adapter_inst (instantiate $adapter (with "shared" (instance $shared))))`,
		`(core instance $adapter_inst (instantiate $adapter))`)
	mod, err := eng.LoadModule(ctx, componentFixture(t, source))
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EntryExport: "run"})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	const sentinel = 0xfacefeed
	for _, core := range inst.linkerInst.Modules() {
		if core != nil && core.ExportedFunction("asyncify_get_state") != nil {
			if !core.Memory().WriteUint32Le(AsyncifyDataAddr, sentinel) {
				t.Fatal("write sentinel")
			}
		}
	}
	err = inst.enableOwnedAsyncify(ctx, 64)
	if err == nil || !strings.Contains(err.Error(), "no local/canonical allocator") {
		t.Fatalf("error = %v", err)
	}
	for _, core := range inst.linkerInst.Modules() {
		if core == nil || core.ExportedFunction("asyncify_get_state") == nil {
			continue
		}
		if got, ok := core.Memory().ReadUint32Le(AsyncifyDataAddr); !ok || got != sentinel {
			t.Fatalf("foreign allocator failure wrote %q header: %#x ok=%v", core.Name(), got, ok)
		}
	}
	if len(inst.asyncifyReservations) != 0 {
		t.Fatalf("foreign allocator retained reservations: %#v", inst.asyncifyReservations)
	}
}

func TestTransformOwnedStackGrowthUsesMemoryBudget(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	raw, err := wat.Compile(`(module (memory (export "memory") 0 1))`)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := eng.LoadModule(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	deniedBudget := budget.New(0)
	denied, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{MemoryBudget: deniedBudget})
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureTransformReservedStackMemory(denied.instance.Memory(), 8); err == nil {
		t.Fatal("zero-budget transform reservation growth succeeded")
	}
	if got := deniedBudget.Usage().Used; got != 0 || denied.instance.Memory().Size() != 0 {
		t.Fatalf("denied growth changed budget or memory: used=%d size=%d", got, denied.instance.Memory().Size())
	}
	if err := denied.Close(ctx); err != nil {
		t.Fatal(err)
	}

	allowedBudget := budget.New(65536)
	allowed, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{MemoryBudget: allowedBudget})
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureTransformReservedStackMemory(allowed.instance.Memory(), 8); err != nil {
		t.Fatalf("admitted transform reservation growth: %v", err)
	}
	if got := allowedBudget.Usage().Used; got != 65536 {
		t.Fatalf("growth charge = %d, want one page", got)
	}
	if err := allowed.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := allowedBudget.Usage().Used; got != 0 {
		t.Fatalf("close retained transform reservation charge: %d", got)
	}
}

// mixedAsyncifyProvenanceComponentWAT has one source core transformed by this
// linker and one externally instrumented core that deliberately exports globals
// shaped like our generated controls. Exact core provenance must keep the latter
// on function controls.
const mixedAsyncifyProvenanceComponentWAT = `(component
  (core module $external
    (memory (export "memory") 1)
    (global (export "asyncify_state") (mut i32) (i32.const 0))
    (global (export "asyncify_data") (mut i32) (i32.const 0))
    (global $next (mut i32) (i32.const 4096))
    (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
      (local $addr i32)
      (local.set $addr (global.get $next))
      (global.set $next (i32.add (local.get $addr) (local.get 3)))
      (local.get $addr))
    (func (export "asyncify_get_state") (result i32) (global.get 0))
    (func (export "asyncify_start_unwind") (param i32) (global.set 0 (i32.const 1)))
    (func (export "asyncify_stop_unwind") (global.set 0 (i32.const 0)))
    (func (export "asyncify_start_rewind") (param i32) (global.set 0 (i32.const 2)))
    (func (export "asyncify_stop_rewind") (global.set 0 (i32.const 0)))
  )
  (core module $embedded
    (memory (export "memory") 1)
    (global $next (mut i32) (i32.const 4096))
    (func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
      (local $addr i32)
      (local.set $addr (global.get $next))
      (global.set $next (i32.add (local.get $addr) (local.get 3)))
      (local.get $addr))
    (func (export "run") (result i32) i32.const 7)
  )
  (core instance $external_inst (instantiate $external))
  (core instance $embedded_inst (instantiate $embedded))
  (alias core export $embedded_inst "run" (core func $run))
  (type $run_type (func (result u32)))
  (func $lift_run (type $run_type) (canon lift (core func $run)))
  (export "run" (func $lift_run))
)`

func TestOwnedAsyncifyEagerControllersUseExactCoreTransformProvenance(t *testing.T) {
	ctx := context.Background()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	mod, err := eng.LoadModule(ctx, componentFixture(t, mixedAsyncifyProvenanceComponentWAT))
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{
		EnableAsyncify:     true,
		AsyncifyStackBytes: 64,
		EntryExport:        "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	entry, err := inst.getExportBinding("run")
	if err != nil {
		t.Fatal(err)
	}
	if entry.asyncify == nil || !entry.asyncify.DirectGlobals() || !inst.linkerInst.IsModuleTransformed(entry.coreMod) {
		t.Fatal("embedded-transformed entry did not receive direct globals")
	}
	var external api.Module
	for _, core := range inst.linkerInst.Modules() {
		if core != nil && core != entry.coreMod && core.ExportedFunction("asyncify_get_state") != nil {
			external = core
			break
		}
	}
	if external == nil || inst.linkerInst.IsModuleTransformed(external) {
		t.Fatal("fixture did not retain an externally instrumented sibling")
	}
	externalState, ok := inst.asyncifyCache[external]
	if !ok || externalState.asyncify == nil {
		t.Fatal("owned eager initialization omitted external sibling")
	}
	if externalState.asyncify.DirectGlobals() {
		t.Fatal("externally instrumented sibling inherited direct-global trust")
	}
}
