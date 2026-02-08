package component

import (
	"testing"
)

// FuzzParseTypeSection tests type section parsing with random inputs
func FuzzParseTypeSection(f *testing.F) {
	// Seed with valid type section examples
	f.Add([]byte{
		0x01,       // 1 type
		0x40,       // func type
		0x00,       // 0 params
		0x01, 0x00, // no result
	})

	f.Add([]byte{
		0x01,            // 1 type
		0x40,            // func type
		0x01,            // 1 param
		0x01, 'a', 0x79, // param "a": u32
		0x00, 0x79, // result: u32
	})

	f.Add([]byte{
		0x01,                           // 1 type
		0x72,                           // record
		0x02,                           // 2 fields
		0x04, 'n', 'a', 'm', 'e', 0x73, // field "name": string
		0x03, 'a', 'g', 'e', 0x79, // field "age": u32
	})

	f.Add([]byte{
		0x01,                     // 1 type
		0x71,                     // variant
		0x02,                     // 2 cases
		0x04, 'n', 'o', 'n', 'e', // name "none"
		0x00,                     // no type
		0x00,                     // no refines
		0x04, 's', 'o', 'm', 'e', // name "some"
		0x01, 0x79, // has type: u32
		0x00, // no refines
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Just verify we don't panic on arbitrary input
		_, _ = ParseTypeSection(data)
	})
}
