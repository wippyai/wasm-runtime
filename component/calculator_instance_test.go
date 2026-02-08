package component

import (
	"errors"
	"io"
	"os"
	"testing"
)

func TestCalculatorInstanceTypes(t *testing.T) {
	data, err := os.ReadFile("../testbed/calculator.wasm")
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	r := getReader(data[8:])
	defer putReader(r)

	sectionNum := 0
	foundTypeSection := false
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
		if _, err := io.ReadFull(r, sectionData); err != nil {
			t.Fatalf("read section data: %v", err)
		}

		if sectionID == 7 && sectionNum == 2 { // Third type section
			foundTypeSection = true
			parsed, err := ParseTypeSection(sectionData)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			if len(parsed.Types) == 0 {
				t.Error("expected at least one type in calculator type section")
			}

			// Verify instance types are properly parsed
			for _, typ := range parsed.Types {
				if instType, ok := typ.(*InstanceType); ok {
					// Instance types should have declarations
					if len(instType.Decls) == 0 {
						t.Error("instance type should have declarations")
					}
				}
			}
			break
		}

		sectionNum++
	}

	if !foundTypeSection {
		t.Error("expected to find third type section in calculator.wasm")
	}
}
