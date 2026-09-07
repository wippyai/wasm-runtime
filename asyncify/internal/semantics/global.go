package semantics

import (
	"fmt"

	"github.com/wippyai/wasm-runtime/wasm"
)

// GlobalOperation describes access to a mutable cell distinct from operand
// snapshots. Resolution never substitutes a type for missing module metadata.
type GlobalOperation struct {
	Index uint32
	Type  wasm.ValType
	Write bool
}

// ResolveGlobal checks the shared imported/defined index space and mutability.
// The returned description owns no module references. The module must belong to
// the instruction's current revision; this is not whole-module validation.
func ResolveGlobal(instr wasm.Instruction, module *wasm.Module) (GlobalOperation, bool, error) {
	if instr.Opcode != wasm.OpGlobalGet && instr.Opcode != wasm.OpGlobalSet {
		return GlobalOperation{}, false, nil
	}
	fail := func(format string, args ...any) (GlobalOperation, bool, error) {
		return GlobalOperation{}, true, fmt.Errorf("asyncify: "+format, args...)
	}
	imm, ok := instr.Imm.(wasm.GlobalImm)
	if !ok {
		return fail("global instruction has invalid immediate %T", instr.Imm)
	}
	if module == nil {
		return fail("global %d requires module metadata", imm.GlobalIdx)
	}
	index := uint64(imm.GlobalIdx)
	var typ *wasm.GlobalType
	for _, imp := range module.Imports {
		if imp.Desc.Kind != wasm.KindGlobal {
			continue
		}
		if index == 0 {
			if imp.Desc.Global == nil {
				return fail("global %d has missing import descriptor", imm.GlobalIdx)
			}
			typ = imp.Desc.Global
			break
		}
		index--
	}
	if typ == nil {
		if index >= uint64(len(module.Globals)) {
			return fail("global index %d out of range", imm.GlobalIdx)
		}
		typ = &module.Globals[index].Type
	}
	write := instr.Opcode == wasm.OpGlobalSet
	if write && !typ.Mutable {
		return fail("global %d is immutable", imm.GlobalIdx)
	}
	return GlobalOperation{Index: imm.GlobalIdx, Type: typ.ValType, Write: write}, true, nil
}
