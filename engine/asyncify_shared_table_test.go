package engine

import (
	"strings"
	"testing"
)

// A's exported table contains a direct reference to A's guest function. B's
// indirect call enters A without a Go bridge, so it cannot select A's saved
// continuation. This valid component must be rejected by the async profile.
const crossCoreSharedTableWAT = `(component
  (core module $a
    (table (export "callbacks") 1 1 funcref)
    (func $callback (result i32) (i32.const 42))
    (elem (i32.const 0) $callback)
  )
  (core instance $ai (instantiate $a))
  (alias core export $ai "callbacks" (core table $callbacks))
  (core instance $arguments (export "callbacks" (table $callbacks)))
  (core module $b
    (type $callback (func (result i32)))
    (import "a" "callbacks" (table 1 1 funcref))
    (func (export "run") (result i32)
      (call_indirect (type $callback) (i32.const 0)))
  )
  (core instance $bi (instantiate $b (with "a" (instance $arguments))))
  (alias core export $bi "run" (core func $run))
  (func (export "run") (result u32) (canon lift (core func $run)))
)`

// Returning a function reference and installing it into a private table has
// the same missing-boundary problem despite neither core importing a table.
const crossCoreReturnedReferenceWAT = `(component
  (core module $a
    (func $callback (result i32) (i32.const 42))
    (elem declare func $callback)
    (func (export "get") (result funcref) (ref.func $callback))
  )
  (core instance $ai (instantiate $a))
  (core module $b
    (type $callback (func (result i32)))
    (import "a" "get" (func $get (result funcref)))
    (table 1 1 funcref)
    (func (export "run") (result i32)
      (table.set (i32.const 0) (call $get))
      (call_indirect (type $callback) (i32.const 0)))
  )
  (core instance $bi (instantiate $b (with "a" (instance $ai))))
  (alias core export $bi "run" (core func $run))
  (func (export "run") (result u32) (canon lift (core func $run)))
)`

func TestAsyncifyRejectsExecutableSharedTableComponent(t *testing.T) {
	t.Run("shared table", func(t *testing.T) { rejectCrossCoreReferenceComponent(t, crossCoreSharedTableWAT) })
	t.Run("returned reference", func(t *testing.T) { rejectCrossCoreReferenceComponent(t, crossCoreReturnedReferenceWAT) })
}

func rejectCrossCoreReferenceComponent(t *testing.T, source string) {
	t.Helper()
	ctx := t.Context()
	eng, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close(ctx)
	mod, err := eng.LoadModule(ctx, componentFixture(t, source))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close(ctx)
	inst, err := mod.InstantiateWithConfig(ctx, &InstanceConfig{EnableAsyncify: true})
	if inst != nil {
		defer inst.Close(ctx)
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported cross-core continuation boundary") {
		t.Fatalf("expected shared-table execution-profile rejection, got %v", err)
	}
}
