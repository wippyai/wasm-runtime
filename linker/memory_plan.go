package linker

import (
	"fmt"
	"math"

	"github.com/wippyai/wasm-runtime/component"
)

// OwnedMemory describes one allocation in the actual core-instantiation order.
// It describes logical guest bytes; it does not claim backing-capacity or RSS
// enforcement. Imported aliases create no allocation and therefore no entry.
type OwnedMemory struct {
	MinimumPages, MaximumPages                     uint64
	InstanceIndex, ModuleIndex, DefinedMemoryIndex uint32
	HasMaximum, Shared, Memory64                   bool
}

// OwnedMemoryPlan returns a detached plan for one component instance. Repeated
// instantiations of a core module produce repeated entries; unused definitions
// and synthetic instances assembled from exports do not allocate new memory.
func (pre *InstancePre) OwnedMemoryPlan() ([]OwnedMemory, error) {
	if pre == nil || pre.closed.Load() {
		return nil, fmt.Errorf("component pre-instance is closed")
	}
	if pre.graph == nil {
		return nil, nil
	}
	var plan []OwnedMemory
	for _, idx := range pre.topoOrder {
		if idx < 0 || idx >= len(pre.graph.Instances) {
			return nil, fmt.Errorf("invalid planned instance %d", idx)
		}
		inst := pre.graph.Instances[idx]
		if inst == nil || inst.Kind == component.CoreInstanceFromExports {
			continue
		}
		if inst.Kind != component.CoreInstanceInstantiate || int(inst.ModuleIndex) >= len(pre.ownedMemoryTypes) {
			return nil, fmt.Errorf("invalid module for instance %d", idx)
		}
		for memoryIndex, m := range pre.ownedMemoryTypes[inst.ModuleIndex] {
			item := OwnedMemory{InstanceIndex: uint32(idx), ModuleIndex: inst.ModuleIndex, DefinedMemoryIndex: uint32(memoryIndex), MinimumPages: m.Limits.Min, Shared: m.Limits.Shared, Memory64: m.Limits.Memory64}
			if m.Limits.Max != nil {
				item.HasMaximum = true
				item.MaximumPages = *m.Limits.Max
			}
			plan = append(plan, item)
		}
	}
	return plan, nil
}

// InitialMemoryBytes checks arithmetic before any admission or allocation.
func InitialMemoryBytes(plan []OwnedMemory) (uint64, error) {
	var total uint64
	for _, memory := range plan {
		if memory.MinimumPages > math.MaxUint64/65536 {
			return 0, fmt.Errorf("memory minimum overflows bytes")
		}
		size := memory.MinimumPages * 65536
		if size > math.MaxUint64-total {
			return 0, fmt.Errorf("aggregate memory minimum overflows bytes")
		}
		total += size
	}
	return total, nil
}
