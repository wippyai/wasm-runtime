package testbed

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/engine"
)

// HTTPChunkOp simulates an async HTTP chunk read
type HTTPChunkOp struct {
	memory api.Memory
	stream *ChunkStream
	bufPtr uint32
	bufLen uint32
}

func (op *HTTPChunkOp) CmdID() engine.CommandID { return 2 }

func (op *HTTPChunkOp) Execute(ctx context.Context) (uint64, error) {
	chunk, ok := op.stream.Next()
	if !ok {
		return 0, nil
	}

	data := chunk
	if len(data) > int(op.bufLen) {
		data = data[:op.bufLen]
	}
	op.memory.Write(op.bufPtr, data)
	return uint64(len(data)), nil
}

// HTTPStatusOp simulates async HTTP status retrieval
type HTTPStatusOp struct {
	status uint32
}

func (op *HTTPStatusOp) CmdID() engine.CommandID { return 3 }

func (op *HTTPStatusOp) Execute(ctx context.Context) (uint64, error) {
	return uint64(op.status), nil
}

// ChunkStream provides chunks for streaming simulation
type ChunkStream struct {
	chunks   [][]byte
	pos      int
	suspends int64
}

func NewChunkStream(numChunks, chunkSize int) *ChunkStream {
	chunks := make([][]byte, numChunks)
	for i := range chunks {
		chunks[i] = make([]byte, chunkSize)
		rand.Read(chunks[i])
	}
	return &ChunkStream{chunks: chunks}
}

func (s *ChunkStream) Next() ([]byte, bool) {
	if s.pos >= len(s.chunks) {
		return nil, false
	}
	chunk := s.chunks[s.pos]
	s.pos++
	atomic.AddInt64(&s.suspends, 1)
	return chunk, true
}

func (s *ChunkStream) Suspends() int64 {
	return atomic.LoadInt64(&s.suspends)
}

func (s *ChunkStream) TotalBytes() int {
	total := 0
	for _, c := range s.chunks {
		total += len(c)
	}
	return total
}

// TestAsyncify_StreamingHTTP tests many suspend/resume cycles simulating HTTP streaming
func TestAsyncify_StreamingHTTP(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("streaming-demo/streaming_demo.wasm")
	if err != nil {
		t.Skipf("streaming_demo.wasm not found: %v", err)
	}

	if !engine.IsAsyncified(wasmBytes) {
		t.Fatal("WASM module is not asyncified")
	}

	tests := []struct {
		name      string
		numChunks int
		chunkSize int
	}{
		{"small_10chunks", 10, 64},
		{"medium_100chunks", 100, 256},
		{"large_1000chunks", 1000, 512},
		{"many_10000chunks", 10000, 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := wazero.NewRuntime(ctx)
			defer runtime.Close(ctx)

			stream := NewChunkStream(tt.numChunks, tt.chunkSize)
			var progressCalls int32

			asyncify := engine.NewAsyncify()
			scheduler := engine.NewScheduler(asyncify)
			testCtx := engine.WithAsyncify(ctx, asyncify)
			testCtx = engine.WithScheduler(testCtx, scheduler)

			var memory api.Memory
			_, err := runtime.NewHostModuleBuilder("env").
				NewFunctionBuilder().
				WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
					return &HTTPChunkOp{
						stream: stream,
						bufPtr: uint32(stack[0]),
						bufLen: uint32(stack[1]),
						memory: memory,
					}
				}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
				Export("http_read_chunk").
				NewFunctionBuilder().
				WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
					return &HTTPStatusOp{status: 200}
				}), nil, []api.ValueType{api.ValueTypeI32}).
				Export("http_get_status").
				NewFunctionBuilder().
				WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {
					atomic.AddInt32(&progressCalls, 1)
				}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
				Export("report_progress").
				Instantiate(testCtx)
			if err != nil {
				t.Fatalf("instantiate host: %v", err)
			}

			compiled, err := runtime.CompileModule(testCtx, wasmBytes)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}

			mod, err := runtime.InstantiateModule(testCtx, compiled,
				wazero.NewModuleConfig().WithName(tt.name))
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			defer mod.Close(testCtx)

			memory = mod.Memory()

			if err := asyncify.Init(mod); err != nil {
				t.Fatalf("init asyncify: %v", err)
			}

			countChunks := mod.ExportedFunction("count_stream_chunks")
			if countChunks == nil {
				t.Fatal("count_stream_chunks not found")
			}

			start := time.Now()
			results, err := scheduler.Run(testCtx, countChunks)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("run: %v", err)
			}

			chunkCount := uint32(results[0])
			suspends := stream.Suspends()

			if int(chunkCount) != tt.numChunks {
				t.Errorf("expected %d chunks, got %d", tt.numChunks, chunkCount)
			}

			// Each chunk read = 1 suspend/resume cycle
			if suspends != int64(tt.numChunks) {
				t.Errorf("expected %d suspends, got %d", tt.numChunks, suspends)
			}

			t.Logf("Processed %d chunks (%d bytes) in %v",
				chunkCount, stream.TotalBytes(), elapsed)
			t.Logf("Suspend/resume cycles: %d, avg: %v/cycle",
				suspends, elapsed/time.Duration(suspends))
		})
	}
}

// BenchmarkAsyncify_Streaming benchmarks asyncify suspend/resume overhead
func BenchmarkAsyncify_Streaming(b *testing.B) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("streaming-demo/streaming_demo.wasm")
	if err != nil {
		b.Skipf("streaming_demo.wasm not found: %v", err)
	}

	benchmarks := []struct {
		name      string
		numChunks int
		chunkSize int
	}{
		{"1chunk_64b", 1, 64},
		{"10chunks_64b", 10, 64},
		{"100chunks_64b", 100, 64},
		{"1000chunks_64b", 1000, 64},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				runtime := wazero.NewRuntime(ctx)
				stream := NewChunkStream(bm.numChunks, bm.chunkSize)

				asyncify := engine.NewAsyncify()
				scheduler := engine.NewScheduler(asyncify)
				testCtx := engine.WithAsyncify(ctx, asyncify)
				testCtx = engine.WithScheduler(testCtx, scheduler)

				var memory api.Memory
				runtime.NewHostModuleBuilder("env").
					NewFunctionBuilder().
					WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
						return &HTTPChunkOp{
							stream: stream,
							bufPtr: uint32(stack[0]),
							bufLen: uint32(stack[1]),
							memory: memory,
						}
					}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
					Export("http_read_chunk").
					NewFunctionBuilder().
					WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
						return &HTTPStatusOp{status: 200}
					}), nil, []api.ValueType{api.ValueTypeI32}).
					Export("http_get_status").
					NewFunctionBuilder().
					WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}),
						[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
					Export("report_progress").
					Instantiate(testCtx)

				compiled, _ := runtime.CompileModule(testCtx, wasmBytes)
				mod, _ := runtime.InstantiateModule(testCtx, compiled,
					wazero.NewModuleConfig().WithName("bench"))

				memory = mod.Memory()
				asyncify.Init(mod)

				countChunks := mod.ExportedFunction("count_stream_chunks")
				scheduler.Run(testCtx, countChunks)

				mod.Close(testCtx)
				runtime.Close(ctx)
			}

			b.ReportMetric(float64(bm.numChunks), "suspends/op")
		})
	}
}

// BenchmarkAsyncify_SuspendResume measures raw suspend/resume overhead
func BenchmarkAsyncify_SuspendResume(b *testing.B) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("streaming-demo/streaming_demo.wasm")
	if err != nil {
		b.Skipf("streaming_demo.wasm not found: %v", err)
	}

	// Setup once
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)

	b.Run("per_suspend_cycle", func(b *testing.B) {
		// Create a stream with exactly b.N chunks
		stream := NewChunkStream(b.N, 64)

		asyncify := engine.NewAsyncify()
		scheduler := engine.NewScheduler(asyncify)
		testCtx := engine.WithAsyncify(ctx, asyncify)
		testCtx = engine.WithScheduler(testCtx, scheduler)

		var memory api.Memory
		_, err := runtime.NewHostModuleBuilder("env").
			NewFunctionBuilder().
			WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
				return &HTTPChunkOp{
					stream: stream,
					bufPtr: uint32(stack[0]),
					bufLen: uint32(stack[1]),
					memory: memory,
				}
			}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
			Export("http_read_chunk").
			NewFunctionBuilder().
			WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
				return &HTTPStatusOp{status: 200}
			}), nil, []api.ValueType{api.ValueTypeI32}).
			Export("http_get_status").
			NewFunctionBuilder().
			WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}),
				[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
			Export("report_progress").
			Instantiate(testCtx)
		if err != nil {
			b.Skipf("failed to instantiate env module: %v", err)
		}

		compiled, err := runtime.CompileModule(testCtx, wasmBytes)
		if err != nil {
			b.Fatalf("compile: %v", err)
		}
		mod, err := runtime.InstantiateModule(testCtx, compiled,
			wazero.NewModuleConfig().WithName("bench_suspend"))
		if err != nil {
			b.Skipf("failed to instantiate module: %v", err)
		}

		memory = mod.Memory()
		asyncify.Init(mod)

		countChunks := mod.ExportedFunction("count_stream_chunks")

		b.ResetTimer()
		scheduler.Run(testCtx, countChunks)
		b.StopTimer()

		mod.Close(testCtx)
	})
}

// TestAsyncify_StreamingProfile runs a profiled streaming test
func TestAsyncify_StreamingProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping profile test in short mode")
	}

	ctx := context.Background()

	wasmBytes, err := os.ReadFile("streaming-demo/streaming_demo.wasm")
	if err != nil {
		t.Skipf("streaming_demo.wasm not found: %v", err)
	}

	const numChunks = 10000
	const chunkSize = 256
	const iterations = 5

	var totalTime time.Duration
	var totalSuspends int64

	for iter := 0; iter < iterations; iter++ {
		runtime := wazero.NewRuntime(ctx)
		stream := NewChunkStream(numChunks, chunkSize)

		asyncify := engine.NewAsyncify()
		scheduler := engine.NewScheduler(asyncify)
		testCtx := engine.WithAsyncify(ctx, asyncify)
		testCtx = engine.WithScheduler(testCtx, scheduler)

		var memory api.Memory
		runtime.NewHostModuleBuilder("env").
			NewFunctionBuilder().
			WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
				return &HTTPChunkOp{
					stream: stream,
					bufPtr: uint32(stack[0]),
					bufLen: uint32(stack[1]),
					memory: memory,
				}
			}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
			Export("http_read_chunk").
			NewFunctionBuilder().
			WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
				return &HTTPStatusOp{status: 200}
			}), nil, []api.ValueType{api.ValueTypeI32}).
			Export("http_get_status").
			NewFunctionBuilder().
			WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}),
				[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
			Export("report_progress").
			Instantiate(testCtx)

		compiled, _ := runtime.CompileModule(testCtx, wasmBytes)
		mod, _ := runtime.InstantiateModule(testCtx, compiled,
			wazero.NewModuleConfig().WithName(fmt.Sprintf("profile_%d", iter)))

		memory = mod.Memory()
		asyncify.Init(mod)

		countChunks := mod.ExportedFunction("count_stream_chunks")

		start := time.Now()
		scheduler.Run(testCtx, countChunks)
		elapsed := time.Since(start)

		totalTime += elapsed
		totalSuspends += stream.Suspends()

		mod.Close(testCtx)
		runtime.Close(ctx)
	}

	avgTime := totalTime / iterations
	avgSuspends := totalSuspends / iterations
	suspendOverhead := avgTime / time.Duration(avgSuspends)
	throughput := float64(numChunks*chunkSize*iterations) / totalTime.Seconds() / 1024 / 1024

	t.Logf("=== Asyncify Streaming Profile ===")
	t.Logf("Chunks: %d x %d bytes = %d KB per iteration", numChunks, chunkSize, numChunks*chunkSize/1024)
	t.Logf("Iterations: %d", iterations)
	t.Logf("Average time: %v", avgTime)
	t.Logf("Average suspends: %d", avgSuspends)
	t.Logf("Suspend overhead: %v per suspend/resume", suspendOverhead)
	t.Logf("Throughput: %.2f MB/s", throughput)
}
