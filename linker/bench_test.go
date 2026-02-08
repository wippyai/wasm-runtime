package linker

import (
	"context"
	"runtime"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/component"
)

// Minimal valid WASM module
var minimalWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
}

// WASM module with exported function
var funcWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f, // type section: () -> i32
	0x03, 0x02, 0x01, 0x00, // func section: 1 func of type 0
	0x07, 0x07, 0x01, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00, // export "add"
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x2a, 0x0b, // code: return 42
}

func BenchmarkNewInstance_Minimal(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	comp := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{minimalWasm},
			CoreInstances: []component.CoreInstance{
				{
					Parsed: &component.ParsedCoreInstance{
						Kind:        component.CoreInstanceInstantiate,
						ModuleIndex: 0,
					},
				},
			},
		},
	}

	pre, err := l.Instantiate(ctx, comp)
	if err != nil {
		b.Fatal(err)
	}
	defer pre.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := pre.NewInstance(ctx)
		if err != nil {
			b.Fatal(err)
		}
		inst.Close(ctx)
	}
}

func BenchmarkNewInstance_WithExports(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	comp := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{funcWasm},
			CoreInstances: []component.CoreInstance{
				{
					Parsed: &component.ParsedCoreInstance{
						Kind:        component.CoreInstanceInstantiate,
						ModuleIndex: 0,
					},
				},
			},
			Exports: []component.Export{
				{Name: "add", Sort: 0x01, SortIndex: 0},
			},
			SectionOrder: []component.SectionMarker{
				{Kind: component.SectionExport, StartIndex: 0, Count: 1},
			},
		},
	}

	pre, err := l.Instantiate(ctx, comp)
	if err != nil {
		b.Fatal(err)
	}
	defer pre.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := pre.NewInstance(ctx)
		if err != nil {
			b.Fatal(err)
		}
		inst.Close(ctx)
	}
}

func BenchmarkNewInstance_MultipleInstances(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	// Component with 3 core instances
	comp := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{minimalWasm, minimalWasm, minimalWasm},
			CoreInstances: []component.CoreInstance{
				{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: 0}},
				{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: 1}},
				{Parsed: &component.ParsedCoreInstance{Kind: component.CoreInstanceInstantiate, ModuleIndex: 2}},
			},
		},
	}

	pre, err := l.Instantiate(ctx, comp)
	if err != nil {
		b.Fatal(err)
	}
	defer pre.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := pre.NewInstance(ctx)
		if err != nil {
			b.Fatal(err)
		}
		inst.Close(ctx)
	}
}

func BenchmarkCall(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	comp := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{funcWasm},
			CoreInstances: []component.CoreInstance{
				{
					Parsed: &component.ParsedCoreInstance{
						Kind:        component.CoreInstanceInstantiate,
						ModuleIndex: 0,
					},
				},
			},
			CoreFuncIndexSpace: []component.CoreFuncEntry{
				{Kind: component.CoreFuncAliasExport, InstanceIdx: 0, ExportName: "add"},
			},
			FuncIndexSpace: []component.FuncIndexEntry{
				{InstanceIdx: 0, ExportName: "add"},
			},
			Canons: []component.Canon{
				{Parsed: &component.CanonDef{Kind: component.CanonLift, FuncIndex: 0}},
			},
			Exports: []component.Export{
				{Name: "add", Sort: 0x01, SortIndex: 0},
			},
			SectionOrder: []component.SectionMarker{
				{Kind: component.SectionCanon, StartIndex: 0, Count: 1},
				{Kind: component.SectionExport, StartIndex: 0, Count: 1},
			},
		},
	}

	pre, err := l.Instantiate(ctx, comp)
	if err != nil {
		b.Fatal(err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := inst.CallRaw(ctx, "add")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// WASM module with memory
var memoryWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x05, 0x03, 0x01, 0x00, 0x01, // memory section: 1 page min, no max
	0x07, 0x0a, 0x01, 0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, // export "memory"
}

func BenchmarkMemoryAccess(b *testing.B) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	comp := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{memoryWasm},
			CoreInstances: []component.CoreInstance{
				{
					Parsed: &component.ParsedCoreInstance{
						Kind:        component.CoreInstanceInstantiate,
						ModuleIndex: 0,
					},
				},
			},
		},
	}

	pre, err := l.Instantiate(ctx, comp)
	if err != nil {
		b.Fatal(err)
	}
	defer pre.Close(ctx)

	inst, err := pre.NewInstance(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	mem := inst.Memory()
	if mem == nil {
		b.Fatal("no memory")
	}

	data := []byte("benchmark data for memory access test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mem.Write(0, data)
		mem.Read(0, uint32(len(data)))
	}
}

func TestMemoryLeak_InstantiateClose(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	l := NewWithDefaults(rt)

	comp := &component.ValidatedComponent{
		Raw: &component.Component{
			CoreModules: [][]byte{memoryWasm},
			CoreInstances: []component.CoreInstance{
				{
					Parsed: &component.ParsedCoreInstance{
						Kind:        component.CoreInstanceInstantiate,
						ModuleIndex: 0,
					},
				},
			},
		},
	}

	pre, err := l.Instantiate(ctx, comp)
	if err != nil {
		t.Fatal(err)
	}
	defer pre.Close(ctx)

	// Warm up and get baseline
	for i := 0; i < 10; i++ {
		inst, _ := pre.NewInstance(ctx)
		inst.Close(ctx)
	}
	runtime.GC()

	var mBefore, mAfter runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	// Run 1000 cycles
	for i := 0; i < 1000; i++ {
		inst, err := pre.NewInstance(ctx)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		inst.Close(ctx)
	}

	runtime.GC()
	runtime.ReadMemStats(&mAfter)

	// Check heap growth (allow some variance, but flag major leaks)
	heapGrowth := int64(mAfter.HeapAlloc) - int64(mBefore.HeapAlloc)
	t.Logf("Heap before: %d KB, after: %d KB, growth: %d KB",
		mBefore.HeapAlloc/1024, mAfter.HeapAlloc/1024, heapGrowth/1024)

	// 1MB growth over 1000 iterations would indicate a leak
	if heapGrowth > 1024*1024 {
		t.Errorf("Potential memory leak: heap grew by %d bytes over 1000 iterations", heapGrowth)
	}
}
