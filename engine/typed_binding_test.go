package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/wasm-runtime/component"
	"go.bytecodealliance.org/wit"
)

func TestTypedCanonicalDispatchAndPreflight(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	resultType := &wit.TypeDef{Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}}}
	def := &component.LowerDef{Name: "typed", Params: []wit.Type{wit.String{}, wit.String{}, &wit.TypeDef{Kind: &wit.List{Type: wit.U8{}}}}, Results: []wit.Type{resultType}}
	called := 0
	denied := false
	hostError := false
	w, err := NewLowerWrapper(def, CheckedHostFunction{
		Handler: BindResult3(func(_ context.Context, a, b string, data []byte) (uint32, error) {
			called++
			if a != "one" || b != "two" || string(data) != "body" {
				t.Fatalf("wrong arguments: %q %q %q", a, b, data)
			}
			if hostError {
				return 0, errors.New("denied")
			}
			return 42, nil
		}),
		Validate: func(context.Context, api.Module, []uint64) error {
			if denied {
				return errors.New("preflight")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.typedInvoke == nil {
		t.Fatal("typed invocation not selected")
	}
	// A successful call must not depend on the reflection dispatcher.
	w.handler = reflect.Value{}
	mem := inst.instance.Memory()
	mem.Write(200, []byte("onetwobody"))
	stack := []uint64{200, 3, 203, 3, 206, 4, 64}
	w.BuildRawFunc()(ctx, inst.instance, stack)
	v, _ := mem.ReadUint32Le(68)
	if v != 42 || called != 1 {
		t.Fatalf("value=%d calls=%d", v, called)
	}
	hostError = true
	w.BuildRawFunc()(ctx, inst.instance, stack)
	if disc, message := readResultString(t, inst, 64); disc != 1 || message != "denied" {
		t.Fatalf("host error: %d %q", disc, message)
	}
	denied = true
	defer func() {
		if recover() == nil || called != 2 {
			t.Fatalf("preflight must trap before host invocation; calls=%d", called)
		}
	}()
	stack[5] = 1 << 30
	w.BuildRawFunc()(ctx, inst.instance, stack)
}

func TestTypedCanonicalDynamicParameterFallback(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	def := &component.LowerDef{Name: "fallback", Params: []wit.Type{wit.U32{}, wit.U32{}, wit.U32{}}, Results: []wit.Type{&wit.TypeDef{Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}}}}}
	w, err := NewLowerWrapper(def, BindResult3(func(_ context.Context, a any, b, c uint32) (uint32, error) {
		return a.(uint32) + b + c, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if w.typedInvoke != nil {
		t.Fatal("unsupported interface parameter must use dynamic lifting")
	}
	w.BuildRawFunc()(ctx, inst.instance, []uint64{1, 2, 3, 64})
	value, _ := inst.instance.Memory().ReadUint32Le(68)
	if value != 6 {
		t.Fatalf("fallback result layout corrupted: %d", value)
	}
}

func TestCompiledNilRecordResultTraps(t *testing.T) {
	type record struct{ Value uint32 }
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	result := &wit.TypeDef{Kind: &wit.Result{OK: &wit.TypeDef{Kind: &wit.Record{Fields: []wit.Field{{Name: "value", Type: wit.U32{}}}}}, Err: wit.String{}}}
	w, err := NewLowerWrapper(&component.LowerDef{Name: "nil-record", Results: []wit.Type{result}}, BindResult0(func(context.Context) (*record, error) { return nil, nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("nil record must trap, not reinterpret the Go pointer slot as a record")
		}
	}()
	w.BuildRawFunc()(ctx, inst.instance, []uint64{64})
}

func TestTypedContinuationSkipsArgumentLifting(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	normalCalls, resumeCalls, checked := 0, 0, 0
	w, err := NewLowerWrapper(&component.LowerDef{
		Name: "continuation", Params: []wit.Type{wit.String{}, wit.String{}, wit.String{}},
		Results: []wit.Type{&wit.TypeDef{Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}}}},
	}, CheckedHostFunction{
		Handler: BindResult3WithResume(func(context.Context, string, string, string) (uint32, error) {
			normalCalls++
			return 1, nil
		}, func(context.Context) (uint32, error) {
			resumeCalls++
			return 2, nil
		}),
		Validate: func(context.Context, api.Module, []uint64) error { checked++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	w.BuildRawFunc()(ctx, inst.instance, []uint64{0, 0, 0, 0, 0, 0, 64})
	async := NewAsyncify()
	if err := async.StartRewind(ctx); err != nil {
		t.Fatal(err)
	}
	// The explicit continuation needs only the completion, even if the original
	// input buffers no longer contain decodable arguments.
	w.BuildRawFunc()(WithAsyncify(ctx, async), inst.instance, []uint64{1 << 30, 1, 1 << 30, 1, 1 << 30, 1, 64})
	value, _ := inst.instance.Memory().ReadUint32Le(68)
	if normalCalls != 1 || resumeCalls != 1 || checked != 2 || value != 2 {
		t.Fatalf("normal=%d resume=%d checked=%d result=%d", normalCalls, resumeCalls, checked, value)
	}
}

func TestCanonicalResultAreaCheckedBeforeHost(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	called := false
	w, err := NewLowerWrapper(&component.LowerDef{
		Name: "bad-retptr", Results: []wit.Type{&wit.TypeDef{Kind: &wit.Result{OK: wit.U32{}, Err: wit.String{}}}},
	}, BindResult0(func(context.Context) (uint32, error) { called = true; return 42, nil }))
	if err != nil {
		t.Fatal(err)
	}
	mem := inst.instance.Memory()
	addr := mem.Size() - 1
	mem.WriteByte(addr, 0x7f)
	defer func() {
		trapped := recover() != nil
		value, _ := mem.ReadByte(addr)
		if !trapped || called || value != 0x7f {
			t.Fatalf("trap=%v called=%v byte=%x", trapped, called, value)
		}
	}()
	w.BuildRawFunc()(ctx, inst.instance, []uint64{uint64(addr)})
}
