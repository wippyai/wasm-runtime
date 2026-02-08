package opcode

type ImmKind int

const (
	ImmNone   ImmKind = iota
	ImmU32            // local.get, br, call, etc.
	ImmI32            // i32.const
	ImmI64            // i64.const
	ImmF32            // f32.const
	ImmF64            // f64.const
	ImmBlock          // block type
	ImmMemarg         // memory operations (align, offset)
	ImmMemIdx         // memory.size/grow (memory index)
)

type Info struct {
	Opcode   byte
	Operands int // number of stack operands for folded form (-1 = variable)
	ImmType  ImmKind
}

func Lookup(name string) (Info, bool) {
	info, ok := table[name]
	return info, ok
}

type MemoryOp struct {
	Opcode       byte
	NaturalAlign uint32
	Operands     int
}

func LookupMemory(name string) (MemoryOp, bool) {
	op, ok := memoryOps[name]
	return op, ok
}

type PrefixedOp struct {
	Subop    uint32
	Operands int
}

func LookupPrefixed(name string) (PrefixedOp, bool) {
	op, ok := prefixedOps[name]
	return op, ok
}

var table = map[string]Info{
	// Control
	"unreachable": {0x00, 0, ImmNone},
	"nop":         {0x01, 0, ImmNone},
	"return":      {0x0F, -1, ImmNone},

	// Variables
	"local.get":  {0x20, 0, ImmU32},
	"local.set":  {0x21, 1, ImmU32},
	"local.tee":  {0x22, 1, ImmU32},
	"global.get": {0x23, 0, ImmU32},
	"global.set": {0x24, 1, ImmU32},

	// Constants
	"i32.const": {0x41, 0, ImmI32},
	"i64.const": {0x42, 0, ImmI64},
	"f32.const": {0x43, 0, ImmF32},
	"f64.const": {0x44, 0, ImmF64},

	// i32 comparison
	"i32.eqz":  {0x45, 1, ImmNone},
	"i32.eq":   {0x46, 2, ImmNone},
	"i32.ne":   {0x47, 2, ImmNone},
	"i32.lt_s": {0x48, 2, ImmNone},
	"i32.lt_u": {0x49, 2, ImmNone},
	"i32.gt_s": {0x4A, 2, ImmNone},
	"i32.gt_u": {0x4B, 2, ImmNone},
	"i32.le_s": {0x4C, 2, ImmNone},
	"i32.le_u": {0x4D, 2, ImmNone},
	"i32.ge_s": {0x4E, 2, ImmNone},
	"i32.ge_u": {0x4F, 2, ImmNone},

	// i64 comparison
	"i64.eqz":  {0x50, 1, ImmNone},
	"i64.eq":   {0x51, 2, ImmNone},
	"i64.ne":   {0x52, 2, ImmNone},
	"i64.lt_s": {0x53, 2, ImmNone},
	"i64.lt_u": {0x54, 2, ImmNone},
	"i64.gt_s": {0x55, 2, ImmNone},
	"i64.gt_u": {0x56, 2, ImmNone},
	"i64.le_s": {0x57, 2, ImmNone},
	"i64.le_u": {0x58, 2, ImmNone},
	"i64.ge_s": {0x59, 2, ImmNone},
	"i64.ge_u": {0x5A, 2, ImmNone},

	// f32 comparison
	"f32.eq": {0x5B, 2, ImmNone},
	"f32.ne": {0x5C, 2, ImmNone},
	"f32.lt": {0x5D, 2, ImmNone},
	"f32.gt": {0x5E, 2, ImmNone},
	"f32.le": {0x5F, 2, ImmNone},
	"f32.ge": {0x60, 2, ImmNone},

	// f64 comparison
	"f64.eq": {0x61, 2, ImmNone},
	"f64.ne": {0x62, 2, ImmNone},
	"f64.lt": {0x63, 2, ImmNone},
	"f64.gt": {0x64, 2, ImmNone},
	"f64.le": {0x65, 2, ImmNone},
	"f64.ge": {0x66, 2, ImmNone},

	// i32 unary
	"i32.clz":    {0x67, 1, ImmNone},
	"i32.ctz":    {0x68, 1, ImmNone},
	"i32.popcnt": {0x69, 1, ImmNone},

	// i32 binary
	"i32.add":   {0x6A, 2, ImmNone},
	"i32.sub":   {0x6B, 2, ImmNone},
	"i32.mul":   {0x6C, 2, ImmNone},
	"i32.div_s": {0x6D, 2, ImmNone},
	"i32.div_u": {0x6E, 2, ImmNone},
	"i32.rem_s": {0x6F, 2, ImmNone},
	"i32.rem_u": {0x70, 2, ImmNone},
	"i32.and":   {0x71, 2, ImmNone},
	"i32.or":    {0x72, 2, ImmNone},
	"i32.xor":   {0x73, 2, ImmNone},
	"i32.shl":   {0x74, 2, ImmNone},
	"i32.shr_s": {0x75, 2, ImmNone},
	"i32.shr_u": {0x76, 2, ImmNone},
	"i32.rotl":  {0x77, 2, ImmNone},
	"i32.rotr":  {0x78, 2, ImmNone},

	// i64 unary
	"i64.clz":    {0x79, 1, ImmNone},
	"i64.ctz":    {0x7A, 1, ImmNone},
	"i64.popcnt": {0x7B, 1, ImmNone},

	// i64 binary
	"i64.add":   {0x7C, 2, ImmNone},
	"i64.sub":   {0x7D, 2, ImmNone},
	"i64.mul":   {0x7E, 2, ImmNone},
	"i64.div_s": {0x7F, 2, ImmNone},
	"i64.div_u": {0x80, 2, ImmNone},
	"i64.rem_s": {0x81, 2, ImmNone},
	"i64.rem_u": {0x82, 2, ImmNone},
	"i64.and":   {0x83, 2, ImmNone},
	"i64.or":    {0x84, 2, ImmNone},
	"i64.xor":   {0x85, 2, ImmNone},
	"i64.shl":   {0x86, 2, ImmNone},
	"i64.shr_s": {0x87, 2, ImmNone},
	"i64.shr_u": {0x88, 2, ImmNone},
	"i64.rotl":  {0x89, 2, ImmNone},
	"i64.rotr":  {0x8A, 2, ImmNone},

	// f32 unary
	"f32.abs":     {0x8B, 1, ImmNone},
	"f32.neg":     {0x8C, 1, ImmNone},
	"f32.ceil":    {0x8D, 1, ImmNone},
	"f32.floor":   {0x8E, 1, ImmNone},
	"f32.trunc":   {0x8F, 1, ImmNone},
	"f32.nearest": {0x90, 1, ImmNone},
	"f32.sqrt":    {0x91, 1, ImmNone},

	// f32 binary
	"f32.add":      {0x92, 2, ImmNone},
	"f32.sub":      {0x93, 2, ImmNone},
	"f32.mul":      {0x94, 2, ImmNone},
	"f32.div":      {0x95, 2, ImmNone},
	"f32.min":      {0x96, 2, ImmNone},
	"f32.max":      {0x97, 2, ImmNone},
	"f32.copysign": {0x98, 2, ImmNone},

	// f64 unary
	"f64.abs":     {0x99, 1, ImmNone},
	"f64.neg":     {0x9A, 1, ImmNone},
	"f64.ceil":    {0x9B, 1, ImmNone},
	"f64.floor":   {0x9C, 1, ImmNone},
	"f64.trunc":   {0x9D, 1, ImmNone},
	"f64.nearest": {0x9E, 1, ImmNone},
	"f64.sqrt":    {0x9F, 1, ImmNone},

	// f64 binary
	"f64.add":      {0xA0, 2, ImmNone},
	"f64.sub":      {0xA1, 2, ImmNone},
	"f64.mul":      {0xA2, 2, ImmNone},
	"f64.div":      {0xA3, 2, ImmNone},
	"f64.min":      {0xA4, 2, ImmNone},
	"f64.max":      {0xA5, 2, ImmNone},
	"f64.copysign": {0xA6, 2, ImmNone},

	// Conversions
	"i32.wrap_i64":        {0xA7, 1, ImmNone},
	"i32.trunc_f32_s":     {0xA8, 1, ImmNone},
	"i32.trunc_f32_u":     {0xA9, 1, ImmNone},
	"i32.trunc_f64_s":     {0xAA, 1, ImmNone},
	"i32.trunc_f64_u":     {0xAB, 1, ImmNone},
	"i64.extend_i32_s":    {0xAC, 1, ImmNone},
	"i64.extend_i32_u":    {0xAD, 1, ImmNone},
	"i64.trunc_f32_s":     {0xAE, 1, ImmNone},
	"i64.trunc_f32_u":     {0xAF, 1, ImmNone},
	"i64.trunc_f64_s":     {0xB0, 1, ImmNone},
	"i64.trunc_f64_u":     {0xB1, 1, ImmNone},
	"f32.convert_i32_s":   {0xB2, 1, ImmNone},
	"f32.convert_i32_u":   {0xB3, 1, ImmNone},
	"f32.convert_i64_s":   {0xB4, 1, ImmNone},
	"f32.convert_i64_u":   {0xB5, 1, ImmNone},
	"f32.demote_f64":      {0xB6, 1, ImmNone},
	"f64.convert_i32_s":   {0xB7, 1, ImmNone},
	"f64.convert_i32_u":   {0xB8, 1, ImmNone},
	"f64.convert_i64_s":   {0xB9, 1, ImmNone},
	"f64.convert_i64_u":   {0xBA, 1, ImmNone},
	"f64.promote_f32":     {0xBB, 1, ImmNone},
	"i32.reinterpret_f32": {0xBC, 1, ImmNone},
	"i64.reinterpret_f64": {0xBD, 1, ImmNone},
	"f32.reinterpret_i32": {0xBE, 1, ImmNone},
	"f64.reinterpret_i64": {0xBF, 1, ImmNone},

	// Sign extension
	"i32.extend8_s":  {0xC0, 1, ImmNone},
	"i32.extend16_s": {0xC1, 1, ImmNone},
	"i64.extend8_s":  {0xC2, 1, ImmNone},
	"i64.extend16_s": {0xC3, 1, ImmNone},
	"i64.extend32_s": {0xC4, 1, ImmNone},

	// Parametric
	"drop": {0x1A, 1, ImmNone},

	// Control with immediates
	"br":    {0x0C, -1, ImmU32},
	"br_if": {0x0D, -1, ImmU32},
	"call":  {0x10, -1, ImmU32},

	// Tail call
	"return_call": {0x12, -1, ImmU32},

	// Memory
	"memory.size": {0x3F, 0, ImmMemIdx},
	"memory.grow": {0x40, 1, ImmMemIdx},

	// Reference types
	"ref.is_null": {0xD1, 1, ImmNone},
}

var prefixedOps = map[string]PrefixedOp{
	// Saturating truncation
	"i32.trunc_sat_f32_s": {0, 1},
	"i32.trunc_sat_f32_u": {1, 1},
	"i32.trunc_sat_f64_s": {2, 1},
	"i32.trunc_sat_f64_u": {3, 1},
	"i64.trunc_sat_f32_s": {4, 1},
	"i64.trunc_sat_f32_u": {5, 1},
	"i64.trunc_sat_f64_s": {6, 1},
	"i64.trunc_sat_f64_u": {7, 1},

	// Bulk memory
	"memory.init": {8, 3},
	"data.drop":   {9, 0},
	"memory.copy": {10, 3},
	"memory.fill": {11, 3},

	// Table operations
	"elem.drop":  {13, 0},
	"table.init": {12, 3},
	"table.copy": {14, 5},
	"table.grow": {15, 2},
	"table.size": {16, 0},
	"table.fill": {17, 3},
}

var memoryOps = map[string]MemoryOp{
	// Loads
	"i32.load":     {0x28, 2, 1},
	"i64.load":     {0x29, 3, 1},
	"f32.load":     {0x2A, 2, 1},
	"f64.load":     {0x2B, 3, 1},
	"i32.load8_s":  {0x2C, 0, 1},
	"i32.load8_u":  {0x2D, 0, 1},
	"i32.load16_s": {0x2E, 1, 1},
	"i32.load16_u": {0x2F, 1, 1},
	"i64.load8_s":  {0x30, 0, 1},
	"i64.load8_u":  {0x31, 0, 1},
	"i64.load16_s": {0x32, 1, 1},
	"i64.load16_u": {0x33, 1, 1},
	"i64.load32_s": {0x34, 2, 1},
	"i64.load32_u": {0x35, 2, 1},

	// Stores
	"i32.store":   {0x36, 2, 2},
	"i64.store":   {0x37, 3, 2},
	"f32.store":   {0x38, 2, 2},
	"f64.store":   {0x39, 3, 2},
	"i32.store8":  {0x3A, 0, 2},
	"i32.store16": {0x3B, 1, 2},
	"i64.store8":  {0x3C, 0, 2},
	"i64.store16": {0x3D, 1, 2},
	"i64.store32": {0x3E, 2, 2},
}
