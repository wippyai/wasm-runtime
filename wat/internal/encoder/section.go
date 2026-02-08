package encoder

import (
	"github.com/wippyai/wasm-runtime/wat/internal/ast"
)

func writeSection(buf *Buffer, id byte, content *Buffer) {
	buf.AppendByte(id)
	buf.WriteU32(uint32(len(content.Bytes)))
	buf.WriteBytes(content.Bytes)
}

func encodeTypeSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Types)))
	for _, ft := range m.Types {
		sec.AppendByte(ast.FuncTypeMarker)
		sec.WriteU32(uint32(len(ft.Params)))
		for _, p := range ft.Params {
			sec.AppendByte(byte(p))
		}
		sec.WriteU32(uint32(len(ft.Results)))
		for _, r := range ft.Results {
			sec.AppendByte(byte(r))
		}
	}
	writeSection(buf, ast.SectionType, sec)
}

func encodeImportSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Imports)))
	for _, imp := range m.Imports {
		sec.WriteString(imp.Module)
		sec.WriteString(imp.Name)
		sec.AppendByte(imp.Desc.Kind)
		switch imp.Desc.Kind {
		case ast.KindFunc:
			sec.WriteU32(imp.Desc.TypeIdx)
		case ast.KindTable:
			tt := imp.Desc.TableTyp
			sec.AppendByte(tt.ElemType)
			sec.WriteLimits(tt.Limits.Min, tt.Limits.Max)
		case ast.KindMemory:
			lim := imp.Desc.MemLimits
			sec.WriteLimits(lim.Min, lim.Max)
		case ast.KindGlobal:
			gt := imp.Desc.GlobalTyp
			sec.AppendByte(byte(gt.ValType))
			if gt.Mutable {
				sec.AppendByte(0x01)
			} else {
				sec.AppendByte(0x00)
			}
		}
	}
	writeSection(buf, ast.SectionImport, sec)
}

func encodeFuncSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Funcs)))
	for _, f := range m.Funcs {
		sec.WriteU32(f.TypeIdx)
	}
	writeSection(buf, ast.SectionFunc, sec)
}

func encodeTableSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Tables)))
	for _, t := range m.Tables {
		sec.AppendByte(t.ElemType)
		sec.WriteLimits(t.Limits.Min, t.Limits.Max)
	}
	writeSection(buf, ast.SectionTable, sec)
}

func encodeMemorySection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Memories)))
	for _, mem := range m.Memories {
		sec.WriteLimits(mem.Limits.Min, mem.Limits.Max)
	}
	writeSection(buf, ast.SectionMemory, sec)
}

func encodeGlobalSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Globals)))
	for _, g := range m.Globals {
		sec.AppendByte(byte(g.Type.ValType))
		if g.Type.Mutable {
			sec.AppendByte(0x01)
		} else {
			sec.AppendByte(0x00)
		}
		for _, instr := range g.Init {
			EncodeInstr(sec, instr)
		}
	}
	writeSection(buf, ast.SectionGlobal, sec)
}

func encodeExportSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Exports)))
	for _, e := range m.Exports {
		sec.WriteString(e.Name)
		sec.AppendByte(e.Kind)
		sec.WriteU32(e.Idx)
	}
	writeSection(buf, ast.SectionExport, sec)
}

func encodeStartSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(*m.Start)
	writeSection(buf, ast.SectionStart, sec)
}

func encodeElemSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Elems)))
	for _, e := range m.Elems {
		hasExprs := len(e.Exprs) > 0
		refTypeByte := e.RefType
		if refTypeByte == 0 {
			refTypeByte = ast.ElemKindFuncref
		}

		switch e.Mode {
		case ast.ElemModeActive:
			if hasExprs {
				sec.AppendByte(ast.ElemFlagActiveExpr)
				encodeExpr(sec, e.Offset)
				sec.WriteU32(uint32(len(e.Exprs)))
				for _, expr := range e.Exprs {
					encodeExpr(sec, expr)
				}
			} else {
				sec.AppendByte(ast.ElemFlagActiveFunc)
				encodeExpr(sec, e.Offset)
				sec.WriteU32(uint32(len(e.Init)))
				for _, idx := range e.Init {
					sec.WriteU32(idx)
				}
			}
		case ast.ElemModePassive:
			if hasExprs {
				sec.AppendByte(ast.ElemFlagPassiveExpr)
				sec.AppendByte(refTypeByte)
				sec.WriteU32(uint32(len(e.Exprs)))
				for _, expr := range e.Exprs {
					encodeExpr(sec, expr)
				}
			} else {
				sec.AppendByte(ast.ElemFlagPassiveFunc)
				sec.AppendByte(ast.ElemKindFuncref)
				sec.WriteU32(uint32(len(e.Init)))
				for _, idx := range e.Init {
					sec.WriteU32(idx)
				}
			}
		case ast.ElemModeActiveTable:
			if hasExprs {
				sec.AppendByte(ast.ElemFlagActiveTableExpr)
				sec.WriteU32(e.TableIdx)
				encodeExpr(sec, e.Offset)
				sec.AppendByte(refTypeByte)
				sec.WriteU32(uint32(len(e.Exprs)))
				for _, expr := range e.Exprs {
					encodeExpr(sec, expr)
				}
			} else {
				sec.AppendByte(ast.ElemFlagActiveTableFunc)
				sec.WriteU32(e.TableIdx)
				encodeExpr(sec, e.Offset)
				sec.AppendByte(ast.ElemKindFuncref)
				sec.WriteU32(uint32(len(e.Init)))
				for _, idx := range e.Init {
					sec.WriteU32(idx)
				}
			}
		case ast.ElemModeDeclarative:
			if hasExprs {
				sec.AppendByte(ast.ElemFlagDeclarativeExpr)
				sec.AppendByte(refTypeByte)
				sec.WriteU32(uint32(len(e.Exprs)))
				for _, expr := range e.Exprs {
					encodeExpr(sec, expr)
				}
			} else {
				sec.AppendByte(ast.ElemFlagDeclarativeFunc)
				sec.AppendByte(ast.ElemKindFuncref)
				sec.WriteU32(uint32(len(e.Init)))
				for _, idx := range e.Init {
					sec.WriteU32(idx)
				}
			}
		}
	}
	writeSection(buf, ast.SectionElem, sec)
}

func encodeExpr(buf *Buffer, instrs []ast.Instr) {
	for _, ins := range instrs {
		EncodeInstr(buf, ins)
	}
}

func encodeCodeSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Code)))
	for _, c := range m.Code {
		code := &Buffer{}

		// Group consecutive locals
		var groups []struct {
			count uint32
			vt    ast.ValType
		}
		for _, l := range c.Locals {
			if len(groups) > 0 && groups[len(groups)-1].vt == l {
				groups[len(groups)-1].count++
			} else {
				groups = append(groups, struct {
					count uint32
					vt    ast.ValType
				}{1, l})
			}
		}

		code.WriteU32(uint32(len(groups)))
		for _, g := range groups {
			code.WriteU32(g.count)
			code.AppendByte(byte(g.vt))
		}

		for _, instr := range c.Code {
			EncodeInstr(code, instr)
		}

		sec.WriteU32(uint32(len(code.Bytes)))
		sec.WriteBytes(code.Bytes)
	}
	writeSection(buf, ast.SectionCode, sec)
}

func encodeDataCountSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Data)))
	writeSection(buf, ast.SectionDataCount, sec)
}

func encodeDataSection(buf *Buffer, m *ast.Module) {
	sec := &Buffer{}
	sec.WriteU32(uint32(len(m.Data)))
	for _, d := range m.Data {
		if d.Passive {
			sec.AppendByte(ast.DataFlagPassive)
		} else if d.MemIdx != 0 {
			sec.AppendByte(ast.DataFlagActiveMemIdx)
			sec.WriteU32(d.MemIdx)
			encodeExpr(sec, d.Offset)
		} else {
			sec.AppendByte(ast.DataFlagActive)
			encodeExpr(sec, d.Offset)
		}
		sec.WriteU32(uint32(len(d.Init)))
		sec.WriteBytes(d.Init)
	}
	writeSection(buf, ast.SectionData, sec)
}
