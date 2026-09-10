package semantics

import (
	"math"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestDirectCallSignatures(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64}},
			{Params: []wasm.ValType{wasm.ValF32, wasm.ValF64}, Results: []wasm.ValType{wasm.ValI32}},
		},
		Imports: []wasm.Import{
			{Desc: wasm.ImportDesc{Kind: wasm.KindFunc, TypeIdx: 0}},
		},
		Funcs: []uint32{1},
	}

	c := NewCalls(m)

	// Imported direct call (funcIdx 0 -> type 0)
	op0, handled, err := c.Resolve(wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
	if err != nil || !handled {
		t.Fatalf("resolve imported call: handled=%v err=%v", handled, err)
	}
	if op0.Kind != DirectCall || op0.TargetIndex != 0 || op0.TypeIndex != 0 || op0.Tail || op0.HasTargetOperand() {
		t.Fatalf("unexpected op0: %+v", op0)
	}
	if op0.ParamCount() != 1 || op0.ParamType(0) != wasm.ValI32 {
		t.Fatalf("unexpected op0 params: count=%d type=%v", op0.ParamCount(), op0.ParamType(0))
	}
	if op0.ResultCount() != 1 || op0.ResultType(0) != wasm.ValI64 {
		t.Fatalf("unexpected op0 results: count=%d type=%v", op0.ResultCount(), op0.ResultType(0))
	}

	// Defined direct call (funcIdx 1 -> type 1)
	op1, handled, err := c.Resolve(wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}})
	if err != nil || !handled {
		t.Fatalf("resolve defined call: handled=%v err=%v", handled, err)
	}
	if op1.Kind != DirectCall || op1.TargetIndex != 1 || op1.TypeIndex != 1 || op1.Tail || op1.HasTargetOperand() {
		t.Fatalf("unexpected op1: %+v", op1)
	}
	if op1.ParamCount() != 2 || op1.ParamType(0) != wasm.ValF32 || op1.ParamType(1) != wasm.ValF64 {
		t.Fatalf("unexpected op1 params: count=%d types=(%v,%v)", op1.ParamCount(), op1.ParamType(0), op1.ParamType(1))
	}
	if op1.ResultCount() != 1 || op1.ResultType(0) != wasm.ValI32 {
		t.Fatalf("unexpected op1 results: count=%d type=%v", op1.ResultCount(), op1.ResultType(0))
	}
}

func TestIndirectRefAndTailVariants(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64}},
			{Params: []wasm.ValType{wasm.ValF32}, Results: []wasm.ValType{wasm.ValF64}},
		},
		Tables: []wasm.TableType{
			{Limits: wasm.Limits{Min: 10}},
			{Limits: wasm.Limits{Min: 20}},
		},
		Funcs: []uint32{0},
	}

	c := NewCalls(m)

	for _, tc := range []struct {
		instr       wasm.Instruction
		kind        CallKind
		targetIndex uint32
		typeIndex   uint32
		tail        bool
		hasTarget   bool
	}{
		{
			instr:       wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
			kind:        DirectCall,
			targetIndex: 0,
			typeIndex:   0,
			tail:        false,
			hasTarget:   false,
		},
		{
			instr:       wasm.Instruction{Opcode: wasm.OpReturnCall, Imm: wasm.CallImm{FuncIdx: 0}},
			kind:        DirectCall,
			targetIndex: 0,
			typeIndex:   0,
			tail:        true,
			hasTarget:   false,
		},
		{
			instr:       wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 1, TableIdx: 1}},
			kind:        IndirectCall,
			targetIndex: 1,
			typeIndex:   1,
			tail:        false,
			hasTarget:   true,
		},
		{
			instr:       wasm.Instruction{Opcode: wasm.OpReturnCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 1}},
			kind:        IndirectCall,
			targetIndex: 1,
			typeIndex:   0,
			tail:        true,
			hasTarget:   true,
		},
		{
			instr:       wasm.Instruction{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 0}},
			kind:        ReferenceCall,
			targetIndex: 0,
			typeIndex:   0,
			tail:        false,
			hasTarget:   true,
		},
		{
			instr:       wasm.Instruction{Opcode: wasm.OpReturnCallRef, Imm: wasm.CallRefImm{TypeIdx: 1}},
			kind:        ReferenceCall,
			targetIndex: 0,
			typeIndex:   1,
			tail:        true,
			hasTarget:   true,
		},
	} {
		op, handled, err := c.Resolve(tc.instr)
		if err != nil || !handled {
			t.Fatalf("resolve opcode %#x: handled=%v err=%v", tc.instr.Opcode, handled, err)
		}
		if op.Kind != tc.kind || op.TargetIndex != tc.targetIndex || op.TypeIndex != tc.typeIndex || op.Tail != tc.tail || op.HasTargetOperand() != tc.hasTarget {
			t.Fatalf("opcode %#x: mismatch got %+v, want kind=%v target=%d type=%d tail=%v hasTarget=%v",
				tc.instr.Opcode, op, tc.kind, tc.targetIndex, tc.typeIndex, tc.tail, tc.hasTarget)
		}
	}
}

func TestBadImmediateNilModuleAndBounds(t *testing.T) {
	m := &wasm.Module{
		Types:  []wasm.FuncType{{Params: []wasm.ValType{wasm.ValI32}}},
		Funcs:  []uint32{0},
		Tables: []wasm.TableType{{Limits: wasm.Limits{Min: 1}}},
	}
	c := NewCalls(m)

	callOpcodes := []byte{
		wasm.OpCall, wasm.OpReturnCall,
		wasm.OpCallIndirect, wasm.OpReturnCallIndirect,
		wasm.OpCallRef, wasm.OpReturnCallRef,
	}

	// Bad immediates
	for _, op := range callOpcodes {
		for _, badImm := range []any{nil, wasm.LocalImm{LocalIdx: 0}, struct{}{}} {
			_, handled, err := c.Resolve(wasm.Instruction{Opcode: op, Imm: badImm})
			if !handled || err == nil {
				t.Fatalf("expected error for opcode %#x with bad immediate %v, got handled=%v err=%v", op, badImm, handled, err)
			}
		}
	}

	// Nil *Calls receiver
	var cNil *Calls
	for _, op := range callOpcodes {
		_, handled, err := cNil.Resolve(wasm.Instruction{Opcode: op, Imm: wasm.CallImm{FuncIdx: 0}})
		if !handled || err == nil {
			t.Fatalf("expected error for nil *Calls opcode %#x", op)
		}
	}
	// Nil *Calls unrelated opcode
	if _, handled, err := cNil.Resolve(wasm.Instruction{Opcode: wasm.OpNop}); handled || err != nil {
		t.Fatalf("expected unrelated op on nil *Calls to return false, nil; got handled=%v err=%v", handled, err)
	}

	// NewCalls(nil)
	cNilMod := NewCalls(nil)
	for _, op := range callOpcodes {
		var imm any
		switch op {
		case wasm.OpCall, wasm.OpReturnCall:
			imm = wasm.CallImm{FuncIdx: 0}
		case wasm.OpCallIndirect, wasm.OpReturnCallIndirect:
			imm = wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}
		case wasm.OpCallRef, wasm.OpReturnCallRef:
			imm = wasm.CallRefImm{TypeIdx: 0}
		}
		_, handled, err := cNilMod.Resolve(wasm.Instruction{Opcode: op, Imm: imm})
		if !handled || err == nil {
			t.Fatalf("expected error for NewCalls(nil) opcode %#x", op)
		}
	}
	if _, handled, err := cNilMod.Resolve(wasm.Instruction{Opcode: wasm.OpNop}); handled || err != nil {
		t.Fatalf("expected unrelated op on NewCalls(nil) to return false, nil; got handled=%v err=%v", handled, err)
	}

	// Out of bounds / MaxUint32 bounds
	for _, tc := range []struct {
		name  string
		instr wasm.Instruction
	}{
		{"maxuint func direct", wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: math.MaxUint32}}},
		{"func bound direct", wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}}},
		{"maxuint table indirect", wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: math.MaxUint32}}},
		{"table bound indirect", wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 1}}},
		{"maxuint type indirect", wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: math.MaxUint32, TableIdx: 0}}},
		{"type bound indirect", wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 1, TableIdx: 0}}},
		{"maxuint type ref", wasm.Instruction{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: math.MaxUint32}}},
		{"type bound ref", wasm.Instruction{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 1}}},
	} {
		_, handled, err := c.Resolve(tc.instr)
		if !handled || err == nil {
			t.Fatalf("%s: expected handled=true and err!=nil, got handled=%v err=%v", tc.name, handled, err)
		}
	}

	// Unrelated opcode
	if _, handled, err := c.Resolve(wasm.Instruction{Opcode: wasm.OpDrop}); handled || err != nil {
		t.Fatalf("unrelated opcode drop handled=%v err=%v", handled, err)
	}
}

func TestNonFunctionGCSignature(t *testing.T) {
	// Type 0: Function type
	// Type 1: Struct type (non-function GC type)
	m := &wasm.Module{
		TypeDefs: []wasm.TypeDef{
			{
				Kind: wasm.TypeDefKindFunc,
				Func: &wasm.FuncType{Params: []wasm.ValType{wasm.ValI32}},
			},
			{
				Kind: wasm.TypeDefKindSub,
				Sub: &wasm.SubType{
					CompType: wasm.CompType{
						Kind:   wasm.CompKindStruct,
						Struct: &wasm.StructType{Fields: []wasm.FieldType{{Type: wasm.StorageType{Kind: wasm.StorageKindVal, ValType: wasm.ValI32}}}},
					},
				},
			},
		},
		Tables: []wasm.TableType{{Limits: wasm.Limits{Min: 1}}},
		Funcs:  []uint32{0, 1}, // Func 1 has invalid type (struct type)
	}

	c := NewCalls(m)

	// Func 0 is valid func
	op0, handled, err := c.Resolve(wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
	if err != nil || !handled || op0.ParamCount() != 1 {
		t.Fatalf("func 0 resolve failed: %v", err)
	}

	// Func 1 points to non-func struct type index 1 -> must fail
	_, handled, err = c.Resolve(wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 1}})
	if !handled || err == nil {
		t.Fatalf("expected error calling function with non-function GC signature, got handled=%v err=%v", handled, err)
	}

	// Call indirect targeting type 1 (struct) -> must fail
	_, handled, err = c.Resolve(wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 1, TableIdx: 0}})
	if !handled || err == nil {
		t.Fatalf("expected error call_indirect on non-function GC signature, got handled=%v err=%v", handled, err)
	}

	// Call ref targeting type 1 (struct) -> must fail
	_, handled, err = c.Resolve(wasm.Instruction{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 1}})
	if !handled || err == nil {
		t.Fatalf("expected error call_ref on non-function GC signature, got handled=%v err=%v", handled, err)
	}
}

func TestModuleMutationOwnership(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{
				Params:  []wasm.ValType{wasm.ValI32},
				Results: []wasm.ValType{wasm.ValI64},
			},
		},
		Funcs:  []uint32{0},
		Tables: []wasm.TableType{{Limits: wasm.Limits{Min: 1}}},
	}

	c := NewCalls(m)

	// Mutate the original module aggressively
	m.Types[0].Params[0] = wasm.ValF32
	m.Types[0].Results[0] = wasm.ValF64
	m.Types = append(m.Types, wasm.FuncType{})
	m.Funcs[0] = 999
	m.Funcs = append(m.Funcs, 0)
	m.Tables = nil

	// Check that snapshot is unaffected
	op, handled, err := c.Resolve(wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
	if err != nil || !handled {
		t.Fatalf("resolve failed after mutation: %v", err)
	}
	if op.ParamCount() != 1 || op.ParamType(0) != wasm.ValI32 {
		t.Fatalf("mutated params leaked into snapshot: got %v", op.ParamType(0))
	}
	if op.ResultCount() != 1 || op.ResultType(0) != wasm.ValI64 {
		t.Fatalf("mutated results leaked into snapshot: got %v", op.ResultType(0))
	}
	if op.TargetIndex != 0 || op.TypeIndex != 0 {
		t.Fatalf("mutated funcToType leaked: target=%d type=%d", op.TargetIndex, op.TypeIndex)
	}

	// Check table count was also snapshotted
	opInd, handled, err := c.Resolve(wasm.Instruction{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}})
	if err != nil || !handled || opInd.TargetIndex != 0 {
		t.Fatalf("table count snapshot mutated: handled=%v err=%v", handled, err)
	}
}

func TestResultAndParamOrder(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{
				Params:  []wasm.ValType{wasm.ValI32, wasm.ValI64, wasm.ValF32, wasm.ValF64},
				Results: []wasm.ValType{wasm.ValF64, wasm.ValF32, wasm.ValI64, wasm.ValI32},
			},
		},
		Funcs: []uint32{0},
	}
	c := NewCalls(m)

	op, handled, err := c.Resolve(wasm.Instruction{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}})
	if err != nil || !handled {
		t.Fatalf("resolve: %v", err)
	}

	if op.ParamCount() != 4 {
		t.Fatalf("param count %d != 4", op.ParamCount())
	}
	if op.ParamType(0) != wasm.ValI32 || op.ParamType(1) != wasm.ValI64 || op.ParamType(2) != wasm.ValF32 || op.ParamType(3) != wasm.ValF64 {
		t.Fatalf("param order mismatch: %v %v %v %v", op.ParamType(0), op.ParamType(1), op.ParamType(2), op.ParamType(3))
	}

	if op.ResultCount() != 4 {
		t.Fatalf("result count %d != 4", op.ResultCount())
	}
	if op.ResultType(0) != wasm.ValF64 || op.ResultType(1) != wasm.ValF32 || op.ResultType(2) != wasm.ValI64 || op.ResultType(3) != wasm.ValI32 {
		t.Fatalf("result order mismatch: %v %v %v %v", op.ResultType(0), op.ResultType(1), op.ResultType(2), op.ResultType(3))
	}
}

func TestZeroAllocationResolve(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.FuncType{
			{Params: []wasm.ValType{wasm.ValI32}, Results: []wasm.ValType{wasm.ValI64}},
		},
		Tables: []wasm.TableType{{Limits: wasm.Limits{Min: 1}}},
		Funcs:  []uint32{0},
	}
	c := NewCalls(m)

	instructions := []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpReturnCall, Imm: wasm.CallImm{FuncIdx: 0}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
		{Opcode: wasm.OpReturnCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 0, TableIdx: 0}},
		{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 0}},
		{Opcode: wasm.OpReturnCallRef, Imm: wasm.CallRefImm{TypeIdx: 0}},
	}

	for _, instr := range instructions {
		var sink CallOperation
		var handled bool
		var err error

		allocs := testing.AllocsPerRun(100, func() {
			sink, handled, err = c.Resolve(instr)
		})
		if allocs != 0 {
			t.Fatalf("opcode %#x: expected 0 allocations, got %f", instr.Opcode, allocs)
		}
		if !handled || err != nil || sink.ParamCount() != 1 {
			t.Fatalf("opcode %#x: unexpected resolution handled=%v err=%v", instr.Opcode, handled, err)
		}
	}
}

func TestZeroValueCallOperation(t *testing.T) {
	var op CallOperation
	if op.ParamCount() != 0 {
		t.Fatalf("expected ParamCount 0, got %d", op.ParamCount())
	}
	if op.ResultCount() != 0 {
		t.Fatalf("expected ResultCount 0, got %d", op.ResultCount())
	}
	if op.HasTargetOperand() {
		t.Fatal("expected HasTargetOperand false for zero value")
	}
}

func TestCallsUseFlatRecursiveTypeIndices(t *testing.T) {
	signature := &wasm.FuncType{Params: []wasm.ValType{wasm.ValI64}, Results: []wasm.ValType{wasm.ValF64}}
	m := &wasm.Module{
		Types: []wasm.FuncType{*signature},
		TypeDefs: []wasm.TypeDef{{Kind: wasm.TypeDefKindRec, Rec: &wasm.RecType{Types: []wasm.SubType{
			{CompType: wasm.CompType{Kind: wasm.CompKindStruct, Struct: &wasm.StructType{}}},
			{CompType: wasm.CompType{Kind: wasm.CompKindFunc, Func: signature}},
		}}}},
		Imports: []wasm.Import{{Desc: wasm.ImportDesc{Kind: wasm.KindTable, Table: &wasm.TableType{ElemType: byte(wasm.ValFuncRef)}}}},
		Funcs:   []uint32{1},
	}
	calls := NewCalls(m)
	for _, instr := range []wasm.Instruction{
		{Opcode: wasm.OpCall, Imm: wasm.CallImm{}},
		{Opcode: wasm.OpCallIndirect, Imm: wasm.CallIndirectImm{TypeIdx: 1}},
		{Opcode: wasm.OpCallRef, Imm: wasm.CallRefImm{TypeIdx: 1}},
	} {
		op, handled, err := calls.Resolve(instr)
		if err != nil || !handled || op.ParamCount() != 1 || op.ParamType(0) != wasm.ValI64 || op.ResultCount() != 1 || op.ResultType(0) != wasm.ValF64 {
			t.Fatalf("flat call type %#x: %+v, %v", instr.Opcode, op, err)
		}
	}
}
