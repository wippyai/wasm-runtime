package linker

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestCanonLower_LazyResolutionAndCacheIsolation(t *testing.T) {
	t.Run("explicit-memory-and-realloc", func(t *testing.T) { testCanonLowerBindings(t, true) })
	t.Run("shared-realloc-fallback", func(t *testing.T) { testCanonLowerBindings(t, false) })
}

func testCanonLowerBindings(t *testing.T, explicitRealloc bool) {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create module with memory and cabi_realloc
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

	modA, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("modA"))
	if err != nil {
		t.Fatalf("instantiate modA: %v", err)
	}
	defer modA.Close(ctx)
	if !modA.Memory().Write(4, []byte{0xAA}) {
		t.Fatal("failed to write marker to modA")
	}

	modB, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("modB"))
	if err != nil {
		t.Fatalf("instantiate modB: %v", err)
	}
	defer modB.Close(ctx)
	if !modB.Memory().Write(4, []byte{0xBB}) {
		t.Fatal("failed to write marker to modB")
	}

	callerBytes, err := wat.Compile(`(module (memory 1))`)
	if err != nil {
		t.Fatalf("compile caller: %v", err)
	}
	callerComp, err := rt.CompileModule(ctx, callerBytes)
	if err != nil {
		t.Fatalf("compile caller: %v", err)
	}
	callerMod, err := rt.InstantiateModule(ctx, callerComp, wazero.NewModuleConfig().WithName("caller"))
	if err != nil {
		t.Fatalf("instantiate caller: %v", err)
	}
	defer callerMod.Close(ctx)

	l := NewWithDefaults(rt)
	var observedMarkerA []byte
	var observedMarkerB []byte
	var retained []api.Module

	ns := l.Namespace("test:pkg/iface@0.1.0")
	ns.DefineFunc("host-fn", func(ctx context.Context, mod api.Module, stack []uint64) {
		retained = append(retained, mod)
		b, ok := mod.Memory().Read(4, 1)
		if !ok {
			t.Fatal("failed to read memory")
		}
		switch b[0] {
		case 0xAA:
			observedMarkerA = append(observedMarkerA, b[0])
		case 0xBB:
			observedMarkerB = append(observedMarkerB, b[0])
		default:
			t.Fatalf("unexpected marker: 0x%02x", b[0])
		}
	}, nil, nil)

	comp := &component.Component{
		Imports: []component.Import{
			{Name: "test:pkg/iface@0.1.0"},
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
					CoreSort: 0x02, // memory
					Instance: 0,
					Name:     "memory",
				},
			},
		},
	}

	if !explicitRealloc {
		comp.CoreFuncIndexSpace[0].ReallocIdx = -1
	}

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: comp,
		},
	}

	instA := &Instance{
		pre:     pre,
		modules: []api.Module{modA},
		coreInstances: map[int]*coreInstance{
			0: {module: modA},
		},
	}

	instB := &Instance{
		pre:     pre,
		modules: []api.Module{modB},
		coreInstances: map[int]*coreInstance{
			0: {module: modB},
		},
	}

	parsedInst := &component.ParsedCoreInstance{
		Kind: component.CoreInstanceFromExports,
		Exports: []component.CoreInstanceExport{
			{Kind: component.CoreExportFunc, Name: "lowered", Index: 0},
		},
	}

	virtA := instA.createVirtualInstance(1, parsedInst)
	virtB := instB.createVirtualInstance(1, parsedInst)

	fnA := instA.collectVirtualExports(virtA, "test:pkg/iface@0.1.0")[0].Fn
	fnB := instB.collectVirtualExports(virtB, "test:pkg/iface@0.1.0")[0].Fn

	// Call instA multiple times
	fnA(WithInstance(ctx, instB), callerMod, nil)
	fnA(WithInstance(ctx, instB), callerMod, nil)

	// Call instB multiple times
	fnB(WithInstance(ctx, instA), callerMod, nil)
	fnB(WithInstance(ctx, instA), callerMod, nil)

	if len(observedMarkerA) != 2 || observedMarkerA[0] != 0xAA || observedMarkerA[1] != 0xAA {
		t.Fatalf("instA observed markers incorrect: %v", observedMarkerA)
	}
	if len(observedMarkerB) != 2 || observedMarkerB[0] != 0xBB || observedMarkerB[1] != 0xBB {
		t.Fatalf("instB observed markers incorrect: %v", observedMarkerB)
	}
	// A host may retain its module argument beyond the invocation. Later calls
	// must not replace the memory or allocator seen by an earlier call.
	for index, mod := range retained {
		want := modA
		if index >= 2 {
			want = modB
		}
		if mod.Memory() != want.Memory() {
			t.Fatal("retained canonical memory changed")
		}
		allocatorOwner := want
		if !explicitRealloc {
			allocatorOwner = modA
			if index < 2 {
				allocatorOwner = modB
			}
		}
		alloc := mod.ExportedFunction("cabi_realloc")
		if alloc == nil {
			t.Fatal("canonical allocator unavailable")
		}
		got, err := alloc.Call(ctx, 0, 0, 0, 0)
		wantMarker, _ := allocatorOwner.Memory().Read(4, 1)
		if err != nil || len(got) != 1 || got[0] != uint64(wantMarker[0]) {
			t.Fatalf("canonical allocator did not preserve explicit/fallback binding: %v, %v", got, err)
		}
	}
}

func TestCanonLower_FirstUseFailurePreserved(t *testing.T) {
	t.Run("partial-binding", func(t *testing.T) { testCanonLowerFailure(t, -1) })
	t.Run("complete-binding", func(t *testing.T) { testCanonLowerFailure(t, 1) })
}

func testCanonLowerFailure(t *testing.T, reallocIndex int32) {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	ns := l.Namespace("test:pkg/iface@0.1.0")
	ns.DefineFunc("host-fn", func(ctx context.Context, mod api.Module, stack []uint64) {
	}, nil, nil)

	comp := &component.Component{
		Imports: []component.Import{
			{Name: "test:pkg/iface@0.1.0"},
		},
		FuncIndexSpace: []component.FuncIndexEntry{
			{InstanceIdx: 0, ExportName: "host-fn"},
		},
		CoreFuncIndexSpace: []component.CoreFuncEntry{
			{
				Kind:       component.CoreFuncCanonLower,
				FuncIndex:  0,
				MemoryIdx:  0,
				ReallocIdx: reallocIndex,
			},
		},
		Aliases: []component.Alias{
			{
				Parsed: &component.ParsedAlias{
					Sort:     0x00,
					CoreSort: 0x02, // memory
					Instance: 0,
					Name:     "memory",
				},
			},
		},
	}

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: comp,
		},
	}

	// inst without coreInstances[0] module
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{},
	}

	parsedInst := &component.ParsedCoreInstance{
		Kind: component.CoreInstanceFromExports,
		Exports: []component.CoreInstanceExport{
			{Kind: component.CoreExportFunc, Name: "lowered", Index: 0},
		},
	}

	virt := inst.createVirtualInstance(1, parsedInst)
	fn := virt.Get("lowered").Source.(HostFunc).Def.GetHandler()

	assertPanics := func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic, got none")
			}
			msg, ok := r.(string)
			if !ok || msg != "canonical import memory unavailable" {
				t.Fatalf("expected 'canonical import memory unavailable', got %v", r)
			}
		}()
		fn(ctx, nil, nil)
	}

	// First call should fail
	assertPanics()
	// Subsequent calls must also preserve the failure
	assertPanics()
}
