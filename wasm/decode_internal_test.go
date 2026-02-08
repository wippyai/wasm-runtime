package wasm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm/internal/binary"
)

// Unit tests for internal parsing functions with controlled readers

// parseFunctionSection uncovered lines:
// Line 595: return err (count read fails)
// Line 601: return err (func idx read fails)

func TestParseFunctionSection_CountTruncated(t *testing.T) {
	// Empty reader - count read will fail
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseFunctionSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseFunctionSection_FuncIdxTruncated(t *testing.T) {
	// count=2, but only 1 byte follows (not enough for 2 LEB128 values)
	r := binary.NewReader(bytes.NewReader([]byte{
		0x02, // count = 2
		0x00, // first func idx = 0
		// second func idx missing
	}))
	m := &Module{}

	err := parseFunctionSection(r, m)
	if err == nil {
		t.Error("expected error when func idx read fails")
	}
}

// parseDataSection uncovered lines - check function
func TestParseDataSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseDataSection_FlagsTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// flags missing
	}))
	m := &Module{}

	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when flags read fails")
	}
}

func TestParseDataSection_MemIdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x02, // flags = 2 (active with explicit memIdx)
		// memIdx missing
	}))
	m := &Module{}

	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when memIdx read fails")
	}
}

func TestParseDataSection_OffsetTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x00, // flags = 0 (active, table 0)
		// offset expr missing
	}))
	m := &Module{}

	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when offset read fails")
	}
}

func TestParseDataSection_InitLenTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,             // count = 1
		0x00,             // flags = 0
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		// init length missing
	}))
	m := &Module{}

	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when init length read fails")
	}
}

func TestParseDataSection_InitDataTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,             // count = 1
		0x00,             // flags = 0
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		0x05,       // init length = 5
		0xAA, 0xBB, // only 2 bytes (need 5)
	}))
	m := &Module{}

	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when init data read fails")
	}
}

// parseCodeSection uncovered lines
func TestParseCodeSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseCodeSection_BodySizeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// body size missing
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when body size read fails")
	}
}

func TestParseCodeSection_BodyDataTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x05, // body size = 5
		0xAA, // only 1 byte (need 5)
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when body data read fails")
	}
}

func TestParseCodeSection_ValidMinimalBody(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,       // count = 1
		0x02,       // body size = 2
		0x00, 0x0B, // local count = 0, then end opcode (valid minimal body)
	}))
	m := &Module{}

	// This should succeed - minimal valid body
	err := parseCodeSection(r, m)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// Body is empty so internal localCount read fails
func TestParseCodeSection_InternalLocalCountFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x00, // body size = 0 (empty body)
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when internal localCount read fails")
	}
}

// Body has localCount=1 but no local entry data
func TestParseCodeSection_InternalLocalEntryCountFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x01, // body size = 1
		0x01, // localCount = 1 (need 1 entry but no data)
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when local entry count read fails")
	}
}

// Body has localCount=1, entry count, but no type byte
func TestParseCodeSection_InternalLocalTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x02, // body size = 2
		0x01, // localCount = 1
		0x01, // entry count = 1 (but no type byte)
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when local type read fails")
	}
}

// Body has GC ref type (0x63) but no heap type
func TestParseCodeSection_InternalGCRefHeapTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x03, // body size = 3
		0x01, // localCount = 1
		0x01, // entry count = 1
		0x63, // type = ValRefNull (GC ref, needs heap type)
		// heap type missing
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when GC ref heap type read fails")
	}
}

func TestParseCodeSection_LocalEntryTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x03, // body size = 3
		0x01, // local count = 1
		0x02, // local entry: count = 2 (but no type)
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when local entry type read fails")
	}
}

// parseElementSection uncovered lines
func TestParseElementSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseElementSection_FlagsTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// flags missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when flags read fails")
	}
}

func TestParseElementSection_TableIdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x02, // flags = 2 (active, explicit table idx)
		// table idx missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when table idx read fails")
	}
}

func TestParseElementSection_OffsetTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x00, // flags = 0 (active, table 0)
		// offset expr missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when offset read fails")
	}
}

func TestParseElementSection_ElemKindTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x01, // flags = 1 (passive)
		// elemkind missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when elemkind read fails")
	}
}

func TestParseElementSection_VecCountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,             // count = 1
		0x00,             // flags = 0 (active, table 0)
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		// vec count missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when vec count read fails")
	}
}

func TestParseElementSection_FuncIdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,             // count = 1
		0x00,             // flags = 0 (active, table 0)
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		0x02, // vec count = 2
		0x00, // first func idx = 0
		// second func idx missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when func idx read fails")
	}
}

func TestParseElementSection_RefTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x05, // flags = 5 (declarative, with exprs, reftype)
		// reftype missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when reftype read fails")
	}
}

func TestParseElementSection_ExprTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,             // count = 1
		0x04,             // flags = 4 (active, no explicit table, with exprs)
		0x41, 0x00, 0x0B, // offset: i32.const 0, end
		0x01, // vec count = 1
		// expr missing
	}))
	m := &Module{}

	err := parseElementSection(r, m)
	if err == nil {
		t.Error("expected error when expr read fails")
	}
}

// readRefType uncovered lines
func TestReadRefType_ByteTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))

	_, _, err := readRefType(r)
	if err == nil {
		t.Error("expected error when byte read fails")
	}
}

func TestReadRefType_HeapTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x64, // ref (non-nullable) - needs heap type
		// heap type missing
	}))

	_, _, err := readRefType(r)
	if err == nil {
		t.Error("expected error when heap type read fails")
	}
}

// readTableType uncovered lines
func TestReadTableType_RefTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when ref type read fails")
	}
}

func TestReadTableType_LimitsTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x70, // funcref
		// limits missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when limits read fails")
	}
}

// copyInitExprImmediate uncovered lines
func TestCopyInitExprImmediate_LEB128Truncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		// i32.const needs LEB128 immediate, but nothing here
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpI32Const)
	if err == nil {
		t.Error("expected error when LEB128 read fails")
	}
}

func TestCopyInitExprImmediate_F32Truncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0xAA, // only 1 byte, need 4
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpF32Const)
	if err == nil {
		t.Error("expected error when f32 read fails")
	}
}

func TestCopyInitExprImmediate_SIMDSubOpTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		// SIMD prefix needs subop, but nothing here
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixSIMD)
	if err == nil {
		t.Error("expected error when SIMD subop read fails")
	}
}

func TestCopyInitExprImmediate_GCSubOpTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		// GC prefix needs subop, but nothing here
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixGC)
	if err == nil {
		t.Error("expected error when GC subop read fails")
	}
}

// copyBytes uncovered lines
func TestCopyBytes_DataTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0xAA, 0xBB, // only 2 bytes
	}))
	var buf bytes.Buffer

	err := copyBytes(r, &buf, 16) // request 16 bytes
	if err == nil {
		t.Error("expected error when bytes read fails")
	}
}

// parseCodeSection additional coverage
func TestParseCodeSection_SecondBodyFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x02,             // count = 2
		0x02, 0x00, 0x0B, // first body: size=2, 0 locals, end
		// second body missing
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when second body fails")
	}
}

// copyInitExprImmediate additional coverage - GC operations
func TestCopyInitExprImmediate_GCStructNew(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		byte(GCStructNew), // subop
		// typeidx missing
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixGC)
	if err == nil {
		t.Error("expected error when typeidx read fails")
	}
}

func TestCopyInitExprImmediate_GCArrayNewFixed(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		byte(GCArrayNewFixed), // subop
		0x00,                  // typeidx = 0
		// count missing
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixGC)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestCopyInitExprImmediate_GCArrayNewData(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		byte(GCArrayNewData), // subop
		0x00,                 // typeidx = 0
		// dataidx missing
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixGC)
	if err == nil {
		t.Error("expected error when dataidx read fails")
	}
}

func TestCopyInitExprImmediate_V128Const(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		byte(SimdV128Const), // v128.const subop
		0xAA, 0xBB,          // only 2 bytes (need 16)
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixSIMD)
	if err == nil {
		t.Error("expected error when v128 data read fails")
	}
}

func TestCopyInitExprImmediate_SIMDNonV128(t *testing.T) {
	// SIMD prefix with non-v128.const subop - should return nil
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // some other SIMD subop (not v128.const)
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixSIMD)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCopyInitExprImmediate_GCArrayNewFixedTypeIdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		byte(GCArrayNewFixed), // subop
		// typeidx missing
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixGC)
	if err == nil {
		t.Error("expected error when typeidx read fails")
	}
}

func TestCopyInitExprImmediate_GCArrayNewDataTypeIdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		byte(GCArrayNewData), // subop
		// typeidx missing
	}))
	var buf bytes.Buffer

	err := copyInitExprImmediate(r, &buf, OpPrefixGC)
	if err == nil {
		t.Error("expected error when typeidx read fails")
	}
}

func TestCopyInitExprImmediate_UnknownOpcode(t *testing.T) {
	// Unknown opcode - should hit default return nil
	r := binary.NewReader(bytes.NewReader([]byte{}))
	var buf bytes.Buffer

	// Use an opcode that doesn't match any case (like OpNop)
	err := copyInitExprImmediate(r, &buf, OpNop)
	if err != nil {
		t.Errorf("unexpected error for unknown opcode: %v", err)
	}
}

// readTableType additional coverage
func TestReadTableType_LimitsMinTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x70, // funcref
		0x00, // limits flags (no max)
		// min missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when limits min read fails")
	}
}

func TestReadTableType_LimitsMaxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x70, // funcref
		0x01, // limits flags (has max)
		0x00, // min = 0
		// max missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when limits max read fails")
	}
}

func TestReadTableType_GCRefHeapTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x64, // ref (non-nullable)
		// heap type missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when heap type read fails")
	}
}

// readTableType 0x40 prefix (table with init expr) error paths
func TestReadTableType_InitExprZeroTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x40, // init expr prefix
		// 0x00 byte missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when 0x00 byte read fails")
	}
}

func TestReadTableType_InitExprRefTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x40, // init expr prefix
		0x00, // required zero byte
		// ref type missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when ref type read fails")
	}
}

func TestReadTableType_InitExprLimitsTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x40, // init expr prefix
		0x00, // required zero byte
		0x70, // funcref
		// limits missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when limits read fails")
	}
}

func TestReadTableType_InitExprInitTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x40, // init expr prefix
		0x00, // required zero byte
		0x70, // funcref
		0x00, // limits: no max
		0x0A, // limits: min = 10
		// init expr missing
	}))

	_, err := readTableType(r)
	if err == nil {
		t.Error("expected error when init expr read fails")
	}
}

// parseGlobalSection coverage
func TestParseGlobalSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseGlobalSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseGlobalSection_TypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// type missing
	}))
	m := &Module{}

	err := parseGlobalSection(r, m)
	if err == nil {
		t.Error("expected error when type read fails")
	}
}

func TestParseGlobalSection_MutabilityTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x7F, // i32 type
		// mutability missing
	}))
	m := &Module{}

	err := parseGlobalSection(r, m)
	if err == nil {
		t.Error("expected error when mutability read fails")
	}
}

func TestParseGlobalSection_InitExprTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x7F, // i32 type
		0x00, // immutable
		// init expr missing
	}))
	m := &Module{}

	err := parseGlobalSection(r, m)
	if err == nil {
		t.Error("expected error when init expr read fails")
	}
}

// parseExportSection coverage
func TestParseExportSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseExportSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseExportSection_NameTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x03, // name length = 3
		// name missing
	}))
	m := &Module{}

	err := parseExportSection(r, m)
	if err == nil {
		t.Error("expected error when name read fails")
	}
}

func TestParseExportSection_KindTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,       // count = 1
		0x01, 0x66, // name = "f"
		// kind missing
	}))
	m := &Module{}

	err := parseExportSection(r, m)
	if err == nil {
		t.Error("expected error when kind read fails")
	}
}

func TestParseExportSection_IdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,       // count = 1
		0x01, 0x66, // name = "f"
		0x00, // kind = func
		// idx missing
	}))
	m := &Module{}

	err := parseExportSection(r, m)
	if err == nil {
		t.Error("expected error when idx read fails")
	}
}

// parseImportSection coverage
func TestParseImportSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseImportSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseImportSection_ModuleNameTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x05, // module name length = 5
		// module name missing
	}))
	m := &Module{}

	err := parseImportSection(r, m)
	if err == nil {
		t.Error("expected error when module name read fails")
	}
}

func TestParseImportSection_FieldNameTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,       // count = 1
		0x01, 0x6D, // module name = "m"
		0x05, // field name length = 5
		// field name missing
	}))
	m := &Module{}

	err := parseImportSection(r, m)
	if err == nil {
		t.Error("expected error when field name read fails")
	}
}

func TestParseImportSection_KindTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,       // count = 1
		0x01, 0x6D, // module name = "m"
		0x01, 0x66, // field name = "f"
		// kind missing
	}))
	m := &Module{}

	err := parseImportSection(r, m)
	if err == nil {
		t.Error("expected error when kind read fails")
	}
}

func TestParseImportSection_TypeIdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,       // count = 1
		0x01, 0x6D, // module name = "m"
		0x01, 0x66, // field name = "f"
		0x00, // kind = func
		// type idx missing
	}))
	m := &Module{}

	err := parseImportSection(r, m)
	if err == nil {
		t.Error("expected error when type idx read fails")
	}
}

// parseTableSection coverage
func TestParseTableSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseTableSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseTableSection_TableTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// table type missing
	}))
	m := &Module{}

	err := parseTableSection(r, m)
	if err == nil {
		t.Error("expected error when table type read fails")
	}
}

// parseMemorySection coverage
func TestParseMemorySection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseMemorySection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseMemorySection_MemTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// memory type missing
	}))
	m := &Module{}

	err := parseMemorySection(r, m)
	if err == nil {
		t.Error("expected error when memory type read fails")
	}
}

// parseCustomSection coverage
func TestParseCustomSection_NameTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x05, // name length = 5
		// name missing
	}))
	m := &Module{}

	err := parseCustomSection(r, m)
	if err == nil {
		t.Error("expected error when name read fails")
	}
}

// parseTagSection coverage
func TestParseTagSection_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	m := &Module{}

	err := parseTagSection(r, m)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestParseTagSection_TagTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// tag type missing
	}))
	m := &Module{}

	err := parseTagSection(r, m)
	if err == nil {
		t.Error("expected error when tag type read fails")
	}
}

// readLimits coverage
func TestReadLimits_FlagsTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))

	_, err := readLimits(r)
	if err == nil {
		t.Error("expected error when flags read fails")
	}
}

func TestReadLimits_MinTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x00, // flags (no max)
		// min missing
	}))

	_, err := readLimits(r)
	if err == nil {
		t.Error("expected error when min read fails")
	}
}

func TestReadLimits_MaxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // flags (has max)
		0x00, // min = 0
		// max missing
	}))

	_, err := readLimits(r)
	if err == nil {
		t.Error("expected error when max read fails")
	}
}

func TestReadLimits_Memory64MinTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x04, // flags: memory64 (bit 2)
		// 8-byte min missing
	}))

	_, err := readLimits(r)
	if err == nil {
		t.Error("expected error when memory64 min read fails")
	}
}

func TestReadLimits_Memory64MaxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x05,                                           // flags: has max (bit 0) + memory64 (bit 2)
		0x0A, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // min = 10 (8 bytes)
		// 8-byte max missing
	}))

	_, err := readLimits(r)
	if err == nil {
		t.Error("expected error when memory64 max read fails")
	}
}

// readExtValTypes coverage
func TestReadExtValTypes_CountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))

	_, _, err := readExtValTypes(r)
	if err == nil {
		t.Error("expected error when count read fails")
	}
}

func TestReadExtValTypes_TypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// type missing
	}))

	_, _, err := readExtValTypes(r)
	if err == nil {
		t.Error("expected error when type read fails")
	}
}

func TestReadExtValTypes_RefHeapTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x64, // ref (non-nullable)
		// heap type missing
	}))

	_, _, err := readExtValTypes(r)
	if err == nil {
		t.Error("expected error when heap type read fails")
	}
}

// readGlobalType coverage
func TestReadGlobalType_TypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))

	_, err := readGlobalType(r)
	if err == nil {
		t.Error("expected error when type read fails")
	}
}

func TestReadGlobalType_MutabilityTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x7F, // i32
		// mutability missing
	}))

	_, err := readGlobalType(r)
	if err == nil {
		t.Error("expected error when mutability read fails")
	}
}

func TestReadGlobalType_RefHeapTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x64, // ref (non-nullable)
		// heap type missing
	}))

	_, err := readGlobalType(r)
	if err == nil {
		t.Error("expected error when heap type read fails")
	}
}

// parseTypeSection coverage - GC type error paths
func TestParseTypeSection_GCFormReadFails(t *testing.T) {
	// Start with a GC type marker, then truncate in loop
	r := binary.NewReader(bytes.NewReader([]byte{
		0x02, // count = 2
		0x4E, // rec type (triggers hasGCTypes=true, breaks first loop)
		// Reset to start, second loop reads 0x4E then tries to read more
		// but we need to make it fail on second form byte read
	}))
	m := &Module{}

	// This triggers GC mode, then Reset, then second loop fails on form read
	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error")
	}
}

func TestParseTypeSection_RecCountFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x4E, // rec type
		// rec count missing
	}))
	m := &Module{}

	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when rec count read fails")
	}
}

func TestParseTypeSection_SubTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x50, // sub type (0x50)
		// sub type content missing
	}))
	m := &Module{}

	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when subtype read fails")
	}
}

func TestParseTypeSection_ArrayTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x5E, // array type
		// array type content missing
	}))
	m := &Module{}

	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when array type read fails")
	}
}

func TestParseTypeSection_UnsupportedFormInGCMode(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x02, // count = 2
		0x4E, // rec type (triggers GC mode)
		0x01, // rec has 1 sub
		0x4F, // sub final
		0x60, // func
		0x00, // 0 params
		0x00, // 0 results
		0xFF, // second type: invalid form
	}))
	m := &Module{}

	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error for unsupported type form in GC mode")
	}
}

// readSubTypeWithPrefix coverage
func TestReadSubTypeWithPrefix_FuncTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		// FuncTypeByte but truncated
	}))

	_, err := readSubTypeWithPrefix(r, FuncTypeByte)
	if err == nil {
		t.Error("expected error when func type read fails")
	}
}

func TestReadSubTypeWithPrefix_StructTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		// StructTypeByte but truncated (need field count)
	}))

	_, err := readSubTypeWithPrefix(r, StructTypeByte)
	if err == nil {
		t.Error("expected error when struct type read fails")
	}
}

func TestReadSubTypeWithPrefix_ArrayTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		// ArrayTypeByte but truncated (need storage type)
	}))

	_, err := readSubTypeWithPrefix(r, ArrayTypeByte)
	if err == nil {
		t.Error("expected error when array type read fails")
	}
}

func TestReadSubTypeWithPrefix_InvalidForm(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))

	_, err := readSubTypeWithPrefix(r, 0xFF)
	if err == nil {
		t.Error("expected error for invalid form")
	}
}

// readStructType coverage
func TestReadStructType_FieldTypeFails(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // field count = 1
		// field type missing
	}))

	_, err := readStructType(r)
	if err == nil {
		t.Error("expected error when field type read fails")
	}
}

// readFieldType coverage
func TestReadFieldType_MutabilityTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x7F, // i32 storage type
		// mutability byte missing
	}))

	_, err := readFieldType(r)
	if err == nil {
		t.Error("expected error when mutability read fails")
	}
}

// readStorageType coverage
func TestReadStorageType_RefHeapTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x63, // ValRefNull (ref type needs heap type)
		// heap type missing
	}))

	_, err := readStorageType(r)
	if err == nil {
		t.Error("expected error when heap type read fails")
	}
}

// parseCodeSection additional coverage - local entry errors
func TestParseCodeSection_LocalEntryCountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x04, // body size = 4
		0x01, // local count = 1
		// local entry count missing
	}))
	m := &Module{}

	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when local entry count read fails")
	}
}

// readInitExpr coverage
func TestReadInitExpr_OpcodeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))

	_, err := readInitExpr(r)
	if err == nil {
		t.Error("expected error when opcode read fails")
	}
}

func TestReadInitExpr_ImmediateTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x41, // i32.const
		// immediate missing
	}))

	_, err := readInitExpr(r)
	if err == nil {
		t.Error("expected error when immediate read fails")
	}
}

// copyLEB128 coverage
func TestCopyLEB128_Truncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{}))
	var buf bytes.Buffer

	err := copyLEB128(r, &buf)
	if err == nil {
		t.Error("expected error when byte read fails")
	}
}

func TestCopyLEB128_MultiBytesTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x80, // continuation bit set
		// next byte missing
	}))
	var buf bytes.Buffer

	err := copyLEB128(r, &buf)
	if err == nil {
		t.Error("expected error when continuation byte read fails")
	}
}

// decodeSIMDImmediate error paths
func TestDecodeSIMDImmediate_MemArgTruncated(t *testing.T) {
	// SimdV128Load (0x00) needs memarg
	r := bytes.NewReader([]byte{
		0x00, // subop = SimdV128Load
		// memarg missing
	})

	_, err := decodeSIMDImmediate(r)
	if err == nil {
		t.Error("expected error when memarg read fails")
	}
}

func TestDecodeSIMDImmediate_V128ConstTruncated(t *testing.T) {
	// SimdV128Const (0x0C) needs 16 bytes
	r := bytes.NewReader([]byte{
		0x0C,             // subop = SimdV128Const
		0x01, 0x02, 0x03, // only 3 bytes (need 16)
	})

	_, err := decodeSIMDImmediate(r)
	if err == nil {
		t.Error("expected error when v128 const data truncated")
	}
}

func TestDecodeSIMDImmediate_ShuffleTruncated(t *testing.T) {
	// SimdI8x16Shuffle (0x0D) needs 16 lane bytes
	r := bytes.NewReader([]byte{
		0x0D,       // subop = SimdI8x16Shuffle
		0x00, 0x01, // only 2 bytes (need 16)
	})

	_, err := decodeSIMDImmediate(r)
	if err == nil {
		t.Error("expected error when shuffle lanes truncated")
	}
}

func TestDecodeSIMDImmediate_LaneExtractTruncated(t *testing.T) {
	// SimdI8x16ExtractLaneS (0x15) needs 1 lane byte
	r := bytes.NewReader([]byte{
		0x15, // subop = SimdI8x16ExtractLaneS
		// lane index missing
	})

	_, err := decodeSIMDImmediate(r)
	if err == nil {
		t.Error("expected error when lane index truncated")
	}
}

func TestDecodeSIMDImmediate_LaneLoadMemArgTruncated(t *testing.T) {
	// SimdV128Load8Lane (0x54) needs memarg + lane
	r := bytes.NewReader([]byte{
		0x54, // subop = SimdV128Load8Lane
		// memarg missing
	})

	_, err := decodeSIMDImmediate(r)
	if err == nil {
		t.Error("expected error when lane load memarg truncated")
	}
}

func TestDecodeSIMDImmediate_LaneLoadLaneTruncated(t *testing.T) {
	// SimdV128Load8Lane (0x54) needs memarg + lane
	r := bytes.NewReader([]byte{
		0x54,       // subop = SimdV128Load8Lane
		0x00, 0x00, // memarg (align=0, offset=0)
		// lane index missing
	})

	_, err := decodeSIMDImmediate(r)
	if err == nil {
		t.Error("expected error when lane load lane index truncated")
	}
}

func TestDecodeSIMDImmediate_ZeroLoadMemArgTruncated(t *testing.T) {
	// SimdV128Load32Zero (0x5C) needs memarg
	r := bytes.NewReader([]byte{
		0x5C, // subop = SimdV128Load32Zero
		// memarg missing
	})

	_, err := decodeSIMDImmediate(r)
	if err == nil {
		t.Error("expected error when zero load memarg truncated")
	}
}

// decodeAtomicImmediate error paths
func TestDecodeAtomicImmediate_FenceReservedTruncated(t *testing.T) {
	// AtomicFence (0x03) needs reserved byte
	r := bytes.NewReader([]byte{
		0x03, // subop = AtomicFence
		// reserved byte missing
	})

	_, err := decodeAtomicImmediate(r)
	if err == nil {
		t.Error("expected error when fence reserved byte truncated")
	}
}

func TestDecodeAtomicImmediate_MemArgTruncated(t *testing.T) {
	// AtomicNotify (0x00) needs memarg
	r := bytes.NewReader([]byte{
		0x00, // subop = AtomicNotify
		// memarg missing
	})

	_, err := decodeAtomicImmediate(r)
	if err == nil {
		t.Error("expected error when atomic memarg truncated")
	}
}

// decodeGCImmediate error paths
func TestDecodeGCImmediate_SubOpTruncated(t *testing.T) {
	r := bytes.NewReader([]byte{})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when subop truncated")
	}
}

func TestDecodeGCImmediate_StructGetTypeIdxTruncated(t *testing.T) {
	// GCStructGet (0x02) needs typeidx, fieldidx
	r := bytes.NewReader([]byte{
		0x02, // subop = GCStructGet
		// typeidx missing
	})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when struct get typeidx truncated")
	}
}

func TestDecodeGCImmediate_ArrayNewTypeIdxTruncated(t *testing.T) {
	// GCArrayNew (0x06) needs typeidx
	r := bytes.NewReader([]byte{
		0x06, // subop = GCArrayNew
		// typeidx missing
	})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when array new typeidx truncated")
	}
}

func TestDecodeGCImmediate_ArrayNewFixedTypeIdxTruncated(t *testing.T) {
	// GCArrayNewFixed (0x08) needs typeidx, size
	r := bytes.NewReader([]byte{
		0x08, // subop = GCArrayNewFixed
		// typeidx missing
	})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when array new fixed typeidx truncated")
	}
}

func TestDecodeGCImmediate_ArrayNewDataTypeIdxTruncated(t *testing.T) {
	// GCArrayNewData (0x09) needs typeidx, dataidx
	r := bytes.NewReader([]byte{
		0x09, // subop = GCArrayNewData
		// typeidx missing
	})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when array new data typeidx truncated")
	}
}

func TestDecodeGCImmediate_ArrayNewElemTypeIdxTruncated(t *testing.T) {
	// GCArrayNewElem (0x0A) needs typeidx, elemidx
	r := bytes.NewReader([]byte{
		0x0A, // subop = GCArrayNewElem
		// typeidx missing
	})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when array new elem typeidx truncated")
	}
}

func TestDecodeGCImmediate_ArrayCopyTypeIdxTruncated(t *testing.T) {
	// GCArrayCopy (0x11) needs typeidx, typeidx2
	r := bytes.NewReader([]byte{
		0x11, // subop = GCArrayCopy
		// typeidx missing
	})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when array copy typeidx truncated")
	}
}

func TestDecodeGCImmediate_BrOnCastFlagsTruncated(t *testing.T) {
	// GCBrOnCast (0x18) needs flags, labelidx, heaptype, heaptype2
	r := bytes.NewReader([]byte{
		0x18, // subop = GCBrOnCast
		// flags missing
	})

	_, err := decodeGCImmediate(r)
	if err == nil {
		t.Error("expected error when br_on_cast flags truncated")
	}
}

// readMemArg error paths
func TestReadMemArg_MultiMemIdxTruncated(t *testing.T) {
	// align with multi-mem bit set (0x40) but memIdx missing
	r := bytes.NewReader([]byte{
		0x40, // align = 0 with multi-mem bit (0x40)
		// memIdx missing
	})

	_, err := readMemArg(r)
	if err == nil {
		t.Error("expected error when multi-mem memIdx truncated")
	}
}

func TestReadMemArg_OffsetTruncated(t *testing.T) {
	// align without multi-mem bit, but offset missing
	r := bytes.NewReader([]byte{
		0x00, // align = 0
		// offset missing
	})

	_, err := readMemArg(r)
	if err == nil {
		t.Error("expected error when offset truncated")
	}
}

// DecodeInstructions error paths
func TestDecodeInstructions_CatchTagIdxTruncated(t *testing.T) {
	code := []byte{
		OpCatch, // catch
		// tagIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when catch tagIdx truncated")
	}
}

func TestDecodeInstructions_RethrowLabelIdxTruncated(t *testing.T) {
	code := []byte{
		OpRethrow, // rethrow
		// labelIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when rethrow labelIdx truncated")
	}
}

func TestDecodeInstructions_TryTableCatchCountTruncated(t *testing.T) {
	code := []byte{
		OpTryTable, // try_table
		0x40,       // block type (empty)
		// catch count missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when try_table catch count truncated")
	}
}

func TestDecodeInstructions_TryTableCatchKindTruncated(t *testing.T) {
	code := []byte{
		OpTryTable, // try_table
		0x40,       // block type (empty)
		0x01,       // catch count = 1
		// catch kind missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when try_table catch kind truncated")
	}
}

func TestDecodeInstructions_TryTableCatchTagIdxTruncated(t *testing.T) {
	code := []byte{
		OpTryTable, // try_table
		0x40,       // block type (empty)
		0x01,       // catch count = 1
		0x00,       // catch kind = CatchKindCatch
		// tagIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when try_table catch tagIdx truncated")
	}
}

func TestDecodeInstructions_TryTableCatchLabelIdxTruncated(t *testing.T) {
	code := []byte{
		OpTryTable, // try_table
		0x40,       // block type (empty)
		0x01,       // catch count = 1
		0x00,       // catch kind = CatchKindCatch
		0x00,       // tagIdx = 0
		// labelIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when try_table catch labelIdx truncated")
	}
}

func TestDecodeInstructions_BrTableLabelTruncated(t *testing.T) {
	code := []byte{
		OpBrTable, // br_table
		0x02,      // count = 2
		0x00,      // label[0] = 0
		// label[1] missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when br_table label truncated")
	}
}

func TestDecodeInstructions_BrTableDefaultTruncated(t *testing.T) {
	code := []byte{
		OpBrTable, // br_table
		0x01,      // count = 1
		0x00,      // label[0] = 0
		// default missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when br_table default truncated")
	}
}

func TestDecodeInstructions_CallIndirectTableIdxTruncated(t *testing.T) {
	code := []byte{
		OpCallIndirect, // call_indirect
		0x00,           // typeIdx = 0
		// tableIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when call_indirect tableIdx truncated")
	}
}

func TestDecodeInstructions_CallRefTypeIdxTruncated(t *testing.T) {
	code := []byte{
		OpCallRef, // call_ref
		// typeIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when call_ref typeIdx truncated")
	}
}

func TestDecodeInstructions_LocalGetIdxTruncated(t *testing.T) {
	code := []byte{
		OpLocalGet, // local.get
		// localIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when local.get idx truncated")
	}
}

func TestDecodeInstructions_GlobalGetIdxTruncated(t *testing.T) {
	code := []byte{
		OpGlobalGet, // global.get
		// globalIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when global.get idx truncated")
	}
}

func TestDecodeInstructions_TableGetIdxTruncated(t *testing.T) {
	code := []byte{
		OpTableGet, // table.get
		// tableIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when table.get idx truncated")
	}
}

func TestDecodeInstructions_MemorySizeIdxTruncated(t *testing.T) {
	code := []byte{
		OpMemorySize, // memory.size
		// memIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when memory.size idx truncated")
	}
}

func TestDecodeInstructions_F32ConstTruncated(t *testing.T) {
	code := []byte{
		OpF32Const, // f32.const
		0x00, 0x00, // only 2 bytes (need 4)
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when f32.const truncated")
	}
}

func TestDecodeInstructions_F64ConstTruncated(t *testing.T) {
	code := []byte{
		OpF64Const,             // f64.const
		0x00, 0x00, 0x00, 0x00, // only 4 bytes (need 8)
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when f64.const truncated")
	}
}

func TestDecodeInstructions_RefNullHeapTypeTruncated(t *testing.T) {
	code := []byte{
		OpRefNull, // ref.null
		// heapType missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when ref.null heapType truncated")
	}
}

func TestDecodeInstructions_RefFuncIdxTruncated(t *testing.T) {
	code := []byte{
		OpRefFunc, // ref.func
		// funcIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when ref.func funcIdx truncated")
	}
}

func TestDecodeInstructions_BrOnNullLabelTruncated(t *testing.T) {
	code := []byte{
		OpBrOnNull, // br_on_null
		// labelIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when br_on_null labelIdx truncated")
	}
}

func TestDecodeInstructions_SelectTypeCountTruncated(t *testing.T) {
	code := []byte{
		OpSelectType, // select with types
		// count missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when select_type count truncated")
	}
}

func TestDecodeInstructions_SelectTypeTypeByteTruncated(t *testing.T) {
	code := []byte{
		OpSelectType, // select with types
		0x01,         // count = 1
		// type byte missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when select_type type byte truncated")
	}
}

func TestDecodeInstructions_SelectTypeRefHeapTruncated(t *testing.T) {
	code := []byte{
		OpSelectType,     // select with types
		0x01,             // count = 1
		byte(ValRefNull), // ref type
		// heapType missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when select_type ref heapType truncated")
	}
}

func TestDecodeInstructions_MiscSubOpTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		// subop missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when misc subop truncated")
	}
}

func TestDecodeInstructions_MiscMemoryInitDataIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscMemoryInit),
		// dataidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when memory.init dataidx truncated")
	}
}

func TestDecodeInstructions_MiscMemoryInitMemIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscMemoryInit),
		0x00, // dataidx = 0
		// memidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when memory.init memidx truncated")
	}
}

func TestDecodeInstructions_MiscDataDropIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscDataDrop),
		// dataidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when data.drop dataidx truncated")
	}
}

func TestDecodeInstructions_MiscMemoryCopyDstTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscMemoryCopy),
		// dstMem missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when memory.copy dstMem truncated")
	}
}

func TestDecodeInstructions_MiscMemoryCopySrcTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscMemoryCopy),
		0x00, // dstMem = 0
		// srcMem missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when memory.copy srcMem truncated")
	}
}

func TestDecodeInstructions_MiscMemoryFillIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscMemoryFill),
		// memIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when memory.fill memIdx truncated")
	}
}

func TestDecodeInstructions_MiscTableInitElemIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscTableInit),
		// elemidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when table.init elemidx truncated")
	}
}

func TestDecodeInstructions_MiscTableInitTableIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscTableInit),
		0x00, // elemidx = 0
		// tableidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when table.init tableidx truncated")
	}
}

func TestDecodeInstructions_MiscElemDropIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscElemDrop),
		// elemidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when elem.drop elemidx truncated")
	}
}

func TestDecodeInstructions_MiscTableCopyDstTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscTableCopy),
		// dst missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when table.copy dst truncated")
	}
}

func TestDecodeInstructions_MiscTableCopySrcTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscTableCopy),
		0x00, // dst = 0
		// src missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when table.copy src truncated")
	}
}

func TestDecodeInstructions_MiscTableGrowIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscTableGrow),
		// tableidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when table.grow tableidx truncated")
	}
}

func TestDecodeInstructions_MiscMemoryDiscardIdxTruncated(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		byte(MiscMemoryDiscard),
		// memidx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when memory.discard memidx truncated")
	}
}

func TestDecodeInstructions_MiscUnknownSubOp(t *testing.T) {
	code := []byte{
		OpPrefixMisc, // 0xFC prefix
		0xFF,         // unknown sub-opcode
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error for unknown misc sub-opcode")
	}
}

func TestDecodeInstructions_I32LoadMemArgTruncated(t *testing.T) {
	code := []byte{
		OpI32Load, // i32.load
		// memarg missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when i32.load memarg truncated")
	}
}

func TestDecodeInstructions_ThrowTagIdxTruncated(t *testing.T) {
	code := []byte{
		OpThrow, // throw
		// tagIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when throw tagIdx truncated")
	}
}

func TestDecodeInstructions_TryTableBlockTypeTruncated(t *testing.T) {
	code := []byte{
		OpTryTable, // try_table
		// block type missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when try_table block type truncated")
	}
}

func TestDecodeInstructions_ReturnCallIndirectTruncated(t *testing.T) {
	code := []byte{
		OpReturnCallIndirect, // return_call_indirect
		0x00,                 // typeIdx = 0
		// tableIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when return_call_indirect tableIdx truncated")
	}
}

func TestParseTypeSection_UnsupportedTypeForm(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x99, // unsupported type form
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err == nil || !strings.Contains(err.Error(), "unsupported type form") {
		t.Errorf("expected unsupported type form error, got: %v", err)
	}
}

func TestParseTypeSection_FormReadError(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// form byte missing
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when form byte missing")
	}
}

func TestParseTypeSection_RecTypeCountTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,        // count = 1
		RecTypeByte, // rec type
		// recCount missing
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when rec type count truncated")
	}
}

func TestParseTypeSection_RecTypeSubTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,        // count = 1
		RecTypeByte, // rec type
		0x01,        // recCount = 1
		// subtype content missing
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when rec type subtype truncated")
	}
}

func TestParseTypeSection_DirectStructType(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,           // count = 1
		StructTypeByte, // struct type (0x5F)
		0x01,           // 1 field
		byte(ValI32),   // i32 field
		0x00,           // not mutable
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(m.TypeDefs) != 1 || m.TypeDefs[0].Kind != TypeDefKindSub {
		t.Error("expected 1 TypeDef with kind Sub")
	}
}

func TestParseTypeSection_DirectArrayType(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,          // count = 1
		ArrayTypeByte, // array type (0x5E)
		byte(ValI32),  // i32 element type
		0x00,          // not mutable
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(m.TypeDefs) != 1 || m.TypeDefs[0].Kind != TypeDefKindSub {
		t.Error("expected 1 TypeDef with kind Sub")
	}
}

func TestParseTypeSection_DirectStructTypeError(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,           // count = 1
		StructTypeByte, // struct type (0x5F)
		// fields missing
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when struct type fields missing")
	}
}

func TestParseTypeSection_DirectArrayTypeError(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,          // count = 1
		ArrayTypeByte, // array type (0x5E)
		// element type missing
	}))
	m := &Module{}
	err := parseTypeSection(r, m)
	if err == nil {
		t.Error("expected error when array type element missing")
	}
}

func TestParseCustomSection_NameReadError(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x03,       // name length = 3
		0x61, 0x62, // only "ab", missing 'c'
	}))
	m := &Module{}
	err := parseCustomSection(r, m)
	if err == nil {
		t.Error("expected error when custom section name truncated")
	}
}

func TestSkipFuncType_RefParamHeapTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,             // paramCount = 1
		byte(ValRefNull), // ref type param
		// heapType missing
	}))
	err := skipFuncType(r)
	if err == nil {
		t.Error("expected error when ref param heapType truncated")
	}
}

func TestSkipFuncType_RefResultHeapTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x00,         // paramCount = 0
		0x01,         // resultCount = 1
		byte(ValRef), // ref type result
		// heapType missing
	}))
	err := skipFuncType(r)
	if err == nil {
		t.Error("expected error when ref result heapType truncated")
	}
}

func TestParseDataSection_InvalidFlags(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x03, // invalid flags (> 2)
	}))
	m := &Module{}
	err := parseDataSection(r, m)
	if err == nil || !strings.Contains(err.Error(), "invalid data segment flags") {
		t.Errorf("expected invalid flags error, got: %v", err)
	}
}

func TestParseDataSection_Flags2MemIdxTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x02, // flags = 2 (active with memIdx)
		// memIdx missing
	}))
	m := &Module{}
	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when memIdx truncated for flags=2")
	}
}

func TestParseDataSection_ActiveOffsetTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x00, // flags = 0 (active with memIdx=0)
		// offset init expr missing
	}))
	m := &Module{}
	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when offset truncated")
	}
}

func TestParseDataSection_PassiveInitLenTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x01, // flags = 1 (passive, no offset)
		// initLen missing
	}))
	m := &Module{}
	err := parseDataSection(r, m)
	if err == nil {
		t.Error("expected error when initLen truncated")
	}
}

func TestParseCodeSection_BodyReadTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		0x05, // body size = 5
		0x00, // locals = 0
		// body bytes missing (need 4 more bytes)
	}))
	m := &Module{}
	err := parseCodeSection(r, m)
	if err == nil {
		t.Error("expected error when code body truncated")
	}
}

func TestReadExtValTypes_TypeByteTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01, // count = 1
		// type byte missing
	}))
	_, _, err := readExtValTypes(r)
	if err == nil {
		t.Error("expected error when type byte truncated")
	}
}

func TestReadExtValTypes_RefHeapTypeTruncated(t *testing.T) {
	r := binary.NewReader(bytes.NewReader([]byte{
		0x01,             // count = 1
		byte(ValRefNull), // ref type
		// heapType missing
	}))
	_, _, err := readExtValTypes(r)
	if err == nil {
		t.Error("expected error when ref heapType truncated")
	}
}

func TestDecodeInstructions_I64ConstTruncated(t *testing.T) {
	code := []byte{
		OpI64Const, // i64.const
		// LEB128 value truncated (need continuation)
		0x80, // has continuation bit but no more bytes
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when i64.const truncated")
	}
}

func TestDecodeInstructions_DelegateIdxTruncated(t *testing.T) {
	code := []byte{
		OpDelegate, // delegate
		// labelIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when delegate labelIdx truncated")
	}
}

func TestDecodeInstructions_ReturnCallRefTruncated(t *testing.T) {
	code := []byte{
		OpReturnCallRef, // return_call_ref
		// typeIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when return_call_ref typeIdx truncated")
	}
}

func TestDecodeInstructions_BlockBlockTypeTruncated(t *testing.T) {
	code := []byte{
		OpBlock, // block
		// block type missing (need LEB128 signed)
		0x80, // has continuation bit but no more bytes
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when block block type truncated")
	}
}

func TestDecodeInstructions_UnknownOpcode(t *testing.T) {
	code := []byte{
		0xFF, // unknown opcode
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error for unknown opcode")
	}
}

func TestDecodeInstructions_ReturnCallFuncIdxTruncated(t *testing.T) {
	code := []byte{
		OpReturnCall, // return_call
		// funcIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when return_call funcIdx truncated")
	}
}

func TestDecodeInstructions_BrOnNonNullLabelTruncated(t *testing.T) {
	code := []byte{
		OpBrOnNonNull, // br_on_non_null
		// labelIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when br_on_non_null labelIdx truncated")
	}
}

func TestDecodeInstructions_CallFuncIdxTruncated(t *testing.T) {
	code := []byte{
		OpCall, // call
		// funcIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when call funcIdx truncated")
	}
}

func TestDecodeInstructions_BrIdxTruncated(t *testing.T) {
	code := []byte{
		OpBr, // br
		// labelIdx missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when br labelIdx truncated")
	}
}

func TestDecodeInstructions_BrTableCountTruncated(t *testing.T) {
	code := []byte{
		OpBrTable, // br_table
		// count missing
	}

	_, err := DecodeInstructions(code)
	if err == nil {
		t.Error("expected error when br_table count truncated")
	}
}
