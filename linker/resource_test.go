package linker

import (
	"testing"
)

func TestResourceTableNew(t *testing.T) {
	rt := NewResourceTable(nil)

	h1 := rt.New(100)
	h2 := rt.New(200)

	if h1 == h2 {
		t.Error("New returned same handle for different resources")
	}

	rep1, ok := rt.Rep(h1)
	if !ok {
		t.Fatal("Rep returned false for valid handle")
	}
	if rep1 != 100 {
		t.Errorf("Rep = %d, want 100", rep1)
	}

	rep2, ok := rt.Rep(h2)
	if !ok {
		t.Fatal("Rep returned false for valid handle")
	}
	if rep2 != 200 {
		t.Errorf("Rep = %d, want 200", rep2)
	}
}

func TestResourceTableDrop(t *testing.T) {
	dtorCalled := false
	var dtorRep uint32
	dtor := func(rep uint32) {
		dtorCalled = true
		dtorRep = rep
	}

	rt := NewResourceTable(dtor)

	h := rt.New(42)
	rep, needsDtor, err := rt.Drop(h)
	if err != nil {
		t.Fatalf("Drop failed: %v", err)
	}
	if rep != 42 {
		t.Errorf("Drop returned rep = %d, want 42", rep)
	}
	if !needsDtor {
		t.Error("Drop returned needsDtor = false, want true")
	}

	rt.RunDestructor(rep)
	if !dtorCalled {
		t.Error("Destructor not called")
	}
	if dtorRep != 42 {
		t.Errorf("Destructor called with rep = %d, want 42", dtorRep)
	}

	// Second drop should fail
	_, _, err = rt.Drop(h)
	if err == nil {
		t.Error("Expected error on double drop")
	}
}

func TestResourceTableDropWithoutDtor(t *testing.T) {
	rt := NewResourceTable(nil)

	h := rt.New(42)
	rep, needsDtor, err := rt.Drop(h)
	if err != nil {
		t.Fatalf("Drop failed: %v", err)
	}
	if rep != 42 {
		t.Errorf("Drop returned rep = %d, want 42", rep)
	}
	if needsDtor {
		t.Error("Drop returned needsDtor = true, want false (no dtor)")
	}
}

func TestResourceTableBorrow(t *testing.T) {
	rt := NewResourceTable(nil)

	h := rt.New(42)

	err := rt.Borrow(h)
	if err != nil {
		t.Fatalf("Borrow failed: %v", err)
	}

	// Can't drop while borrowed
	_, _, err = rt.Drop(h)
	if err == nil {
		t.Error("Expected error dropping borrowed resource")
	}

	err = rt.EndBorrow(h)
	if err != nil {
		t.Fatalf("EndBorrow failed: %v", err)
	}

	// Now drop should work
	_, _, err = rt.Drop(h)
	if err != nil {
		t.Errorf("Drop failed after end borrow: %v", err)
	}
}

func TestResourceTableMultipleBorrows(t *testing.T) {
	rt := NewResourceTable(nil)

	h := rt.New(42)

	_ = rt.Borrow(h)
	_ = rt.Borrow(h)
	_ = rt.Borrow(h)

	// Still can't drop
	_, _, err := rt.Drop(h)
	if err == nil {
		t.Error("Expected error dropping with active borrows")
	}

	_ = rt.EndBorrow(h)
	_ = rt.EndBorrow(h)

	// Still can't drop
	_, _, err = rt.Drop(h)
	if err == nil {
		t.Error("Expected error dropping with active borrow")
	}

	_ = rt.EndBorrow(h)

	// Now can drop
	_, _, err = rt.Drop(h)
	if err != nil {
		t.Errorf("Drop failed: %v", err)
	}
}

func TestResourceTableClone(t *testing.T) {
	dtor := func(rep uint32) {}
	rt := NewResourceTable(dtor)

	h := rt.New(42)

	err := rt.Clone(h)
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	// First drop shouldn't need dtor (ref count still > 0)
	_, needsDtor, err := rt.Drop(h)
	if err != nil {
		t.Fatalf("First Drop failed: %v", err)
	}
	if needsDtor {
		t.Error("First drop shouldn't need dtor (ref count > 0)")
	}

	// Rep should still work
	rep, ok := rt.Rep(h)
	if !ok {
		t.Error("Rep should still work after first drop")
	}
	if rep != 42 {
		t.Errorf("Rep = %d, want 42", rep)
	}

	// Second drop should need dtor (ref count = 0)
	_, needsDtor, err = rt.Drop(h)
	if err != nil {
		t.Fatalf("Second Drop failed: %v", err)
	}
	if !needsDtor {
		t.Error("Second drop should need dtor")
	}
}

func TestResourceTableLen(t *testing.T) {
	rt := NewResourceTable(nil)

	if rt.Len() != 0 {
		t.Errorf("Len() = %d, want 0", rt.Len())
	}

	h1 := rt.New(1)
	h2 := rt.New(2)

	if rt.Len() != 2 {
		t.Errorf("Len() = %d, want 2", rt.Len())
	}

	if _, _, err := rt.Drop(h1); err != nil {
		t.Fatal(err)
	}
	if rt.Len() != 1 {
		t.Errorf("Len() = %d, want 1", rt.Len())
	}

	if _, _, err := rt.Drop(h2); err != nil {
		t.Fatal(err)
	}
	if rt.Len() != 0 {
		t.Errorf("Len() = %d, want 0", rt.Len())
	}
}

func TestResourceTableFreeListReuse(t *testing.T) {
	rt := NewResourceTable(nil)

	h1 := rt.New(1)
	if _, _, err := rt.Drop(h1); err != nil {
		t.Fatal(err)
	}

	// New allocation should reuse the dropped slot
	h2 := rt.New(2)
	if h2 != h1 {
		t.Error("Expected handle reuse from free list")
	}

	rep, ok := rt.Rep(h2)
	if !ok {
		t.Fatal("Rep failed for reused handle")
	}
	if rep != 2 {
		t.Errorf("Rep = %d, want 2", rep)
	}
}

func TestResourceStore(t *testing.T) {
	store := NewResourceStore()

	t1 := store.Table(1)
	t2 := store.Table(2)

	if t1 == t2 {
		t.Error("Different type IDs returned same table")
	}

	// Same type ID should return same table
	t1Again := store.Table(1)
	if t1Again != t1 {
		t.Error("Same type ID returned different table")
	}
}

func TestResourceStoreWithDtor(t *testing.T) {
	store := NewResourceStore()

	called := false
	dtor := func(rep uint32) { called = true }

	t1 := store.TableWithDtor(1, dtor)
	h := t1.New(42)
	rep, needsDtor, _ := t1.Drop(h)
	if needsDtor {
		t1.RunDestructor(rep)
	}

	if !called {
		t.Error("Destructor not called")
	}
}

func TestResourceTableInvalidHandle(t *testing.T) {
	rt := NewResourceTable(nil)

	// Invalid handle operations
	_, ok := rt.Rep(999)
	if ok {
		t.Error("Rep should return false for invalid handle")
	}

	_, _, err := rt.Drop(999)
	if err == nil {
		t.Error("Drop should fail for invalid handle")
	}

	err = rt.Borrow(999)
	if err == nil {
		t.Error("Borrow should fail for invalid handle")
	}

	err = rt.EndBorrow(999)
	if err == nil {
		t.Error("EndBorrow should fail for invalid handle")
	}

	err = rt.Clone(999)
	if err == nil {
		t.Error("Clone should fail for invalid handle")
	}
}

func TestResourceStoreTableWithDtor_ExistingTable(t *testing.T) {
	store := NewResourceStore()

	// Create table first with Table()
	t1 := store.Table(1)

	// TableWithDtor should return the same table
	dtor := func(rep uint32) {}
	t1Again := store.TableWithDtor(1, dtor)

	if t1Again != t1 {
		t.Error("TableWithDtor should return existing table")
	}
}

func TestResourceTableClone_DroppedHandle(t *testing.T) {
	rt := NewResourceTable(nil)

	h := rt.New(42)
	if _, _, err := rt.Drop(h); err != nil {
		t.Fatal(err)
	}

	err := rt.Clone(h)
	if err == nil {
		t.Error("Clone should fail for dropped handle")
	}
}

func TestResourceTableBorrow_DroppedHandle(t *testing.T) {
	rt := NewResourceTable(nil)

	h := rt.New(42)
	if _, _, err := rt.Drop(h); err != nil {
		t.Fatal(err)
	}

	err := rt.Borrow(h)
	if err == nil {
		t.Error("Borrow should fail for dropped handle")
	}
}
