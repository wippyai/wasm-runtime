package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/wat"
)

// btreeInsertShiftWat is the LLVM-style mix from sqlite3BtreeInsert: void
// blocks, leftover-stack select, overlapping memory.copy, SP global, param
// reuse, and a (i32 i32 i32)->() call_indirect.
const btreeInsertShiftWat = `(module
  (memory (export "memory") 1)
  (global $sp (mut i32) (i32.const 1024))
  (global $mb i32 (i32.const 0))
  (table 1 funcref)
  (type $cb (func (param i32 i32 i32)))
  (func $cb (param i32 i32 i32)
    (i32.store8 (local.get 0) (i32.add (local.get 1) (local.get 2))))
  (elem (i32.const 0) $cb)
  (func (export "run") (param $p0 i32) (result i32)
    (local $sp i32) (local $a i32) (local $b i32) (local $c i32) (local $n i32) (local $d i32) (local $e i32)
    global.get $sp
    i32.const 32
    i32.sub
    local.tee $sp
    global.set $sp
    (i32.store (i32.const 0) (i32.const 0x01020304))
    (i32.store (i32.const 4) (i32.const 0x05060708))
    (local.set $a (i32.const 8))
    (local.set $b (i32.const 4))
    (local.set $c (i32.const 6))
    (local.set $d (i32.const 2))
    (local.set $e (i32.const 0))
    (block $out
      (block $skip
        local.get $a
        local.get $b
        local.get $c
        local.get $b
        local.get $c
        i32.lt_s
        select
        local.tee $n
        i32.lt_s
        br_if $skip
        (br_if $out (i32.eqz (local.get $n)))
        (memory.copy (local.get $e) (local.get $d) (local.get $n))
        br $out)
      (memory.fill (i32.const 0) (i32.const 0xff) (i32.const 4)))
    local.get $sp
    global.get $mb
    local.tee $p0
    i32.add
    i32.const 7
    i32.store offset=0
    (call_indirect (type $cb) (i32.const 16) (i32.const 10) (i32.const 5) (i32.const 0))
    local.get $sp
    i32.const 32
    i32.add
    global.set $sp
    (i32.add (i32.load (i32.const 0)) (i32.load8_u (i32.const 16)))))`

func evalExportRun(t *testing.T, code []byte) (results []uint64, mem []byte) {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = rt.Close(ctx) })
	mod, err := rt.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	res, err := mod.ExportedFunction("run").Call(ctx, 99)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := mod.Memory().Read(0, 32)
	if !ok {
		t.Fatal("memory read")
	}
	out := append([]byte(nil), raw...)
	return res, out
}

func TestAsyncify_BtreeInsertShiftNormalPath(t *testing.T) {
	raw, err := wat.Compile(btreeInsertShiftWat)
	if err != nil {
		t.Fatal(err)
	}
	wantRes, wantMem := evalExportRun(t, raw)
	tr, err := asyncify.Transform(raw, asyncify.Config{ExportGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	gotRes, gotMem := evalExportRun(t, tr)
	if len(gotRes) != len(wantRes) || gotRes[0] != wantRes[0] {
		t.Fatalf("run result=%v want %v", gotRes, wantRes)
	}
	for i := range wantMem {
		if gotMem[i] != wantMem[i] {
			t.Fatalf("memory[%d]=%d want %d\n got %v\nwant %v", i, gotMem[i], wantMem[i], gotMem, wantMem)
		}
	}
}

func TestAsyncify_BtreeInsertShiftBinaryen(t *testing.T) {
	wasmOpt := os.Getenv("WASM_OPT")
	if wasmOpt == "" {
		wasmOpt = "wasm-opt"
	}
	wasmOpt, err := exec.LookPath(wasmOpt)
	if err != nil {
		if os.Getenv("WASM_OPT") != "" {
			t.Fatalf("configured WASM_OPT is unavailable: %v", err)
		}
		t.Skip("wasm-opt not found; install it or set WASM_OPT")
	}

	raw, err := wat.Compile(btreeInsertShiftWat)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.wasm")
	out := filepath.Join(dir, "out.wasm")
	if err := os.WriteFile(in, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), wasmOpt, in, "--enable-bulk-memory", "--asyncify", "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wasm-opt --asyncify: %v\n%s", err, b)
	}
	bin, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	wantRes, wantMem := evalExportRun(t, bin)
	tr, err := asyncify.Transform(raw, asyncify.Config{ExportGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	gotRes, gotMem := evalExportRun(t, tr)
	if len(gotRes) != len(wantRes) || gotRes[0] != wantRes[0] {
		t.Fatalf("ours %v binaryen %v", gotRes, wantRes)
	}
	for i := range wantMem {
		if gotMem[i] != wantMem[i] {
			t.Fatalf("memory[%d] ours=%d binaryen=%d", i, gotMem[i], wantMem[i])
		}
	}
}

func TestAsyncify_CallIndirectCopyUnwindRewind(t *testing.T) {
	ctx := context.Background()
	code := transformModule(t, `(module
  (import "env" "pause" (func $pause (result i32)))
  (memory (export "memory") 1)
  (table 1 funcref)
  (type $cb (func (param i32 i32 i32)))
  (func $cb (param i32 i32 i32)
    (i32.store8 (local.get 0) (i32.add (local.get 1) (local.get 2))))
  (elem (i32.const 0) $cb)
  (func (export "run") (result i32) (local $n i32)
    (i32.store (i32.const 0) (i32.const 0x01020304))
    (i32.store (i32.const 4) (i32.const 0x05060708))
    (memory.copy (i32.const 2) (i32.const 0) (i32.const 6))
    (local.set $n (call $pause))
    (call_indirect (type $cb) (i32.const 16) (local.get $n) (i32.const 5) (i32.const 0))
    (i32.add (i32.load (i32.const 2)) (i32.load8_u (i32.const 16)))))`, []string{"env.pause"})
	runtime := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = runtime.Close(ctx) })
	_, err := runtime.NewHostModuleBuilder("env").NewFunctionBuilder().WithGoModuleFunction(MakeAsyncHandler(func(context.Context, api.Module, []uint64) PendingOp {
		return &traceOp{name: "pause", id: 1}
	}), nil, []api.ValueType{api.ValueTypeI32}).Export("pause").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	module, err := runtime.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	async := NewAsyncify()
	if err := async.Init(module); err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(async)
	ctx = WithScheduler(WithAsyncify(ctx, async), scheduler)
	if err := scheduler.Execute(ctx, module.ExportedFunction("run")); err != nil {
		t.Fatal(err)
	}
	first, err := scheduler.Step(ctx, nil)
	if err != nil || first.Status != StepContinue {
		t.Fatalf("suspend: %v %v", first, err)
	}
	last, err := scheduler.Step(ctx, &YieldResult{Value: 10})
	if err != nil || last.Status != StepDone {
		t.Fatalf("resume: %v %v", last, err)
	}
	// overlapping copy of 0x01020304/0x05060708 by 2 bytes -> 0x01020301 at offset 2 as i32le
	// plus call_indirect stores 15 at byte 16.
	if len(last.Results) != 1 {
		t.Fatalf("results %v", last.Results)
	}
	b, ok := module.Memory().Read(2, 4)
	if !ok {
		t.Fatal("memory read")
	}
	copied := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	if last.Results[0] != uint64(copied)+15 {
		t.Fatalf("got %d memle=%d want memle+15", last.Results[0], copied)
	}
}
