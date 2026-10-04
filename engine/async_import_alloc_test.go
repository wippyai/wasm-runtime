package engine

import (
	"bytes"
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func metadataLargeFunction() []byte {
	code := append(bytes.Repeat([]byte{wasm.OpNop}, 65536), wasm.OpEnd)
	return (&wasm.Module{Types: []wasm.FuncType{{}}, Funcs: []uint32{0}, Code: []wasm.FuncBody{{Code: code}}}).Encode()
}

func TestAsyncImportMetadataDoesNotMaterializeInstructions(t *testing.T) {
	data := metadataLargeFunction()
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if _, err := parseCoreModuleMeta(0, data); err != nil {
				b.Fatal(err)
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > int64(len(data)*4) {
		t.Fatalf("metadata scan allocated %d bytes for %d bytes of code", got, len(data))
	}
}

func BenchmarkAsyncImportMetadataLargeFunction(b *testing.B) {
	data := metadataLargeFunction()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := parseCoreModuleMeta(0, data); err != nil {
			b.Fatal(err)
		}
	}
}
