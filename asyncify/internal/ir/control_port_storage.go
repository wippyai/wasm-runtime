package ir

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// bindSourcePorts connects immutable source ports to their assigned control
// carriers. A reused physical local can serve distinct non-overlapping scopes;
// the source port identities remain distinct. This is an assignment contract,
// not a claim that a carrier currently holds its port value on every path.
func (s *controlStorage) bindSourcePorts(values *ValuePlan) error {
	if values == nil || values.control == nil {
		return fmt.Errorf("asyncify: missing source ports")
	}
	if s.source != nil && s.source.values != values {
		return fmt.Errorf("asyncify: normalized source disagrees with source ports")
	}
	hasFunctionBranch := values.control.hasFunctionBranch
	if s.source != nil {
		hasFunctionBranch = s.source.HasFunctionBranch()
	}
	if hasFunctionBranch {
		wrapper, ok := s.root.(*BlockNode)
		if !ok || wrapper.Body != values.control.root || wrapper.label != 0 || wrapper.Opcode != wasm.OpBlock || wrapper.Imm.Type != -64 || len(wrapper.ParamTypes) != 0 || !slices.Equal(wrapper.ResultTypes, values.control.functionResults) {
			return fmt.Errorf("asyncify: function carrier wrapper disagrees with source scope")
		}
	} else if s.root != values.control.root {
		return fmt.Errorf("asyncify: unexpected materialized function carrier")
	}
	bindings := make(map[ValueID]uint32)
	for label, scope := range values.scopes {
		if s.source != nil && !s.source.Active(scope.owner) {
			continue
		}
		owner := scope.owner
		if label == 0 {
			if s.root == values.control.root {
				continue
			} // No materialized function label.
			owner = s.root
		}
		carriers, ok := s.nodes[owner]
		if !ok {
			return fmt.Errorf("asyncify: scope %d has no carrier assignment", label)
		}
		for _, group := range []struct {
			ports  []ValueID
			locals []uint32
		}{{scope.params, carriers.params}, {scope.results, carriers.results}} {
			if len(group.ports) != len(group.locals) {
				return fmt.Errorf("asyncify: scope %d carrier arity disagrees with source ports", label)
			}
			for index, id := range group.ports {
				info, defined := values.ValueInfo(id)
				local := group.locals[index]
				storedType, assigned := s.types[local]
				if !defined || !assigned || info.Type != storedType {
					return fmt.Errorf("asyncify: source port %d carrier type mismatch", id)
				}
				if _, duplicate := bindings[id]; duplicate {
					return fmt.Errorf("asyncify: source port %d assigned more than once", id)
				}
				bindings[id] = local
			}
		}
	}
	s.portLocals = bindings
	return nil
}

// PortStorage returns the assigned local for a materialized source control port.
// It says nothing about the value currently held by that local: typed transfer
// actions must establish that binding on each path before a read.
func (l *LoweredControl) PortStorage(port ValueID) (uint32, bool) {
	local, ok := l.portLocals[port]
	return local, ok
}
