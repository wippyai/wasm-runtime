package encoder

import (
	"github.com/wippyai/wasm-runtime/wat/internal/ast"
)

func EncodeInstr(buf *Buffer, ins ast.Instr) {
	buf.AppendByte(ins.Opcode)

	switch ins.Opcode {
	// Instructions with u32 immediate (br, br_if, call, local.*, global.*)
	case ast.OpBr, ast.OpBrIf, ast.OpCall, ast.OpReturnCall,
		ast.OpLocalGet, ast.OpLocalSet, ast.OpLocalTee,
		ast.OpGlobalGet, ast.OpGlobalSet:
		buf.WriteU32(ins.Imm.(uint32))

	case ast.OpI32Const:
		buf.WriteI32(ins.Imm.(int32))

	case ast.OpI64Const:
		buf.WriteI64(ins.Imm.(int64))

	case ast.OpF32Const:
		buf.WriteF32(ins.Imm.(float32))

	case ast.OpF64Const:
		buf.WriteF64(ins.Imm.(float64))

	// Block types
	case ast.OpBlock, ast.OpLoop, ast.OpIf:
		bt := ins.Imm.(ast.BlockType)
		if bt.TypeIdx >= 0 {
			buf.WriteI33(int64(bt.TypeIdx))
		} else {
			buf.AppendByte(bt.Simple)
		}

	// Memory operations
	case ast.OpI32Load, ast.OpI64Load, ast.OpF32Load, ast.OpF64Load,
		ast.OpI32Load8S, ast.OpI32Load8U, ast.OpI32Load16S, ast.OpI32Load16U,
		ast.OpI64Load8S, ast.OpI64Load8U, ast.OpI64Load16S, ast.OpI64Load16U,
		ast.OpI64Load32S, ast.OpI64Load32U,
		ast.OpI32Store, ast.OpI64Store, ast.OpF32Store, ast.OpF64Store,
		ast.OpI32Store8, ast.OpI32Store16,
		ast.OpI64Store8, ast.OpI64Store16, ast.OpI64Store32:
		ma := ins.Imm.(ast.Memarg)
		if ma.MemIdx > 0 {
			buf.WriteU32(ma.Align | 0x40)
			buf.WriteU32(ma.MemIdx)
		} else {
			buf.WriteU32(ma.Align)
		}
		buf.WriteU32(ma.Offset)

	case ast.OpMemorySize, ast.OpMemoryGrow:
		if ins.Imm == nil {
			buf.AppendByte(0x00)
		} else {
			buf.WriteU32(ins.Imm.(uint32))
		}

	case ast.OpBrTable:
		labels := ins.Imm.([]uint32)
		buf.WriteU32(uint32(len(labels) - 1))
		for _, label := range labels {
			buf.WriteU32(label)
		}

	case ast.OpCallIndirect, ast.OpReturnCallIndirect:
		indices := ins.Imm.([]uint32)
		buf.WriteU32(indices[0])
		buf.WriteU32(indices[1])

	case ast.OpTableGet, ast.OpTableSet:
		buf.WriteU32(ins.Imm.(uint32))

	case ast.OpRefNull:
		buf.AppendByte(ins.Imm.(byte))

	case ast.OpRefFunc:
		buf.WriteU32(ins.Imm.(uint32))

	case ast.OpSelectTyped:
		types := ins.Imm.([]ast.ValType)
		buf.WriteU32(uint32(len(types)))
		for _, t := range types {
			buf.AppendByte(byte(t))
		}

	case ast.OpPrefixMisc:
		encodeMiscOp(buf, ins.Imm)
	}
}

func encodeMiscOp(buf *Buffer, imm interface{}) {
	switch v := imm.(type) {
	case uint32:
		// Used for saturating truncation ops (subops 0-7) which have no extra immediates
		buf.WriteU32(v)
	case []uint32:
		// Used for bulk memory and table ops: [subop, ...indices]
		for _, val := range v {
			buf.WriteU32(val)
		}
	}
}
