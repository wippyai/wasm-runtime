package ir

import (
	"fmt"
	"slices"
)

// bindSavedCarriers retains every carrier of every enclosing source scope.
// Completed sibling scopes cannot contain the target continuation. While
// rewinding through those scopes, guest writes/branches are guarded; arbitrary
// inactive selectors can only select pure routing toward earlier, skipped calls.
// Guest operand snapshots have independent storage and remain saved separately.
// The unscoped mode is a structural test primitive for this selection rule.
// Production rejects opaque control before this method is reached.
func (s *controlStorage) bindSavedCarriers(analysis *Analysis, lowered *LoweredControl, scoped bool) error {
	if analysis == nil || lowered == nil {
		return fmt.Errorf("asyncify: incomplete saved-carrier inputs")
	}
	if s.source != nil && (s.source.values == nil || s.source.values.control != analysis) {
		return fmt.Errorf("asyncify: normalized source disagrees with saved carriers")
	}
	atCall := make(map[uint64][]uint32)
	all := make(map[uint32]bool)
	var visit func(Node, []uint32) error
	visit = func(node Node, active []uint32) error {
		// The materialized function label is a lowering-only wrapper, so it is
		// intentionally absent from NormalizedSource while remaining active for
		// every suspension nested in the function body.
		if !s.active(node, analysis) {
			return nil
		}
		carriers, ok := s.nodes[node]
		if !ok {
			return fmt.Errorf("asyncify: missing carrier scope")
		}
		own := append([]uint32(nil), carriers.params...)
		own = append(own, carriers.results...)
		own = append(own, carriers.branchValues...)
		if carriers.hasSelector {
			own = append(own, carriers.selector)
		}
		for _, local := range own {
			all[local] = true
		}
		// Each recursive call restores the parent's length. Snapshots at calls own
		// copies, so sibling traversal may reuse the backing capacity safely.
		active = append(active, own...)
		switch n := node.(type) {
		case *SeqNode:
			for _, child := range n.Children {
				if err := visit(child, active); err != nil {
					return err
				}
			}
		case *BlockNode:
			return visit(n.Body, active)
		case *IfNode:
			if err := visit(n.Then, active); err != nil {
				return err
			}
			if n.Else != nil {
				return visit(n.Else, active)
			}
		case *InstrNode:
			suspends := false
			if s.source != nil {
				suspends = s.source.NodeSuspends(n)
			} else if n.callID != 0 && n.callID <= uint64(len(analysis.calls)) {
				suspends = analysis.calls[n.callID-1].suspends
			}
			if n.callID != 0 && suspends {
				atCall[n.callID] = append([]uint32(nil), active...)
			}
		default:
			return fmt.Errorf("asyncify: unknown saved-carrier scope %T", node)
		}
		return nil
	}
	if err := visit(s.root, nil); err != nil {
		return err
	}
	if len(atCall) != len(lowered.suspensions) {
		return fmt.Errorf("asyncify: saved-carrier suspension count mismatch")
	}
	for i := range lowered.suspensions {
		site := &lowered.suspensions[i]
		locals, ok := atCall[site.SourceCallID]
		if !ok {
			return fmt.Errorf("asyncify: suspension without carrier scope")
		}
		if !scoped {
			locals = make([]uint32, 0, len(all))
			for local := range all {
				locals = append(locals, local)
			}
		}
		slices.Sort(locals)
		site.ControlLocals = locals
	}
	return nil
}
