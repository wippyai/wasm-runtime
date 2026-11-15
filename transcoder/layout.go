package transcoder

import (
	"github.com/wippyai/wasm-runtime/transcoder/internal/abi"
	"github.com/wippyai/wasm-runtime/transcoder/internal/layout"
	"go.bytecodealliance.org/wit"
)

type LayoutInfo = layout.Info

type LayoutCalculator struct {
	calc *layout.Calculator
}

func NewLayoutCalculator() *LayoutCalculator {
	return &LayoutCalculator{
		calc: layout.NewCalculator(),
	}
}

func (lc *LayoutCalculator) Calculate(t wit.Type) LayoutInfo {
	return lc.calc.Calculate(t)
}

// Sequence lays out types as consecutive tuple elements, the canonical ABI
// layout of a function's results in a return area. It returns the offset of
// each element and the layout of the whole sequence.
func (lc *LayoutCalculator) Sequence(types []wit.Type) ([]uint32, LayoutInfo) {
	return lc.calc.Sequence(types)
}

func (lc *LayoutCalculator) calculateRecord(r *wit.Record) LayoutInfo {
	typedef := &wit.TypeDef{Kind: r}
	return lc.calc.Calculate(typedef)
}

var alignTo = abi.AlignTo
