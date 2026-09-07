package engine

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/asyncify/internal/semantics"
	"github.com/wippyai/wasm-runtime/wasm"
)

// validateInputIdentities runs before any module mutation. Section references
// and global/function instruction identities must refer to original guest
// objects, never to objects that a later lowering stage happens to generate.
// This is deliberately not a complete Wasm operand/control-flow validator.
func validateInputIdentities(m *wasm.Module) error {
	if err := m.Validate(); err != nil {
		return err
	}
	calls := semantics.NewCalls(m)
	numFuncs := uint64(m.NumImportedFuncs()) + uint64(len(m.Funcs))
	checkFunction := func(index uint32) error {
		if uint64(index) >= numFuncs {
			return fmt.Errorf("function index %d out of range", index)
		}
		return nil
	}
	checkExpression := func(code []byte) error {
		instrs, err := wasm.DecodeInstructions(code)
		if err != nil {
			return err
		}
		for _, instr := range instrs {
			switch imm := instr.Imm.(type) {
			case wasm.BlockImm:
				if _, err := semantics.ResolveBlockType(imm.Type, m); err != nil {
					return err
				}
			case wasm.TryTableImm:
				if _, err := semantics.ResolveBlockType(imm.BlockType, m); err != nil {
					return err
				}
			}
			if _, handled, err := semantics.ResolveGlobal(instr, m); handled {
				if err != nil {
					return err
				}
				continue
			}
			if _, handled, err := calls.Resolve(instr); handled {
				if err != nil {
					return err
				}
				continue
			}
			// ref.func shares the original function space, but is not a call.
			if imm, ok := instr.Imm.(wasm.RefFuncImm); ok {
				if err := checkFunction(imm.FuncIdx); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visitModuleExpressions(m, checkExpression)
}

// visitModuleExpressions visits every stored executable/constant expression.
// Keep traversal separate from instruction semantics so future identity checks
// and remapping passes cannot silently omit element initializers or offsets.
// Visitors must treat the bytes and module as read-only.
func visitModuleExpressions(m *wasm.Module, visit func([]byte) error) error {
	numImported := m.NumImportedFuncs()
	for i := range m.Code {
		if err := visit(m.Code[i].Code); err != nil {
			return fmt.Errorf("func %d: %w", numImported+i, err)
		}
	}
	for i := range m.Globals {
		if err := visit(m.Globals[i].Init); err != nil {
			return fmt.Errorf("global %d initializer: %w", i, err)
		}
	}
	for i := range m.Tables {
		if m.Tables[i].Init != nil {
			if err := visit(m.Tables[i].Init); err != nil {
				return fmt.Errorf("table %d initializer: %w", i, err)
			}
		}
	}
	for i := range m.Elements {
		elem := &m.Elements[i]
		if elem.Offset != nil {
			if err := visit(elem.Offset); err != nil {
				return fmt.Errorf("element %d offset: %w", i, err)
			}
		}
		for j, expr := range elem.Exprs {
			if err := visit(expr); err != nil {
				return fmt.Errorf("element %d initializer %d: %w", i, j, err)
			}
		}
	}
	for i := range m.Data {
		if m.Data[i].Offset != nil {
			if err := visit(m.Data[i].Offset); err != nil {
				return fmt.Errorf("data %d offset: %w", i, err)
			}
		}
	}
	return nil
}
