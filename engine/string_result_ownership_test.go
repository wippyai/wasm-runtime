package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wat"
	"go.bytecodealliance.org/wit"
)

func TestStringResultOwnsBytesWithoutPostReturn(t *testing.T) {
	ctx := t.Context()
	code, err := wat.Compile(`(module
		(memory (export "memory") 1)
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) i32.const 1024)
		(func (export "echo") (param $ptr i32) (param $len i32) (result i32)
			i32.const 0 local.get $ptr i32.store
			i32.const 4 local.get $len i32.store
			i32.const 0))`)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close(ctx) })
	mod, err := eng.LoadModule(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	types := []wit.Type{wit.String{}}
	for _, into := range []bool{false, true} {
		t.Run(map[bool]string{false: "CallWithTypes", true: "CallInto"}[into], func(t *testing.T) {
			inst, err := mod.Instantiate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer inst.Close(ctx)
			call := func(input string) string {
				t.Helper()
				if into {
					var out string
					if err := inst.CallInto(ctx, "echo", types, types, &out, input); err != nil {
						t.Fatal(err)
					}
					return out
				}
				out, err := inst.CallWithTypes(ctx, "echo", types, types, input)
				if err != nil {
					t.Fatal(err)
				}
				return out.(string)
			}
			first := call("first")
			second := call("other")
			if first != "first" || second != "other" {
				t.Fatalf("guest reuse mutated returned strings: first=%q second=%q", first, second)
			}
		})
	}
}

func TestStringCallRejectsOutOfBoundsAllocationBeforeGuestCall(t *testing.T) {
	ctx := t.Context()
	code, err := wat.Compile(`(module
		(memory (export "memory") 1)
		(global $called (mut i32) (i32.const 0))
		(func (export "cabi_realloc") (param i32 i32 i32 i32) (result i32) i32.const 65535)
		(func (export "echo") (param i32 i32) (result i32)
			i32.const 1 global.set $called i32.const 0)
		(func (export "called") (result i32) global.get $called))`)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	mod, err := eng.LoadModule(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	types := []wit.Type{wit.String{}}
	if _, err := inst.CallWithTypes(ctx, "echo", types, types, "invalid allocation"); err == nil {
		t.Fatal("out-of-bounds allocation accepted")
	}
	called, err := inst.CallWithTypes(ctx, "called", nil, []wit.Type{wit.U32{}})
	if err != nil {
		t.Fatal(err)
	}
	if called != uint32(0) {
		t.Fatal("guest ran with an unwritten argument")
	}
}
