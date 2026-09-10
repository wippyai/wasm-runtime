package engine

import (
	"fmt"
	"math"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// Frame sizes are encoded as positive i32 constants by the current protocol.
const maxAsyncifyFrameSize = math.MaxInt32

// frameSlot identifies a saved value and its location in the continuation frame.
// Slots are copied from analysis inputs; those inputs can subsequently change
// without altering the plan consumed by save and restore emission.
type frameSlot struct {
	localIdx uint32
	offset   uint32
	valType  wasm.ValType
}

// framePlan is constructed once after saved values have been selected. It must
// not be mutated or reused after rewriting local identities. Its owned slots
// preserve the existing ABI: call index at byte 0, saved values from byte 4 in
// ascending local order. Planning validates the layout before emission starts.
type framePlan struct {
	slots     []frameSlot
	frameSize uint32
}

func newFramePlan(localTypes []wasm.ValType, liveUnion map[uint32]bool) (*framePlan, error) {
	indices := make([]uint32, 0, len(liveUnion))
	for idx := range liveUnion {
		indices = append(indices, idx)
	}
	slices.Sort(indices)
	plan := &framePlan{frameSize: 4, slots: make([]frameSlot, 0, len(indices))}
	for _, idx := range indices {
		if uint64(idx) >= uint64(len(localTypes)) {
			return nil, fmt.Errorf("asyncify: saved local index %d out of range", idx)
		}
		vt := localTypes[idx]
		if !CanStoreToMemory(vt) {
			return nil, fmt.Errorf("asyncify: saved local %d has non-storable type %s", idx, vt)
		}
		// The validated type has a positive width. Widen before adding so an
		// oversized layout cannot wrap before the representability check.
		next := uint64(plan.frameSize) + uint64(ValTypeSize(vt))
		if next > maxAsyncifyFrameSize {
			return nil, fmt.Errorf("asyncify: frame size %d exceeds maximum %d", next, maxAsyncifyFrameSize)
		}
		plan.slots = append(plan.slots, frameSlot{localIdx: idx, valType: vt, offset: plan.frameSize})
		plan.frameSize = uint32(next)
	}
	return plan, nil
}
