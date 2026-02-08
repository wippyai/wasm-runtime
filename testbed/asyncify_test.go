package testbed

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/engine"
)

// ReadLineOp is a pending operation for reading a line
type ReadLineOp struct {
	memory api.Memory
	reader *LineReader
	bufPtr uint32
	bufLen uint32
}

func (op *ReadLineOp) CmdID() engine.CommandID { return 1 }

func (op *ReadLineOp) Execute(ctx context.Context) (uint64, error) {
	line, ok := op.reader.Next()
	if !ok {
		return 0, nil // EOF
	}

	// Write line to WASM memory
	data := []byte(line)
	if len(data) > int(op.bufLen) {
		data = data[:op.bufLen]
	}
	op.memory.Write(op.bufPtr, data)
	return uint64(len(data)), nil
}

// LineReader provides lines for the WASM module
type LineReader struct {
	lines []string
	pos   int
}

func NewLineReader(content string) *LineReader {
	lines := strings.Split(content, "\n")
	return &LineReader{lines: lines}
}

func (r *LineReader) Next() (string, bool) {
	if r.pos >= len(r.lines) {
		return "", false
	}
	line := r.lines[r.pos]
	r.pos++
	return line, true
}

func TestAsyncify_ProcessLines(t *testing.T) {
	ctx := context.Background()

	// Load asyncified WASM
	wasmBytes, err := os.ReadFile("asyncify-demo/asyncify_demo.wasm")
	if err != nil {
		t.Skipf("asyncify_demo.wasm not found: %v", err)
	}

	// Verify it's asyncified
	if !engine.IsAsyncified(wasmBytes) {
		t.Fatal("WASM module is not asyncified")
	}

	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)

	// Create line reader with test data
	reader := NewLineReader("line1\nline2\nline3\nline4\nline5")

	// Create asyncify runtime
	asyncify := engine.NewAsyncify()
	scheduler := engine.NewScheduler(asyncify)

	// Add to context
	ctx = engine.WithAsyncify(ctx, asyncify)
	ctx = engine.WithScheduler(ctx, scheduler)

	// Register host module with read_line function
	var memory api.Memory
	_, err = runtime.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
			bufPtr := uint32(stack[0])
			bufLen := uint32(stack[1])
			return &ReadLineOp{
				reader: reader,
				bufPtr: bufPtr,
				bufLen: bufLen,
				memory: memory,
			}
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("read_line").
		NewFunctionBuilder().
		WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {
			// write_output - not used in this test
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("write_output").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host: %v", err)
	}

	// Compile and instantiate WASM module
	compiled, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	mod, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("asyncify_demo"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	memory = mod.Memory()

	// Initialize asyncify
	err = asyncify.Init(mod)
	if err != nil {
		t.Fatalf("init asyncify: %v", err)
	}

	// Get process_lines function
	processLines := mod.ExportedFunction("process_lines")
	if processLines == nil {
		t.Fatal("process_lines not found")
	}

	// Run through scheduler
	results, err := scheduler.Run(ctx, processLines)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	lineCount := results[0]
	if lineCount != 5 {
		t.Errorf("expected 5 lines, got %d", lineCount)
	}

	t.Logf("Successfully processed %d lines using asyncify suspend/resume", lineCount)
}

func TestAsyncify_SumNumbers(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("asyncify-demo/asyncify_demo.wasm")
	if err != nil {
		t.Skipf("asyncify_demo.wasm not found: %v", err)
	}

	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)

	// Create line reader with numbers
	reader := NewLineReader("10\n20\n30\n40")

	asyncify := engine.NewAsyncify()
	scheduler := engine.NewScheduler(asyncify)
	ctx = engine.WithAsyncify(ctx, asyncify)
	ctx = engine.WithScheduler(ctx, scheduler)

	var memory api.Memory
	_, err = runtime.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
			bufPtr := uint32(stack[0])
			bufLen := uint32(stack[1])
			return &ReadLineOp{
				reader: reader,
				bufPtr: bufPtr,
				bufLen: bufLen,
				memory: memory,
			}
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("read_line").
		NewFunctionBuilder().
		WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}),
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("write_output").
		Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host: %v", err)
	}

	compiled, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	mod, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("asyncify_demo"))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer mod.Close(ctx)

	memory = mod.Memory()

	err = asyncify.Init(mod)
	if err != nil {
		t.Fatalf("init asyncify: %v", err)
	}

	sumNumbers := mod.ExportedFunction("sum_numbers")
	if sumNumbers == nil {
		t.Fatal("sum_numbers not found")
	}

	results, err := scheduler.Run(ctx, sumNumbers)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	sum := int64(results[0])
	expected := int64(100) // 10+20+30+40
	if sum != expected {
		t.Errorf("expected sum %d, got %d", expected, sum)
	}

	t.Logf("Successfully summed numbers: %d using asyncify", sum)
}

// TestAsyncify_MultipleInstances tests asyncify with multiple instances
func TestAsyncify_MultipleInstances(t *testing.T) {
	ctx := context.Background()

	wasmBytes, err := os.ReadFile("asyncify-demo/asyncify_demo.wasm")
	if err != nil {
		t.Skipf("asyncify_demo.wasm not found: %v", err)
	}

	const numInstances = 3

	// Run multiple instances in parallel
	type result struct {
		err   error
		id    int
		lines int
	}
	results := make(chan result, numInstances)

	for i := 0; i < numInstances; i++ {
		go func(id int) {
			runtime := wazero.NewRuntime(ctx)
			defer runtime.Close(ctx)

			// Each instance gets different data
			data := strings.Repeat("line\n", id+1)
			reader := NewLineReader(strings.TrimSuffix(data, "\n"))

			asyncify := engine.NewAsyncify()
			scheduler := engine.NewScheduler(asyncify)
			testCtx := engine.WithAsyncify(ctx, asyncify)
			testCtx = engine.WithScheduler(testCtx, scheduler)

			var memory api.Memory
			_, err := runtime.NewHostModuleBuilder("env").
				NewFunctionBuilder().
				WithGoModuleFunction(engine.MakeAsyncHandler(func(ctx context.Context, mod api.Module, stack []uint64) engine.PendingOp {
					return &ReadLineOp{
						reader: reader,
						bufPtr: uint32(stack[0]),
						bufLen: uint32(stack[1]),
						memory: memory,
					}
				}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
				Export("read_line").
				NewFunctionBuilder().
				WithGoFunction(api.GoFunc(func(ctx context.Context, stack []uint64) {}),
					[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
				Export("write_output").
				Instantiate(testCtx)
			if err != nil {
				results <- result{id: id, err: err}
				return
			}

			compiled, err := runtime.CompileModule(testCtx, wasmBytes)
			if err != nil {
				results <- result{id: id, err: err}
				return
			}

			mod, err := runtime.InstantiateModule(testCtx, compiled,
				wazero.NewModuleConfig().WithName(fmt.Sprintf("instance_%d", id)))
			if err != nil {
				results <- result{id: id, err: err}
				return
			}
			defer mod.Close(testCtx)

			memory = mod.Memory()

			if err := asyncify.Init(mod); err != nil {
				results <- result{id: id, err: err}
				return
			}

			processLines := mod.ExportedFunction("process_lines")
			runResults, err := scheduler.Run(testCtx, processLines)
			if err != nil {
				results <- result{id: id, err: err}
				return
			}

			results <- result{id: id, lines: int(runResults[0])}
		}(i)
	}

	// Collect results
	for i := 0; i < numInstances; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("Instance %d failed: %v", r.id, r.err)
		} else {
			expected := r.id + 1
			if r.lines != expected {
				t.Errorf("Instance %d: expected %d lines, got %d", r.id, expected, r.lines)
			} else {
				t.Logf("Instance %d: processed %d lines", r.id, r.lines)
			}
		}
	}
}
