package runtime

import (
	"context"
	"fmt"

	"github.com/wippyai/wasm-runtime/engine"
)

// CallSession wraps engine.CallSession for step-based async execution.
type CallSession struct {
	session *engine.CallSession
}

// Step advances execution. Pass nil on first call, then resume with YieldResult.
func (cs *CallSession) Step(ctx context.Context, yr *engine.YieldResult) (engine.StepResult, error) {
	if cs == nil || cs.session == nil {
		return engine.StepResult{}, fmt.Errorf("call session is nil")
	}
	return cs.session.Step(ctx, yr)
}

// LiftResult decodes raw wasm results into Go values.
func (cs *CallSession) LiftResult(ctx context.Context, rawResults []uint64) (any, error) {
	if cs == nil || cs.session == nil {
		return nil, fmt.Errorf("call session is nil")
	}
	return cs.session.LiftResult(ctx, rawResults)
}
