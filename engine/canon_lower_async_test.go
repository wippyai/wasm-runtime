package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/wat"
)

func resultStringString() *wit.TypeDef {
	return &wit.TypeDef{Kind: &wit.Result{OK: wit.String{}, Err: wit.String{}}}
}

func resultBoolString() *wit.TypeDef {
	return &wit.TypeDef{Kind: &wit.Result{OK: wit.Bool{}, Err: wit.String{}}}
}

func instantiateLowerTestModule(t *testing.T) (context.Context, *WazeroEngine, *WazeroInstance) {
	t.Helper()
	ctx := context.Background()
	wasmBytes, err := wat.Compile(echoWAT)
	if err != nil {
		t.Fatalf("compile WAT: %v", err)
	}
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatalf("NewWazeroEngine: %v", err)
	}
	mod, err := eng.LoadModule(ctx, wasmBytes)
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("LoadModule: %v", err)
	}
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{Name: ""})
	if err != nil {
		eng.Close(ctx)
		t.Fatalf("Instantiate: %v", err)
	}
	return ctx, eng, inst
}

func readResultString(t *testing.T, inst *WazeroInstance, retptr uint32) (disc byte, s string) {
	t.Helper()
	header, ok := inst.instance.Memory().Read(retptr, 12)
	if !ok {
		t.Fatalf("read result header at %d", retptr)
	}
	ptr := binary.LittleEndian.Uint32(header[4:8])
	n := binary.LittleEndian.Uint32(header[8:12])
	if n == 0 {
		return header[0], ""
	}
	data, ok := inst.instance.Memory().Read(ptr, n)
	if !ok {
		t.Fatalf("read result string at %d len %d", ptr, n)
	}
	return header[0], string(data)
}

func TestLowerWrapper_AsyncifyRewindRetainsRetptrAndArgs(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	const (
		argPtr = uint32(200)
		retptr = uint32(64)
		arg    = "alice"
	)
	if !inst.instance.Memory().Write(argPtr, []byte(arg)) {
		t.Fatal("write arg string")
	}

	async := NewAsyncify()
	sched := NewScheduler(async)
	ctx = WithAsyncify(ctx, async)
	ctx = WithScheduler(ctx, sched)

	var seen []string
	w, err := NewLowerWrapper(&component.LowerDef{
		Name:    "receive",
		Params:  []wit.Type{wit.String{}},
		Results: []wit.Type{resultStringString()},
	}, func(ctx context.Context, target string) (string, error) {
		seen = append(seen, target)
		a := GetAsyncify(ctx)
		if a != nil && a.IsRewinding(ctx) {
			if _, err := Resume(ctx); err != nil {
				return "", err
			}
			return "from:" + target, nil
		}
		if err := Suspend(ctx, &mockPendingOp{}); err != nil {
			return "", err
		}
		return "", nil
	})
	if err != nil {
		t.Fatalf("NewLowerWrapper: %v", err)
	}
	if !w.usesRetptr() {
		t.Fatal("result<string,string> must use retptr")
	}

	raw := w.BuildRawFunc()
	raw(ctx, inst.instance, []uint64{uint64(argPtr), uint64(len(arg)), uint64(retptr)})
	if !async.IsUnwinding(ctx) {
		t.Fatal("first call should park")
	}
	if disc, s := readResultString(t, inst, retptr); disc != 0 || s != "" {
		t.Fatalf("unwind must not write the result area, disc=%d s=%q", disc, s)
	}

	if err := async.StartRewind(ctx); err != nil {
		t.Fatal(err)
	}
	raw(ctx, inst.instance, []uint64{0, 0, 0})

	if len(seen) != 2 || seen[0] != arg || seen[1] != arg {
		t.Fatalf("rewind lost original args: %v", seen)
	}
	disc, s := readResultString(t, inst, retptr)
	if disc != 0 || s != "from:alice" {
		t.Fatalf("result at retptr=%d disc=%d s=%q", retptr, disc, s)
	}
	zeroDisc, zeroS := readResultString(t, inst, 0)
	if zeroDisc == 0 && zeroS == "from:alice" {
		t.Fatal("result was written at retptr=0")
	}
}

func TestLowerWrapper_ResultErrStringFromGoError(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	w, err := NewLowerWrapper(&component.LowerDef{
		Name:    "send",
		Params:  []wit.Type{wit.String{}},
		Results: []wit.Type{resultBoolString()},
	}, func(_ context.Context, target string) (bool, error) {
		if target == "" {
			return false, errors.New("invalid-target")
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("NewLowerWrapper: %v", err)
	}

	const retptr = uint32(64)
	raw := w.BuildRawFunc()
	raw(ctx, inst.instance, []uint64{0, 0, uint64(retptr)})

	disc, s := readResultString(t, inst, retptr)
	if disc != 1 {
		t.Fatalf("want err discriminant 1, got %d", disc)
	}
	if s != "invalid-target" {
		t.Fatalf("err payload = %q, want invalid-target", s)
	}
}

func TestHandlerErrPayload_OrdinaryErrorIsString(t *testing.T) {
	got := handlerErrPayload(errors.New("invalid-target"), wit.String{})
	s, ok := got.(string)
	if !ok || s != "invalid-target" {
		t.Fatalf("got %#v (%T), want string invalid-target", got, got)
	}
}

func TestHandlerErrPayload_CodeField(t *testing.T) {
	type coded struct{ Code uint32 }
	got := handlerErrPayload(coded{Code: 7}, nil)
	if got != uint64(7) {
		t.Fatalf("got %#v (%T), want uint64(7)", got, got)
	}
}

func TestHandlerErrPayload_WITErrorPayload(t *testing.T) {
	got := handlerErrPayload(witPayloadErr{}, wit.String{})
	if got != "stream-error" {
		t.Fatalf("got %#v, want stream-error", got)
	}
}

type witPayloadErr struct{}

func (witPayloadErr) Error() string        { return "ignored" }
func (witPayloadErr) WITErrorPayload() any { return "stream-error" }

func TestAsyncify_HostArgsClearedOnResetAndCancel(t *testing.T) {
	a := NewAsyncify()
	a.ParkHostArgs([]uint64{64, 1, 2})
	s := NewScheduler(a)
	s.Reset()
	if got := a.TakeHostArgs(); got != nil {
		t.Fatalf("Reset leaked host args: %v", got)
	}

	s = NewScheduler(a)
	if err := s.Execute(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	a.ParkHostArgs([]uint64{9})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Step(ctx, nil); err == nil {
		t.Fatal("expected canceled step")
	}
	if got := a.TakeHostArgs(); got != nil {
		t.Fatalf("canceled step leaked host args: %v", got)
	}
}

func TestLowerWrapper_StoreErrorTraps(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	w, err := NewLowerWrapper(&component.LowerDef{
		Name:    "send",
		Params:  nil,
		Results: []wit.Type{resultBoolString()},
	}, func(context.Context) (bool, error) {
		return false, errors.New("invalid-target")
	})
	if err != nil {
		t.Fatalf("NewLowerWrapper: %v", err)
	}

	// Force a type mismatch by folding is bypassed: handler returns (bool, error)
	// which is folded to result<bool,string>. To trap, store a non-result value.
	w.def.Results = []wit.Type{wit.String{}}
	w.resultTypes = nil

	defer func() {
		if recover() == nil {
			t.Fatal("store mismatch must trap")
		}
	}()
	w.BuildRawFunc()(ctx, inst.instance, []uint64{64})
}

func TestLowerWrapper_LiftErrorTraps(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	w, err := NewLowerWrapper(&component.LowerDef{
		Name:    "send",
		Params:  []wit.Type{wit.String{}},
		Results: []wit.Type{wit.U32{}},
	}, func(_ context.Context, _ string) uint32 { return 0 })
	if err != nil {
		t.Fatalf("NewLowerWrapper: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("out-of-bounds lift must trap")
		}
	}()
	w.BuildRawFunc()(ctx, inst.instance, []uint64{1 << 30, 100})
}

func TestParkedHostArgsOwnedAndReusable(t *testing.T) {
	for _, size := range []int{0, 1, 16, 17, 40} {
		a := NewAsyncify()
		input := make([]uint64, size)
		for i := range input {
			input[i] = uint64(i + 1)
		}
		a.ParkHostArgs(input)
		clear(input)
		saved := a.TakeHostArgs()
		a.ParkHostArgs(input)
		for i, v := range saved {
			if v != uint64(i+1) {
				t.Fatalf("size %d owned arg %d changed: %d", size, i, v)
			}
		}
		a.ClearHostArgs()
		for _, length := range []int{0, size, size + 1} {
			a.ParkHostArgs(saved)
			target := make([]uint64, length)
			restored := a.restoreParkedArgs(target)
			a.ParkHostArgs(input)
			for i, v := range saved {
				if restored[i] != v {
					t.Fatalf("size %d target %d argument %d changed", size, length, i)
				}
			}
			a.ClearHostArgs()
			if a.TakeHostArgs() != nil {
				t.Fatal("clear retained arguments")
			}
		}
	}
}
