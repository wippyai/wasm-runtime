package runtime

import (
	"context"
	"os"
	"testing"

	"go.bytecodealliance.org/wit"
)

var minimalWasm []byte

func init() {
	var err error
	minimalWasm, err = os.ReadFile("../testbed/minimal.wasm")
	if err != nil {
		panic("failed to load minimal.wasm: " + err.Error())
	}
}

// BenchmarkCall_Primitive benchmarks calling a function with primitive types
func BenchmarkCall_Primitive(b *testing.B) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	// Register required host module
	if err := rt.RegisterHost(&benchHost{}); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, minimalWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	// Warmup
	_, err = inst.Call(ctx, "compute", uint32(5), uint32(3))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.Call(ctx, "compute", uint32(5), uint32(3))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCallWithTypes_Primitive benchmarks CallWithTypes with primitive types
func BenchmarkCallWithTypes_Primitive(b *testing.B) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	// Register required host module
	if err := rt.RegisterHost(&benchHost{}); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, minimalWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.U32{}, wit.U32{}}
	results := []wit.Type{wit.U32{}}

	// Warmup
	_, err = inst.CallWithTypes(ctx, "compute", params, results, uint32(5), uint32(3))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "compute", params, results, uint32(5), uint32(3))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHostCallback_Primitive benchmarks a component calling a host function
func BenchmarkHostCallback_Primitive(b *testing.B) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	host := &benchHost{}
	if err := rt.RegisterHost(host); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, minimalWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	// Warmup
	_, err = inst.Call(ctx, "compute-using-host", uint32(5), uint32(3))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.Call(ctx, "compute-using-host", uint32(5), uint32(3))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// benchHost implements the test:minimal/host interface for benchmarking
type benchHost struct{}

func (h *benchHost) Namespace() string { return "test:minimal/host@0.1.0" }

func (h *benchHost) Add(ctx context.Context, a, b uint32) uint32 {
	return a + b
}

var stringsWasm []byte

func init() {
	var err error
	stringsWasm, err = os.ReadFile("../testbed/strings.wasm")
	if err != nil {
		// Skip if not found
		return
	}
}

// BenchmarkCall_String benchmarks calling a function with string types
func BenchmarkCall_String(b *testing.B) {
	if stringsWasm == nil {
		b.Skip("strings.wasm not found")
	}

	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	// Register host module even though echo doesn't use it, component needs it
	host := &benchStringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, stringsWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.String{}}
	results := []wit.Type{wit.String{}}

	// Warmup
	_, err = inst.CallWithTypes(ctx, "echo", params, results, "hello")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "echo", params, results, "hello")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHostCallback_String benchmarks a component calling a host function with strings
func BenchmarkHostCallback_String(b *testing.B) {
	if stringsWasm == nil {
		b.Skip("strings.wasm not found")
	}

	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	host := &benchStringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, stringsWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.String{}}
	results := []wit.Type{wit.String{}}

	// Warmup
	_, err = inst.CallWithTypes(ctx, "process", params, results, "hello")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err = inst.CallWithTypes(ctx, "process", params, results, "hello")
		if err != nil {
			b.Fatal(err)
		}
	}
}

type benchStringsHost struct{}

func (h *benchStringsHost) Namespace() string { return "test:strings/host@0.1.0" }

func (h *benchStringsHost) Log(ctx context.Context, msg string) {
}

func (h *benchStringsHost) Concat(ctx context.Context, a, b string) string {
	return a + b
}

// Zero-allocation CallInto benchmarks

// BenchmarkCallInto_Primitive benchmarks zero-alloc primitive call
func BenchmarkCallInto_Primitive(b *testing.B) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	if err := rt.RegisterHost(&benchHost{}); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, minimalWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.U32{}, wit.U32{}}
	results := []wit.Type{wit.U32{}}

	var result uint32

	// Warmup
	err = inst.CallInto(ctx, "compute", params, results, &result, uint32(5), uint32(3))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "compute", params, results, &result, uint32(5), uint32(3))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCallInto_String benchmarks zero-alloc string call
func BenchmarkCallInto_String(b *testing.B) {
	if stringsWasm == nil {
		b.Skip("strings.wasm not found")
	}

	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	host := &benchStringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, stringsWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.String{}}
	results := []wit.Type{wit.String{}}

	var result string

	// Warmup
	err = inst.CallInto(ctx, "echo", params, results, &result, "hello")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "echo", params, results, &result, "hello")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCallInto_HostCallback benchmarks zero-alloc string call with host callback
func BenchmarkCallInto_HostCallback(b *testing.B) {
	if stringsWasm == nil {
		b.Skip("strings.wasm not found")
	}

	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	host := &benchStringsHost{}
	if err := rt.RegisterHost(host); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, stringsWasm)
	if err != nil {
		b.Fatal(err)
	}

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close(ctx)

	params := []wit.Type{wit.String{}}
	results := []wit.Type{wit.String{}}

	var result string

	// Warmup
	err = inst.CallInto(ctx, "process", params, results, &result, "hello")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = inst.CallInto(ctx, "process", params, results, &result, "hello")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Pool benchmarks

// BenchmarkModuleInstantiate measures instantiation from pre-compiled module
func BenchmarkModuleInstantiate(b *testing.B) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	if err := rt.RegisterHost(&benchHost{}); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, minimalWasm)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			b.Fatal(err)
		}
		inst.Close(ctx)
	}
}

// BenchmarkInstancePool simulates instance reuse pattern
func BenchmarkInstancePool(b *testing.B) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	if err := rt.RegisterHost(&benchHost{}); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, minimalWasm)
	if err != nil {
		b.Fatal(err)
	}

	const poolSize = 8
	pool := make([]*Instance, poolSize)
	for i := 0; i < poolSize; i++ {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			b.Fatal(err)
		}
		pool[i] = inst
	}
	defer func() {
		for _, inst := range pool {
			inst.Close(ctx)
		}
	}()

	params := []wit.Type{wit.U32{}, wit.U32{}}
	results := []wit.Type{wit.U32{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := pool[i%poolSize]
		_, err = inst.CallWithTypes(ctx, "compute", params, results, uint32(i), uint32(1))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInstancePoolParallel tests concurrent instantiation and calls
// Each goroutine gets its own instance since wasm instances are not thread-safe
func BenchmarkInstancePoolParallel(b *testing.B) {
	ctx := context.Background()

	rt, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close(ctx)

	if err := rt.RegisterHost(&benchHost{}); err != nil {
		b.Fatal(err)
	}

	mod, err := rt.LoadComponent(ctx, minimalWasm)
	if err != nil {
		b.Fatal(err)
	}

	params := []wit.Type{wit.U32{}, wit.U32{}}
	results := []wit.Type{wit.U32{}}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		inst, err := mod.Instantiate(ctx)
		if err != nil {
			b.Fatal(err)
		}
		defer inst.Close(ctx)

		i := 0
		for pb.Next() {
			_, err = inst.CallWithTypes(ctx, "compute", params, results, uint32(i), uint32(1))
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
