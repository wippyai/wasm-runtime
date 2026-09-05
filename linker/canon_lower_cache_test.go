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
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create module with memory and cabi_realloc
	wasmBytes, err := wat.Compile(`(module
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			i32.const 42)
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

	ns := l.Namespace("test:pkg/iface@0.1.0")
	ns.DefineFunc("host-fn", func(ctx context.Context, mod api.Module, stack []uint64) {
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

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: comp,
		},
	}

	instA := &Instance{
		pre: pre,
		coreInstances: map[int]*coreInstance{
			0: {module: modA},
		},
	}

	instB := &Instance{
		pre: pre,
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

	fnA := virtA.Get("lowered").Source.(HostFunc).Def.GetHandler()
	fnB := virtB.Get("lowered").Source.(HostFunc).Def.GetHandler()

	// Call instA multiple times
	fnA(ctx, callerMod, nil)
	fnA(ctx, callerMod, nil)

	// Call instB multiple times
	fnB(ctx, callerMod, nil)
	fnB(ctx, callerMod, nil)

	if len(observedMarkerA) != 2 || observedMarkerA[0] != 0xAA || observedMarkerA[1] != 0xAA {
		t.Fatalf("instA observed markers incorrect: %v", observedMarkerA)
	}
	if len(observedMarkerB) != 2 || observedMarkerB[0] != 0xBB || observedMarkerB[1] != 0xBB {
		t.Fatalf("instB observed markers incorrect: %v", observedMarkerB)
	}
}

func TestCanonLower_FirstUseFailurePreserved(t *testing.T) {
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
				ReallocIdx: -1,
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
