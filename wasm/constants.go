package wasm

// WebAssembly binary format magic number and version.
const (
	// Magic is the WebAssembly binary magic number ("\0asm" in little-endian).
	Magic uint32 = 0x6D736100

	// Version is the supported WebAssembly binary format version.
	Version uint32 = 0x01
)

// Section IDs define the binary identifiers for each module section.
// Sections must appear in increasing order by ID (except custom sections).
const (
	SectionCustom    byte = 0  // Custom section (can appear anywhere)
	SectionType      byte = 1  // Type section (function signatures)
	SectionImport    byte = 2  // Import section
	SectionFunction  byte = 3  // Function section (type indices)
	SectionTable     byte = 4  // Table section
	SectionMemory    byte = 5  // Memory section
	SectionGlobal    byte = 6  // Global section
	SectionExport    byte = 7  // Export section
	SectionStart     byte = 8  // Start section
	SectionElement   byte = 9  // Element section
	SectionCode      byte = 10 // Code section (function bodies)
	SectionData      byte = 11 // Data section
	SectionDataCount byte = 12 // Data count section (bulk memory)
	SectionTag       byte = 13 // Tag section (exception handling)
)

// Import/Export descriptor kinds identify the type of imported or exported item.
const (
	KindFunc   byte = 0 // Function import/export
	KindTable  byte = 1 // Table import/export
	KindMemory byte = 2 // Memory import/export
	KindGlobal byte = 3 // Global import/export
	KindTag    byte = 4 // Tag import/export (exception handling)
)

// Value type encodings as defined in the WebAssembly binary format.
// Core types use 0x7F-0x7B, reference types use 0x70-0x63.
const (
	ValI32     ValType = 0x7F // 32-bit integer
	ValI64     ValType = 0x7E // 64-bit integer
	ValF32     ValType = 0x7D // 32-bit float
	ValF64     ValType = 0x7C // 64-bit float
	ValV128    ValType = 0x7B // 128-bit vector (SIMD)
	ValFuncRef ValType = 0x70 // Function reference
	ValExtern  ValType = 0x6F // External reference

	// GC proposal reference types
	ValRefNull       ValType = 0x63 // (ref null ht) - nullable reference with heap type
	ValRef           ValType = 0x64 // (ref ht) - non-nullable reference with heap type
	ValNullFuncRef   ValType = 0x73 // nullfuncref - null function reference
	ValNullExternRef ValType = 0x72 // nullexternref - null external reference
	ValNullRef       ValType = 0x71 // nullref - bottom type for internal hierarchy
	ValEqRef         ValType = 0x6D // eqref - equality-comparable reference
	ValI31Ref        ValType = 0x6C // i31ref - 31-bit integer reference
	ValStructRef     ValType = 0x6B // structref - struct reference
	ValArrayRef      ValType = 0x6A // arrayref - array reference
	ValAnyRef        ValType = 0x6E // anyref - any internal reference
)

// Block type constants
const (
	BlockTypeVoid int32 = -64 // 0x40
	BlockTypeI32  int32 = -1  // 0x7F
	BlockTypeI64  int32 = -2  // 0x7E
	BlockTypeF32  int32 = -3  // 0x7D
	BlockTypeF64  int32 = -4  // 0x7C
	BlockTypeV128 int32 = -5  // 0x7B
)

// Control flow opcodes
const (
	OpUnreachable        byte = 0x00
	OpNop                byte = 0x01
	OpBlock              byte = 0x02
	OpLoop               byte = 0x03
	OpIf                 byte = 0x04
	OpElse               byte = 0x05
	OpTry                byte = 0x06 // Exception handling
	OpCatch              byte = 0x07 // Exception handling
	OpThrow              byte = 0x08 // Exception handling
	OpRethrow            byte = 0x09 // Exception handling
	OpEnd                byte = 0x0B
	OpBr                 byte = 0x0C
	OpBrIf               byte = 0x0D
	OpBrTable            byte = 0x0E
	OpReturn             byte = 0x0F
	OpCall               byte = 0x10
	OpCallIndirect       byte = 0x11
	OpReturnCall         byte = 0x12 // Tail call proposal
	OpReturnCallIndirect byte = 0x13 // Tail call proposal
	OpCallRef            byte = 0x14 // Typed function references
	OpReturnCallRef      byte = 0x15 // Typed function references
	OpDelegate           byte = 0x18 // Exception handling
	OpCatchAll           byte = 0x19 // Exception handling
	OpThrowRef           byte = 0x0A // Exception handling
	OpTryTable           byte = 0x1F // Exception handling (new)
)

// Reference type opcodes (WASM 2.0 + typed function references)
const (
	OpRefNull      byte = 0xD0
	OpRefIsNull    byte = 0xD1
	OpRefFunc      byte = 0xD2
	OpRefAsNonNull byte = 0xD3 // Typed function references
	OpRefEq        byte = 0xD4 // GC proposal
	OpBrOnNull     byte = 0xD5 // Typed function references
	OpBrOnNonNull  byte = 0xD6 // Typed function references
)

// Parametric opcodes
const (
	OpDrop       byte = 0x1A
	OpSelect     byte = 0x1B
	OpSelectType byte = 0x1C
)

// Variable access opcodes
const (
	OpLocalGet  byte = 0x20
	OpLocalSet  byte = 0x21
	OpLocalTee  byte = 0x22
	OpGlobalGet byte = 0x23
	OpGlobalSet byte = 0x24
)

// Table opcodes (WASM 2.0)
const (
	OpTableGet byte = 0x25
	OpTableSet byte = 0x26
)

// Memory load opcodes
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
)

// Memory store opcodes
const (
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

// Memory size/grow opcodes
const (
	OpMemorySize byte = 0x3F
	OpMemoryGrow byte = 0x40
)

// Constant opcodes
const (
	OpI32Const byte = 0x41
	OpI64Const byte = 0x42
	OpF32Const byte = 0x43
	OpF64Const byte = 0x44
)

// i32 comparison opcodes
const (
	OpI32Eqz byte = 0x45
	OpI32Eq  byte = 0x46
	OpI32Ne  byte = 0x47
	OpI32LtS byte = 0x48
	OpI32LtU byte = 0x49
	OpI32GtS byte = 0x4A
	OpI32GtU byte = 0x4B
	OpI32LeS byte = 0x4C
	OpI32LeU byte = 0x4D
	OpI32GeS byte = 0x4E
	OpI32GeU byte = 0x4F
)

// i64 comparison opcodes
const (
	OpI64Eqz byte = 0x50
	OpI64Eq  byte = 0x51
	OpI64Ne  byte = 0x52
	OpI64LtS byte = 0x53
	OpI64LtU byte = 0x54
	OpI64GtS byte = 0x55
	OpI64GtU byte = 0x56
	OpI64LeS byte = 0x57
	OpI64LeU byte = 0x58
	OpI64GeS byte = 0x59
	OpI64GeU byte = 0x5A
)

// f32 comparison opcodes
const (
	OpF32Eq byte = 0x5B
	OpF32Ne byte = 0x5C
	OpF32Lt byte = 0x5D
	OpF32Gt byte = 0x5E
	OpF32Le byte = 0x5F
	OpF32Ge byte = 0x60
)

// f64 comparison opcodes
const (
	OpF64Eq byte = 0x61
	OpF64Ne byte = 0x62
	OpF64Lt byte = 0x63
	OpF64Gt byte = 0x64
	OpF64Le byte = 0x65
	OpF64Ge byte = 0x66
)

// i32 numeric opcodes
const (
	OpI32Clz    byte = 0x67
	OpI32Ctz    byte = 0x68
	OpI32Popcnt byte = 0x69
	OpI32Add    byte = 0x6A
	OpI32Sub    byte = 0x6B
	OpI32Mul    byte = 0x6C
	OpI32DivS   byte = 0x6D
	OpI32DivU   byte = 0x6E
	OpI32RemS   byte = 0x6F
	OpI32RemU   byte = 0x70
	OpI32And    byte = 0x71
	OpI32Or     byte = 0x72
	OpI32Xor    byte = 0x73
	OpI32Shl    byte = 0x74
	OpI32ShrS   byte = 0x75
	OpI32ShrU   byte = 0x76
	OpI32Rotl   byte = 0x77
	OpI32Rotr   byte = 0x78
)

// i64 numeric opcodes
const (
	OpI64Clz    byte = 0x79
	OpI64Ctz    byte = 0x7A
	OpI64Popcnt byte = 0x7B
	OpI64Add    byte = 0x7C
	OpI64Sub    byte = 0x7D
	OpI64Mul    byte = 0x7E
	OpI64DivS   byte = 0x7F
	OpI64DivU   byte = 0x80
	OpI64RemS   byte = 0x81
	OpI64RemU   byte = 0x82
	OpI64And    byte = 0x83
	OpI64Or     byte = 0x84
	OpI64Xor    byte = 0x85
	OpI64Shl    byte = 0x86
	OpI64ShrS   byte = 0x87
	OpI64ShrU   byte = 0x88
	OpI64Rotl   byte = 0x89
	OpI64Rotr   byte = 0x8A
)

// f32 numeric opcodes
const (
	OpF32Abs      byte = 0x8B
	OpF32Neg      byte = 0x8C
	OpF32Ceil     byte = 0x8D
	OpF32Floor    byte = 0x8E
	OpF32Trunc    byte = 0x8F
	OpF32Nearest  byte = 0x90
	OpF32Sqrt     byte = 0x91
	OpF32Add      byte = 0x92
	OpF32Sub      byte = 0x93
	OpF32Mul      byte = 0x94
	OpF32Div      byte = 0x95
	OpF32Min      byte = 0x96
	OpF32Max      byte = 0x97
	OpF32Copysign byte = 0x98
)

// f64 numeric opcodes
const (
	OpF64Abs      byte = 0x99
	OpF64Neg      byte = 0x9A
	OpF64Ceil     byte = 0x9B
	OpF64Floor    byte = 0x9C
	OpF64Trunc    byte = 0x9D
	OpF64Nearest  byte = 0x9E
	OpF64Sqrt     byte = 0x9F
	OpF64Add      byte = 0xA0
	OpF64Sub      byte = 0xA1
	OpF64Mul      byte = 0xA2
	OpF64Div      byte = 0xA3
	OpF64Min      byte = 0xA4
	OpF64Max      byte = 0xA5
	OpF64Copysign byte = 0xA6
)

// Conversion opcodes
const (
	OpI32WrapI64        byte = 0xA7
	OpI32TruncF32S      byte = 0xA8
	OpI32TruncF32U      byte = 0xA9
	OpI32TruncF64S      byte = 0xAA
	OpI32TruncF64U      byte = 0xAB
	OpI64ExtendI32S     byte = 0xAC
	OpI64ExtendI32U     byte = 0xAD
	OpI64TruncF32S      byte = 0xAE
	OpI64TruncF32U      byte = 0xAF
	OpI64TruncF64S      byte = 0xB0
	OpI64TruncF64U      byte = 0xB1
	OpF32ConvertI32S    byte = 0xB2
	OpF32ConvertI32U    byte = 0xB3
	OpF32ConvertI64S    byte = 0xB4
	OpF32ConvertI64U    byte = 0xB5
	OpF32DemoteF64      byte = 0xB6
	OpF64ConvertI32S    byte = 0xB7
	OpF64ConvertI32U    byte = 0xB8
	OpF64ConvertI64S    byte = 0xB9
	OpF64ConvertI64U    byte = 0xBA
	OpF64PromoteF32     byte = 0xBB
	OpI32ReinterpretF32 byte = 0xBC
	OpI64ReinterpretF64 byte = 0xBD
	OpF32ReinterpretI32 byte = 0xBE
	OpF64ReinterpretI64 byte = 0xBF
)

// Sign extension opcodes (WASM 2.0)
const (
	OpI32Extend8S  byte = 0xC0
	OpI32Extend16S byte = 0xC1
	OpI64Extend8S  byte = 0xC2
	OpI64Extend16S byte = 0xC3
	OpI64Extend32S byte = 0xC4
)

// Multi-byte opcode prefixes indicate extended instruction sets.
// These are followed by a LEB128-encoded sub-opcode.
const (
	OpPrefixGC     byte = 0xFB // GC proposal: struct, array, ref operations
	OpPrefixMisc   byte = 0xFC // Misc: saturating trunc, bulk memory, table ops
	OpPrefixSIMD   byte = 0xFD // SIMD: 128-bit vector operations
	OpPrefixAtomic byte = 0xFE // Threads: atomic memory operations
)

// Misc opcodes (0xFC prefix)
const (
	MiscI32TruncSatF32S uint32 = 0x00
	MiscI32TruncSatF32U uint32 = 0x01
	MiscI32TruncSatF64S uint32 = 0x02
	MiscI32TruncSatF64U uint32 = 0x03
	MiscI64TruncSatF32S uint32 = 0x04
	MiscI64TruncSatF32U uint32 = 0x05
	MiscI64TruncSatF64S uint32 = 0x06
	MiscI64TruncSatF64U uint32 = 0x07
	MiscMemoryInit      uint32 = 0x08
	MiscDataDrop        uint32 = 0x09
	MiscMemoryCopy      uint32 = 0x0A
	MiscMemoryFill      uint32 = 0x0B
	MiscTableInit       uint32 = 0x0C
	MiscElemDrop        uint32 = 0x0D
	MiscTableCopy       uint32 = 0x0E
	MiscTableGrow       uint32 = 0x0F
	MiscTableSize       uint32 = 0x10
	MiscTableFill       uint32 = 0x11
	MiscMemoryDiscard   uint32 = 0x12 // Memory control proposal
)

// Abstract heap types for GC instructions (encoded as negative s33 values)
const (
	HeapTypeFunc     int64 = -16 // 0x70 - function reference
	HeapTypeExtern   int64 = -17 // 0x6F - external reference
	HeapTypeAny      int64 = -18 // 0x6E - any reference
	HeapTypeEq       int64 = -19 // 0x6D - eq reference
	HeapTypeI31      int64 = -20 // 0x6C - i31 reference
	HeapTypeStruct   int64 = -21 // 0x6B - struct reference
	HeapTypeArray    int64 = -22 // 0x6A - array reference
	HeapTypeExn      int64 = -23 // 0x69 - exception reference
	HeapTypeNone     int64 = -15 // 0x71 - none (bottom of internal)
	HeapTypeNoExtern int64 = -14 // 0x72 - noextern (bottom of external)
	HeapTypeNoFunc   int64 = -13 // 0x73 - nofunc (bottom of func)
	HeapTypeNoExn    int64 = -12 // 0x74 - noexn (bottom of exn)
)

// Cast flags for br_on_cast and br_on_cast_fail
const (
	CastFlagsNone       byte = 0x00 // neither type nullable
	CastFlagsFirstNull  byte = 0x01 // first type nullable
	CastFlagsSecondNull byte = 0x02 // second type nullable
	CastFlagsBothNull   byte = 0x03 // both types nullable
)

// Catch clause kinds for try_table
const (
	CatchKindCatch       byte = 0x00
	CatchKindCatchRef    byte = 0x01
	CatchKindCatchAll    byte = 0x02
	CatchKindCatchAllRef byte = 0x03
)

// GC opcodes (0xFB prefix) - struct, array, and reference operations
const (
	GCStructNew        uint32 = 0x00
	GCStructNewDefault uint32 = 0x01
	GCStructGet        uint32 = 0x02
	GCStructGetS       uint32 = 0x03
	GCStructGetU       uint32 = 0x04
	GCStructSet        uint32 = 0x05
	GCArrayNew         uint32 = 0x06
	GCArrayNewDefault  uint32 = 0x07
	GCArrayNewFixed    uint32 = 0x08
	GCArrayNewData     uint32 = 0x09
	GCArrayNewElem     uint32 = 0x0A
	GCArrayGet         uint32 = 0x0B
	GCArrayGetS        uint32 = 0x0C
	GCArrayGetU        uint32 = 0x0D
	GCArraySet         uint32 = 0x0E
	GCArrayLen         uint32 = 0x0F
	GCArrayFill        uint32 = 0x10
	GCArrayCopy        uint32 = 0x11
	GCArrayInitData    uint32 = 0x12
	GCArrayInitElem    uint32 = 0x13
	GCRefTest          uint32 = 0x14
	GCRefTestNull      uint32 = 0x15
	GCRefCast          uint32 = 0x16
	GCRefCastNull      uint32 = 0x17
	GCBrOnCast         uint32 = 0x18
	GCBrOnCastFail     uint32 = 0x19
	GCAnyConvertExtern uint32 = 0x1A
	GCExternConvertAny uint32 = 0x1B
	GCRefI31           uint32 = 0x1C
	GCI31GetS          uint32 = 0x1D
	GCI31GetU          uint32 = 0x1E
)

// Atomic opcodes (0xFE prefix)
const (
	AtomicNotify     uint32 = 0x00 // memory.atomic.notify
	AtomicWait32     uint32 = 0x01 // memory.atomic.wait32
	AtomicWait64     uint32 = 0x02 // memory.atomic.wait64
	AtomicFence      uint32 = 0x03 // atomic.fence
	AtomicI32Load    uint32 = 0x10 // i32.atomic.load
	AtomicI64Load    uint32 = 0x11 // i64.atomic.load
	AtomicI32Load8U  uint32 = 0x12 // i32.atomic.load8_u
	AtomicI32Load16U uint32 = 0x13 // i32.atomic.load16_u
	AtomicI64Load8U  uint32 = 0x14 // i64.atomic.load8_u
	AtomicI64Load16U uint32 = 0x15 // i64.atomic.load16_u
	AtomicI64Load32U uint32 = 0x16 // i64.atomic.load32_u
	AtomicI32Store   uint32 = 0x17 // i32.atomic.store
	AtomicI64Store   uint32 = 0x18 // i64.atomic.store
	AtomicI32Store8  uint32 = 0x19 // i32.atomic.store8
	AtomicI32Store16 uint32 = 0x1A // i32.atomic.store16
	AtomicI64Store8  uint32 = 0x1B // i64.atomic.store8
	AtomicI64Store16 uint32 = 0x1C // i64.atomic.store16
	AtomicI64Store32 uint32 = 0x1D // i64.atomic.store32
	// RMW operations (0x1E-0x4E)
	AtomicI32RmwAdd        uint32 = 0x1E
	AtomicI64RmwAdd        uint32 = 0x1F
	AtomicI32Rmw8AddU      uint32 = 0x20
	AtomicI32Rmw16AddU     uint32 = 0x21
	AtomicI64Rmw8AddU      uint32 = 0x22
	AtomicI64Rmw16AddU     uint32 = 0x23
	AtomicI64Rmw32AddU     uint32 = 0x24
	AtomicI32RmwSub        uint32 = 0x25
	AtomicI64RmwSub        uint32 = 0x26
	AtomicI32Rmw8SubU      uint32 = 0x27
	AtomicI32Rmw16SubU     uint32 = 0x28
	AtomicI64Rmw8SubU      uint32 = 0x29
	AtomicI64Rmw16SubU     uint32 = 0x2A
	AtomicI64Rmw32SubU     uint32 = 0x2B
	AtomicI32RmwAnd        uint32 = 0x2C
	AtomicI64RmwAnd        uint32 = 0x2D
	AtomicI32Rmw8AndU      uint32 = 0x2E
	AtomicI32Rmw16AndU     uint32 = 0x2F
	AtomicI64Rmw8AndU      uint32 = 0x30
	AtomicI64Rmw16AndU     uint32 = 0x31
	AtomicI64Rmw32AndU     uint32 = 0x32
	AtomicI32RmwOr         uint32 = 0x33
	AtomicI64RmwOr         uint32 = 0x34
	AtomicI32Rmw8OrU       uint32 = 0x35
	AtomicI32Rmw16OrU      uint32 = 0x36
	AtomicI64Rmw8OrU       uint32 = 0x37
	AtomicI64Rmw16OrU      uint32 = 0x38
	AtomicI64Rmw32OrU      uint32 = 0x39
	AtomicI32RmwXor        uint32 = 0x3A
	AtomicI64RmwXor        uint32 = 0x3B
	AtomicI32Rmw8XorU      uint32 = 0x3C
	AtomicI32Rmw16XorU     uint32 = 0x3D
	AtomicI64Rmw8XorU      uint32 = 0x3E
	AtomicI64Rmw16XorU     uint32 = 0x3F
	AtomicI64Rmw32XorU     uint32 = 0x40
	AtomicI32RmwXchg       uint32 = 0x41
	AtomicI64RmwXchg       uint32 = 0x42
	AtomicI32Rmw8XchgU     uint32 = 0x43
	AtomicI32Rmw16XchgU    uint32 = 0x44
	AtomicI64Rmw8XchgU     uint32 = 0x45
	AtomicI64Rmw16XchgU    uint32 = 0x46
	AtomicI64Rmw32XchgU    uint32 = 0x47
	AtomicI32RmwCmpxchg    uint32 = 0x48
	AtomicI64RmwCmpxchg    uint32 = 0x49
	AtomicI32Rmw8CmpxchgU  uint32 = 0x4A
	AtomicI32Rmw16CmpxchgU uint32 = 0x4B
	AtomicI64Rmw8CmpxchgU  uint32 = 0x4C
	AtomicI64Rmw16CmpxchgU uint32 = 0x4D
	AtomicI64Rmw32CmpxchgU uint32 = 0x4E
)

// Limits flags
const (
	LimitsNoMax    byte = 0x00
	LimitsHasMax   byte = 0x01
	LimitsShared   byte = 0x02
	LimitsMemory64 byte = 0x04
)

// Memory page limits per WASM spec
const (
	MemoryMaxPages32 uint64 = 65536           // 2^16 pages (4GB) for 32-bit memory
	MemoryMaxPages64 uint64 = 281474976710656 // 2^48 pages for 64-bit memory
)

// Type section encodings
const (
	FuncTypeByte   byte = 0x60 // func
	StructTypeByte byte = 0x5F // struct (GC)
	ArrayTypeByte  byte = 0x5E // array (GC)
	RecTypeByte    byte = 0x4E // rec (GC recursive types)
	SubTypeByte    byte = 0x50 // sub (GC subtyping)
	SubFinalByte   byte = 0x4F // sub final (GC subtyping, no further subtypes)
)

// Field mutability for GC struct/array fields
const (
	FieldImmutable byte = 0x00
	FieldMutable   byte = 0x01
)

// SIMD opcodes (0xFD prefix) - key operations
const (
	SimdV128Load        uint32 = 0x00
	SimdV128Load8x8S    uint32 = 0x01
	SimdV128Load8x8U    uint32 = 0x02
	SimdV128Load16x4S   uint32 = 0x03
	SimdV128Load16x4U   uint32 = 0x04
	SimdV128Load32x2S   uint32 = 0x05
	SimdV128Load32x2U   uint32 = 0x06
	SimdV128Load8Splat  uint32 = 0x07
	SimdV128Load16Splat uint32 = 0x08
	SimdV128Load32Splat uint32 = 0x09
	SimdV128Load64Splat uint32 = 0x0A
	SimdV128Store       uint32 = 0x0B
	SimdV128Const       uint32 = 0x0C
	SimdI8x16Shuffle    uint32 = 0x0D
	SimdI8x16Swizzle    uint32 = 0x0E
	SimdI8x16Splat      uint32 = 0x0F
	SimdI16x8Splat      uint32 = 0x10
	SimdI32x4Splat      uint32 = 0x11
	SimdI64x2Splat      uint32 = 0x12
	SimdF32x4Splat      uint32 = 0x13
	SimdF64x2Splat      uint32 = 0x14
	// Lane operations
	SimdI8x16ExtractLaneS uint32 = 0x15
	SimdI8x16ExtractLaneU uint32 = 0x16
	SimdI8x16ReplaceLane  uint32 = 0x17
	SimdI16x8ExtractLaneS uint32 = 0x18
	SimdI16x8ExtractLaneU uint32 = 0x19
	SimdI16x8ReplaceLane  uint32 = 0x1A
	SimdI32x4ExtractLane  uint32 = 0x1B
	SimdI32x4ReplaceLane  uint32 = 0x1C
	SimdI64x2ExtractLane  uint32 = 0x1D
	SimdI64x2ReplaceLane  uint32 = 0x1E
	SimdF32x4ExtractLane  uint32 = 0x1F
	SimdF32x4ReplaceLane  uint32 = 0x20
	SimdF64x2ExtractLane  uint32 = 0x21
	SimdF64x2ReplaceLane  uint32 = 0x22
	// Load lane operations
	SimdV128Load8Lane   uint32 = 0x54
	SimdV128Load16Lane  uint32 = 0x55
	SimdV128Load32Lane  uint32 = 0x56
	SimdV128Load64Lane  uint32 = 0x57
	SimdV128Store8Lane  uint32 = 0x58
	SimdV128Store16Lane uint32 = 0x59
	SimdV128Store32Lane uint32 = 0x5A
	SimdV128Store64Lane uint32 = 0x5B
	SimdV128Load32Zero  uint32 = 0x5C
	SimdV128Load64Zero  uint32 = 0x5D

	// i8x16 unary operations
	SimdI8x16Abs     uint32 = 0x60
	SimdI8x16Neg     uint32 = 0x61
	SimdI8x16Popcnt  uint32 = 0x62
	SimdI8x16AllTrue uint32 = 0x63
	SimdI8x16Bitmask uint32 = 0x64

	// f32x4 unary operations
	SimdF32x4Abs  uint32 = 0x67
	SimdF32x4Neg  uint32 = 0x68
	SimdF32x4Sqrt uint32 = 0x69

	// i8x16 binary arithmetic (range 0x6E-0x7B)
	SimdI8x16NarrowI16x8S uint32 = 0x65
	SimdI8x16NarrowI16x8U uint32 = 0x66
	SimdI8x16Shl          uint32 = 0x6B
	SimdI8x16ShrS         uint32 = 0x6C
	SimdI8x16ShrU         uint32 = 0x6D
	SimdI8x16Add          uint32 = 0x6E
	SimdI8x16AddSatS      uint32 = 0x6F
	SimdI8x16AddSatU      uint32 = 0x70
	SimdI8x16Sub          uint32 = 0x71
	SimdI8x16SubSatS      uint32 = 0x72
	SimdI8x16SubSatU      uint32 = 0x73

	// f64x2 rounding operations
	SimdF64x2Ceil    uint32 = 0x74
	SimdF64x2Floor   uint32 = 0x75
	SimdF64x2Trunc   uint32 = 0x76
	SimdF64x2Nearest uint32 = 0x77

	// i16x8 operations
	SimdI16x8ExtAddPairwiseI8x16S uint32 = 0x7C
	SimdI16x8ExtAddPairwiseI8x16U uint32 = 0x7D

	// i16x8 all_true/bitmask
	SimdI16x8AllTrue uint32 = 0x83
	SimdI16x8Bitmask uint32 = 0x84

	// i32x4 all_true/bitmask
	SimdI32x4AllTrue uint32 = 0xA3
	SimdI32x4Bitmask uint32 = 0xA4

	// i64x2 all_true/bitmask
	SimdI64x2AllTrue uint32 = 0xC3
	SimdI64x2Bitmask uint32 = 0xC4

	// v128 bitwise and misc
	SimdV128Not       uint32 = 0x4D
	SimdV128And       uint32 = 0x4E
	SimdV128AndNot    uint32 = 0x4F
	SimdV128Or        uint32 = 0x50
	SimdV128Xor       uint32 = 0x51
	SimdV128Bitselect uint32 = 0x52
	SimdV128AnyTrue   uint32 = 0x53

	// f32x4 rounding operations
	SimdF32x4Ceil    uint32 = 0xE0
	SimdF32x4Floor   uint32 = 0xE1
	SimdF32x4Trunc   uint32 = 0xE2
	SimdF32x4Nearest uint32 = 0xE3

	// f64x2 unary operations
	SimdF64x2Abs  uint32 = 0xEC
	SimdF64x2Neg  uint32 = 0xED
	SimdF64x2Sqrt uint32 = 0xEF
)
