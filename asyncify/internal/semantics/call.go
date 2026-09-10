package semantics

import (
	"fmt"
	"slices"

	"github.com/wippyai/wasm-runtime/wasm"
)

// CallKind distinguishes direct, indirect, and reference calls.
type CallKind uint8

const (
	DirectCall CallKind = iota
	IndirectCall
	ReferenceCall
)

type privateSignature struct {
	params  []wasm.ValType
	results []wasm.ValType
}

// CallOperation describes a resolved call instruction. It is immutable and owns
// no mutable module references.
type CallOperation struct {
	signature   *privateSignature
	Kind        CallKind
	TargetIndex uint32
	TypeIndex   uint32
	Tail        bool
}

// ParamCount returns the number of parameter operands expected by the callee.
func (op CallOperation) ParamCount() int {
	if op.signature == nil {
		return 0
	}
	return len(op.signature.params)
}

// ParamType returns the parameter type at index i. Callers must only index within
// [0, ParamCount()).
func (op CallOperation) ParamType(i int) wasm.ValType {
	return op.signature.params[i]
}

// ResultCount returns the number of results produced by the callee.
func (op CallOperation) ResultCount() int {
	if op.signature == nil {
		return 0
	}
	return len(op.signature.results)
}

// ResultType returns the result type at index i. Callers must only index within
// [0, ResultCount()).
func (op CallOperation) ResultType(i int) wasm.ValType {
	return op.signature.results[i]
}

// HasTargetOperand reports whether the call takes a dynamic target operand from
// the stack (e.g. table element index for indirect calls or funcref for call_ref).
func (op CallOperation) HasTargetOperand() bool {
	return op.Kind == IndirectCall || op.Kind == ReferenceCall
}

// Calls holds an immutable, frozen snapshot of module function-to-type mappings,
// cloned signatures, and table count.
type Calls struct {
	funcToType []uint32
	signatures []*privateSignature
	tableCount uint64
}

// NewCalls creates an immutable snapshot of module call metadata. Parameters and
// results are cloned once per unique type definition, not per function or lookup.
// Mutating the module after NewCalls does not affect call resolution.
func NewCalls(m *wasm.Module) *Calls {
	if m == nil {
		return &Calls{}
	}

	numTypes := m.NumTypes()
	sigs := make([]*privateSignature, numTypes)
	m.RangeFunctionTypes(func(index uint32, ft *wasm.FuncType) bool {
		if ft != nil {
			sigs[index] = &privateSignature{params: slices.Clone(ft.Params), results: slices.Clone(ft.Results)}
		}
		return true
	})

	numImported := m.NumImportedFuncs()
	funcToType := make([]uint32, 0, numImported+len(m.Funcs))
	for _, imp := range m.Imports {
		if imp.Desc.Kind == wasm.KindFunc {
			funcToType = append(funcToType, imp.Desc.TypeIdx)
		}
	}
	funcToType = append(funcToType, m.Funcs...)

	tableCount := uint64(m.NumImportedTables()) + uint64(len(m.Tables))

	return &Calls{
		funcToType: funcToType,
		signatures: sigs,
		tableCount: tableCount,
	}
}

// Resolve resolves call instructions against the frozen module snapshot.
// The boolean flag distinguishes unrelated opcodes from call family opcodes.
// Unrelated opcodes return (CallOperation{}, false, nil) even with a nil context.
func (c *Calls) Resolve(instr wasm.Instruction) (CallOperation, bool, error) {
	kind, tail, handled := classifyCall(instr.Opcode)
	if !handled {
		return CallOperation{}, false, nil
	}

	if c == nil {
		return CallOperation{}, true, fmt.Errorf("asyncify: call instruction requires module calls metadata")
	}

	switch kind {
	case DirectCall:
		imm, ok := instr.Imm.(wasm.CallImm)
		if !ok {
			return CallOperation{}, true, fmt.Errorf("asyncify: call instruction has invalid immediate %T", instr.Imm)
		}
		if uint64(imm.FuncIdx) >= uint64(len(c.funcToType)) {
			return CallOperation{}, true, fmt.Errorf("asyncify: call function index %d out of range", imm.FuncIdx)
		}
		typeIdx := c.funcToType[imm.FuncIdx]
		if uint64(typeIdx) >= uint64(len(c.signatures)) || c.signatures[typeIdx] == nil {
			return CallOperation{}, true, fmt.Errorf("asyncify: call function %d has missing or invalid signature for type index %d", imm.FuncIdx, typeIdx)
		}
		return CallOperation{
			Kind:        DirectCall,
			TargetIndex: imm.FuncIdx,
			TypeIndex:   typeIdx,
			Tail:        tail,
			signature:   c.signatures[typeIdx],
		}, true, nil

	case IndirectCall:
		imm, ok := instr.Imm.(wasm.CallIndirectImm)
		if !ok {
			return CallOperation{}, true, fmt.Errorf("asyncify: call indirect instruction has invalid immediate %T", instr.Imm)
		}
		if uint64(imm.TableIdx) >= c.tableCount {
			return CallOperation{}, true, fmt.Errorf("asyncify: call indirect table index %d out of range", imm.TableIdx)
		}
		if uint64(imm.TypeIdx) >= uint64(len(c.signatures)) || c.signatures[imm.TypeIdx] == nil {
			return CallOperation{}, true, fmt.Errorf("asyncify: call indirect type index %d out of range or missing signature", imm.TypeIdx)
		}
		return CallOperation{
			Kind:        IndirectCall,
			TargetIndex: imm.TableIdx,
			TypeIndex:   imm.TypeIdx,
			Tail:        tail,
			signature:   c.signatures[imm.TypeIdx],
		}, true, nil

	case ReferenceCall:
		imm, ok := instr.Imm.(wasm.CallRefImm)
		if !ok {
			return CallOperation{}, true, fmt.Errorf("asyncify: call ref instruction has invalid immediate %T", instr.Imm)
		}
		if uint64(imm.TypeIdx) >= uint64(len(c.signatures)) || c.signatures[imm.TypeIdx] == nil {
			return CallOperation{}, true, fmt.Errorf("asyncify: call ref type index %d out of range or missing signature", imm.TypeIdx)
		}
		return CallOperation{
			Kind:        ReferenceCall,
			TargetIndex: 0,
			TypeIndex:   imm.TypeIdx,
			Tail:        tail,
			signature:   c.signatures[imm.TypeIdx],
		}, true, nil
	}

	return CallOperation{}, false, nil
}

// IsCallInstruction recognizes the call family using the same classification as
// signature resolution. It does not validate immediates or resolve a target.
func IsCallInstruction(opcode byte) bool {
	_, _, handled := classifyCall(opcode)
	return handled
}

func classifyCall(opcode byte) (CallKind, bool, bool) {
	switch opcode {
	case wasm.OpCall:
		return DirectCall, false, true
	case wasm.OpReturnCall:
		return DirectCall, true, true
	case wasm.OpCallIndirect:
		return IndirectCall, false, true
	case wasm.OpReturnCallIndirect:
		return IndirectCall, true, true
	case wasm.OpCallRef:
		return ReferenceCall, false, true
	case wasm.OpReturnCallRef:
		return ReferenceCall, true, true
	default:
		return 0, false, false
	}
}
