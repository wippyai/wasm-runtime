package engine

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/asyncify"
	"github.com/wippyai/wasm-runtime/linker"
)

func TestAsyncifyContextPreservesControllerOwnership(t *testing.T) {
	root, child, sibling := NewAsyncify(), NewAsyncify(), NewAsyncify()
	entry := &engineCallContext{asyncify: root}
	entry.InstanceContext = linker.NewInstanceContext(context.Background(), nil)
	wrapped, cancel := context.WithCancel(entry)
	defer cancel()
	first := asyncify.WithRuntimeController(wrapped, child)
	second := asyncify.WithRuntimeController(wrapped, sibling)
	for _, tc := range []struct {
		ctx  context.Context
		want *Asyncify
	}{{wrapped, root}, {first, child}, {second, sibling}} {
		if got := GetAsyncify(tc.ctx); got != tc.want {
			t.Fatalf("controller = %p, want %p", got, tc.want)
		}
	}
	state, present := asyncify.RuntimeControllerStateFromContext(first)
	parent, hasParent := asyncify.ParentRuntimeController(state)
	if !present || !hasParent || state.Controller != child || parent.Controller != root {
		t.Fatal("retained child activation changed after sibling entry")
	}

	masked := asyncify.WithRuntimeController(first, nil)
	if GetAsyncify(masked) != nil {
		t.Fatal("untracked core borrowed an ancestor controller")
	}
}

func TestAsyncifyRootControllerLookupDoesNotAllocate(t *testing.T) {
	root := NewAsyncify()
	entry := &engineCallContext{asyncify: root}
	entry.InstanceContext = linker.NewInstanceContext(context.Background(), nil)
	wrapped, cancel := context.WithCancel(entry)
	defer cancel()
	var selected *Asyncify
	allocations := testing.AllocsPerRun(100, func() { selected = GetAsyncify(wrapped) })
	if selected != root || allocations != 0 {
		t.Fatalf("root lookup = %p, %g allocations; want %p, 0 allocations", selected, allocations, root)
	}
}
