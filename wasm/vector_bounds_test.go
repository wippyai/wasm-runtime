package wasm

import (
	"fmt"
	"testing"
)

func TestParseModuleRejectsTruncatedVectors(t *testing.T) {
	// Each section contains only a maximum u32 vector count, with no elements.
	// Parsing must reject it before trying to allocate the claimed vector.
	for _, section := range []byte{1, 2, 3, 4, 5, 6, 7, 9, 10, 11, 13} {
		t.Run(fmt.Sprint(section), func(t *testing.T) {
			module := []byte{0, 97, 115, 109, 1, 0, 0, 0, section, 5, 255, 255, 255, 255, 15}
			if _, err := ParseModule(module); err == nil {
				t.Fatal("accepted missing vector elements")
			}
		})
	}
}

func TestDataCountIsNotAnInlineVector(t *testing.T) {
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0, 12, 1, 3}
	m, err := ParseModule(module)
	if err != nil {
		t.Fatal(err)
	}
	if m.DataCount == nil || *m.DataCount != 3 {
		t.Fatal("data count lost")
	}
}
