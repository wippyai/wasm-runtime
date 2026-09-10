package component

import (
	"os"
	"path/filepath"
	"testing"
)

// Spec: section_8(vec(<canon>)) — a canonical section is a vector, not a singleton.
// wasm-tools packs consecutive canon.lower / canon.lift into one section.

func TestDecode_CanonSectionVectorTwoLowers(t *testing.T) {
	payload := []byte{
		0x02,                   // vec count = 2
		0x01, 0x00, 0x00, 0x00, // lower func 0, no opts
		0x01, 0x00, 0x01, 0x00, // lower func 1, no opts
	}
	comp, err := DecodeWithOptions(minimalComponentWithCanonSection(payload), DecodeOptions{})
	if err != nil {
		t.Fatalf("decode two lowers in one canon section: %v", err)
	}
	if len(comp.Canons) != 2 {
		t.Fatalf("Canons = %d, want 2", len(comp.Canons))
	}
	for i, c := range comp.Canons {
		if c.Parsed == nil {
			t.Fatalf("Canons[%d].Parsed is nil", i)
		}
		if c.Parsed.Kind != CanonLower {
			t.Errorf("Canons[%d].Kind = %d, want lower", i, c.Parsed.Kind)
		}
		if c.Parsed.FuncIndex != uint32(i) {
			t.Errorf("Canons[%d].FuncIndex = %d, want %d", i, c.Parsed.FuncIndex, i)
		}
	}
	if len(comp.CoreFuncIndexSpace) != 2 {
		t.Fatalf("CoreFuncIndexSpace = %d, want 2", len(comp.CoreFuncIndexSpace))
	}
	for i, e := range comp.CoreFuncIndexSpace {
		if e.Kind != CoreFuncCanonLower {
			t.Errorf("CoreFuncIndexSpace[%d].Kind = %v, want CoreFuncCanonLower", i, e.Kind)
		}
		if e.FuncIndex != uint32(i) {
			t.Errorf("CoreFuncIndexSpace[%d].FuncIndex = %d, want %d", i, e.FuncIndex, i)
		}
	}
	assertCanonSectionCount(t, comp, 2)
}

func TestDecode_CanonSectionVectorTwoLifts(t *testing.T) {
	payload := []byte{
		0x02,                         // vec count = 2
		0x00, 0x00, 0x00, 0x00, 0x00, // lift core 0, no opts, type 0
		0x00, 0x00, 0x01, 0x00, 0x01, // lift core 1, no opts, type 1
	}
	comp, err := DecodeWithOptions(minimalComponentWithCanonSection(payload), DecodeOptions{})
	if err != nil {
		t.Fatalf("decode two lifts in one canon section: %v", err)
	}
	if len(comp.Canons) != 2 {
		t.Fatalf("Canons = %d, want 2", len(comp.Canons))
	}
	want := []struct {
		funcIdx uint32
		typeIdx uint32
	}{
		{0, 0},
		{1, 1},
	}
	for i, c := range comp.Canons {
		if c.Parsed == nil {
			t.Fatalf("Canons[%d].Parsed is nil", i)
		}
		if c.Parsed.Kind != CanonLift {
			t.Errorf("Canons[%d].Kind = %d, want lift", i, c.Parsed.Kind)
		}
		if c.Parsed.FuncIndex != want[i].funcIdx {
			t.Errorf("Canons[%d].FuncIndex = %d, want %d", i, c.Parsed.FuncIndex, want[i].funcIdx)
		}
		if c.Parsed.TypeIndex != want[i].typeIdx {
			t.Errorf("Canons[%d].TypeIndex = %d, want %d", i, c.Parsed.TypeIndex, want[i].typeIdx)
		}
	}
	assertCanonSectionCount(t, comp, 2)
}

func TestDecode_CanonSectionVectorLowerThenLift(t *testing.T) {
	payload := []byte{
		0x02,
		0x01, 0x00, 0x03, 0x00, // lower func 3, no opts
		0x00, 0x00, 0x04, 0x00, 0x05, // lift core 4, no opts, type 5
	}
	comp, err := DecodeWithOptions(minimalComponentWithCanonSection(payload), DecodeOptions{})
	if err != nil {
		t.Fatalf("decode mixed canon section: %v", err)
	}
	if len(comp.Canons) != 2 {
		t.Fatalf("Canons = %d, want 2", len(comp.Canons))
	}
	if comp.Canons[0].Parsed.Kind != CanonLower || comp.Canons[0].Parsed.FuncIndex != 3 {
		t.Errorf("first = kind %d func %d, want lower func 3",
			comp.Canons[0].Parsed.Kind, comp.Canons[0].Parsed.FuncIndex)
	}
	if comp.Canons[1].Parsed.Kind != CanonLift || comp.Canons[1].Parsed.FuncIndex != 4 || comp.Canons[1].Parsed.TypeIndex != 5 {
		t.Errorf("second = kind %d func %d type %d, want lift core 4 type 5",
			comp.Canons[1].Parsed.Kind, comp.Canons[1].Parsed.FuncIndex, comp.Canons[1].Parsed.TypeIndex)
	}
	if len(comp.CoreFuncIndexSpace) != 1 || comp.CoreFuncIndexSpace[0].Kind != CoreFuncCanonLower {
		t.Fatalf("CoreFuncIndexSpace = %+v, want one lower", comp.CoreFuncIndexSpace)
	}
	assertCanonSectionCount(t, comp, 2)
}

func TestDecode_CanonSectionTrailingBytes(t *testing.T) {
	payload := []byte{
		0x01,                   // vec count = 1
		0x01, 0x00, 0x00, 0x00, // one lower
		0x00, // leftover
	}
	_, err := DecodeWithOptions(minimalComponentWithCanonSection(payload), DecodeOptions{})
	if err == nil {
		t.Fatal("expected error for trailing bytes after canon vector")
	}
}

func TestDecode_CanonSectionTruncated(t *testing.T) {
	payload := []byte{
		0x02,                   // vec count = 2
		0x01, 0x00, 0x00, 0x00, // only one entry
	}
	_, err := DecodeWithOptions(minimalComponentWithCanonSection(payload), DecodeOptions{})
	if err == nil {
		t.Fatal("expected error for truncated canon vector")
	}
}

func TestDecode_WasmToolsPackedLowersAndLifts(t *testing.T) {
	wasm := loadPackedCanonComponent(t)
	comp, err := DecodeWithOptions(wasm, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatalf("decode wasm-tools component: %v", err)
	}

	var lowers, lifts int
	for i, c := range comp.Canons {
		if c.Parsed == nil {
			t.Fatalf("Canons[%d].Parsed is nil", i)
		}
		switch c.Parsed.Kind {
		case CanonLower:
			lowers++
		case CanonLift:
			lifts++
		}
	}
	if len(comp.Canons) != 4 {
		t.Fatalf("Canons = %d, want 4", len(comp.Canons))
	}
	if lowers != 2 {
		t.Fatalf("lowers = %d, want 2", lowers)
	}
	if lifts != 2 {
		t.Fatalf("lifts = %d, want 2", lifts)
	}

	var packed int
	for _, m := range comp.SectionOrder {
		if m.Kind == SectionCanon && m.Count > 1 {
			packed++
			if m.StartIndex < 0 || m.StartIndex+m.Count > len(comp.Canons) {
				t.Fatalf("section marker [%d,%d) out of Canons len %d", m.StartIndex, m.StartIndex+m.Count, len(comp.Canons))
			}
		}
	}
	if packed < 2 {
		t.Fatalf("packed canon sections = %d, want >= 2 (two lowers, then two lifts)", packed)
	}
	if comp.Canons[0].Parsed.Kind != CanonLower || comp.Canons[1].Parsed.Kind != CanonLower {
		t.Fatalf("first two canons = %d,%d, want lower,lower", comp.Canons[0].Parsed.Kind, comp.Canons[1].Parsed.Kind)
	}
	if comp.Canons[2].Parsed.Kind != CanonLift || comp.Canons[3].Parsed.Kind != CanonLift {
		t.Fatalf("last two canons = %d,%d, want lift,lift", comp.Canons[2].Parsed.Kind, comp.Canons[3].Parsed.Kind)
	}

	coreLowers := 0
	for _, e := range comp.CoreFuncIndexSpace {
		if e.Kind == CoreFuncCanonLower {
			coreLowers++
		}
	}
	if coreLowers != lowers {
		t.Fatalf("CoreFuncIndexSpace lowers = %d, Canons lowers = %d", coreLowers, lowers)
	}
}

func TestStreamingValidator_CanonSectionVector(t *testing.T) {
	payload := []byte{
		0x02,
		0x01, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x01, 0x00,
	}
	v := NewStreamingValidator()
	if err := v.Version(0x0d); err != nil {
		t.Fatalf("version: %v", err)
	}
	if err := v.ProcessSection(8, payload); err != nil {
		t.Fatalf("process packed canon section: %v", err)
	}
	current, err := v.current()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := current.GetCoreFunc(0); err != nil {
		t.Fatalf("core func 0: %v", err)
	}
	if _, err := current.GetCoreFunc(1); err != nil {
		t.Fatalf("core func 1: %v", err)
	}
	if _, err := current.GetCoreFunc(2); err == nil {
		t.Fatal("expected exactly 2 core funcs from packed canon section")
	}
}

func assertCanonSectionCount(t *testing.T, comp *Component, want int) {
	t.Helper()
	found := false
	for _, m := range comp.SectionOrder {
		if m.Kind == SectionCanon {
			if m.Count != want {
				t.Fatalf("SectionCanon Count = %d, want %d", m.Count, want)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing SectionCanon marker")
	}
}

func minimalComponentWithCanonSection(canonPayload []byte) []byte {
	coreMod := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	out := []byte{0x00, 0x61, 0x73, 0x6D, 0x0D, 0x00, 0x01, 0x00}
	out = append(out, 0x01)
	out = append(out, encodeULEB(uint32(len(coreMod)))...)
	out = append(out, coreMod...)
	out = append(out, 8)
	out = append(out, encodeULEB(uint32(len(canonPayload)))...)
	out = append(out, canonPayload...)
	return out
}

func encodeULEB(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func loadPackedCanonComponent(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "packed_canon.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseCanonSectionEntries_RejectsOversizedCountsBeforeAllocation(t *testing.T) {
	for _, data := range [][]byte{
		{0xff, 0xff, 0xff, 0xff, 0x0f},
		{1, CanonLower, 0, 0, 0xff, 0xff, 0xff, 0xff, 0x0f},
		{1, CanonLift, 0, 0, 0xff, 0xff, 0xff, 0xff, 0x0f, 0},
	} {
		if _, err := ParseCanonSectionEntries(data); err == nil {
			t.Fatalf("accepted forged count: %x", data)
		}
	}
}

func TestPackedCanonicalRegistryExportNames(t *testing.T) {
	data := loadPackedCanonComponent(t)
	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewCanonRegistry(comp, NewTypeResolverWithInstances(comp.TypeIndexSpace, comp.InstanceTypes))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"call-peek", "call-mirror"} {
		if reg.FindLift(name) == nil {
			t.Errorf("missing %q; lifts=%+v exports=%+v sections=%+v", name, reg.Lifts, comp.Exports, comp.SectionOrder)
		}
	}
}
