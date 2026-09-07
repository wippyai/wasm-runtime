package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero/api"
)

const asyncifyHeaderBytes uint32 = 8

type asyncifyStackReservation struct {
	memory    api.Memory
	allocator *wazeroAllocator
	dataAddr  uint32
	bytes     uint32
}

type asyncifyReservationCandidate struct {
	module            api.Module
	memory            api.Memory
	allocator         *wazeroAllocator
	transformReserved bool
}

// enableOwnedAsyncify configures Asyncify with storage allocated by a proven
// same-memory guest allocator, or by a linker-proven Asyncify-created memory.
// It is intentionally private: public AsyncifyConfig remains the legacy
// compatibility path, whose default address is not an ownership proof.
func (i *WazeroInstance) enableOwnedAsyncify(ctx context.Context, stackBytes uint32) error {
	return i.enableAsyncify(ctx, AsyncifyConfig{ownedStackBytes: stackBytes})
}

// asyncifyStackModulesLocked returns every current core which declares the
// Asyncify control export. A component may route different exports to distinct
// core memories, so configuring only the entry core is insufficient.
func (i *WazeroInstance) asyncifyStackModulesLocked(init api.Module) []api.Module {
	seen := make(map[api.Module]struct{})
	modules := make([]api.Module, 0, 1)
	add := func(mod api.Module) {
		if mod == nil || mod.ExportedFunction("asyncify_get_state") == nil {
			return
		}
		if _, ok := seen[mod]; ok {
			return
		}
		seen[mod] = struct{}{}
		modules = append(modules, mod)
	}
	add(init)
	if i.linkerInst != nil {
		for _, mod := range i.linkerInst.Modules() {
			add(mod)
		}
	}
	return modules
}

func validOwnedStackAllocator(fn api.Function) bool {
	if fn == nil || fn.Definition() == nil {
		return false
	}
	def := fn.Definition()
	params, results := def.ParamTypes(), def.ResultTypes()
	if len(results) != 1 || results[0] != api.ValueTypeI32 || (len(params) != 1 && len(params) != 4) {
		return false
	}
	for _, typ := range params {
		if typ != api.ValueTypeI32 {
			return false
		}
	}
	return true
}

// ownedStackAllocatorLocked proves that allocator calls write the target
// memory. Canonical provenance or a local allocator definition is sufficient
// only when the allocator owner exposes the exact same api.Memory object as the
// target. This permits shared-memory adapters without borrowing another core's
// heap; re-exported imports remain unproven.
func (i *WazeroInstance) ownedStackAllocatorLocked(mod api.Module) (*wazeroAllocator, bool, error) {
	if mod == nil || mod.Memory() == nil {
		return nil, false, fmt.Errorf("asyncify: owned stack requested for core %q, but it has no memory", asyncifyCoreName(mod))
	}
	mem := mod.Memory()
	if i.linkerInst != nil && i.linkerInst.IsModuleAsyncifyMemoryAdded(mod) {
		// The linker proves this core began memoryless and the embedded
		// transformer added its sole memory. No guest data can own an address
		// in it, so the runtime can reserve from address zero without a guest
		// allocator. This exception is never inferred from an export or name.
		return nil, true, nil
	}
	if i.linkerInst != nil && i.module != nil && i.module.canonRegistry != nil {
		for _, lift := range i.module.canonRegistry.AllLifts() {
			exp, ok := i.linkerInst.GetExport(lift.Name)
			if !ok || exp.Canon == nil || exp.Canon.ReallocMod == nil || exp.Canon.Memory != mem || !validOwnedStackAllocator(exp.Canon.Realloc) {
				continue
			}
			// ReallocMod is linker-proven local provenance. Requiring its
			// exported memory to be the target's exact object permits a WASI
			// adapter to use its application's allocator only when both cores
			// genuinely share one linear memory.
			if exp.Canon.ReallocMod.Memory() != mem {
				continue
			}
			if alloc := i.getOrCreateAllocatorLocked(exp.Canon.Realloc, exp.Canon.ReallocMod); alloc != nil {
				return alloc, false, nil
			}
		}
	}
	owners := []api.Module{mod}
	if i.linkerInst != nil {
		owners = i.linkerInst.Modules()
	}
	for _, owner := range owners {
		// A locally defined conventional allocator is sufficient only if its
		// module exposes the exact same memory object. Name matching alone is
		// never provenance: a separate core's heap must not be borrowed.
		if owner == nil || owner.Memory() != mem {
			continue
		}
		for _, name := range []string{CabiRealloc, legacyRealloc, simpleAlloc, "malloc"} {
			if localFunctionOwner(owner, name) != owner {
				continue
			}
			fn := owner.ExportedFunction(name)
			if !validOwnedStackAllocator(fn) {
				continue
			}
			if alloc := i.getOrCreateAllocatorLocked(fn, owner); alloc != nil {
				return alloc, false, nil
			}
		}
	}
	return nil, false, fmt.Errorf("asyncify: owned stack requested for core %q, but no local/canonical allocator is proven for its memory (%s); provide a guest-reserved Asyncify region via AsyncifyConfig.DataAddr", asyncifyCoreName(mod), i.ownedStackAllocatorDiagnosticsLocked(mem))
}

// ownedStackAllocatorDiagnosticsLocked describes only provenance facts used in
// the rejection. It makes adapter failures actionable without weakening the
// exact-memory requirement.
func (i *WazeroInstance) ownedStackAllocatorDiagnosticsLocked(target api.Memory) string {
	owners := []api.Module{i.instance}
	if i.linkerInst != nil {
		owners = i.linkerInst.Modules()
	}
	parts := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner == nil {
			continue
		}
		facts := make([]string, 0, 4)
		for _, name := range []string{CabiRealloc, legacyRealloc, simpleAlloc, "malloc"} {
			fn := owner.ExportedFunction(name)
			if fn == nil {
				continue
			}
			facts = append(facts, fmt.Sprintf("%s(local=%t,signature=%t)", name, localFunctionOwner(owner, name) == owner, validOwnedStackAllocator(fn)))
		}
		if len(facts) == 0 {
			facts = append(facts, "no-conventional-allocator")
		}
		parts = append(parts, fmt.Sprintf("%s(same-memory=%t;%s)", asyncifyCoreName(owner), owner.Memory() == target, strings.Join(facts, ",")))
	}
	if len(parts) == 0 {
		return "no instantiated core modules"
	}
	return strings.Join(parts, " ")
}

func asyncifyCoreName(mod api.Module) string {
	if mod == nil || mod.Name() == "" {
		return "default"
	}
	return mod.Name()
}

// prepareOwnedAsyncifyReservationsLocked performs all ownership checks before
// allocating or writing any Asyncify header. Reservations are retained for the
// instance lifetime and are reused when the same owned configuration is
// re-applied.
func (i *WazeroInstance) prepareOwnedAsyncifyReservationsLocked(ctx context.Context, init api.Module, stackBytes uint32) error {
	if stackBytes == 0 {
		return fmt.Errorf("asyncify: owned stack size must be greater than zero")
	}
	if stackBytes > ^uint32(0)-asyncifyHeaderBytes {
		return fmt.Errorf("asyncify: owned stack size overflows header reservation")
	}
	if i.asyncifyOwnedStackBytes != 0 && i.asyncifyOwnedStackBytes != stackBytes {
		return fmt.Errorf("asyncify: cannot change instance-owned stack size from %d to %d after reservation", i.asyncifyOwnedStackBytes, stackBytes)
	}
	bytes := asyncifyHeaderBytes + stackBytes
	modules := i.asyncifyStackModulesLocked(init)
	candidates := make([]asyncifyReservationCandidate, 0, len(modules))
	for _, mod := range modules {
		if reservation, ok := i.asyncifyReservations[mod]; ok {
			if reservation.bytes != bytes || reservation.memory != mod.Memory() {
				return fmt.Errorf("asyncify: owned stack reservation for core %q is incompatible with requested configuration", asyncifyCoreName(mod))
			}
			continue
		}
		allocator, transformReserved, err := i.ownedStackAllocatorLocked(mod)
		if err != nil {
			return err
		}
		candidates = append(candidates, asyncifyReservationCandidate{module: mod, memory: mod.Memory(), allocator: allocator, transformReserved: transformReserved})
	}
	if i.asyncifyReservations == nil {
		i.asyncifyReservations = make(map[api.Module]asyncifyStackReservation)
	}
	for _, candidate := range candidates {
		var addr uint32
		if candidate.transformReserved {
			// The linker-proven synthetic memory is exclusively ours. Address
			// zero is an explicit reservation origin, not the legacy address-16
			// fallback used by caller-managed Asyncify configurations.
			if err := ensureTransformReservedStackMemory(candidate.memory, bytes); err != nil {
				return fmt.Errorf("asyncify: reserve transform-owned stack for core %q: %w", asyncifyCoreName(candidate.module), err)
			}
			addr = 0
		} else {
			var err error
			addr, err = candidate.allocator.AllocContext(i.prepareCallContext(ctx), bytes, 4)
			if err != nil {
				return fmt.Errorf("asyncify: reserve %d bytes for core %q: %w", bytes, asyncifyCoreName(candidate.module), err)
			}
		}
		if addr%4 != 0 {
			return fmt.Errorf("asyncify: allocator returned unaligned reservation for core %q", asyncifyCoreName(candidate.module))
		}
		if addr > ^uint32(0)-bytes {
			return fmt.Errorf("asyncify: allocator returned overflowing reservation for core %q", asyncifyCoreName(candidate.module))
		}
		// Read proves the allocator returned storage in this exact core memory
		// and that the complete header-plus-stack range is addressable before a
		// later prepareInit can write the header.
		if _, ok := candidate.memory.Read(addr, bytes); !ok {
			return fmt.Errorf("asyncify: owned stack reservation outside core %q memory", asyncifyCoreName(candidate.module))
		}
		i.asyncifyReservations[candidate.module] = asyncifyStackReservation{dataAddr: addr, bytes: bytes, memory: candidate.memory, allocator: candidate.allocator}
	}
	return nil
}

// ensureTransformReservedStackMemory grows only a linker-proven Asyncify-added
// memory. Growth goes through wazero's normal memory allocator, so an active
// MemoryBudget admits and accounts for it just like guest memory.grow.
func ensureTransformReservedStackMemory(memory api.Memory, bytes uint32) error {
	if memory == nil {
		return fmt.Errorf("asyncify: transform-owned reservation has no memory")
	}
	if memory.Size() >= bytes {
		return nil
	}
	missing := uint64(bytes) - uint64(memory.Size())
	pages := uint64(65536)
	delta := uint32((missing + pages - 1) / pages)
	if delta == 0 {
		return fmt.Errorf("asyncify: transform-owned stack growth overflow")
	}
	if _, ok := memory.Grow(delta); !ok {
		return fmt.Errorf("asyncify: transform-owned memory cannot grow by %d pages for stack reservation", delta)
	}
	if memory.Size() < bytes {
		return fmt.Errorf("asyncify: transform-owned memory growth did not provide stack reservation")
	}
	return nil
}

func (i *WazeroInstance) ownedAsyncifyReservationLocked(mod api.Module, stackBytes uint32) (asyncifyStackReservation, error) {
	reservation, ok := i.asyncifyReservations[mod]
	if !ok || reservation.bytes != asyncifyHeaderBytes+stackBytes || reservation.memory != mod.Memory() {
		return asyncifyStackReservation{}, fmt.Errorf("asyncify: missing owned stack reservation for core %q", asyncifyCoreName(mod))
	}
	return reservation, nil
}
