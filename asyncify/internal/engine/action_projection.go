package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
	"github.com/wippyai/wasm-runtime/wasm"
)

type stepRange struct{ start, end int }

// executionPlan owns the one expansion used for byte-level analyses. Semantic
// simulation/emission use actions, never these steps. A capture's private
// control flow must not appear in the outer dispatcher's control stack.
type executionPlan struct {
	source  *ir.LoweredControl
	actions []ir.Action
	steps   []wasm.Instruction
	ranges  []stepRange
	owners  []int
}

func newExecutionPlan(actions []ir.Action, source ...*ir.LoweredControl) (*executionPlan, error) {
	p := &executionPlan{actions: actions, ranges: make([]stepRange, len(actions))}
	if len(source) > 0 {
		p.source = source[0]
	}
	for index, action := range actions {
		steps, err := action.CopyInstructions()
		if err != nil {
			return nil, fmt.Errorf("asyncify: expand action %d: %w", index, err)
		}
		if _, primitive := action.Primitive(); primitive && len(steps) != 1 {
			return nil, fmt.Errorf("asyncify: action %d has invalid primitive expansion", index)
		}
		p.ranges[index] = stepRange{start: len(p.steps), end: len(p.steps) + len(steps)}
		p.steps = append(p.steps, steps...)
		for range steps {
			p.owners = append(p.owners, index)
		}
	}
	// The final function End belongs to the enclosing engine frame, not an
	// action or source occurrence. Keep it outside all action ranges.
	p.steps = append(p.steps, wasm.Instruction{Opcode: wasm.OpEnd})
	p.owners = append(p.owners, -1)
	return p, nil
}

func (p *executionPlan) sourceStep(action int) (int, error) {
	if action < 0 || action >= len(p.actions) || p.actions[action].Kind() != ir.SourceInstruction {
		return 0, fmt.Errorf("asyncify: call does not refer to a source action")
	}
	r := p.ranges[action]
	if r.end != r.start+1 || p.owners[r.start] != action {
		return 0, fmt.Errorf("asyncify: source call has no unique analysis step")
	}
	return r.start, nil
}

func (ft *FunctionTransformer) closedActionRegions(p *executionPlan, excludedActions map[int]bool) (map[int]int, error) {
	excludedSteps := make(map[int]bool)
	for index, action := range p.actions {
		_, primitive := action.Primitive()
		branch, isBranch := action.Branch()
		copyable := primitive || (isBranch && branch.CopyableInClosedRegion())
		if excludedActions[index] || !copyable {
			for step := p.ranges[index].start; step < p.ranges[index].end; step++ {
				excludedSteps[step] = true
			}
		}
	}
	result := make(map[int]int)
	for start, end := range ft.closedVoidRegionSpans(p.steps, excludedSteps) {
		first, last := p.owners[start], p.owners[end]
		if first < 0 || last < first {
			return nil, fmt.Errorf("asyncify: copyable region has inconsistent action range")
		}
		copyable := true
		for action := first; action <= last; action++ {
			r := p.ranges[action]
			_, primitive := p.actions[action].Primitive()
			branch, isBranch := p.actions[action].Branch()
			copyableAction := primitive || (isBranch && branch.CopyableInClosedRegion())
			if !copyableAction || r.start != start+action-first || r.end != r.start+1 {
				copyable = false
				break
			}
		}
		if copyable {
			result[first] = last
		}
	}
	return result, nil
}
