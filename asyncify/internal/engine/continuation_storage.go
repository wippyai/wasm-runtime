package engine

import "fmt"

// continuationStorage owns the exact operand snapshots selected for each
// suspension. Frame selection and emission verification consume the same frozen
// records; source identity equality alone cannot hide a changed physical slot.
type continuationStorage struct {
	operands map[int][]stackEntry
	required map[int][]uint32
	sealed   bool
}

func newContinuationStorage(sites []CallSite) (*continuationStorage, error) {
	p := &continuationStorage{operands: make(map[int][]stackEntry), required: make(map[int][]uint32)}
	for _, site := range sites {
		_, duplicate := p.required[site.ActionIndex]
		if site.ActionIndex < 0 || duplicate {
			return nil, fmt.Errorf("asyncify: invalid or duplicate continuation action %d", site.ActionIndex)
		}
		p.required[site.ActionIndex] = append(append([]uint32(nil), site.LiveLocals...), site.ControlLocals...)
	}
	return p, nil
}

func (p *continuationStorage) capture(action int, operands []stackEntry) error {
	_, expected := p.required[action]
	if p.sealed || !expected {
		return fmt.Errorf("asyncify: unexpected continuation storage capture %d", action)
	}
	if _, exists := p.operands[action]; exists {
		return fmt.Errorf("asyncify: continuation storage captured twice at %d", action)
	}
	for i, operand := range operands {
		_, stored := operand.LocalIndex()
		_, literal := operand.Literal()
		if _, bound := operand.Binding(); !bound || (!stored && !literal) {
			return fmt.Errorf("asyncify: continuation %d operand %d has no bound runtime value", action, i)
		}
	}
	p.operands[action] = append([]stackEntry(nil), operands...)
	return nil
}

func (p *continuationStorage) seal() error {
	for action := range p.required {
		if _, exists := p.operands[action]; !exists {
			return fmt.Errorf("asyncify: missing continuation storage at action %d", action)
		}
	}
	p.sealed = true
	return nil
}

func (p *continuationStorage) savedLocals() (map[uint32]bool, error) {
	if !p.sealed {
		return nil, fmt.Errorf("asyncify: continuation storage is not sealed")
	}
	locals := make(map[uint32]bool)
	for _, required := range p.required {
		for _, local := range required {
			locals[local] = true
		}
	}
	for _, operands := range p.operands {
		for _, operand := range operands {
			if local, stored := operand.LocalIndex(); stored {
				locals[local] = true
			}
		}
	}
	return locals, nil
}

func (p *continuationStorage) verify(action, length int, at func(int) (stackEntry, bool)) error {
	expected, exists := p.operands[action]
	if !p.sealed || !exists || length != len(expected) {
		return fmt.Errorf("asyncify: continuation %d storage shape differs from simulation", action)
	}
	for i, planned := range expected {
		actual, exists := at(i)
		if !exists || actual != planned {
			return fmt.Errorf("asyncify: continuation %d operand %d storage differs from simulation", action, i)
		}
	}
	return nil
}
