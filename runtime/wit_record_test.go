package runtime

import (
	"context"
	"reflect"
	"testing"

	"go.bytecodealliance.org/wit"
)

func TestParseWitFunctions_Record(t *testing.T) {
	funcs, err := parseWitFunctions(`
		record point {
			x: s32,
			y: s32,
		}
		record segment { start: point, end: point, tags: list<string> }

		translate: func(p: point, d: s32) -> point;
		length: func(s: segment) -> option<point>;
	`)
	if err != nil {
		t.Fatalf("parseWitFunctions: %v", err)
	}
	if len(funcs) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(funcs))
	}

	sig := funcs["translate"]
	if sig == nil || len(sig.params) != 2 || len(sig.results) != 1 {
		t.Fatalf("translate signature = %+v", sig)
	}
	point := assertRecord(t, sig.params[0], "point", "x", "y")
	if sig.results[0] != point {
		t.Fatal("translate result must resolve to the same point definition as its param")
	}
	for _, f := range point.Kind.(*wit.Record).Fields {
		if _, ok := f.Type.(wit.S32); !ok {
			t.Fatalf("point.%s type = %T, want s32", f.Name, f.Type)
		}
	}

	seg := funcs["length"]
	segment := assertRecord(t, seg.params[0], "segment", "start", "end", "tags")
	fields := segment.Kind.(*wit.Record).Fields
	if fields[0].Type != point || fields[1].Type != point {
		t.Fatal("segment fields must reference the point definition")
	}
	opt, ok := seg.results[0].(*wit.TypeDef).Kind.(*wit.Option)
	if !ok || opt.Type != point {
		t.Fatalf("length result = %#v, want option<point>", seg.results[0])
	}
}

func TestParseWitFunctions_RecordErrors(t *testing.T) {
	tests := map[string]string{
		"unknown type":    `record r { a: missing } f: func(x: r);`,
		"recursive":       `record a { b: b } record b { a: a } f: func(x: a);`,
		"duplicate":       `record r { a: u8 } record r { b: u8 } f: func(x: r);`,
		"duplicate field": `record r { a: u8, a: u16 } f: func(x: r);`,
		"empty":           `record r { } f: func(x: r);`,
		"malformed field": `record r { a u8 } f: func(x: r);`,
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseWitFunctions(src); err == nil {
				t.Fatalf("expected error for %q", src)
			}
		})
	}
}

func assertRecord(t *testing.T, typ wit.Type, name string, fields ...string) *wit.TypeDef {
	t.Helper()
	td, ok := typ.(*wit.TypeDef)
	if !ok {
		t.Fatalf("type = %T, want record %s", typ, name)
	}
	rec, ok := td.Kind.(*wit.Record)
	if !ok {
		t.Fatalf("kind = %T, want record %s", td.Kind, name)
	}
	if td.Name == nil || *td.Name != name {
		t.Fatalf("record name = %v, want %s", td.Name, name)
	}
	if len(rec.Fields) != len(fields) {
		t.Fatalf("record %s has %d fields, want %d", name, len(rec.Fields), len(fields))
	}
	for i, f := range fields {
		if rec.Fields[i].Name != f {
			t.Fatalf("record %s field %d = %s, want %s", name, i, rec.Fields[i].Name, f)
		}
	}
	return td
}

// recordWAT implements record-typed functions over the canonical ABI:
// translate(p: point, d: s32) -> point flattens p to (i32, i32) and returns a
// pointer to a static return area; widen(m: mixed) -> mixed takes (i32, i64)
// and stores { a: u8 at 0, b: u64 at 8 }.
const recordWAT = `(module
	(memory (export "memory") 1)
	(func (export "translate") (param $x i32) (param $y i32) (param $d i32) (result i32)
		(i32.store (i32.const 64) (i32.add (local.get $x) (local.get $d)))
		(i32.store (i32.const 68) (i32.add (local.get $y) (local.get $d)))
		i32.const 64
	)
	(func (export "widen") (param $a i32) (param $b i64) (result i32)
		(i64.store (i32.const 128) (i64.const -1))
		(i32.store8 (i32.const 128) (i32.add (local.get $a) (i32.const 1)))
		(i64.store (i32.const 136) (i64.add (local.get $b) (i64.const 1)))
		i32.const 128
	)
)`

const recordWIT = `
	record point { x: s32, y: s32 }
	record mixed { a: u8, b: u64 }

	translate: func(p: point, d: s32) -> point;
	widen: func(m: mixed) -> mixed;
`

type witPoint struct {
	X int32
	Y int32
}

type witMixed struct {
	A uint8
	B uint64
}

func TestWIT_RecordRoundTrip(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	mod, err := rt.LoadWAT(ctx, recordWAT, recordWIT)
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}
	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	t.Run("map", func(t *testing.T) {
		got, err := inst.Call(ctx, "translate", map[string]any{"x": int32(1), "y": int32(-2)}, int32(10))
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		want := map[string]any{"x": int32(11), "y": int32(8)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("translate = %#v, want %#v", got, want)
		}
	})

	t.Run("mixed map", func(t *testing.T) {
		got, err := inst.Call(ctx, "widen", map[string]any{"a": uint8(6), "b": uint64(1 << 40)})
		if err != nil {
			t.Fatalf("widen: %v", err)
		}
		want := map[string]any{"a": uint8(7), "b": uint64(1<<40 + 1)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("widen = %#v, want %#v", got, want)
		}
	})

	t.Run("mixed struct", func(t *testing.T) {
		got, err := inst.Call(ctx, "widen", witMixed{A: 6, B: 1 << 40})
		if err != nil {
			t.Fatalf("widen: %v", err)
		}
		if want := (witMixed{A: 7, B: 1<<40 + 1}); got != want {
			t.Fatalf("widen = %#v, want %#v", got, want)
		}
	})

	t.Run("mixed into", func(t *testing.T) {
		params, results, err := mod.GetFunctionTypes("widen")
		if err != nil {
			t.Fatal(err)
		}
		var got witMixed
		if err := inst.CallInto(ctx, "widen", params, results, &got, witMixed{A: 6, B: 1 << 40}); err != nil {
			t.Fatalf("widen: %v", err)
		}
		if want := (witMixed{A: 7, B: 1<<40 + 1}); got != want {
			t.Fatalf("widen = %+v, want %+v", got, want)
		}
	})

	t.Run("point into", func(t *testing.T) {
		params, results, err := mod.GetFunctionTypes("translate")
		if err != nil {
			t.Fatal(err)
		}
		var got witPoint
		if err := inst.CallInto(ctx, "translate", params, results, &got, witPoint{X: 3, Y: 4}, int32(-1)); err != nil {
			t.Fatalf("translate: %v", err)
		}
		if want := (witPoint{X: 2, Y: 3}); got != want {
			t.Fatalf("translate = %+v, want %+v", got, want)
		}
	})
}
