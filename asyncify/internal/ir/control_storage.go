package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// controlStorage fixes every carrier required by control lowering before any
// routing instruction is emitted. Carriers belong to source nodes, including
// the materialized function label. Emission cannot allocate additional storage.
// Every carrier lives for its entire source construct. Only completed sibling
// scopes may share slots; ancestors and branch scratch never alias active ports.
// Loads at scope exit transfer results back to operands before a slot is freed.
// Operand snapshots are subsequently owned by the existing value-lowering phase.
type controlStorage struct {
	types      map[uint32]wasm.ValType
	portLocals map[ValueID]uint32
	// source is the optional executable source view used to allocate only
	// reachable control carriers. Nil preserves the structural test path.
	source *NormalizedSource
	root   Node
	nodes  map[Node]controlCarriers
}

type controlCarriers struct {
	params       []uint32
	results      []uint32
	branchValues []uint32
	selector     uint32
	hasSelector  bool
}

type carrierRequest struct {
	owner Node
	types []wasm.ValType
	role  carrierRole
}

type carrierRole uint8

const (
	parameterCarrier carrierRole = iota
	resultCarrier
	selectorCarrier
	branchValueCarrier
)

type storageEvent struct {
	request int
	release bool
}

type controlStorageBuilder struct {
	scopes   map[Node][]int
	analysis *Analysis
	source   *NormalizedSource
	plan     *controlStorage
	targets  map[LabelID][]wasm.ValType
	events   []storageEvent
	requests []carrierRequest
}

// planControlStorage accepts an optional normalized executable source. Without
// it, the legacy structural planning behavior is retained for its callers and
// control-only tests. A normalized source must be derived from this analysis.
func planControlStorage(analysis *Analysis, allocate func(wasm.ValType) uint32, reuse bool, normalized ...*NormalizedSource) (*controlStorage, error) {
	if analysis == nil || allocate == nil {
		return nil, fmt.Errorf("asyncify: incomplete control storage inputs")
	}
	if len(normalized) > 1 {
		return nil, fmt.Errorf("asyncify: multiple normalized sources")
	}
	var source *NormalizedSource
	if len(normalized) == 1 {
		source = normalized[0]
		if source != nil && (source.values == nil || source.values.control != analysis) {
			return nil, fmt.Errorf("asyncify: normalized source disagrees with control analysis")
		}
	}
	root := analysis.root
	hasFunctionBranch := analysis.hasFunctionBranch
	if source != nil {
		hasFunctionBranch = source.HasFunctionBranch()
	}
	if hasFunctionBranch {
		root = &BlockNode{Body: root, ResultTypes: analysis.functionResults, Opcode: wasm.OpBlock, Imm: wasm.BlockImm{Type: -64}}
	}
	b := &controlStorageBuilder{analysis: analysis, source: source, plan: &controlStorage{root: root, source: source, nodes: make(map[Node]controlCarriers), types: make(map[uint32]wasm.ValType)}, targets: make(map[LabelID][]wasm.ValType), scopes: make(map[Node][]int)}
	if err := b.visit(root); err != nil {
		return nil, err
	}
	// Assign abstract typed slots using explicit scope enter/exit events. All
	// source requests are known before the physical allocator is invoked.
	free := make(map[wasm.ValType][]int)
	var slotTypes []wasm.ValType
	assignments := make([][]int, len(b.requests))
	for _, event := range b.events {
		request := b.requests[event.request]
		if event.release {
			if reuse {
				for i, slot := range assignments[event.request] {
					vt := request.types[i]
					free[vt] = append(free[vt], slot)
				}
			}
			continue
		}
		for _, vt := range request.types {
			available := free[vt]
			slot := len(slotTypes)
			if len(available) > 0 {
				slot = available[len(available)-1]
				free[vt] = available[:len(available)-1]
			} else {
				slotTypes = append(slotTypes, vt)
			}
			assignments[event.request] = append(assignments[event.request], slot)
		}
	}
	if err := verifyControlSlots(b.requests, b.events, assignments, slotTypes); err != nil {
		return nil, err
	}
	seen := make(map[uint32]bool)
	physical := make([]uint32, len(slotTypes))
	for i, vt := range slotTypes {
		local := allocate(vt)
		if seen[local] {
			return nil, fmt.Errorf("asyncify: control allocator reused distinct slot at local %d", local)
		}
		seen[local] = true
		physical[i] = local
		b.plan.types[local] = vt
	}
	for index, request := range b.requests {
		locals := make([]uint32, len(request.types))
		for i, slot := range assignments[index] {
			locals[i] = physical[slot]
		}
		carriers := b.plan.nodes[request.owner]
		switch request.role {
		case parameterCarrier:
			carriers.params = locals
		case resultCarrier:
			carriers.results = locals
		case selectorCarrier:
			carriers.selector = locals[0]
			carriers.hasSelector = true
		case branchValueCarrier:
			carriers.branchValues = make([]uint32, len(locals))
			for i, local := range locals {
				carriers.branchValues[len(locals)-1-i] = local
			}
		}
		b.plan.nodes[request.owner] = carriers
	}
	return b.plan, nil
}

func (b *controlStorageBuilder) request(owner Node, role carrierRole, types []wasm.ValType) {
	if len(types) != 0 {
		index := len(b.requests)
		b.requests = append(b.requests, carrierRequest{owner: owner, role: role, types: append([]wasm.ValType(nil), types...)})
		b.events = append(b.events, storageEvent{request: index})
		b.scopes[owner] = append(b.scopes[owner], index)
	}
}

func (b *controlStorageBuilder) active(node Node) bool {
	// A function-label wrapper is lowering-only and deliberately has no source
	// identity. Every retained source node is controlled by normalization.
	if b.source == nil || node == b.plan.root && node != b.analysis.root {
		return true
	}
	return b.source.Active(node)
}

func (s *controlStorage) active(node Node, analysis *Analysis) bool {
	if s.source == nil || node == s.root && s.root != analysis.root {
		return true
	}
	return s.source.Active(node)
}

func (b *controlStorageBuilder) nodeSuspends(node Node) bool {
	if b.source != nil {
		return b.source.NodeSuspends(node)
	}
	return b.analysis.suspends[node]
}

func (b *controlStorageBuilder) visit(node Node) error {
	if !b.active(node) {
		return nil
	}
	b.plan.nodes[node] = controlCarriers{}
	defer func() {
		for _, index := range b.scopes[node] {
			b.events = append(b.events, storageEvent{request: index, release: true})
		}
	}()
	switch n := node.(type) {
	case *SeqNode:
		for _, child := range n.Children {
			if err := b.visit(child); err != nil {
				return err
			}
		}
	case *BlockNode:
		b.request(n, parameterCarrier, n.ParamTypes)
		b.request(n, resultCarrier, n.ResultTypes)
		target := n.ResultTypes
		if n.Opcode == wasm.OpLoop {
			target = n.ParamTypes
		}
		b.targets[n.label] = target
		err := b.visit(n.Body)
		delete(b.targets, n.label)
		return err
	case *IfNode:
		if b.nodeSuspends(n) || len(n.ParamTypes) != 0 {
			b.request(n, selectorCarrier, []wasm.ValType{wasm.ValI32})
		}
		b.request(n, parameterCarrier, n.ParamTypes)
		b.request(n, resultCarrier, n.ResultTypes)
		b.targets[n.label] = n.ResultTypes
		if err := b.visit(n.Then); err != nil {
			return err
		}
		if n.Else != nil {
			if err := b.visit(n.Else); err != nil {
				return err
			}
		}
		delete(b.targets, n.label)
	case *InstrNode:
		switch n.Instr.Opcode {
		case wasm.OpBr, wasm.OpBrIf, wasm.OpBrTable:
			if len(n.branchTargets) == 0 {
				return fmt.Errorf("asyncify: control storage branch without target")
			}
			var targetTypes []wasm.ValType
			for _, target := range n.branchTargets {
				types, ok := b.targets[target]
				if !ok {
					return fmt.Errorf("asyncify: control storage inactive target %d", target)
				}
				if len(targetTypes) == 0 && len(types) != 0 {
					targetTypes = types
				}
			}
			if len(targetTypes) != 0 {
				if n.Instr.Opcode == wasm.OpBrIf || n.Instr.Opcode == wasm.OpBrTable {
					b.request(n, selectorCarrier, []wasm.ValType{wasm.ValI32})
				}
				if n.Instr.Opcode == wasm.OpBrTable {
					reversed := make([]wasm.ValType, len(targetTypes))
					for i, vt := range targetTypes {
						reversed[len(targetTypes)-1-i] = vt
					}
					b.request(n, branchValueCarrier, reversed)
				}
			}
		}
	default:
		return fmt.Errorf("asyncify: unknown control storage node %T", node)
	}
	return nil
}

func (l *linearizer) carriers(node Node) controlCarriers {
	carriers, ok := l.storage.nodes[node]
	if !ok && l.err == nil {
		l.err = fmt.Errorf("asyncify: control node has no storage plan")
	}
	return carriers
}
