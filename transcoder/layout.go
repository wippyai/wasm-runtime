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

func (lc *LayoutCalculator) calculateRecord(r *wit.Record) LayoutInfo {
	typedef := &wit.TypeDef{Kind: r}
	return lc.calc.Calculate(typedef)
}

var alignTo = abi.AlignTo
