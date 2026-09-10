package engine

import (
	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

type temporaryPool struct {
	domain    semantics.ExecutionDomain
	valueType wasm.ValType
}

// temporaryStorage owns slots, definitions and reference counts. It knows
// nothing about guest cells, instructions, branches or materialization plans.
// References include operands and caller-owned control snapshots. Zero references
// permits overwrite; it does not erase the resident definition.
type temporaryStorage struct {
	pools          map[temporaryPool][]uint32
	refCount       map[uint32]int
	localTypes     map[uint32]wasm.ValType
	values         map[uint32]semantics.Value
	domains        map[uint32]semantics.ExecutionDomain
	nextValueID    uint64
	firstTempLocal uint32
	nextFreshLocal uint32
}

func newTemporaryStorage(first uint32) *temporaryStorage {
	return &temporaryStorage{
		pools:          make(map[temporaryPool][]uint32),
		refCount:       make(map[uint32]int),
		localTypes:     make(map[uint32]wasm.ValType),
		values:         make(map[uint32]semantics.Value),
		domains:        make(map[uint32]semantics.ExecutionDomain),
		firstTempLocal: first, nextFreshLocal: first,
	}
}

func (s *temporaryStorage) define(vt wasm.ValType) semantics.Value {
	return s.defineInDomain(vt, semantics.GuestExecution)
}

func (s *temporaryStorage) defineInDomain(vt wasm.ValType, domain semantics.ExecutionDomain) semantics.Value {
	pool := temporaryPool{domain: domain, valueType: vt}
	for _, idx := range s.pools[pool] {
		if s.refCount[idx] == 0 {
			return s.replace(idx, vt)
		}
	}
	idx := s.nextFreshLocal
	s.nextFreshLocal++
	s.pools[pool] = append(s.pools[pool], idx)
	s.localTypes[idx] = vt
	s.domains[idx] = domain
	return s.replace(idx, vt)
}

func (s *temporaryStorage) replace(idx uint32, vt wasm.ValType) semantics.Value {
	s.nextValueID++
	value := semantics.Value{ID: s.nextValueID, LocalIdx: idx, Type: vt}
	s.values[idx] = value
	s.refCount[idx] = 1
	return value
}

func (s *temporaryStorage) retainValue(value semantics.Value) bool {
	if value.ID == 0 || s.values[value.LocalIdx] != value {
		return false
	}
	return s.retain(value.LocalIdx)
}

func (s *temporaryStorage) retain(idx uint32) bool {
	if _, ok := s.values[idx]; !ok {
		return false
	}
	s.refCount[idx]++
	return true
}

func (s *temporaryStorage) release(idx uint32) bool {
	if s.refCount[idx] <= 0 {
		return false
	}
	s.refCount[idx]--
	return true
}
