package engine

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
	"github.com/wippyai/wasm-runtime/wat"
)

func TestFunctionListsMatchEveryExportAlias(t *testing.T) {
	data, err := wat.Compile(`(module
  (import "env" "yield" (func $yield))
  (func $target call $yield)
  (export "selected" (func $target))
  (export "other" (func $target)))`)
	if err != nil {
		t.Fatal(err)
	}
	matcher := &testFuncMatcher{names: map[string]bool{"selected": true}}
	for _, test := range []struct {
		name string
		cfg  Config
		want bool
	}{
		{"add", Config{AddList: matcher}, true},
		{"only", Config{Matcher: newExactMatcher([]string{"env.yield"}), OnlyList: matcher}, true},
		{"remove", Config{Matcher: newExactMatcher([]string{"env.yield"}), RemoveList: matcher}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, err := wasm.ParseModule(data)
			if err != nil {
				t.Fatal(err)
			}
			for order := 0; order < 2; order++ {
				marked, err := New(test.cfg).findAsyncFuncs(m)
				if err != nil {
					t.Fatal(err)
				}
				if marked[1] != test.want {
					t.Fatalf("export order %d: target marked=%v, want %v", order, marked[1], test.want)
				}
				m.Exports[0], m.Exports[1] = m.Exports[1], m.Exports[0]
			}
		})
	}
}
