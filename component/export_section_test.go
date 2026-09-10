package component

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeExports_PackedFuncNamesConsumeOptionType(t *testing.T) {
	payload := concatBytes(
		[]byte{0x02},
		exportPlain("call-peek", SortFunc, 2, nil),
		exportPlain("call-mirror", SortFunc, 3, nil),
	)
	got, err := decodeExports(payload)
	if err != nil {
		t.Fatalf("decode packed exports: %v", err)
	}
	want := []struct {
		name  string
		sort  byte
		index uint32
	}{
		{"call-peek", SortFunc, 2},
		{"call-mirror", SortFunc, 3},
	}
	if len(got) != len(want) {
		t.Fatalf("exports = %+v, want %d entries", got, len(want))
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].Sort != w.sort || got[i].SortIndex != w.index {
			t.Errorf("exports[%d] = {%q sort=%d idx=%d}, want {%q sort=%d idx=%d}",
				i, got[i].Name, got[i].Sort, got[i].SortIndex, w.name, w.sort, w.index)
		}
		if got[i].ExternType != nil {
			t.Errorf("exports[%d].ExternType = %+v, want none", i, got[i].ExternType)
		}
	}
}

func TestDecodeExports_MissingOptionByteIsInvalid(t *testing.T) {
	payload := []byte{
		0x01,
		0x00, 0x09, 'c', 'a', 'l', 'l', '-', 'p', 'e', 'e', 'k',
		0x01, 0x02,
	}
	if _, err := decodeExports(payload); err == nil {
		t.Fatal("expected error for export missing externtype option byte")
	}
}

func TestDecodeExports_TrailingBytes(t *testing.T) {
	payload := concatBytes(
		[]byte{0x01},
		exportPlain("call-peek", SortFunc, 2, nil),
		[]byte{0x00},
	)
	if _, err := decodeExports(payload); err == nil {
		t.Fatal("expected error for trailing bytes after export vector")
	}
}

func TestDecodeExports_Truncated(t *testing.T) {
	payload := concatBytes(
		[]byte{0x02},
		exportPlain("call-peek", SortFunc, 2, nil),
	)
	if _, err := decodeExports(payload); err == nil {
		t.Fatal("expected error for truncated export vector")
	}
}

func TestDecodeExports_MalformedTags(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{
			name: "unknown nameattributes",
			payload: []byte{
				0x01,
				0x03, 0x03, 'f', 'o', 'o',
				0x01, 0x00, 0x00,
			},
		},
		{
			name: "unknown externtype option",
			payload: concatBytes(
				[]byte{0x01},
				[]byte{0x00, 0x03, 'f', 'o', 'o', 0x01, 0x00, 0x02},
			),
		},
		{
			name: "unknown sort",
			payload: []byte{
				0x01,
				0x00, 0x03, 'f', 'o', 'o',
				0x0b, 0x00, 0x00,
			},
		},
		{
			name: "unknown core sort",
			payload: []byte{
				0x01,
				0x00, 0x03, 'm', 'o', 'd',
				0x00, 0x13, 0x00, 0x00,
			},
		},
		{
			name: "unknown attribute kind",
			payload: []byte{
				0x01,
				0x02, 0x03, 'f', 'o', 'o',
				0x01, 0x03, 0x01, 'x',
				0x01, 0x00, 0x00,
			},
		},
		{
			name: "duplicate attribute kind",
			payload: []byte{
				0x01,
				0x02, 0x03, 'f', 'o', 'o',
				0x02,
				0x00, 0x01, 'a',
				0x00, 0x01, 'b',
				0x01, 0x00, 0x00,
			},
		},
		{
			name: "unknown externtype kind",
			payload: []byte{
				0x01,
				0x00, 0x03, 'f', 'o', 'o',
				0x01, 0x00, 0x01, 0x06, 0x00,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeExports(tt.payload); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDecodeExports_NameKind01PreservesName(t *testing.T) {
	payload := []byte{
		0x01,
		0x01, 0x12, 'w', 'a', 's', 'i', ':', 'c', 'l', 'i', '/', 'r', 'u', 'n', '@', '0', '.', '2', '.', '0',
		0x01, 0x00, 0x00,
	}
	got, err := decodeExports(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].NameKind != 0x01 {
		t.Errorf("NameKind = %d, want 1", got[0].NameKind)
	}
	if got[0].Name != "wasi:cli/run@0.2.0" {
		t.Errorf("Name = %q, want versioned interface name", got[0].Name)
	}
}

func TestDecodeExports_AnnotationsAndPlainPacked(t *testing.T) {
	payload := concatBytes(
		[]byte{0x03},
		[]byte{0x02}, encodeExportString("primary"),
		[]byte{0x02},
		[]byte{ExportAttrImplements}, encodeExportString("wasi:keyvalue/store"),
		[]byte{ExportAttrExternalID}, encodeExportString("https://example.com/kv"),
		[]byte{SortInstance, 0x00, 0x00},
		[]byte{0x02}, encodeExportString("wasi:cli/run@0.2"),
		[]byte{0x01},
		[]byte{ExportAttrVersionSuffix}, encodeExportString(".0"),
		[]byte{SortFunc, 0x00, 0x00},
		exportPlain("plain", SortFunc, 1, nil),
	)
	got, err := decodeExports(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	if got[0].Name != "primary" || got[0].NameKind != 0x02 || got[0].Sort != SortInstance {
		t.Errorf("first = %+v", got[0])
	}
	if len(got[0].Attributes) != 2 {
		t.Fatalf("first attrs = %+v", got[0].Attributes)
	}
	if got[0].Attributes[0] != (ExportAttribute{Kind: ExportAttrImplements, Value: "wasi:keyvalue/store"}) {
		t.Errorf("implements = %+v", got[0].Attributes[0])
	}
	if got[0].Attributes[1] != (ExportAttribute{Kind: ExportAttrExternalID, Value: "https://example.com/kv"}) {
		t.Errorf("external-id = %+v", got[0].Attributes[1])
	}
	if got[1].Name != "wasi:cli/run@0.2" || len(got[1].Attributes) != 1 {
		t.Fatalf("second = %+v", got[1])
	}
	if got[1].Attributes[0] != (ExportAttribute{Kind: ExportAttrVersionSuffix, Value: ".0"}) {
		t.Errorf("versionsuffix = %+v", got[1].Attributes[0])
	}
	if got[2].Name != "plain" || got[2].NameKind != 0x00 || len(got[2].Attributes) != 0 {
		t.Errorf("plain = %+v", got[2])
	}
}

func TestDecodeExports_ExplicitExternType(t *testing.T) {
	payload := concatBytes(
		[]byte{0x02},
		exportPlain("ascribed", SortFunc, 0, &ExportExternType{Kind: ExternFunc, TypeIndex: 4}),
		exportPlain("plain", SortFunc, 0, nil),
	)
	got, err := decodeExports(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ExternType == nil || got[0].ExternType.Kind != ExternFunc || got[0].ExternType.TypeIndex != 4 {
		t.Errorf("ascribed type = %+v", got[0].ExternType)
	}
	if got[1].ExternType != nil {
		t.Errorf("plain type = %+v, want none", got[1].ExternType)
	}
}

func TestDecodeExports_TypeBoundAndValueBound(t *testing.T) {
	typeEq := concatBytes(
		[]byte{0x01},
		[]byte{0x00, 0x01, 't'},
		[]byte{SortType, 0x00, 0x01, ExternType, 0x00, 0x03},
	)
	got, err := decodeExports(typeEq)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ExternType == nil {
		t.Fatalf("got %+v", got)
	}
	if !got[0].ExternType.HasBound || got[0].ExternType.BoundKind != 0x00 || got[0].ExternType.TypeIndex != 3 {
		t.Errorf("type eq = %+v", got[0].ExternType)
	}

	valueTy := concatBytes(
		[]byte{0x01},
		[]byte{0x00, 0x01, 'v'},
		[]byte{SortValue, 0x00, 0x01, ExternValue, 0x01, 0x79},
	)
	got, err = decodeExports(valueTy)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ExternType == nil || got[0].ExternType.BoundKind != 0x01 {
		t.Fatalf("value bound = %+v", got[0].ExternType)
	}
	prim, ok := got[0].ExternType.ValueType.(PrimValType)
	if !ok || prim.Type != PrimU32 {
		t.Errorf("value type = %T %+v, want u32", got[0].ExternType.ValueType, got[0].ExternType.ValueType)
	}
}

func TestDecodeExports_CoreModuleSort(t *testing.T) {
	payload := []byte{
		0x01,
		0x00, 0x03, 'l', 'i', 'b',
		SortCore, 0x11, 0x00,
		0x00,
	}
	got, err := decodeExports(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "lib" || got[0].Sort != SortCore || got[0].CoreSort != 0x11 || got[0].SortIndex != 0 {
		t.Errorf("got %+v", got[0])
	}
}

func TestDecodeWithOptions_PackedCanonExportNames(t *testing.T) {
	data, err := os.ReadFile("testdata/packed_canon.wasm")
	if err != nil {
		t.Fatal(err)
	}
	comp, err := DecodeWithOptions(data, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"call-peek", "call-mirror"}
	if len(comp.Exports) != len(want) {
		t.Fatalf("exports = %+v, want %v", comp.Exports, want)
	}
	for i, name := range want {
		if comp.Exports[i].Name != name || comp.Exports[i].Sort != SortFunc {
			t.Errorf("exports[%d] = {%q sort=%d idx=%d}, want %q func",
				i, comp.Exports[i].Name, comp.Exports[i].Sort, comp.Exports[i].SortIndex, name)
		}
	}
}

func TestDecodeWithOptions_WasmToolsPackedExportsNoAnnotations(t *testing.T) {
	comp := decodeTestdataComponent(t, "packed_exports.wasm")
	want := []struct {
		name     string
		sort     byte
		coreSort byte
	}{
		{"lib", SortCore, 0x11},
		{"call-peek", SortFunc, 0},
		{"the-type", SortType, 0},
		{"host", SortInstance, 0},
		{"wasi:cli/run@0.2.0", SortFunc, 0},
	}
	if len(comp.Exports) != len(want) {
		t.Fatalf("exports = %+v, want %d", comp.Exports, len(want))
	}
	for i, w := range want {
		exp := comp.Exports[i]
		if exp.Name != w.name || exp.Sort != w.sort || exp.CoreSort != w.coreSort {
			t.Errorf("exports[%d] = {%q sort=%d core=%d}, want {%q sort=%d core=%d}",
				i, exp.Name, exp.Sort, exp.CoreSort, w.name, w.sort, w.coreSort)
		}
		if exp.NameKind != 0x00 {
			t.Errorf("exports[%d].NameKind = %d, want 0", i, exp.NameKind)
		}
		if len(exp.Attributes) != 0 {
			t.Errorf("exports[%d].Attributes = %+v", i, exp.Attributes)
		}
		if exp.ExternType != nil {
			t.Errorf("exports[%d].ExternType = %+v, want none", i, exp.ExternType)
		}
	}
}

func TestDecodeWithOptions_WasmToolsPackedExportsWithAscription(t *testing.T) {
	comp := decodeTestdataComponent(t, "packed_exports_ascribed.wasm")
	if len(comp.Exports) != 3 {
		t.Fatalf("exports = %+v, want 3", comp.Exports)
	}
	if comp.Exports[0].Name != "ascribed" || comp.Exports[0].ExternType == nil {
		t.Fatalf("ascribed = %+v", comp.Exports[0])
	}
	if comp.Exports[0].ExternType.Kind != ExternFunc || comp.Exports[0].ExternType.TypeIndex != 0 {
		t.Errorf("ascribed type = %+v", comp.Exports[0].ExternType)
	}
	if comp.Exports[1].Name != "plain" || comp.Exports[1].ExternType != nil {
		t.Errorf("plain = %+v", comp.Exports[1])
	}
	if comp.Exports[2].Name != "the-type" || comp.Exports[2].ExternType == nil {
		t.Fatalf("the-type = %+v", comp.Exports[2])
	}
	et := comp.Exports[2].ExternType
	if et.Kind != ExternType || !et.HasBound || et.BoundKind != 0x00 || et.TypeIndex != 0 {
		t.Errorf("the-type externtype = %+v", et)
	}
}

func TestDecodeWithOptions_PackedExportsWithAnnotations(t *testing.T) {
	comp := decodeTestdataComponent(t, "packed_exports_annotations.wasm")
	if len(comp.Exports) != 4 {
		t.Fatalf("exports = %+v, want 4", comp.Exports)
	}
	primary := comp.Exports[0]
	if primary.Name != "primary" || primary.NameKind != 0x02 || primary.Sort != SortInstance {
		t.Errorf("primary = %+v", primary)
	}
	if len(primary.Attributes) != 2 ||
		primary.Attributes[0].Kind != ExportAttrImplements ||
		primary.Attributes[0].Value != "wasi:keyvalue/store" ||
		primary.Attributes[1].Kind != ExportAttrExternalID ||
		primary.Attributes[1].Value != "https://example.com/kv" {
		t.Errorf("primary attrs = %+v", primary.Attributes)
	}
	versioned := comp.Exports[1]
	if versioned.Name != "wasi:cli/run@0.2" || len(versioned.Attributes) != 1 ||
		versioned.Attributes[0] != (ExportAttribute{Kind: ExportAttrVersionSuffix, Value: ".0"}) {
		t.Errorf("versioned = %+v", versioned)
	}
	if comp.Exports[2].Name != "legacy" || comp.Exports[2].NameKind != 0x01 {
		t.Errorf("legacy = %+v", comp.Exports[2])
	}
	if comp.Exports[3].Name != "plain" || comp.Exports[3].NameKind != 0x00 || len(comp.Exports[3].Attributes) != 0 {
		t.Errorf("plain = %+v", comp.Exports[3])
	}
}

func decodeTestdataComponent(t *testing.T, name string) *Component {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	comp, err := DecodeWithOptions(data, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return comp
}

func exportPlain(name string, sort byte, index uint32, et *ExportExternType) []byte {
	out := []byte{0x00}
	out = append(out, encodeExportString(name)...)
	out = append(out, sort)
	out = append(out, encodeULEB(index)...)
	if et == nil {
		return append(out, 0x00)
	}
	out = append(out, 0x01, et.Kind)
	out = append(out, encodeULEB(et.TypeIndex)...)
	return out
}

func encodeExportString(s string) []byte {
	b := []byte(s)
	out := encodeULEB(uint32(len(b)))
	return append(out, b...)
}

func concatBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
