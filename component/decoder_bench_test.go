package component

import (
	"bytes"
	"os"
	"testing"
)

var (
	benchComponent     []byte
	benchComponentOnce = false
)

func loadBenchComponent(b *testing.B) []byte {
	if !benchComponentOnce {
		data, err := os.ReadFile("../testbed/reference-component.wasm")
		if err != nil {
			b.Skipf("Skipping benchmark: reference component not found: %v", err)
		}
		benchComponent = data
		benchComponentOnce = true
	}
	return benchComponent
}

func BenchmarkIsComponent(b *testing.B) {
	data := loadBenchComponent(b)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = IsComponent(data)
	}
}

func BenchmarkDecode(b *testing.B) {
	data := loadBenchComponent(b)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: false})
		if err != nil {
			b.Fatal(err)
		}
		_ = comp
	}
}

func BenchmarkDecodeWithTypeParsing(b *testing.B) {
	data := loadBenchComponent(b)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: true})
		if err != nil {
			b.Fatal(err)
		}
		_ = comp
	}
}

func BenchmarkParseTypeSection(b *testing.B) {
	data := loadBenchComponent(b)

	// Extract a type section for isolated benchmarking
	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: false})
	if err != nil {
		b.Fatal(err)
	}

	if len(comp.Types) == 0 {
		b.Skip("No type sections in component")
	}

	typeSectionData := comp.Types[0].RawData

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := ParseTypeSection(typeSectionData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseTypeSection_Large(b *testing.B) {
	data := loadBenchComponent(b)

	comp, err := DecodeWithOptions(data, DecodeOptions{ParseTypes: false})
	if err != nil {
		b.Fatal(err)
	}

	// Find the largest type section (section 27 has 21 types)
	var largestSection []byte
	maxTypes := 0

	for _, t := range comp.Types {
		if t.Parsed != nil && len(t.Parsed.Types) > maxTypes {
			maxTypes = len(t.Parsed.Types)
			largestSection = t.RawData
		}
	}

	if largestSection == nil {
		// Parse all sections to find the largest
		for _, t := range comp.Types {
			parsed, err := ParseTypeSection(t.RawData)
			if err == nil && len(parsed.Types) > maxTypes {
				maxTypes = len(parsed.Types)
				largestSection = t.RawData
			}
		}
	}

	if largestSection == nil {
		b.Skip("No large type sections found")
	}

	b.Logf("Benchmarking section with %d types", maxTypes)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := ParseTypeSection(largestSection)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadLEB128 benchmarks LEB128 decoding
func BenchmarkReadLEB128(b *testing.B) {
	data := []byte{0x80, 0x80, 0x80, 0x01} // Large LEB128 value

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, err := readLEB128(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadSLEB128 benchmarks signed LEB128 decoding
func BenchmarkReadSLEB128(b *testing.B) {
	data := []byte{0xFF, 0xFF, 0xFF, 0x7F} // Negative LEB128 value

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, err := readSLEB128(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadString benchmarks string reading
func BenchmarkReadString(b *testing.B) {
	data := []byte{0x0A, 'h', 'e', 'l', 'l', 'o', 'w', 'o', 'r', 'l', 'd'}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, err := readString(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseVariantType benchmarks variant type parsing
func BenchmarkParseVariantType(b *testing.B) {
	// Variant with 2 cases: none, some(u32)
	data := []byte{
		0x02,                     // 2 cases
		0x04, 'n', 'o', 'n', 'e', // name "none"
		0x00,                     // no type
		0x00,                     // no refines
		0x04, 's', 'o', 'm', 'e', // name "some"
		0x01, 0x79, // has type: u32
		0x00, // no refines
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, err := parseVariantType(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseRecordType benchmarks record type parsing
func BenchmarkParseRecordType(b *testing.B) {
	// Record with 2 fields: {name: string, age: u32}
	data := []byte{
		0x02,                           // 2 fields
		0x04, 'n', 'a', 'm', 'e', 0x73, // field "name": string
		0x03, 'a', 'g', 'e', 0x79, // field "age": u32
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, err := parseRecordType(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseComponentFuncType benchmarks function type parsing
func BenchmarkParseComponentFuncType(b *testing.B) {
	// func(a: u32, b: string) -> u32
	data := []byte{
		0x02,            // 2 params
		0x01, 'a', 0x79, // param "a": u32
		0x01, 'b', 0x73, // param "b": string
		0x00, 0x79, // resultlist: 0x00 (has result) + u32
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, err := parseFuncType(r)
		if err != nil {
			b.Fatal(err)
		}
	}
}
