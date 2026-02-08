package component

import (
	"errors"
	"io"
	"os"
	"testing"
)

func TestDetailedTypeSectionParse(t *testing.T) {
	data, err := os.ReadFile("../testbed/complex.wasm")
	if err != nil {
		t.Fatalf("failed to read complex.wasm: %v", err)
	}

	r := getReader(data[8:])
	defer putReader(r)

	sectionNum := 0
	typeSecNum := 0
	totalTypesFound := 0
	for {
		sectionID, err := readByte(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read section ID: %v", err)
		}

		size, err := readLEB128(r)
		if err != nil {
			t.Fatalf("read section size: %v", err)
		}

		sectionData := make([]byte, size)
		_, err = io.ReadFull(r, sectionData)
		if err != nil {
			t.Fatalf("read section data: %v", err)
		}

		if sectionID == 7 {
			parsed, err := ParseTypeSection(sectionData)
			if err != nil {
				t.Errorf("failed to parse type section %d: %v", typeSecNum, err)
			} else {
				totalTypesFound += len(parsed.Types)
			}

			typeSecNum++

			if typeSecNum >= 3 {
				break
			}
		}

		sectionNum++
	}

	// Verify we found type sections
	if typeSecNum == 0 {
		t.Error("expected at least one type section in complex.wasm")
	}

	// Verify we parsed types successfully
	if totalTypesFound == 0 {
		t.Error("expected to parse at least one type from complex.wasm")
	}
}
