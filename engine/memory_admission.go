package engine

import (
	"fmt"

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
	plan, ready, rawBytes := m.memoryPlan, m.memoryPlanReady, m.rawBytes
	m.closeMu.Unlock()
	if ready {
		return plan, nil
	}
	return parseOwnedMemoryPlan(rawBytes)
}

func parseOwnedMemoryPlan(rawBytes []byte) ([]linker.OwnedMemory, error) {
	metadata, err := wasm.ParseModuleMetadata(rawBytes)
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
	return plan, nil
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
	if m.validated != nil || m.sourceReleased || m.rawBytes == nil {
		return nil
	}
	plan, err := parseOwnedMemoryPlan(m.rawBytes)
	if err != nil {
		return err
	}
	m.memoryPlan = plan
	m.memoryPlanReady = true
	m.rawBytes = nil
	m.sourceReleased = true
	return nil
}
