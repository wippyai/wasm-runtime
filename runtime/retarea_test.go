package runtime

import (
	"context"
	"reflect"
	"testing"

	"go.bytecodealliance.org/wit"
)

// retAreaWAT returns a pointer to a static return area holding a u8 at offset 0
// and a u64 at offset 8, the canonical ABI layout of tuple<u8, u64> and of
// record { a: u8, b: u64 }. Bytes 1..7 are padding filled with 0xff.
const retAreaWAT = `(module
	(memory (export "memory") 1)
	(data (i32.const 64) "\07\ff\ff\ff\ff\ff\ff\ff\88\77\66\55\44\33\22\11")
	(func (export "pair") (result i32)
		i32.const 64
	)
)`

const retAreaB = uint64(0x1122334455667788)

type retAreaPair struct {
	A uint8
	B uint64
}

func instantiateRetArea(t *testing.T) (*Instance, func()) {
	t.Helper()
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.LoadWAT(ctx, retAreaWAT, `pair: func() -> (u8, u64);`)
	if err != nil {
		rt.Close(ctx)
		t.Fatalf("LoadWAT: %v", err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		rt.Close(ctx)
		t.Fatalf("Instantiate: %v", err)
	}
	return inst, func() {
		inst.Close(ctx)
		rt.Close(ctx)
	}
}

func TestRetArea_MultiResultAlignment(t *testing.T) {
	inst, done := instantiateRetArea(t)
	defer done()

	got, err := inst.CallWithTypes(context.Background(), "pair", nil, []wit.Type{wit.U8{}, wit.U64{}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	want := []any{uint8(7), retAreaB}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %#v, want %#v", got, want)
	}
}

func TestRetArea_MultiResultIntoAlignment(t *testing.T) {
	inst, done := instantiateRetArea(t)
	defer done()

	var got retAreaPair
	err := inst.CallInto(context.Background(), "pair", nil, []wit.Type{wit.U8{}, wit.U64{}}, &got)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if want := (retAreaPair{A: 7, B: retAreaB}); got != want {
		t.Fatalf("results = %+v, want %+v", got, want)
	}
}

func TestRetArea_RecordIntoAlignment(t *testing.T) {
	inst, done := instantiateRetArea(t)
	defer done()

	record := &wit.TypeDef{Kind: &wit.Record{Fields: []wit.Field{
		{Name: "a", Type: wit.U8{}},
		{Name: "b", Type: wit.U64{}},
	}}}

	var got retAreaPair
	err := inst.CallInto(context.Background(), "pair", nil, []wit.Type{record}, &got)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if want := (retAreaPair{A: 7, B: retAreaB}); got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}
