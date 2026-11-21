package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify"
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
	plan, err := m.ownedMemoryPlan()
	if err != nil {
		return nil, err
	}
	return linker.AdmitMemoryPlan(plan, cfg.MemoryBudget, lifetime.stop)
}

// ownedMemoryPlan returns the memories the core module defines. It reads the
// plan saved by ReleaseSource, or parses rawBytes, the same final core binary
// passed to the stock compiler. Its metadata includes unexported owned
// memories; imported aliases add no owner.
func (m *WazeroModule) ownedMemoryPlan() ([]linker.OwnedMemory, error) {
	m.closeMu.Lock()
	plan, ready, rawBytes := m.memoryPlan, m.sourceReleased, m.rawBytes
	m.closeMu.Unlock()
	if ready {
		return plan, nil
	}

	metadata, err := wasm.ParseModuleMetadata(rawBytes)
	if err != nil {
		return nil, err
	}
	return memoryPlanFromMetadata(metadata), nil
}

func memoryPlanFromMetadata(metadata *wasm.Module) []linker.OwnedMemory {
	plan := make([]linker.OwnedMemory, 0, len(metadata.Memories))
	for index, memory := range metadata.Memories {
		entry := linker.OwnedMemory{DefinedMemoryIndex: uint32(index), MinimumPages: memory.Limits.Min, Shared: memory.Limits.Shared, Memory64: memory.Limits.Memory64}
		if memory.Limits.Max != nil {
			entry.HasMaximum = true
			entry.MaximumPages = *memory.Limits.Max
		}
		plan = append(plan, entry)
	}
	return plan
}

// ReleaseSource drops the module binary that a compiled core module keeps for
// later Asyncify transforms and memory admission. It saves the memory plan
// first. After the release, a Compile that needs an Asyncify transform fails.
// Components keep their source.
func (m *WazeroModule) ReleaseSource() error {
	m.compileMu.Lock()
	defer m.compileMu.Unlock()

	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	if m.closed {
		return fmt.Errorf("module is closed")
	}
	if m.canonRegistry != nil || m.rawBytes == nil {
		return nil
	}
	metadata, err := wasm.ParseModuleMetadata(m.rawBytes)
	if err != nil {
		return err
	}
	m.memoryPlan = memoryPlanFromMetadata(metadata)
	m.sourceReleased = true
	m.rawBytes = nil
	m.releasedAsyncified = asyncify.HasAsyncifyProtocolExports(metadata)
	return nil
}
