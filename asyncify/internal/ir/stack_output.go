package ir

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

type StackOutputKind uint8

const (
	invalidStackOutput StackOutputKind = iota
	MaterializedOutput
	ForwardedOutput
	ValidationOutput
)

// StackOutput describes how an action may produce a declared source value.
// Forwarding names a present input. Materialization reads a source definition
// or port into an unbound operand. Validation has no runtime storage.
type StackOutput struct {
	input     int
	valueType wasm.ValType
	kind      StackOutputKind
}

func (o StackOutput) Kind() StackOutputKind   { return o.kind }
func (o StackOutput) Type() wasm.ValType      { return o.valueType }
func (o StackOutput) InputIndex() (int, bool) { return o.input, o.kind == ForwardedOutput }
func (c StackContract) Output(index int) (StackOutput, bool) {
	if index < 0 || index >= len(c.outputProvenance) {
		return StackOutput{}, false
	}
	return c.outputProvenance[index], true
}

func (l *LoweredControl) StackContract(index int) (StackContract, error) {
	c, err := l.rawStackContract(index)
	if err != nil || !c.Checked() {
		return c, err
	}
	c.outputProvenance = make([]StackOutput, len(c.outputs))
	for i, value := range c.outputs {
		output := StackOutput{kind: MaterializedOutput}
		if value == 0 {
			output.kind = ValidationOutput
		} else {
			if uint64(value) > uint64(len(l.values.definitions)) {
				return StackContract{}, fmt.Errorf("asyncify: output %d has undefined source value %d", i, value)
			}
			definition := l.values.definitions[value-1]
			output.valueType = definition.Type
			if definition.ValidationOnly {
				output.kind = ValidationOutput
			} else {
				for input, source := range c.inputs {
					if value == source {
						output.kind, output.input = ForwardedOutput, input
						break
					}
				}
			}
		}
		c.outputProvenance[i] = output
	}
	return c, nil
}
