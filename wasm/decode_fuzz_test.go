package wasm

import "testing"

// FuzzParseModule exercises the core decoder with valid headers, truncated
// sections and hostile counts. The input cap bounds corpus size, not decoded
// memory use or execution time; this is parser robustness evidence only.
func FuzzParseModule(f *testing.F) {
	header := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	f.Add(header)
	f.Add([]byte{})
	for _, section := range []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13} {
		seed := append([]byte{}, header...)
		f.Add(append(seed, section, 5, 255, 255, 255, 255, 15))
	}
	f.Add(append(append([]byte{}, header...), 1, 4, 1, 0x60, 0, 0))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			t.Skip()
		}
		_, _ = ParseModule(data)
	})
}
