package transcoder

import (
	"sync"

	wasmruntime "github.com/wippyai/wasm-runtime"
)

type Memory = wasmruntime.Memory
type Allocator = wasmruntime.Allocator

type Allocation struct {
	Ptr   uint32
	Size  uint32
	Align uint32
}

type AllocationList struct {
	allocations []Allocation
}

var allocationListPool = sync.Pool{
	New: func() any {
		return &AllocationList{allocations: make([]Allocation, 0, 8)}
	},
}

func NewAllocationList() *AllocationList {
	return allocationListPool.Get().(*AllocationList)
}

const maxPooledAllocationCapacity = 128

// Release returns to pool. Must call after Free(); list invalid after Release.
func (al *AllocationList) Release() {
	// Only pool small allocations to prevent memory bloat
	if cap(al.allocations) > maxPooledAllocationCapacity {
		return
	}
	al.Reset()
	allocationListPool.Put(al)
}

func (al *AllocationList) FreeAndRelease(allocator Allocator) {
	al.Free(allocator)
	al.Release()
}

func (al *AllocationList) Add(ptr, size, align uint32) {
	al.allocations = append(al.allocations, Allocation{
		Ptr:   ptr,
		Size:  size,
		Align: align,
	})
}

func (al *AllocationList) Free(allocator Allocator) {
	if allocator == nil {
		return
	}
	for _, a := range al.allocations {
		if a.Ptr != 0 {
			allocator.Free(a.Ptr, a.Size, a.Align)
		}
	}
}

func (al *AllocationList) Reset() {
	al.allocations = al.allocations[:0]
}

func (al *AllocationList) Count() int {
	return len(al.allocations)
}
