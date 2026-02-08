package resolve

import (
	"testing"
)

func TestNewVirtualInstance(t *testing.T) {
	vi := NewVirtualInstance("test")
	if vi.Name() != "test" {
		t.Errorf("expected name 'test', got %s", vi.Name())
	}
	if vi.entities == nil {
		t.Error("entities map should not be nil")
	}
}

func TestVirtualInstance_Define(t *testing.T) {
	vi := NewVirtualInstance("test")
	entity := Entity{Kind: EntityFunc, Source: TrapFunc{Name: "test"}}
	vi.Define("func1", entity)

	got := vi.Get("func1")
	if got == nil {
		t.Fatal("expected entity, got nil")
	}
	if got.Kind != EntityFunc {
		t.Errorf("expected EntityFunc, got %v", got.Kind)
	}
}

func TestVirtualInstance_Get_NotFound(t *testing.T) {
	vi := NewVirtualInstance("test")
	got := vi.Get("nonexistent")
	if got != nil {
		t.Error("expected nil for nonexistent entity")
	}
}

func TestVirtualInstance_GetFunc_NilModule(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.DefineModuleExport("func1", EntityFunc, nil, "export1")

	fn := vi.GetFunc("func1")
	if fn != nil {
		t.Error("expected nil for nil module")
	}
}

func TestVirtualInstance_GetFunc_WrongKind(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.Define("mem", Entity{Kind: EntityMemory})

	fn := vi.GetFunc("mem")
	if fn != nil {
		t.Error("expected nil for non-func entity")
	}
}

func TestVirtualInstance_GetFunc_HostFunc(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.Define("hostfn", Entity{
		Kind:   EntityFunc,
		Source: HostFunc{},
	})

	fn := vi.GetFunc("hostfn")
	if fn != nil {
		t.Error("expected nil for host func (needs separate handling)")
	}
}

func TestVirtualInstance_GetMemory_NilModule(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.DefineModuleExport("mem", EntityMemory, nil, "memory")

	mem := vi.GetMemory("mem")
	if mem != nil {
		t.Error("expected nil for nil module")
	}
}

func TestVirtualInstance_GetMemory_WrongKind(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.Define("func1", Entity{Kind: EntityFunc})

	mem := vi.GetMemory("func1")
	if mem != nil {
		t.Error("expected nil for non-memory entity")
	}
}

func TestVirtualInstance_GetMemory_DirectMemory(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.DefineMemory("mem", nil)

	// DirectMemory with nil Memory
	mem := vi.GetMemory("mem")
	if mem != nil {
		t.Error("expected nil for nil memory")
	}
}

func TestVirtualInstance_GetGlobal_NilModule(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.DefineModuleExport("global", EntityGlobal, nil, "g1")

	g := vi.GetGlobal("global")
	if g != nil {
		t.Error("expected nil for nil module")
	}
}

func TestVirtualInstance_GetGlobal_WrongKind(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.Define("func1", Entity{Kind: EntityFunc})

	g := vi.GetGlobal("func1")
	if g != nil {
		t.Error("expected nil for non-global entity")
	}
}

func TestVirtualInstance_HasTable(t *testing.T) {
	vi := NewVirtualInstance("test")
	if vi.HasTable("table1") {
		t.Error("expected false for nonexistent table")
	}

	vi.DefineTableRef("table1", nil, "tbl")
	if !vi.HasTable("table1") {
		t.Error("expected true for defined table")
	}

	// Wrong kind
	vi.Define("notatable", Entity{Kind: EntityFunc})
	if vi.HasTable("notatable") {
		t.Error("expected false for wrong kind")
	}
}

func TestVirtualInstance_All_ReturnsCopy(t *testing.T) {
	vi := NewVirtualInstance("test")
	vi.Define("e1", Entity{Kind: EntityFunc})

	all := vi.All()
	all["e2"] = Entity{Kind: EntityMemory}

	// Original should not be affected
	if vi.Get("e2") != nil {
		t.Error("All() should return a copy")
	}
}

func TestVirtualInstance_HasMemory(t *testing.T) {
	vi := NewVirtualInstance("test")
	if vi.HasMemory() {
		t.Error("expected false for empty instance")
	}

	vi.Define("mem", Entity{Kind: EntityMemory, Source: nil})
	if vi.HasMemory() {
		t.Error("expected false for nil source")
	}

	vi.DefineMemory("mem2", nil) // Has Source even if Memory is nil
	if !vi.HasMemory() {
		t.Error("expected true for memory with source")
	}
}

func TestVirtualInstance_HasTableEntity(t *testing.T) {
	vi := NewVirtualInstance("test")
	if vi.HasTableEntity() {
		t.Error("expected false for empty instance")
	}

	vi.DefineTableRef("table", nil, "tbl")
	if !vi.HasTableEntity() {
		t.Error("expected true after adding table")
	}
}

func TestVirtualInstance_HasGlobals(t *testing.T) {
	vi := NewVirtualInstance("test")
	if vi.HasGlobals() {
		t.Error("expected false for empty instance")
	}

	// DirectGlobal without SourceModule
	vi.DefineGlobal("g1", nil)
	if vi.HasGlobals() {
		t.Error("expected false without SourceModule")
	}
}
