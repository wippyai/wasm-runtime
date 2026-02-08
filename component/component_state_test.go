package component

import (
	"testing"

	"github.com/wippyai/wasm-runtime/component/internal/arena"
)

func TestComponentState_Basic(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	if cs == nil {
		t.Fatal("NewComponentState returned nil")
	}
}

func TestComponentState_AddGetType(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	// Add types
	idx1 := cs.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: 0})
	idx2 := cs.AddType(arena.AnyTypeID{Kind: arena.TypeKindFunc, ID: 1})

	if idx1 != 0 {
		t.Errorf("first type index = %d, want 0", idx1)
	}
	if idx2 != 1 {
		t.Errorf("second type index = %d, want 1", idx2)
	}

	// Get types
	t1, err := cs.GetType(0)
	if err != nil {
		t.Fatalf("GetType(0) failed: %v", err)
	}
	if t1.Kind != arena.TypeKindDefined || t1.ID != 0 {
		t.Errorf("GetType(0) = %v, want {Defined, 0}", t1)
	}

	// Get out of range
	_, err = cs.GetType(100)
	if err == nil {
		t.Error("GetType(100) should fail")
	}
}

func TestComponentState_AddGetFunc(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	idx := cs.AddFunc(42)
	if idx != 0 {
		t.Errorf("AddFunc returned %d, want 0", idx)
	}

	typeID, err := cs.GetFunc(0)
	if err != nil {
		t.Fatalf("GetFunc(0) failed: %v", err)
	}
	if typeID != 42 {
		t.Errorf("GetFunc(0) = %d, want 42", typeID)
	}

	_, err = cs.GetFunc(100)
	if err == nil {
		t.Error("GetFunc(100) should fail")
	}
}

func TestComponentState_AddGetValue(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	idx := cs.AddValue(arena.ValType{Primitive: arena.PrimBool})
	if idx != 0 {
		t.Errorf("AddValue returned %d, want 0", idx)
	}

	val, err := cs.GetValue(0)
	if err != nil {
		t.Fatalf("GetValue(0) failed: %v", err)
	}
	if val.Primitive != arena.PrimBool {
		t.Errorf("GetValue(0).Primitive = %v, want Bool", val.Primitive)
	}

	_, err = cs.GetValue(100)
	if err == nil {
		t.Error("GetValue(100) should fail")
	}
}

func TestComponentState_MarkValueUsed(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	cs.AddValue(arena.ValType{Primitive: arena.PrimS32})
	cs.AddValue(arena.ValType{Primitive: arena.PrimU32})

	// MarkValueUsed marks the value as used (no error return)
	cs.MarkValueUsed(0)

	// Marking out of range is a no-op
	cs.MarkValueUsed(100)
}

func TestComponentState_AddGetComponent(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	idx := cs.AddComponent(10)
	if idx != 0 {
		t.Errorf("AddComponent returned %d, want 0", idx)
	}

	typeID, err := cs.GetComponent(0)
	if err != nil {
		t.Fatalf("GetComponent(0) failed: %v", err)
	}
	if typeID != 10 {
		t.Errorf("GetComponent(0) = %d, want 10", typeID)
	}

	_, err = cs.GetComponent(100)
	if err == nil {
		t.Error("GetComponent(100) should fail")
	}
}

func TestComponentState_AddGetCoreModule(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	idx := cs.AddCoreModule(5)
	if idx != 0 {
		t.Errorf("AddCoreModule returned %d, want 0", idx)
	}

	typeID, err := cs.GetCoreModule(0)
	if err != nil {
		t.Fatalf("GetCoreModule(0) failed: %v", err)
	}
	if typeID != 5 {
		t.Errorf("GetCoreModule(0) = %d, want 5", typeID)
	}

	_, err = cs.GetCoreModule(100)
	if err == nil {
		t.Error("GetCoreModule(100) should fail")
	}
}

func TestComponentState_AddGetCoreFunc(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	idx := cs.AddCoreFunc(7)
	if idx != 0 {
		t.Errorf("AddCoreFunc returned %d, want 0", idx)
	}

	typeID, err := cs.GetCoreFunc(0)
	if err != nil {
		t.Fatalf("GetCoreFunc(0) failed: %v", err)
	}
	if typeID != 7 {
		t.Errorf("GetCoreFunc(0) = %d, want 7", typeID)
	}

	_, err = cs.GetCoreFunc(100)
	if err == nil {
		t.Error("GetCoreFunc(100) should fail")
	}
}

func TestComponentState_Import(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	err := cs.AddImport("test", arena.EntityType{Kind: arena.EntityKindFunc, ID: 0})
	if err != nil {
		t.Fatalf("AddImport failed: %v", err)
	}

	// Duplicate import should fail
	err = cs.AddImport("test", arena.EntityType{Kind: arena.EntityKindFunc, ID: 1})
	if err == nil {
		t.Error("duplicate import should fail")
	}
}

func TestComponentState_Instances(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	cs.AddInstance(1)
	cs.AddInstance(2)

	instances := cs.Instances()
	if len(instances) != 2 {
		t.Errorf("Instances() len = %d, want 2", len(instances))
	}
}

func TestComponentState_GetInstance(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	idx := cs.AddInstance(3)
	if idx != 0 {
		t.Errorf("AddInstance returned %d, want 0", idx)
	}

	typeID, err := cs.GetInstance(0)
	if err != nil {
		t.Fatalf("GetInstance(0) failed: %v", err)
	}
	if typeID != 3 {
		t.Errorf("GetInstance(0) = %d, want 3", typeID)
	}

	_, err = cs.GetInstance(100)
	if err == nil {
		t.Error("GetInstance(100) should fail")
	}
}

func TestComponentState_Counts(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	if cs.TypeCount() != 0 {
		t.Error("expected 0 types initially")
	}
	if cs.FuncCount() != 0 {
		t.Error("expected 0 funcs initially")
	}
	if cs.InstanceCount() != 0 {
		t.Error("expected 0 instances initially")
	}

	cs.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: 0})
	cs.AddFunc(0)
	cs.AddInstance(0)

	if cs.TypeCount() != 1 {
		t.Error("expected 1 type after AddType")
	}
	if cs.FuncCount() != 1 {
		t.Error("expected 1 func after AddFunc")
	}
	if cs.InstanceCount() != 1 {
		t.Error("expected 1 instance after AddInstance")
	}
}

func TestComponentState_Export(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	err := cs.AddExport("testExport", arena.EntityType{Kind: arena.EntityKindFunc, ID: 0})
	if err != nil {
		t.Fatalf("AddExport failed: %v", err)
	}

	// Duplicate export should fail
	err = cs.AddExport("testExport", arena.EntityType{Kind: arena.EntityKindFunc, ID: 1})
	if err == nil {
		t.Error("duplicate export should fail")
	}
}

func TestComponentState_MultipleTypes(t *testing.T) {
	cs := arena.NewState(arena.KindComponent)

	// Add multiple types
	for i := 0; i < 10; i++ {
		idx := cs.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: arena.TypeID(i)})
		if idx != uint32(i) {
			t.Errorf("AddType %d returned %d, want %d", i, idx, i)
		}
	}

	// Verify all can be retrieved
	for i := 0; i < 10; i++ {
		typ, err := cs.GetType(uint32(i))
		if err != nil {
			t.Errorf("GetType(%d) failed: %v", i, err)
		}
		if typ.ID != arena.TypeID(i) {
			t.Errorf("GetType(%d).ID = %d, want %d", i, typ.ID, i)
		}
	}
}

func TestComponentState_InstanceType(t *testing.T) {
	// Create ComponentState with instance type kind
	csInstance := arena.NewState(arena.KindInstanceType)
	if csInstance == nil {
		t.Fatal("NewComponentState returned nil for instance type kind")
	}

	// Should still be able to add types, funcs, etc.
	idx := csInstance.AddType(arena.AnyTypeID{Kind: arena.TypeKindDefined, ID: 0})
	if idx != 0 {
		t.Errorf("AddType returned %d, want 0", idx)
	}
}

func TestComponentState_ComponentType(t *testing.T) {
	// Create ComponentState with component type kind
	csCompType := arena.NewState(arena.KindComponentType)
	if csCompType == nil {
		t.Fatal("NewComponentState returned nil for component type kind")
	}

	idx := csCompType.AddFunc(0)
	if idx != 0 {
		t.Errorf("AddFunc returned %d, want 0", idx)
	}
}
