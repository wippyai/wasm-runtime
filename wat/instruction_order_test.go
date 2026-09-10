package wat

import (
	"bytes"
	"testing"
)

func TestMixedInstructionOrder(t *testing.T) {
	for _, tc := range []struct{ name, mixed, folded string }{
		{"call-before-loop", `call $mark (loop nop)`, `(call $mark) (loop nop)`},
		{"call-before-call", `call $mark (call $mark)`, `(call $mark) (call $mark)`},
		{"set-before-set", `i32.const 1 local.set 0 (local.set 0 (i32.const 2))`, `(local.set 0 (i32.const 1)) (local.set 0 (i32.const 2))`},
		{"drop-before-call", `i32.const 1 drop (call $mark)`, `(drop (i32.const 1)) (call $mark)`},
		{"store-before-call", `i32.const 0 i32.const 1 i32.store (call $mark)`, `(i32.store (i32.const 0) (i32.const 1)) (call $mark)`},
		{"flat-if-folded-body", `i32.const 1 if (call $mark) end`, `(if (i32.const 1) (then (call $mark)))`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrap := func(body string) string {
				return `(module (import "host" "mark" (func $mark)) (memory 1) (func (local i32) ` + body + `))`
			}
			mixed, err := Compile(wrap(tc.mixed))
			if err != nil {
				t.Fatal(err)
			}
			folded, err := Compile(wrap(tc.folded))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(mixed, folded) {
				t.Fatalf("equivalent forms changed instruction encoding\nmixed %x\nfolded %x", mixed, folded)
			}
		})
	}
}

func TestOffsetExpressionSyntaxOwnership(t *testing.T) {
	for _, pair := range [][2]string{
		{`(module (memory 1) (data (i32.add (i32.const 0) (i32.const 1)) "x"))`, `(module (memory 1) (data (offset (i32.add (i32.const 0) (i32.const 1))) "x"))`},
		{`(module (table 2 funcref) (func) (elem (i32.add (i32.const 0) (i32.const 1)) func 0))`, `(module (table 2 funcref) (func) (elem (offset (i32.add (i32.const 0) (i32.const 1))) func 0))`},
	} {
		a, err := Compile(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		b, err := Compile(pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatal("offset sugar changed expression encoding")
		}
	}
}
