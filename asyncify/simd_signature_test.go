package asyncify

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

func wasmSection(id byte, contents ...byte) []byte {
	return append([]byte{id, byte(len(contents))}, contents...)
}

// simdAcrossAsyncModule keeps a vector live across an async call and applies
// SIMD operations whose operand count and result type differ.
func simdAcrossAsyncModule() []byte {
	body := []byte{
		0x01, 0x01, 0x7b, // one v128 local
		0xfd, 0x0c, 0x01, 0x00, 0xfe, 0xff, 0x03, 0x00, 0xfc, 0xff, 0x05, 0x00, 0x06, 0x00, 0x07, 0x00, 0x08, 0x00, // v128.const i16x8 1 -2 3 -4 5 6 7 8
		0xfd, 0xa7, 0x01, 0x21, 0x00, // i32x4.extend_low_i16x8_s
		0x20, 0x00, 0x10, 0x00, 0xfd, 0x11, 0xfd, 0xae, 0x01, 0x21, 0x00, // i32x4.add v (i32x4.splat (call async))
		0x20, 0x00, 0x41, 0x01, 0xfd, 0xab, 0x01, 0x21, 0x00, // i32x4.shl v 1
		0x20, 0x00, 0xfd, 0xfa, 0x01, 0x21, 0x00, // f32x4.convert_i32x4_s
		0x20, 0x00, 0xfd, 0x67, 0x21, 0x00, // f32x4.ceil
		0x20, 0x00, 0xfd, 0xf8, 0x01, 0x21, 0x00, // i32x4.trunc_sat_f32x4_s
		0x20, 0x00, 0x20, 0x00, 0xfd, 0xbc, 0x01, 0x21, 0x00, // i32x4.extmul_low_i16x8_s
		0x41, 0x00, 0x20, 0x00, 0xfd, 0x0b, 0x04, 0x00, // v128.store
		0x20, 0x00, 0xfd, 0x1b, 0x01, // i32x4.extract_lane 1
		0x20, 0x00, 0xfd, 0xa3, 0x01, // i32x4.all_true
		0x6a, 0x0b, // i32.add, end
	}
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	module = append(module, wasmSection(1, 0x01, 0x60, 0x00, 0x01, 0x7f)...)
	module = append(module, wasmSection(2, 0x01, 0x03, 'e', 'n', 'v', 0x05, 'a', 's', 'y', 'n', 'c', 0x00, 0x00)...)
	module = append(module, wasmSection(3, 0x01, 0x00)...)
	module = append(module, wasmSection(5, 0x01, 0x00, 0x01)...)
	module = append(module, wasmSection(7, 0x01, 0x04, 't', 'e', 's', 't', 0x00, 0x01)...)
	code := append([]byte{0x01, byte(len(body))}, body...)
	return append(module, wasmSection(10, code...)...)
}

func TestTransformKeepsSIMDOperandsAcrossAsyncCall(t *testing.T) {
	wasmBytes := simdAcrossAsyncModule()
	transformed, err := Transform(wasmBytes, Config{Matcher: NewExactMatcher([]string{"env.async"})})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	run := func(module []byte) uint32 {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		defer rt.Close(ctx)
		_, err := rt.NewHostModuleBuilder("env").NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
				stack[0] = 10
			}), nil, []api.ValueType{api.ValueTypeI32}).Export("async").Instantiate(ctx)
		if err != nil {
			t.Fatalf("host module: %v", err)
		}
		mod, err := rt.Instantiate(ctx, module)
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		results, err := mod.ExportedFunction("test").Call(ctx)
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return uint32(results[0])
	}

	want := run(wasmBytes)
	if got := run(transformed); got != want {
		t.Fatalf("transformed result = %d, want %d", got, want)
	}
}

func TestTransformKeepsSIMDOperandsAcrossSuspendResume(t *testing.T) {
	code, err := Transform(simdAcrossAsyncModule(), Config{Matcher: NewExactMatcher([]string{"env.async"})})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = rt.Close(ctx) })
	calls := 0
	_, err = rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, module api.Module, stack []uint64) {
		calls++
		name := "asyncify_start_unwind"
		var args []uint64
		if calls == 1 {
			if !module.Memory().WriteUint32Le(1024, 1032) || !module.Memory().WriteUint32Le(1028, 8192) {
				t.Fatal("write stack header")
			}
			args = []uint64{1024}
		} else {
			name = "asyncify_stop_rewind"
		}
		if _, err := module.ExportedFunction(name).Call(ctx, args...); err != nil {
			t.Fatal(err)
		}
		stack[0] = 10
	}), nil, []api.ValueType{api.ValueTypeI32}).Export("async").Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	module, err := rt.Instantiate(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	call := func(name string, args ...uint64) []uint64 {
		t.Helper()
		results, err := module.ExportedFunction(name).Call(ctx, args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return results
	}
	call("test")
	if state := call("asyncify_get_state"); len(state) != 1 || state[0] != 1 {
		t.Fatalf("did not unwind: %v", state)
	}
	call("asyncify_stop_unwind")
	call("asyncify_start_rewind", 1024)
	got := call("test")
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("resumed SIMD result = %v, want [0]", got)
	}
	for lane, want := range []uint32{484, 0, 256, 0} {
		got, ok := module.Memory().ReadUint32Le(uint32(lane * 4))
		if !ok || got != want {
			t.Fatalf("vector lane %d = %d, want %d", lane, got, want)
		}
	}
	if calls != 2 {
		t.Fatalf("host calls = %d, want suspend and resume", calls)
	}
	if state := call("asyncify_get_state"); len(state) != 1 || state[0] != 0 {
		t.Fatalf("rewind not finished: %v", state)
	}
}
