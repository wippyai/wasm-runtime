package asyncify_test

import (
	"context"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

// TestIsAsyncified_BeforeProof_PassiveDataAndCustomNames verifies that valid modules
// containing helper names in passive data, active data, custom sections, or import names
// are NOT mistakenly detected as asyncified.
func TestIsAsyncified_BeforeProof_PassiveDataAndCustomNames(t *testing.T) {
	helpers := []string{
		"asyncify_start_unwind",
		"asyncify_stop_unwind",
		"asyncify_start_rewind",
		"asyncify_stop_rewind",
		"asyncify_get_state",
	}

	// 1. Passive data segment containing each helper name individually
	for _, helper := range helpers {
		watSrc := `(module
			(memory 1)
			(data "` + helper + `")
			(func (export "run") (result i32) i32.const 42)
		)`
		raw, err := wat.Compile(watSrc)
		if err != nil {
			t.Fatalf("wat.Compile for helper %q: %v", helper, err)
		}
		if asyncify.IsAsyncified(raw) {
			t.Errorf("expected IsAsyncified=false for module with passive data containing %q", helper)
		}
	}

	// 2. Passive data containing ALL helper names in one module
	allHelpersWat := `(module
		(memory 1)
		(data "asyncify_start_unwind asyncify_stop_unwind asyncify_start_rewind asyncify_stop_rewind asyncify_get_state")
		(func (export "run") (result i32) i32.const 42)
	)`
	allRaw, err := wat.Compile(allHelpersWat)
	if err != nil {
		t.Fatalf("wat.Compile all helpers in passive data: %v", err)
	}
	if asyncify.IsAsyncified(allRaw) {
		t.Error("expected IsAsyncified=false for module containing all helper names in passive data")
	}

	// 3. Active data segment containing helper names
	activeDataWat := `(module
		(memory 1)
		(data (i32.const 0) "asyncify_start_unwind asyncify_stop_unwind")
		(func (export "run") (result i32) i32.const 42)
	)`
	activeRaw, err := wat.Compile(activeDataWat)
	if err != nil {
		t.Fatalf("wat.Compile active data: %v", err)
	}
	if asyncify.IsAsyncified(activeRaw) {
		t.Error("expected IsAsyncified=false for module with active data containing helper names")
	}

	// 4. Custom section containing helper names
	customMod := &wasm.Module{
		CustomSections: []wasm.CustomSection{
			{
				Name: "asyncify_start_unwind",
				Data: []byte("asyncify_stop_unwind asyncify_start_rewind asyncify_stop_rewind asyncify_get_state"),
			},
		},
	}
	customRaw := customMod.Encode()
	if asyncify.IsAsyncified(customRaw) {
		t.Error("expected IsAsyncified=false for module with custom section containing helper names")
	}

	// 5. Function names / identifiers inside the module (name section)
	funcNameWat := `(module
		(func $asyncify_start_unwind (result i32) i32.const 1)
		(func $asyncify_stop_unwind (result i32) i32.const 2)
		(func (export "run") (result i32) call $asyncify_start_unwind)
	)`
	funcNameRaw, err := wat.Compile(funcNameWat)
	if err != nil {
		t.Fatalf("wat.Compile func names: %v", err)
	}
	if asyncify.IsAsyncified(funcNameRaw) {
		t.Error("expected IsAsyncified=false for internal unexported function names")
	}

	// 6. Import names matching helper names
	importNamesWat := `(module
		(import "env" "asyncify_start_unwind" (func $start (param i32)))
		(import "env" "asyncify_stop_unwind" (func $stop))
		(func (export "run") (result i32) i32.const 42)
	)`
	importNamesRaw, err := wat.Compile(importNamesWat)
	if err != nil {
		t.Fatalf("wat.Compile import names: %v", err)
	}
	if asyncify.IsAsyncified(importNamesRaw) {
		t.Error("expected IsAsyncified=false for module importing helpers without export")
	}
}

// TestIsAsyncified_ImportedOrWrongKindOrWrongSignatureOrPartial verifies that
// invalid or incomplete helper exports return false.
func TestIsAsyncified_ImportedOrWrongKindOrWrongSignatureOrPartial(t *testing.T) {
	// 1. Re-exported imported helper functions (not locally defined functions)
	reexportedImportWat := `(module
		(import "env" "asyncify_start_unwind" (func $start (param i32)))
		(func $stop_unwind)
		(func $start_rewind (param i32))
		(func $stop_rewind)
		(func $get_state (result i32) i32.const 0)
		(export "asyncify_start_unwind" (func $start))
		(export "asyncify_stop_unwind" (func $stop_unwind))
		(export "asyncify_start_rewind" (func $start_rewind))
		(export "asyncify_stop_rewind" (func $stop_rewind))
		(export "asyncify_get_state" (func $get_state))
	)`
	raw, err := wat.Compile(reexportedImportWat)
	if err != nil {
		t.Fatalf("wat.Compile reexported import: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when asyncify_start_unwind is an imported function")
	}

	// 2. Wrong export kind: helper is exported as a Global instead of Function
	wrongKindGlobalWat := `(module
		(global (export "asyncify_start_unwind") i32 (i32.const 0))
		(func $stop_unwind)
		(func $start_rewind (param i32))
		(func $stop_rewind)
		(func $get_state (result i32) i32.const 0)
		(export "asyncify_stop_unwind" (func $stop_unwind))
		(export "asyncify_start_rewind" (func $start_rewind))
		(export "asyncify_stop_rewind" (func $stop_rewind))
		(export "asyncify_get_state" (func $get_state))
	)`
	raw, err = wat.Compile(wrongKindGlobalWat)
	if err != nil {
		t.Fatalf("wat.Compile wrong kind global: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when asyncify_start_unwind is exported as global")
	}

	// 3. Wrong export kind: helper is exported as Memory
	wrongKindMemWat := `(module
		(memory (export "asyncify_start_unwind") 1)
		(func $stop_unwind)
		(func $start_rewind (param i32))
		(func $stop_rewind)
		(func $get_state (result i32) i32.const 0)
		(export "asyncify_stop_unwind" (func $stop_unwind))
		(export "asyncify_start_rewind" (func $start_rewind))
		(export "asyncify_stop_rewind" (func $stop_rewind))
		(export "asyncify_get_state" (func $get_state))
	)`
	raw, err = wat.Compile(wrongKindMemWat)
	if err != nil {
		t.Fatalf("wat.Compile wrong kind memory: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when asyncify_start_unwind is exported as memory")
	}

	// 4. Wrong signature: asyncify_start_unwind has 0 params instead of 1
	wrongSigNoParamWat := `(module
		(func (export "asyncify_start_unwind"))
		(func (export "asyncify_stop_unwind"))
		(func (export "asyncify_start_rewind") (param i32))
		(func (export "asyncify_stop_rewind"))
		(func (export "asyncify_get_state") (result i32) i32.const 0)
	)`
	raw, err = wat.Compile(wrongSigNoParamWat)
	if err != nil {
		t.Fatalf("wat.Compile wrong sig no param: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when asyncify_start_unwind has no params")
	}

	// 5. Wrong signature: asyncify_start_unwind has f32 param instead of i32/i64
	wrongSigF32Wat := `(module
		(func (export "asyncify_start_unwind") (param f32))
		(func (export "asyncify_stop_unwind"))
		(func (export "asyncify_start_rewind") (param f32))
		(func (export "asyncify_stop_rewind"))
		(func (export "asyncify_get_state") (result i32) i32.const 0)
	)`
	raw, err = wat.Compile(wrongSigF32Wat)
	if err != nil {
		t.Fatalf("wat.Compile wrong sig f32: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when pointer param is f32")
	}

	// 6. Wrong signature: asyncify_stop_unwind returns i32 instead of void
	wrongSigStopUnwindRetWat := `(module
		(func (export "asyncify_start_unwind") (param i32))
		(func (export "asyncify_stop_unwind") (result i32) i32.const 0)
		(func (export "asyncify_start_rewind") (param i32))
		(func (export "asyncify_stop_rewind"))
		(func (export "asyncify_get_state") (result i32) i32.const 0)
	)`
	raw, err = wat.Compile(wrongSigStopUnwindRetWat)
	if err != nil {
		t.Fatalf("wat.Compile wrong sig stop_unwind result: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when asyncify_stop_unwind returns a value")
	}

	// 7. Wrong signature: asyncify_get_state returns nothing instead of i32
	wrongSigGetStateVoidWat := `(module
		(func (export "asyncify_start_unwind") (param i32))
		(func (export "asyncify_stop_unwind"))
		(func (export "asyncify_start_rewind") (param i32))
		(func (export "asyncify_stop_rewind"))
		(func (export "asyncify_get_state"))
	)`
	raw, err = wat.Compile(wrongSigGetStateVoidWat)
	if err != nil {
		t.Fatalf("wat.Compile wrong sig get_state void: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when asyncify_get_state returns void")
	}

	// 8. Pointer type mismatch: start_unwind is i32, but start_rewind is i64
	mismatchedPtrWat := `(module
		(func (export "asyncify_start_unwind") (param i32))
		(func (export "asyncify_stop_unwind"))
		(func (export "asyncify_start_rewind") (param i64))
		(func (export "asyncify_stop_rewind"))
		(func (export "asyncify_get_state") (result i32) i32.const 0)
	)`
	raw, err = wat.Compile(mismatchedPtrWat)
	if err != nil {
		t.Fatalf("wat.Compile mismatched pointer: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when start_unwind and start_rewind have mismatched pointer widths")
	}

	// 9. Partial helper exports: 4 out of 5 helpers (missing asyncify_get_state)
	missingGetStateWat := `(module
		(func (export "asyncify_start_unwind") (param i32))
		(func (export "asyncify_stop_unwind"))
		(func (export "asyncify_start_rewind") (param i32))
		(func (export "asyncify_stop_rewind"))
	)`
	raw, err = wat.Compile(missingGetStateWat)
	if err != nil {
		t.Fatalf("wat.Compile missing get_state: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when asyncify_get_state is missing")
	}

	// 10. Partial helper exports: only asyncify_start_unwind exported
	onlyOneWat := `(module
		(func (export "asyncify_start_unwind") (param i32))
	)`
	raw, err = wat.Compile(onlyOneWat)
	if err != nil {
		t.Fatalf("wat.Compile only one: %v", err)
	}
	if asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=false when only 1 helper is exported")
	}
}

// TestIsAsyncified_Wasm64Support verifies that 64-bit asyncified modules
// (where data pointers are i64) are correctly detected as asyncified.
func TestIsAsyncified_Wasm64Support(t *testing.T) {
	wasm64Wat := `(module
		(func (export "asyncify_start_unwind") (param i64))
		(func (export "asyncify_stop_unwind"))
		(func (export "asyncify_start_rewind") (param i64))
		(func (export "asyncify_stop_rewind"))
		(func (export "asyncify_get_state") (result i32) i32.const 0)
		(func (export "main") (result i64) i64.const 42)
	)`
	raw, err := wat.Compile(wasm64Wat)
	if err != nil {
		t.Fatalf("wat.Compile wasm64: %v", err)
	}
	if !asyncify.IsAsyncified(raw) {
		t.Error("expected IsAsyncified=true for valid wasm64 module")
	}
}

// TestIsAsyncified_ActualTransformedModules verifies that genuinely transformed
// modules (Binaryen pre-transformed and Wippy-transformed) return true.
func TestIsAsyncified_ActualTransformedModules(t *testing.T) {
	// 1. Wippy-transformed module
	rawWat := `(module
		(import "env" "sleep" (func $sleep (param i32)))
		(memory (export "memory") 1)
		(func (export "run")
			i32.const 50
			call $sleep
		)
	)`
	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	if asyncify.IsAsyncified(raw) {
		t.Fatal("pre-transformation module must not be asyncified")
	}

	transformed, err := asyncify.Transform(raw, asyncify.Config{
		AsyncImports: []string{"env.sleep"},
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if !asyncify.IsAsyncified(transformed) {
		t.Error("expected IsAsyncified=true for Wippy-transformed module")
	}

	// 2. Binaryen pre-transformed demo wasm
	demoBytes, err := os.ReadFile("../testbed/asyncify-demo/asyncify_demo.wasm")
	if err == nil {
		if !asyncify.IsAsyncified(demoBytes) {
			t.Error("expected IsAsyncified=true for Binaryen asyncify_demo.wasm")
		}
	}

	// 3. Binaryen streaming demo wasm
	streamingBytes, err := os.ReadFile("../testbed/streaming-demo/streaming_demo.wasm")
	if err == nil {
		if !asyncify.IsAsyncified(streamingBytes) {
			t.Error("expected IsAsyncified=true for Binaryen streaming_demo.wasm")
		}
	}
}

// TestIsAsyncified_MalformedInputNoPanic verifies that malformed, truncated,
// and corrupted byte sequences return false without panicking.
func TestIsAsyncified_MalformedInputNoPanic(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"nil slice", nil},
		{"empty slice", []byte{}},
		{"1 byte", []byte{0x00}},
		{"3 bytes", []byte{0x00, 0x61, 0x73}},
		{"4 bytes magic only", []byte{0x00, 0x61, 0x73, 0x6d}},
		{"7 bytes truncated version", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00}},
		{"bad magic", []byte{0x01, 0x02, 0x03, 0x04, 0x01, 0x00, 0x00, 0x00}},
		{"bad version", []byte{0x00, 0x61, 0x73, 0x6d, 0x02, 0x00, 0x00, 0x00}},
		{"valid header + fake string bytes", append([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, []byte("asyncify_start_unwind")...)},
		{"corrupted section size", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{"truncated section content", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x50, 0x01}},
		{"out of order section", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x07, 0x01, 0x00, 0x01, 0x01, 0x00}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked on %s: %v", tc.name, r)
				}
			}()
			if asyncify.IsAsyncified(tc.data) {
				t.Errorf("expected IsAsyncified=false for %s", tc.name)
			}
		})
	}
}

// TestDirectCallHostYield_WithHelperTextInData_TransformsAndResumes verifies the critical scenario:
// A module containing asyncify helper text in its data section and a direct call to a host yield
// function is NOT bypassed by IsAsyncified, undergoes proper Asyncify transformation, and
// successfully suspends and resumes one effect on both compiler and interpreter backends.
func TestDirectCallHostYield_WithHelperTextInData_TransformsAndResumes(t *testing.T) {
	rawWat := `(module
		(import "env" "yield" (func $yield))
		(memory (export "memory") 1)
		(global $effect (mut i32) (i32.const 0))

		;; Ordinary data segment containing asyncify helper text.
		;; Prior defective implementation used bytes.Contains() and mistakenly
		;; considered this module pre-asyncified, bypassing transformation!
		(data (i32.const 0) "asyncify_start_unwind asyncify_stop_unwind asyncify_start_rewind asyncify_stop_rewind asyncify_get_state")

		(func (export "run") (result i32)
			;; Pre-yield effect
			global.get $effect
			i32.const 1
			i32.add
			global.set $effect

			;; Host yield call
			call $yield

			;; Post-yield effect
			global.get $effect
			i32.const 10
			i32.add
			global.set $effect

			global.get $effect
		)

		(func (export "get_effect") (result i32)
			global.get $effect
		)
	)`

	raw, err := wat.Compile(rawWat)
	if err != nil {
		t.Fatalf("wat.Compile: %v", err)
	}

	// 1. Defect verification: module must NOT be detected as asyncified before transformation!
	if asyncify.IsAsyncified(raw) {
		t.Fatal("DEFECT DETECTED: module with helper text in data was falsely identified as asyncified!")
	}

	// 2. Module is properly transformed because IsAsyncified returned false
	transformed, err := asyncify.Transform(raw, asyncify.Config{
		AsyncImports: []string{"env.yield"},
	})
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// 3. Post-transformation check: structurally asyncified
	if !asyncify.IsAsyncified(transformed) {
		t.Fatal("transformed module must be identified as asyncified")
	}

	// 4. Test actual execution, suspension, and resume on both compiler and interpreter backends
	for _, backend := range []string{"compiler", "interpreter"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()

			var cfg wazero.RuntimeConfig
			if backend == "interpreter" {
				cfg = wazero.NewRuntimeConfigInterpreter()
			} else {
				cfg = wazero.NewRuntimeConfigCompiler()
			}

			rt := wazero.NewRuntimeWithConfig(ctx, cfg)
			defer rt.Close(ctx)

			var yieldCount int
			_, err = rt.NewHostModuleBuilder("env").
				NewFunctionBuilder().
				WithFunc(func(ctx context.Context, mod api.Module) {
					st, callErr := mod.ExportedFunction("asyncify_get_state").Call(ctx)
					if callErr != nil {
						t.Fatalf("asyncify_get_state: %v", callErr)
					}
					state := st[0]
					switch state {
					case 0: // Normal -> start unwinding
						yieldCount++
						// Asyncify stack buffer at 1024: [stack_ptr=1032, stack_end=8192]
						if !mod.Memory().WriteUint32Le(1024, 1032) || !mod.Memory().WriteUint32Le(1028, 8192) {
							t.Fatal("failed to write async stack")
						}
						if _, err := mod.ExportedFunction("asyncify_start_unwind").Call(ctx, 1024); err != nil {
							t.Fatalf("asyncify_start_unwind: %v", err)
						}
					case 2: // Rewinding -> stop rewinding
						if _, err := mod.ExportedFunction("asyncify_stop_rewind").Call(ctx); err != nil {
							t.Fatalf("asyncify_stop_rewind: %v", err)
						}
					default:
						t.Fatalf("unexpected asyncify state during host call: %d", state)
					}
				}).
				Export("yield").
				Instantiate(ctx)
			if err != nil {
				t.Fatalf("host instantiate: %v", err)
			}

			inst, err := rt.Instantiate(ctx, transformed)
			if err != nil {
				t.Fatalf("module instantiate: %v", err)
			}
			defer inst.Close(ctx)

			// Step 1: Call run() -> calls yield() -> unwinds
			_, err = inst.ExportedFunction("run").Call(ctx)
			if err != nil {
				t.Fatalf("initial run call: %v", err)
			}

			// Verify unwind state
			st, err := inst.ExportedFunction("asyncify_get_state").Call(ctx)
			if err != nil || len(st) != 1 || st[0] != 1 {
				t.Fatalf("expected state 1 (unwinding), got %v (err: %v)", st, err)
			}

			// Verify pre-yield effect has run once
			effectRes, err := inst.ExportedFunction("get_effect").Call(ctx)
			if err != nil || effectRes[0] != 1 {
				t.Fatalf("expected effect=1 during unwind, got %v", effectRes)
			}

			// Step 2: Stop unwind
			if _, err := inst.ExportedFunction("asyncify_stop_unwind").Call(ctx); err != nil {
				t.Fatalf("stop_unwind: %v", err)
			}

			// Step 3: Start rewind
			if _, err := inst.ExportedFunction("asyncify_start_rewind").Call(ctx, 1024); err != nil {
				t.Fatalf("start_rewind: %v", err)
			}

			// Step 4: Resume execution by re-calling run()
			res, err := inst.ExportedFunction("run").Call(ctx)
			if err != nil {
				t.Fatalf("resume run call: %v", err)
			}

			if len(res) != 1 || res[0] != 11 {
				t.Fatalf("expected final result 11, got %v", res)
			}

			// Verify final effect
			effectRes, err = inst.ExportedFunction("get_effect").Call(ctx)
			if err != nil || effectRes[0] != 11 {
				t.Fatalf("expected final effect=11, got %v", effectRes)
			}

			if yieldCount != 1 {
				t.Fatalf("expected exactly 1 yield, got %d", yieldCount)
			}
		})
	}
}

// Benchmarks to measure parser overhead: full ParseModule vs metadata-only ParseModuleMetadata.
func BenchmarkParseModule_Full(b *testing.B) {
	data, err := os.ReadFile("../testbed/asyncify-demo/asyncify_demo.wasm")
	if err != nil {
		b.Skipf("demo wasm not found: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m, err := wasm.ParseModule(data)
		if err != nil || m == nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseModule_Metadata(b *testing.B) {
	data, err := os.ReadFile("../testbed/asyncify-demo/asyncify_demo.wasm")
	if err != nil {
		b.Skipf("demo wasm not found: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m, err := wasm.ParseModuleMetadata(data)
		if err != nil || m == nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsAsyncified_DemoWasm(b *testing.B) {
	data, err := os.ReadFile("../testbed/asyncify-demo/asyncify_demo.wasm")
	if err != nil {
		b.Skipf("demo wasm not found: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !asyncify.IsAsyncified(data) {
			b.Fatal("expected true")
		}
	}
}

func BenchmarkIsAsyncified_WithFakeData(b *testing.B) {
	rawWat := `(module
		(memory 1)
		(data (i32.const 0) "asyncify_start_unwind asyncify_stop_unwind asyncify_start_rewind asyncify_stop_rewind asyncify_get_state")
		(func (export "run") (result i32) i32.const 42)
	)`
	data, err := wat.Compile(rawWat)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if asyncify.IsAsyncified(data) {
			b.Fatal("expected false")
		}
	}
}
