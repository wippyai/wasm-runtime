package wasm

import (
	"bytes"
	"fmt"
)

// Opcode constants are defined in constants.go

// Instruction represents a decoded WebAssembly instruction
type Instruction struct {
	Imm    interface{}
	Opcode byte
}

// BlockImm holds the block type for block, loop, if, and try instructions.
type BlockImm struct {
	Type int32 // Block type: -64=void, -1=i32, -2=i64, -3=f32, -4=f64, >=0=type index
}

// BranchImm holds the label index for br and br_if instructions.
type BranchImm struct {
	LabelIdx uint32
}

// BrTableImm holds the label table for br_table instruction.
type BrTableImm struct {
	Labels  []uint32
	Default uint32
}

// CallImm holds the function index for call instruction.
type CallImm struct {
	FuncIdx uint32
}

// CallIndirectImm holds type and table indices for call_indirect instruction.
type CallIndirectImm struct {
	TypeIdx  uint32
	TableIdx uint32
}

// LocalImm holds the local index for local.get, local.set, local.tee.
type LocalImm struct {
	LocalIdx uint32
}

// GlobalImm holds the global index for global.get and global.set.
type GlobalImm struct {
	GlobalIdx uint32
}

// MemoryImm holds memory access parameters for load and store instructions.
type MemoryImm struct {
	Offset uint64
	Align  uint32
	MemIdx uint32
}

// MemoryIdxImm holds memory index for memory.size, memory.grow
type MemoryIdxImm struct {
	MemIdx uint32
}

// I32Imm holds the constant value for i32.const instruction.
type I32Imm struct {
	Value int32
}

// I64Imm holds the constant value for i64.const instruction.
type I64Imm struct {
	Value int64
}

// F32Imm holds the constant value for f32.const instruction.
type F32Imm struct {
	Value float32
}

// F64Imm holds the constant value for f64.const instruction.
type F64Imm struct {
	Value float64
}

// MiscImm holds the sub-opcode and immediates for 0xFC prefix instructions
type MiscImm struct {
	Operands  []uint32
	SubOpcode uint32
}

// TableImm holds table index for table.get/table.set
type TableImm struct {
	TableIdx uint32
}

// RefNullImm holds the heap type for ref.null
type RefNullImm struct {
	HeapType int64 // Heap type encoded as s33 (funcref=0x70/-16, externref=0x6F/-17, or type index)
}

// RefFuncImm holds the function index for ref.func
type RefFuncImm struct {
	FuncIdx uint32
}

// SelectTypeImm holds value types for typed select
type SelectTypeImm struct {
	Types    []ValType    // Simple types for backward compatibility
	ExtTypes []ExtValType // Extended types for GC reference support
}

// SIMDImm holds SIMD instruction immediates
type SIMDImm struct {
	MemArg    *MemoryImm
	LaneIdx   *byte
	V128Bytes []byte
	SubOpcode uint32
}

// AtomicImm holds atomic instruction immediates
type AtomicImm struct {
	MemArg    *MemoryImm
	SubOpcode uint32
}

// GCImm holds GC instruction immediates for struct/array/ref operations
type GCImm struct {
	SubOpcode uint32
	TypeIdx   uint32 // For struct.new, array.new, etc.
	FieldIdx  uint32 // For struct.get/set
	TypeIdx2  uint32 // Second type for array.copy
	DataIdx   uint32 // For array.new_data, array.init_data
	ElemIdx   uint32 // For array.new_elem, array.init_elem
	Size      uint32 // For array.new_fixed
	LabelIdx  uint32 // For br_on_cast
	HeapType  int64  // For ref.test, ref.cast (s33 per spec)
	HeapType2 int64  // Second heap type for br_on_cast
	CastFlags byte   // For br_on_cast variants
}

// ThrowImm holds tag index for throw instruction
type ThrowImm struct {
	TagIdx uint32
}

// CallRefImm holds type index for call_ref and return_call_ref
type CallRefImm struct {
	TypeIdx uint32
}

// CatchClause represents a single catch clause in try_table
type CatchClause struct {
	Kind     byte   // 0=catch, 1=catch_ref, 2=catch_all, 3=catch_all_ref
	TagIdx   uint32 // Only for Kind 0, 1
	LabelIdx uint32
}

// TryTableImm holds immediates for try_table instruction
type TryTableImm struct {
	Catches   []CatchClause
	BlockType int32
}

// GetCallTarget returns the call target if this is a call instruction
func (i Instruction) GetCallTarget() (uint32, bool) {
	if i.Opcode == OpCall {
		if imm, ok := i.Imm.(CallImm); ok {
			return imm.FuncIdx, true
		}
	}
	return 0, false
}

// IsIndirectCall returns true if this is a call_indirect instruction
func (i Instruction) IsIndirectCall() bool {
	return i.Opcode == OpCallIndirect
}

// DecodeInstructions decodes a sequence of instructions from raw bytes
func DecodeInstructions(code []byte) ([]Instruction, error) {
	r := bytes.NewReader(code)
	// Pre-allocate based on estimation: roughly 2 bytes per instruction on average
	instrs := make([]Instruction, 0, len(code)/2)

	for r.Len() > 0 {
		op, err := r.ReadByte()
		if err != nil {
			break
		}

		instr := Instruction{Opcode: op}

		switch op {
		case OpBlock, OpLoop, OpIf, OpTry:
			bt, err := ReadLEB128s(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = BlockImm{Type: bt}

		case OpCatch:
			tagIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = ThrowImm{TagIdx: tagIdx}

		case OpThrow:
			tagIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = ThrowImm{TagIdx: tagIdx}

		case OpRethrow, OpDelegate:
			labelIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = BranchImm{LabelIdx: labelIdx}

		case OpTryTable:
			bt, err := ReadLEB128s(r)
			if err != nil {
				return nil, err
			}
			catchCount, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			catches := make([]CatchClause, catchCount)
			for i := uint32(0); i < catchCount; i++ {
				kind, err := r.ReadByte()
				if err != nil {
					return nil, err
				}
				var tagIdx uint32
				if kind == CatchKindCatch || kind == CatchKindCatchRef {
					tagIdx, err = ReadLEB128u(r)
					if err != nil {
						return nil, err
					}
				}
				labelIdx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				catches[i] = CatchClause{Kind: kind, TagIdx: tagIdx, LabelIdx: labelIdx}
			}
			instr.Imm = TryTableImm{BlockType: bt, Catches: catches}

		case OpBr, OpBrIf:
			idx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = BranchImm{LabelIdx: idx}

		case OpBrTable:
			count, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			labels := make([]uint32, count)
			for i := uint32(0); i < count; i++ {
				labels[i], err = ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
			}
			def, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = BrTableImm{Labels: labels, Default: def}

		case OpCall, OpReturnCall:
			idx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = CallImm{FuncIdx: idx}

		case OpCallIndirect, OpReturnCallIndirect:
			typeIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			tableIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = CallIndirectImm{TypeIdx: typeIdx, TableIdx: tableIdx}

		case OpCallRef, OpReturnCallRef:
			typeIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = CallRefImm{TypeIdx: typeIdx}

		case OpLocalGet, OpLocalSet, OpLocalTee:
			idx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = LocalImm{LocalIdx: idx}

		case OpGlobalGet, OpGlobalSet:
			idx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = GlobalImm{GlobalIdx: idx}

		case OpTableGet, OpTableSet:
			idx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = TableImm{TableIdx: idx}

		case OpI32Load, OpI64Load, OpF32Load, OpF64Load,
			OpI32Load8S, OpI32Load8U, OpI32Load16S, OpI32Load16U,
			OpI64Load8S, OpI64Load8U, OpI64Load16S, OpI64Load16U, OpI64Load32S, OpI64Load32U,
			OpI32Store, OpI64Store, OpF32Store, OpF64Store,
			OpI32Store8, OpI32Store16, OpI64Store8, OpI64Store16, OpI64Store32:
			memImm, err := readMemArg(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = memImm

		case OpMemorySize, OpMemoryGrow:
			// Memory index (0 for single memory, can be non-zero for multi-memory)
			memIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = MemoryIdxImm{MemIdx: memIdx}

		case OpI32Const:
			val, err := ReadLEB128s(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = I32Imm{Value: val}

		case OpI64Const:
			val, err := ReadLEB128s64(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = I64Imm{Value: val}

		case OpF32Const:
			val, err := ReadFloat32(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = F32Imm{Value: val}

		case OpF64Const:
			val, err := ReadFloat64(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = F64Imm{Value: val}

		case OpRefNull:
			heapType, err := ReadLEB128s64(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = RefNullImm{HeapType: heapType}

		case OpRefFunc:
			funcIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = RefFuncImm{FuncIdx: funcIdx}

		case OpBrOnNull, OpBrOnNonNull:
			labelIdx, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = BranchImm{LabelIdx: labelIdx}

		case OpSelectType:
			count, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			types := make([]ValType, count)
			extTypes := make([]ExtValType, count)
			hasExtTypes := false
			for i := uint32(0); i < count; i++ {
				t, err := r.ReadByte()
				if err != nil {
					return nil, err
				}
				types[i] = ValType(t)
				if t == byte(ValRefNull) || t == byte(ValRef) {
					heapType, err := ReadLEB128s64(r)
					if err != nil {
						return nil, err
					}
					extTypes[i] = ExtValType{
						Kind:    ExtValKindRef,
						ValType: ValType(t),
						RefType: RefType{Nullable: t == byte(ValRefNull), HeapType: heapType},
					}
					hasExtTypes = true
				} else {
					extTypes[i] = ExtValType{Kind: ExtValKindSimple, ValType: ValType(t)}
				}
			}
			imm := SelectTypeImm{Types: types}
			if hasExtTypes {
				imm.ExtTypes = extTypes
			}
			instr.Imm = imm

		// Instructions with no immediates - do nothing
		case OpUnreachable, OpNop, OpElse, OpEnd, OpReturn, OpDrop, OpSelect, OpRefIsNull,
			OpRefAsNonNull, OpRefEq, OpCatchAll, OpThrowRef,
			OpI32Eqz, OpI32Eq, OpI32Ne, OpI32LtS, OpI32LtU, OpI32GtS, OpI32GtU,
			OpI32LeS, OpI32LeU, OpI32GeS, OpI32GeU,
			OpI64Eqz, OpI64Eq, OpI64Ne, OpI64LtS, OpI64LtU, OpI64GtS, OpI64GtU,
			OpI64LeS, OpI64LeU, OpI64GeS, OpI64GeU,
			OpF32Eq, OpF32Ne, OpF32Lt, OpF32Gt, OpF32Le, OpF32Ge,
			OpF64Eq, OpF64Ne, OpF64Lt, OpF64Gt, OpF64Le, OpF64Ge,
			OpI32Clz, OpI32Ctz, OpI32Popcnt, OpI32Add, OpI32Sub, OpI32Mul,
			OpI32DivS, OpI32DivU, OpI32RemS, OpI32RemU, OpI32And, OpI32Or, OpI32Xor,
			OpI32Shl, OpI32ShrS, OpI32ShrU, OpI32Rotl, OpI32Rotr,
			OpI64Clz, OpI64Ctz, OpI64Popcnt, OpI64Add, OpI64Sub, OpI64Mul,
			OpI64DivS, OpI64DivU, OpI64RemS, OpI64RemU, OpI64And, OpI64Or, OpI64Xor,
			OpI64Shl, OpI64ShrS, OpI64ShrU, OpI64Rotl, OpI64Rotr,
			OpF32Abs, OpF32Neg, OpF32Ceil, OpF32Floor, OpF32Trunc, OpF32Nearest, OpF32Sqrt,
			OpF32Add, OpF32Sub, OpF32Mul, OpF32Div, OpF32Min, OpF32Max, OpF32Copysign,
			OpF64Abs, OpF64Neg, OpF64Ceil, OpF64Floor, OpF64Trunc, OpF64Nearest, OpF64Sqrt,
			OpF64Add, OpF64Sub, OpF64Mul, OpF64Div, OpF64Min, OpF64Max, OpF64Copysign,
			OpI32WrapI64, OpI32TruncF32S, OpI32TruncF32U, OpI32TruncF64S, OpI32TruncF64U,
			OpI64ExtendI32S, OpI64ExtendI32U, OpI64TruncF32S, OpI64TruncF32U,
			OpI64TruncF64S, OpI64TruncF64U,
			OpF32ConvertI32S, OpF32ConvertI32U, OpF32ConvertI64S, OpF32ConvertI64U, OpF32DemoteF64,
			OpF64ConvertI32S, OpF64ConvertI32U, OpF64ConvertI64S, OpF64ConvertI64U, OpF64PromoteF32,
			OpI32ReinterpretF32, OpI64ReinterpretF64, OpF32ReinterpretI32, OpF64ReinterpretI64,
			OpI32Extend8S, OpI32Extend16S, OpI64Extend8S, OpI64Extend16S, OpI64Extend32S:
			// No immediate

		case OpPrefixMisc:
			subOp, err := ReadLEB128u(r)
			if err != nil {
				return nil, err
			}
			imm := MiscImm{SubOpcode: subOp}
			switch subOp {
			case MiscI32TruncSatF32S, MiscI32TruncSatF32U,
				MiscI32TruncSatF64S, MiscI32TruncSatF64U,
				MiscI64TruncSatF32S, MiscI64TruncSatF32U,
				MiscI64TruncSatF64S, MiscI64TruncSatF64U:
				// Saturating truncations: no additional operands
			case MiscMemoryInit:
				dataidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				memidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{dataidx, memidx}
			case MiscDataDrop:
				dataidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{dataidx}
			case MiscMemoryCopy:
				dstMem, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				srcMem, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{dstMem, srcMem}
			case MiscMemoryFill:
				memIdx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{memIdx}
			case MiscTableInit:
				elemidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				tableidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{elemidx, tableidx}
			case MiscElemDrop:
				elemidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{elemidx}
			case MiscTableCopy:
				dst, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				src, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{dst, src}
			case MiscTableGrow, MiscTableSize, MiscTableFill:
				tableidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{tableidx}
			case MiscMemoryDiscard:
				memidx, err := ReadLEB128u(r)
				if err != nil {
					return nil, err
				}
				imm.Operands = []uint32{memidx}
			default:
				return nil, fmt.Errorf("unknown 0xFC sub-opcode: 0x%02x", subOp)
			}
			instr.Imm = imm

		case OpPrefixSIMD:
			imm, err := decodeSIMDImmediate(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = imm

		case OpPrefixAtomic:
			imm, err := decodeAtomicImmediate(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = imm

		case OpPrefixGC:
			imm, err := decodeGCImmediate(r)
			if err != nil {
				return nil, err
			}
			instr.Imm = imm

		default:
			return nil, fmt.Errorf("unknown opcode: 0x%02x", op)
		}

		instrs = append(instrs, instr)
	}

	return instrs, nil
}

// EncodeInstructionTo writes a single instruction to the provided buffer.
// This avoids allocations compared to EncodeInstructions for single instructions.
func EncodeInstructionTo(buf *bytes.Buffer, instr *Instruction) {
	buf.WriteByte(instr.Opcode)

	switch instr.Opcode {
	case OpBlock, OpLoop, OpIf, OpTry:
		imm := instr.Imm.(BlockImm)
		WriteLEB128s(buf, imm.Type)

	case OpCatch, OpThrow:
		imm := instr.Imm.(ThrowImm)
		WriteLEB128u(buf, imm.TagIdx)

	case OpRethrow, OpDelegate:
		imm := instr.Imm.(BranchImm)
		WriteLEB128u(buf, imm.LabelIdx)

	case OpTryTable:
		imm := instr.Imm.(TryTableImm)
		WriteLEB128s(buf, imm.BlockType)
		WriteLEB128u(buf, uint32(len(imm.Catches)))
		for _, c := range imm.Catches {
			buf.WriteByte(c.Kind)
			if c.Kind == CatchKindCatch || c.Kind == CatchKindCatchRef {
				WriteLEB128u(buf, c.TagIdx)
			}
			WriteLEB128u(buf, c.LabelIdx)
		}

	case OpBr, OpBrIf:
		imm := instr.Imm.(BranchImm)
		WriteLEB128u(buf, imm.LabelIdx)

	case OpBrTable:
		imm := instr.Imm.(BrTableImm)
		WriteLEB128u(buf, uint32(len(imm.Labels)))
		for _, l := range imm.Labels {
			WriteLEB128u(buf, l)
		}
		WriteLEB128u(buf, imm.Default)

	case OpCall, OpReturnCall:
		imm := instr.Imm.(CallImm)
		WriteLEB128u(buf, imm.FuncIdx)

	case OpCallIndirect, OpReturnCallIndirect:
		imm := instr.Imm.(CallIndirectImm)
		WriteLEB128u(buf, imm.TypeIdx)
		WriteLEB128u(buf, imm.TableIdx)

	case OpCallRef, OpReturnCallRef:
		imm := instr.Imm.(CallRefImm)
		WriteLEB128u(buf, imm.TypeIdx)

	case OpLocalGet, OpLocalSet, OpLocalTee:
		imm := instr.Imm.(LocalImm)
		WriteLEB128u(buf, imm.LocalIdx)

	case OpGlobalGet, OpGlobalSet:
		imm := instr.Imm.(GlobalImm)
		WriteLEB128u(buf, imm.GlobalIdx)

	case OpTableGet, OpTableSet:
		imm := instr.Imm.(TableImm)
		WriteLEB128u(buf, imm.TableIdx)

	case OpI32Load, OpI64Load, OpF32Load, OpF64Load,
		OpI32Load8S, OpI32Load8U, OpI32Load16S, OpI32Load16U,
		OpI64Load8S, OpI64Load8U, OpI64Load16S, OpI64Load16U, OpI64Load32S, OpI64Load32U,
		OpI32Store, OpI64Store, OpF32Store, OpF64Store,
		OpI32Store8, OpI32Store16, OpI64Store8, OpI64Store16, OpI64Store32:
		imm := instr.Imm.(MemoryImm)
		writeMemArg(buf, imm)

	case OpMemorySize, OpMemoryGrow:
		imm := instr.Imm.(MemoryIdxImm)
		WriteLEB128u(buf, imm.MemIdx)

	case OpI32Const:
		imm := instr.Imm.(I32Imm)
		WriteLEB128s(buf, imm.Value)

	case OpI64Const:
		imm := instr.Imm.(I64Imm)
		WriteLEB128s64(buf, imm.Value)

	case OpF32Const:
		imm := instr.Imm.(F32Imm)
		WriteFloat32(buf, imm.Value)

	case OpF64Const:
		imm := instr.Imm.(F64Imm)
		WriteFloat64(buf, imm.Value)

	case OpRefNull:
		imm := instr.Imm.(RefNullImm)
		WriteLEB128s64(buf, imm.HeapType)

	case OpRefFunc:
		imm := instr.Imm.(RefFuncImm)
		WriteLEB128u(buf, imm.FuncIdx)

	case OpBrOnNull, OpBrOnNonNull:
		imm := instr.Imm.(BranchImm)
		WriteLEB128u(buf, imm.LabelIdx)

	case OpSelectType:
		imm := instr.Imm.(SelectTypeImm)
		if len(imm.ExtTypes) > 0 {
			WriteLEB128u(buf, uint32(len(imm.ExtTypes)))
			for _, t := range imm.ExtTypes {
				if t.Kind == ExtValKindRef {
					if t.RefType.Nullable {
						buf.WriteByte(byte(ValRefNull))
					} else {
						buf.WriteByte(byte(ValRef))
					}
					WriteLEB128s64(buf, t.RefType.HeapType)
				} else {
					buf.WriteByte(byte(t.ValType))
				}
			}
		} else {
			WriteLEB128u(buf, uint32(len(imm.Types)))
			for _, t := range imm.Types {
				buf.WriteByte(byte(t))
			}
		}

	case OpPrefixMisc:
		imm := instr.Imm.(MiscImm)
		WriteLEB128u(buf, imm.SubOpcode)
		switch imm.SubOpcode {
		case MiscI32TruncSatF32S, MiscI32TruncSatF32U,
			MiscI32TruncSatF64S, MiscI32TruncSatF64U,
			MiscI64TruncSatF32S, MiscI64TruncSatF32U,
			MiscI64TruncSatF64S, MiscI64TruncSatF64U:
			// No additional operands
		case MiscMemoryInit:
			WriteLEB128u(buf, imm.Operands[0]) // dataidx
			WriteLEB128u(buf, imm.Operands[1]) // memidx
		case MiscDataDrop:
			WriteLEB128u(buf, imm.Operands[0])
		case MiscMemoryCopy:
			WriteLEB128u(buf, imm.Operands[0]) // dst memory
			WriteLEB128u(buf, imm.Operands[1]) // src memory
		case MiscMemoryFill:
			WriteLEB128u(buf, imm.Operands[0]) // memory index
		case MiscTableInit:
			WriteLEB128u(buf, imm.Operands[0]) // elemidx
			WriteLEB128u(buf, imm.Operands[1]) // tableidx
		case MiscElemDrop:
			WriteLEB128u(buf, imm.Operands[0])
		case MiscTableCopy:
			WriteLEB128u(buf, imm.Operands[0]) // dst
			WriteLEB128u(buf, imm.Operands[1]) // src
		case MiscTableGrow, MiscTableSize, MiscTableFill:
			WriteLEB128u(buf, imm.Operands[0])
		case MiscMemoryDiscard:
			WriteLEB128u(buf, imm.Operands[0])
		}

	case OpPrefixSIMD:
		encodeSIMDImmediate(buf, instr.Imm.(SIMDImm))

	case OpPrefixAtomic:
		encodeAtomicImmediate(buf, instr.Imm.(AtomicImm))

	case OpPrefixGC:
		encodeGCImmediate(buf, instr.Imm.(GCImm))
	}
}

// EncodeInstructionsTo writes multiple instructions to the provided buffer.
func EncodeInstructionsTo(buf *bytes.Buffer, instrs []Instruction) {
	for i := range instrs {
		EncodeInstructionTo(buf, &instrs[i])
	}
}

// EncodeInstructions encodes instructions to bytes
func EncodeInstructions(instrs []Instruction) []byte {
	var buf bytes.Buffer
	buf.Grow(len(instrs) * 3) // estimate 3 bytes per instruction
	EncodeInstructionsTo(&buf, instrs)
	return buf.Bytes()
}

func decodeSIMDImmediate(r *bytes.Reader) (SIMDImm, error) {
	subOp, err := ReadLEB128u(r)
	if err != nil {
		return SIMDImm{}, err
	}

	imm := SIMDImm{SubOpcode: subOp}

	switch {
	case subOp <= SimdV128Load64Splat || subOp == SimdV128Store:
		// Basic memory operations: memarg
		memArg, err := readMemArg(r)
		if err != nil {
			return SIMDImm{}, err
		}
		imm.MemArg = &memArg

	case subOp == SimdV128Const:
		// 16 bytes constant
		raw := make([]byte, 16)
		for i := 0; i < 16; i++ {
			b, err := r.ReadByte()
			if err != nil {
				return SIMDImm{}, err
			}
			raw[i] = b
		}
		imm.V128Bytes = raw

	case subOp == SimdI8x16Shuffle:
		// 16 lane indices
		raw := make([]byte, 16)
		for i := 0; i < 16; i++ {
			b, err := r.ReadByte()
			if err != nil {
				return SIMDImm{}, err
			}
			raw[i] = b
		}
		imm.V128Bytes = raw

	case subOp >= SimdI8x16ExtractLaneS && subOp <= SimdF64x2ReplaceLane:
		// Lane extract/replace: lane index (1 byte)
		b, err := r.ReadByte()
		if err != nil {
			return SIMDImm{}, err
		}
		imm.LaneIdx = &b

	case subOp >= SimdV128Load8Lane && subOp <= SimdV128Store64Lane:
		// Lane load/store: memarg + laneidx
		memArg, err := readMemArg(r)
		if err != nil {
			return SIMDImm{}, err
		}
		imm.MemArg = &memArg
		b, err := r.ReadByte()
		if err != nil {
			return SIMDImm{}, err
		}
		imm.LaneIdx = &b

	case subOp == SimdV128Load32Zero || subOp == SimdV128Load64Zero:
		// Zero-extending loads: memarg only
		memArg, err := readMemArg(r)
		if err != nil {
			return SIMDImm{}, err
		}
		imm.MemArg = &memArg

	default:
		// Most SIMD instructions have no immediates
	}

	return imm, nil
}

func encodeSIMDImmediate(buf *bytes.Buffer, imm SIMDImm) {
	WriteLEB128u(buf, imm.SubOpcode)

	if imm.MemArg != nil {
		writeMemArg(buf, *imm.MemArg)
	}
	if len(imm.V128Bytes) > 0 {
		buf.Write(imm.V128Bytes)
	}
	if imm.LaneIdx != nil {
		buf.WriteByte(*imm.LaneIdx)
	}
}

func decodeAtomicImmediate(r *bytes.Reader) (AtomicImm, error) {
	subOp, err := ReadLEB128u(r)
	if err != nil {
		return AtomicImm{}, err
	}

	imm := AtomicImm{SubOpcode: subOp}

	if subOp == AtomicFence {
		// atomic.fence has a single reserved byte
		if _, err := r.ReadByte(); err != nil {
			return AtomicImm{}, err
		}
	} else {
		// All other atomic ops have memarg
		memArg, err := readMemArg(r)
		if err != nil {
			return AtomicImm{}, err
		}
		imm.MemArg = &memArg
	}

	return imm, nil
}

func encodeAtomicImmediate(buf *bytes.Buffer, imm AtomicImm) {
	WriteLEB128u(buf, imm.SubOpcode)

	if imm.SubOpcode == AtomicFence {
		buf.WriteByte(0) // reserved byte
		return
	}

	if imm.MemArg != nil {
		writeMemArg(buf, *imm.MemArg)
	}
}

func decodeGCImmediate(r *bytes.Reader) (GCImm, error) {
	subOp, err := ReadLEB128u(r)
	if err != nil {
		return GCImm{}, err
	}

	imm := GCImm{SubOpcode: subOp}

	switch subOp {
	case GCStructNew, GCStructNewDefault:
		// typeidx
		imm.TypeIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCStructGet, GCStructGetS, GCStructGetU, GCStructSet:
		// typeidx, fieldidx
		imm.TypeIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}
		imm.FieldIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCArrayNew, GCArrayNewDefault, GCArrayGet, GCArrayGetS, GCArrayGetU,
		GCArraySet, GCArrayFill:
		// typeidx
		imm.TypeIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCArrayNewFixed:
		// typeidx, size
		imm.TypeIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}
		imm.Size, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCArrayNewData, GCArrayInitData:
		// typeidx, dataidx
		imm.TypeIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}
		imm.DataIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCArrayNewElem, GCArrayInitElem:
		// typeidx, elemidx
		imm.TypeIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}
		imm.ElemIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCArrayCopy:
		// typeidx, typeidx
		imm.TypeIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}
		imm.TypeIdx2, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCRefTest, GCRefTestNull, GCRefCast, GCRefCastNull:
		// heaptype (s33)
		imm.HeapType, err = ReadLEB128s64(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCBrOnCast, GCBrOnCastFail:
		// castflags, labelidx, heaptype, heaptype
		flags, err := r.ReadByte()
		if err != nil {
			return GCImm{}, err
		}
		imm.CastFlags = flags
		imm.LabelIdx, err = ReadLEB128u(r)
		if err != nil {
			return GCImm{}, err
		}
		imm.HeapType, err = ReadLEB128s64(r)
		if err != nil {
			return GCImm{}, err
		}
		imm.HeapType2, err = ReadLEB128s64(r)
		if err != nil {
			return GCImm{}, err
		}

	case GCArrayLen, GCAnyConvertExtern, GCExternConvertAny,
		GCRefI31, GCI31GetS, GCI31GetU:
		// No immediates

	default:
		return GCImm{}, fmt.Errorf("unknown 0xFB sub-opcode: 0x%02x", subOp)
	}

	return imm, nil
}

func encodeGCImmediate(buf *bytes.Buffer, imm GCImm) {
	WriteLEB128u(buf, imm.SubOpcode)

	switch imm.SubOpcode {
	case GCStructNew, GCStructNewDefault:
		WriteLEB128u(buf, imm.TypeIdx)

	case GCStructGet, GCStructGetS, GCStructGetU, GCStructSet:
		WriteLEB128u(buf, imm.TypeIdx)
		WriteLEB128u(buf, imm.FieldIdx)

	case GCArrayNew, GCArrayNewDefault, GCArrayGet, GCArrayGetS, GCArrayGetU,
		GCArraySet, GCArrayFill:
		WriteLEB128u(buf, imm.TypeIdx)

	case GCArrayNewFixed:
		WriteLEB128u(buf, imm.TypeIdx)
		WriteLEB128u(buf, imm.Size)

	case GCArrayNewData, GCArrayInitData:
		WriteLEB128u(buf, imm.TypeIdx)
		WriteLEB128u(buf, imm.DataIdx)

	case GCArrayNewElem, GCArrayInitElem:
		WriteLEB128u(buf, imm.TypeIdx)
		WriteLEB128u(buf, imm.ElemIdx)

	case GCArrayCopy:
		WriteLEB128u(buf, imm.TypeIdx)
		WriteLEB128u(buf, imm.TypeIdx2)

	case GCRefTest, GCRefTestNull, GCRefCast, GCRefCastNull:
		WriteLEB128s64(buf, imm.HeapType)

	case GCBrOnCast, GCBrOnCastFail:
		buf.WriteByte(imm.CastFlags)
		WriteLEB128u(buf, imm.LabelIdx)
		WriteLEB128s64(buf, imm.HeapType)
		WriteLEB128s64(buf, imm.HeapType2)

	case GCArrayLen, GCAnyConvertExtern, GCExternConvertAny,
		GCRefI31, GCI31GetS, GCI31GetU:
		// No immediates
	}
}

// Multi-memory memarg bit flag
const memArgMultiMemBit = 0x40

// readMemArg reads a memarg with multi-memory support.
// If bit 6 of align is set, a separate memidx LEB128 follows.
func readMemArg(r *bytes.Reader) (MemoryImm, error) {
	alignRaw, err := ReadLEB128u(r)
	if err != nil {
		return MemoryImm{}, err
	}

	var memIdx uint32
	if alignRaw&memArgMultiMemBit != 0 {
		memIdx, err = ReadLEB128u(r)
		if err != nil {
			return MemoryImm{}, err
		}
	}

	offset, err := ReadLEB128u64(r)
	if err != nil {
		return MemoryImm{}, err
	}

	return MemoryImm{
		Align:  alignRaw & ^uint32(memArgMultiMemBit),
		Offset: offset,
		MemIdx: memIdx,
	}, nil
}

// writeMemArg writes a memarg with multi-memory support.
func writeMemArg(buf *bytes.Buffer, imm MemoryImm) {
	alignRaw := imm.Align
	if imm.MemIdx != 0 {
		alignRaw |= memArgMultiMemBit
	}
	WriteLEB128u(buf, alignRaw)
	if imm.MemIdx != 0 {
		WriteLEB128u(buf, imm.MemIdx)
	}
	WriteLEB128u64(buf, imm.Offset)
}
