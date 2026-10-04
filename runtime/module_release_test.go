package runtime

import (
	"context"
	"sync"
	"testing"
)

func TestModuleReleasePreservesSharedRuntime(t *testing.T) {
	ctx := context.Background()
	rt, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	first, err := rt.LoadWASM(ctx, addWASM, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := rt.LoadWASM(ctx, addWASM, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, module := range []*Module{first, second} {
		if err := module.Compile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := first.Release(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if _, err := first.Instantiate(ctx); err == nil {
		t.Fatal("released module admitted a new instance")
	}
	instance, err := second.Instantiate(ctx)
	if err != nil {
		t.Fatalf("sibling module was closed: %v", err)
	}
	defer instance.Close(ctx)
	result, err := instance.wazeroInstance.GetExportedFunction("add").Call(ctx, 2, 3)
	if err != nil || len(result) != 1 || result[0] != 5 {
		t.Fatalf("sibling call: %v, %v", result, err)
	}
	if _, err := rt.LoadWASM(ctx, addWASM, ""); err != nil {
		t.Fatalf("shared runtime cannot load after individual release: %v", err)
	}
}
