package ir

import (
	"testing"

	"github.com/wippyai/wasm-runtime/wasm"
)

func TestSavedCarriersFollowSourceScopes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scoped bool
		want   int
	}{
		{name: "production", scoped: true, want: 0},
		{name: "unscoped-structural", scoped: false, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values, err := valueFixture(t, `(module (import "env" "yield" (func $yield))
   (func (result i32) i32.const 7 i32.const 1 if (param i32) (result i32) i32.const 2 i32.add else i32.const 3 i32.add end call $yield))`)
			if err != nil {
				t.Fatal(err)
			}
			var next uint32
			config := &LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { local := next; next++; return local }}
			var lowered *LoweredControl
			if tc.scoped {
				lowered, err = Linearize(values, config)
			} else {
				lowered, err = linearizeUnscopedStorageForTest(values, config)
			}
			if err != nil {
				t.Fatal(err)
			}
			if next != 3 {
				t.Fatalf("expected three declared carriers, got %d", next)
			}
			if got := len(lowered.suspensions[0].ControlLocals); got != tc.want {
				t.Fatalf("scoped=%t saved %d carriers, want %d", tc.scoped, got, tc.want)
			}
		})
	}
}

// linearizeUnscopedStorageForTest isolates the carrier-selection experiment
// from production Linearize. It intentionally changes only storage lifetime
// policy; no opaque source operation is admitted through this test path.
func linearizeUnscopedStorageForTest(values *ValuePlan, config *LinearizeConfig) (*LoweredControl, error) {
	return linearizeWithStorage(values.control, config, false, values)
}

func TestSavedCarriersKeepOnlyActiveAncestors(t *testing.T) {
	values, err := valueFixture(t, `(module (import "env" "yield" (func $yield))
  (func (result i32) i32.const 1 if (result i32)
   i32.const 7 i32.const 1 if (param i32) (result i32) i32.const 2 i32.add else i32.const 3 i32.add end call $yield
  else i32.const 99 end call $yield))`)
	if err != nil {
		t.Fatal(err)
	}
	var next uint32
	lowered, err := Linearize(values, &LinearizeConfig{AllocLocal: func(wasm.ValType) uint32 { local := next; next++; return local }})
	if err != nil {
		t.Fatal(err)
	}
	if len(lowered.suspensions) != 2 {
		t.Fatal("lost source call bindings")
	}
	first, second := lowered.suspensions[0], lowered.suspensions[1]
	if first.SourceCallID != 1 || len(first.ControlLocals) != 2 || first.ControlLocals[0] != 0 || first.ControlLocals[1] != 1 {
		t.Fatalf("wrong active ancestor carriers: %+v", first)
	}
	if second.SourceCallID != 2 || len(second.ControlLocals) != 0 {
		t.Fatalf("completed ancestor still saved: %+v", second)
	}
}
