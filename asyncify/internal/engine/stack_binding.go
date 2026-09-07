package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/ir"
)

// stackBindingStep brackets one semantic action in both simulation and emission.
// It holds no parallel stack: tokens live on the actual operand entries.
type stackBindingStep struct {
	contract ir.StackContract
	before   int
	action   int
}

func matchStackValue(expected ir.ValueID, index int, at func(int) (stackEntry, bool)) error {
	entry, exists := at(index)
	actual, bound := entry.Binding()
	if !exists || !bound || actual != uint64(expected) {
		return fmt.Errorf("asyncify: stack operand %d source identity %d (bound=%t), expected %d", index, actual, bound, expected)
	}
	return nil
}
func (s *stackBindingStep) begin(contract ir.StackContract, action, length int, at func(int) (stackEntry, bool)) error {
	s.contract = contract
	s.action = action
	s.before = length
	if !contract.Checked() {
		return nil
	}
	if length < contract.InputCount()+contract.PrefixCount() {
		return fmt.Errorf("asyncify: action %d source operands exceed stack", action)
	}
	for i := 0; i < contract.PrefixCount(); i++ {
		if err := matchStackValue(contract.PrefixValue(i), i, at); err != nil {
			return fmt.Errorf("action %d prefix: %w", action, err)
		}
	}
	for i := 0; i < contract.InputCount(); i++ {
		if err := matchStackValue(contract.InputValue(i), length-contract.InputCount()+i, at); err != nil {
			return fmt.Errorf("action %d input: %w", action, err)
		}
	}
	return nil
}
func (s *stackBindingStep) finish(length int, at func(int) (stackEntry, bool), bind func(int, uint64) error) error {
	c := s.contract
	if !c.Checked() {
		return nil
	}
	base := s.before - c.InputCount()
	if c.TruncatesToPrefix() {
		base = c.PrefixCount()
	}
	if length != base+c.OutputCount() {
		return fmt.Errorf("asyncify: action %d stack effect leaves %d operands, expected %d", s.action, length, base+c.OutputCount())
	}
	for i := 0; i < c.PrefixCount(); i++ {
		if err := matchStackValue(c.PrefixValue(i), i, at); err != nil {
			return fmt.Errorf("action %d retained prefix: %w", s.action, err)
		}
	}
	for i := 0; i < c.OutputCount(); i++ {
		output, declared := c.Output(i)
		entry, exists := at(base + i)
		if !declared || !exists {
			return fmt.Errorf("asyncify: action %d output %d has no provenance or operand", s.action, i)
		}
		if output.Kind() == ir.ForwardedOutput {
			input, ok := output.InputIndex()
			if !ok || input < 0 || input >= c.InputCount() || c.InputValue(input) != c.OutputValue(i) {
				return fmt.Errorf("asyncify: action %d output %d has invalid forwarding input", s.action, i)
			}
		}
		if err := verifyStackOutput(output, c.OutputValue(i), entry); err != nil {
			return fmt.Errorf("asyncify: action %d output %d: %w", s.action, i, err)
		}
	}
	// Validate the entire output tuple before assigning any source identities.
	for i := 0; i < c.OutputCount(); i++ {
		if err := bind(base+i, uint64(c.OutputValue(i))); err != nil {
			return err
		}
	}
	s.contract = ir.StackContract{}
	return nil
}

// verifyStackOutput checks whether assigning the declared source identity is
// authorized by the source output's provenance. Forwarding must preserve the
// consumed input token even when materializing a new physical snapshot.
func verifyStackOutput(output ir.StackOutput, value ir.ValueID, entry stackEntry) error {
	if output.Type() != 0 && entry.Type() != output.Type() {
		return fmt.Errorf("type %v disagrees with declared %v", entry.Type(), output.Type())
	}
	token, bound := entry.Binding()
	switch output.Kind() {
	case ir.MaterializedOutput:
		if bound {
			return fmt.Errorf("new output already carries source identity %d", token)
		}
	case ir.ForwardedOutput:
		if !bound || token != uint64(value) {
			return fmt.Errorf("forwarded output lacks input source identity (token=%d, bound=%t)", token, bound)
		}
	case ir.ValidationOutput:
		if !entry.ValidationOnly() {
			return fmt.Errorf("validation output has runtime storage")
		}
		if bound && token != uint64(value) {
			return fmt.Errorf("validation output carries unrelated source identity %d", token)
		}
		return nil
	default:
		return fmt.Errorf("unknown output provenance")
	}
	if entry.ValidationOnly() {
		return fmt.Errorf("runtime output is validation-only")
	}
	_, stored := entry.LocalIndex()
	_, literal := entry.Literal()
	if !stored && !literal {
		return fmt.Errorf("runtime output is absent")
	}
	return nil
}
