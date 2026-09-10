package engine

import (
	"github.com/wippyai/wasm-runtime/linker"
	"github.com/wippyai/wasm-runtime/wasm"
)

func (m *WazeroModule) admitInstanceMemory(cfg *InstanceConfig, lifetime *executionLifetime, pre *linker.InstancePre) (*linker.MemoryAdmission, error) {
	if cfg == nil || cfg.MemoryBudget == nil {
		return nil, nil
	}
	if pre != nil {
		return pre.AdmitMemory(cfg.MemoryBudget, lifetime.stop)
	}
	// rawBytes is the same final core binary passed to the stock compiler. Its
	// metadata includes unexported owned memories; imported aliases add no owner.
	metadata, err := wasm.ParseModuleMetadata(m.rawBytes)
	if err != nil {
		return nil, err
	}
	plan := make([]linker.OwnedMemory, 0, len(metadata.Memories))
	for index, memory := range metadata.Memories {
		entry := linker.OwnedMemory{DefinedMemoryIndex: uint32(index), MinimumPages: memory.Limits.Min, Shared: memory.Limits.Shared, Memory64: memory.Limits.Memory64}
		if memory.Limits.Max != nil {
			entry.HasMaximum = true
			entry.MaximumPages = *memory.Limits.Max
		}
		plan = append(plan, entry)
	}
	return linker.AdmitMemoryPlan(plan, cfg.MemoryBudget, lifetime.stop)
}
