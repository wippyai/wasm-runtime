package wasm

import (
	"github.com/wippyai/wasm-runtime/wasm/internal/binary"
)

// Encode encodes the module to WebAssembly binary format
func (m *Module) Encode() []byte {
	w := binary.NewWriter()

	// Magic number and version
	w.WriteU32LE(Magic)
	w.WriteU32LE(Version)

	// Type section
	if len(m.TypeDefs) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.TypeDefs)))
		for _, td := range m.TypeDefs {
			writeTypeDef(sec, td)
		}
		writeSection(w, SectionType, sec.Bytes())
	} else if len(m.Types) > 0 {
		// Fallback for legacy Types-only modules
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Types)))
		for _, ft := range m.Types {
			sec.Byte(FuncTypeByte)
			writeValTypes(sec, ft.Params)
			writeValTypes(sec, ft.Results)
		}
		writeSection(w, SectionType, sec.Bytes())
	}

	// Import section
	if len(m.Imports) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Imports)))
		for _, imp := range m.Imports {
			sec.WriteName(imp.Module)
			sec.WriteName(imp.Name)
			sec.Byte(imp.Desc.Kind)
			switch imp.Desc.Kind {
			case KindFunc:
				sec.WriteU32(imp.Desc.TypeIdx)
			case KindTable:
				if imp.Desc.Table != nil {
					writeTableType(sec, *imp.Desc.Table)
				}
			case KindMemory:
				if imp.Desc.Memory != nil {
					writeMemoryType(sec, *imp.Desc.Memory)
				}
			case KindGlobal:
				if imp.Desc.Global != nil {
					writeGlobalType(sec, *imp.Desc.Global)
				}
			case KindTag:
				if imp.Desc.Tag != nil {
					writeTagType(sec, *imp.Desc.Tag)
				}
			}
		}
		writeSection(w, SectionImport, sec.Bytes())
	}

	// Function section
	if len(m.Funcs) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Funcs)))
		for _, typeIdx := range m.Funcs {
			sec.WriteU32(typeIdx)
		}
		writeSection(w, SectionFunction, sec.Bytes())
	}

	// Table section
	if len(m.Tables) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Tables)))
		for _, t := range m.Tables {
			writeTableType(sec, t)
		}
		writeSection(w, SectionTable, sec.Bytes())
	}

	// Memory section
	if len(m.Memories) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Memories)))
		for _, mem := range m.Memories {
			writeMemoryType(sec, mem)
		}
		writeSection(w, SectionMemory, sec.Bytes())
	}

	// Tag section (must come between Memory and Global per spec)
	if len(m.Tags) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Tags)))
		for _, tag := range m.Tags {
			writeTagType(sec, tag)
		}
		writeSection(w, SectionTag, sec.Bytes())
	}

	// Global section
	if len(m.Globals) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Globals)))
		for _, g := range m.Globals {
			writeGlobalType(sec, g.Type)
			sec.WriteBytes(g.Init)
		}
		writeSection(w, SectionGlobal, sec.Bytes())
	}

	// Export section
	if len(m.Exports) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Exports)))
		for _, exp := range m.Exports {
			sec.WriteName(exp.Name)
			sec.Byte(exp.Kind)
			sec.WriteU32(exp.Idx)
		}
		writeSection(w, SectionExport, sec.Bytes())
	}

	// Start section
	if m.Start != nil {
		sec := binary.NewWriter()
		sec.WriteU32(*m.Start)
		writeSection(w, SectionStart, sec.Bytes())
	}

	// Element section
	if len(m.Elements) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Elements)))
		for _, elem := range m.Elements {
			sec.WriteU32(elem.Flags)

			hasTableIdx := elem.Flags&0x02 != 0 && elem.Flags&0x01 == 0
			hasOffset := elem.Flags&0x01 == 0
			usesExprs := elem.Flags&0x04 != 0

			if hasTableIdx {
				sec.WriteU32(elem.TableIdx)
			}

			if hasOffset {
				sec.WriteBytes(elem.Offset)
			}

			// Flags 1, 2, 3: elemkind; flags 5, 6, 7: reftype
			if elem.Flags&0x03 != 0 {
				if usesExprs {
					if elem.RefType != nil {
						if elem.RefType.Nullable {
							sec.Byte(byte(ValRefNull))
						} else {
							sec.Byte(byte(ValRef))
						}
						sec.WriteS64(elem.RefType.HeapType)
					} else {
						sec.Byte(byte(elem.Type))
					}
				} else {
					sec.Byte(elem.ElemKind)
				}
			}

			if usesExprs {
				sec.WriteU32(uint32(len(elem.Exprs)))
				for _, expr := range elem.Exprs {
					sec.WriteBytes(expr)
				}
			} else {
				sec.WriteU32(uint32(len(elem.FuncIdxs)))
				for _, idx := range elem.FuncIdxs {
					sec.WriteU32(idx)
				}
			}
		}
		writeSection(w, SectionElement, sec.Bytes())
	}

	// DataCount section (must appear before Code section if present)
	if m.DataCount != nil {
		sec := binary.NewWriter()
		sec.WriteU32(*m.DataCount)
		writeSection(w, SectionDataCount, sec.Bytes())
	}

	// Code section
	if len(m.Code) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Code)))
		for _, body := range m.Code {
			bodyBuf := binary.NewWriter()
			bodyBuf.WriteU32(uint32(len(body.Locals)))
			for _, local := range body.Locals {
				bodyBuf.WriteU32(local.Count)
				if local.ExtType != nil && local.ExtType.Kind == ExtValKindRef {
					if local.ExtType.RefType.Nullable {
						bodyBuf.Byte(byte(ValRefNull))
					} else {
						bodyBuf.Byte(byte(ValRef))
					}
					bodyBuf.WriteS64(local.ExtType.RefType.HeapType)
				} else {
					bodyBuf.Byte(byte(local.ValType))
				}
			}
			bodyBuf.WriteBytes(body.Code)
			sec.WriteU32(uint32(bodyBuf.Len()))
			sec.WriteBytes(bodyBuf.Bytes())
		}
		writeSection(w, SectionCode, sec.Bytes())
	}

	// Data section
	if len(m.Data) > 0 {
		sec := binary.NewWriter()
		sec.WriteU32(uint32(len(m.Data)))
		for _, d := range m.Data {
			sec.WriteU32(d.Flags)

			if d.Flags == 2 {
				sec.WriteU32(d.MemIdx)
			}

			if d.Flags != 1 {
				sec.WriteBytes(d.Offset)
			}

			sec.WriteU32(uint32(len(d.Init)))
			sec.WriteBytes(d.Init)
		}
		writeSection(w, SectionData, sec.Bytes())
	}

	// Custom sections (at end)
	for _, cs := range m.CustomSections {
		sec := binary.NewWriter()
		sec.WriteName(cs.Name)
		sec.WriteBytes(cs.Data)
		writeSection(w, SectionCustom, sec.Bytes())
	}

	return w.Bytes()
}

func writeSection(w *binary.Writer, id byte, data []byte) {
	w.Byte(id)
	w.WriteU32(uint32(len(data)))
	w.WriteBytes(data)
}

func writeValTypes(w *binary.Writer, types []ValType) {
	w.WriteU32(uint32(len(types)))
	for _, t := range types {
		w.Byte(byte(t))
	}
}

func writeLimits(w *binary.Writer, l Limits) {
	var flags byte
	if l.Max != nil {
		flags |= LimitsHasMax
	}
	if l.Shared {
		flags |= LimitsShared
	}
	if l.Memory64 {
		flags |= LimitsMemory64
	}
	w.Byte(flags)

	if l.Memory64 {
		w.WriteU64(l.Min)
		if l.Max != nil {
			w.WriteU64(*l.Max)
		}
	} else {
		w.WriteU32(uint32(l.Min))
		if l.Max != nil {
			w.WriteU32(uint32(*l.Max))
		}
	}
}

func writeTableType(w *binary.Writer, t TableType) {
	if len(t.Init) > 0 {
		// Table with init expression: 0x40 0x00 prefix
		w.Byte(0x40)
		w.Byte(0x00)
		writeTableElemType(w, t)
		writeLimits(w, t.Limits)
		w.WriteBytes(t.Init)
	} else {
		// Standard format
		writeTableElemType(w, t)
		writeLimits(w, t.Limits)
	}
}

func writeTableElemType(w *binary.Writer, t TableType) {
	if t.RefElemType != nil {
		if t.RefElemType.Nullable {
			w.Byte(byte(ValRefNull))
		} else {
			w.Byte(byte(ValRef))
		}
		w.WriteS64(t.RefElemType.HeapType)
	} else {
		w.Byte(t.ElemType)
	}
}

func writeMemoryType(w *binary.Writer, m MemoryType) {
	writeLimits(w, m.Limits)
}

func writeGlobalType(w *binary.Writer, g GlobalType) {
	if g.ExtType != nil && g.ExtType.Kind == ExtValKindRef {
		if g.ExtType.RefType.Nullable {
			w.Byte(byte(ValRefNull))
		} else {
			w.Byte(byte(ValRef))
		}
		w.WriteS64(g.ExtType.RefType.HeapType)
	} else {
		w.Byte(byte(g.ValType))
	}
	if g.Mutable {
		w.Byte(1)
	} else {
		w.Byte(0)
	}
}

func writeTagType(w *binary.Writer, t TagType) {
	w.Byte(t.Attribute)
	w.WriteU32(t.TypeIdx)
}

func writeTypeDef(w *binary.Writer, td TypeDef) {
	switch td.Kind {
	case TypeDefKindFunc:
		w.Byte(FuncTypeByte)
		writeFuncType(w, *td.Func)
	case TypeDefKindSub:
		writeSubType(w, *td.Sub)
	case TypeDefKindRec:
		w.Byte(RecTypeByte)
		w.WriteU32(uint32(len(td.Rec.Types)))
		for _, sub := range td.Rec.Types {
			writeSubType(w, sub)
		}
	}
}

func writeFuncType(w *binary.Writer, ft FuncType) {
	// Use extended types if available, otherwise fall back to simple types
	if len(ft.ExtParams) > 0 {
		writeExtValTypes(w, ft.ExtParams)
	} else {
		writeValTypes(w, ft.Params)
	}
	if len(ft.ExtResults) > 0 {
		writeExtValTypes(w, ft.ExtResults)
	} else {
		writeValTypes(w, ft.Results)
	}
}

func writeExtValTypes(w *binary.Writer, types []ExtValType) {
	w.WriteU32(uint32(len(types)))
	for _, t := range types {
		switch t.Kind {
		case ExtValKindRef:
			if t.RefType.Nullable {
				w.Byte(byte(ValRefNull)) // 0x63
			} else {
				w.Byte(byte(ValRef)) // 0x64
			}
			w.WriteS64(t.RefType.HeapType)
		default:
			w.Byte(byte(t.ValType))
		}
	}
}

func writeSubType(w *binary.Writer, sub SubType) {
	if len(sub.Parents) > 0 || !sub.Final {
		// Need explicit sub/sub_final prefix
		if sub.Final {
			w.Byte(SubFinalByte)
		} else {
			w.Byte(SubTypeByte)
		}
		w.WriteU32(uint32(len(sub.Parents)))
		for _, p := range sub.Parents {
			w.WriteU32(p)
		}
		writeCompType(w, sub.CompType)
	} else {
		// Shorthand: directly write composite type
		writeCompType(w, sub.CompType)
	}
}

func writeCompType(w *binary.Writer, ct CompType) {
	switch ct.Kind {
	case CompKindFunc:
		w.Byte(FuncTypeByte)
		writeFuncType(w, *ct.Func)
	case CompKindStruct:
		w.Byte(StructTypeByte)
		writeStructType(w, *ct.Struct)
	case CompKindArray:
		w.Byte(ArrayTypeByte)
		writeArrayType(w, *ct.Array)
	}
}

func writeStructType(w *binary.Writer, st StructType) {
	w.WriteU32(uint32(len(st.Fields)))
	for _, f := range st.Fields {
		writeFieldType(w, f)
	}
}

func writeArrayType(w *binary.Writer, at ArrayType) {
	writeFieldType(w, at.Element)
}

func writeFieldType(w *binary.Writer, ft FieldType) {
	writeStorageType(w, ft.Type)
	if ft.Mutable {
		w.Byte(1)
	} else {
		w.Byte(0)
	}
}

func writeStorageType(w *binary.Writer, st StorageType) {
	switch st.Kind {
	case StorageKindVal:
		w.Byte(byte(st.ValType))
	case StorageKindPacked:
		w.Byte(st.Packed)
	case StorageKindRef:
		if st.RefType.Nullable {
			w.Byte(byte(ValRefNull)) // 0x63
		} else {
			w.Byte(byte(ValRef)) // 0x64
		}
		w.WriteS64(st.RefType.HeapType)
	}
}
