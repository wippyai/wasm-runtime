package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/transcoder"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"go.bytecodealliance.org/wit"
)

func streamErrorType() wit.Type {
	return &wit.TypeDef{Kind: &wit.Variant{Cases: []wit.Case{
		{Name: "last-operation-failed", Type: &wit.TypeDef{Kind: &wit.Own{}}},
		{Name: "closed"},
	}}}
}

func resultU64StreamError() *wit.TypeDef {
	return &wit.TypeDef{Kind: &wit.Result{OK: wit.U64{}, Err: streamErrorType()}}
}

func resultListU8StreamError() *wit.TypeDef {
	return &wit.TypeDef{Kind: &wit.Result{OK: &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}, Err: streamErrorType()}}
}

func resultUnitStreamError() *wit.TypeDef {
	return &wit.TypeDef{Kind: &wit.Result{Err: streamErrorType()}}
}

func compileU32U64Types(t *testing.T) (*transcoder.Decoder, []*transcoder.CompiledType) {
	t.Helper()
	compiler := transcoder.NewCompiler()
	ctU32, err := compiler.Compile(wit.U32{}, reflect.TypeOf(uint32(0)))
	if err != nil {
		t.Fatalf("compile u32: %v", err)
	}
	ctU64, err := compiler.Compile(wit.U64{}, reflect.TypeOf(uint64(0)))
	if err != nil {
		t.Fatalf("compile u64: %v", err)
	}
	return transcoder.NewDecoderWithCompiler(compiler), []*transcoder.CompiledType{ctU32, ctU64}
}

func compileU32ListU8Types(t *testing.T) (*transcoder.Decoder, []*transcoder.CompiledType) {
	t.Helper()
	compiler := transcoder.NewCompiler()
	ctU32, err := compiler.Compile(wit.U32{}, reflect.TypeOf(uint32(0)))
	if err != nil {
		t.Fatalf("compile u32: %v", err)
	}
	ctList, err := compiler.Compile(&wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}, reflect.TypeOf([]byte{}))
	if err != nil {
		t.Fatalf("compile list<u8>: %v", err)
	}
	return transcoder.NewDecoderWithCompiler(compiler), []*transcoder.CompiledType{ctU32, ctList}
}

func readResultBytes(t *testing.T, inst *WazeroInstance, addr, n uint32) []byte {
	t.Helper()
	data, ok := inst.instance.Memory().Read(addr, n)
	if !ok {
		t.Fatalf("read memory at %d len %d", addr, n)
	}
	return append([]byte(nil), data...)
}

func readResultListU8(t *testing.T, inst *WazeroInstance, retptr, payloadOff uint32) (byte, []byte) {
	t.Helper()
	header := readResultBytes(t, inst, retptr, payloadOff+8)
	if header[0] != 0 {
		return header[0], nil
	}
	ptr := binary.LittleEndian.Uint32(header[payloadOff:])
	n := binary.LittleEndian.Uint32(header[payloadOff+4:])
	if n == 0 {
		return 0, []byte{}
	}
	return 0, readResultBytes(t, inst, ptr, n)
}

func TestBindResult1_CanonicalSuccessMatchesDynamic(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	def := &component.LowerDef{
		Name:    "check-write",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{resultU64StreamError()},
	}
	handler := func(_ context.Context, self uint32) (uint64, error) {
		if self != 7 {
			t.Fatalf("self=%d", self)
		}
		return 4096, nil
	}
	typed, err := NewLowerWrapper(def, BindResult1(handler))
	if err != nil {
		t.Fatal(err)
	}
	if typed.typedInvoke == nil {
		t.Fatal("typed invocation not selected")
	}
	dynamic, err := NewLowerWrapper(def, handler)
	if err != nil {
		t.Fatal(err)
	}
	if dynamic.typedInvoke != nil {
		t.Fatal("dynamic handler selected typed path")
	}
	typed.handler = reflect.Value{}
	const retTyped, retDyn uint32 = 64, 128
	typed.BuildRawFunc()(ctx, inst.instance, []uint64{7, uint64(retTyped)})
	dynamic.BuildRawFunc()(ctx, inst.instance, []uint64{7, uint64(retDyn)})
	got := readResultBytes(t, inst, retTyped, typed.resultAreaSize)
	want := readResultBytes(t, inst, retDyn, dynamic.resultAreaSize)
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical success mismatch\ngot  %v\nwant %v", got, want)
	}
	if got[0] != 0 {
		t.Fatalf("discriminant=%d, want 0", got[0])
	}
	if v := binary.LittleEndian.Uint64(got[typed.resultPayloadOffset:]); v != 4096 {
		t.Fatalf("ok payload=%d, want 4096", v)
	}
}

func TestBindResult2_CanonicalSuccessMatchesDynamic(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	def := &component.LowerDef{
		Name:    "read",
		Params:  []wit.Type{wit.U32{}, wit.U64{}},
		Results: []wit.Type{resultListU8StreamError()},
	}
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	handler := func(_ context.Context, self uint32, n uint64) ([]byte, error) {
		if self != 3 || n != 16 {
			t.Fatalf("self=%d n=%d", self, n)
		}
		return payload, nil
	}
	typed, err := NewLowerWrapper(def, BindResult2(handler))
	if err != nil {
		t.Fatal(err)
	}
	if typed.typedInvoke == nil {
		t.Fatal("typed invocation not selected")
	}
	dynamic, err := NewLowerWrapper(def, handler)
	if err != nil {
		t.Fatal(err)
	}
	typed.handler = reflect.Value{}
	const retTyped, retDyn uint32 = 64, 128
	typed.BuildRawFunc()(ctx, inst.instance, []uint64{3, 16, uint64(retTyped)})
	dynamic.BuildRawFunc()(ctx, inst.instance, []uint64{3, 16, uint64(retDyn)})
	discGot, dataGot := readResultListU8(t, inst, retTyped, typed.resultPayloadOffset)
	discWant, dataWant := readResultListU8(t, inst, retDyn, dynamic.resultPayloadOffset)
	if discGot != 0 || discWant != 0 || !bytes.Equal(dataGot, payload) || !bytes.Equal(dataWant, payload) {
		t.Fatalf("list result typed=(%d,%v) dynamic=(%d,%v)", discGot, dataGot, discWant, dataWant)
	}
}

func TestBindResult1_StreamErrorVariantAndHandle(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	def := &component.LowerDef{
		Name:    "check-write",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{resultU64StreamError()},
	}
	closed := &preview2.StreamError{Closed: true}
	failed := &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: 42}
	for _, tc := range []struct {
		err  *preview2.StreamError
		name string
	}{
		{name: "closed", err: closed},
		{name: "last-operation-failed", err: failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := func(context.Context, uint32) (uint64, error) { return 0, tc.err }
			typed, err := NewLowerWrapper(def, BindResult1(handler))
			if err != nil {
				t.Fatal(err)
			}
			dynamic, err := NewLowerWrapper(def, handler)
			if err != nil {
				t.Fatal(err)
			}
			typed.handler = reflect.Value{}
			const retTyped, retDyn uint32 = 64, 128
			typed.BuildRawFunc()(ctx, inst.instance, []uint64{1, uint64(retTyped)})
			dynamic.BuildRawFunc()(ctx, inst.instance, []uint64{1, uint64(retDyn)})
			got := readResultBytes(t, inst, retTyped, typed.resultAreaSize)
			want := readResultBytes(t, inst, retDyn, dynamic.resultAreaSize)
			if !bytes.Equal(got, want) {
				t.Fatalf("stream-error encoding mismatch\ngot  %v\nwant %v", got, want)
			}
			if got[0] != 1 {
				t.Fatalf("discriminant=%d, want 1", got[0])
			}
		})
	}
}

func TestBindResult2_NilSuccess(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	def := &component.LowerDef{
		Name:    "read",
		Params:  []wit.Type{wit.U32{}, wit.U64{}},
		Results: []wit.Type{resultListU8StreamError()},
	}
	handler := func(context.Context, uint32, uint64) ([]byte, error) { return nil, nil }
	typed, err := NewLowerWrapper(def, BindResult2(handler))
	if err != nil {
		t.Fatal(err)
	}
	dynamic, err := NewLowerWrapper(def, handler)
	if err != nil {
		t.Fatal(err)
	}
	typed.handler = reflect.Value{}
	const retTyped, retDyn uint32 = 64, 128
	typed.BuildRawFunc()(ctx, inst.instance, []uint64{1, 8, uint64(retTyped)})
	dynamic.BuildRawFunc()(ctx, inst.instance, []uint64{1, 8, uint64(retDyn)})
	discGot, dataGot := readResultListU8(t, inst, retTyped, typed.resultPayloadOffset)
	discWant, dataWant := readResultListU8(t, inst, retDyn, dynamic.resultPayloadOffset)
	if discGot != 0 || discWant != 0 || len(dataGot) != 0 || len(dataWant) != 0 {
		t.Fatalf("nil success typed=(%d,%v) dynamic=(%d,%v)", discGot, dataGot, discWant, dataWant)
	}
}

func TestBindResult2_UnitResultMatchesErrorOnly(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	def := &component.LowerDef{
		Name:    "write",
		Params:  []wit.Type{wit.U32{}, &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}},
		Results: []wit.Type{resultUnitStreamError()},
	}
	failed := &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: 9}
	mem := inst.instance.Memory()
	mem.Write(200, []byte("ok"))
	for _, tc := range []struct {
		err  *preview2.StreamError
		name string
	}{
		{name: "ok", err: nil},
		{name: "failed", err: failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typedHandler := func(_ context.Context, self uint32, data []byte) (struct{}, error) {
				if self != 4 || string(data) != "ok" {
					t.Fatalf("self=%d data=%q", self, data)
				}
				if tc.err != nil {
					return struct{}{}, tc.err
				}
				return struct{}{}, nil
			}
			dynamicHandler := func(_ context.Context, self uint32, data []byte) *preview2.StreamError {
				if self != 4 || string(data) != "ok" {
					t.Fatalf("self=%d data=%q", self, data)
				}
				return tc.err
			}
			typed, err := NewLowerWrapper(def, BindResult2(typedHandler))
			if err != nil {
				t.Fatal(err)
			}
			if typed.typedInvoke == nil {
				t.Fatal("struct{} unit result must select typed invocation")
			}
			dynamic, err := NewLowerWrapper(def, dynamicHandler)
			if err != nil {
				t.Fatal(err)
			}
			if !dynamic.errorOnly {
				t.Fatal("dynamic unit handler must use error-only constructor")
			}
			typed.handler = reflect.Value{}
			const retTyped, retDyn uint32 = 64, 128
			stack := []uint64{4, 200, 2, 0}
			typedStack := append([]uint64(nil), stack...)
			typedStack[3] = uint64(retTyped)
			dynStack := append([]uint64(nil), stack...)
			dynStack[3] = uint64(retDyn)
			typed.BuildRawFunc()(ctx, inst.instance, typedStack)
			dynamic.BuildRawFunc()(ctx, inst.instance, dynStack)
			got := readResultBytes(t, inst, retTyped, typed.resultAreaSize)
			want := readResultBytes(t, inst, retDyn, dynamic.resultAreaSize)
			if !bytes.Equal(got, want) {
				t.Fatalf("unit result encoding mismatch\ngot  %v\nwant %v", got, want)
			}
			if tc.err == nil && got[0] != 0 {
				t.Fatalf("success discriminant=%d, want 0", got[0])
			}
			if tc.err != nil && got[0] != 1 {
				t.Fatalf("error discriminant=%d, want 1", got[0])
			}
		})
	}
}

func TestBindResult2_InvalidListRejectsBeforeHost(t *testing.T) {
	dec, paramTypes := compileU32ListU8Types(t)
	mem := newTestMemory(32)
	called := false
	handler := func(context.Context, uint32, []byte) (struct{}, error) {
		called = true
		return struct{}{}, nil
	}
	_, hostErr, liftErr := BindResult2(handler).invoke(context.Background(), dec, paramTypes, []uint64{1, 200, 16}, mem)
	if called {
		t.Fatal("host ran on invalid list lift")
	}
	if liftErr == nil {
		t.Fatal("expected lift error")
	}
	if hostErr != nil {
		t.Fatalf("hostErr=%v", hostErr)
	}
}

func TestBindResult1_MalformedStackAndNilChecks(t *testing.T) {
	dec, paramTypes := compileU32U64Types(t)
	mem := newTestMemory(64)
	called := false
	handler := func(context.Context, uint32) (uint64, error) {
		called = true
		return 1, nil
	}
	thf := BindResult1(handler)
	_, _, liftErr := thf.invoke(context.Background(), dec, paramTypes[:1], nil, mem)
	if liftErr == nil || called {
		t.Fatalf("short stack: liftErr=%v called=%v", liftErr, called)
	}
	_, _, liftErr = thf.invoke(context.Background(), nil, paramTypes[:1], []uint64{1}, mem)
	if liftErr == nil {
		t.Fatal("expected liftErr for nil decoder")
	}
	_, _, liftErr = thf.invoke(context.Background(), dec, nil, []uint64{1}, mem)
	if liftErr == nil {
		t.Fatal("expected liftErr for nil paramTypes")
	}
}

func TestBindResult2_MalformedStackAndNilChecks(t *testing.T) {
	dec, paramTypes := compileU32U64Types(t)
	mem := newTestMemory(64)
	called := false
	handler := func(context.Context, uint32, uint64) (uint64, error) {
		called = true
		return 1, nil
	}
	thf := BindResult2(handler)
	_, _, liftErr := thf.invoke(context.Background(), dec, paramTypes, []uint64{1}, mem)
	if liftErr == nil || called {
		t.Fatalf("short stack: liftErr=%v called=%v", liftErr, called)
	}
	_, _, liftErr = thf.invoke(context.Background(), nil, paramTypes, []uint64{1, 2}, mem)
	if liftErr == nil {
		t.Fatal("expected liftErr for nil decoder")
	}
	_, _, liftErr = thf.invoke(context.Background(), dec, nil, []uint64{1, 2}, mem)
	if liftErr == nil {
		t.Fatal("expected liftErr for nil paramTypes")
	}
}

func TestBindResult1_StreamErrorIdentity(t *testing.T) {
	dec, paramTypes := compileU32U64Types(t)
	mem := newTestMemory(64)
	want := &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: 11}
	handler := func(context.Context, uint32) (uint64, error) { return 0, want }
	_, hostErr, liftErr := BindResult1(handler).invoke(context.Background(), dec, paramTypes[:1], []uint64{1}, mem)
	if liftErr != nil {
		t.Fatalf("liftErr=%v", liftErr)
	}
	if hostErr != want { //nolint:errorlint // The adapter must preserve the original error object.
		t.Fatalf("hostErr=%p want %p", hostErr, want)
	}
}

func TestCheckedHostFunctionPreflightOnTypedWrite(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	called := 0
	w, err := NewLowerWrapper(&component.LowerDef{
		Name:    "write",
		Params:  []wit.Type{wit.U32{}, &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}},
		Results: []wit.Type{resultUnitStreamError()},
	}, CheckedHostFunction{
		Handler: BindResult2(func(context.Context, uint32, []byte) (struct{}, error) {
			called++
			return struct{}{}, nil
		}),
		Validate: func(context.Context, api.Module, []uint64) error {
			return errors.New("preflight")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.typedInvoke == nil {
		t.Fatal("typed invocation not selected")
	}
	defer func() {
		if recover() == nil || called != 0 {
			t.Fatalf("preflight must trap before host; calls=%d", called)
		}
	}()
	w.BuildRawFunc()(ctx, inst.instance, []uint64{1, 0, 1, 64})
}
