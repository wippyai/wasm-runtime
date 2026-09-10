package engine

import "fmt"

// routingBoundary brackets a contiguous sequence of rewind-routing actions.
// Routing may use a temporary suffix, but cannot change or consume the guest
// prefix. The suffix must be empty before the next guest action. The retained
// operands include both source identities and physical snapshots.
type routingBoundary struct {
	prefix []stackEntry
	active bool
}

func (r *routingBoundary) advance(routing bool, length int, at func(int) (stackEntry, bool)) error {
	if r.active {
		if length < len(r.prefix) {
			return fmt.Errorf("asyncify: routing consumed guest stack prefix")
		}
		for i, expected := range r.prefix {
			actual, exists := at(i)
			if !exists || actual != expected {
				return fmt.Errorf("asyncify: routing changed guest operand %d", i)
			}
		}
		if !routing {
			if length != len(r.prefix) {
				return fmt.Errorf("asyncify: routing left %d temporary operands", length-len(r.prefix))
			}
			r.active = false
			r.prefix = r.prefix[:0]
		}
	}
	if routing && !r.active {
		r.prefix = r.prefix[:0]
		for i := 0; i < length; i++ {
			entry, exists := at(i)
			if _, bound := entry.Binding(); !exists || !bound {
				return fmt.Errorf("asyncify: routing entered with unbound guest operand %d", i)
			}
			r.prefix = append(r.prefix, entry)
		}
		r.active = true
	}
	return nil
}
