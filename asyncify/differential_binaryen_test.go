package asyncify_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// discoverWasmOpt locates the wasm-opt binary via explicit WASM_OPT env var or system PATH.
// Any explicit WASM_OPT configuration failure fails setup immediately rather than falling back or skipping.
func discoverWasmOpt(t *testing.T) (string, string) {
	t.Helper()

	checkCandidate := func(candidate string) (string, error) {
		if candidate == "" {
			return "", fmt.Errorf("empty path")
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("file inaccessible: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, candidate, "--version")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("version check failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}

	// 1. Explicit WASM_OPT environment variable: must be valid if set
	if envPath, set := os.LookupEnv("WASM_OPT"); set {
		if envPath == "" {
			t.Fatalf("setup failure: WASM_OPT is set but empty")
		}
		ver, err := checkCandidate(envPath)
		if err != nil {
			t.Fatalf("setup failure: explicit WASM_OPT=%q is invalid: %v", envPath, err)
		}
		return envPath, ver
	}

	// 2. Standard system PATH
	if sysPath, err := exec.LookPath("wasm-opt"); err == nil {
		if ver, err := checkCandidate(sysPath); err == nil {
			return sysPath, ver
		}
		t.Fatalf("setup failure: wasm-opt found in PATH (%q) is invalid: %v", sysPath, err)
	}

	t.Skip("skipping differential test: wasm-opt executable not available (set WASM_OPT env var or add wasm-opt to PATH)")
	return "", ""
}

// compileBinaryen transforms a WASM binary with wasm-opt using --asyncify and --enable-multivalue.
// optimizeLevel enables Asyncify's internal optimization stages without adding
// a separate outer optimization pipeline, as -O2 would.
// Uses t.TempDir() and bounded execution with context timeouts.
func compileBinaryen(t *testing.T, wasmOptPath string, rawWasm []byte, asyncImports []string, optimizeLevel int) []byte {
	t.Helper()

	scratchDir := t.TempDir()
	inPath := filepath.Join(scratchDir, "diff_in.wasm")
	if err := os.WriteFile(inPath, rawWasm, 0600); err != nil {
		t.Fatalf("write temp input: %v", err)
	}

	outPath := filepath.Join(scratchDir, "diff_out.wasm")
	args := []string{inPath, "--asyncify", fmt.Sprintf("--optimize-level=%d", optimizeLevel), "--enable-multivalue"}
	if len(asyncImports) > 0 {
		args = append(args, "--pass-arg=asyncify-imports@"+strings.Join(asyncImports, ","))
	}
	args = append(args, "-o", outPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, wasmOptPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wasm-opt failed: %v\nOutput: %s", err, string(out))
	}

	transformed, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read wasm-opt output: %v", err)
	}
	return transformed
}

type diffMemoryCheck struct {
	Expected []byte
	Offset   uint32
}

type diffTestCase struct {
	Name                 string
	WAT                  string
	EntryFunc            string
	FloatCheck           string
	AsyncImports         []string
	EntryArgs            []uint64
	ExpectedReturns      []uint64
	ExpectedLogs         []int32
	ExpectedMemoryChecks []diffMemoryCheck
	MaxSuspends          int
	ExpectedSuspends     int
	YieldVal             uint32
}

type diffRunResult struct {
	Err            error
	Returns        []uint64
	Logs           []int32
	Memory         []byte
	FinalState     uint64
	SuspendCount   int
	PeakFrameBytes uint32
}

func compareFloat32(t *testing.T, name, role string, got, want uint64) {
	t.Helper()
	gotU := uint32(got)
	wantU := uint32(want)
	gotF := float64(math.Float32frombits(gotU))
	wantF := float64(math.Float32frombits(wantU))

	if math.IsNaN(wantF) {
		if !math.IsNaN(gotF) {
			t.Errorf("[%s] %s expected NaN, got %g (0x%08x)", name, role, gotF, gotU)
		}
		return
	}
	if math.IsNaN(gotF) {
		t.Errorf("[%s] %s unexpected NaN: got 0x%08x, want %g (0x%08x)", name, role, gotU, wantF, wantU)
		return
	}
	// Exact operation: bitwise comparison, no generic abs tolerance
	if gotU != wantU {
		t.Errorf("[%s] %s exact f32 bit mismatch: got 0x%08x (%g), want 0x%08x (%g)",
			name, role, gotU, gotF, wantU, wantF)
	}
}

func compareFloat64(t *testing.T, name, role string, got, want uint64) {
	t.Helper()
	gotF := math.Float64frombits(got)
	wantF := math.Float64frombits(want)

	if math.IsNaN(wantF) {
		if !math.IsNaN(gotF) {
			t.Errorf("[%s] %s expected NaN, got %g (0x%016x)", name, role, gotF, got)
		}
		return
	}
	if math.IsNaN(gotF) {
		t.Errorf("[%s] %s unexpected NaN: got 0x%016x, want %g (0x%016x)", name, role, got, wantF, want)
		return
	}
	// Exact operation: bitwise comparison, no generic abs tolerance
	if got != want {
		t.Errorf("[%s] %s exact f64 bit mismatch: got 0x%016x (%g), want 0x%016x (%g)",
			name, role, got, gotF, want, wantF)
	}
}

func verifyReturns(t *testing.T, tc diffTestCase, role string, actual []uint64) {
	t.Helper()
	if len(actual) != len(tc.ExpectedReturns) {
		t.Fatalf("[%s] %s return count mismatch: got %d (%v), want %d (%v)",
			tc.Name, role, len(actual), actual, len(tc.ExpectedReturns), tc.ExpectedReturns)
	}
	for i, want := range tc.ExpectedReturns {
		got := actual[i]
		switch tc.FloatCheck {
		case "f32":
			compareFloat32(t, tc.Name, role, got, want)
		case "f64":
			compareFloat64(t, tc.Name, role, got, want)
		default:
			if got != want {
				t.Errorf("[%s] %s return[%d] mismatch: got %d (0x%x), want %d (0x%x)",
					tc.Name, role, i, got, got, want, want)
			}
		}
	}
}

func verifyLogs(t *testing.T, tc diffTestCase, role string, actual []int32) {
	t.Helper()
	if len(actual) == 0 && len(tc.ExpectedLogs) == 0 {
		return
	}
	if !reflect.DeepEqual(actual, tc.ExpectedLogs) {
		t.Errorf("[%s] %s side effect logs mismatch: got %v, want %v",
			tc.Name, role, actual, tc.ExpectedLogs)
	}
}

func verifyMemory(t *testing.T, tc diffTestCase, role string, mem []byte) {
	t.Helper()
	for _, check := range tc.ExpectedMemoryChecks {
		end := check.Offset + uint32(len(check.Expected))
		if uint32(len(mem)) < end {
			t.Fatalf("[%s] %s memory bounds error: mem size %d < required %d",
				tc.Name, role, len(mem), end)
		}
		actual := mem[check.Offset:end]
		if !bytes.Equal(actual, check.Expected) {
			t.Errorf("[%s] %s memory effect mismatch at offset %d: got %x, want %x",
				tc.Name, role, check.Offset, actual, check.Expected)
		}
	}
}

func executeDiffRuntime(
	t *testing.T,
	backend string, // "compiler" or "interpreter"
	wasmBytes []byte,
	tc diffTestCase,
	isTransformed bool,
	shouldSuspend bool,
) diffRunResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cfg wazero.RuntimeConfig
	if backend == "interpreter" {
		cfg = wazero.NewRuntimeConfigInterpreter()
	} else {
		cfg = wazero.NewRuntimeConfigCompiler()
	}
	cfg = cfg.WithCloseOnContextDone(true)

	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	defer func() { _ = rt.Close(ctx) }()

	var logs []int32
	suspensionsDone := 0

	builder := rt.NewHostModuleBuilder("env")

	// void yield
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module) {
			if !isTransformed || !shouldSuspend {
				return
			}
			fnGetState := mod.ExportedFunction("asyncify_get_state")
			if fnGetState == nil {
				panic("asyncify_get_state export missing")
			}
			st, err := fnGetState.Call(ctx)
			if err != nil {
				panic(fmt.Errorf("asyncify_get_state: %w", err))
			}
			state := st[0]
			switch state {
			case 0:
				if suspensionsDone < tc.MaxSuspends {
					suspensionsDone++
					if !mod.Memory().WriteUint32Le(1024, 1032) {
						panic("WriteUint32Le failed at 1024")
					}
					if !mod.Memory().WriteUint32Le(1028, 8192) {
						panic("WriteUint32Le failed at 1028")
					}
					fnStartUnwind := mod.ExportedFunction("asyncify_start_unwind")
					if fnStartUnwind == nil {
						panic("asyncify_start_unwind export missing")
					}
					if _, err := fnStartUnwind.Call(ctx, 1024); err != nil {
						panic(fmt.Errorf("asyncify_start_unwind: %w", err))
					}
				}
			case 2:
				fnStopRewind := mod.ExportedFunction("asyncify_stop_rewind")
				if fnStopRewind == nil {
					panic("asyncify_stop_rewind export missing")
				}
				if _, err := fnStopRewind.Call(ctx); err != nil {
					panic(fmt.Errorf("asyncify_stop_rewind: %w", err))
				}
			default:
				panic(fmt.Errorf("unexpected asyncify state in yield: %d", state))
			}
		}).
		Export("yield")

	// yield_val (returning uint32)
	// Returns host value after suspension budget is exhausted.
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module) uint32 {
			if !isTransformed || !shouldSuspend {
				return tc.YieldVal
			}
			fnGetState := mod.ExportedFunction("asyncify_get_state")
			if fnGetState == nil {
				panic("asyncify_get_state export missing")
			}
			st, err := fnGetState.Call(ctx)
			if err != nil {
				panic(fmt.Errorf("asyncify_get_state: %w", err))
			}
			state := st[0]
			switch state {
			case 0:
				if suspensionsDone < tc.MaxSuspends {
					suspensionsDone++
					if !mod.Memory().WriteUint32Le(1024, 1032) {
						panic("WriteUint32Le failed at 1024")
					}
					if !mod.Memory().WriteUint32Le(1028, 8192) {
						panic("WriteUint32Le failed at 1028")
					}
					fnStartUnwind := mod.ExportedFunction("asyncify_start_unwind")
					if fnStartUnwind == nil {
						panic("asyncify_start_unwind export missing")
					}
					if _, err := fnStartUnwind.Call(ctx, 1024); err != nil {
						panic(fmt.Errorf("asyncify_start_unwind: %w", err))
					}
					return 0
				}
				// Suspension budget exhausted: return host value on normal execution path
				return tc.YieldVal
			case 2:
				fnStopRewind := mod.ExportedFunction("asyncify_stop_rewind")
				if fnStopRewind == nil {
					panic("asyncify_stop_rewind export missing")
				}
				if _, err := fnStopRewind.Call(ctx); err != nil {
					panic(fmt.Errorf("asyncify_stop_rewind: %w", err))
				}
				return tc.YieldVal
			default:
				panic(fmt.Errorf("unexpected asyncify state in yield_val: %d", state))
			}
		}).
		Export("yield_val")

	// log side effect (param i32)
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, v int32) {
			logs = append(logs, v)
		}).
		Export("log")

	if _, err := builder.Instantiate(ctx); err != nil {
		return diffRunResult{Err: fmt.Errorf("host builder instantiate: %w", err)}
	}

	inst, err := rt.Instantiate(ctx, wasmBytes)
	if err != nil {
		return diffRunResult{Err: fmt.Errorf("module instantiate: %w", err)}
	}

	entryName := tc.EntryFunc
	if entryName == "" {
		entryName = "run"
	}
	fn := inst.ExportedFunction(entryName)
	if fn == nil {
		return diffRunResult{Err: fmt.Errorf("entry function %s not found", entryName)}
	}

	readMemorySnapshot := func(m api.Module) ([]byte, error) {
		mem := m.Memory()
		if mem == nil {
			return nil, nil
		}
		buf, ok := mem.Read(0, mem.Size())
		if !ok {
			return nil, fmt.Errorf("failed to read module memory (size=%d)", mem.Size())
		}
		cp := make([]byte, len(buf))
		copy(cp, buf)
		return cp, nil
	}

	if !isTransformed || !shouldSuspend {
		res, callErr := fn.Call(ctx, tc.EntryArgs...)
		memSnapshot, memErr := readMemorySnapshot(inst)
		if callErr == nil && memErr != nil {
			callErr = memErr
		}
		return diffRunResult{Returns: res, Logs: logs, Memory: memSnapshot, Err: callErr}
	}

	// Controlled suspend/resume bounded loop
	const maxResumeSteps = 50
	var finalRes []uint64
	var peakFrameBytes uint32
resumeLoop:
	for step := 0; step < maxResumeSteps; step++ {
		res, callErr := fn.Call(ctx, tc.EntryArgs...)
		if callErr != nil {
			return diffRunResult{
				Logs:         logs,
				SuspendCount: suspensionsDone,
				Err:          fmt.Errorf("resume step %d call: %w", step, callErr),
			}
		}

		fnGetState := inst.ExportedFunction("asyncify_get_state")
		if fnGetState == nil {
			return diffRunResult{Logs: logs, SuspendCount: suspensionsDone, Err: fmt.Errorf("asyncify_get_state missing")}
		}
		st, stateErr := fnGetState.Call(ctx)
		if stateErr != nil {
			return diffRunResult{Logs: logs, SuspendCount: suspensionsDone, Err: fmt.Errorf("get_state error: %w", stateErr)}
		}
		state := st[0]
		switch state {
		case 1:
			pointer, ok := inst.Memory().ReadUint32Le(1024)
			if !ok || pointer < 1032 {
				return diffRunResult{Err: fmt.Errorf("invalid unwound stack pointer")}
			}
			if used := pointer - 1032; used > peakFrameBytes {
				peakFrameBytes = used
			}
			// Unwound
			fnStopUnwind := inst.ExportedFunction("asyncify_stop_unwind")
			if fnStopUnwind == nil {
				return diffRunResult{Err: fmt.Errorf("asyncify_stop_unwind missing")}
			}
			if _, err := fnStopUnwind.Call(ctx); err != nil {
				return diffRunResult{Err: fmt.Errorf("stop_unwind: %w", err)}
			}
			fnStartRewind := inst.ExportedFunction("asyncify_start_rewind")
			if fnStartRewind == nil {
				return diffRunResult{Err: fmt.Errorf("asyncify_start_rewind missing")}
			}
			if _, err := fnStartRewind.Call(ctx, 1024); err != nil {
				return diffRunResult{Err: fmt.Errorf("start_rewind: %w", err)}
			}
		case 0:
			// Completed execution
			finalRes = res
			break resumeLoop
		default:
			return diffRunResult{Err: fmt.Errorf("unexpected state after call: %d", state)}
		}

		if step == maxResumeSteps-1 {
			return diffRunResult{Err: fmt.Errorf("exceeded maxResumeSteps bound (%d)", maxResumeSteps)}
		}
	}

	fnGetState := inst.ExportedFunction("asyncify_get_state")
	if fnGetState == nil {
		return diffRunResult{Err: fmt.Errorf("asyncify_get_state missing")}
	}
	finalSt, err := fnGetState.Call(ctx)
	if err != nil {
		return diffRunResult{Err: fmt.Errorf("final get_state: %w", err)}
	}
	memSnapshot, memErr := readMemorySnapshot(inst)
	if memErr != nil {
		return diffRunResult{Err: memErr}
	}

	return diffRunResult{
		Returns:        finalRes,
		PeakFrameBytes: peakFrameBytes,
		Logs:           logs,
		Memory:         memSnapshot,
		FinalState:     finalSt[0],
		SuspendCount:   suspensionsDone,
	}
}

// runDifferentialScenario compares both upstream pipeline configurations with
// the port. Binaryen's internal local coalescing and frame optimization stages
// run only when the optimization level is greater than zero.
func runDifferentialScenario(t *testing.T, tc diffTestCase) {
	t.Helper()
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("upstream_opt_%d", level), func(t *testing.T) {
			runDifferentialScenarioWithOptimization(t, tc, level)
		})
	}
}

// runDifferentialScenarioWithOptimization executes a behavioral comparison
// against independently calculated oracles (return values, logs, memory effects, suspension counts).
func runDifferentialScenarioWithOptimization(t *testing.T, tc diffTestCase, optimizeLevel int) {
	t.Helper()

	entryFunc := tc.EntryFunc
	if entryFunc == "" {
		entryFunc = "run"
	}
	tc.EntryFunc = entryFunc

	wasmOptPath, wasmOptVer := discoverWasmOpt(t)
	t.Logf("[%s] Using wasm-opt: %s (%s)", tc.Name, wasmOptPath, wasmOptVer)

	rawWasm, err := wat.Compile(tc.WAT)
	if err != nil {
		t.Fatalf("wat.Compile failed: %v", err)
	}

	binWasm := compileBinaryen(t, wasmOptPath, rawWasm, tc.AsyncImports, optimizeLevel)

	goWasm, err := asyncify.Transform(rawWasm, asyncify.Config{
		AsyncImports: tc.AsyncImports,
	})
	if err != nil {
		t.Fatalf("asyncify.Transform (GoPort) failed: %v", err)
	}

	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			// 1. Raw Reference (non-suspending baseline verified against independent oracle)
			rawRes := executeDiffRuntime(t, backend, rawWasm, tc, false, false)
			if rawRes.Err != nil {
				t.Fatalf("Raw reference execution failed: %v", rawRes.Err)
			}
			if rawRes.SuspendCount != 0 {
				t.Fatalf("Raw reference unexpected suspension count: %d", rawRes.SuspendCount)
			}
			verifyReturns(t, tc, "raw", rawRes.Returns)
			verifyLogs(t, tc, "raw", rawRes.Logs)
			verifyMemory(t, tc, "raw", rawRes.Memory)

			// 2. Binaryen Transformed (suspending, verified against independent oracle)
			binRes := executeDiffRuntime(t, backend, binWasm, tc, true, true)
			if binRes.Err != nil {
				t.Fatalf("Binaryen transformed execution failed: %v", binRes.Err)
			}
			if binRes.FinalState != 0 {
				t.Errorf("Binaryen final state not 0: %d", binRes.FinalState)
			}
			if binRes.SuspendCount != tc.ExpectedSuspends {
				t.Errorf("Binaryen suspension count mismatch: got %d, want %d", binRes.SuspendCount, tc.ExpectedSuspends)
			}
			verifyReturns(t, tc, "binaryen", binRes.Returns)
			verifyLogs(t, tc, "binaryen", binRes.Logs)
			verifyMemory(t, tc, "binaryen", binRes.Memory)

			// 3. GoPort Transformed (suspending, verified against independent oracle)
			goRes := executeDiffRuntime(t, backend, goWasm, tc, true, true)
			if goRes.Err != nil {
				t.Fatalf("GoPort transformed execution failed: %v", goRes.Err)
			}
			if goRes.FinalState != 0 {
				t.Errorf("GoPort final state not 0: %d", goRes.FinalState)
			}
			if goRes.SuspendCount != tc.ExpectedSuspends {
				t.Errorf("GoPort suspension count mismatch: got %d, want %d", goRes.SuspendCount, tc.ExpectedSuspends)
			}
			verifyReturns(t, tc, "goport", goRes.Returns)
			verifyLogs(t, tc, "goport", goRes.Logs)
			verifyMemory(t, tc, "goport", goRes.Memory)

			// 4. Binaryen Transparency (non-suspending, verified against independent oracle)
			binNoSus := executeDiffRuntime(t, backend, binWasm, tc, true, false)
			if binNoSus.Err != nil {
				t.Fatalf("Binaryen non-suspending execution failed: %v", binNoSus.Err)
			}
			if binNoSus.FinalState != 0 {
				t.Errorf("Binaryen non-suspending final state not 0: %d", binNoSus.FinalState)
			}
			if binNoSus.SuspendCount != 0 {
				t.Errorf("Binaryen non-suspending unexpected suspensions: %d", binNoSus.SuspendCount)
			}
			verifyReturns(t, tc, "binaryen_nosus", binNoSus.Returns)
			verifyLogs(t, tc, "binaryen_nosus", binNoSus.Logs)
			verifyMemory(t, tc, "binaryen_nosus", binNoSus.Memory)

			// 5. GoPort Transparency (non-suspending, verified against independent oracle)
			goNoSus := executeDiffRuntime(t, backend, goWasm, tc, true, false)
			if goNoSus.Err != nil {
				t.Fatalf("GoPort non-suspending execution failed: %v", goNoSus.Err)
			}
			if goNoSus.FinalState != 0 {
				t.Errorf("GoPort non-suspending final state not 0: %d", goNoSus.FinalState)
			}
			if goNoSus.SuspendCount != 0 {
				t.Errorf("GoPort non-suspending unexpected suspensions: %d", goNoSus.SuspendCount)
			}
			verifyReturns(t, tc, "goport_nosus", goNoSus.Returns)
			verifyLogs(t, tc, "goport_nosus", goNoSus.Logs)
			verifyMemory(t, tc, "goport_nosus", goNoSus.Memory)

			// 6. Cross-parity sanity check
			if binRes.SuspendCount != goRes.SuspendCount {
				t.Errorf("Suspension count mismatch between transforms: binaryen=%d, goport=%d",
					binRes.SuspendCount, goRes.SuspendCount)
			}
			if !reflect.DeepEqual(binRes.Returns, goRes.Returns) {
				t.Errorf("Return values mismatch between transforms: binaryen=%v, goport=%v",
					binRes.Returns, goRes.Returns)
			}
			if !reflect.DeepEqual(binRes.Logs, goRes.Logs) {
				t.Errorf("Logs mismatch between transforms: binaryen=%v, goport=%v",
					binRes.Logs, goRes.Logs)
			}
		})
	}
}

// 1. Typed results: i32, i64, f32, f64, mixed locals, multi-value
func TestDifferential_TypedResults(t *testing.T) {
	tests := []diffTestCase{
		{
			Name: "TypedResult_i32",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    (local $x i32)
    i32.const 42
    local.set $x
    call $yield
    local.get $x
    i32.const 10
    i32.add
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{52},
		},
		{
			Name: "TypedResult_i64",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result i64)
    (local $x i64)
    i64.const 0x7fffffffffffffff
    local.set $x
    call $yield
    local.get $x
    i64.const 1
    i64.sub
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{0x7ffffffffffffffe},
		},
		{
			Name: "TypedResult_f32",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result f32)
    (local $x f32)
    f32.const 1.5
    local.set $x
    call $yield
    local.get $x
    f32.const 2.5
    f32.mul
  )
)`,
			AsyncImports:     []string{"env.yield"},
			FloatCheck:       "f32",
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{uint64(math.Float32bits(3.75))},
		},
		{
			Name: "TypedResult_f64",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result f64)
    (local $x f64)
    f64.const 3.141592653589793
    local.set $x
    call $yield
    local.get $x
    f64.const 2.0
    f64.mul
  )
)`,
			AsyncImports:     []string{"env.yield"},
			FloatCheck:       "f64",
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{math.Float64bits(3.141592653589793 * 2.0)},
		},
		{
			Name: "TypedResult_MixedLocals",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result f64)
    (local $i32 i32)
    (local $i64 i64)
    (local $f32 f32)
    (local $f64 f64)
    i32.const 10
    local.set $i32
    i64.const 20
    local.set $i64
    f32.const 30.5
    local.set $f32
    f64.const 40.25
    local.set $f64
    call $yield
    local.get $f64
    local.get $f32
    f64.promote_f32
    f64.add
    local.get $i64
    f64.convert_i64_s
    f64.add
    local.get $i32
    f64.convert_i32_s
    f64.add
  )
)`,
			AsyncImports:     []string{"env.yield"},
			FloatCheck:       "f64",
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{math.Float64bits(100.75)},
		},
		{
			Name: "TypedResult_MultiValue",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result i32 i64)
    (local $x i32)
    (local $y i64)
    i32.const 42
    local.set $x
    i64.const 99999
    local.set $y
    call $yield
    local.get $x
    local.get $y
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{42, 99999},
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runDifferentialScenario(t, tc)
		})
	}
}

// 2. Side effects executed once across unwinding and rewinding (host logs and guest memory writes)
func TestDifferential_SideEffects(t *testing.T) {
	tests := []diffTestCase{
		{
			Name: "SideEffects_Once",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (import "env" "log" (func $log (param i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    i32.const 101
    call $log

    ;; Pre-yield memory store: increment counter at address 0
    i32.const 0
    i32.const 0
    i32.load
    i32.const 1
    i32.add
    i32.store

    call $yield

    i32.const 102
    call $log

    ;; Post-yield memory store: increment counter at address 4
    i32.const 4
    i32.const 4
    i32.load
    i32.const 1
    i32.add
    i32.store

    i32.const 42
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{42},
			ExpectedLogs:     []int32{101, 102},
			ExpectedMemoryChecks: []diffMemoryCheck{
				{Offset: 0, Expected: []byte{1, 0, 0, 0}},
				{Offset: 4, Expected: []byte{1, 0, 0, 0}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runDifferentialScenario(t, tc)
		})
	}
}

// 3. Operand values surviving calls across unwind/rewind
func TestDifferential_OperandStack(t *testing.T) {
	tests := []diffTestCase{
		{
			Name: "OperandStack_SurvivingCall",
			WAT: `(module
  (import "env" "yield_val" (func $yield_val (result i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    i32.const 10
    i32.const 20
    call $yield_val
    i32.add
    i32.add
  )
)`,
			AsyncImports:     []string{"env.yield_val"},
			YieldVal:         5,
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{35},
		},
		{
			Name: "OperandStack_MixedSurvivingCall",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result f64)
    i32.const 10
    f64.convert_i32_s
    i64.const 20
    f64.convert_i64_s
    f64.const 40.25
    f32.const 30.5
    call $yield
    f64.promote_f32
    f64.add
    f64.add
    f64.add
  )
)`,
			AsyncImports:     []string{"env.yield"},
			FloatCheck:       "f64",
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{math.Float64bits(100.75)},
		},
		{
			Name: "OperandStack_SelectCondition",
			WAT: `(module
  (import "env" "yield_val" (func $yield_val (result i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    i32.const 111
    i32.const 222
    call $yield_val
    select
  )
)`,
			AsyncImports:     []string{"env.yield_val"},
			YieldVal:         1,
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{111},
		},
		{
			// Regression covering yield_val returning host value after suspension budget exhausted
			Name: "OperandStack_ExtraCallAfterBudget",
			WAT: `(module
  (import "env" "yield_val" (func $yield_val (result i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    call $yield_val
    call $yield_val
    i32.add
  )
)`,
			AsyncImports:     []string{"env.yield_val"},
			YieldVal:         25,
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{50},
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runDifferentialScenario(t, tc)
		})
	}
}

// 4. Control flow: br, br_if, br_table in nested loop/block/if
func TestDifferential_ControlFlow(t *testing.T) {
	tests := []diffTestCase{
		{
			Name: "ControlFlow_Loop_br_if",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (import "env" "log" (func $log (param i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    (local $i i32)
    (local $sum i32)
    i32.const 0
    local.set $i
    i32.const 0
    local.set $sum
    loop $loop
      local.get $i
      call $log
      local.get $sum
      local.get $i
      i32.add
      local.set $sum

      local.get $i
      i32.const 2
      i32.eq
      if
        call $yield
      end

      local.get $i
      i32.const 1
      i32.add
      local.set $i

      local.get $i
      i32.const 5
      i32.lt_s
      br_if $loop
    end
    local.get $sum
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{10},
			ExpectedLogs:     []int32{0, 1, 2, 3, 4},
		},
		{
			Name: "ControlFlow_Nested_Block_br",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    (local $acc i32)
    i32.const 10
    local.set $acc
    block $outer
      block $inner
        call $yield
        local.get $acc
        i32.const 50
        i32.add
        local.set $acc
        br $outer
      end
      i32.const 999
      local.set $acc
    end
    local.get $acc
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{60},
		},
		{
			Name: "ControlFlow_Nested_Loops",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    (local $outer_i i32)
    (local $inner_i i32)
    (local $acc i32)
    i32.const 0
    local.set $outer_i
    i32.const 0
    local.set $acc

    loop $outer
      i32.const 0
      local.set $inner_i

      loop $inner
        local.get $outer_i
        i32.const 1
        i32.eq
        if
          local.get $inner_i
          i32.const 1
          i32.eq
          if
            call $yield
          end
        end

        local.get $acc
        i32.const 1
        i32.add
        local.set $acc

        local.get $inner_i
        i32.const 1
        i32.add
        local.set $inner_i

        local.get $inner_i
        i32.const 3
        i32.lt_s
        br_if $inner
      end

      local.get $outer_i
      i32.const 1
      i32.add
      local.set $outer_i

      local.get $outer_i
      i32.const 3
      i32.lt_s
      br_if $outer
    end

    local.get $acc
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{9},
		},
		{
			Name: "ControlFlow_br_table",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (param $idx i32) (result i32)
    block $default
      block $t1
        block $t0
          local.get $idx
          br_table $t0 $t1 $default
        end
        i32.const 100
        return
      end
      call $yield
      i32.const 200
      return
    end
    i32.const 300
    return
  )
)`,
			AsyncImports:     []string{"env.yield"},
			EntryArgs:        []uint64{1},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{200},
		},
		{
			Name: "ControlFlow_IfElse_Branches",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func (export "run") (param $take_then i32) (result i32)
    (local $res i32)
    local.get $take_then
    if
      call $yield
      i32.const 777
      local.set $res
    else
      i32.const 888
      local.set $res
    end
    local.get $res
  )
)`,
			AsyncImports:     []string{"env.yield"},
			EntryArgs:        []uint64{1},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{777},
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runDifferentialScenario(t, tc)
		})
	}
}

// 5. Direct and indirect cycles with equivalent type indices
func TestDifferential_Cycles(t *testing.T) {
	tests := []diffTestCase{
		{
			Name: "Cycles_DirectRecursion",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (memory (export "memory") 1)
  (func $rec (export "run") (param $n i32) (param $acc i32) (result i32)
    local.get $n
    i32.eqz
    if (result i32)
      call $yield
      local.get $acc
    else
      local.get $n
      i32.const 1
      i32.sub
      local.get $acc
      local.get $n
      i32.add
      call $rec
    end
  )
)`,
			AsyncImports:     []string{"env.yield"},
			EntryArgs:        []uint64{5, 0},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{15},
		},
		{
			Name: "Cycles_IndirectMutual_EquivalentTypeIndices",
			WAT: `(module
  (type $sig (func (param i32 i32) (result i32)))
  (import "env" "yield" (func $yield))
  (table 2 funcref)
  (memory (export "memory") 1)

  (func $f0 (type $sig) (param $n i32) (param $acc i32) (result i32)
    local.get $n
    i32.eqz
    if (result i32)
      call $yield
      local.get $acc
    else
      local.get $n
      i32.const 1
      i32.sub
      local.get $acc
      i32.const 10
      i32.add
      i32.const 1
      call_indirect (type $sig)
    end
  )

  (func $f1 (type $sig) (param $n i32) (param $acc i32) (result i32)
    local.get $n
    i32.eqz
    if (result i32)
      call $yield
      local.get $acc
    else
      local.get $n
      i32.const 1
      i32.sub
      local.get $acc
      i32.const 100
      i32.add
      i32.const 0
      call_indirect (type $sig)
    end
  )

  (elem (i32.const 0) $f0 $f1)

  (func (export "run") (param $n i32) (result i32)
    local.get $n
    i32.const 0
    i32.const 0
    call_indirect (type $sig)
  )
)`,
			AsyncImports:     []string{"env.yield"},
			EntryArgs:        []uint64{3},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{120},
		},
		{
			Name: "Cycles_IndirectMixed_AsyncAndSync",
			WAT: `(module
  (type $sig (func (param i32) (result i32)))
  (import "env" "yield" (func $yield))
  (table 2 funcref)
  (memory (export "memory") 1)

  (func $async_fn (type $sig) (param $x i32) (result i32)
    call $yield
    local.get $x
    i32.const 10
    i32.add
  )

  (func $sync_fn (type $sig) (param $x i32) (result i32)
    local.get $x
    i32.const 20
    i32.add
  )

  (elem (i32.const 0) $async_fn $sync_fn)

  (func (export "run") (param $idx i32) (result i32)
    i32.const 5
    local.get $idx
    call_indirect (type $sig)
  )
)`,
			AsyncImports:     []string{"env.yield"},
			EntryArgs:        []uint64{0},
			MaxSuspends:      1,
			ExpectedSuspends: 1,
			ExpectedReturns:  []uint64{15},
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runDifferentialScenario(t, tc)
		})
	}
}

// 6. Repeated controlled suspension
func TestDifferential_RepeatedSuspension(t *testing.T) {
	tests := []diffTestCase{
		{
			Name: "RepeatedSuspension_Sequential",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (import "env" "log" (func $log (param i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    (local $v i32)
    i32.const 10
    local.set $v

    i32.const 1
    call $log
    call $yield

    local.get $v
    i32.const 20
    i32.add
    local.set $v

    i32.const 2
    call $log
    call $yield

    local.get $v
    i32.const 30
    i32.add
    local.set $v

    i32.const 3
    call $log
    call $yield

    local.get $v
    i32.const 40
    i32.add
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      3,
			ExpectedSuspends: 3,
			ExpectedReturns:  []uint64{100},
			ExpectedLogs:     []int32{1, 2, 3},
		},
		{
			Name: "RepeatedSuspension_Loop",
			WAT: `(module
  (import "env" "yield" (func $yield))
  (import "env" "log" (func $log (param i32)))
  (memory (export "memory") 1)
  (func (export "run") (result i32)
    (local $i i32)
    (local $sum i32)
    i32.const 0
    local.set $i
    i32.const 0
    local.set $sum

    loop $loop
      local.get $i
      call $log

      call $yield

      local.get $sum
      local.get $i
      i32.add
      local.set $sum

      local.get $i
      i32.const 1
      i32.add
      local.set $i

      local.get $i
      i32.const 3
      i32.lt_s
      br_if $loop
    end

    local.get $sum
  )
)`,
			AsyncImports:     []string{"env.yield"},
			MaxSuspends:      3,
			ExpectedSuspends: 3,
			ExpectedReturns:  []uint64{3},
			ExpectedLogs:     []int32{0, 1, 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runDifferentialScenario(t, tc)
		})
	}
}
