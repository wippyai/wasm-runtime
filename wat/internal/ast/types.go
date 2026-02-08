package ast

type Module struct {
	Types    []FuncType
	Imports  []Import
	Funcs    []FuncEntry
	Tables   []Table
	Memories []Memory
	Globals  []Global
	Exports  []Export
	Start    *uint32
	Elems    []Elem
	Code     []FuncBody
	Data     []DataSegment
}

type FuncType struct {
	Params  []ValType
	Results []ValType
}

func (ft FuncType) Equal(other FuncType) bool {
	if len(ft.Params) != len(other.Params) || len(ft.Results) != len(other.Results) {
		return false
	}
	for i, p := range ft.Params {
		if p != other.Params[i] {
			return false
		}
	}
	for i, r := range ft.Results {
		if r != other.Results[i] {
			return false
		}
	}
	return true
}

type Import struct {
	Desc   ImportDesc
	Module string
	Name   string
}

type ImportDesc struct {
	Type      *FuncType
	GlobalTyp *GlobalType
	MemLimits *Limits
	TableTyp  *Table
	TypeIdx   uint32
	Kind      byte
}

type FuncEntry struct {
	TypeIdx uint32
}

type Table struct {
	Limits   Limits
	ElemType byte
}

type Memory struct {
	Limits Limits
}

type Limits struct {
	Max *uint32
	Min uint32
}

type Global struct {
	Init []Instr
	Type GlobalType
}

type GlobalType struct {
	ValType ValType
	Mutable bool
}

type Export struct {
	Name string
	Kind byte
	Idx  uint32
}

type Elem struct {
	Offset   []Instr
	Init     []uint32
	Exprs    [][]Instr
	Mode     int
	TableIdx uint32
	RefType  byte
}

type FuncBody struct {
	Locals []ValType
	Code   []Instr
}

type DataSegment struct {
	Offset  []Instr
	Init    []byte
	MemIdx  uint32
	Passive bool
}

type Instr struct {
	Imm    interface{}
	Opcode byte
}

type Memarg struct {
	Align  uint32
	Offset uint32
	MemIdx uint32 // Memory index for multi-memory
}

type BlockType struct {
	Params  []ValType
	Results []ValType
	TypeIdx int32
	Simple  byte
}
