package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// Continuation is an immutable source operand contract at a suspending call.
// It includes call arguments and any target operand, in bottom-to-top order.
// Its value identities are source definitions, never physical local indices.
type Continuation struct {
	operands []continuationOperand
}

type continuationOperand struct {
	value     ValueID
	literal   semantics.Literal
	valueType wasm.ValType
}

func (c *Continuation) OperandLiteral(index int) (semantics.Literal, bool) {
	literal := c.operands[index].literal
	return literal, literal.Valid()
}

func (c *Continuation) OperandCount() int                  { return len(c.operands) }
func (c *Continuation) OperandType(index int) wasm.ValType { return c.operands[index].valueType }
func (c *Continuation) OperandValue(index int) ValueID     { return c.operands[index].value }

func (p *ValuePlan) bindContinuations(lowered *LoweredControl) error {
	expected := len(p.continuations)
	if lowered.source != nil {
		expected = lowered.source.SuspensionCount()
	}
	if len(lowered.suspensions) != expected {
		return fmt.Errorf("asyncify: source continuation count mismatch")
	}
	seen := make(map[uint64]bool)
	for i := range lowered.suspensions {
		site := &lowered.suspensions[i]
		if lowered.source != nil {
			id, ok := lowered.source.SuspensionID(i)
			if !ok || id != site.SourceCallID {
				return fmt.Errorf("asyncify: normalized continuation identity mismatch")
			}
		}
		if seen[site.SourceCallID] {
			return fmt.Errorf("asyncify: repeated source continuation")
		}
		seen[site.SourceCallID] = true
		values, ok := p.continuations[site.SourceCallID]
		if !ok {
			return fmt.Errorf("asyncify: suspension has no source continuation")
		}
		continuation := &Continuation{operands: make([]continuationOperand, len(values))}
		for j, value := range values {
			var vt wasm.ValType
			if value != 0 {
				if uint64(value) > uint64(len(p.definitions)) {
					return fmt.Errorf("asyncify: continuation references undefined source value")
				}
				vt = p.definitions[value-1].Type
			}
			literal := p.literals[value]
			if literal.Valid() && literal.Type() != vt {
				return fmt.Errorf("asyncify: continuation literal type disagrees with source value")
			}
			continuation.operands[j] = continuationOperand{value: value, valueType: vt, literal: literal}
		}
		site.Operands = continuation
	}
	return nil
}
