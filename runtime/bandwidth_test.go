package runtime

import (
	"context"
	"os"
	"testing"

	"go.bytecodealliance.org/wit"
)

var complexWasmForBandwidth []byte

func init() {
	var err error
	complexWasmForBandwidth, err = os.ReadFile("../testbed/complex.wasm")
	if err != nil {
		return
	}
}

// BenchmarkByteTransfer tests raw bandwidth for binary data transfer
func BenchmarkByteTransfer_1KB(b *testing.B) {
	if complexWasmForBandwidth == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasmForBandwidth)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	// 1KB of data
	data := make([]int32, 256) // 256 * 4 bytes = 1KB
	for i := range data {
		data[i] = int32(i)
	}

	var result []int32
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.SetBytes(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-list-s32", params, results, &result, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkByteTransfer_64KB(b *testing.B) {
	if complexWasmForBandwidth == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasmForBandwidth)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	// 64KB of data
	data := make([]int32, 16384) // 16384 * 4 bytes = 64KB
	for i := range data {
		data[i] = int32(i)
	}

	var result []int32
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.SetBytes(65536)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-list-s32", params, results, &result, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkByteTransfer_1MB(b *testing.B) {
	if complexWasmForBandwidth == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasmForBandwidth)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	// 1MB of data
	data := make([]int32, 262144) // 262144 * 4 bytes = 1MB
	for i := range data {
		data[i] = int32(i % 10000)
	}

	var result []int32
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.SetBytes(1048576)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-list-s32", params, results, &result, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkByteTransfer_16MB(b *testing.B) {
	if complexWasmForBandwidth == nil {
		b.Skip("complex.wasm not found")
	}
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)
	rt.RegisterHost(&complexHost{})
	mod, err := rt.LoadComponent(ctx, complexWasmForBandwidth)
	if err != nil {
		b.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	// 16MB of data
	data := make([]int32, 4194304) // 4194304 * 4 bytes = 16MB
	for i := range data {
		data[i] = int32(i % 10000)
	}

	var result []int32
	params := []wit.Type{listS32Type}
	results := []wit.Type{listS32Type}

	b.SetBytes(16777216)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo-list-s32", params, results, &result, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
