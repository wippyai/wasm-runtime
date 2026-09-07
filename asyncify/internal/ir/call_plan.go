package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
)

// LoweredControl transfers owned instruction storage and checked suspension
// positions to the next phase. Calls retain their original source identities.
type LoweredControl struct {
	source         *NormalizedSource
	portLocals     map[ValueID]uint32
	selectorLocals map[LabelID]uint32
	branchCarriers map[*InstrNode]branchCarrierAllocation
	values         *ValuePlan
	branchDepths   map[*InstrNode][]uint32
	actions        []Action
	suspensions    []SuspensionSite
	stateGlobal    uint32
	stateRewinding int32
	// rootMaterialized records the one lowering-only control frame: a wrapper
	// for the implicit function label when a source branch targets label zero.
	// It is retained so source-action coverage can distinguish its result
	// transfer from direct function completion, which has no carrier write.
	rootMaterialized bool
}

// branchCarrierAllocation is the fixed storage assigned to a source branch.
// It is retained independently from the action so byte projections cannot
// substitute arbitrary selector or table-value cells.
type branchCarrierAllocation struct {
	branchValues []uint32
	selector     uint32
	hasSelector  bool
}

type SuspensionSite struct {
	Operands      *Continuation
	ControlLocals []uint32
	Call          semantics.CallOperation
	ActionIndex   int
	SourceCallID  uint64
}

type sourceCall struct {
	operation semantics.CallOperation
	suspends  bool
}

// bindLoweredCalls consumes recorded source occurrences after verifying the
// complete origin stream. Suspension policy is never re-run or inferred from
// generated opcodes.
func (a *Analysis) bindLoweredCalls(output *loweringOutput, values *ValuePlan, storage *controlStorage, config *LinearizeConfig) (*LoweredControl, error) {
	if output.err != nil {
		return nil, output.err
	}
	result := &LoweredControl{actions: output.actions, values: values, rootMaterialized: storage != nil && storage.root != a.root, branchDepths: make(map[*InstrNode][]uint32, len(output.branchDepths))}
	if storage != nil {
		result.source = storage.source
	}
	for node, depths := range output.branchDepths {
		result.branchDepths[node] = append([]uint32(nil), depths...)
	}
	for _, action := range output.actions {
		if action.kind != SourceBranch {
			continue
		}
		if storage == nil || action.origin.source == nil {
			return nil, fmt.Errorf("asyncify: source branch has no storage allocation")
		}
		if _, duplicate := result.branchCarriers[action.origin.source]; duplicate {
			return nil, fmt.Errorf("asyncify: duplicate source branch allocation")
		}
		carriers, exists := storage.nodes[action.origin.source]
		if !exists {
			return nil, fmt.Errorf("asyncify: source branch has no storage allocation")
		}
		if result.branchCarriers == nil {
			result.branchCarriers = make(map[*InstrNode]branchCarrierAllocation)
		}
		result.branchCarriers[action.origin.source] = branchCarrierAllocation{
			branchValues: append([]uint32(nil), carriers.branchValues...),
			selector:     carriers.selector,
			hasSelector:  carriers.hasSelector,
		}
	}
	if values != nil {
		result.portLocals = storage.portLocals
		result.selectorLocals = make(map[LabelID]uint32)
		for label, scope := range values.scopes {
			if scope.kind != IfScope {
				continue
			}
			if carriers, ok := storage.nodes[scope.owner]; ok && carriers.hasSelector {
				result.selectorLocals[label] = carriers.selector
			}
		}
		result.stateGlobal = config.StateGlobal
		result.stateRewinding = config.StateRewinding
	}
	if err := a.verifyOrigins(result); err != nil {
		return nil, err
	}
	calls := 0
	var previousID uint64
	expectedCalls := len(a.calls)
	if result.source != nil {
		expectedCalls = 0
		for _, node := range result.source.instructions {
			if node.callID != 0 {
				expectedCalls++
			}
		}
	}
	for index, action := range output.actions {
		if _, primitive := action.Primitive(); !primitive {
			continue
		}
		origin := action.origin
		if origin.source == nil || origin.source.callID == 0 {
			continue
		}
		id := origin.source.callID
		if id <= previousID || id > uint64(len(a.calls)) || (result.source == nil && id != uint64(calls)+1) {
			return nil, fmt.Errorf("asyncify: inconsistent source call occurrence")
		}
		previousID = id
		call := a.calls[id-1]
		if call.suspends {
			result.suspensions = append(result.suspensions, SuspensionSite{Call: call.operation, ActionIndex: index, SourceCallID: id})
		}
		calls++
	}
	if calls != expectedCalls {
		return nil, fmt.Errorf("asyncify: source calls omitted during lowering")
	}
	return result, nil
}
