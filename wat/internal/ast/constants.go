package ast

type ValType byte

const (
	ValTypeI32       ValType = 0x7F
	ValTypeI64       ValType = 0x7E
	ValTypeF32       ValType = 0x7D
	ValTypeF64       ValType = 0x7C
	ValTypeFuncref   ValType = 0x70
	ValTypeExternref ValType = 0x6F
)

const BlockTypeEmpty byte = 0x40

const (
	KindFunc   byte = 0
	KindTable  byte = 1
	KindMemory byte = 2
	KindGlobal byte = 3
)

const (
	ElemModeActive      = 0
	ElemModePassive     = 1
	ElemModeActiveTable = 2
	ElemModeDeclarative = 3
)

const (
	RefTypeFuncref   byte = 0x70
	RefTypeExternref byte = 0x6F
)

const (
	SectionCustom    byte = 0
	SectionType      byte = 1
	SectionImport    byte = 2
	SectionFunc      byte = 3
	SectionTable     byte = 4
	SectionMemory    byte = 5
	SectionGlobal    byte = 6
	SectionExport    byte = 7
	SectionStart     byte = 8
	SectionElem      byte = 9
	SectionCode      byte = 10
	SectionData      byte = 11
	SectionDataCount byte = 12
)

const (
	FuncTypeMarker byte = 0x60
	LimitsNoMax    byte = 0x00
	LimitsHasMax   byte = 0x01
)

const (
	ElemFlagActiveFunc      byte = 0x00
	ElemFlagPassiveFunc     byte = 0x01
	ElemFlagActiveTableFunc byte = 0x02
	ElemFlagDeclarativeFunc byte = 0x03
	ElemFlagActiveExpr      byte = 0x04
	ElemFlagPassiveExpr     byte = 0x05
	ElemFlagActiveTableExpr byte = 0x06
	ElemFlagDeclarativeExpr byte = 0x07
)

const (
	DataFlagActive       byte = 0x00
	DataFlagPassive      byte = 0x01
	DataFlagActiveMemIdx byte = 0x02
)

const ElemKindFuncref byte = 0x00

const (
	OpUnreachable        byte = 0x00
	OpNop                byte = 0x01
	OpBlock              byte = 0x02
	OpLoop               byte = 0x03
	OpIf                 byte = 0x04
	OpElse               byte = 0x05
	OpEnd                byte = 0x0B
	OpBr                 byte = 0x0C
	OpBrIf               byte = 0x0D
	OpBrTable            byte = 0x0E
	OpReturn             byte = 0x0F
	OpCall               byte = 0x10
	OpCallIndirect       byte = 0x11
	OpReturnCall         byte = 0x12
	OpReturnCallIndirect byte = 0x13
	OpDrop               byte = 0x1A
	OpSelect             byte = 0x1B
	OpSelectTyped        byte = 0x1C
	OpLocalGet           byte = 0x20
	OpLocalSet           byte = 0x21
	OpLocalTee           byte = 0x22
	OpGlobalGet          byte = 0x23
	OpGlobalSet          byte = 0x24
	OpTableGet           byte = 0x25
	OpTableSet           byte = 0x26
	OpI32Const           byte = 0x41
	OpI64Const           byte = 0x42
	OpF32Const           byte = 0x43
	OpF64Const           byte = 0x44
	OpMemorySize         byte = 0x3F
	OpMemoryGrow         byte = 0x40
	OpRefNull            byte = 0xD0
	OpRefIsNull          byte = 0xD1
	OpRefFunc            byte = 0xD2
	OpPrefixMisc         byte = 0xFC
)

const (
	OpI32Load    byte = 0x28
	OpI64Load    byte = 0x29
	OpF32Load    byte = 0x2A
	OpF64Load    byte = 0x2B
	OpI32Load8S  byte = 0x2C
	OpI32Load8U  byte = 0x2D
	OpI32Load16S byte = 0x2E
	OpI32Load16U byte = 0x2F
	OpI64Load8S  byte = 0x30
	OpI64Load8U  byte = 0x31
	OpI64Load16S byte = 0x32
	OpI64Load16U byte = 0x33
	OpI64Load32S byte = 0x34
	OpI64Load32U byte = 0x35
	OpI32Store   byte = 0x36
	OpI64Store   byte = 0x37
	OpF32Store   byte = 0x38
	OpF64Store   byte = 0x39
	OpI32Store8  byte = 0x3A
	OpI32Store16 byte = 0x3B
	OpI64Store8  byte = 0x3C
	OpI64Store16 byte = 0x3D
	OpI64Store32 byte = 0x3E
)

const (
	MiscOpMemoryInit uint32 = 8
	MiscOpDataDrop   uint32 = 9
	MiscOpMemoryCopy uint32 = 10
	MiscOpMemoryFill uint32 = 11
	MiscOpTableInit  uint32 = 12
	MiscOpElemDrop   uint32 = 13
	MiscOpTableCopy  uint32 = 14
	MiscOpTableGrow  uint32 = 15
	MiscOpTableSize  uint32 = 16
	MiscOpTableFill  uint32 = 17
)
