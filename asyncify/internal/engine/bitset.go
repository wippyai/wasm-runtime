package engine

// BitSet is a compact set of uint32 values using a bitmap.
// Optimized for small dense sets (typical local variable indices).
type BitSet struct {
	bits []uint64
}

// NewBitSet creates a BitSet that can hold values up to maxVal (inclusive).
func NewBitSet(maxVal int) *BitSet {
	words := (maxVal + 64) / 64
	return &BitSet{bits: make([]uint64, words)}
}

// Set adds val to the set.
func (b *BitSet) Set(val uint32) {
	word := val / 64
	if int(word) >= len(b.bits) {
		b.grow(int(word) + 1)
	}
	b.bits[word] |= 1 << (val % 64)
}

// Clear removes val from the set.
func (b *BitSet) Clear(val uint32) {
	word := val / 64
	if int(word) < len(b.bits) {
		b.bits[word] &^= 1 << (val % 64)
	}
}

// Has returns true if val is in the set.
func (b *BitSet) Has(val uint32) bool {
	word := val / 64
	if int(word) >= len(b.bits) {
		return false
	}
	return b.bits[word]&(1<<(val%64)) != 0
}

// Union adds all elements from other into this set.
func (b *BitSet) Union(other *BitSet) {
	if len(other.bits) > len(b.bits) {
		b.grow(len(other.bits))
	}
	for i := range other.bits {
		b.bits[i] |= other.bits[i]
	}
}

// Reset clears all elements from the set.
func (b *BitSet) Reset() {
	for i := range b.bits {
		b.bits[i] = 0
	}
}

// ToSlice returns sorted slice of all values in the set.
func (b *BitSet) ToSlice() []uint32 {
	var result []uint32
	for i, word := range b.bits {
		if word == 0 {
			continue
		}
		base := uint32(i * 64)
		for bit := 0; bit < 64; bit++ {
			if word&(1<<bit) != 0 {
				result = append(result, base+uint32(bit))
			}
		}
	}
	return result
}

// Count returns the number of elements in the set.
func (b *BitSet) Count() int {
	count := 0
	for _, word := range b.bits {
		count += popcount(word)
	}
	return count
}

// grow expands the bitset to n words.
// Callers guarantee n > len(b.bits).
func (b *BitSet) grow(n int) {
	newBits := make([]uint64, n)
	copy(newBits, b.bits)
	b.bits = newBits
}

// popcount returns number of 1 bits in x (Hamming weight).
func popcount(x uint64) int {
	// Brian Kernighan's algorithm
	count := 0
	for x != 0 {
		x &= x - 1
		count++
	}
	return count
}
