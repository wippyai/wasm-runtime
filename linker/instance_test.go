package linker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/linker/internal/invoke"
	"github.com/wippyai/wasm-runtime/wat"
	"go.bytecodealliance.org/wit"
)

func TestNewInstance_NilGraph(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		graph:  nil,
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}

	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}

	if inst.resources == nil {
		t.Error("resources should not be nil")
	}
}

func TestInstance_GetExport_NotFound(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	_, ok := inst.GetExport("nonexistent")
	if ok {
		t.Error("GetExport should return false for nonexistent")
	}
}

func TestInstance_Call_NotFound(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	_, err := inst.Call(ctx, "nonexistent")
	if err == nil {
		t.Error("Call should error for nonexistent export")
	}
}

func TestInstance_Call_NilCoreFunc(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	// Manually add an export with nil CoreFunc
	inst.exports["test"] = Export{Name: "test", CoreFunc: nil}

	_, err := inst.Call(ctx, "test")
	if err == nil {
		t.Error("Call should error for nil CoreFunc")
	}
}

func TestInstance_Modules(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	mods := inst.Modules()
	if mods == nil {
		t.Error("Modules() should not return nil")
	}
	if len(mods) != 0 {
		t.Errorf("Modules() should return empty slice, got %d", len(mods))
	}
}

func TestInstance_Resources(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	res := inst.Resources()
	if res == nil {
		t.Error("Resources() should not return nil")
	}
}

func TestInstance_Close(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	err := inst.Close(ctx)
	if err != nil {
		t.Errorf("Close error: %v", err)
	}

	if inst.modules != nil {
		t.Error("modules should be nil after Close")
	}
	if inst.exports != nil {
		t.Error("exports should be nil after Close")
	}
}

func TestInstance_Close_CleansUpHostModules(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	// Manually create a host module and track it
	hostModName := "test-host-module"
	_, err := rt.NewHostModuleBuilder(hostModName).
		NewFunctionBuilder().
		WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}), nil, nil).
		Export("test").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("create host module: %v", err)
	}
	inst.bridgeModules[hostModName] = true
	// Add refs immediately (as the production code now does)
	l.addBridgeRefs(inst.bridgeModules)

	// Verify module exists before close
	if rt.Module(hostModName) == nil {
		t.Fatal("host module should exist before close")
	}

	// Close should release refs via ref counting (module still has ref count > 0)
	err = inst.Close(ctx)
	if err != nil {
		t.Errorf("Close error: %v", err)
	}

	// Module no longer exists after last ref released
	// (ref count went from 1 to 0, so it was closed)
	if rt.Module(hostModName) != nil {
		t.Error("bridge module should be closed when last reference is released")
	}

	// bridgeModules slice should be nil to release memory
	if inst.bridgeModules != nil {
		t.Error("bridgeModules should be nil after Close")
	}
}

func TestInstance_Close_RefCountingProtectsSharedModules(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}

	// Create two instances that share the same bridge module
	inst1, _ := pre.NewInstance(ctx)
	inst2, _ := pre.NewInstance(ctx)

	// Manually create a host module and track it in both instances
	hostModName := "shared-module"
	_, err := rt.NewHostModuleBuilder(hostModName).
		NewFunctionBuilder().
		WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}), nil, nil).
		Export("test").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("create host module: %v", err)
	}

	// Simulate both instances creating/tracking the module
	// inst1 creates it (adds to bridgeModules and refs)
	inst1.bridgeModules[hostModName] = true
	l.addBridgeRefs(map[string]bool{hostModName: true})

	// inst2 doesn't add to bridgeModules (didn't create) but uses the module

	// Verify module exists before close
	if rt.Module(hostModName) == nil {
		t.Fatal("host module should exist before close")
	}

	// Close inst1 - module should still exist (ref count goes to 0 but inst2 might need it)
	// Actually in this test inst2 didn't add refs, so module closes
	err = inst1.Close(ctx)
	if err != nil {
		t.Errorf("Close error: %v", err)
	}

	// Module no longer exists after inst1's ref is released
	if rt.Module(hostModName) != nil {
		t.Error("bridge module should be closed when last reference is released")
	}

	// Close inst2 (no-op for this module since it wasn't tracked)
	err = inst2.Close(ctx)
	if err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestCoreExportKindToEntity(t *testing.T) {
	tests := []struct {
		kind byte
		want EntityKind
	}{
		{0x00, EntityFunc},   // CoreExportFunc
		{0x01, EntityTable},  // CoreExportTable
		{0x02, EntityMemory}, // CoreExportMemory
		{0x03, EntityGlobal}, // CoreExportGlobal
		{0xFF, EntityFunc},   // Unknown defaults to Func
	}

	for _, tt := range tests {
		got := coreExportKindToEntity(tt.kind)
		if got != tt.want {
			t.Errorf("coreExportKindToEntity(%d) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestBuildCoreEntitySpace_Empty(t *testing.T) {
	comp := &component.Component{}
	entries := buildCoreEntitySpace(comp, 0x02)
	if len(entries) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(entries))
	}
}

func TestBuildCoreEntitySpace_WithMemoryAliases(t *testing.T) {
	comp := &component.Component{
		Aliases: []component.Alias{
			{Parsed: &component.ParsedAlias{Sort: 0x00, CoreSort: 0x02, Instance: 0, Name: "memory0"}},
			{Parsed: &component.ParsedAlias{Sort: 0x00, CoreSort: 0x00, Instance: 0, Name: "func0"}},
			{Parsed: &component.ParsedAlias{Sort: 0x00, CoreSort: 0x02, Instance: 1, Name: "memory1"}},
			{Parsed: nil}, // Should be skipped
		},
	}

	entries := buildCoreEntitySpace(comp, 0x02)
	if len(entries) != 2 {
		t.Fatalf("expected 2 memory entries, got %d", len(entries))
	}

	if entries[0].instanceIdx != 0 || entries[0].exportName != "memory0" {
		t.Errorf("first entry mismatch: got %+v", entries[0])
	}
	if entries[1].instanceIdx != 1 || entries[1].exportName != "memory1" {
		t.Errorf("second entry mismatch: got %+v", entries[1])
	}
}

func TestBuildCoreEntitySpace_WithGlobalAliases(t *testing.T) {
	comp := &component.Component{
		Aliases: []component.Alias{
			{Parsed: &component.ParsedAlias{Sort: 0x00, CoreSort: 0x03, Instance: 0, Name: "global0"}},
			{Parsed: &component.ParsedAlias{Sort: 0x01, CoreSort: 0x03, Instance: 0, Name: "notcore"}},
		},
	}

	entries := buildCoreEntitySpace(comp, 0x03)
	if len(entries) != 1 {
		t.Fatalf("expected 1 global entry, got %d", len(entries))
	}
}

func TestCallStart_NoComponent(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l, component: nil}
	inst, _ := pre.NewInstance(ctx)

	err := inst.callStart(ctx)
	if err != nil {
		t.Errorf("callStart with nil component should succeed: %v", err)
	}
}

func TestCallStart_NoStartSection(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{Start: nil},
		},
	}
	inst, _ := pre.NewInstance(ctx)

	err := inst.callStart(ctx)
	if err != nil {
		t.Errorf("callStart with no start section should succeed: %v", err)
	}
}

func TestCallStart_FuncIndexOutOfRange(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				Start:          &component.StartFunc{FuncIndex: 99},
				FuncIndexSpace: nil,
			},
		},
	}
	inst := &Instance{pre: pre, coreInstances: make(map[int]*coreInstance)}

	err := inst.callStart(ctx)
	if err == nil {
		t.Error("callStart with out-of-range func index should error")
	}
}

func TestCallStart_InstanceNotFound(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				Start: &component.StartFunc{FuncIndex: 0},
				FuncIndexSpace: []component.FuncIndexEntry{
					{InstanceIdx: 99, ExportName: "start"},
				},
			},
		},
	}
	inst := &Instance{pre: pre, coreInstances: make(map[int]*coreInstance)}

	err := inst.callStart(ctx)
	if err == nil {
		t.Error("callStart with missing instance should error")
	}
}

func TestCallStart_WithArgsNotSupported(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create a module with a start function
	wasmBytes, err := wat.Compile(`(module (func (export "start")))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				Start: &component.StartFunc{
					FuncIndex: 0,
					Args:      []uint32{0, 1}, // Has args
				},
				FuncIndexSpace: []component.FuncIndexEntry{
					{InstanceIdx: 0, ExportName: "start"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	err = inst.callStart(ctx)
	if err == nil {
		t.Error("callStart with args should error when value space is empty")
	}
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected 'out of range' error, got: %v", err)
	}
}

func TestCallStart_WithArgsFromValueSpace(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create a module with a start function that takes two i32 args
	wasmBytes, err := wat.Compile(`(module
		(global (export "result") (mut i32) (i32.const 0))
		(func (export "start") (param i32 i32)
			(global.set 0 (i32.add (local.get 0) (local.get 1)))
		)
	)`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				Start: &component.StartFunc{
					FuncIndex: 0,
					Args:      []uint32{0, 1}, // Use values from value space
				},
				FuncIndexSpace: []component.FuncIndexEntry{
					{InstanceIdx: 0, ExportName: "start"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
		valueSpace:    []uint64{10, 32}, // Pre-populate value space with test values
	}

	err = inst.callStart(ctx)
	if err != nil {
		t.Fatalf("callStart with args failed: %v", err)
	}

	// Verify the start function was called with correct args
	result := mod.ExportedGlobal("result")
	if result == nil {
		t.Fatal("result global not found")
	}
	if got := result.Get(); got != 42 {
		t.Errorf("expected result=42, got %d", got)
	}
}

func TestGetCoreFunc_OutOfRange(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst := &Instance{pre: pre, coreInstances: make(map[int]*coreInstance)}

	comp := &component.Component{}
	fn := inst.getCoreFunc(comp, 99)
	if fn != nil {
		t.Error("getCoreFunc should return nil for out-of-range index")
	}
}

func TestGetCoreFunc_CanonLower(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst := &Instance{pre: pre, coreInstances: make(map[int]*coreInstance)}

	comp := &component.Component{
		CoreFuncIndexSpace: []component.CoreFuncEntry{
			{Kind: component.CoreFuncCanonLower},
		},
	}
	fn := inst.getCoreFunc(comp, 0)
	if fn != nil {
		t.Error("getCoreFunc should return nil for CanonLower")
	}
}

func TestGetCoreFunc_UnknownKind(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst := &Instance{pre: pre, coreInstances: make(map[int]*coreInstance)}

	comp := &component.Component{
		CoreFuncIndexSpace: []component.CoreFuncEntry{
			{Kind: 99},
		},
	}
	fn := inst.getCoreFunc(comp, 0)
	if fn != nil {
		t.Error("getCoreFunc should return nil for unknown kind")
	}
}

func TestCreateBridgeModule_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create a host module first using linker's runtime
	_, err := l.Runtime().NewHostModuleBuilder("existing").
		NewFunctionBuilder().
		WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}), nil, nil).
		Export("test").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("create existing module: %v", err)
	}

	pre := &InstancePre{linker: l}
	inst := &Instance{pre: pre, coreInstances: make(map[int]*coreInstance)}

	// Create a dummy source module
	wasmBytes, err := wat.Compile(`(module (func (export "dummy")))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := l.Runtime().CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	source, err := l.Runtime().InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("source"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer source.Close(ctx)

	// Should not error when module already exists
	_, err = inst.createBridgeFrom(ctx, "existing", &coreInstance{module: source}, nil, nil, nil)
	if err != nil {
		t.Errorf("createBridgeFrom should succeed for existing module: %v", err)
	}
}

func TestCreateBridgeFrom_EmptyVirtual(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst := &Instance{
		pre:           pre,
		coreInstances: make(map[int]*coreInstance),
	}

	// Create an empty virtual instance
	virt := NewVirtualInstance("test")

	// Should return false (no exports created) without error
	created, err := inst.createBridgeFrom(ctx, "empty", &coreInstance{virtual: virt}, nil, nil, nil)
	if err != nil {
		t.Errorf("createBridgeFrom error: %v", err)
	}
	if created {
		t.Error("created should be false for empty virtual")
	}
}

func TestCreateBridgeFrom_WithHostFunc(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst := &Instance{
		pre:           pre,
		coreInstances: make(map[int]*coreInstance),
		bridgeModules: make(map[string]bool),
	}

	// Create a virtual instance with a host func
	virt := NewVirtualInstance("test")
	virt.Define("myfunc", Entity{
		Kind: EntityFunc,
		Source: HostFunc{
			Def: &FuncDef{
				Handler:     func(ctx context.Context, mod api.Module, stack []uint64) {},
				ParamTypes:  []api.ValueType{},
				ResultTypes: []api.ValueType{},
			},
		},
	})

	// Should return true (export created)
	created, err := inst.createBridgeFrom(ctx, "withfunc", &coreInstance{virtual: virt}, nil, nil, nil)
	if err != nil {
		t.Errorf("createBridgeFrom error: %v", err)
	}
	if !created {
		t.Error("created should be true when func exported")
	}

	// Module should exist in runtime
	mod := rt.Module("withfunc")
	if mod == nil {
		t.Error("module should exist in runtime")
	}
}

func TestCreateBridgeFrom_WithModuleExport(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create a source module with a function
	sourceWat, err := wat.Compile(`
		(module
			(func (export "add") (param i32 i32) (result i32)
				local.get 0
				local.get 1
				i32.add
			)
		)
	`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	sourceCompiled, err := rt.CompileModule(ctx, sourceWat)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	source, err := rt.InstantiateModule(ctx, sourceCompiled, wazero.NewModuleConfig().WithName("source"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer source.Close(ctx)

	pre := &InstancePre{linker: l}
	inst := &Instance{
		pre:           pre,
		coreInstances: make(map[int]*coreInstance),
		bridgeModules: make(map[string]bool),
	}

	// Create virtual instance with ModuleExport entity
	virt := NewVirtualInstance("test")
	virt.DefineModuleExport("add", EntityFunc, source, "add")

	// Add a non-func entity to test skipping
	virt.DefineMemory("mem", nil)

	created, err := inst.createBridgeFrom(ctx, "bridge-mod", &coreInstance{virtual: virt}, nil, nil, nil)
	if err != nil {
		t.Fatalf("createBridgeFrom: %v", err)
	}
	if !created {
		t.Error("should create bridge module")
	}

	// Verify the function was bridged by calling it through a consumer
	consumerWat, err := wat.Compile(`
		(module
			(import "bridge-mod" "add" (func $add (param i32 i32) (result i32)))
			(func (export "test_add") (param i32 i32) (result i32)
				local.get 0
				local.get 1
				call $add
			)
		)
	`)
	if err != nil {
		t.Fatalf("wat compile consumer: %v", err)
	}
	consumerCompiled, err := rt.CompileModule(ctx, consumerWat)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	consumer, err := rt.InstantiateModule(ctx, consumerCompiled, wazero.NewModuleConfig().WithName("consumer"))
	if err != nil {
		t.Fatalf("instantiate consumer: %v", err)
	}
	defer consumer.Close(ctx)

	// Call through the bridge
	fn := consumer.ExportedFunction("test_add")
	results, err := fn.Call(ctx, 10, 5)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if results[0] != 15 {
		t.Errorf("got %d, want 15", results[0])
	}
}

func TestCreateBridgeFrom_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create a module first
	_, err := rt.NewHostModuleBuilder("existing").
		NewFunctionBuilder().
		WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}), nil, nil).
		Export("test").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("create existing: %v", err)
	}

	pre := &InstancePre{linker: l}
	inst := &Instance{
		pre:           pre,
		coreInstances: make(map[int]*coreInstance),
	}

	virt := NewVirtualInstance("test")

	// When source has no exports to register, return false (no bridge created)
	// The existing module remains but we didn't set up a bridge from the empty source
	created, err := inst.createBridgeFrom(ctx, "existing", &coreInstance{virtual: virt}, nil, nil, nil)
	if err != nil {
		t.Errorf("error: %v", err)
	}
	if created {
		t.Error("should return false when source has no exports")
	}
}

func TestCreateBridgeFrom_ForwardsFunctions(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create source module with functions
	sourceWat, err := wat.Compile(`
		(module
			(func (export "add") (param i32 i32) (result i32)
				local.get 0
				local.get 1
				i32.add
			)
		)
	`)
	if err != nil {
		t.Fatalf("wat compile source: %v", err)
	}
	sourceCompiled, err := rt.CompileModule(ctx, sourceWat)
	if err != nil {
		t.Fatalf("compile source: %v", err)
	}
	source, err := rt.InstantiateModule(ctx, sourceCompiled, wazero.NewModuleConfig().WithName("source"))
	if err != nil {
		t.Fatalf("instantiate source: %v", err)
	}
	defer source.Close(ctx)

	pre := &InstancePre{linker: l}
	inst := &Instance{
		pre:           pre,
		bridgeModules: make(map[string]bool),
	}

	// Create bridge module
	_, err = inst.createBridgeFrom(ctx, "bridge", &coreInstance{module: source}, nil, nil, nil)
	if err != nil {
		t.Fatalf("createBridgeFrom: %v", err)
	}

	// Verify bridge module exists
	bridge := rt.Module("bridge")
	if bridge == nil {
		t.Fatal("bridge module should exist")
	}

	// Create a consumer module that imports from the bridge
	consumerWat, err := wat.Compile(`
		(module
			(import "bridge" "add" (func $add (param i32 i32) (result i32)))
			(func (export "call_add") (param i32 i32) (result i32)
				local.get 0
				local.get 1
				call $add
			)
		)
	`)
	if err != nil {
		t.Fatalf("wat compile consumer: %v", err)
	}
	consumerCompiled, err := rt.CompileModule(ctx, consumerWat)
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	consumer, err := rt.InstantiateModule(ctx, consumerCompiled, wazero.NewModuleConfig().WithName("consumer"))
	if err != nil {
		t.Fatalf("instantiate consumer: %v", err)
	}
	defer consumer.Close(ctx)

	// Call the consumer's function which uses the bridged add
	callAddFn := consumer.ExportedFunction("call_add")
	if callAddFn == nil {
		t.Fatal("call_add function should be exported")
	}
	results, err := callAddFn.Call(ctx, 3, 5)
	if err != nil {
		t.Fatalf("call call_add: %v", err)
	}
	if len(results) != 1 || results[0] != 8 {
		t.Errorf("call_add(3, 5) = %v, want [8]", results)
	}

	// Verify host module is tracked
	if len(inst.bridgeModules) != 1 || !inst.bridgeModules["bridge"] {
		t.Errorf("bridgeModules = %v, want {bridge: true}", inst.bridgeModules)
	}
}

func TestCreateBridgeModule_EmptySource(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create source module with no exports
	wasmBytes, err := wat.Compile(`(module)`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	source, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("empty"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer source.Close(ctx)

	pre := &InstancePre{linker: l}
	inst := &Instance{
		pre:           pre,
		bridgeModules: make(map[string]bool),
	}

	// Create bridge module - should succeed but create no module
	_, err = inst.createBridgeFrom(ctx, "nobr", &coreInstance{module: source}, nil, nil, nil)
	if err != nil {
		t.Fatalf("createBridgeFrom: %v", err)
	}

	// Bridge module should NOT exist (no exports to bridge)
	if rt.Module("nobr") != nil {
		t.Error("bridge module should not exist for empty source")
	}
}

func TestCreateVirtualInstance_Basic(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create source module with exports
	wasmBytes, err := wat.Compile(`
		(module
			(memory (export "memory") 1)
			(global (export "g") i32 (i32.const 42))
			(func (export "fn") (result i32) i32.const 123)
		)
	`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	source, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("source"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer source.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		modules:       []api.Module{source},
		coreInstances: map[int]*coreInstance{0: {module: source}},
	}

	parsed := &component.ParsedCoreInstance{
		Kind: component.CoreInstanceFromExports,
		Exports: []component.CoreInstanceExport{
			{Kind: 0x00, Name: "fn", Index: 0},     // func
			{Kind: 0x02, Name: "memory", Index: 0}, // memory
			{Kind: 0x03, Name: "g", Index: 0},      // global
		},
	}

	virt := inst.createVirtualInstance(0, parsed)
	if virt == nil {
		t.Fatal("createVirtualInstance returned nil")
	}

	// Check function entity
	fnEnt := virt.Get("fn")
	if fnEnt == nil || fnEnt.Kind != EntityFunc {
		t.Errorf("fn entity missing or wrong kind: %v", fnEnt)
	}

	// Check memory entity
	memEnt := virt.Get("memory")
	if memEnt == nil || memEnt.Kind != EntityMemory {
		t.Errorf("memory entity missing or wrong kind: %v", memEnt)
	}

	// Check global entity
	gEnt := virt.Get("g")
	if gEnt == nil || gEnt.Kind != EntityGlobal {
		t.Errorf("global entity missing or wrong kind: %v", gEnt)
	}
}

func TestResolveCompFunc_Lift(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create module with function
	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	sources := map[uint32]compFuncSource{
		0: {kind: compFuncLift, coreFunc: 0},
	}

	fn := inst.resolveCompFunc(pre.component.Raw, sources, 0)
	if fn == nil {
		t.Fatal("resolveCompFunc returned nil for lift")
	}
}

func TestResolveCompFunc_Alias(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				FuncIndexSpace: []component.FuncIndexEntry{
					{InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	sources := map[uint32]compFuncSource{
		0: {kind: compFuncAlias},
	}

	fn := inst.resolveCompFunc(pre.component.Raw, sources, 0)
	if fn == nil {
		t.Fatal("resolveCompFunc returned nil for alias")
	}
}

func TestResolveCompFunc_ReExportChain(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	// Chain: 2 -> 1 -> 0 (lift)
	sources := map[uint32]compFuncSource{
		0: {kind: compFuncLift, coreFunc: 0},
		1: {kind: compFuncExport, reExportOf: 0},
		2: {kind: compFuncExport, reExportOf: 1},
	}

	fn := inst.resolveCompFunc(pre.component.Raw, sources, 2)
	if fn == nil {
		t.Fatal("resolveCompFunc returned nil for re-export chain")
	}
}

func TestResolveCompFunc_CycleDetection(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}
	inst := &Instance{pre: pre}

	// Cycle: 0 -> 1 -> 0
	sources := map[uint32]compFuncSource{
		0: {kind: compFuncExport, reExportOf: 1},
		1: {kind: compFuncExport, reExportOf: 0},
	}

	fn := inst.resolveCompFunc(pre.component.Raw, sources, 0)
	if fn != nil {
		t.Error("resolveCompFunc should return nil for cycle")
	}
}

func TestResolveCompFunc_NotFound(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}
	inst := &Instance{pre: pre}

	sources := map[uint32]compFuncSource{}

	fn := inst.resolveCompFunc(pre.component.Raw, sources, 99)
	if fn != nil {
		t.Error("resolveCompFunc should return nil for not found")
	}
}

func TestNewInstance_NilParsedInstance(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create a graph with a nil Parsed field
	instances := []component.CoreInstance{{Parsed: nil}}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker: l,
		graph:  graph,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance should skip nil instances: %v", err)
	}
	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}
}

func TestNewInstance_ModuleIndexOutOfRange(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create instance that references module index 99 (doesn't exist)
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 99, // out of range
		}},
	}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{}, // empty
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}

	_, err := pre.NewInstance(ctx)
	if err == nil {
		t.Error("NewInstance should error for module index out of range")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention out of range: %v", err)
	}
}

func TestNewInstance_ArgKindNotInstance(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Compile a minimal module
	wasmBytes := []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close(ctx)

	// Create instance with arg that's not CoreInstantiateInstance kind
	// InstanceIndex=999 is out of range, so graph ignores it (no cycle)
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 0,
			Args: []component.CoreInstanceArg{
				{
					Kind:          0xFF, // not CoreInstantiateInstance
					Name:          "test",
					InstanceIndex: 999, // out of range, ignored by graph
				},
			},
		}},
	}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{compiled},
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance should skip non-instance args: %v", err)
	}
	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}
}

func TestNewInstance_WithBridgeFromModule(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := New(rt, Options{})

	// Create module 0: exports a function
	mod0Src := `(module
		(func $f (result i32) (i32.const 42))
		(export "func" (func $f))
	)`
	mod0Bytes, err := wat.Compile(mod0Src)
	if err != nil {
		t.Fatalf("compile mod0 wat: %v", err)
	}
	compiled0, err := rt.CompileModule(ctx, mod0Bytes)
	if err != nil {
		t.Fatalf("compile mod0: %v", err)
	}
	defer compiled0.Close(ctx)

	// Create module 1: imports "source"."func"
	mod1Src := `(module
		(import "source" "func" (func $imported (result i32)))
		(func $call (result i32) (call $imported))
		(export "call" (func $call))
	)`
	mod1Bytes, err := wat.Compile(mod1Src)
	if err != nil {
		t.Fatalf("compile mod1 wat: %v", err)
	}
	compiled1, err := rt.CompileModule(ctx, mod1Bytes)
	if err != nil {
		t.Fatalf("compile mod1: %v", err)
	}
	defer compiled1.Close(ctx)

	// Create instance graph: instance 1 depends on instance 0
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 0,
		}},
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 1,
			Args: []component.CoreInstanceArg{
				{
					Kind:          component.CoreInstantiateInstance,
					Name:          "source",
					InstanceIndex: 0, // points to instance 0
				},
			},
		}},
	}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{compiled0, compiled1},
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}

	// Both modules should be instantiated
	if len(inst.Modules()) != 2 {
		t.Errorf("expected 2 modules, got %d", len(inst.Modules()))
	}
}

func TestNewInstance_WithFromExports(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := New(rt, Options{})

	// Create module that exports a function and memory
	modSrc := `(module
		(memory 1)
		(func $f (result i32) (i32.const 42))
		(export "func" (func $f))
		(export "mem" (memory 0))
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, modBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close(ctx)

	// Instance 0: instantiate module
	// Instance 1: from-exports with func and memory
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 0,
		}},
		{Parsed: &component.ParsedCoreInstance{
			Kind: component.CoreInstanceFromExports,
			Exports: []component.CoreInstanceExport{
				{Name: "func", Kind: component.CoreExportFunc, Index: 0},
				{Name: "mem", Kind: component.CoreExportMemory, Index: 0},
			},
		}},
	}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{compiled},
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "func"},
				},
			},
		},
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	if inst == nil {
		t.Fatal("NewInstance returned nil")
	}

	// Should have 1 real module
	if len(inst.Modules()) != 1 {
		t.Errorf("expected 1 module, got %d", len(inst.Modules()))
	}

	// Should have a virtual instance for index 1
	if inst.coreInstances[1] == nil {
		t.Error("expected virtual instance at index 1")
	}
}

func TestNewInstance_WithBridgeFromVirtual(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := New(rt, Options{})

	// Module 0: exports a function
	mod0Src := `(module
		(func $f (result i32) (i32.const 42))
		(export "myfunc" (func $f))
	)`
	mod0Bytes, err := wat.Compile(mod0Src)
	if err != nil {
		t.Fatalf("compile mod0 wat: %v", err)
	}
	compiled0, err := rt.CompileModule(ctx, mod0Bytes)
	if err != nil {
		t.Fatalf("compile mod0: %v", err)
	}
	defer compiled0.Close(ctx)

	// Module 1: imports "virtual"."myfunc"
	mod1Src := `(module
		(import "virtual" "myfunc" (func $imported (result i32)))
		(func $call (result i32) (call $imported))
		(export "call" (func $call))
	)`
	mod1Bytes, err := wat.Compile(mod1Src)
	if err != nil {
		t.Fatalf("compile mod1 wat: %v", err)
	}
	compiled1, err := rt.CompileModule(ctx, mod1Bytes)
	if err != nil {
		t.Fatalf("compile mod1: %v", err)
	}
	defer compiled1.Close(ctx)

	// Instance 0: instantiate module 0
	// Instance 1: from-exports referencing instance 0
	// Instance 2: instantiate module 1, with args pointing to virtual instance 1
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 0,
		}},
		{Parsed: &component.ParsedCoreInstance{
			Kind: component.CoreInstanceFromExports,
			Exports: []component.CoreInstanceExport{
				{Name: "myfunc", Kind: component.CoreExportFunc, Index: 0},
			},
		}},
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 1,
			Args: []component.CoreInstanceArg{
				{
					Kind:          component.CoreInstantiateInstance,
					Name:          "virtual",
					InstanceIndex: 1, // points to virtual instance
				},
			},
		}},
	}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{compiled0, compiled1},
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "myfunc"},
				},
			},
		},
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}

	// Should have 2 real modules
	if len(inst.Modules()) != 2 {
		t.Errorf("expected 2 modules, got %d", len(inst.Modules()))
	}
}

func TestNewInstance_WithGlobalExport(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := New(rt, Options{})

	// Create module that exports a global
	modSrc := `(module
		(global $g (export "counter") (mut i32) (i32.const 42))
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, modBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close(ctx)

	// Instance 0: instantiate module
	// Instance 1: from-exports with global
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 0,
		}},
		{Parsed: &component.ParsedCoreInstance{
			Kind: component.CoreInstanceFromExports,
			Exports: []component.CoreInstanceExport{
				{Name: "counter", Kind: component.CoreExportGlobal, Index: 0},
			},
		}},
	}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{compiled},
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}

	// Should have virtual instance with global
	ci := inst.coreInstances[1]
	if ci == nil || ci.virtual == nil {
		t.Fatal("expected virtual instance")
	}

	e := ci.virtual.Get("counter")
	if e == nil {
		t.Error("expected global entity")
	}
}

func TestNewInstance_WithTableExport(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := New(rt, Options{})

	// Create module with table
	modSrc := `(module
		(table $t (export "tbl") 10 funcref)
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, modBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close(ctx)

	// Instance with table export
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 0,
		}},
		{Parsed: &component.ParsedCoreInstance{
			Kind: component.CoreInstanceFromExports,
			Exports: []component.CoreInstanceExport{
				{Name: "tbl", Kind: component.CoreExportTable, Index: 0},
			},
		}},
	}
	graph := component.NewInstanceGraph(instances)

	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{compiled},
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}

	// Should have virtual instance (table skipped per wazero limitation)
	virt := inst.coreInstances[1]
	if virt == nil {
		t.Fatal("expected virtual instance")
	}
}

func TestInstance_UsesResolverForImports(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	// Create a virtual instance with the "add" function
	virt := NewVirtualInstance("host")
	addCalled := false
	addHandler := func(ctx context.Context, mod api.Module, stack []uint64) {
		addCalled = true
		a := uint32(stack[0])
		b := uint32(stack[1])
		stack[0] = uint64(a + b)
	}
	virt.DefineFunc("add", &FuncDef{
		Name:        "add",
		Handler:     addHandler,
		ParamTypes:  []api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		ResultTypes: []api.ValueType{api.ValueTypeI32},
	})

	// Register in Resolver under the import name
	resolver := l.Resolver()
	resolver.RegisterInstance("test:minimal/host@0.1.0", virt)

	// Instantiate - should use the Resolver-registered instance
	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	// Call compute-using-host which uses the host "add" function
	results, err := inst.CallRaw(ctx, "compute-using-host", 3, 4)
	if err != nil {
		t.Fatalf("CallRaw error: %v", err)
	}
	if len(results) == 0 || results[0] != 7 {
		t.Errorf("compute-using-host(3, 4) = %v, want 7", results)
	}

	if !addCalled {
		t.Error("Resolver-registered add function was not called")
	}
}

func TestInstance_ResolverWithModule(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	// Create a real WASM module that exports "add" (a+b)
	// wazero host modules don't support ExportedFunction, so we need real WASM
	modSrc := `(module
		(func $add (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
		(export "add" (func $add))
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	compiled, err := rt.CompileModule(ctx, modBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close(ctx)

	wasmMod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("add-impl"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer wasmMod.Close(ctx)

	// Register the real WASM module in Resolver
	resolver := l.Resolver()
	resolver.RegisterModule("test:minimal/host@0.1.0", wasmMod)

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	results, err := inst.CallRaw(ctx, "compute-using-host", 5, 3)
	if err != nil {
		t.Fatalf("CallRaw error: %v", err)
	}
	if len(results) == 0 || results[0] != 8 {
		t.Errorf("compute-using-host(5, 3) = %v, want 8", results)
	}
}

func TestInstance_ResolverVirtualInstancePriority(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	// Register both VirtualInstance and Module under same name
	// VirtualInstance should win (checked first in code)
	virtCalled := false
	virt := NewVirtualInstance("host")
	virt.DefineFunc("add", &FuncDef{
		Name: "add",
		Handler: func(ctx context.Context, mod api.Module, stack []uint64) {
			virtCalled = true
			stack[0] = uint64(uint32(stack[0]) + uint32(stack[1]))
		},
		ParamTypes:  []api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
		ResultTypes: []api.ValueType{api.ValueTypeI32},
	})

	// Create a real WASM module (not used if VirtualInstance wins)
	modSrc := `(module
		(func $add (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
		(export "add" (func $add))
	)`
	modBytes, _ := wat.Compile(modSrc)
	compiled, _ := rt.CompileModule(ctx, modBytes)
	defer compiled.Close(ctx)
	wasmMod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("priority-test"))
	defer wasmMod.Close(ctx)

	resolver := l.Resolver()
	resolver.RegisterInstance("test:minimal/host@0.1.0", virt)
	resolver.RegisterModule("test:minimal/host@0.1.0", wasmMod)

	pre, _ := l.Instantiate(ctx, validated)
	defer pre.Close(ctx)

	inst, _ := pre.NewInstance(ctx)
	defer inst.Close(ctx)

	_, _ = inst.CallRaw(ctx, "compute-using-host", 1, 2)

	if !virtCalled {
		t.Error("VirtualInstance should be called (has priority)")
	}
}

func TestInstance_ResolverFallsBackToBindings(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	// Register something unrelated in Resolver
	resolver := l.Resolver()
	resolver.RegisterInstance("unrelated:something@1.0.0", NewVirtualInstance("unrelated"))

	// Define the actual import via namespace (bindings)
	bindingCalled := false
	ns := l.Namespace("test:minimal/host@0.1.0")
	ns.DefineFunc("add", func(ctx context.Context, mod api.Module, stack []uint64) {
		bindingCalled = true
		stack[0] = uint64(uint32(stack[0]) + uint32(stack[1]))
	}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})

	pre, _ := l.Instantiate(ctx, validated)
	defer pre.Close(ctx)

	inst, _ := pre.NewInstance(ctx)
	defer inst.Close(ctx)

	results, _ := inst.CallRaw(ctx, "compute-using-host", 10, 5)
	if len(results) == 0 || results[0] != 15 {
		t.Errorf("compute-using-host(10, 5) = %v, want 15", results)
	}

	if !bindingCalled {
		t.Error("Binding should be used when Resolver has nothing for that import")
	}
}

func TestLinker_ResolverLazyInitReturnsSameInstance(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	r1 := l.Resolver()
	r2 := l.Resolver()

	if r1 != r2 {
		t.Error("Resolver() should return the same instance on subsequent calls")
	}
}

func TestNewInstance_StartFunctionError(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := New(rt, Options{})

	// Create a module
	modSrc := `(module
		(func $f (result i32) (i32.const 42))
		(export "main" (func $f))
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, modBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close(ctx)

	// Graph with one instance
	instances := []component.CoreInstance{
		{Parsed: &component.ParsedCoreInstance{
			Kind:        component.CoreInstanceInstantiate,
			ModuleIndex: 0,
		}},
	}
	graph := component.NewInstanceGraph(instances)

	// Component with Start pointing to non-existent export
	pre := &InstancePre{
		linker:   l,
		graph:    graph,
		compiled: []wazero.CompiledModule{compiled},
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				Start: &component.StartFunc{
					FuncIndex: 0,
				},
				FuncIndexSpace: []component.FuncIndexEntry{
					{InstanceIdx: 0, ExportName: "nonexistent"},
				},
			},
		},
	}

	// This exercises callStart error path through NewInstance
	_, err = pre.NewInstance(ctx)
	if err == nil {
		t.Error("expected error for start function pointing to nonexistent export")
	}
	if !strings.Contains(err.Error(), "not exported") {
		t.Errorf("expected 'not exported' error, got: %v", err)
	}
}

func TestInstance_Memory(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create a module with memory
	modSrc := `(module
		(memory 1)
		(export "memory" (memory 0))
		(func $f (result i32) (i32.const 42))
		(export "main" (func $f))
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	l := NewWithDefaults(rt)
	validated := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{modBytes},
			CoreInstances: []component.CoreInstance{
				{Parsed: &component.ParsedCoreInstance{
					Kind:        component.CoreInstanceInstantiate,
					ModuleIndex: 0,
				}},
			},
		},
	}

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	defer inst.Close(ctx)

	mem := inst.Memory()
	if mem == nil {
		t.Error("Memory() should return non-nil for module with memory")
	}
}

func TestInstance_Memory_NoMemory(t *testing.T) {
	inst := &Instance{
		modules: []api.Module{nil},
	}
	if mem := inst.Memory(); mem != nil {
		t.Error("Memory() should return nil when no modules have memory")
	}
}

func TestInstance_Allocator(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create a module with cabi_realloc
	modSrc := `(module
		(memory 1)
		(func $cabi_realloc (param i32 i32 i32 i32) (result i32) (i32.const 0))
		(export "cabi_realloc" (func $cabi_realloc))
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	l := NewWithDefaults(rt)
	validated := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{modBytes},
			CoreInstances: []component.CoreInstance{
				{Parsed: &component.ParsedCoreInstance{
					Kind:        component.CoreInstanceInstantiate,
					ModuleIndex: 0,
				}},
			},
		},
	}

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	defer inst.Close(ctx)

	alloc := inst.Allocator()
	if alloc == nil {
		t.Error("Allocator() should find cabi_realloc")
	}
}

func TestInstance_Allocator_NotFound(t *testing.T) {
	inst := &Instance{
		modules: []api.Module{nil},
	}
	if alloc := inst.Allocator(); alloc != nil {
		t.Error("Allocator() should return nil when no allocator exists")
	}
}

func TestInstance_Free(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create a module with cabi_free
	modSrc := `(module
		(memory 1)
		(func $cabi_free (param i32))
		(export "cabi_free" (func $cabi_free))
	)`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	l := NewWithDefaults(rt)
	validated := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{modBytes},
			CoreInstances: []component.CoreInstance{
				{Parsed: &component.ParsedCoreInstance{
					Kind:        component.CoreInstanceInstantiate,
					ModuleIndex: 0,
				}},
			},
		},
	}

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	defer inst.Close(ctx)

	freeFn := inst.Free()
	if freeFn == nil {
		t.Error("Free() should find cabi_free")
	}
}

func TestInstance_Free_NotFound(t *testing.T) {
	inst := &Instance{
		modules: []api.Module{nil},
	}
	if freeFn := inst.Free(); freeFn != nil {
		t.Error("Free() should return nil when no free exists")
	}
}

func TestInstance_GetModule(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	modSrc := `(module (func $f (result i32) (i32.const 42)) (export "f" (func $f)))`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	l := NewWithDefaults(rt)
	validated := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{modBytes},
			CoreInstances: []component.CoreInstance{
				{Parsed: &component.ParsedCoreInstance{
					Kind:        component.CoreInstanceInstantiate,
					ModuleIndex: 0,
				}},
			},
		},
	}

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	defer inst.Close(ctx)

	mod := inst.GetModule(0)
	if mod == nil {
		t.Error("GetModule(0) should return the instantiated module")
	}

	if mod2 := inst.GetModule(999); mod2 != nil {
		t.Error("GetModule(999) should return nil for out of range index")
	}
}

func TestInstance_Graph(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	modSrc := `(module (func $f (result i32) (i32.const 42)) (export "f" (func $f)))`
	modBytes, err := wat.Compile(modSrc)
	if err != nil {
		t.Fatalf("compile wat: %v", err)
	}

	l := NewWithDefaults(rt)
	validated := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{modBytes},
			CoreInstances: []component.CoreInstance{
				{Parsed: &component.ParsedCoreInstance{
					Kind:        component.CoreInstanceInstantiate,
					ModuleIndex: 0,
				}},
			},
		},
	}

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	defer inst.Close(ctx)

	g := inst.Graph()
	if g == nil {
		t.Error("Graph() should return non-nil for component with instances")
	}
}

func TestInstance_Graph_NilPre(t *testing.T) {
	inst := &Instance{pre: nil}
	if g := inst.Graph(); g != nil {
		t.Error("Graph() should return nil when pre is nil")
	}
}

func TestResolveCompFunc_LongChain(t *testing.T) {
	// Test re-export chain longer than 8 to exercise map fallback
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	// Chain of 10 re-exports: 10 -> 9 -> 8 -> 7 -> 6 -> 5 -> 4 -> 3 -> 2 -> 1 -> 0 (lift)
	sources := map[uint32]compFuncSource{
		0:  {kind: compFuncLift, coreFunc: 0},
		1:  {kind: compFuncExport, reExportOf: 0},
		2:  {kind: compFuncExport, reExportOf: 1},
		3:  {kind: compFuncExport, reExportOf: 2},
		4:  {kind: compFuncExport, reExportOf: 3},
		5:  {kind: compFuncExport, reExportOf: 4},
		6:  {kind: compFuncExport, reExportOf: 5},
		7:  {kind: compFuncExport, reExportOf: 6},
		8:  {kind: compFuncExport, reExportOf: 7},
		9:  {kind: compFuncExport, reExportOf: 8},
		10: {kind: compFuncExport, reExportOf: 9},
	}

	// Start from 10, will follow chain of 10 -> 9 -> ... -> 0
	fn := inst.resolveCompFunc(pre.component.Raw, sources, 10)
	if fn == nil {
		t.Fatal("resolveCompFunc returned nil for long chain (map fallback)")
	}
}

func TestResolveCompFunc_LongChainCycle(t *testing.T) {
	// Test cycle detection with chain longer than 8
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}
	inst := &Instance{pre: pre}

	// Chain of 10 with cycle at the end: 10 -> 9 -> ... -> 1 -> 0 -> 10
	sources := map[uint32]compFuncSource{
		0:  {kind: compFuncExport, reExportOf: 10}, // cycle back
		1:  {kind: compFuncExport, reExportOf: 0},
		2:  {kind: compFuncExport, reExportOf: 1},
		3:  {kind: compFuncExport, reExportOf: 2},
		4:  {kind: compFuncExport, reExportOf: 3},
		5:  {kind: compFuncExport, reExportOf: 4},
		6:  {kind: compFuncExport, reExportOf: 5},
		7:  {kind: compFuncExport, reExportOf: 6},
		8:  {kind: compFuncExport, reExportOf: 7},
		9:  {kind: compFuncExport, reExportOf: 8},
		10: {kind: compFuncExport, reExportOf: 9},
	}

	fn := inst.resolveCompFunc(pre.component.Raw, sources, 10)
	if fn != nil {
		t.Error("resolveCompFunc should return nil for cycle in long chain")
	}
}

func TestResolveCompFuncWithCanon_Lift(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	sources := map[uint32]compFuncSource{
		0: {kind: compFuncLift, coreFunc: 0},
	}

	fn, canon := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 0)
	if fn == nil {
		t.Fatal("resolveCompFuncWithCanon returned nil for lift")
	}
	// Canon may be nil without proper canonLifts/typeResolver setup
	_ = canon
}

func TestResolveCompFuncWithCanon_Alias(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				FuncIndexSpace: []component.FuncIndexEntry{
					{InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	sources := map[uint32]compFuncSource{
		0: {kind: compFuncAlias},
	}

	fn, canon := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 0)
	if fn == nil {
		t.Fatal("resolveCompFuncWithCanon returned nil for alias")
	}
	if canon != nil {
		t.Error("alias should not have canon info")
	}
}

func TestResolveCompFuncWithCanon_ReExportChain(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	// Chain: 2 -> 1 -> 0 (lift)
	sources := map[uint32]compFuncSource{
		0: {kind: compFuncLift, coreFunc: 0},
		1: {kind: compFuncExport, reExportOf: 0},
		2: {kind: compFuncExport, reExportOf: 1},
	}

	fn, _ := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 2)
	if fn == nil {
		t.Fatal("resolveCompFuncWithCanon returned nil for re-export chain")
	}
}

func TestResolveCompFuncWithCanon_CycleDetection(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}
	inst := &Instance{pre: pre}

	// Cycle: 0 -> 1 -> 0
	sources := map[uint32]compFuncSource{
		0: {kind: compFuncExport, reExportOf: 1},
		1: {kind: compFuncExport, reExportOf: 0},
	}

	fn, canon := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 0)
	if fn != nil || canon != nil {
		t.Error("resolveCompFuncWithCanon should return nil for cycle")
	}
}

func TestResolveCompFuncWithCanon_NotFound(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}
	inst := &Instance{pre: pre}

	sources := map[uint32]compFuncSource{}

	fn, canon := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 99)
	if fn != nil || canon != nil {
		t.Error("resolveCompFuncWithCanon should return nil for not found")
	}
}

func TestResolveCompFuncWithCanon_LongChain(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, err := wat.Compile(`(module (func (export "fn") (result i32) i32.const 42))`)
	if err != nil {
		t.Fatalf("wat compile: %v", err)
	}
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				CoreFuncIndexSpace: []component.CoreFuncEntry{
					{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{0: {module: mod}},
	}

	// Chain of 10: 10 -> 9 -> ... -> 0 (lift)
	sources := map[uint32]compFuncSource{
		0:  {kind: compFuncLift, coreFunc: 0},
		1:  {kind: compFuncExport, reExportOf: 0},
		2:  {kind: compFuncExport, reExportOf: 1},
		3:  {kind: compFuncExport, reExportOf: 2},
		4:  {kind: compFuncExport, reExportOf: 3},
		5:  {kind: compFuncExport, reExportOf: 4},
		6:  {kind: compFuncExport, reExportOf: 5},
		7:  {kind: compFuncExport, reExportOf: 6},
		8:  {kind: compFuncExport, reExportOf: 7},
		9:  {kind: compFuncExport, reExportOf: 8},
		10: {kind: compFuncExport, reExportOf: 9},
	}

	fn, _ := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 10)
	if fn == nil {
		t.Fatal("resolveCompFuncWithCanon returned nil for long chain (map fallback)")
	}
}

func TestResolveCompFuncWithCanon_LongChainCycle(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}
	inst := &Instance{pre: pre}

	// Chain of 10 with cycle: 10 -> 9 -> ... -> 0 -> 10
	sources := map[uint32]compFuncSource{
		0:  {kind: compFuncExport, reExportOf: 10},
		1:  {kind: compFuncExport, reExportOf: 0},
		2:  {kind: compFuncExport, reExportOf: 1},
		3:  {kind: compFuncExport, reExportOf: 2},
		4:  {kind: compFuncExport, reExportOf: 3},
		5:  {kind: compFuncExport, reExportOf: 4},
		6:  {kind: compFuncExport, reExportOf: 5},
		7:  {kind: compFuncExport, reExportOf: 6},
		8:  {kind: compFuncExport, reExportOf: 7},
		9:  {kind: compFuncExport, reExportOf: 8},
		10: {kind: compFuncExport, reExportOf: 9},
	}

	fn, canon := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 10)
	if fn != nil || canon != nil {
		t.Error("resolveCompFuncWithCanon should return nil for cycle in long chain")
	}
}

func TestResolveCompFuncWithCanon_AliasOutOfRange(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				FuncIndexSpace: []component.FuncIndexEntry{},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{},
	}

	sources := map[uint32]compFuncSource{
		5: {kind: compFuncAlias},
	}

	fn, canon := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 5)
	if fn != nil || canon != nil {
		t.Error("alias out of range should return nil")
	}
}

func TestResolveCompFuncWithCanon_AliasNilInstance(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{
				FuncIndexSpace: []component.FuncIndexEntry{
					{InstanceIdx: 5, ExportName: "fn"},
				},
			},
		},
	}
	inst := &Instance{
		pre:           pre,
		coreInstances: map[int]*coreInstance{},
	}

	sources := map[uint32]compFuncSource{
		0: {kind: compFuncAlias},
	}

	fn, canon := inst.resolveCompFuncWithCanon(pre.component.Raw, sources, 0)
	if fn != nil || canon != nil {
		t.Error("alias with nil instance should return nil")
	}
}

// TestCall_WithoutCanon tests Call without Canon info (callRawWithCoercion path)
func TestCall_WithoutCanon(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	ns := l.Namespace("test:minimal/host@0.1.0")
	ns.DefineFunc("add", func(ctx context.Context, mod api.Module, stack []uint64) {
		a := uint32(stack[0])
		b := uint32(stack[1])
		stack[0] = uint64(a + b)
	}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	// Clear Canon to test callRawWithCoercion path
	exp, ok := inst.GetExport("compute")
	if !ok {
		t.Fatal("compute export not found")
	}
	exp.Canon = nil
	inst.exports["compute"] = exp

	// Call with various Go types - should coerce to uint64
	results, err := inst.Call(ctx, "compute", uint32(5), uint32(6))
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected result")
	}
	// Result is any (uint64 wrapped)
	if results[0].(uint64) != 30 {
		t.Errorf("compute(5, 6) = %v, want 30", results[0])
	}

	// Test with int type
	results, err = inst.Call(ctx, "compute", 3, 4)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if results[0].(uint64) != 12 {
		t.Errorf("compute(3, 4) = %v, want 12", results[0])
	}

	// Test with bool type
	results, err = inst.Call(ctx, "compute", true, false)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	// true=1, false=0, 1*0=0
	if results[0].(uint64) != 0 {
		t.Errorf("compute(true, false) = %v, want 0", results[0])
	}

	// Test with unsupported type - should error
	_, err = inst.Call(ctx, "compute", "string", "value")
	if err == nil {
		t.Error("Expected error for unsupported type")
	}
}

// TestExportedFunction tests the ExportedFunction method
func TestExportedFunction(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	ns := l.Namespace("test:minimal/host@0.1.0")
	ns.DefineFunc("add", func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = uint64(uint32(stack[0]) + uint32(stack[1]))
	}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	// Test ExportedFunction
	fn := inst.ExportedFunction("compute")
	if fn == nil {
		t.Fatal("ExportedFunction returned nil for 'compute'")
	}

	// Call it directly
	results, err := fn.Call(ctx, 7, 8)
	if err != nil {
		t.Fatalf("Direct call error: %v", err)
	}
	if results[0] != 56 {
		t.Errorf("compute(7, 8) = %d, want 56", results[0])
	}

	// Test non-existent
	fn = inst.ExportedFunction("nonexistent")
	if fn != nil {
		t.Error("ExportedFunction should return nil for nonexistent")
	}
}

func TestSynthModule(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create host module
	hostMod, err := rt.NewHostModuleBuilder("$host").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			t.Logf("0 called")
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("0").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			t.Logf("1 called")
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("1").
		Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hostMod.Close(ctx)

	// Build synthetic module
	builder := newSynthModuleBuilder("$host")
	builder.addFunc("0", []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil)
	builder.addFunc("1", []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil)
	builder.setTableSize(2)

	wasm := builder.build()
	t.Logf("Generated WASM: %x", wasm)

	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	defer compiled.Close(ctx)

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("$synth"))
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer mod.Close(ctx)

	// Check exports
	defs := mod.ExportedFunctionDefinitions()
	t.Logf("Exported functions: %v", defs)

	fn0 := mod.ExportedFunction("0")
	if fn0 != nil {
		_, err = fn0.Call(ctx, 1, 2)
		if err != nil {
			t.Errorf("call 0 error: %v", err)
		}
	} else {
		t.Error("function 0 not exported")
	}
}

func TestStringsComponentStructure(t *testing.T) {
	validated := loadTestComponent(t, "../testbed/strings.wasm")
	if validated == nil {
		t.Skip("strings.wasm not found")
	}

	comp := validated.Raw

	// Verify component has expected structure
	if len(comp.CoreModules) == 0 {
		t.Error("expected at least one core module in strings.wasm")
	}

	if len(comp.CoreInstances) == 0 {
		t.Error("expected at least one core instance in strings.wasm")
	}

	// Verify core instances have valid parsed data
	validInstances := 0
	for _, ci := range comp.CoreInstances {
		if ci.Parsed != nil {
			validInstances++
		}
	}
	if validInstances == 0 {
		t.Error("expected at least one core instance with parsed data")
	}

	// Verify aliases exist for a typical component
	if len(comp.Aliases) == 0 {
		t.Error("expected at least one alias in strings.wasm")
	}

	// Verify aliases have valid parsed data
	validAliases := 0
	for _, alias := range comp.Aliases {
		if alias.Parsed != nil {
			validAliases++
		}
	}
	if validAliases == 0 {
		t.Error("expected at least one alias with parsed data")
	}
}

func TestVirtualInstance_API(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create a module with memory and global
	wasmBytes, _ := wat.Compile(`(module
		(memory (export "mem") 1)
		(global (export "g") i32 (i32.const 42))
		(table (export "t") 1 funcref)
		(func (export "fn") (result i32) i32.const 1)
	)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	v := NewVirtualInstance("test-virt")

	// Test Name
	if v.Name() != "test-virt" {
		t.Errorf("Name() = %q, want %q", v.Name(), "test-virt")
	}

	// DefineModuleExport for func
	v.DefineModuleExport("fn", EntityFunc, mod, "fn")

	// DefineMemory
	v.DefineMemory("memory", mod.Memory())

	// DefineGlobal
	v.DefineGlobal("global", mod.ExportedGlobal("g"))

	// DefineTableRef
	v.DefineTableRef("table", mod, "t")

	// GetFunc
	fn := v.GetFunc("fn")
	if fn == nil {
		t.Error("GetFunc returned nil for valid func")
	}

	// GetFunc for non-existent
	if v.GetFunc("nonexistent") != nil {
		t.Error("GetFunc should return nil for non-existent")
	}

	// GetMemory
	mem := v.GetMemory("memory")
	if mem == nil {
		t.Error("GetMemory returned nil for valid memory")
	}

	// GetMemory for non-existent
	if v.GetMemory("nonexistent") != nil {
		t.Error("GetMemory should return nil for non-existent")
	}

	// GetGlobal
	g := v.GetGlobal("global")
	if g == nil {
		t.Error("GetGlobal returned nil for valid global")
	}

	// GetGlobal for non-existent
	if v.GetGlobal("nonexistent") != nil {
		t.Error("GetGlobal should return nil for non-existent")
	}

	// HasTable
	if !v.HasTable("table") {
		t.Error("HasTable returned false for valid table")
	}
	if v.HasTable("nonexistent") {
		t.Error("HasTable should return false for non-existent")
	}

	// All
	all := v.All()
	if len(all) != 4 {
		t.Errorf("All() returned %d entities, want 4", len(all))
	}

	// Get for wrong kind
	if v.GetFunc("memory") != nil {
		t.Error("GetFunc should return nil for memory entity")
	}
	if v.GetMemory("fn") != nil {
		t.Error("GetMemory should return nil for func entity")
	}
	if v.GetGlobal("fn") != nil {
		t.Error("GetGlobal should return nil for func entity")
	}
}

func TestVirtualInstance_HostFunc(t *testing.T) {
	v := NewVirtualInstance("test")

	// DefineFunc with host func
	def := &FuncDef{
		Name:       "test-fn",
		ParamTypes: []api.ValueType{api.ValueTypeI32},
	}
	v.DefineFunc("hostfn", def)

	// GetFunc returns nil for host funcs (they need separate handling)
	if v.GetFunc("hostfn") != nil {
		t.Error("GetFunc should return nil for host func")
	}

	// But Get should return the entity
	e := v.Get("hostfn")
	if e == nil {
		t.Fatal("Get should return entity for host func")
	}
	if e.Kind != EntityFunc {
		t.Errorf("Entity kind = %d, want EntityFunc", e.Kind)
	}
}

func TestResolver_API(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Create module with exports
	wasmBytes, _ := wat.Compile(`(module
		(memory (export "mem") 1)
		(global (export "g") i32 (i32.const 42))
		(func (export "fn") (result i32) i32.const 1)
	)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	r := l.Resolver()

	// RegisterModule
	r.RegisterModule("mod1", mod)

	// ResolveFunc from module
	fn := r.ResolveFunc("mod1", "fn")
	if fn == nil {
		t.Error("ResolveFunc returned nil for valid module func")
	}

	// ResolveMemory from module
	mem := r.ResolveMemory("mod1", "mem")
	if mem == nil {
		t.Error("ResolveMemory returned nil for valid module memory")
	}

	// ResolveGlobal from module
	g := r.ResolveGlobal("mod1", "g")
	if g == nil {
		t.Error("ResolveGlobal returned nil for valid module global")
	}

	// Register virtual instance
	v := NewVirtualInstance("virt1")
	v.DefineModuleExport("fn", EntityFunc, mod, "fn")
	v.DefineMemory("mem", mod.Memory())
	v.DefineGlobal("g", mod.ExportedGlobal("g"))
	r.RegisterInstance("virt1", v)

	// ResolveFunc from virtual
	fn2 := r.ResolveFunc("virt1", "fn")
	if fn2 == nil {
		t.Error("ResolveFunc returned nil for valid virtual func")
	}

	// ResolveMemory from virtual
	mem2 := r.ResolveMemory("virt1", "mem")
	if mem2 == nil {
		t.Error("ResolveMemory returned nil for valid virtual memory")
	}

	// ResolveGlobal from virtual
	g2 := r.ResolveGlobal("virt1", "g")
	if g2 == nil {
		t.Error("ResolveGlobal returned nil for valid virtual global")
	}

	// Resolve non-existent
	if r.ResolveFunc("nonexistent", "fn") != nil {
		t.Error("ResolveFunc should return nil for non-existent instance")
	}
	if r.ResolveMemory("nonexistent", "mem") != nil {
		t.Error("ResolveMemory should return nil for non-existent instance")
	}
	if r.ResolveGlobal("nonexistent", "g") != nil {
		t.Error("ResolveGlobal should return nil for non-existent instance")
	}
}

func TestResolver_CreateVirtualFromModule(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	wasmBytes, _ := wat.Compile(`(module
		(memory (export "mem") 1)
		(func (export "fn") (result i32) i32.const 1)
	)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	r := l.Resolver()

	// CreateVirtualFromModule
	v := r.CreateVirtualFromModule("wrapped", mod, map[string]EntityKind{
		"fn":  EntityFunc,
		"mem": EntityMemory,
	})

	if v == nil {
		t.Fatal("CreateVirtualFromModule returned nil")
	}

	// Should be registered
	if r.GetInstance("wrapped") != v {
		t.Error("Instance not registered after CreateVirtualFromModule")
	}

	// Exports should work
	if v.GetFunc("fn") == nil {
		t.Error("GetFunc failed on wrapped module")
	}
	if v.GetMemory("mem") == nil {
		t.Error("GetMemory failed on wrapped module")
	}
}

func TestValTypeToWasm(t *testing.T) {
	tests := []struct {
		input api.ValueType
		want  byte
	}{
		{api.ValueTypeI32, 0x7f},
		{api.ValueTypeI64, 0x7e},
		{api.ValueTypeF32, 0x7d},
		{api.ValueTypeF64, 0x7c},
		{api.ValueType(99), 0x7f}, // unknown defaults to i32
	}

	for _, tt := range tests {
		got := valTypeToWasm(tt.input)
		if got != tt.want {
			t.Errorf("valTypeToWasm(%d) = 0x%x, want 0x%x", tt.input, got, tt.want)
		}
	}
}

func TestSynthModuleBuilder_F32F64(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Build synth module with f32/f64 params
	builder := newSynthModuleBuilder("$test")
	builder.addFunc("f32func", []api.ValueType{api.ValueTypeF32}, []api.ValueType{api.ValueTypeF32})
	builder.addFunc("f64func", []api.ValueType{api.ValueTypeF64}, []api.ValueType{api.ValueTypeF64})
	builder.setTableSize(4)

	wasmBytes := builder.build()
	if wasmBytes == nil {
		t.Fatal("build returned nil")
	}

	// Host module
	_, err := rt.NewHostModuleBuilder("$test").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// echo f32
		}), []api.ValueType{api.ValueTypeF32}, []api.ValueType{api.ValueTypeF32}).
		Export("f32func").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// echo f64
		}), []api.ValueType{api.ValueTypeF64}, []api.ValueType{api.ValueTypeF64}).
		Export("f64func").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("host instantiate: %v", err)
	}

	// Should compile and instantiate
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile synth: %v", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("synth"))
	if err != nil {
		t.Fatalf("instantiate synth: %v", err)
	}
	defer mod.Close(ctx)

	// Check exports exist (exported with their original names)
	if mod.ExportedFunction("f32func") == nil {
		t.Error("f32func wrapper not exported")
	}
	if mod.ExportedFunction("f64func") == nil {
		t.Error("f64func wrapper not exported")
	}
}

func TestInstantiationError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := &InstantiationError{
		Phase:         "test",
		InstanceIndex: 0,
		ImportPath:    "test:pkg/iface@1.0.0#func",
		Reason:        "resolution failed",
		Cause:         inner,
	}

	// Unwrap
	unwrapped := err.Unwrap()
	if !errors.Is(unwrapped, inner) {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, inner)
	}

	// errors.Is should work
	if !errors.Is(err, inner) {
		t.Error("errors.Is failed to find inner error")
	}
}

func TestRewriteImportSection_TableImport(t *testing.T) {
	// WASM with table import (kind 0x01)
	wasmBytes, err := wat.Compile(`(module (import "" "t" (table 1 funcref)))`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	rewritten := rewriteEmptyModuleNames(wasmBytes)
	if len(rewritten) == 0 {
		t.Fatal("rewrite returned empty")
	}

	// Verify rewrite happened (size increased due to "$" replacing "")
	if len(rewritten) <= len(wasmBytes) {
		t.Error("rewritten should be larger")
	}
}

func TestRewriteImportSection_MemoryImport(t *testing.T) {
	// WASM with memory import (kind 0x02)
	wasmBytes, err := wat.Compile(`(module (import "" "m" (memory 1)))`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	rewritten := rewriteEmptyModuleNames(wasmBytes)
	if len(rewritten) == 0 {
		t.Fatal("rewrite returned empty")
	}

	// Should be larger due to "$" replacing ""
	if len(rewritten) <= len(wasmBytes) {
		t.Error("rewritten should be larger (added $ for empty name)")
	}
}

func TestRewriteImportSection_MemoryImportWithMax(t *testing.T) {
	// WASM with memory import with max (hasMax bit set)
	wasmBytes, err := wat.Compile(`(module (import "" "m" (memory 1 10)))`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	rewritten := rewriteEmptyModuleNames(wasmBytes)
	if len(rewritten) == 0 {
		t.Fatal("rewrite returned empty")
	}
}

func TestRewriteImportSection_GlobalImport(t *testing.T) {
	// WASM with global import (kind 0x03)
	wasmBytes, err := wat.Compile(`(module (import "" "g" (global i32)))`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	rewritten := rewriteEmptyModuleNames(wasmBytes)
	if len(rewritten) == 0 {
		t.Fatal("rewrite returned empty")
	}

	// Verify rewrite happened (size increased)
	if len(rewritten) <= len(wasmBytes) {
		t.Error("rewritten should be larger")
	}

	// Verify rewritten module compiles
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, rewritten)
	if err != nil {
		t.Fatalf("compile rewritten: %v", err)
	}
	_ = compiled
}

func TestRewriteImportSection_TableImportWithMax(t *testing.T) {
	// WASM with table import with max
	wasmBytes, err := wat.Compile(`(module (import "" "t" (table 1 10 funcref)))`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	rewritten := rewriteEmptyModuleNames(wasmBytes)
	if len(rewritten) == 0 {
		t.Fatal("rewrite returned empty")
	}
}

func TestCallRaw_NilCoreFunc(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{
		linker: l,
		component: &component.ValidatedComponent{
			Raw: &component.Component{},
		},
	}

	inst := &Instance{
		pre: pre,
		exports: map[string]Export{
			"test": {CoreFunc: nil}, // nil function
		},
	}

	// CallRaw should error for nil function
	_, err := inst.CallRaw(ctx, "test")
	if err == nil {
		t.Error("CallRaw should fail for nil CoreFunc")
	}
	if !strings.Contains(err.Error(), "no core function") {
		t.Errorf("error should mention 'no core function': %v", err)
	}

	// CallRaw should error for non-existent export
	_, err = inst.CallRaw(ctx, "nonexistent")
	if err == nil {
		t.Error("CallRaw should fail for non-existent export")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found': %v", err)
	}
}

func TestCallRawWithCoercion_UnsupportedType(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module (func (export "fn") (param i32)))`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("fn")
	inst := &Instance{}

	// Call with unsupported type (struct)
	type unsupported struct{ x int }
	_, err := inst.callRawWithCoercion(ctx, fn, []any{unsupported{42}})
	if err == nil {
		t.Error("callRawWithCoercion should fail for unsupported type")
	}
	if !strings.Contains(err.Error(), "cannot coerce") {
		t.Errorf("error should mention 'cannot coerce': %v", err)
	}
}

// Canonical ABI tests

func TestFlatCountForType(t *testing.T) {
	tests := []struct {
		typ      wit.Type
		name     string
		expected int
	}{
		{wit.Bool{}, "bool", 1},
		{wit.U8{}, "u8", 1},
		{wit.U32{}, "u32", 1},
		{wit.U64{}, "u64", 1},
		{wit.F32{}, "f32", 1},
		{wit.F64{}, "f64", 1},
		{wit.String{}, "string", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := invoke.FlatCount(tt.typ)
			if got != tt.expected {
				t.Errorf("invoke.FlatCount(%s) = %d, want %d", tt.name, got, tt.expected)
			}
		})
	}
}

func TestFlatCountForTypeDef(t *testing.T) {
	recordType := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "x", Type: wit.S32{}},
				{Name: "y", Type: wit.S32{}},
			},
		},
	}
	if got := invoke.FlatCount(recordType); got != 2 {
		t.Errorf("invoke.FlatCount(record{x,y}) = %d, want 2", got)
	}

	listType := &wit.TypeDef{
		Kind: &wit.List{Type: wit.U8{}},
	}
	if got := invoke.FlatCount(listType); got != 2 {
		t.Errorf("invoke.FlatCount(list<u8>) = %d, want 2", got)
	}

	tupleType := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.S32{}, wit.S32{}}},
	}
	if got := invoke.FlatCount(tupleType); got != 2 {
		t.Errorf("invoke.FlatCount(tuple<s32,s32>) = %d, want 2", got)
	}

	optionType := &wit.TypeDef{
		Kind: &wit.Option{Type: wit.U32{}},
	}
	if got := invoke.FlatCount(optionType); got != 2 {
		t.Errorf("invoke.FlatCount(option<u32>) = %d, want 2 (1+1)", got)
	}

	resultType := &wit.TypeDef{
		Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}},
	}
	if got := invoke.FlatCount(resultType); got != 3 {
		t.Errorf("invoke.FlatCount(result<u32,string>) = %d, want 3 (1+max(1,2))", got)
	}

	enumType := &wit.TypeDef{
		Kind: &wit.Enum{Cases: []wit.EnumCase{{Name: "a"}, {Name: "b"}}},
	}
	if got := invoke.FlatCount(enumType); got != 1 {
		t.Errorf("invoke.FlatCount(enum) = %d, want 1", got)
	}

	flagsType := &wit.TypeDef{
		Kind: &wit.Flags{Flags: []wit.Flag{{Name: "a"}, {Name: "b"}}},
	}
	if got := invoke.FlatCount(flagsType); got != 1 {
		t.Errorf("invoke.FlatCount(flags) = %d, want 1", got)
	}

	variantType := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "a", Type: wit.U32{}},
				{Name: "b", Type: wit.String{}},
			},
		},
	}
	if got := invoke.FlatCount(variantType); got != 3 {
		t.Errorf("invoke.FlatCount(variant) = %d, want 3 (1+max(1,2))", got)
	}
}

func TestCanonicalABI_Minimal(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/minimal.wasm")
	if validated == nil {
		t.Skip("minimal.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	ns := l.Namespace("test:minimal/host@0.1.0")
	ns.DefineFunc("add", func(ctx context.Context, mod api.Module, stack []uint64) {
		a := uint32(stack[0])
		b := uint32(stack[1])
		stack[0] = uint64(a + b)
	}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	results, err := inst.Call(ctx, "compute", uint32(5), uint32(6))
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected result")
	}
	got, ok := results[0].(uint32)
	if !ok {
		t.Fatalf("Expected uint32 result, got %T", results[0])
	}
	if got != 30 {
		t.Errorf("compute(5, 6) = %d, want 30", got)
	}

	results, err = inst.Call(ctx, "compute-using-host", uint32(10), uint32(5))
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected result")
	}
	got, ok = results[0].(uint32)
	if !ok {
		t.Fatalf("Expected uint32 result, got %T", results[0])
	}
	if got != 15 {
		t.Errorf("compute-using-host(10, 5) = %d, want 15", got)
	}
}

func TestCanonicalABI_Strings(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/strings.wasm")
	if validated == nil {
		t.Skip("strings.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	ns := l.Namespace("test:strings/host@0.1.0")

	ns.DefineFunc("log", func(ctx context.Context, mod api.Module, stack []uint64) {
	}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil)

	ns.DefineFunc("concat", func(ctx context.Context, mod api.Module, stack []uint64) {
		ptr1, len1 := uint32(stack[0]), uint32(stack[1])
		ptr2, len2 := uint32(stack[2]), uint32(stack[3])
		retptr := uint32(stack[4])

		mem := mod.Memory()
		s1, _ := mem.Read(ptr1, len1)
		s2, _ := mem.Read(ptr2, len2)

		result := string(s1) + string(s2)

		realloc := mod.ExportedFunction("cabi_realloc")
		if realloc == nil {
			return
		}

		results, err := realloc.Call(ctx, 0, 0, 1, uint64(len(result)))
		if err != nil || len(results) == 0 {
			return
		}

		resultPtr := uint32(results[0])
		mem.Write(resultPtr, []byte(result))

		mem.WriteUint32Le(retptr, resultPtr)
		mem.WriteUint32Le(retptr+4, uint32(len(result)))
	}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil)

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	exp, ok := inst.GetExport("echo")
	if !ok {
		t.Fatal("echo export not found")
	}

	t.Logf("echo export Canon: %+v", exp.Canon)
	if exp.Canon != nil {
		t.Logf("  ParamTypes: %v", exp.Canon.ParamTypes)
		t.Logf("  ResultTypes: %v", exp.Canon.ResultTypes)
	}

	if exp.Canon != nil && len(exp.Canon.ParamTypes) > 0 {
		results, err := inst.Call(ctx, "echo", "hello")
		if err != nil {
			t.Fatalf("Call error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("Expected result")
		}
		got, ok := results[0].(string)
		if !ok {
			t.Fatalf("Expected string result, got %T: %v", results[0], results[0])
		}
		if got != "hello" {
			t.Errorf("echo('hello') = %q, want 'hello'", got)
		}
		t.Logf("echo('hello') = %q - canonical encoding works!", got)
	} else {
		t.Log("No Canon info - testing raw call")
	}
}

func TestCanonicalABI_Complex(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/complex.wasm")
	if validated == nil {
		t.Skip("complex.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	exp, ok := inst.GetExport("echo-point")
	if !ok {
		t.Fatal("echo-point export not found")
	}

	t.Logf("echo-point Canon: %+v", exp.Canon)
	if exp.Canon != nil {
		t.Logf("  ParamTypes: %v", exp.Canon.ParamTypes)
		t.Logf("  ResultTypes: %v", exp.Canon.ResultTypes)

		point := map[string]any{"x": int32(10), "y": int32(20)}
		results, err := inst.Call(ctx, "echo-point", point)
		if err != nil {
			t.Fatalf("Call error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("Expected result")
		}
		t.Logf("echo-point({x:10, y:20}) = %v", results[0])
	}

	exp2, ok := inst.GetExport("sum-list")
	if ok && exp2.Canon != nil {
		numbers := []int32{1, 2, 3, 4, 5}
		results, err := inst.Call(ctx, "sum-list", numbers)
		if err != nil {
			t.Fatalf("Call error: %v", err)
		}
		if len(results) > 0 {
			t.Logf("sum-list([1,2,3,4,5]) = %v", results[0])
		}
	}

	exp3, ok := inst.GetExport("swap-pair")
	if ok && exp3.Canon != nil {
		results, err := inst.Call(ctx, "swap-pair", int32(1), int32(2))
		if err != nil {
			t.Fatalf("Call error: %v", err)
		}
		if len(results) > 0 {
			t.Logf("swap-pair(1, 2) = %v", results[0])
		}
	}
}

func TestCanonicalABI_Counter(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/counter.wasm")
	if validated == nil {
		t.Skip("counter.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	t.Log("counter.wasm requires resource host functions - checking instantiation")

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Logf("counter.wasm instantiation requires resource host functions: %v", err)
		t.Skip("Resource host functions not implemented yet")
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Skipf("counter.wasm NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	exp, ok := inst.GetExport("run-test")
	if ok {
		t.Logf("run-test Canon: %+v", exp.Canon)
	}
}

func TestCanonicalABI_PostReturn(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/complex.wasm")
	if validated == nil {
		t.Skip("complex.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	exp, ok := inst.GetExport("echo-person")
	if !ok {
		t.Fatal("echo-person export not found")
	}

	t.Logf("echo-person Canon: %+v", exp.Canon)
	if exp.Canon == nil {
		t.Skip("No Canon info for echo-person")
	}

	if exp.Canon.PostReturn != nil {
		t.Log("PostReturn function is resolved - good!")
	} else {
		t.Log("PostReturn is nil - may not be in component or not resolved")
	}

	person := map[string]any{"name": "Alice", "age": uint32(30)}
	results, err := inst.Call(ctx, "echo-person", person)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected result")
	}

	t.Logf("echo-person({name:'Alice', age:30}) = %v", results[0])

	resultMap, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected map result, got %T", results[0])
	}
	if resultMap["name"] != "Alice" {
		t.Errorf("name = %v, want 'Alice'", resultMap["name"])
	}
	if resultMap["age"] != uint32(30) {
		t.Errorf("age = %v, want 30", resultMap["age"])
	}
}

func TestCanonicalABI_InstanceExportMethods(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	validated := loadTestComponent(t, "../testbed/sleep-test.wasm")
	if validated == nil {
		t.Skip("sleep-test.wasm not found")
	}

	l := New(rt, Options{
		SemverMatching: true,
	})

	clockNS := l.Namespace("wasi:clocks/monotonic-clock@0.2.0")
	clockNS.DefineFunc("now",
		func(context.Context, api.Module, []uint64) {},
		nil,
		[]api.ValueType{api.ValueTypeI64},
	)
	clockNS.DefineFunc("subscribe-duration",
		func(context.Context, api.Module, []uint64) {},
		[]api.ValueType{api.ValueTypeI64},
		[]api.ValueType{api.ValueTypeI32},
	)

	pollNS := l.Namespace("wasi:io/poll@0.2.0")
	pollNS.DefineFunc("[method]pollable.block",
		func(context.Context, api.Module, []uint64) {},
		[]api.ValueType{api.ValueTypeI32},
		nil,
	)

	pre, err := l.Instantiate(ctx, validated)
	if err != nil {
		t.Fatalf("Instantiate error: %v", err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		t.Fatalf("NewInstance error: %v", err)
	}
	defer inst.Close(ctx)

	if exp, ok := inst.GetExport("test-sleep"); !ok {
		t.Fatal("test-sleep instance export not found")
	} else if exp.CoreFunc != nil {
		t.Fatal("test-sleep should be an instance export, not a direct function export")
	}

	methods := []string{
		"test-sleep#sleep-ms",
		"test-sleep#work-with-sleep",
	}
	for _, name := range methods {
		exp, ok := inst.GetExport(name)
		if !ok {
			t.Fatalf("%s export not found", name)
		}
		if exp.CoreFunc == nil {
			t.Fatalf("%s should resolve to a callable core function", name)
		}
		if exp.Canon == nil {
			t.Fatalf("%s should have canon ABI metadata", name)
		}
	}

	hidden := []string{
		"sleep-ms",
		"work-with-sleep",
		"cabi_realloc",
	}
	for _, name := range hidden {
		if _, ok := inst.GetExport(name); ok {
			t.Fatalf("%s should not be exported", name)
		}
	}
}

// Context-based instance lookup tests

func TestWithInstance_And_InstanceFromContext(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	if got := InstanceFromContext(ctx); got != nil {
		t.Error("InstanceFromContext should return nil for empty context")
	}

	ctxWithInst := WithInstance(ctx, inst)

	got := InstanceFromContext(ctxWithInst)
	if got != inst {
		t.Error("InstanceFromContext should return the instance we added")
	}

	if InstanceFromContext(ctx) != nil {
		t.Error("original context should still be empty")
	}
}

func TestInstanceFromContext_NilInstance(t *testing.T) {
	ctx := context.Background()

	ctxWithNil := WithInstance(ctx, nil)

	got := InstanceFromContext(ctxWithNil)
	if got != nil {
		t.Error("InstanceFromContext should return nil for nil instance")
	}
}

func TestInstanceFromContext_WrongType(t *testing.T) {
	ctx := context.Background()

	got := InstanceFromContext(ctx)
	if got != nil {
		t.Error("expected nil")
	}
}

func TestExtractInstanceID(t *testing.T) {
	tests := []struct {
		name   string
		wantID uint64
		wantOK bool
	}{
		{"$0#123", 123, true},
		{"module#456", 456, true},
		{"test:namespace/mod@1.0.0#789", 789, true},
		{"$#0", 0, true},
		{"no-hash", 0, false},
		{"trailing#", 0, false},
		{"#", 0, false},
		{"", 0, false},
		{"bad#abc", 0, false},
		{"mod#-1", 0, false},
	}

	for _, tc := range tests {
		id, ok := extractInstanceID(tc.name)
		if ok != tc.wantOK {
			t.Errorf("extractInstanceID(%q): ok=%v, want %v", tc.name, ok, tc.wantOK)
		}
		if ok && id != tc.wantID {
			t.Errorf("extractInstanceID(%q): id=%d, want %d", tc.name, id, tc.wantID)
		}
	}
}

func TestLookupInstanceFromCaller_WithRegistry(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	moduleName := fmt.Sprintf("test#%d", inst.instanceID)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(moduleName))
	defer mod.Close(ctx)

	got := lookupInstanceFromCaller(mod)
	if got != inst {
		t.Error("lookupInstanceFromCaller should find the registered instance")
	}
}

func TestLookupInstanceFromCaller_NotRegistered(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test#99999"))
	defer mod.Close(ctx)

	got := lookupInstanceFromCaller(mod)
	if got != nil {
		t.Error("lookupInstanceFromCaller should return nil for unregistered ID")
	}
}

func TestLookupInstanceFromCaller_InvalidName(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("$"))
	defer mod.Close(ctx)

	got := lookupInstanceFromCaller(mod)
	if got != nil {
		t.Error("lookupInstanceFromCaller should return nil for invalid module name")
	}
}

func TestCreateSharedMemoryHandler_UsesContextFallback(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	var handlerCalled bool
	var receivedModule api.Module

	def := &FuncDef{
		Name:        "test",
		ParamTypes:  nil,
		ResultTypes: nil,
		Handler: func(ctx context.Context, mod api.Module, stack []uint64) {
			handlerCalled = true
			receivedModule = mod
		},
	}

	handler := createSharedMemoryHandler(def)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	shimMod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("$"))
	defer shimMod.Close(ctx)

	ctxWithInst := WithInstance(ctx, inst)
	handler(ctxWithInst, shimMod, nil)

	if !handlerCalled {
		t.Error("handler should have been called")
	}
	if receivedModule == nil {
		t.Error("handler should have received a module")
	}
}

func TestCreateSharedMemoryHandler_NoInstanceFallback(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	var handlerCalled bool

	def := &FuncDef{
		Name:        "test",
		ParamTypes:  nil,
		ResultTypes: nil,
		Handler: func(ctx context.Context, mod api.Module, stack []uint64) {
			handlerCalled = true
		},
	}

	handler := createSharedMemoryHandler(def)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("$"))
	defer mod.Close(ctx)

	handler(ctx, mod, nil)

	if !handlerCalled {
		t.Error("handler should be called even without instance")
	}
}

func BenchmarkWithInstance(b *testing.B) {
	ctx := context.Background()
	inst := &Instance{instanceID: 42}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WithInstance(ctx, inst)
	}
}

func BenchmarkInstanceFromContext_Hit(b *testing.B) {
	inst := &Instance{instanceID: 42}
	ctx := WithInstance(context.Background(), inst)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = InstanceFromContext(ctx)
	}
}

func BenchmarkInstanceFromContext_Miss(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = InstanceFromContext(ctx)
	}
}

func BenchmarkExtractInstanceID_Valid(b *testing.B) {
	name := "module#12345"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = extractInstanceID(name)
	}
}

func BenchmarkExtractInstanceID_Invalid(b *testing.B) {
	name := "$"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = extractInstanceID(name)
	}
}

func BenchmarkLookupInstanceFromCaller_Hit(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	moduleName := fmt.Sprintf("test#%d", inst.instanceID)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(moduleName))
	defer mod.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lookupInstanceFromCaller(mod)
	}
}

func BenchmarkLookupInstanceFromCaller_Miss(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("$"))
	defer mod.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lookupInstanceFromCaller(mod)
	}
}

func BenchmarkSharedMemoryHandler_WithCallerLookup(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	def := &FuncDef{
		Handler: func(ctx context.Context, mod api.Module, stack []uint64) {},
	}
	handler := createSharedMemoryHandler(def)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	moduleName := fmt.Sprintf("test#%d", inst.instanceID)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(moduleName))
	defer mod.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler(ctx, mod, nil)
	}
}

func BenchmarkSharedMemoryHandler_WithContextFallback(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	def := &FuncDef{
		Handler: func(ctx context.Context, mod api.Module, stack []uint64) {},
	}
	handler := createSharedMemoryHandler(def)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("$"))
	defer mod.Close(ctx)

	ctxWithInst := WithInstance(ctx, inst)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler(ctxWithInst, mod, nil)
	}
}

func BenchmarkSharedMemoryHandler_NoInstance(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	def := &FuncDef{
		Handler: func(ctx context.Context, mod api.Module, stack []uint64) {},
	}
	handler := createSharedMemoryHandler(def)

	wasmBytes, _ := wat.Compile(`(module)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("$"))
	defer mod.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler(ctx, mod, nil)
	}
}

func TestInstanceRegistry_CleanupAfterClose(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)
	instanceID := inst.instanceID

	if _, ok := instanceRegistry.Load(instanceID); !ok {
		t.Fatal("instance should be registered after creation")
	}

	inst.Close(ctx)

	if _, ok := instanceRegistry.Load(instanceID); ok {
		t.Error("instance should be unregistered after Close")
	}
}

func TestMultipleInstancesWithContext(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}

	inst1, _ := pre.NewInstance(ctx)
	inst2, _ := pre.NewInstance(ctx)

	ctx1 := WithInstance(ctx, inst1)
	ctx2 := WithInstance(ctx, inst2)

	if InstanceFromContext(ctx1) != inst1 {
		t.Error("ctx1 should return inst1")
	}
	if InstanceFromContext(ctx2) != inst2 {
		t.Error("ctx2 should return inst2")
	}

	ctxNested := WithInstance(ctx1, inst2)
	if InstanceFromContext(ctxNested) != inst2 {
		t.Error("nested context should return inst2")
	}

	if InstanceFromContext(ctx1) != inst1 {
		t.Error("original ctx1 should still return inst1")
	}
}

func TestConcurrentRegistryAccess(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}

	const numInstances = 100
	instances := make([]*Instance, numInstances)

	var wg sync.WaitGroup
	for i := 0; i < numInstances; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			inst, err := pre.NewInstance(ctx)
			if err != nil {
				t.Errorf("NewInstance failed: %v", err)
				return
			}
			instances[idx] = inst
		}(i)
	}
	wg.Wait()

	for i, inst := range instances {
		if inst == nil {
			continue
		}
		if _, ok := instanceRegistry.Load(inst.instanceID); !ok {
			t.Errorf("instance %d not registered", i)
		}
	}

	for i := 0; i < numInstances; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if instances[idx] != nil {
				instances[idx].Close(ctx)
			}
		}(i)
	}
	wg.Wait()

	for i, inst := range instances {
		if inst == nil {
			continue
		}
		if _, ok := instanceRegistry.Load(inst.instanceID); ok {
			t.Errorf("instance %d still registered after Close", i)
		}
	}
}

// Memory wrapper tests

func TestMemoryWrapper_ReadWriteU8(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module (memory (export "memory") 1))`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	mem := mod.Memory()
	wrapper := &memoryWrapper{Mem: mem}

	if err := wrapper.WriteU8(0, 0x42); err != nil {
		t.Fatalf("WriteU8 failed: %v", err)
	}
	v, err := wrapper.ReadU8(0)
	if err != nil {
		t.Fatalf("ReadU8 failed: %v", err)
	}
	if v != 0x42 {
		t.Errorf("ReadU8 = %x, want 0x42", v)
	}

	_, err = wrapper.ReadU8(65536 * 100)
	if err == nil {
		t.Error("ReadU8 should fail for out of bounds")
	}
	if err := wrapper.WriteU8(65536*100, 0); err == nil {
		t.Error("WriteU8 should fail for out of bounds")
	}
}

func TestMemoryWrapper_ReadWriteU16(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module (memory (export "memory") 1))`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	mem := mod.Memory()
	wrapper := &memoryWrapper{Mem: mem}

	if err := wrapper.WriteU16(0, 0x1234); err != nil {
		t.Fatalf("WriteU16 failed: %v", err)
	}
	v, err := wrapper.ReadU16(0)
	if err != nil {
		t.Fatalf("ReadU16 failed: %v", err)
	}
	if v != 0x1234 {
		t.Errorf("ReadU16 = %x, want 0x1234", v)
	}

	_, err = wrapper.ReadU16(65536 * 100)
	if err == nil {
		t.Error("ReadU16 should fail for out of bounds")
	}
	if err := wrapper.WriteU16(65536*100, 0); err == nil {
		t.Error("WriteU16 should fail for out of bounds")
	}
}

func TestMemoryWrapper_ReadWriteU64(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module (memory (export "memory") 1))`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	mem := mod.Memory()
	wrapper := &memoryWrapper{Mem: mem}

	if err := wrapper.WriteU64(0, 0x123456789ABCDEF0); err != nil {
		t.Fatalf("WriteU64 failed: %v", err)
	}
	v, err := wrapper.ReadU64(0)
	if err != nil {
		t.Fatalf("ReadU64 failed: %v", err)
	}
	if v != 0x123456789ABCDEF0 {
		t.Errorf("ReadU64 = %x, want 0x123456789ABCDEF0", v)
	}

	_, err = wrapper.ReadU64(65536 * 100)
	if err == nil {
		t.Error("ReadU64 should fail for out of bounds")
	}
	if err := wrapper.WriteU64(65536*100, 0); err == nil {
		t.Error("WriteU64 should fail for out of bounds")
	}
}

func TestAllocatorWrapper_Free(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	var freeCalled bool
	var lastPtr, lastOldSize, lastNewSize uint64

	_, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			lastPtr = stack[0]
			lastOldSize = stack[1]
			lastNewSize = stack[3]
			if lastPtr != 0 && lastOldSize != 0 && lastNewSize == 0 {
				freeCalled = true
			}
			stack[0] = 1024
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("realloc").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host: %v", err)
	}

	wasmBytes, _ := wat.Compile(`(module
		(import "env" "realloc" (func $realloc (param i32 i32 i32 i32) (result i32)))
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			local.get 0
			local.get 1
			local.get 2
			local.get 3
			call $realloc
		)
	)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("cabi_realloc")
	if fn == nil {
		t.Fatal("cabi_realloc not found")
	}

	wrapper := &allocatorWrapper{Ctx: ctx, Fn: fn}

	ptr, err := wrapper.Alloc(100, 8)
	if err != nil {
		t.Fatalf("Alloc failed: %v", err)
	}
	if ptr != 1024 {
		t.Errorf("Alloc returned %d, want 1024", ptr)
	}

	wrapper.Free(ptr, 100, 8)

	if !freeCalled {
		t.Error("Free did not call cabi_realloc with newSize=0")
	}
	if lastNewSize != 0 {
		t.Errorf("Free called with newSize=%d, want 0", lastNewSize)
	}
	_ = lastPtr
}

func TestMemoryWrapper_ErrorPaths(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module (memory (export "memory") 1))`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	defer mod.Close(ctx)

	mem := mod.Memory()
	wrapper := &memoryWrapper{Mem: mem}

	_, err := wrapper.ReadU32(65536 * 100)
	if err == nil {
		t.Error("ReadU32 should fail for out of bounds")
	}

	err = wrapper.WriteU32(65536*100, 0)
	if err == nil {
		t.Error("WriteU32 should fail for out of bounds")
	}

	_, err = wrapper.Read(65536*100, 100)
	if err == nil {
		t.Error("Read should fail for out of bounds")
	}

	err = wrapper.Write(65536*100, []byte("test"))
	if err == nil {
		t.Error("Write should fail for out of bounds")
	}
}

func TestAllocatorWrapper_ErrorPaths(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasmBytes, _ := wat.Compile(`(module
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32)
			i32.const 0
		)
	)`)
	compiled, _ := rt.CompileModule(ctx, wasmBytes)
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("cabi_realloc")
	wrapper := &allocatorWrapper{Ctx: ctx, Fn: fn}

	ptr, err := wrapper.Alloc(100, 8)
	if err != nil {
		t.Logf("Alloc returned error: %v", err)
	} else if ptr != 0 {
		t.Errorf("Alloc returned %d, want 0", ptr)
	}
}

func TestWrapMemory_Nil(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	wrapped := inst.wrapMemory(nil)
	if wrapped != nil {
		t.Error("wrapMemory(nil) should return nil")
	}
}

func TestWrapAllocator_Nil(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)
	pre := &InstancePre{linker: l}
	inst, _ := pre.NewInstance(ctx)

	wrapper := inst.wrapAllocator(ctx, nil)
	if wrapper != nil {
		t.Error("wrapAllocator with nil func should return nil")
	}
}
