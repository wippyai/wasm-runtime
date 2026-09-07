package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// verifyControlSlots independently checks the assignment against the source
// lifetime event protocol before any physical local is allocated. Every request
// starts and ends once, every active slot has one owner, and types never change.
func verifyControlSlots(requests []carrierRequest, events []storageEvent, assignments [][]int, types []wasm.ValType) error {
	if len(assignments) != len(requests) {
		return fmt.Errorf("asyncify: incomplete control assignments")
	}
	states := make([]uint8, len(requests))
	active := make(map[int]int)
	for _, event := range events {
		if event.request < 0 || event.request >= len(requests) {
			return fmt.Errorf("asyncify: unknown control lifetime request")
		}
		request := requests[event.request]
		slots := assignments[event.request]
		if len(slots) != len(request.types) {
			return fmt.Errorf("asyncify: control carrier arity mismatch")
		}
		if event.release {
			if states[event.request] != 1 {
				return fmt.Errorf("asyncify: release outside control lifetime")
			}
			for _, slot := range slots {
				owner, ok := active[slot]
				if !ok || owner != event.request {
					return fmt.Errorf("asyncify: control slot released by wrong owner")
				}
				delete(active, slot)
			}
			states[event.request] = 2
			continue
		}
		if states[event.request] != 0 {
			return fmt.Errorf("asyncify: repeated control lifetime")
		}
		for i, slot := range slots {
			if slot < 0 || slot >= len(types) || types[slot] != request.types[i] {
				return fmt.Errorf("asyncify: control slot type mismatch")
			}
			if _, ok := active[slot]; ok {
				return fmt.Errorf("asyncify: overlapping control carrier lifetimes")
			}
			active[slot] = event.request
		}
		states[event.request] = 1
	}
	for _, state := range states {
		if state != 2 {
			return fmt.Errorf("asyncify: incomplete control lifetime")
		}
	}
	return nil
}
