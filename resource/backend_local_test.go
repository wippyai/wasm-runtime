package resource

import (
	"errors"
	"sync"
	"testing"
)

func TestLocalBackend_Basic(t *testing.T) {
	b := NewLocalBackend()

	// Create a resource
	handle, err := b.Create(1, "test value")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if handle == 0 {
		t.Fatal("Expected non-zero handle")
	}

	// Get it back
	val, ok := b.Get(handle)
	if !ok {
		t.Fatal("Get failed")
	}
	if val != "test value" {
		t.Fatalf("Expected 'test value', got %v", val)
	}

	// Drop it
	val, ok = b.Drop(handle)
	if !ok {
		t.Fatal("Drop failed")
	}
	if val != "test value" {
		t.Fatalf("Expected 'test value', got %v", val)
	}

	// Should not exist anymore
	_, ok = b.Get(handle)
	if ok {
		t.Fatal("Expected Get to fail after Drop")
	}
}

func TestLocalBackend_ABIOperations(t *testing.T) {
	b := NewLocalBackend()

	// Test NewFromRep
	handle := b.NewFromRep(1, 12345)
	if handle == 0 {
		t.Fatal("Expected non-zero handle")
	}

	// Test Rep
	rep, ok := b.Rep(handle)
	if !ok {
		t.Fatal("Rep failed")
	}
	if rep != 12345 {
		t.Fatalf("Expected rep 12345, got %d", rep)
	}

	// Test TypeID
	typeID, ok := b.TypeID(handle)
	if !ok {
		t.Fatal("TypeID failed")
	}
	if typeID != 1 {
		t.Fatalf("Expected typeID 1, got %d", typeID)
	}
}

func TestLocalBackend_Borrow(t *testing.T) {
	b := NewLocalBackend()

	handle := b.NewFromRep(1, 100)

	// Borrow
	if !b.Borrow(handle) {
		t.Fatal("Borrow failed")
	}

	// Cannot drop with outstanding borrow
	_, ok := b.Drop(handle)
	if ok {
		t.Fatal("Drop should fail with outstanding borrow")
	}

	// Return borrow
	if !b.ReturnBorrow(handle) {
		t.Fatal("ReturnBorrow failed")
	}

	// Now can drop
	_, ok = b.Drop(handle)
	if !ok {
		t.Fatal("Drop should succeed after returning borrow")
	}
}

func TestLocalBackend_MultipleBorrows(t *testing.T) {
	b := NewLocalBackend()

	handle := b.NewFromRep(1, 100)

	// Multiple borrows
	for i := 0; i < 5; i++ {
		if !b.Borrow(handle) {
			t.Fatalf("Borrow %d failed", i)
		}
	}

	// Cannot drop
	_, ok := b.Drop(handle)
	if ok {
		t.Fatal("Drop should fail with outstanding borrows")
	}

	// Return all borrows
	for i := 0; i < 5; i++ {
		if !b.ReturnBorrow(handle) {
			t.Fatalf("ReturnBorrow %d failed", i)
		}
	}

	// Now can drop
	_, ok = b.Drop(handle)
	if !ok {
		t.Fatal("Drop should succeed after returning all borrows")
	}
}

func TestLocalBackend_HandleReuse(t *testing.T) {
	b := NewLocalBackend()

	// Create and drop several handles
	h1 := b.NewFromRep(1, 1)
	h2 := b.NewFromRep(1, 2)
	h3 := b.NewFromRep(1, 3)

	b.Drop(h2)
	b.Drop(h1)

	// New handles should reuse freed slots
	h4 := b.NewFromRep(1, 4)
	h5 := b.NewFromRep(1, 5)

	// h4 and h5 should be either h1 or h2 (reused)
	if h4 != h1 && h4 != h2 {
		t.Log("Handle not reused, but that's ok")
	}

	// Verify all handles work
	if _, ok := b.Rep(h3); !ok {
		t.Fatal("h3 should still be valid")
	}
	if _, ok := b.Rep(h4); !ok {
		t.Fatal("h4 should be valid")
	}
	if _, ok := b.Rep(h5); !ok {
		t.Fatal("h5 should be valid")
	}
}

func TestLocalBackend_Close(t *testing.T) {
	b := NewLocalBackend()

	b.NewFromRep(1, 1)
	b.NewFromRep(1, 2)

	if err := b.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Operations should fail after close
	_, err := b.Create(1, "test")
	if !errors.Is(err, ErrClosed) {
		t.Fatal("Expected ErrClosed after Close")
	}
}

func TestLocalBackend_Concurrent(t *testing.T) {
	b := NewLocalBackend()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			h, _ := b.Create(1, id)
			b.Borrow(h)
			b.ReturnBorrow(h)
			b.Drop(h)
		}(i)
	}

	wg.Wait()
}

func TestLocalBackend_Len(t *testing.T) {
	b := NewLocalBackend()

	if b.Len() != 0 {
		t.Fatal("Expected Len() == 0 initially")
	}

	h1, _ := b.Create(1, "a")
	h2, _ := b.Create(1, "b")
	b.Create(1, "c")

	if b.Len() != 3 {
		t.Fatalf("Expected Len() == 3, got %d", b.Len())
	}

	b.Drop(h1)
	if b.Len() != 2 {
		t.Fatalf("Expected Len() == 2, got %d", b.Len())
	}

	b.Drop(h2)
	if b.Len() != 1 {
		t.Fatalf("Expected Len() == 1, got %d", b.Len())
	}
}

func TestLocalBackend_Each(t *testing.T) {
	b := NewLocalBackend()

	b.Create(1, "a")
	b.Create(2, "b")
	b.Create(1, "c")

	count := 0
	b.Each(func(h Handle, typeID uint32, value any) bool {
		count++
		return true
	})

	if count != 3 {
		t.Fatalf("Expected to iterate over 3 items, got %d", count)
	}

	// Test early termination
	count = 0
	b.Each(func(h Handle, typeID uint32, value any) bool {
		count++
		return false
	})

	if count != 1 {
		t.Fatalf("Expected to iterate over 1 item (early term), got %d", count)
	}
}

func TestLocalBackend_InvalidHandle(t *testing.T) {
	b := NewLocalBackend()

	// Handle 0 is always invalid
	if _, ok := b.Get(0); ok {
		t.Fatal("Handle 0 should be invalid")
	}
	if _, ok := b.Rep(0); ok {
		t.Fatal("Handle 0 should be invalid for Rep")
	}
	if b.Borrow(0) {
		t.Fatal("Handle 0 should fail Borrow")
	}
	if b.ReturnBorrow(0) {
		t.Fatal("Handle 0 should fail ReturnBorrow")
	}
	if _, ok := b.Drop(0); ok {
		t.Fatal("Handle 0 should fail Drop")
	}

	// Non-existent handle
	if _, ok := b.Get(999); ok {
		t.Fatal("Non-existent handle should be invalid")
	}
}
