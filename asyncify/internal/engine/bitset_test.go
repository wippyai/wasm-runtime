package engine

import (
	"testing"
)

func TestBitSet_SetHasClear(t *testing.T) {
	b := NewBitSet(100)

	// Initially empty
	if b.Has(42) {
		t.Error("new bitset should not have 42")
	}

	// Set and check
	b.Set(42)
	if !b.Has(42) {
		t.Error("bitset should have 42 after Set")
	}

	// Clear and check
	b.Clear(42)
	if b.Has(42) {
		t.Error("bitset should not have 42 after Clear")
	}
}

func TestBitSet_GrowsAutomatically(t *testing.T) {
	b := NewBitSet(10)

	// Set beyond initial capacity
	b.Set(200)
	if !b.Has(200) {
		t.Error("bitset should have 200 after grow")
	}

	// Original range should still work
	b.Set(5)
	if !b.Has(5) {
		t.Error("bitset should have 5")
	}
}

func TestBitSet_Union(t *testing.T) {
	a := NewBitSet(100)
	b := NewBitSet(100)

	a.Set(1)
	a.Set(5)
	a.Set(10)

	b.Set(5)
	b.Set(15)
	b.Set(20)

	a.Union(b)

	expected := []uint32{1, 5, 10, 15, 20}
	for _, v := range expected {
		if !a.Has(v) {
			t.Errorf("after union, bitset should have %d", v)
		}
	}
}

func TestBitSet_UnionLargerOther(t *testing.T) {
	// Test Union where other bitset is larger (triggers grow)
	a := NewBitSet(10) // small
	b := NewBitSet(10) // also small initially
	b.Set(500)         // b grows to accommodate 500

	a.Set(1)
	a.Union(b) // should grow a to match b

	if !a.Has(1) {
		t.Error("should have 1")
	}
	if !a.Has(500) {
		t.Error("should have 500 after union")
	}
}

func TestBitSet_ClearOutOfRange(t *testing.T) {
	// Clear of value beyond capacity should be no-op
	b := NewBitSet(10)
	b.Set(5)
	b.Clear(1000) // out of range, should not panic
	if !b.Has(5) {
		t.Error("should still have 5")
	}
}

func TestBitSet_HasOutOfRange(t *testing.T) {
	// Has for value beyond capacity should return false
	b := NewBitSet(10)
	if b.Has(1000) {
		t.Error("should not have 1000")
	}
}

func TestBitSet_ToSlice(t *testing.T) {
	b := NewBitSet(100)
	b.Set(10)
	b.Set(5)
	b.Set(20)
	b.Set(1)

	slice := b.ToSlice()
	if len(slice) != 4 {
		t.Fatalf("expected 4 elements, got %d", len(slice))
	}

	// Should be sorted
	expected := []uint32{1, 5, 10, 20}
	for i, v := range expected {
		if slice[i] != v {
			t.Errorf("slice[%d] = %d, want %d", i, slice[i], v)
		}
	}
}

func TestBitSet_Count(t *testing.T) {
	b := NewBitSet(100)
	if b.Count() != 0 {
		t.Error("empty bitset should have count 0")
	}

	b.Set(1)
	b.Set(63)
	b.Set(64)
	b.Set(65)

	if b.Count() != 4 {
		t.Errorf("count = %d, want 4", b.Count())
	}
}

func TestBitSet_Reset(t *testing.T) {
	b := NewBitSet(100)
	b.Set(1)
	b.Set(50)
	b.Set(99)

	b.Reset()

	if b.Count() != 0 {
		t.Error("reset bitset should have count 0")
	}
	if b.Has(1) || b.Has(50) || b.Has(99) {
		t.Error("reset bitset should not have any values")
	}
}

func TestBitSet_LargeValues(t *testing.T) {
	b := NewBitSet(10)

	// Test values spanning multiple words
	values := []uint32{0, 63, 64, 127, 128, 1000}
	for _, v := range values {
		b.Set(v)
	}

	for _, v := range values {
		if !b.Has(v) {
			t.Errorf("should have %d", v)
		}
	}

	// Non-set values
	if b.Has(1) || b.Has(65) || b.Has(500) {
		t.Error("should not have unset values")
	}
}

func TestPopcount(t *testing.T) {
	tests := []struct {
		x    uint64
		want int
	}{
		{0, 0},
		{1, 1},
		{3, 2},
		{0xFF, 8},
		{0xFFFFFFFFFFFFFFFF, 64},
		{0x8000000000000001, 2},
	}
	for _, tc := range tests {
		got := popcount(tc.x)
		if got != tc.want {
			t.Errorf("popcount(%x) = %d, want %d", tc.x, got, tc.want)
		}
	}
}
