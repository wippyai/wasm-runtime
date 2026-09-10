package component

import (
	"os"
	"testing"
)

func TestExportedFunctionAliasesPreserveSourceType(t *testing.T) {
	data, err := os.ReadFile("testdata/exported_func_aliases.wasm")
	if err != nil {
		t.Fatal(err)
	}
	validated, err := DecodeAndValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	want, err := validated.state.GetFunc(1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := validated.state.GetFunc(0)
	if err != nil || first == want {
		t.Fatalf("fixture requires distinct function types: first=%d second=%d err=%v", first, want, err)
	}
	for _, index := range []uint32{2, 3} {
		got, err := validated.state.GetFunc(index)
		if err != nil || got != want {
			t.Errorf("export alias %d: type=%d want=%d err=%v", index, got, want, err)
		}
	}
}

func TestExportedFunctionAliasRejectsMissingSource(t *testing.T) {
	for _, index := range []uint32{0, 1, ^uint32(0)} {
		payload := append([]byte{1}, exportPlain("missing", SortFunc, index, nil)...)
		data := []byte{0, 0x61, 0x73, 0x6d, 0x0d, 0, 1, 0, 1, 8, 0, 0x61, 0x73, 0x6d, 1, 0, 0, 0}
		data = append(data, 11)
		data = append(data, encodeULEB(uint32(len(payload)))...)
		data = append(data, payload...)
		if _, err := DecodeAndValidate(data); err == nil {
			t.Errorf("validation accepted nonexistent function %d", index)
		}
	}
}
