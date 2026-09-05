package linker

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
)

const virtualHostNS = "test:pkg/iface@0.1.0"

func TestVirtualHostDispatch_DistinctCanonicalBindings(t *testing.T) {
	t.Run("explicit-memory-and-realloc", func(t *testing.T) {
		testVirtualHostDispatchBindings(t, true)
	})
	t.Run("shared-realloc-fallback", func(t *testing.T) {
		testVirtualHostDispatchBindings(t, false)
	})
}

func testVirtualHostDispatchBindings(t *testing.T, explicitRealloc bool) {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	modA := instantiateMarkerModule(ctx, t, rt, "modA", 0xAA)
	modB := instantiateMarkerModule(ctx, t, rt, "modB", 0xBB)

	var observedA, observedB []byte
	var retained []api.Module
	l := NewWithDefaults(rt)
	ns := l.Namespace(virtualHostNS)
	ns.DefineFunc("host-fn", func(ctx context.Context, mod api.Module, stack []uint64) {
		retained = append(retained, mod)
		b, ok := mod.Memory().Read(4, 1)
		if !ok {
			t.Fatal("failed to read memory")
		}
		switch b[0] {
		case 0xAA:
			observedA = append(observedA, b[0])
		case 0xBB:
			observedB = append(observedB, b[0])
		default:
			t.Fatalf("unexpected marker: 0x%02x", b[0])
		}
	}, nil, nil)

	pre := newVirtualHostDispatchPre(l, explicitRealloc)
	instA := newVirtualHostDispatchInstance(t, pre, modA)
	instB := newVirtualHostDispatchInstance(t, pre, modB)

	virtA := instA.createVirtualInstance(1, virtualHostParsedInstance())
	virtB := instB.createVirtualInstance(1, virtualHostParsedInstance())

	if _, err := instA.createBridgeFrom(ctx, virtualHostNS, &coreInstance{virtual: virtA}, nil, nil, nil); err != nil {
		t.Fatalf("owner createBridgeFrom: %v", err)
	}
	if _, err := instB.createBridgeFrom(ctx, virtualHostNS, &coreInstance{virtual: virtB}, nil, nil, nil); err != nil {
		t.Fatalf("second createBridgeFrom: %v", err)
	}

	compiled := compileVirtualHostGuest(ctx, t, rt)
	defer compiled.Close(ctx)

	guestA := instantiateNamed(ctx, t, rt, compiled, instA.moduleName("guest"))
	guestB := instantiateNamed(ctx, t, rt, compiled, instB.moduleName("guest"))
	guestCtx := instantiateNamed(ctx, t, rt, compiled, "guest-context")
	guestLegacy := instantiateNamed(ctx, t, rt, compiled, "guest-legacy")

	callRun(ctx, t, guestA)
	callRun(ctx, t, guestA)
	callRun(ctx, t, guestB)
	callRun(ctx, t, guestB)

	if len(observedA) != 2 || observedA[0] != 0xAA || observedA[1] != 0xAA {
		t.Fatalf("caller A markers = %v, want [0xAA 0xAA]", observedA)
	}
	if len(observedB) != 2 || observedB[0] != 0xBB || observedB[1] != 0xBB {
		t.Fatalf("caller B markers = %v, want [0xBB 0xBB]", observedB)
	}

	callRun(WithInstance(ctx, instB), t, guestCtx)
	if len(observedB) != 3 || observedB[2] != 0xBB {
		t.Fatalf("context fallback markers = %v, want third 0xBB", observedB)
	}

	callRun(WithInstance(ctx, instB), t, guestA)
	if len(observedA) != 3 || observedA[2] != 0xAA {
		t.Fatalf("caller-name must win over context: A markers = %v", observedA)
	}

	callRun(ctx, t, guestLegacy)
	if len(observedA) != 4 || observedA[3] != 0xAA {
		t.Fatalf("legacy no-instance path markers = %v, want owner 0xAA", observedA)
	}

	if len(retained) != 7 {
		t.Fatalf("retained wrappers = %d, want 7", len(retained))
	}
	for i, mod := range retained {
		wantMem := modA
		switch i {
		case 2, 3, 4:
			wantMem = modB
		}
		if mod.Memory() != wantMem.Memory() {
			t.Fatalf("retained wrapper %d memory changed", i)
		}
		alloc := mod.ExportedFunction("cabi_realloc")
		if !explicitRealloc && i == 6 {
			if alloc != nil {
				t.Fatal("legacy fallback path must not invent a cross-instance allocator")
			}
			continue
		}
		if alloc == nil {
			t.Fatal("canonical allocator unavailable")
		}
		got, err := alloc.Call(ctx, 0, 0, 0, 0)
		wantMarker, ok := wantMem.Memory().Read(4, 1)
		if !ok || err != nil || len(got) != 1 || got[0] != uint64(wantMarker[0]) {
			t.Fatalf("allocator binding %d: got %v err %v want marker %v", i, got, err, wantMarker)
		}
	}

	if err := instA.Close(ctx); err != nil {
		t.Fatalf("owner Close: %v", err)
	}
	if instA.lookupVirtualHostFn(virtualHostNS, "lowered") != nil {
		t.Fatal("owner handler map must be cleared on Close")
	}

	before := len(observedB)
	callRun(ctx, t, guestB)
	if len(observedB) != before+1 || observedB[before] != 0xBB {
		t.Fatalf("survivor after owner.Close markers = %v", observedB)
	}

	if err := instB.Close(ctx); err != nil {
		t.Fatalf("survivor Close: %v", err)
	}
}

func TestVirtualHostDispatch_MissingBindingTraps(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	modA := instantiateMarkerModule(ctx, t, rt, "modA", 0xAA)
	calls := 0
	l := NewWithDefaults(rt)
	ns := l.Namespace(virtualHostNS)
	ns.DefineFunc("host-fn", func(ctx context.Context, mod api.Module, stack []uint64) {
		calls++
	}, nil, nil)

	pre := newVirtualHostDispatchPre(l, true)
	instA := newVirtualHostDispatchInstance(t, pre, modA)
	virtA := instA.createVirtualInstance(1, virtualHostParsedInstance())
	if _, err := instA.createBridgeFrom(ctx, virtualHostNS, &coreInstance{virtual: virtA}, nil, nil, nil); err != nil {
		t.Fatalf("owner createBridgeFrom: %v", err)
	}

	instC := newVirtualHostDispatchInstance(t, pre, nil)
	compiled := compileVirtualHostGuest(ctx, t, rt)
	defer compiled.Close(ctx)
	guestA := instantiateNamed(ctx, t, rt, compiled, instA.moduleName("guest"))
	callRun(ctx, t, guestA)
	if calls != 1 {
		t.Fatalf("owner calls = %d, want 1", calls)
	}

	guestC := instantiateNamed(ctx, t, rt, compiled, instC.moduleName("guest"))
	fn := guestC.ExportedFunction("run")
	if fn == nil {
		t.Fatal("missing run")
	}
	_, err := fn.Call(ctx)
	if err == nil {
		t.Fatal("missing binding must trap")
	}
	if !strings.Contains(err.Error(), "virtual host binding missing for "+virtualHostNS+"#lowered") {
		t.Fatalf("trap = %v", err)
	}
	if calls != 1 {
		t.Fatalf("missing binding used owner original: calls = %d", calls)
	}

	if err := instA.Close(ctx); err != nil {
		t.Fatalf("owner Close: %v", err)
	}
	if err := instC.Close(ctx); err != nil {
		t.Fatalf("unbound Close: %v", err)
	}
}

func newVirtualHostDispatchPre(l *Linker, explicitRealloc bool) *InstancePre {
	comp := &component.Component{
		Imports: []component.Import{
			{Name: virtualHostNS},
		},
		FuncIndexSpace: []component.FuncIndexEntry{
			{InstanceIdx: 0, ExportName: "host-fn"},
		},
		CoreFuncIndexSpace: []component.CoreFuncEntry{
			{
				Kind:       component.CoreFuncCanonLower,
				FuncIndex:  0,
				MemoryIdx:  0,
				ReallocIdx: 1,
			},
			{
				Kind:        component.CoreFuncAliasExport,
				InstanceIdx: 0,
				ExportName:  "cabi_realloc",
			},
		},
		Aliases: []component.Alias{
			{
				Parsed: &component.ParsedAlias{
					Sort:     0x00,
					CoreSort: 0x02,
					Instance: 0,
					Name:     "memory",
				},
			},
		},
	}
	if !explicitRealloc {
		comp.CoreFuncIndexSpace[0].ReallocIdx = -1
	}
	return &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: comp,
		},
	}
}

func virtualHostParsedInstance() *component.ParsedCoreInstance {
	return &component.ParsedCoreInstance{
		Kind: component.CoreInstanceFromExports,
		Exports: []component.CoreInstanceExport{
			{Kind: component.CoreExportFunc, Name: "lowered", Index: 0},
		},
	}
}

func newVirtualHostDispatchInstance(t *testing.T, pre *InstancePre, mem api.Module) *Instance {
	t.Helper()
	inst := &Instance{
		pre:            pre,
		instanceID:     atomic.AddUint64(&instanceCounter, 1),
		bridgeModules:  make(map[string]bool),
		virtualBridges: make(map[string]bool),
		coreInstances:  make(map[int]*coreInstance),
	}
	if mem != nil {
		inst.modules = []api.Module{mem}
		inst.coreInstances[0] = &coreInstance{module: mem}
	}
	instanceRegistry.Store(inst.instanceID, inst)
	t.Cleanup(func() {
		instanceRegistry.Delete(inst.instanceID)
	})
	return inst
}

func instantiateMarkerModule(ctx context.Context, t *testing.T, rt wazero.Runtime, name string, marker byte) api.Module {
	t.Helper()
	wasmBytes, err := wat.Compile(`(module
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			i32.const 4 i32.load8_u)
	)`)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(name))
	if err != nil {
		t.Fatalf("instantiate %s: %v", name, err)
	}
	t.Cleanup(func() { _ = mod.Close(ctx) })
	if !mod.Memory().Write(4, []byte{marker}) {
		t.Fatalf("write marker to %s", name)
	}
	return mod
}

func compileVirtualHostGuest(ctx context.Context, t *testing.T, rt wazero.Runtime) wazero.CompiledModule {
	t.Helper()
	wasmBytes, err := wat.Compile(`(module
		(import "test:pkg/iface@0.1.0" "lowered" (func $lowered))
		(func (export "run")
			call $lowered)
	)`)
	if err != nil {
		t.Fatalf("compile guest wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile guest: %v", err)
	}
	return compiled
}

func instantiateNamed(ctx context.Context, t *testing.T, rt wazero.Runtime, compiled wazero.CompiledModule, name string) api.Module {
	t.Helper()
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(name))
	if err != nil {
		t.Fatalf("instantiate %s: %v", name, err)
	}
	t.Cleanup(func() { _ = mod.Close(ctx) })
	return mod
}

func callRun(ctx context.Context, t *testing.T, guest api.Module) {
	t.Helper()
	fn := guest.ExportedFunction("run")
	if fn == nil {
		t.Fatal("missing run")
	}
	if _, err := fn.Call(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
}
