package engine

import (
	"testing"

	"github.com/tetratelabs/wazero/api"
)

func TestLinearMemoryUsageCountsAllCores(t *testing.T) {
	ctx := t.Context()
	eng, mod := loadTwoCoreModule(t)
	t.Cleanup(func() { _ = eng.Close(ctx) })
	for _, entry := range []string{"", "func1", "func2"} {
		t.Run("entry="+entry, func(t *testing.T) {
			inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EntryExport: entry})
			if err != nil {
				t.Fatal(err)
			}
			defer inst.Close(ctx)
			if size, present := inst.LinearMemoryUsage(); !present || size != 5*65536 {
				t.Fatalf("usage = (%d, %v), want five pages across both cores", size, present)
			}
			// Auxiliary-core growth must count even when another core is selected.
			var grown api.Module
			for _, core := range inst.linkerInst.Modules() {
				if core != nil && hasMemory(core.Memory()) {
					grown = core
					break
				}
			}
			if _, ok := grown.Memory().Grow(1); !ok {
				t.Fatal("grow failed")
			}
			if size, present := inst.LinearMemoryUsage(); !present || size != 6*65536 {
				t.Fatalf("usage after growth = (%d, %v)", size, present)
			}
			// Aliases/imports must not charge the same memory twice.
			if size, present := coreMemoryUsage([]api.Module{nil, grown, grown}); !present || size != uint64(grown.Memory().Size()) {
				t.Fatalf("aliased usage = (%d, %v)", size, present)
			}
		})
	}
}

func TestLinearMemoryUsageNil(t *testing.T) {
	var inst *WazeroInstance
	if size, present := inst.LinearMemoryUsage(); size != 0 || present {
		t.Fatalf("nil usage = (%d, %v)", size, present)
	}
}

type accountingMemory struct {
	api.Memory
	pages uint32
}

func (m *accountingMemory) Grow(delta uint32) (uint32, bool) {
	if delta != 0 {
		panic("accounting must never grow memory")
	}
	return m.pages, true
}

func (m *accountingMemory) Size() uint32 { return m.pages * 65536 }

type accountingModule struct {
	api.Module
	mem api.Memory
}

func (m *accountingModule) Memory() api.Memory { return m.mem }

func TestLinearMemoryUsageZeroAndFullWasm32Range(t *testing.T) {
	for _, pages := range []uint32{0, 65536} {
		mem := &accountingMemory{pages: pages}
		// Separate modules importing the same memory must count only once.
		mods := []api.Module{&accountingModule{mem: mem}, &accountingModule{mem: mem}}
		if size, present := coreMemoryUsage(mods); !present || size != uint64(pages)*65536 {
			t.Fatalf("%d pages: usage = (%d, %v)", pages, size, present)
		}
	}
}
