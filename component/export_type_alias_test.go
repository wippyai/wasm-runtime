package component

import (
	"os"
	"strings"
	"testing"
)

func TestExportedTypeAliasRejectsUnavailableSource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		exports [][]byte
	}{
		{"self", [][]byte{exportPlain("self", SortType, 1, nil)}},
		{"forward", [][]byte{exportPlain("forward", SortType, 2, nil)}},
		{"maximum index", [][]byte{exportPlain("maximum", SortType, ^uint32(0), nil)}},
		{"second self", [][]byte{exportPlain("first", SortType, 0, nil), exportPlain("second", SortType, 2, nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// One function type exists before the packed export section.
			data := []byte{
				0, 0x61, 0x73, 0x6d, 0x0d, 0, 1, 0,
				1, 8, 0, 0x61, 0x73, 0x6d, 1, 0, 0, 0,
				7, 5, 1, 0x40, 0, 0, 0x79, // (type (func (result u32)))
			}
			payload := append(encodeULEB(uint32(len(tc.exports))), concatBytes(tc.exports...)...)
			data = append(data, 11)
			data = append(data, encodeULEB(uint32(len(payload)))...)
			data = append(data, payload...)
			if _, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true}); err == nil || !strings.Contains(err.Error(), "out of range") {
				t.Fatalf("decode should reject unavailable exported type: %v", err)
			}
			if _, err := DecodeAndValidate(data); err == nil || !strings.Contains(err.Error(), "out of range") {
				t.Fatalf("validation should reject unavailable exported type: %v", err)
			}
		})
	}
}

func TestPackedExportedTypeAliasesPreserveFunctionSignature(t *testing.T) {
	data, err := os.ReadFile("testdata/packed_type_aliases.wasm")
	if err != nil {
		t.Fatal(err)
	}
	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(comp.TypeIndexSpace) != 3 || len(comp.Imports) != 1 || comp.Imports[0].TypeIndex != 2 {
		t.Fatalf("lost chained alias: types=%d imports=%+v", len(comp.TypeIndexSpace), comp.Imports)
	}
	resolver := NewTypeResolverWithInstances(comp.TypeIndexSpace, comp.InstanceTypes)
	ft, err := resolver.ResolveFuncType(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ft.Params) != 0 || ft.Result == nil || *ft.Result != (PrimValType{Type: 0x79}) {
		t.Fatalf("alias changed function signature: %+v", ft)
	}
	if _, err := DecodeAndValidate(data); err != nil {
		t.Fatalf("validation lost chained aliases: %v", err)
	}
}

func TestExportedTypeAliasCanBeUsedByLaterImport(t *testing.T) {
	data, err := os.ReadFile("testdata/exported_type_alias.wasm")
	if err != nil {
		t.Fatal(err)
	}
	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(comp.Imports) != 1 || comp.Imports[0].TypeIndex != 1 {
		t.Fatalf("unexpected import metadata: %+v", comp.Imports)
	}
	if _, err := DecodeAndValidate(data); err != nil {
		t.Fatalf("streaming validation lost exported type alias: %v", err)
	}
	resolver := NewTypeResolverWithInstances(comp.TypeIndexSpace, comp.InstanceTypes)
	if _, err := resolver.ResolveFuncType(comp.Imports[0].TypeIndex); err != nil {
		t.Fatalf("valid exported type alias lost: %v (type entries=%d)", err, len(comp.TypeIndexSpace))
	}
}

func TestParseCanonSectionRetainsSingleEntryAPI(t *testing.T) {
	entry, err := ParseCanonSection([]byte{1, CanonLower, 0, 7, 0})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != CanonLower || entry.FuncIndex != 7 {
		t.Fatalf("wrong legacy entry: %+v", entry)
	}
	for _, data := range [][]byte{{0}, {2, CanonLower, 0, 0, 0, CanonLower, 0, 1, 0}} {
		if _, err := ParseCanonSection(data); err == nil {
			t.Fatal("legacy single-entry API accepted a non-singleton section")
		}
	}
}
