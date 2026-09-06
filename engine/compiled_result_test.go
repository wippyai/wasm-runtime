package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/component"
)

func TestCompiledResult_Regression_Bool(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	mem := &WazeroMemory{mem: inst.instance.Memory()}
	alloc := &moduleAllocator{ctx: ctx, allocFunc: inst.instance.ExportedFunction(CabiRealloc)}

	def := &component.LowerDef{
		Name:    "test-bool",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{resultBoolString()},
	}

	tests := []struct {
		handler func(context.Context, uint32) (bool, error)
		name    string
	}{
		{
			name: "success_true",
			handler: func(_ context.Context, _ uint32) (bool, error) {
				return true, nil
			},
		},
		{
			name: "success_false",
			handler: func(_ context.Context, _ uint32) (bool, error) {
				return false, nil
			},
		},
		{
			name: "error",
			handler: func(_ context.Context, _ uint32) (bool, error) {
				return false, errors.New("bool-error")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewLowerWrapper(def, tc.handler)
			if err != nil {
				t.Fatalf("NewLowerWrapper: %v", err)
			}
			if w.resultSuccessType == nil {
				t.Fatal("expected compiled success type for bool")
			}

			// Run compiled path at retptr1
			retptr1 := uint32(64)
			raw := w.BuildRawFunc()
			raw(ctx, inst.instance, []uint64{0, uint64(retptr1)})

			// Compute dynamic expectation at retptr2
			retptr2 := uint32(128)
			okVal, errVal := tc.handler(ctx, 0)
			errType := resultErrType(def.Results[0])
			folded := foldResultValue(okVal, errVal, errType)
			if err := w.storeResultToMemoryWithAlloc(def.Results[0], folded, retptr2, mem, alloc); err != nil {
				t.Fatalf("dynamic store: %v", err)
			}

			// Size of result<bool, string> in memory is 12 bytes
			got, ok := inst.instance.Memory().Read(retptr1, 12)
			if !ok {
				t.Fatal("read retptr1 failed")
			}
			want, ok := inst.instance.Memory().Read(retptr2, 12)
			if !ok {
				t.Fatal("read retptr2 failed")
			}

			// For strings, data pointers might differ because alloc calls are sequential,
			// but discriminant and length must match, and referenced string bytes must match.
			if got[0] != want[0] {
				t.Fatalf("discriminant mismatch: got %d, want %d", got[0], want[0])
			}
			if tc.name == "error" {
				discGot, strGot := readResultString(t, inst, retptr1)
				discWant, strWant := readResultString(t, inst, retptr2)
				if discGot != discWant || strGot != strWant {
					t.Fatalf("error payload mismatch: got (%d, %q), want (%d, %q)", discGot, strGot, discWant, strWant)
				}
			} else if got[4] != want[4] {
				t.Fatalf("bool payload mismatch at offset 4: got %d, want %d", got[4], want[4])
			}
		})
	}
}

type testRecord struct {
	Value  uint32
	Active bool
}

func resultRecordString() *wit.TypeDef {
	return &wit.TypeDef{
		Kind: &wit.Result{
			OK: &wit.TypeDef{
				Kind: &wit.Record{
					Fields: []wit.Field{
						{Name: "value", Type: wit.U32{}},
						{Name: "active", Type: wit.Bool{}},
					},
				},
			},
			Err: wit.String{},
		},
	}
}

func TestCompiledResult_Regression_Record(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	mem := &WazeroMemory{mem: inst.instance.Memory()}
	alloc := &moduleAllocator{ctx: ctx, allocFunc: inst.instance.ExportedFunction(CabiRealloc)}

	def := &component.LowerDef{
		Name:    "test-record",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{resultRecordString()},
	}

	tests := []struct {
		handler func(context.Context, uint32) (testRecord, error)
		name    string
	}{
		{
			name: "success",
			handler: func(_ context.Context, _ uint32) (testRecord, error) {
				return testRecord{Value: 42, Active: true}, nil
			},
		},
		{
			name: "error",
			handler: func(_ context.Context, _ uint32) (testRecord, error) {
				return testRecord{}, errors.New("record-fail")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewLowerWrapper(def, tc.handler)
			if err != nil {
				t.Fatalf("NewLowerWrapper: %v", err)
			}
			if w.resultSuccessType == nil {
				t.Fatal("expected compiled success type for record")
			}

			retptr1 := uint32(64)
			raw := w.BuildRawFunc()
			raw(ctx, inst.instance, []uint64{0, uint64(retptr1)})

			retptr2 := uint32(128)
			okVal, errVal := tc.handler(ctx, 0)
			errType := resultErrType(def.Results[0])
			folded := foldResultValue(okVal, errVal, errType)
			if err := w.storeResultToMemoryWithAlloc(def.Results[0], folded, retptr2, mem, alloc); err != nil {
				t.Fatalf("dynamic store: %v", err)
			}

			if tc.name == "error" {
				discGot, strGot := readResultString(t, inst, retptr1)
				discWant, strWant := readResultString(t, inst, retptr2)
				if discGot != discWant || strGot != strWant {
					t.Fatalf("error payload mismatch: got (%d, %q), want (%d, %q)", discGot, strGot, discWant, strWant)
				}
			} else {
				// Compare 12 bytes of result layout
				got, ok := inst.instance.Memory().Read(retptr1, 12)
				if !ok {
					t.Fatal("read retptr1 failed")
				}
				want, ok := inst.instance.Memory().Read(retptr2, 12)
				if !ok {
					t.Fatal("read retptr2 failed")
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("record memory mismatch:\ngot:  %v\nwant: %v", got, want)
				}
			}
		})
	}
}

func resultOptionString() *wit.TypeDef {
	return &wit.TypeDef{
		Kind: &wit.Result{
			OK: &wit.TypeDef{
				Kind: &wit.Option{
					Type: wit.U32{},
				},
			},
			Err: wit.String{},
		},
	}
}

func TestCompiledResult_Regression_Option(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	mem := &WazeroMemory{mem: inst.instance.Memory()}
	alloc := &moduleAllocator{ctx: ctx, allocFunc: inst.instance.ExportedFunction(CabiRealloc)}

	def := &component.LowerDef{
		Name:    "test-option",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{resultOptionString()},
	}

	val := uint32(12345)
	tests := []struct {
		handler func(context.Context, uint32) (*uint32, error)
		name    string
	}{
		{
			name: "some",
			handler: func(_ context.Context, _ uint32) (*uint32, error) {
				return &val, nil
			},
		},
		{
			name: "none",
			handler: func(_ context.Context, _ uint32) (*uint32, error) {
				return nil, nil
			},
		},
		{
			name: "error",
			handler: func(_ context.Context, _ uint32) (*uint32, error) {
				return nil, errors.New("opt-err")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := NewLowerWrapper(def, tc.handler)
			if err != nil {
				t.Fatalf("NewLowerWrapper: %v", err)
			}
			if w.resultSuccessType == nil {
				t.Fatal("expected compiled success type for option")
			}

			retptr1 := uint32(64)
			raw := w.BuildRawFunc()
			raw(ctx, inst.instance, []uint64{0, uint64(retptr1)})

			retptr2 := uint32(128)
			okVal, errVal := tc.handler(ctx, 0)
			errType := resultErrType(def.Results[0])
			folded := foldResultValue(okVal, errVal, errType)
			if err := w.storeResultToMemoryWithAlloc(def.Results[0], folded, retptr2, mem, alloc); err != nil {
				t.Fatalf("dynamic store: %v", err)
			}

			if tc.name == "error" {
				discGot, strGot := readResultString(t, inst, retptr1)
				discWant, strWant := readResultString(t, inst, retptr2)
				if discGot != discWant || strGot != strWant {
					t.Fatalf("error payload mismatch: got (%d, %q), want (%d, %q)", discGot, strGot, discWant, strWant)
				}
			} else {
				got, ok := inst.instance.Memory().Read(retptr1, 12)
				if !ok {
					t.Fatal("read retptr1 failed")
				}
				want, ok := inst.instance.Memory().Read(retptr2, 12)
				if !ok {
					t.Fatal("read retptr2 failed")
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("option memory mismatch:\ngot:  %v\nwant: %v", got, want)
				}
			}
		})
	}
}

func TestCompiledResult_InvalidMemory(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	def := &component.LowerDef{
		Name:    "test-invalid-mem",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{resultBoolString()},
	}

	w, err := NewLowerWrapper(def, func(_ context.Context, _ uint32) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("NewLowerWrapper: %v", err)
	}

	raw := w.BuildRawFunc()

	// retptr way out of bounds - should trap cleanly via panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on out of bounds store")
		}
		errStr := fmt.Sprint(r)
		if !bytes.Contains([]byte(errStr), []byte("result pointer")) {
			t.Fatalf("expected result-pointer trap error, got: %v", errStr)
		}
	}()

	raw(ctx, inst.instance, []uint64{0, uint64(0xFFFFFFF0)})
}

func TestCompiledResult_Fallback_UnsupportedType(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	// An unsupported Go return type for compilation (any / interface{})
	def := &component.LowerDef{
		Name:    "test-fallback",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{resultBoolString()},
	}

	w, err := NewLowerWrapper(def, func(_ context.Context, _ uint32) (any, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("NewLowerWrapper: %v", err)
	}
	if w.resultSuccessType != nil {
		t.Fatal("expected resultSuccessType to be nil for unsupported interface{} type")
	}

	retptr := uint32(64)
	raw := w.BuildRawFunc()
	raw(ctx, inst.instance, []uint64{0, uint64(retptr)})

	got, ok := inst.instance.Memory().Read(retptr, 8)
	if !ok {
		t.Fatal("read retptr failed")
	}
	// Discriminant 0 at byte 0, bool true at byte 4
	if got[0] != 0 || got[4] != 1 {
		t.Fatalf("expected disc 0 and bool 1, got disc %d bool %d", got[0], got[4])
	}
}

func TestCompiledResult_Fallback_FlatResults(t *testing.T) {
	ctx, eng, inst := instantiateLowerTestModule(t)
	defer eng.Close(ctx)
	defer inst.Close(ctx)

	// Flat result: result<u32, u32> fits within 2 stack values (flat result count = 2)
	// Wait, MaxFlatResults is 1, so flatResultCount(result<u32, u32>) is 2 > 1 -> uses retptr.
	// What fits in flat results? A function returning (uint32) with 0 error!
	def := &component.LowerDef{
		Name:    "test-flat",
		Params:  []wit.Type{wit.U32{}},
		Results: []wit.Type{wit.U32{}},
	}

	w, err := NewLowerWrapper(def, func(_ context.Context, a uint32) uint32 {
		return a * 10
	})
	if err != nil {
		t.Fatalf("NewLowerWrapper: %v", err)
	}
	if w.usesRetptr() {
		t.Fatal("expected flat results for single u32")
	}

	raw := w.BuildRawFunc()
	stack := []uint64{5}
	raw(ctx, inst.instance, stack)

	if stack[0] != 50 {
		t.Fatalf("expected stack[0] = 50, got %d", stack[0])
	}
}
