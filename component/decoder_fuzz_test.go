package component

import (
	"testing"
)

func FuzzDecode(f *testing.F) {
	// Add valid component as seed
	validComponent := []byte{0x00, 0x61, 0x73, 0x6D, 0x0D, 0x00, 0x01, 0x00}
	f.Add(validComponent)

	// Add core wasm module as seed
	coreModule := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	f.Add(coreModule)

	// Add truncated data
	f.Add([]byte{0x00, 0x61, 0x73})

	// Add random bytes
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Fuzzing should not panic
		DecodeWithOptions(data, DecodeOptions{ParseTypes: false})
	})
}

func FuzzIsComponent(f *testing.F) {
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x0D, 0x00, 0x01, 0x00})
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00})
	f.Add([]byte{})
	f.Add([]byte{0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Fuzzing should not panic
		IsComponent(data)
	})
}
